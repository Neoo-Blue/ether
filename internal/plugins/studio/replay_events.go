package studio

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/plugins/wasm"
)

// EventReplayReport summarizes what a plugin would have done when fed recent
// normalized events from plugins_event_log in a dry-run context.
type EventReplayReport struct {
	Days            int                    `json:"days"`
	EventsLoaded    int                    `json:"eventsLoaded"`
	EventsMatched   int                    `json:"eventsMatched"`
	EventsDelivered int                    `json:"eventsDelivered"`
	EventsAcked     int                    `json:"eventsAcked"`
	Batches         int                    `json:"batches"`
	Writes          []RecordedWrite        `json:"writes"`
	Notifications   []RecordedNotification `json:"notifications"`
	OutboundCalls   []RecordedOutbound     `json:"outboundCalls,omitempty"`
	Logs            []RecordedLog          `json:"logs,omitempty"`
	TotalUnits      int64                  `json:"totalUnits"`
}

// RecordedWrite is one host write captured during a dry-run replay without
// being applied to live storage.
type RecordedWrite struct {
	Op       string          `json:"op"`
	ViewerID int64           `json:"viewerId,omitempty"`
	Target   string          `json:"target,omitempty"`
	Watched  *bool           `json:"watched,omitempty"`
	Value    json.RawMessage `json:"value,omitempty"`
}

