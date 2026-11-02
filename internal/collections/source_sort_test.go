package collections

import (
	"testing"

	"github.com/braedonsaunders/ether/internal/core"
)

// Source order keeps the list's own order even when the spec carries the
// editor's default "descending" direction (#264).
func TestSourceOrderIgnoresDirection(t *testing.T) {
	for _, desc := range []bool{false, true} {
		cs := []*cand{
			{e: &entry{key: 7, pop: 1}, order: 0},
			{e: &entry{key: 3, pop: 9}, order: 1},
			{e: &entry{key: 5, pop: 5}, order: 2},
		}
		sortCands(cs, "source", desc, 0)
		for i, want := range []core.RatingKey{7, 3, 5} {
			if cs[i].e.key != want {
				t.Fatalf("desc=%v: position %d got %d want %d", desc, i, cs[i].e.key, want)
			}
		}
	}
}
