// Package support connects authenticated frontend requests to the owner's
// bounded diagnostics recorder. Bodies are sampled only during a capture.
package support

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/braedonsaunders/ether/internal/diagnostics"
	"github.com/braedonsaunders/ether/internal/frontend"
	"github.com/braedonsaunders/ether/internal/store"
	"github.com/go-chi/chi/v5/middleware"
)

type subject struct {
	user   *frontend.Viewer
	device frontend.Device
}
type bag struct {
	mu    sync.Mutex
	value subject
}
type bagKey struct{}

// Sighting feeds the owner device picker without retaining request data.
func Sighting(rec *diagnostics.Recorder, protocol string, device frontend.Device) {
	if rec == nil || device.ID == "" {
		return
	}
	rec.Observe(diagnostics.Event{Kind: diagnostics.KindDevice, DeviceKey: protocol + ":" + device.ID, Product: device.Product, Platform: device.Platform, DeviceName: device.DeviceName, AppVersion: device.Version})
}

// Seen is called only after the adapter has authenticated the viewer and
// checked its device binding. Raw credentials never cross this interface.
func Seen(r *http.Request, rec *diagnostics.Recorder, protocol string, user *frontend.Viewer, device frontend.Device) {
	if rec == nil || user == nil || device.ID == "" {
		return
	}
	Sighting(rec, protocol, device)
	if b, ok := r.Context().Value(bagKey{}).(*bag); ok {
		b.mu.Lock()
		b.value = subject{user: user, device: device}
		b.mu.Unlock()
	}
}

type sample struct {
	buf       bytes.Buffer
	truncated bool
}

func (s *sample) Write(p []byte) (int, error) {
	const limit = 1 << 20
	available := limit - s.buf.Len()
	if available < len(p) {
		s.truncated = true
	}
	if available > 0 {
		n := len(p)
		if n > available {
			n = available
		}
		_, _ = s.buf.Write(p[:n])
	}
	return len(p), nil
}

// Wrap leaves the writer untouched outside an active recording. Authentication
// and the device binding are checked before a response can be sampled.
func Wrap(rec *diagnostics.Recorder, protocol string, authenticate func(*http.Request) (*frontend.Viewer, frontend.Device), safePath func(string) string, next http.Handler) http.Handler {
	if rec == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rec.Recording() {
			next.ServeHTTP(w, r)
			return
		}
		b := &bag{}
		r = r.WithContext(context.WithValue(r.Context(), bagKey{}, b))
		viewer, d := authenticate(r)
		if viewer != nil {
			Seen(r, rec, protocol, viewer, d)
		}
		sampleBody := viewer != nil && d.ID != "" && sampleable(protocol, r) && rec.BeginSample(protocol+":"+d.ID)
		if sampleBody {
			defer rec.EndSample()
		}
		sw := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		var data sample
		if sampleBody {
			sw.Tee(&data)
		}
		start := time.Now()
		next.ServeHTTP(sw, r)
		b.mu.Lock()
		subject := b.value
		b.mu.Unlock()
		if subject.user == nil || subject.device.ID == "" {
			return
		}
		ev := diagnostics.Event{Time: time.Now(), Kind: diagnostics.KindRequest, Protocol: protocol, DeviceKey: protocol + ":" + subject.device.ID, ViewerID: subject.user.ID, Username: subject.user.Username, Product: subject.device.Product, Platform: subject.device.Platform, DeviceName: subject.device.DeviceName, Device: subject.device.Client, AppVersion: subject.device.Version, Method: r.Method, Route: safePath(r.URL.Path), Query: diagnostics.SafeQuery(r.URL.Query()), Status: sw.Status(), Bytes: int64(sw.BytesWritten()), Duration: time.Since(start), Cancelled: r.Context().Err() != nil}
		if ev.Status == 0 {
			ev.Status = http.StatusOK
		}
		if stats := store.QueryStatsFrom(r.Context()); stats != nil {
			ev.DBQueries = stats.Queries()
			ev.DBTime = stats.DBTime()
		}
		// A response sample is usable only when its pre-auth claim was verified.
		if sampleBody && d.ID == subject.device.ID && viewer != nil && viewer.ID == subject.user.ID {
			ev.Response = summarize(data.buf.Bytes(), data.truncated, sw.Header().Get("Content-Encoding"))
		}
		rec.Observe(ev)
	})
}

func sampleable(protocol string, r *http.Request) bool {
	p := strings.ToLower(r.URL.Path)
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		return false
	}
	for _, skip := range []string{"/images/", "/image/", "/videos/", "/audio/", "/sessions/", "/download"} {
		if strings.Contains(p, skip) {
			return false
		}
	}
	if protocol != "stremio" && strings.Contains(p, "/stream/") {
		return false
	}
	return true
}

