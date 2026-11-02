package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/store"
)

// Viewer lifecycle refusals. Plex viewers are managed on plex.tv, full
// stop: Ether never pauses, removes or merges them. The owner can never
// be paused, removed or merged either.
var (
	ErrViewerIsOwner     = errors.New("auth: viewer is the owner")
	ErrViewerPlexManaged = errors.New("auth: viewer is managed on plex.tv")
	ErrMergeSameViewer   = errors.New("auth: cannot merge a viewer into itself")
)

// plexManaged reports whether r is a Plex viewer managed on plex.tv.
func (r *row) plexManaged() bool { return !r.IsOwner && r.HasPlex }

type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func loadRow(ctx context.Context, q rowQuerier, id int64) (*row, error) {
	r, err := scanRow(q.QueryRowContext(ctx, `SELECT `+userCols+` FROM auth_users WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, core.ErrNotFound
	}
	return r, err
}

func checkLifecycleAllowed(r *row) error {
	if r.IsOwner {
		return ErrViewerIsOwner
	}
	if r.plexManaged() {
		return ErrViewerPlexManaged
	}
	return nil
}

// PauseViewer pauses a viewer's access: everything of theirs is kept,
// but they can no longer sign in or watch. The caller signs them out
// everywhere as part of pausing (admin does, via identity grants).
func (s *Service) PauseViewer(ctx context.Context, viewerID int64) error {
	r, err := loadRow(ctx, s.db, viewerID)
	if err != nil {
		return err
	}
	if err := checkLifecycleAllowed(r); err != nil {
		return err
	}
	return s.SetOwnerEnabled(ctx, viewerID, false)
}

// ResumeViewer ends a pause.
func (s *Service) ResumeViewer(ctx context.Context, viewerID int64) error {
	r, err := loadRow(ctx, s.db, viewerID)
	if err != nil {
		return err
	}
	if err := checkLifecycleAllowed(r); err != nil {
		return err
	}
	return s.SetOwnerEnabled(ctx, viewerID, true)
}

// RevokeViewerSessions deletes the viewer's console sessions and
// delegated credentials, returning how many were revoked. Device grants
// and app passwords live in identity and are revoked there; together
// they make "sign out everywhere".
func (s *Service) RevokeViewerSessions(ctx context.Context, viewerID int64) (int, error) {
	var total int64
	for _, q := range []string{
		`DELETE FROM auth_sessions WHERE user_id=?`,
		`DELETE FROM auth_delegations WHERE user_id=?`,
	} {
		res, err := s.db.ExecContext(ctx, q, viewerID)
		if err != nil {
			return int(total), err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return int(total), err
		}
		total += n
	}
	return int(total), nil
}

// RemoveViewer deletes a viewer and all of their data in one transaction:
// every registered per-viewer purge (store.PurgeViewer), then the
// auth_users row itself, whose ON DELETE CASCADE children are a backstop.
func (s *Service) RemoveViewer(ctx context.Context, viewerID int64) error {
	s.membershipMu.Lock()
	defer s.membershipMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := loadRow(ctx, tx, viewerID)
	if err != nil {
		return err
	}
	if err := checkLifecycleAllowed(r); err != nil {
		return err
	}
	if err := store.PurgeViewer(ctx, tx, viewerID); err != nil {
		return err
	}
	if err := deleteViewerRow(ctx, tx, viewerID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.forgetViewer(viewerID)
	if err := s.reload(ctx); err != nil {
		return err
	}
	s.gen.Add(1)
	s.notify("")
	return nil
}

// MergeViewers moves everything from the source viewer into the target
// (store.MergeViewer: the target wins conflicts, watch history and plays
// union), then removes the source, in one transaction. Neither side may
// be the owner or a Plex viewer.
func (s *Service) MergeViewers(ctx context.Context, fromID, intoID int64) error {
	if fromID == intoID {
		return ErrMergeSameViewer
	}
	s.membershipMu.Lock()
	defer s.membershipMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	from, err := loadRow(ctx, tx, fromID)
	if err != nil {
		return err
	}
	if err := checkLifecycleAllowed(from); err != nil {
		return err
	}
	into, err := loadRow(ctx, tx, intoID)
	if err != nil {
		return err
	}
	if err := checkLifecycleAllowed(into); err != nil {
		return err
	}
	if err := store.MergeViewer(ctx, tx, fromID, intoID); err != nil {
		return err
	}
	// Anything a merge chose not to move is the source's to lose: purge
	// the remainder so removing the source leaves nothing behind.
	if err := store.PurgeViewer(ctx, tx, fromID); err != nil {
		return err
	}
	if err := deleteViewerRow(ctx, tx, fromID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.forgetViewer(fromID)
	if err := s.reload(ctx); err != nil {
		return err
	}
	s.gen.Add(1)
	s.notify("")
	return nil
}

func deleteViewerRow(ctx context.Context, tx *sql.Tx, viewerID int64) error {
	if _, err := tx.ExecContext(ctx, `UPDATE auth_membership_epoch SET epoch=epoch+1 WHERE id=1`); err != nil {
		return fmt.Errorf("auth: bump membership epoch: %w", err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM auth_users WHERE id=? AND is_owner=0`, viewerID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return core.ErrNotFound
	}
	return nil
}

// forgetViewer drops in-memory traces of a removed viewer. reload
// rebuilds the user and token maps and retires cached validations; the
// pending last-seen write must not resurrect a row reference either.
func (s *Service) forgetViewer(viewerID int64) {
	s.seenMu.Lock()
	delete(s.seen, viewerID)
	s.seenMu.Unlock()
}
