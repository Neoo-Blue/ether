package catalog

import "github.com/braedonsaunders/ether/internal/store"

func init() {
	store.Register("catalog", 21, `CREATE INDEX catalog_titles_episode_recent ON catalog_titles(released_ts DESC, key, show_key) WHERE type='episode' AND release_known=1;`)
}