// RecordedNotification is one notification or notice rendered during a dry-run
// replay without being sent.
type RecordedNotification struct {
	Kind    string          `json:"kind"` // "owner_notice", "channel_render", "webhook"
	EventID string          `json:"eventId,omitempty"`
	Title   string          `json:"title,omitempty"`
	Text    string          `json:"text"`
	Format  string          `json:"format,omitempty"`
	Target  string          `json:"target,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// RecordedOutbound is one outbound HTTP request intercepted in dry-run mode.
type RecordedOutbound struct {
	Method string `json:"method"`
	URL    string `json:"url"`
	Body   string `json:"body,omitempty"`
}

// RecordedLog is one plugin log line captured during replay.
type RecordedLog struct {
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

func (r *Runner) replayEvents(ctx context.Context, target *resolvedTarget, params ReplayParams) (*EventReplayReport, error) {
	days := params.Days
	if days <= 0 {
		days = DefaultEventDays
	}
	if days > MaxEventDays {
		days = MaxEventDays
	}
	limit := params.Limit
	if limit <= 0 {
		limit = DefaultEventLimit
	}
	if limit > MaxEventLimit {
		limit = MaxEventLimit
	}

	cutoff := r.now().Add(-time.Duration(days) * 24 * time.Hour)
	allEvents, err := r.loadEventLog(ctx, cutoff, limit)
	if err != nil {
		return nil, err
	}

	subscribed := subscribedEventSet(target.manifest, params.Events)
	matched := make([]plugins.Event, 0, len(allEvents))
	for _, ev := range allEvents {
		if len(subscribed) > 0 && !subscribed[ev.Name] {
			continue
		}
		matched = append(matched, sanitizeEventForTarget(ev, target.manifest, target.grants))
	}

	report := &EventReplayReport{
		Days:          days,
		EventsLoaded:  len(allEvents),
		EventsMatched: len(matched),
		Writes:        []RecordedWrite{},
		Notifications: []RecordedNotification{},
	}
	if len(matched) == 0 {
		return report, nil
	}

	// If this is an installed non-hosted plugin running via Manager.Call,
	// force shadow mode for the duration of the replay so writes are recorded
	// rather than applied.
	var prevJournalMaxID int64
	if target.wasmPlug == nil && target.installed && target.mgr != nil {
		rec, err := target.mgr.Get(ctx, target.id)
		if err == nil && !rec.Shadow {
			_ = target.mgr.SetShadow(ctx, target.id, true)
			defer func() {
				_ = target.mgr.SetShadow(context.Background(), target.id, false)
			}()
		}
		if entries, err := target.mgr.Journal(ctx, target.id, 1, true); err == nil && len(entries) > 0 {
			prevJournalMaxID = entries[0].ID
		}
	}

	const batchSize = 50
	for start := 0; start < len(matched); start += batchSize {
		if err := ctx.Err(); err != nil {
			break
		}
		end := start + batchSize
		if end > len(matched) {
			end = len(matched)
		}
		batch := matched[start:end]
		body, _ := json.Marshal(map[string]any{
			"events": batch,
			"dryRun": true,
		})
		resp, out, err := target.call(ctx, plugins.Request{
			PluginID: target.id,
			Method:   "POST",
			Path:     "/ether/event",
			Body:     body,
		})
		report.TotalUnits += out.Units
		report.Batches++
		report.EventsDelivered += len(batch)

		if err != nil {
			return nil, err
		}

		// If a notification_channel plugin only implements /ether/notify, test-render each event.
		if resp.Status == http.StatusNotFound && target.manifest.HasCapability(plugins.CapNotify) {
			for _, ev := range batch {
				nbody, _ := json.Marshal(map[string]any{
					"event":  ev,
					"dryRun": true,
				})
				nresp, nout, nerr := target.call(ctx, plugins.Request{
					PluginID: target.id,
					Method:   "POST",
					Path:     "/ether/notify",
					Body:     nbody,
				})
				report.TotalUnits += nout.Units
				if nerr == nil && nresp.Status >= 200 && nresp.Status < 300 {
					report.EventsAcked++
					collectRenderedNotifications(nresp.Body, ev.ID, &report.Notifications)
				}
			}
			continue
		}

		var ackDoc struct {
			Ack           []string          `json:"ack"`
			Notifications []json.RawMessage `json:"notifications"`
			Messages      []json.RawMessage `json:"messages"`
			Rendered      string            `json:"rendered"`
		}
		if len(resp.Body) > 0 && json.Unmarshal(resp.Body, &ackDoc) == nil {
			if len(ackDoc.Ack) > 0 {
				report.EventsAcked += len(ackDoc.Ack)
			} else if resp.Status >= 200 && resp.Status < 300 {
				report.EventsAcked += len(batch)
			}
			collectRenderedNotifications(resp.Body, "", &report.Notifications)
		} else if resp.Status >= 200 && resp.Status < 300 {
			report.EventsAcked += len(batch)
		}
	}

	if target.dryHost != nil {
		target.dryHost.mu.Lock()
		report.Writes = append(report.Writes, target.dryHost.writes...)
		report.Notifications = append(report.Notifications, target.dryHost.notifications...)
		report.OutboundCalls = append(report.OutboundCalls, target.dryHost.outbound...)
		report.Logs = append(report.Logs, target.dryHost.logs...)
		target.dryHost.mu.Unlock()
	}

	if target.wasmPlug == nil && target.installed && target.mgr != nil {
		if entries, err := target.mgr.Journal(ctx, target.id, 200, true); err == nil {
			for i := len(entries) - 1; i >= 0; i-- {
				e := entries[i]
				if e.ID <= prevJournalMaxID {
					continue
				}
				report.Writes = append(report.Writes, RecordedWrite{
					Op:     e.Op,
					Target: e.Detail,
					Value:  e.Inverse,
				})
			}
		}
	}

	return report, nil
}

func collectRenderedNotifications(raw json.RawMessage, defaultEventID string, dest *[]RecordedNotification) {
	if len(raw) == 0 {
		return
	}
	var doc struct {
		Rendered      string `json:"rendered"`
		Text          string `json:"text"`
		Message       string `json:"message"`
		Title         string `json:"title"`
		Format        string `json:"format"`
		Notifications []struct {
			EventID string `json:"eventId"`
			Title   string `json:"title"`
			Text    string `json:"text"`
			Message string `json:"message"`
			Format  string `json:"format"`
		} `json:"notifications"`
		Messages []struct {
			EventID string `json:"eventId"`
			Title   string `json:"title"`
			Text    string `json:"text"`
			Message string `json:"message"`
			Format  string `json:"format"`
		} `json:"messages"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return
	}
	for _, n := range append(doc.Notifications, doc.Messages...) {
		txt := n.Text
		if txt == "" {
			txt = n.Message
		}
		if txt == "" {
			continue
		}
		evID := n.EventID
		if evID == "" {
			evID = defaultEventID
		}
		*dest = append(*dest, RecordedNotification{
			Kind:    "channel_render",
			EventID: evID,
			Title:   n.Title,
			Text:    txt,
			Format:  n.Format,
		})
	}
	single := doc.Rendered
	if single == "" {
		single = doc.Text
	}
	if single == "" {
		single = doc.Message
	}
	if single != "" && len(doc.Notifications) == 0 && len(doc.Messages) == 0 {
		*dest = append(*dest, RecordedNotification{
			Kind:    "channel_render",
			EventID: defaultEventID,
			Title:   doc.Title,
			Text:    single,
			Format:  doc.Format,
		})
	}
}

