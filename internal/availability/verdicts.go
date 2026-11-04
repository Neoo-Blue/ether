package availability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/store"
)

func init() {
	// One durable "When they can play" verdict per (policy, title, release).
	// The policy key covers the owner's profile and resolver settings, so a
	// section load only evaluates feed items it has never judged (#303).
	store.Register("availability_verdicts", 1, `
CREATE TABLE availability_verdicts (
 policy TEXT NOT NULL, rating_key INTEGER NOT NULL, release_id TEXT NOT NULL,
 digest TEXT NOT NULL, reason TEXT NOT NULL DEFAULT '', evaluated_at INTEGER NOT NULL,
 PRIMARY KEY(policy, rating_key, release_id));
CREATE INDEX availability_verdicts_release ON availability_verdicts(release_id);
CREATE INDEX availability_verdicts_age ON availability_verdicts(evaluated_at);`)
}

// feedPair is one retained release considered as evidence for one title.
// The release carries its parsed Info.
type feedPair struct {
	t *core.Title
	r core.Release
}

type feedEvaluator interface {
	FeedEvaluation(context.Context) (core.FeedEvaluation, error)
}

// judge reports which pairs the owner's quality policy accepts. A validator
// that prepares evaluation passes is asked once per pass, and only for
// pairs with no durable verdict under the current policy; others are asked
// one release at a time.
func (s *Service) judge(ctx context.Context, pairs []feedPair) []bool {
	ok := make([]bool, len(pairs))
	if len(pairs) == 0 {
		return ok
	}
	source, batched := s.d.Validator.(feedEvaluator)
	if !batched {
		for i, p := range pairs {
			_, reason, err := s.d.Validator.FeedCandidate(ctx, p.t, p.r)
			ok[i] = err == nil && reason == ""
		}
		return ok
	}
	ev, err := source.FeedEvaluation(ctx)
	if err != nil {
		return ok
	}
	defer ev.Close()
	policy := ev.Key()
	digests := make([]string, len(pairs))
	titles := map[core.RatingKey]string{}
	for i, p := range pairs {
		td, seen := titles[p.t.Key]
		if !seen {
			td = titleDigest(p.t)
			titles[p.t.Key] = td
		}
		digests[i] = verdictDigest(td, p.r)
	}
	stored := s.storedVerdicts(ctx, policy, titles)
	type group struct {
		t       *core.Title
		indexes []int
	}
	var order []core.RatingKey
	pending := map[core.RatingKey]*group{}
	for i, p := range pairs {
		if v, hit := stored[verdictKey(p.t.Key, p.r.ID)]; hit && v.digest == digests[i] {
			ok[i] = v.reason == "" && ev.Transient(p.r.ID) == ""
			continue
		}
		g := pending[p.t.Key]
		if g == nil {
			g = &group{t: p.t}
			pending[p.t.Key] = g
			order = append(order, p.t.Key)
		}
		g.indexes = append(g.indexes, i)
	}
	type fresh struct {
		key             core.RatingKey
		id, digest, why string
	}
	var durable []fresh
	written := map[string]bool{}
	for _, k := range order {
		g := pending[k]
		rs := make([]core.Release, len(g.indexes))
		for j, i := range g.indexes {
			rs[j] = pairs[i].r
		}
		verdicts, err := ev.Evaluate(ctx, g.t, rs)
		if err != nil || len(verdicts) != len(rs) {
			continue
		}
		for j, i := range g.indexes {
			v := verdicts[j]
			ok[i] = v.Reason == ""
			vk := verdictKey(k, pairs[i].r.ID)
			if !v.Volatile && !written[vk] {
				written[vk] = true
				durable = append(durable, fresh{key: k, id: pairs[i].r.ID, digest: digests[i], why: v.Reason})
			}
		}
	}
	if len(durable) == 0 {
		return ok
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ok
	}
	defer tx.Rollback()
	now := s.now().Unix()
	for _, f := range durable {
		if _, err := tx.ExecContext(ctx, `INSERT INTO availability_verdicts(policy,rating_key,release_id,digest,reason,evaluated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(policy,rating_key,release_id) DO UPDATE SET digest=excluded.digest,reason=excluded.reason,evaluated_at=excluded.evaluated_at`, policy, int64(f.key), f.id, f.digest, f.why, now); err != nil {
			return ok
		}
	}
	_ = tx.Commit()
	return ok
}

