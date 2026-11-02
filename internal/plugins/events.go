package plugins

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

func newEventID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Publish normalizes and fans out one Ether event: it is written to the
// 7-day event log and queued for every enabled plugin that subscribed to
// it (and was not revoked). Viewer identity is stripped per plugin unless
// the plugin's grants allow it.
func (m *Manager) Publish(ctx context.Context, ev Event) {
	if !KnownEvents[ev.Name] {
		return
	}
	if ev.ID == "" {
		ev.ID = newEventID()
	}
	if ev.At.IsZero() {
		ev.At = m.now()
	}
	m.store.logEvent(ctx, ev)
	recs, err := m.store.list(ctx)
	if err != nil {
		return
	}
	for _, r := range recs {
		if !r.Enabled || r.Manifest.Ether == nil || !r.Manifest.HasCapability(CapEvents) {
			continue
		}
		sub := false
		if evMeta := r.Manifest.Ether.Contributes.Events; evMeta != nil {
			for _, n := range evMeta.Subscribe {
				if n == ev.Name {
					sub = true
				}
			}
		}
		if !sub {
			continue
		}
		g, _ := m.store.grants(ctx, r.ID)
		if g.revoked("events") {
			continue
		}
		out := ev
		if !viewerOK(g, normLevel(r.Manifest.Ether.Permissions.Viewers.Identity), ev.ViewerID) {
			out.ViewerID = 0
			out.Payload = stripViewer(out.Payload)
		}
		dropped, err := m.store.enqueue(ctx, r.ID, out)
		if err == nil && dropped > 0 {
			if h, err := m.store.health(ctx, r.ID); err == nil {
				h.Drops += int64(dropped)
				_ = m.store.putHealth(ctx, r.ID, h)
			}
		}
	}
}

func stripViewer(payload json.RawMessage) json.RawMessage {
	var doc map[string]any
	if json.Unmarshal(payload, &doc) != nil {
		return payload
	}
	changed := false
	for k := range doc {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "viewer") || strings.Contains(lk, "user") {
			delete(doc, k)
			changed = true
		}
	}
	if !changed {
		return payload
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return payload
	}
	return out
}

// NormalizePlayback maps a playback.Manager event kind to §5.7 names.
// state is the raw timeline state for updates (playing, paused, stopped).
// It returns "" for kinds with no normalized event.
func NormalizePlayback(kind, state string) string {
	switch kind {
	case "start", "started":
		return EventPlaybackStarted
	case "stop", "stopped":
		return EventPlaybackStopped
	case "completed", "complete":
		return EventPlaybackCompleted
	case "error":
		return EventPlaybackError
	case "failover", "downfailover":
		return EventPlaybackFailover
	case "update", "progress":
		switch state {
		case "paused":
			return EventPlaybackPaused
		case "playing":
			return EventPlaybackResumed
		}
	}
	return ""
}

// NormalizeNotify maps a notify event kind to §5.7 names.
func NormalizeNotify(kind string) string {
	switch kind {
	case "title.available":
		return EventTitleAvailable
	case "user.joined":
		return EventViewerAdded
	case "playback.error":
		return EventPlaybackError
	case "playback.failover":
		return EventPlaybackFailover
	case "playback.start":
		return EventPlaybackStarted
	case "playback.stop":
		return EventPlaybackStopped
	}
	return ""
}

// DeliverOutbox sends queued events to one plugin in batches of 50 and
// deletes the ones it acks. A missing ack keeps events for redelivery
// (at least once).
func (m *Manager) DeliverOutbox(ctx context.Context, pluginID string) error {
	for {
		evs, ids, err := m.store.outboxBatch(ctx, pluginID, 50)
		if err != nil || len(evs) == 0 {
			return err
		}
		body, _ := json.Marshal(map[string]any{"events": evs})
		resp, err := m.Call(ctx, pluginID, Request{Method: "POST", Path: "/ether/event", Body: body})
		if err != nil {
			return err
		}
		var out struct {
			Ack []string `json:"ack"`
		}
		_ = json.Unmarshal(resp.Body, &out)
		acked := map[string]bool{}
		for _, id := range out.Ack {
			acked[id] = true
		}
		var done []int64
		for i, ev := range evs {
			if acked[ev.ID] {
				done = append(done, ids[i])
			}
		}
		if len(done) == 0 {
			return nil // nothing acked; try again later
		}
		if err := m.store.ackOutbox(ctx, done); err != nil {
			return err
		}
		if len(done) < len(evs) {
			return nil
		}
	}
}

