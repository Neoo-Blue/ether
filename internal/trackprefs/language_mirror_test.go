package trackprefs

import (
	"context"
	"reflect"
	"testing"

	"github.com/braedonsaunders/ether/internal/store"
)

// The viewer's Audio & subtitles lists are mirrored onto the user record for
// code that reads languages from it: both lists, on save and on reset.
func TestRulesMirrorBothLanguageLists(t *testing.T) {
	ctx := context.Background()
	db, err := store.OpenMemory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	type mirrored struct {
		user             int64
		audio, subtitles []string
	}
	var got []mirrored
	s := New(db, Options{SyncLanguages: func(_ context.Context, user int64, audio, subtitles []string) error {
		got = append(got, mirrored{user, audio, subtitles})
		return nil
	}})
	r := Default()
	r.AudioLanguages = []string{"ja", "en"}
	r.SubtitleLanguages = []string{"en"}
	if err := s.PutRules(ctx, 4, r); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRules(ctx, 0, r); err != nil { // shared default: no account row
		t.Fatal(err)
	}
	if err := s.DeleteRules(ctx, 4); err != nil {
		t.Fatal(err)
	}
	want := []mirrored{{4, []string{"ja", "en"}, []string{"en"}}, {4, []string{}, []string{}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mirrored %+v, want %+v", got, want)
	}
}
