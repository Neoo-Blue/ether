package livetv

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/store"
)

func saveAccessChannels(t *testing.T, s *Service, ids ...string) {
	t.Helper()
	for i, id := range ids {
		c := testChannel()
		c.ID, c.Number, c.Name = id, string(rune('1'+i)), "Channel "+id
		if _, err := s.SaveChannel(context.Background(), c); err != nil {
			t.Fatal(err)
		}
	}
}

func visibleChannels(t *testing.T, s *Service, u *core.User) []string {
	t.Helper()
	cs, err := s.AllowedChannels(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, c := range cs {
		out = append(out, c.ID)
	}
	slices.Sort(out)
	return out
}

func TestChannelViewersEditViewerPolicies(t *testing.T) {
	ctx := context.Background()
	s := testService(t, Options{})
	saveAccessChannels(t, s, "a", "b", "c")
	viewer := &core.User{ID: 7}
	if got := visibleChannels(t, s, viewer); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("default access = %v", got)
	}

	// Denying a viewer on "all channels" restricts them to every other channel.
	if err := s.SetChannelViewers(ctx, "b", ChannelViewers{Deny: []int64{7}}); err != nil {
		t.Fatal(err)
	}
	p, _ := s.Policy(ctx, 7)
	if !p.Restricted || !slices.Equal(p.Channels, []string{"a", "c"}) {
		t.Fatalf("deny from all: %+v", p)
	}
	// Restricted viewers no longer receive new channels; others do.
	saveAccessChannels(t, s, "a", "b", "c", "d")
	if got := visibleChannels(t, s, viewer); !slices.Equal(got, []string{"a", "c"}) {
		t.Fatalf("restricted viewer saw a new channel: %v", got)
	}
	if got := visibleChannels(t, s, &core.User{ID: 8}); !slices.Equal(got, []string{"a", "b", "c", "d"}) {
		t.Fatalf("unrestricted viewer missed the new channel: %v", got)
	}

	if err := s.SetChannelViewers(ctx, "d", ChannelViewers{Allow: []int64{7, 8}}); err != nil {
		t.Fatal(err)
	}
	if got := visibleChannels(t, s, viewer); !slices.Equal(got, []string{"a", "c", "d"}) {
		t.Fatalf("allow restricted: %v", got)
	}
	if p, _ := s.Policy(ctx, 8); p.Restricted || len(p.Channels) != 0 {
		t.Fatalf("allowing an unrestricted viewer changed them: %+v", p)
	}

	// Removing the last channel leaves no channels, never "all".
	for _, id := range []string{"a", "c", "d"} {
		if err := s.SetChannelViewers(ctx, id, ChannelViewers{Deny: []int64{7}}); err != nil {
			t.Fatal(err)
		}
	}
	if got := visibleChannels(t, s, viewer); len(got) != 0 {
		t.Fatalf("empty allow-list widened to %v", got)
	}

	// Disabled viewers stay disabled when a channel allows them.
	if err := s.SavePolicy(ctx, Policy{UserID: 9, Disabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChannelViewers(ctx, "a", ChannelViewers{Allow: []int64{9}}); err != nil {
		t.Fatal(err)
	}
	if got := visibleChannels(t, s, &core.User{ID: 9}); len(got) != 0 {
		t.Fatalf("disabled viewer saw %v", got)
	}

	if err := s.SetChannelViewers(ctx, "missing", ChannelViewers{Deny: []int64{7}}); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("unknown channel: %v", err)
	}
	if err := s.SetChannelViewers(ctx, "a", ChannelViewers{Allow: []int64{7}, Deny: []int64{7}}); err == nil {
		t.Fatal("allowed and denied the same viewer")
	}
	// Owners ignore the list.
	if got := visibleChannels(t, s, &core.User{ID: 7, IsOwner: true}); len(got) != 4 {
		t.Fatalf("owner access = %v", got)
	}
}

func TestPolicyAPIListsAndNormalizes(t *testing.T) {
	s := testService(t, Options{})
	saveAccessChannels(t, s, "a", "b")
	h := s.Handler()
	do := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec
	}
	if rec := do("PUT", "/policies/3", `{"restricted":false,"channels":["a"]}`); rec.Code != 200 || strings.Contains(rec.Body.String(), `"channels"`) {
		t.Fatalf("unrestricted policy kept a list: %d %s", rec.Code, rec.Body)
	}
	if rec := do("PUT", "/channels/a/viewers", `{"deny":[3,4]}`); rec.Code != 200 {
		t.Fatalf("channel viewers: %d %s", rec.Code, rec.Body)
	}
	if rec := do("PUT", "/channels/zz/viewers", `{"deny":[3]}`); rec.Code != 404 {
		t.Fatalf("unknown channel: %d", rec.Code)
	}
	rec := do("GET", "/policies", "")
	var list []Policy
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list) != 2 {
		t.Fatalf("policies: %s %v", rec.Body, err)
	}
	for _, p := range list {
		if !p.Restricted || !slices.Equal(p.Channels, []string{"b"}) {
			t.Fatalf("policy %+v", p)
		}
	}
	// The retired channel field is not part of the channel API.
	if rec := do("POST", "/channels", `{"id":"x","number":"9","name":"X","kind":"virtual","enabled":true,"allowedUsers":[3],"items":[{"key":1,"title":"T","durationMs":1000}]}`); rec.Code != 400 {
		t.Fatalf("allowedUsers accepted: %d %s", rec.Code, rec.Body)
	}
}

