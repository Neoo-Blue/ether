package catalog

import (
	"context"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/store"
)

// A search the literal title match cannot answer well is retried with
// spelling rewrites from the search lexicon (search_lexicon.go): misspelt
// words corrected, accents and inner punctuation restored ("amelie",
// "spiderman"), run-together words split ("starwars"), and, when nothing
// matched literally, the words matched in any order and across punctuation
// ("star wars episode iv"). Rewritten matches rank after literal ones, closest
// rewrite first. Ordinary searches, whose every word a title contains,
// never reach the database again.

const (
	// searchLexiconRefresh is the least time between rebuilds after the
	// catalogue changes; searchLexiconMaxAge bounds staleness from writes this
	// node cannot see (another cluster node's sync).
	searchLexiconRefresh = 10 * time.Minute
	searchLexiconMaxAge  = time.Hour
	// searchLexiconRetry spaces failed builds.
	searchLexiconRetry = time.Minute
	// searchLexiconFirstWait is how long a search waits for the first build.
	searchLexiconFirstWait    = 250 * time.Millisecond
	searchLexiconBuildTimeout = 5 * time.Minute

	// spellMaxRewrites bounds the rewritten queries one search runs.
	spellMaxRewrites = 6
	// spellNearPerWord bounds the corrections considered per query word.
	spellNearPerWord = 4
	// spellMaxWords bounds the query words a search tries to correct.
	spellMaxWords = 8
)

type searchSpell struct {
	lx       atomic.Pointer[searchLexicon]
	mu       sync.Mutex
	gen      uint64    // catalogue write generation the lexicon reflects
	at       time.Time // when the last build finished
	next     time.Time // earliest next build after a failure
	building chan struct{}
}

// WarmSearch starts building the search lexicon in the background so the
// first misspelt search does not wait for it.
func (c *Catalog) WarmSearch(ctx context.Context) {
	_ = c.searchLexicon(ctx, false)
}

// searchLexicon returns the current lexicon, starting a background rebuild
// when it is missing or stale. Only the very first build is waited for, and
// only briefly (wait); a stale lexicon is served while its successor builds.
func (c *Catalog) searchLexicon(ctx context.Context, wait bool) *searchLexicon {
	st := &c.spell
	lx := st.lx.Load()
	now := c.now()
	gen := c.writeGen.Load()
	st.mu.Lock()
	stale := lx == nil || (gen != st.gen && now.Sub(st.at) >= searchLexiconRefresh) || now.Sub(st.at) >= searchLexiconMaxAge
	if stale && st.building == nil && !now.Before(st.next) {
		done := make(chan struct{})
		st.building = done
		go c.rebuildSearchLexicon(gen, done)
	}
	building := st.building
	st.mu.Unlock()
	if lx != nil || building == nil || !wait {
		return lx
	}
	timer := time.NewTimer(searchLexiconFirstWait)
	defer timer.Stop()
	select {
	case <-building:
	case <-timer.C:
	case <-ctx.Done():
	}
	return st.lx.Load()
}

func (c *Catalog) rebuildSearchLexicon(gen uint64, done chan struct{}) {
	st := &c.spell
	defer close(done)
	ctx, cancel := context.WithTimeout(context.Background(), searchLexiconBuildTimeout)
	defer cancel()
	started := time.Now()
	lx, err := c.loadSearchLexicon(ctx)
	st.mu.Lock()
	defer st.mu.Unlock()
	st.building = nil
	if err != nil {
		st.next = c.now().Add(searchLexiconRetry)
		c.log.Warn("catalog: build search spelling index", "err", err)
		return
	}
	st.lx.Store(lx)
	st.gen, st.at = gen, c.now()
	c.log.Debug("catalog: search spelling index built", "words", lx.size(), "took", time.Since(started))
}

