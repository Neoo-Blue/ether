package safety

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/braedonsaunders/ether/internal/contentrating"
	"github.com/braedonsaunders/ether/internal/store"
)

// Safety policies used to carry their own stream limit and content rating
// limit, merged with the user record's (the stricter won). Both now live
// only on the user record. safety/2 copies the stricter of the two onto
// auth_users once, so no viewer gains access, and drops the two keys from the
// stored policies.
func init() {
	store.RegisterAfter("safety", "auth")
	store.Register("safety", 2, "SELECT 1;")
	store.RegisterApply("safety", 2, carryLimitsToUsers)
}

type legacyLimits struct {
	StreamLimit      int    `json:"streamLimit"`
	MaxContentRating string `json:"maxContentRating"`
}

func carryLimitsToUsers(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT user_id, policy FROM safety_policies ORDER BY user_id`)
	if err != nil {
		return err
	}
	type stored struct {
		id  int64
		raw string
	}
	var all []stored
	for rows.Next() {
		var r stored
		if err := rows.Scan(&r.id, &r.raw); err != nil {
			rows.Close()
			return err
		}
		all = append(all, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range all {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(r.raw), &fields); err != nil {
			return fmt.Errorf("safety policy %d: %w", r.id, err)
		}
		_, hasStreams := fields["streamLimit"]
		_, hasRating := fields["maxContentRating"]
		if !hasStreams && !hasRating {
			continue
		}
		var old legacyLimits
		if err := json.Unmarshal([]byte(r.raw), &old); err != nil {
			return fmt.Errorf("safety policy %d: %w", r.id, err)
		}
		var streams int
		var rating string
		err := tx.QueryRowContext(ctx, `SELECT stream_limit, max_content_rating FROM auth_users WHERE id=?`, r.id).Scan(&streams, &rating)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// A policy without a user row restricts nobody; just drop the keys.
		case err != nil:
			return err
		default:
			nextStreams := StricterStreamLimit(streams, old.StreamLimit)
			nextRating := StricterRating(rating, old.MaxContentRating)
			if nextStreams != streams || nextRating != rating {
				if _, err := tx.ExecContext(ctx, `UPDATE auth_users SET stream_limit=?, max_content_rating=? WHERE id=?`, nextStreams, nextRating, r.id); err != nil {
					return err
				}
			}
		}
		delete(fields, "streamLimit")
		delete(fields, "maxContentRating")
		next, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE safety_policies SET policy=? WHERE user_id=?`, string(next), r.id); err != nil {
			return err
		}
	}
	return nil
}

// StricterStreamLimit is the tighter of two per-viewer stream limits, where
// 0 adds no limit of its own: the smaller non-zero value.
func StricterStreamLimit(a, b int) int {
	switch {
	case a <= 0:
		return max(b, 0)
	case b <= 0:
		return a
	default:
		return min(a, b)
	}
}

// StricterRating is the more restrictive of two content rating limits, where
// "" is unrestricted. It orders labels by contentrating level, as the old
// account-and-policy merge did.
func StricterRating(account, policy string) string {
	policy = strings.TrimSpace(policy)
	if policy == "" {
		return account
	}
	if strings.TrimSpace(account) == "" || contentrating.LevelOf(policy) < contentrating.LevelOf(account) {
		return policy
	}
	return account
}