func summarize(raw []byte, truncated bool, encoding string) *diagnostics.ResponseSummary {
	if truncated {
		return &diagnostics.ResponseSummary{Truncated: true}
	}
	if strings.EqualFold(strings.TrimSpace(encoding), "gzip") {
		gz, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return &diagnostics.ResponseSummary{Truncated: true}
		}
		defer gz.Close()
		decoded, err := io.ReadAll(io.LimitReader(gz, (1<<20)+1))
		if err != nil || len(decoded) > 1<<20 {
			return &diagnostics.ResponseSummary{Truncated: true}
		}
		raw = decoded
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(raw, &doc) != nil {
		switch strings.TrimSpace(string(raw)) {
		case "Playback unavailable", "Selected track unavailable", "Track source unavailable":
			return &diagnostics.ResponseSummary{Decision: "failed", ErrorCode: strings.TrimSpace(string(raw))}
		}
		var list []json.RawMessage
		if json.Unmarshal(raw, &list) != nil {
			return nil
		}
		doc = map[string]json.RawMessage{"items": raw}
	}
	out := &diagnostics.ResponseSummary{}
	matched := false
	if raw, ok := doc["ErrorCode"]; ok {
		var code string
		if json.Unmarshal(raw, &code) == nil && code == "NoCompatibleStream" {
			out.ErrorCode = code
			matched = true
		}
	}
	if raw, ok := doc["detail"]; ok {
		var detail string
		if json.Unmarshal(raw, &detail) == nil && len(detail) <= 512 {
			out.Error = detail
			matched = true
		}
	}
	if raw, ok := doc["MediaSources"]; ok {
		var sources []struct {
			DirectPlay   bool `json:"SupportsDirectPlay"`
			DirectStream bool `json:"SupportsDirectStream"`
			Transcode    bool `json:"SupportsTranscoding"`
		}
		if json.Unmarshal(raw, &sources) == nil {
			matched = true
			out.Size = len(sources)
			out.ItemCount = len(sources)
			if len(sources) > 0 {
				switch {
				case sources[0].DirectPlay:
					out.Decision = "direct play offered"
				case sources[0].DirectStream:
					out.Decision = "direct stream offered"
				case sources[0].Transcode:
					out.Decision = "transcode offered"
				}
			}
		}
	}
	if raw, ok := doc["playback_plan"]; ok {
		var plan struct {
			Delivery string `json:"delivery"`
		}
		if json.Unmarshal(raw, &plan) == nil {
			switch plan.Delivery {
			case "original_http", "server_transcode_hls", "server_remux_hls":
				out.Decision = plan.Delivery
				matched = true
			}
		}
	}
	var list []json.RawMessage
	for _, key := range []string{"Items", "items", "metas", "streams", "results", "playlists"} {
		if v, ok := doc[key]; ok && json.Unmarshal(v, &list) == nil {
			break
		}
	}
	if list == nil {
		if matched {
			return out
		}
		return nil
	}
	out.Size, out.ItemCount = len(list), len(list)
	if raw, ok := doc["page"]; ok {
		var page struct {
			HasMore *bool `json:"has_more"`
		}
		if json.Unmarshal(raw, &page) == nil {
			out.More = page.HasMore
		}
	}
	for _, key := range []string{"TotalRecordCount", "totalRecordCount", "total"} {
		if raw, ok := doc[key]; ok {
			var n int
			if json.Unmarshal(raw, &n) == nil {
				out.TotalSize = &n
				break
			}
		}
	}
	for _, raw := range list {
		var item struct {
			Name  string            `json:"Name"`
			Title string            `json:"title"`
			ID    string            `json:"id"`
			Type  string            `json:"section_type"`
			Items []json.RawMessage `json:"items"`
		}
		if json.Unmarshal(raw, &item) == nil {
			title := item.Name
			if title == "" {
				title = item.Title
			}
			if title != "" {
				out.Items = append(out.Items, title)
			}
			if item.Items != nil && len(out.Hubs) < 64 {
				hub := diagnostics.HubSummary{Identifier: item.ID, Title: title, Type: item.Type, Size: len(item.Items), ItemCount: len(item.Items)}
				for _, child := range item.Items {
					var c struct {
						Name  string `json:"name"`
						Title string `json:"title"`
					}
					if json.Unmarshal(child, &c) == nil {
						if c.Title != "" {
							hub.Items = append(hub.Items, c.Title)
						} else if c.Name != "" {
							hub.Items = append(hub.Items, c.Name)
						}
					}
					if len(hub.Items) >= 64 {
						break
					}
				}
				out.Hubs = append(out.Hubs, hub)
			}
		}
		if len(out.Items) >= 64 {
			if len(list) > 64 {
				out.Truncated = true
			}
			break
		}
	}
	return out
}
