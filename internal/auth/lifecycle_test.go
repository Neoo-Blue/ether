package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/plextv"
	"github.com/braedonsaunders/ether/internal/store"
)

func TestViewerLifecycleRefusalsAndRemoval(t *testing.T) {
	ctx := context.Background()
	db := store.OpenTest(t)
	s, err := New(ctx, db, Options{})
	if err != nil {
		t.Fatal(err)
	}
	mk := func(username, q string) int64 {
		t.Helper()
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
		var id int64
		if err := db.QueryRowContext(ctx, `SELECT id FROM auth_users WHERE username=?`, username).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	owner := mk("owner", `INSERT INTO auth_users(username,title,is_owner,active,origin,created_at,updated_at) VALUES('owner','Owner',1,1,'local',1,1)`)
	local := mk("bob", `INSERT INTO auth_users(username,title,is_owner,active,origin,created_at,updated_at) VALUES('bob','Bob',0,1,'local',1,1)`)
	other := mk("amy", `INSERT INTO auth_users(username,title,is_owner,active,origin,created_at,updated_at) VALUES('amy','Amy',0,1,'local',1,1)`)
	if err := s.SyncFriends(ctx, []plextv.Friend{
		{PlexID: 42, Username: "friend", Token: "tok-friend"},
		{PlexID: 43, Username: "homekid", Token: "tok-home", Home: true},
	}); err != nil {
		t.Fatal(err)
	}
	var plex, plexHome int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM auth_users WHERE plex_id=42`).Scan(&plex); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT id FROM auth_users WHERE plex_id=43`).Scan(&plexHome); err != nil {
		t.Fatal(err)
	}
	if err := s.RefreshViewers(ctx); err != nil {
		t.Fatal(err)
	}

	// The owner and any Plex viewer refuse pause, resume, remove, merge.
	for _, tc := range []struct {
		name string
		id   int64
		want error
	}{
		{"owner", owner, ErrViewerIsOwner},
		{"plex", plex, ErrViewerPlexManaged},
		{"plexHome", plexHome, ErrViewerPlexManaged},
	} {
		if err := s.PauseViewer(ctx, tc.id); !errors.Is(err, tc.want) {
			t.Fatalf("pause %s: %v, want %v", tc.name, err, tc.want)
		}
		if err := s.ResumeViewer(ctx, tc.id); !errors.Is(err, tc.want) {
			t.Fatalf("resume %s: %v, want %v", tc.name, err, tc.want)
		}
		if err := s.RemoveViewer(ctx, tc.id); !errors.Is(err, tc.want) {
			t.Fatalf("remove %s: %v, want %v", tc.name, err, tc.want)
		}
		if err := s.MergeViewers(ctx, tc.id, local); !errors.Is(err, tc.want) {
			t.Fatalf("merge from %s: %v, want %v", tc.name, err, tc.want)
		}
		if err := s.MergeViewers(ctx, local, tc.id); !errors.Is(err, tc.want) {
			t.Fatalf("merge into %s: %v, want %v", tc.name, err, tc.want)
		}
	}
	if err := s.MergeViewers(ctx, plex, plexHome); !errors.Is(err, ErrViewerPlexManaged) {
		t.Fatalf("merge plex into plexHome: %v", err)
	}
	if err := s.MergeViewers(ctx, local, local); !errors.Is(err, ErrMergeSameViewer) {
		t.Fatalf("merge self: %v", err)
	}
	if err := s.RemoveViewer(ctx, 99999); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("remove missing: %v", err)
	}
	if err := s.PauseViewer(ctx, 99999); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("pause missing: %v", err)
	}

	// Pause flips owner_enabled and blocks the user; resume restores it.
	if err := s.PauseViewer(ctx, local); err != nil {
		t.Fatal(err)
	}
	infos, err := s.AllUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]UserInfo{}
	for _, info := range infos {
		byID[info.ID] = info
	}
	if byID[local].OwnerEnabled || byID[local].Active {
		t.Fatalf("paused viewer still enabled: %+v", byID[local])
	}
	if !byID[plex].HasPlex || !byID[plex].PlexShared || byID[plexHome].HasPlex != true {
		t.Fatalf("plex flags wrong: %+v %+v", byID[plex], byID[plexHome])
	}
	if err := s.ResumeViewer(ctx, local); err != nil {
		t.Fatal(err)
	}

	// Removal deletes the row and the viewer's identity links and tokens.
	if _, err := db.ExecContext(ctx, `INSERT INTO identity_links(viewer_id,provider,subject,display,verified_at,password_hash) VALUES(?,'local','bob','bob',1,'hash')`, local); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_sessions(token_hash,user_id,created_at,expires_at) VALUES('sess',?,1,9999999999)`, local); err != nil {
		t.Fatal(err)
	}
	n, err := s.RevokeViewerSessions(ctx, local)
	if err != nil || n != 1 {
		t.Fatalf("revoke sessions n=%d err=%v", n, err)
	}
	if err := s.RemoveViewer(ctx, local); err != nil {
		t.Fatal(err)
	}
	var count int
	for _, q := range []string{
		`SELECT COUNT(*) FROM auth_users WHERE id=?`,
		`SELECT COUNT(*) FROM identity_links WHERE viewer_id=?`,
	} {
		if err := db.QueryRowContext(ctx, q, local).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count=%d err=%v", q, count, err)
		}
	}
	if _, err := s.User(ctx, local); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("removed viewer still in cache: %v", err)
	}

	// Merge of two local viewers works at this level too.
	if _, err := db.ExecContext(ctx, `INSERT INTO identity_links(viewer_id,provider,subject,display,verified_at,password_hash) VALUES(?,'local','amy','amy',1,'hash')`, other); err != nil {
		t.Fatal(err)
	}
	third := mk("cat", `INSERT INTO auth_users(username,title,is_owner,active,origin,created_at,updated_at) VALUES('cat','Cat',0,1,'local',1,1)`)
	if err := s.RefreshViewers(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.MergeViewers(ctx, other, third); err != nil {
		t.Fatal(err)
	}
	var linkViewer int64
	if err := db.QueryRowContext(ctx, `SELECT viewer_id FROM identity_links WHERE subject='amy'`).Scan(&linkViewer); err != nil || linkViewer != third {
		t.Fatalf("merged link viewer=%d err=%v, want %d", linkViewer, err, third)
	}
	if _, err := s.User(ctx, other); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("merged-away viewer still present: %v", err)
	}
}