func (r *Runner) loadEventLog(ctx context.Context, since time.Time, limit int) ([]plugins.Event, error) {
	if r.deps.DB == nil {
		return nil, nil
	}
	rows, err := r.deps.DB.QueryContext(ctx,
		`SELECT id, name, payload, created
		 FROM plugins_event_log
		 WHERE created >= ?
		 ORDER BY created DESC, id DESC
		 LIMIT ?`, since.Unix(), limit)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var rev []plugins.Event
	for rows.Next() {
		var rowID, created int64
		var name, raw string
		if err := rows.Scan(&rowID, &name, &raw, &created); err != nil {
			return nil, err
		}
		var ev plugins.Event
		if json.Unmarshal([]byte(raw), &ev) != nil || (ev.Name == "" && ev.ID == "" && len(ev.Payload) == 0) {
			ev = plugins.Event{
				ID:      strconv.FormatInt(rowID, 10),
				Name:    name,
				At:      time.Unix(created, 0).UTC(),
				Payload: json.RawMessage(raw),
			}
		} else {
			if ev.ID == "" {
				ev.ID = strconv.FormatInt(rowID, 10)
			}
			if ev.Name == "" {
				ev.Name = name
			}
			if ev.At.IsZero() {
				ev.At = time.Unix(created, 0).UTC()
			}
		}
		rev = append(rev, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Replay in chronological order so event sequences behave deterministically.
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev, nil
}

func subscribedEventSet(m plugins.Manifest, explicit []string) map[string]bool {
	if len(explicit) > 0 {
		out := make(map[string]bool, len(explicit))
		for _, e := range explicit {
			out[e] = true
		}
		return out
	}
	if m.Ether != nil && m.Ether.Contributes.Events != nil && len(m.Ether.Contributes.Events.Subscribe) > 0 {
		out := make(map[string]bool, len(m.Ether.Contributes.Events.Subscribe))
		for _, e := range m.Ether.Contributes.Events.Subscribe {
			out[e] = true
		}
		return out
	}
	return nil
}

func sanitizeEventForTarget(ev plugins.Event, m plugins.Manifest, g plugins.Grants) plugins.Event {
	if m.Ether == nil {
		ev.ViewerID = 0
		return ev
	}
	level := m.Ether.Permissions.Viewers.Identity
	allowed := level == "all"
	if level == "granted_viewers" {
		if len(g.Viewers) == 0 {
			allowed = true // dry-run default when no explicit viewer restriction was set
		} else {
			for _, id := range g.Viewers {
				if id == ev.ViewerID {
					allowed = true
					break
				}
			}
		}
	}
	if !allowed {
		ev.ViewerID = 0
	}
	return ev
}

// dryRunHost implements wasm.Host for Studio replays. All writes and
// notifications are recorded in memory and never applied to live state.
type dryRunHost struct {
	mu            sync.Mutex
	manifest      plugins.Manifest
	grants        plugins.Grants
	secrets       map[string]string
	kv            map[string][]byte
	deps          ReplayDeps
	writes        []RecordedWrite
	notifications []RecordedNotification
	outbound      []RecordedOutbound
	logs          []RecordedLog
}

func newDryRunHost(manifest plugins.Manifest, grants plugins.Grants, secrets map[string]string, kvSeed map[string][]byte, deps ReplayDeps) *dryRunHost {
	kv := make(map[string][]byte, len(kvSeed))
	for k, v := range kvSeed {
		kv[k] = append([]byte(nil), v...)
	}
	sec := make(map[string]string, len(secrets))
	for k, v := range secrets {
		sec[k] = v
	}
	return &dryRunHost{
		manifest: manifest,
		grants:   grants,
		secrets:  sec,
		kv:       kv,
		deps:     deps,
	}
}

func (h *dryRunHost) CallHost(ctx context.Context, name string, raw json.RawMessage) (json.RawMessage, error) {
	var args []json.RawMessage
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, &wasm.Fault{Code: "bad_arguments", Message: "host arguments must be an array"}
		}
	}
	str := func(i int) string {
		var s string
		if i < len(args) {
			_ = json.Unmarshal(args[i], &s)
		}
		return s
	}
	num := func(i int) int64 {
		var n float64
		if i < len(args) {
			_ = json.Unmarshal(args[i], &n)
		}
		return int64(n)
	}
	marshalResult := func(v any) (json.RawMessage, error) {
		if v == nil {
			return json.RawMessage("null"), nil
		}
		if r, ok := v.(json.RawMessage); ok {
			return r, nil
		}
		return json.Marshal(v)
	}

	switch name {
	case "kv.get":
		h.mu.Lock()
		v, ok := h.kv[str(0)]
		h.mu.Unlock()
		if !ok {
			return json.RawMessage("null"), nil
		}
		return marshalResult(string(v))

	case "kv.put":
		key := str(0)
		var val string
		var rawVal json.RawMessage
		if len(args) > 1 {
			rawVal = args[1]
			if json.Unmarshal(args[1], &val) != nil {
				val = string(args[1])
			}
		}
		if len(rawVal) == 0 {
			rawVal, _ = json.Marshal(val)
		}
		h.mu.Lock()
		h.kv[key] = []byte(val)
		h.writes = append(h.writes, RecordedWrite{
			Op:     "kv.put",
			Target: key,
			Value:  rawVal,
		})
		h.mu.Unlock()
		return json.RawMessage("null"), nil

	case "kv.delete":
		key := str(0)
		h.mu.Lock()
		delete(h.kv, key)
		h.writes = append(h.writes, RecordedWrite{
			Op:     "kv.delete",
			Target: key,
		})
		h.mu.Unlock()
		return json.RawMessage("null"), nil

	case "kv.list":
		prefix := str(0)
		h.mu.Lock()
		var keys []string
		for k := range h.kv {
			if strings.HasPrefix(k, prefix) {
				keys = append(keys, k)
			}
		}
		h.mu.Unlock()
		sort.Strings(keys)
		if keys == nil {
			keys = []string{}
		}
		return marshalResult(keys)

	case "secret.get":
		secName := str(0)
		if !h.secretDeclared(secName) {
			return nil, wasm.Forbidden("secret not declared in manifest: " + secName)
		}
		h.mu.Lock()
		v, ok := h.secrets[secName]
		h.mu.Unlock()
		if !ok {
			v = "dry-run-secret-" + secName
		}
		return marshalResult(v)

	case "http.fetch":
		var req struct {
			Method  string            `json:"method"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
			Body    string            `json:"body"`
		}
		if len(args) > 0 {
			if err := json.Unmarshal(args[0], &req); err != nil {
				return nil, &wasm.Fault{Code: "bad_arguments", Message: "http.fetch takes {method, url, headers, body}"}
			}
		}
		if req.Method == "" {
			req.Method = http.MethodGet
		}
		u, err := url.Parse(req.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, wasm.Forbidden("invalid fetch url")
		}
		if !h.networkAllowed(u.Hostname()) {
			return nil, wasm.Forbidden("network host not declared in manifest: " + u.Hostname())
		}
		h.mu.Lock()
		h.outbound = append(h.outbound, RecordedOutbound{
			Method: req.Method,
			URL:    req.URL,
			Body:   req.Body,
		})
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			h.notifications = append(h.notifications, RecordedNotification{
				Kind:    "webhook",
				Target:  req.URL,
				Text:    req.Body,
				Payload: json.RawMessage(req.Body),
			})
		}
		h.mu.Unlock()
		return marshalResult(map[string]any{
			"status":  200,
			"headers": map[string]string{"Content-Type": "application/json"},
			"body":    `{"ok":true,"dryRun":true}`,
		})

	case "catalog.search":
		if !h.catalogAllowed() {
			return nil, wasm.Forbidden("catalog permission not declared")
		}
		return marshalResult(map[string]any{"titles": []any{}})

	case "catalog.get":
		if !h.catalogAllowed() {
			return nil, wasm.Forbidden("catalog permission not declared")
		}
		return marshalResult(map[string]any{"id": str(0)})

	case "viewers.history":
		if !h.watchHistoryAllowed() {
			return nil, wasm.Forbidden("viewers.watch_history permission not declared")
		}
		return marshalResult(map[string]any{"history": []any{}})

	case "viewers.setWatchState":
		if !h.writeWatchStateAllowed() {
			return nil, wasm.Forbidden("viewers.write_watch_state permission not declared")
		}
		var viewerID int64
		var titleID string
		var watched bool
		if len(args) == 1 {
			var obj struct {
				ViewerID int64  `json:"viewerId"`
				TitleID  string `json:"titleId"`
				Watched  bool   `json:"watched"`
			}
			if json.Unmarshal(args[0], &obj) == nil {
				viewerID, titleID, watched = obj.ViewerID, obj.TitleID, obj.Watched
			}
		} else {
			viewerID = num(0)
			titleID = str(1)
			if len(args) > 2 {
				_ = json.Unmarshal(args[2], &watched)
			}
		}
		w := watched
		valJSON, _ := json.Marshal(map[string]any{
			"viewerId": viewerID,
			"titleId":  titleID,
			"watched":  watched,
		})
		h.mu.Lock()
		h.writes = append(h.writes, RecordedWrite{
			Op:       "viewers.setWatchState",
			ViewerID: viewerID,
			Target:   titleID,
			Watched:  &w,
			Value:    valJSON,
		})
		h.mu.Unlock()
		return json.RawMessage("null"), nil

	case "collections.upsert":
		if !h.collectionsAllowed() {
			return nil, wasm.Forbidden("collections write permission not declared")
		}
		var doc json.RawMessage
		if len(args) > 0 {
			doc = args[0]
		}
		var targetKey string
		var parsed struct {
			ID    string `json:"id"`
			Key   string `json:"key"`
			Title string `json:"title"`
			Name  string `json:"name"`
		}
		if json.Unmarshal(doc, &parsed) == nil {
			switch {
			case parsed.Key != "":
				targetKey = parsed.Key
			case parsed.ID != "":
				targetKey = parsed.ID
			case parsed.Title != "":
				targetKey = parsed.Title
			default:
				targetKey = parsed.Name
			}
		}
		h.mu.Lock()
		h.writes = append(h.writes, RecordedWrite{
			Op:     "collections.upsert",
			Target: targetKey,
			Value:  doc,
		})
		h.mu.Unlock()
		return json.RawMessage("null"), nil

	case "folders.list":
		return marshalResult([]plugins.FolderEntry{})

	case "folders.stat":
		return marshalResult(plugins.FolderEntry{Name: str(1), Path: str(1)})

	case "notify.owner":
		text := str(0)
		h.mu.Lock()
		h.notifications = append(h.notifications, RecordedNotification{
			Kind: "owner_notice",
			Text: text,
		})
		h.mu.Unlock()
		return json.RawMessage("null"), nil

	case "log":
		level, msg := str(0), str(1)
		if len(args) == 1 {
			level, msg = "info", str(0)
		}
		var attrs map[string]any
		if len(args) > 2 {
			_ = json.Unmarshal(args[2], &attrs)
		}
		h.mu.Lock()
		if len(h.logs) < 100 {
			h.logs = append(h.logs, RecordedLog{Level: level, Message: msg, Attrs: attrs})
		}
		h.mu.Unlock()
		return json.RawMessage("null"), nil

	default:
		return nil, wasm.Forbidden(fmt.Sprintf("unknown host service %s", name))
	}
}

func (h *dryRunHost) secretDeclared(name string) bool {
	if h.manifest.Ether == nil {
		return false
	}
	for _, s := range h.manifest.Ether.Permissions.Secrets {
		if s == name {
			return true
		}
	}
	return false
}

func (h *dryRunHost) networkAllowed(host string) bool {
	if h.manifest.Ether == nil {
		return false
	}
	host = strings.ToLower(host)
	for _, p := range h.manifest.Ether.Permissions.Network {
		p = strings.ToLower(p)
		if strings.HasPrefix(p, "*.") {
			if strings.HasSuffix(host, p[1:]) && host != p[2:] {
				return true
			}
			continue
		}
		if p == host {
			return true
		}
	}
	return false
}

func (h *dryRunHost) catalogAllowed() bool {
	return h.manifest.Ether != nil && h.manifest.Ether.Permissions.Catalog == "read"
}

func (h *dryRunHost) watchHistoryAllowed() bool {
	if h.manifest.Ether == nil {
		return false
	}
	l := h.manifest.Ether.Permissions.Viewers.WatchHistory
	return l == "all" || l == "granted_viewers"
}

func (h *dryRunHost) writeWatchStateAllowed() bool {
	if h.manifest.Ether == nil {
		return false
	}
	l := h.manifest.Ether.Permissions.Viewers.WriteWatchState
	return l == "granted_viewers" || l == "all"
}

func (h *dryRunHost) collectionsAllowed() bool {
	return h.manifest.Ether != nil && h.manifest.Ether.Permissions.Collections == "write"
}
