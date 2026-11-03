package catalog

import (
	"context"
	"testing"

	"github.com/braedonsaunders/ether/internal/core"
)

// History and catalogue form one ordered sequence even at a page boundary.
func TestBrowseHistoryBoundary(t *testing.T) {
	c := newChunkTestCatalog(t)
	ctx := context.Background()
	for i := int64(1); i <= 5; i++ {
		key := int64(core.MovieKey(i))
		if _, err := c.db.Exec(`INSERT INTO catalog_titles(key,type,title,sort_title,released_ts,browsable,adult) VALUES(?,'movie','x',?,1600000000,1,0)`, key, string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
		if i <= 2 {
			if _, err := c.db.Exec(`INSERT INTO state_watch(user_id,rating_key,last_viewed_at) VALUES(1,?,?)`, key, i*100); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, tc := range []struct {
		desc bool
		want []int64
	}{{false, []int64{1, 2, 3, 4, 5}}, {true, []int64{2, 1, 3, 4, 5}}} {
		for offset := 0; offset < len(tc.want); offset++ {
			q := core.BrowseQuery{Filter: core.Filter{Type: core.TypeMovie}, SortLastViewed: true, SortViewerID: 1, Desc: tc.desc, Offset: offset, Limit: 2}
			page, err := c.Browse(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			want := tc.want[offset:min(offset+2, len(tc.want))]
			if page.Total != 5 || len(page.Items) != len(want) {
				t.Fatalf("desc=%t offset=%d total=%d items=%d", tc.desc, offset, page.Total, len(page.Items))
			}
			for i, id := range want {
				if page.Items[i].Key != core.MovieKey(id) {
					t.Fatalf("desc=%t offset=%d item=%d: got %v", tc.desc, offset, i, page.Items[i].Key)
				}
			}
		}
	}
}