// TestMigrationCarriesChannelViewersIntoPolicies replays livetv/3 over data
// written before the per-channel viewer list was retired.
func TestMigrationCarriesChannelViewersIntoPolicies(t *testing.T) {
	ctx := context.Background()
	db := store.OpenTest(t)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	// This package's test binary does not register the auth component.
	exec(`CREATE TABLE IF NOT EXISTS auth_users(id INTEGER PRIMARY KEY, is_owner INTEGER NOT NULL DEFAULT 0)`)
	for _, u := range [][2]int64{{1, 1}, {2, 0}, {3, 0}, {4, 0}, {5, 0}, {6, 0}} {
		exec(`INSERT INTO auth_users(id, is_owner) VALUES(?, ?)`, u[0], u[1])
	}
	channel := func(id, allowed string) {
		data := `{"id":"` + id + `","number":"1","name":"` + id + `","kind":"virtual","enabled":true,"rating":"G","epoch":"1970-01-01T00:00:00Z","seed":0,"shuffle":false,"items":[{"key":1,"title":"T","durationMs":60000}]` + allowed + `}`
		exec(`INSERT INTO livetv_channels(id, data) VALUES(?, ?)`, id, data)
	}
	channel("open", "")
	channel("kids", `,"allowedUsers":[2,3]`)
	channel("news", `,"allowedUsers":[4]`)
	policy := func(id int64, data string) { exec(`INSERT INTO livetv_policies(user_id, data) VALUES(?, ?)`, id, data) }
	policy(3, `{"userId":3,"disabled":false,"channels":["kids","news","gone","kids"],"maxRating":"PG"}`)
	policy(5, `{"userId":5,"disabled":true}`)
	policy(1, `{"userId":1,"disabled":false}`)
	exec(`DELETE FROM schema_migrations WHERE component='livetv' AND version=3`)
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	s := testService(t, Options{DB: db})
	want := map[int64]struct {
		restricted bool
		channels   []string
		visible    []string
	}{
		// Viewer 2 had all channels but was left off news.
		2: {true, []string{"kids", "open"}, []string{"kids", "open"}},
		// Viewer 3 listed kids, news, a deleted channel and a duplicate;
		// news never let them in.
		3: {true, []string{"gone", "kids"}, []string{"kids"}},
		4: {true, []string{"news", "open"}, []string{"news", "open"}},
		// Disabled viewers keep the list they would return to.
		5: {true, []string{"open"}, nil},
		6: {true, []string{"open"}, []string{"open"}},
	}
	for id, w := range want {
		p, err := s.Policy(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		got := slices.Clone(p.Channels)
		slices.Sort(got)
		if p.Restricted != w.restricted || !slices.Equal(got, w.channels) {
			t.Fatalf("viewer %d policy = %+v, want restricted=%v %v", id, p, w.restricted, w.channels)
		}
		if got := visibleChannels(t, s, &core.User{ID: id}); !slices.Equal(got, w.visible) {
			t.Fatalf("viewer %d sees %v, want %v", id, got, w.visible)
		}
	}
	if p, _ := s.Policy(ctx, 3); p.MaxRating != "PG" {
		t.Fatalf("policy lost its other settings: %+v", p)
	}
	if p, _ := s.Policy(ctx, 1); p.Restricted {
		t.Fatalf("owner policy restricted: %+v", p)
	}
	var leftovers int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM livetv_channels WHERE `+store.Contains(store.DialectOf(db), "data", "'allowedUsers'")).Scan(&leftovers); err != nil || leftovers != 0 {
		t.Fatalf("channels still carry allowedUsers: %d %v", leftovers, err)
	}

	// No channel lists: viewers keep "all channels" and get no policy row.
	db2 := store.OpenTest(t)
	if _, err := db2.ExecContext(ctx, `INSERT INTO livetv_channels(id, data) VALUES('open', '{"id":"open","number":"1","name":"open","kind":"virtual","enabled":true,"epoch":"1970-01-01T00:00:00Z","seed":0,"shuffle":false,"items":[]}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db2.ExecContext(ctx, `INSERT INTO livetv_policies(user_id, data) VALUES(8, '{"userId":8,"disabled":false}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db2.ExecContext(ctx, `DELETE FROM schema_migrations WHERE component='livetv' AND version=3`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db2); err != nil {
		t.Fatal(err)
	}
	s2 := testService(t, Options{DB: db2})
	if p, _ := s2.Policy(ctx, 8); p.Restricted {
		t.Fatalf("unlimited viewer restricted: %+v", p)
	}
}
