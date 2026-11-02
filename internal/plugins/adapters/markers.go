package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/markers"
	"github.com/braedonsaunders/ether/internal/plugins"
)

type markerJob struct {
	key      core.RatingKey
	duration time.Duration
}
type markerWork struct {
	job  markerJob
	done chan struct{}
}
type MarkerBridge struct {
	mgr      *plugins.Manager
	svc      *markers.Service
	catalog  core.Catalog
	selected func(string) bool
	mu       sync.Mutex
	active   map[string]bool
	recent   map[markerJob]time.Time
	jobs     chan markerWork
	done     chan struct{}
}

func AttachMarkers(ctx context.Context, mgr *plugins.Manager, svc *markers.Service, catalog core.Catalog, selected func(string) bool) *MarkerBridge {
	b := &MarkerBridge{mgr: mgr, svc: svc, catalog: catalog, selected: selected, active: map[string]bool{}, recent: map[markerJob]time.Time{}, jobs: make(chan markerWork, 128), done: make(chan struct{})}
	mgr.OnSync(b.sync)
	go b.run(ctx)
	return b
}
func (b *MarkerBridge) sync(ctx context.Context, r plugins.Record) {
	b.mu.Lock()
	if r.Enabled && r.Manifest.Ether != nil && r.Manifest.HasCapability(plugins.CapMarkers) {
		b.active[r.ID] = true
	} else {
		delete(b.active, r.ID)
	}
	enabled := b.active[r.ID]
	if !enabled {
		_ = b.svc.DeletePlugin(ctx, extend.PluginID(r.ID, "markers"))
	}
	b.mu.Unlock()
}

// Schedule queues work without contacting a plugin on the caller's path.
func (b *MarkerBridge) Schedule(key core.RatingKey, d time.Duration) <-chan struct{} {
	done := make(chan struct{})
	if d <= 0 {
		close(done)
		return done
	}
	select {
	case b.jobs <- markerWork{markerJob{key, d}, done}:
	default:
		close(done)
	}
	return done
}
func (b *MarkerBridge) run(ctx context.Context) {
	defer close(b.done)
	if targets, err := b.svc.RefreshTargets(ctx); err == nil {
		for _, target := range targets {
			b.recent[markerJob{target.Key, target.Duration}] = target.LastPlayed
		}
	}
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case work := <-b.jobs:
			j := work.job
			b.mu.Lock()
			b.recent[j] = time.Now()
			_ = b.svc.RememberRefreshTarget(ctx, j.key, j.duration)
			b.mu.Unlock()
			b.refresh(ctx, j)
			if j.key.Kind() == core.KindEpisode {
				if next, err := b.catalog.NextEpisode(ctx, j.key); err == nil && next != nil {
					duration := next.Runtime
					if duration <= 0 {
						duration = j.duration
					}
					nextJob := markerJob{next.Key, duration}
					b.mu.Lock()
					b.recent[nextJob] = time.Now()
					b.mu.Unlock()
					_ = b.svc.RememberRefreshTarget(ctx, next.Key, duration)
					b.refresh(ctx, nextJob)
				}
			}
			close(work.done)
		case <-ticker.C:
			b.mu.Lock()
			var jobs []markerJob
			for j, at := range b.recent {
				if time.Since(at) > 30*24*time.Hour {
					delete(b.recent, j)
				} else {
					jobs = append(jobs, j)
				}
			}
			b.mu.Unlock()
			for _, j := range jobs {
				b.refresh(ctx, j)
			}
		}
	}
}
func (b *MarkerBridge) refresh(ctx context.Context, j markerJob) {
	title, err := b.catalog.Get(ctx, j.key)
	if err != nil || title == nil {
		return
	}
	typ, id := subtitleIdentity(title)
	if id == "" {
		return
	}
	b.mu.Lock()
	ids := make([]string, 0, len(b.active))
	for id := range b.active {
		if b.selected != nil && !b.selected(extend.PluginID(id, "markers")) {
			continue
		}
		ids = append(ids, id)
	}
	b.mu.Unlock()
	for _, pluginID := range ids {
		resp, err := b.mgr.Call(ctx, pluginID, plugins.Request{Method: "GET", Path: "/ether/markers/" + url.PathEscape(typ) + "/" + url.PathEscape(id) + ".json"})
		if err != nil {
			continue
		}
		if resp.Status >= 400 {
			b.mgr.RecordFailure(ctx, pluginID, fmt.Sprintf("markers HTTP %d", resp.Status))
			continue
		}
		var result struct {
			Markers *[]struct {
				Kind  string  `json:"kind"`
				Start float64 `json:"start"`
				End   float64 `json:"end"`
			} `json:"markers"`
		}
		if json.Unmarshal(resp.Body, &result) != nil || result.Markers == nil {
			b.mgr.RecordFailure(ctx, pluginID, "invalid markers response")
			continue
		}
		ranges := make([]markers.Marker, 0, len(*result.Markers))
		for _, item := range *result.Markers {
			ranges = append(ranges, markers.Marker{Type: item.Kind, StartMS: int64(item.Start * 1000), EndMS: int64(item.End * 1000), Final: item.Kind == "credits" && int64(item.End*1000) >= j.duration.Milliseconds()-1000})
		}
		b.mu.Lock()
		active := b.active[pluginID]
		if active {
			err = b.svc.PutPlugin(ctx, j.key, j.duration, extend.PluginID(pluginID, "markers"), ranges)
		}
		b.mu.Unlock()
		if err != nil {
			b.mgr.RecordFailure(ctx, pluginID, "invalid marker ranges")
		}
	}
}

// Wait waits for the background worker to stop after its context is cancelled.
func (b *MarkerBridge) Wait() { <-b.done }
