package safety

import (
	"context"
	"encoding/json"
	"testing"

	_ "github.com/braedonsaunders/ether/internal/auth" // auth_users schema
	"github.com/braedonsaunders/ether/internal/store"
)

// safety/2 moves the old policy stream and rating limits onto the user
// record, keeping the stricter value so no viewer gains access.
func TestLimitsMigrationKeepsTheStricterValue(t *testing.T) {
	ctx := context.Background()
	db := store.OpenTest(t)
	type user struct {
		id      int64
		streams int
		rating  string
		policy  string // "" means no policy row
	}
	cases := []struct {
		user
		wantStreams int
		wantRating  string
	}{
		// Streams: 0 adds no limit, otherwise the smaller wins.
		{user{1, 0, "", `{"streamLimit":3}`}, 3, ""},
		{user{2, 4, "", `{"streamLimit":0}`}, 4, ""},
		{user{3, 4, "", `{"streamLimit":2}`}, 2, ""},
		{user{4, 1, "", `{"streamLimit":5}`}, 1, ""},
		{user{5, 0, "", `{"streamLimit":0}`}, 0, ""},
		// Ratings: "" is unrestricted, otherwise the lower level wins.
		{user{6, 0, "", `{"maxContentRating":"PG"}`}, 0, "PG"},
		{user{7, 0, "TV-14", `{"maxContentRating":"TV-PG"}`}, 0, "TV-PG"},
		{user{8, 0, "PG", `{"maxContentRating":"gb/12A"}`}, 0, "PG"},
		{user{9, 0, "R", `{"maxContentRating":""}`}, 0, "R"},
		{user{10, 0, "TV-14", `{"maxContentRating":"PG-13"}`}, 0, "TV-14"}, // same level keeps the account value
		// Both at once, plus unrelated policy fields that must survive.
		{user{11, 6, "R", `{"streamLimit":2,"maxContentRating":"TV-Y","remoteDisabled":true,"blockedGenres":["Horror"]}`}, 2, "TV-Y"},
		// No policy row: untouched.
		{user{12, 3, "PG", ""}, 3, "PG"},
	}
	for _, c := range cases {
		if _, err := db.ExecContext(ctx, `INSERT INTO auth_users(id,plex_id,username,stream_limit,max_content_rating,created_at,updated_at) VALUES(?,?,?,?,?,1,1)`,
			c.id, 1000+c.id, "viewer", c.streams, c.rating); err != nil {
			t.Fatal(err)
		}
		if c.policy != "" {
			if _, err := db.ExecContext(ctx, `INSERT INTO safety_policies(user_id,policy) VALUES(?,?)`, c.id, c.policy); err != nil {
				t.Fatal(err)
			}
		}
	}
	// A policy whose user row is gone still loses the legacy keys.
	if _, err := db.ExecContext(ctx, `INSERT INTO safety_policies(user_id,policy) VALUES(99,'{"streamLimit":1,"maxBitrateKbps":800}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE component='safety' AND version=2`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		var streams int
		var rating string
		if err := db.QueryRowContext(ctx, `SELECT stream_limit, max_content_rating FROM auth_users WHERE id=?`, c.id).Scan(&streams, &rating); err != nil {
			t.Fatal(err)
		}
		if streams != c.wantStreams || rating != c.wantRating {
			t.Errorf("viewer %d: streams=%d rating=%q, want %d %q", c.id, streams, rating, c.wantStreams, c.wantRating)
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT user_id, policy FROM safety_policies`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal([]byte(raw), &fields); err != nil {
			t.Fatal(err)
		}
		if _, ok := fields["streamLimit"]; ok {
			t.Errorf("viewer %d policy kept streamLimit: %s", id, raw)
		}
		if _, ok := fields["maxContentRating"]; ok {
			t.Errorf("viewer %d policy kept maxContentRating: %s", id, raw)
		}
		switch id {
		case 11:
			if fields["remoteDisabled"] != true || len(fields["blockedGenres"].([]any)) != 1 {
				t.Errorf("viewer 11 lost policy fields: %s", raw)
			}
		case 99:
			if fields["maxBitrateKbps"] != float64(800) {
				t.Errorf("orphan policy lost fields: %s", raw)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	// The migrated policies load and validate.
	s, err := New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if p := s.Get(11); !p.RemoteDisabled || len(p.BlockedGenres) != 1 {
		t.Fatalf("policy 11 %+v", p)
	}
}

func TestStricterLimits(t *testing.T) {
	for _, c := range []struct{ a, b, want int }{{0, 0, 0}, {0, 2, 2}, {2, 0, 2}, {3, 2, 2}, {2, 3, 2}, {-1, 0, 0}} {
		if got := StricterStreamLimit(c.a, c.b); got != c.want {
			t.Errorf("StricterStreamLimit(%d,%d)=%d want %d", c.a, c.b, got, c.want)
		}
	}
	for _, c := range []struct{ account, policy, want string }{
		{"", "", ""}, {"", "PG", "PG"}, {"PG", "", "PG"}, {"R", "PG-13", "PG-13"}, {"PG", "TV-14", "PG"}, {"TV-MA", "de/16", "TV-MA"},
	} {
		if got := StricterRating(c.account, c.policy); got != c.want {
			t.Errorf("StricterRating(%q,%q)=%q want %q", c.account, c.policy, got, c.want)
		}
	}
}
