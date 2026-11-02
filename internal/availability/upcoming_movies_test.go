package availability

import (
	"github.com/braedonsaunders/ether/internal/core"
	"testing"
	"time"
)

func TestUpcomingMovieDiscoveryOptIn(t *testing.T) {
	x := setup(t)
	title := x.cat.titles[core.MovieKey(1)]
	input := x.s.discoverySnapshot()
	title.Released = input.now.Add(72 * time.Hour)
	for _, enabled := range []bool{false, true, false} {
		input.showUpcomingMovies = enabled
		for _, cached := range []bool{false, true} {
			var got []core.Title
			if cached {
				got = x.s.filterDiscoverySnapshot(t.Context(), []core.Title{title}, input)
			} else {
				got = x.s.filterDiscovery(t.Context(), []core.Title{title}, input)
			}
			want := 0
			if enabled {
				want = 1
			}
			if len(got) != want {
				t.Fatalf("enabled=%v cached=%v: %d", enabled, cached, len(got))
			}
		}
	}
}
