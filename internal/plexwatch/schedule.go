package plexwatch

import (
	"context"
	"errors"
	"math"
	"net/http"
	"time"
)

// RunImports is started under cluster leadership. Each pass is bounded and the
// persisted lease also serializes manual imports and overlapping leaders.
func (s *Service) RunImports(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		s.importDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-s.closed:
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) importDue(ctx context.Context) {
	now := s.now().UnixNano()
	rows, err := s.db.QueryContext(ctx, `SELECT user_id,plex_id FROM plexwatch_account WHERE (direction & 2)<>0 AND revoked=0 AND token<>'' AND next_import<=? AND next_allowed<=? AND import_lease<=? ORDER BY next_import,user_id LIMIT 8`, now, now, now)
	if err != nil {
		return
	}
	type viewer struct{ user, plex int64 }
	var viewers []viewer
	for rows.Next() {
		var v viewer
		if rows.Scan(&v.user, &v.plex) == nil {
			viewers = append(viewers, v)
		}
	}
	rows.Close()
	for _, v := range viewers {
		if ctx.Err() != nil {
			return
		}
		_, _ = s.ImportCurrent(ctx, v.user, v.plex)
	}
}

// ImportCurrent uses the same cursor and persisted status for manual and scheduled imports.
func (s *Service) ImportCurrent(ctx context.Context, userID, plexID int64) (Status, error) {
	acct, err := s.requireMatch(ctx, userID, plexID)
	if err != nil {
		return Status{}, err
	}
	now := s.now()
	lease := now.Add(15 * time.Minute).UnixNano()
	res, err := s.db.ExecContext(ctx, `UPDATE plexwatch_account SET import_lease=? WHERE user_id=? AND plex_id=? AND token_generation=? AND import_lease<=? AND next_allowed<=?`, lease, userID, plexID, acct.tokenGeneration, now.UnixNano(), now.UnixNano())
	if err != nil {
		return Status{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Status{}, err
	}
	if n == 0 {
		return s.Status(ctx, userID, plexID)
	}
	importCtx, cancelImport := context.WithTimeout(ctx, 10*time.Minute)
	defer cancelImport()
	err = s.refreshEligibility(importCtx, userID, acct)
	if err == nil {
		err = s.importCurrent(importCtx, userID, plexID, acct.tokenGeneration)
	}
	if importCtx.Err() != nil { // use a short independent context to release the lease after leadership loss
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = s.db.ExecContext(cleanup, `UPDATE plexwatch_account SET import_lease=0 WHERE user_id=? AND import_lease=?`, userID, lease)
		return Status{}, importCtx.Err()
	}
	now = s.now()
	var interval, failures int
	e := s.db.QueryRowContext(ctx, `SELECT import_interval,import_failures FROM plexwatch_account WHERE user_id=? AND token_generation=? AND import_lease=?`, userID, acct.tokenGeneration, lease).Scan(&interval, &failures)
	if e != nil {
		return Status{}, e
	}
	code := 0
	problem := ""
	nextAllowed := int64(0)
	last := int64(0)
	delay := time.Duration(interval) * time.Minute
	if err != nil {
		failures++
		problem = "network"
		switch {
		case errors.Is(err, ErrNotEligible):
			problem = "consent"
		case errors.Is(err, ErrRevoked):
			problem = "token"
		case errors.Is(err, ErrCredentialForbidden):
			problem = "credential"
		}
		delay = time.Minute * time.Duration(1<<min(failures-1, 8))
		var status *StatusError
		if errors.As(err, &status) {
			code = status.PlexCode
		}
		if status != nil && status.Status == http.StatusTooManyRequests && status.RetryAfter > delay {
			delay = status.RetryAfter
		}
		maxDelay := time.Unix(0, math.MaxInt64).Sub(now) - time.Minute
		if maxDelay < 0 {
			maxDelay = 0
		}
		if delay > maxDelay {
			delay = maxDelay
		}
		nextAllowed = now.Add(delay).UnixNano()
	} else {
		failures = 0
		last = now.Unix()
	}
	// Stable per-viewer jitter spreads work without changing the configured interval.
	jitter := time.Duration(userID%61) * time.Second
	next := now.Add(delay + jitter).UnixNano()
	_, e = s.db.ExecContext(ctx, `UPDATE plexwatch_account SET import_lease=0,import_problem=?,problem_code=?,import_failures=?,next_allowed=?,next_import=?,last_import=CASE WHEN ?>0 THEN ? ELSE last_import END WHERE user_id=? AND token_generation=? AND import_lease=?`, problem, code, failures, nextAllowed, next, last, last, userID, acct.tokenGeneration, lease)
	if e != nil {
		return Status{}, e
	}
	st, statusErr := s.Status(ctx, userID, plexID)
	if statusErr != nil {
		return Status{}, statusErr
	}
	return st, err
}

// Unmatched returns one bounded page of unmatched Plex GUIDs.
func (s *Service) Unmatched(ctx context.Context, userID, plexID int64, after string) ([]string, error) {
	if _, err := s.requireMatch(ctx, userID, plexID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT guid FROM plexwatch_unmapped WHERE user_id=? AND guid>? ORDER BY guid LIMIT 100`, userID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0, 100)
	for rows.Next() {
		var guid string
		if err = rows.Scan(&guid); err != nil {
			return nil, err
		}
		out = append(out, guid)
	}
	return out, rows.Err()
}