// loadSearchLexicon reads every movie and show name. One ordered pass:
// each title's row repeats once per alternative title.
func (c *Catalog) loadSearchLexicon(ctx context.Context) (*searchLexicon, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT t.key, t.title, t.original_title, COALESCE(a.title, '') FROM catalog_titles t
		LEFT JOIN catalog_alt_titles a ON a.key=t.key
		WHERE t.type IN ('movie','show') AND t.adult=0 ORDER BY t.key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	b := newLexiconBuilder()
	var names []string
	last := int64(-1)
	for rows.Next() {
		var key int64
		var title, original, alt string
		if err := rows.Scan(&key, &title, &original, &alt); err != nil {
			return nil, err
		}
		if key != last {
			if len(names) > 0 {
				b.addTitle(names...)
			}
			names = append(names[:0], title)
			if original != "" && original != title {
				names = append(names, original)
			}
			last = key
		}
		if alt != "" {
			names = append(names, alt)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(names) > 0 {
		b.addTitle(names...)
	}
	return b.build(), nil
}

// queryWord is one word of a search query, with where it sits in the query.
type queryWord struct {
	foldedWord
	start, end int
}

// splitQuery splits query into words as lexiconWords does (without parts).
func splitQuery(query string) []queryWord {
	var out []queryWord
	i := 0
	for i < len(query) {
		r, n := utf8.DecodeRuneInString(query[i:])
		if isWordBreak(r) {
			i += n
			continue
		}
		start := i
		for i < len(query) {
			r, n := utf8.DecodeRuneInString(query[i:])
			if isWordBreak(r) {
				break
			}
			i += n
		}
		field := query[start:i]
		trimmed := strings.TrimLeftFunc(field, func(r rune) bool { return !isWordRune(r) })
		s := start + len(field) - len(trimmed)
		trimmed = strings.TrimRightFunc(trimmed, func(r rune) bool { return !isWordRune(r) })
		if trimmed == "" {
			continue
		}
		surface := strings.ToLower(trimmed)
		if key := foldKey(surface); key != "" {
			out = append(out, queryWord{foldedWord: foldedWord{key: key, surface: surface}, start: s, end: s + len(trimmed)})
		}
	}
	return out
}

// spellRewrite is a rewritten search: its words, matched in any order, and
// how far it strays from what was typed (edits; accent, punctuation and word
// order changes are free).
type spellRewrite struct {
	words []string
	cost  int
}

// spellOption is one way to read a query word.
type spellOption struct {
	words []string
	cost  int
}

// spellPlan decides how a query may be rewritten. literalHits is whether
// the literal match found anything: when it did, only words no title
// contains are corrected; when it did not, every word may be, and the
// words as typed are also tried in any order.
func (lx *searchLexicon) spellPlan(query string, literalHits bool) []spellRewrite {
	words := splitQuery(query)
	if len(words) == 0 || len(words) > spellMaxWords {
		return nil
	}
	opts := make([][]spellOption, len(words))
	changed := false
	for i, w := range words {
		typed := spellOption{words: []string{w.surface}}
		opts[i] = []spellOption{typed}
		idx := lx.find(w.key)
		known := idx >= 0 && containsString(lx.surfaces(idx), w.surface)
		if !known && i == len(words)-1 && lx.hasPrefix(w.key) && idx < 0 {
			// The word being typed: a prefix of a title word is no misspelling.
			known = true
		}
		if idx >= 0 {
			for _, s := range lx.surfaces(idx) {
				if s != w.surface {
					opts[i] = append(opts[i], spellOption{words: []string{s}})
					changed = true
				}
			}
		}
		if known && literalHits {
			continue
		}
		if dist := spellDistance(w.key); dist > 0 {
			for _, m := range lx.near(w.key, dist, spellNearPerWord) {
				opts[i] = append(opts[i], spellOption{words: []string{lx.surfaces(m.idx)[0]}, cost: m.dist})
				changed = true
			}
		}
		if idx < 0 && !known {
			if split := lx.split(w.key); split != nil {
				opts[i] = append(opts[i], spellOption{words: split, cost: 1})
				changed = true
			}
		}
	}
	if !changed && (literalHits || len(words) < 2) {
		return nil
	}
	var out []spellRewrite
	if !literalHits && len(words) >= 2 {
		// The words as typed, in any order and across punctuation.
		typed := make([]string, len(words))
		for i, w := range words {
			typed[i] = w.surface
		}
		out = append(out, spellRewrite{words: typed})
	}
	// One or two words changed at a time, cheapest first: a query rarely
	// holds more than two mistakes, and this keeps the rewrites few.
	type combo struct {
		repl    map[int]spellOption
		cost    int
		changes int
	}
	var combos []combo
	for i := range words {
		for _, o := range opts[i][1:] {
			combos = append(combos, combo{repl: map[int]spellOption{i: o}, cost: o.cost, changes: 1})
		}
	}
	for i := range words {
		for k := i + 1; k < len(words); k++ {
			for _, a := range opts[i][1:min(len(opts[i]), 3)] {
				for _, b := range opts[k][1:min(len(opts[k]), 3)] {
					combos = append(combos, combo{repl: map[int]spellOption{i: a, k: b}, cost: a.cost + b.cost, changes: 2})
				}
			}
		}
	}
	sort.SliceStable(combos, func(a, b int) bool {
		if combos[a].cost != combos[b].cost {
			return combos[a].cost < combos[b].cost
		}
		return combos[a].changes < combos[b].changes
	})
	for _, cb := range combos {
		if len(out) >= spellMaxRewrites {
			break
		}
		var ws []string
		for i, w := range words {
			if o, ok := cb.repl[i]; ok {
				ws = append(ws, o.words...)
			} else {
				ws = append(ws, w.surface)
			}
		}
		out = append(out, spellRewrite{words: ws, cost: cb.cost})
	}
	return out
}

// spellDistance is how many edits a query word may be corrected by: none for
// short words and numbers (so "It", "Up" and "1917" stay as typed), one for
// words of four to six letters, two for longer ones.
func spellDistance(key string) int {
	n := utf8.RuneCountInString(key)
	if n < 4 || strings.IndexFunc(key, func(r rune) bool { return !unicode.IsDigit(r) }) < 0 {
		return 0
	}
	if n <= 6 {
		return 1
	}
	return 2
}

// split returns key as two title words ("starwars" -> star, wars), the
// most common such pair, or nil.
func (lx *searchLexicon) split(key string) []string {
	r := []rune(key)
	var best []string
	bestDocs := uint32(0)
	for i := 2; i <= len(r)-2; i++ {
		a, b := lx.find(string(r[:i])), lx.find(string(r[i:]))
		if a < 0 || b < 0 {
			continue
		}
		if d := min(lx.docs[a], lx.docs[b]); best == nil || d > bestDocs {
			best, bestDocs = []string{lx.surfaces(a)[0], lx.surfaces(b)[0]}, d
		}
	}
	return best
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// titleWordsMatch matches titles whose name, original name or one
// alternative name contains every word (in any order). alias is as for
// titleMatch. ok is false when no word is long enough for the trigram index
// on SQLite, where the match would read every title.
func (c *Catalog) titleWordsMatch(alias string, words []string) (string, []any, bool) {
	if alias == "" {
		alias = "catalog_titles."
	}
	indexed := false
	for _, w := range words {
		if utf8.RuneCountInString(w) >= 3 {
			indexed = true
		}
	}
	if len(words) == 0 || !indexed {
		return "", nil, false
	}
	likes := make([]any, len(words))
	for i, w := range words {
		likes[i] = "%" + escapeLike(w) + "%"
	}
	repeat := func(n int) []any {
		var out []any
		for range n {
			out = append(out, likes...)
		}
		return out
	}
	d := store.DialectOf(c.db)
	if d == store.SQLite {
		all := func(col string) string {
			parts := make([]string, len(words))
			for i := range words {
				parts[i] = col + " LIKE ?"
			}
			return strings.Join(parts, " AND ")
		}
		// FTS5 answers several LIKE constraints together from the index; it
		// cannot with an ESCAPE clause, and the words hold no metacharacters.
		return alias + `key IN (SELECT rowid FROM catalog_titles_fts WHERE ` + all("title") + ` UNION SELECT rowid FROM catalog_titles_fts WHERE ` + all("original_title") + `
			UNION SELECT a.key FROM catalog_alt_titles a WHERE a.rowid IN (SELECT rowid FROM catalog_alt_titles_fts WHERE ` + all("title") + `))`, repeat(3), true
	}
	op := store.Like(d)
	all := func(col string) string {
		parts := make([]string, len(words))
		for i := range words {
			parts[i] = col + " " + op + ` ? ESCAPE '\'`
		}
		return "(" + strings.Join(parts, " AND ") + ")"
	}
	return "(" + all(alias+"title") + " OR " + all(alias+"original_title") + " OR EXISTS (SELECT 1 FROM catalog_alt_titles a WHERE a.key=" + alias + "key AND " + all("a.title") + "))", repeat(3), true
}

// spellMatch is a title found by a rewrite, for ranking.
type spellMatch struct {
	cost  int
	words []string
}

// searchSpelling runs the query's rewrites against the local catalogue
// (the remote catalogue is searched with the query as typed) and returns
// what they found, cheapest rewrite first. Rewrites stop at the first cost
// that found anything, so a near correction is never buried under a far one.
func (c *Catalog) searchSpelling(ctx context.Context, query string, literalHits bool, limit int, run func(match string, args []any) ([]core.Title, error)) ([]core.Title, map[core.RatingKey]spellMatch) {
	lx := c.searchLexicon(ctx, true)
	if lx == nil {
		return nil, nil
	}
	plan := lx.spellPlan(query, literalHits)
	if len(plan) == 0 {
		return nil, nil
	}
	var out []core.Title
	found := map[core.RatingKey]spellMatch{}
	foundCost := -1
	for _, rw := range plan {
		if foundCost >= 0 && rw.cost > foundCost {
			break
		}
		if len(out) >= limit || ctx.Err() != nil {
			break
		}
		match, args, ok := c.titleWordsMatch("", rw.words)
		if !ok {
			continue
		}
		items, err := run(match, args)
		if err != nil {
			c.log.Debug("catalog: spelling search", "err", err)
			continue
		}
		for _, t := range items {
			if _, dup := found[t.Key]; dup {
				continue
			}
			found[t.Key] = spellMatch{cost: rw.cost, words: rw.words}
			out = append(out, t)
		}
		if len(items) > 0 && foundCost < 0 {
			foundCost = rw.cost
		}
	}
	return out, found
}

// spellCloseness ranks a title against a rewrite's words: 0 the title is
// those words, 1 it starts with them, 2 it holds them in order, 3 in any.
func spellCloseness(title string, words []string) int {
	keys := make([]string, 0, len(words))
	for _, w := range words {
		if k := foldKey(w); k != "" {
			keys = append(keys, k)
		}
	}
	want := strings.Join(keys, " ")
	var have []string
	for _, w := range splitQuery(title) {
		have = append(have, w.key)
	}
	got := strings.Join(have, " ")
	switch {
	case got == want:
		return 0
	case strings.HasPrefix(got, want+" "):
		return 1
	case strings.Contains(" "+got+" ", " "+want+" "):
		return 2
	}
	return 3
}