// Run drives health checks, scheduled jobs and outbox delivery until ctx
// ends. Leader plugins run jobs and event delivery only on the leader;
// every_node plugins run everywhere. Health checks run on every node.
func (m *Manager) Run(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	lastHealth := map[string]time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			m.runOnce(ctx, lastHealth)
		}
	}
}

// runOnce is one scheduler pass; tests drive it directly.
func (m *Manager) runOnce(ctx context.Context, lastHealth map[string]time.Time) {
	recs, err := m.store.list(ctx)
	if err != nil {
		return
	}
	now := m.now()
	for _, r := range recs {
		if !r.Enabled || r.Manifest.Ether == nil {
			continue
		}
		interval := time.Minute
		if d, err := time.ParseDuration(r.Manifest.Ether.Health.Interval); err == nil && d >= 15*time.Second {
			interval = d
		}
		if _, hasRuntime := m.factoryFor(r); hasRuntime && now.Sub(lastHealth[r.ID]) >= interval {
			lastHealth[r.ID] = now
			h, _ := m.store.health(ctx, r.ID)
			if h.NextRetry.IsZero() || !now.Before(h.NextRetry) || h.Status != "error" {
				_, _ = m.CheckHealth(ctx, r.ID)
			}
		}
		if !m.mayRunJobs(r) {
			continue
		}
		for _, job := range r.Manifest.Ether.Schedule {
			every, err := time.ParseDuration(job.Every)
			if err != nil {
				continue
			}
			key := r.ID + "|" + job.Call
			m.mu.Lock()
			next, seen := m.nextRun[key]
			if !seen {
				m.nextRun[key] = now.Add(every)
				m.mu.Unlock()
				continue
			}
			if now.Before(next) {
				m.mu.Unlock()
				continue
			}
			m.nextRun[key] = now.Add(every)
			m.mu.Unlock()
			_, _ = m.Call(ctx, r.ID, Request{Method: "POST", Path: job.Call})
		}
		_ = m.DeliverOutbox(ctx, r.ID)
		// Capability refreshes ride the schedule: list sources and
		// channels declare their own refresh in contributes.
		m.maybeRefresh(ctx, r, now)
	}
}

func (m *Manager) factoryFor(r Record) (RuntimeFactory, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.Manifest.Ether == nil {
		return nil, false
	}
	f, ok := m.fatories[r.Manifest.Ether.Runtime.Kind]
	return f, ok
}

// mayRunJobs applies cluster placement: leader plugins only on the
// leader, every_node everywhere.
func (m *Manager) mayRunJobs(r Record) bool {
	if r.Manifest.Ether.Cluster == "every_node" {
		return true
	}
	return m.isLeader()
}

func (m *Manager) maybeRefresh(ctx context.Context, r Record, now time.Time) {
	type refresh struct {
		key   string
		every time.Duration
		path  string
	}
	var jobs []refresh
	c := r.Manifest.Ether.Contributes
	if c.Channels != nil && c.Channels.Refresh != "" {
		if d, err := time.ParseDuration(c.Channels.Refresh); err == nil {
			jobs = append(jobs, refresh{"channels", d, "/ether/channels.json"})
		}
	}
	for _, ls := range c.ListSources {
		refreshStr := ls.Refresh
		if refreshStr == "" {
			refreshStr = "1h"
		}
		if d, err := time.ParseDuration(refreshStr); err == nil {
			jobs = append(jobs, refresh{"list:" + ls.Key, d, "/catalog/movie/" + ls.Catalog + ".json"})
		}
	}
	for _, j := range jobs {
		key := r.ID + "|refresh:" + j.key
		m.mu.Lock()
		next, seen := m.nextRun[key]
		if !seen {
			m.nextRun[key] = now.Add(j.every)
			m.mu.Unlock()
			continue
		}
		if now.Before(next) {
			m.mu.Unlock()
			continue
		}
		m.nextRun[key] = now.Add(j.every)
		m.mu.Unlock()
		_, _ = m.Call(ctx, r.ID, Request{Method: "GET", Path: j.path})
	}
}