type storedVerdict struct{ digest, reason string }

func verdictKey(key core.RatingKey, id string) string {
	return observationKey(id, key, "")
}

func (s *Service) storedVerdicts(ctx context.Context, policy string, titles map[core.RatingKey]string) map[string]storedVerdict {
	out := map[string]storedVerdict{}
	keys := make([]any, 0, len(titles))
	for k := range titles {
		keys = append(keys, int64(k))
	}
	for start := 0; start < len(keys); start += 500 {
		batch := keys[start:min(start+500, len(keys))]
		args := append([]any{policy}, batch...)
		rows, err := s.db.QueryContext(ctx, `SELECT rating_key,release_id,digest,reason FROM availability_verdicts WHERE policy=? AND rating_key IN (`+strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")+`)`, args...)
		if err != nil {
			return out
		}
		for rows.Next() {
			var key int64
			var id string
			var v storedVerdict
			if rows.Scan(&key, &id, &v.digest, &v.reason) == nil {
				out[verdictKey(core.RatingKey(key), id)] = v
			}
		}
		rows.Close()
	}
	return out
}

// titleDigest covers the title facts release matching and the quality
// policy read: identity, names, year and dates, runtime (size sanity),
// languages, genres and keywords (anime), countries and directors.
// Popularity and artwork churn do not invalidate a verdict.
func titleDigest(t *core.Title) string {
	directors := make([]string, len(t.Directors))
	for i, d := range t.Directors {
		directors[i] = d.Name
	}
	b, _ := json.Marshal([]any{t.Key, t.Type, t.IDs, t.ShowKey, t.ShowIDs, t.ShowTitle, t.ShowOriginalTitle, t.ShowYear,
		t.Season, t.Episode, t.AbsoluteEpisode, t.Title, t.OriginalTitle, t.Year, t.Released, t.Runtime, t.Genres,
		t.Keywords, t.AlternativeTitles, t.ShowAlternativeTitles, t.Countries, t.Language, directors})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// verdictDigest binds a verdict to the exact release descriptor (its parsed
// Info derives from the name) and title facts it judged.
func verdictDigest(title string, r core.Release) string {
	r.Info = core.ReleaseInfo{}
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(append([]byte(title+"\x00"), b...))
	return hex.EncodeToString(sum[:])
}

// pruneVerdicts keeps the verdict table to the current policy, to releases
// still retained as feed items or observations, and to 60,000 rows: the
// retained feed items plus the release observations it can describe.
func (s *Service) pruneVerdicts(ctx context.Context, retainedChanged bool) error {
	if source, ok := s.d.Validator.(feedEvaluator); ok {
		if ev, err := source.FeedEvaluation(ctx); err == nil {
			policy := ev.Key()
			ev.Close()
			var stale bool
			if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM availability_verdicts WHERE policy<>?)`, policy).Scan(&stale); err != nil {
				return err
			}
			if stale {
				if _, err := s.db.ExecContext(ctx, `DELETE FROM availability_verdicts WHERE policy<>?`, policy); err != nil {
					return err
				}
			}
		}
	}
	if retainedChanged {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM availability_verdicts WHERE NOT EXISTS(SELECT 1 FROM availability_feed_items f WHERE f.id=availability_verdicts.release_id) AND NOT EXISTS(SELECT 1 FROM availability_releases r WHERE r.id=availability_verdicts.release_id)`); err != nil {
			return err
		}
	}
	var overflow bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM availability_verdicts LIMIT 1 OFFSET 60000)`).Scan(&overflow); err != nil {
		return err
	}
	if overflow {
		_, err := s.db.ExecContext(ctx, `DELETE FROM availability_verdicts WHERE (policy,rating_key,release_id) IN (SELECT policy,rating_key,release_id FROM availability_verdicts ORDER BY evaluated_at DESC LIMIT 2147483647 OFFSET 60000)`)
		return err
	}
	return nil
}
