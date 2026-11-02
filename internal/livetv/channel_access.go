package livetv

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/store"
)

// Channel access has one source: each viewer's Policy. The owner edits it on
// the viewer's page (their allowed channels) and on each channel (which
// viewers may watch it); both write the same policies.

// Permits reports whether the policy lets its viewer watch channel id. Rating
// and library limits are checked separately.
func (p Policy) Permits(id string) bool {
	if p.Disabled {
		return false
	}
	if !p.Restricted {
		return true
	}
	if p.channelSet != nil {
		_, ok := p.channelSet[id]
		return ok
	}
	return slices.Contains(p.Channels, id)
}

// normalize drops duplicate channels and the list of an unrestricted viewer,
// who sees every channel.
func (p *Policy) normalize() {
	if !p.Restricted {
		p.Channels = nil
		return
	}
	seen := make(map[string]struct{}, len(p.Channels))
	out := p.Channels[:0]
	for _, id := range p.Channels {
		if _, dup := seen[id]; id == "" || dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	p.Channels = out
}

// indexChannels makes Permits constant time for long allow-lists: a guide
// checks every channel against the same policy.
func (p *Policy) indexChannels() {
	if len(p.Channels) < 16 {
		return
	}
	p.channelSet = make(map[string]struct{}, len(p.Channels))
	for _, id := range p.Channels {
		p.channelSet[id] = struct{}{}
	}
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func savePolicy(ctx context.Context, db execer, p Policy) error {
	p.normalize()
	p.channelSet = nil
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO livetv_policies(user_id,data) VALUES(?,?) ON CONFLICT(user_id) DO UPDATE SET data=excluded.data`, p.UserID, b)
	return err
}

func readPolicy(ctx context.Context, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, userID int64) (Policy, error) {
	p := Policy{UserID: userID}
	var b []byte
	err := db.QueryRowContext(ctx, `SELECT data FROM livetv_policies WHERE user_id=?`, userID).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err == nil {
		err = json.Unmarshal(b, &p)
		p.UserID = userID
	}
	return p, err
}

// Policies lists every stored viewer policy. Viewers without one see every
// channel.
func (s *Service) Policies(ctx context.Context) ([]Policy, error) {
	rows, err := s.o.DB.QueryContext(ctx, `SELECT user_id, data FROM livetv_policies ORDER BY user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Policy{}
	for rows.Next() {
		var id int64
		var b []byte
		if err := rows.Scan(&id, &b); err != nil {
			return nil, err
		}
		var p Policy
		if err := json.Unmarshal(b, &p); err != nil {
			return nil, err
		}
		p.UserID = id
		out = append(out, p)
	}
	return out, rows.Err()
}

// ChannelViewers changes which viewers may watch one channel by editing their
// policies. Allowing a restricted viewer adds the channel to their list.
// Denying an unrestricted viewer restricts them to every other current
// channel, so they stop receiving new channels automatically. A viewer with
// Live TV disabled keeps it disabled.
type ChannelViewers struct {
	Allow []int64 `json:"allow,omitempty"`
	Deny  []int64 `json:"deny,omitempty"`
}

func (s *Service) SetChannelViewers(ctx context.Context, channelID string, change ChannelViewers) error {
	for _, id := range append(slices.Clone(change.Allow), change.Deny...) {
		if id <= 0 {
			return fmt.Errorf("viewer ids must be positive")
		}
	}
	if slices.ContainsFunc(change.Allow, func(id int64) bool { return slices.Contains(change.Deny, id) }) {
		return fmt.Errorf("a viewer cannot be both allowed and denied")
	}
	tx, err := s.o.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := store.LockKey(ctx, store.DialectOf(s.o.DB), tx, "livetv_policies"); err != nil {
		return err
	}
	ids, err := channelIDs(ctx, tx)
	if err != nil {
		return err
	}
	if !slices.Contains(ids, channelID) {
		return core.ErrNotFound
	}
	for _, id := range change.Allow {
		p, err := readPolicy(ctx, tx, id)
		if err != nil {
			return err
		}
		if !p.Restricted || slices.Contains(p.Channels, channelID) {
			continue
		}
		p.Channels = append(p.Channels, channelID)
		if err := savePolicy(ctx, tx, p); err != nil {
			return err
		}
	}
	for _, id := range change.Deny {
		p, err := readPolicy(ctx, tx, id)
		if err != nil {
			return err
		}
		if !p.Restricted {
			p.Restricted = true
			p.Channels = slices.DeleteFunc(slices.Clone(ids), func(c string) bool { return c == channelID })
		} else if slices.Contains(p.Channels, channelID) {
			p.Channels = slices.DeleteFunc(p.Channels, func(c string) bool { return c == channelID })
		} else {
			continue
		}
		if err := savePolicy(ctx, tx, p); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.bumpLineup()
	return nil
}

func channelIDs(ctx context.Context, db queryer) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT id FROM livetv_channels ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
