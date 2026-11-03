package adapters

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/braedonsaunders/ether/internal/core"
	"github.com/braedonsaunders/ether/internal/plugins"
	"github.com/braedonsaunders/ether/internal/subtitles"
)

type probeCatalog interface {
	CachedGet(context.Context, core.RatingKey) (*core.Title, error)
}
type subtitleProbeSession struct {
	svc         *subtitles.Service
	title       core.Title
	plugin, dir string
	expires     time.Time
}

// SubtitleProbe exercises the ordinary subtitle broker with one installed
// provider. Offers and explicit picks are isolated from viewer selections and
// production sidecars. Each session is single-use, bounded and cleaned up.
type SubtitleProbe struct {
	mgr      *plugins.Manager
	catalog  probeCatalog
	mu       sync.Mutex
	sessions map[string]*subtitleProbeSession
	slots    chan struct{}
}

func NewSubtitleProbe(mgr *plugins.Manager, catalog probeCatalog) *SubtitleProbe {
	return &SubtitleProbe{mgr: mgr, catalog: catalog, sessions: map[string]*subtitleProbeSession{}, slots: make(chan struct{}, 2)}
}
func (p *SubtitleProbe) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, s := range p.sessions {
		_ = os.RemoveAll(s.dir)
		delete(p.sessions, id)
	}
}
func (p *SubtitleProbe) take(id string) *subtitleProbeSession {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.sessions[id]
	delete(p.sessions, id)
	if s != nil && time.Now().After(s.expires) {
		_ = os.RemoveAll(s.dir)
		return nil
	}
	return s
}
func (p *SubtitleProbe) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /subtitles/search", p.search)
	mux.HandleFunc("POST /subtitles/{session}/pick", p.pick)
	mux.HandleFunc("DELETE /subtitles/{session}", func(w http.ResponseWriter, r *http.Request) {
		if s := p.take(r.PathValue("session")); s != nil {
			_ = os.RemoveAll(s.dir)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}
func probeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func probeError(w http.ResponseWriter, status int, err error) {
	probeJSON(w, status, map[string]any{"error": map[string]string{"code": "subtitle_probe", "message": err.Error()}})
}
func (p *SubtitleProbe) enter(w http.ResponseWriter) bool {
	select {
	case p.slots <- struct{}{}:
		return true
	default:
		probeError(w, http.StatusTooManyRequests, errors.New("Two provider checks are already running. Try again shortly."))
		return false
	}
}
func (p *SubtitleProbe) search(w http.ResponseWriter, r *http.Request) {
	if !p.enter(w) {
		return
	}
	defer func() { <-p.slots }()
	var body struct {
		Plugin   string `json:"plugin"`
		Key      string `json:"key"`
		Language string `json:"language"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body); err != nil {
		probeError(w, 400, err)
		return
	}
	key, err := strconv.ParseInt(body.Key, 10, 64)
	if err != nil || key <= 0 {
		probeError(w, 400, errors.New("Choose a stored catalogue title."))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	rec, err := p.mgr.Get(ctx, body.Plugin)
	if err != nil || !rec.Enabled || !rec.Manifest.HasCapability(plugins.CapSubtitles) {
		probeError(w, 400, errors.New("This subtitle provider is not enabled."))
		return
	}
	title, err := p.catalog.CachedGet(ctx, core.RatingKey(key))
	if err != nil {
		probeError(w, 404, errors.New("This title is not available in the local catalogue."))
		return
	}
	// Reserve before any upstream work. Expired checks have no downloaded files;
	// downloads consume and clean their session even when validation fails.
	p.mu.Lock()
	for id, s := range p.sessions {
		if time.Now().After(s.expires) {
			_ = os.RemoveAll(s.dir)
			delete(p.sessions, id)
		}
	}
	if len(p.sessions) >= 8 {
		p.mu.Unlock()
		probeError(w, 429, errors.New("Close an existing provider check before starting another."))
		return
	}
	dir, err := os.MkdirTemp("", "ether-subtitle-probe-")
	if err != nil {
		p.mu.Unlock()
		probeError(w, 500, err)
		return
	}
	var bytes [24]byte
	if _, err = rand.Read(bytes[:]); err != nil {
		p.mu.Unlock()
		_ = os.RemoveAll(dir)
		probeError(w, 500, err)
		return
	}
	id := hex.EncodeToString(bytes[:])
	svc := subtitles.New(subtitles.Options{CacheDir: dir})
	svc.RegisterPluginProvider(&subtitleProvider{p.mgr, rec.ID, rec.Name})
	session := &subtitleProbeSession{svc: svc, title: *title, plugin: rec.ID, dir: dir, expires: time.Now().Add(10 * time.Minute)}
	p.sessions[id] = session
	p.mu.Unlock()
	offers, err := svc.SearchOnDemand(ctx, title, body.Language, "")
	if err != nil {
		p.take(id)
		_ = os.RemoveAll(dir)
		probeError(w, 502, err)
		return
	}
	type offer struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Language string `json:"language"`
		Provider string `json:"provider"`
	}
	result := make([]offer, 0, len(offers))
	for _, c := range offers {
		result = append(result, offer{strconv.FormatInt(c.FileID, 10), c.FileName, c.Language, c.Provider})
	}
	probeJSON(w, 200, map[string]any{"session": id, "title": title.Title, "offers": result, "expiresAt": session.expires, "downloaded": false})
}
func (p *SubtitleProbe) pick(w http.ResponseWriter, r *http.Request) {
	if !p.enter(w) {
		return
	}
	defer func() { <-p.slots }()
	var body struct {
		Offer string `json:"offer"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body); err != nil {
		probeError(w, 400, err)
		return
	}
	id, err := strconv.ParseInt(body.Offer, 10, 64)
	if err != nil {
		probeError(w, 400, errors.New("Choose an offered subtitle."))
		return
	}
	s := p.take(r.PathValue("session"))
	if s == nil {
		probeError(w, 404, errors.New("This check has expired. Search again."))
		return
	}
	defer os.RemoveAll(s.dir)
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	rec, err := p.mgr.Get(ctx, s.plugin)
	if err != nil || !rec.Enabled {
		probeError(w, 400, errors.New("This subtitle provider is no longer enabled."))
		return
	}
	info, err := s.svc.DownloadOnDemand(ctx, &s.title, id)
	if err != nil {
		probeError(w, 502, err)
		return
	}
	reader, format, err := s.svc.Open(strings.TrimPrefix(info.ExternalURL, "ether-subtitle:"))
	if err != nil {
		probeError(w, 502, err)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(reader, 2<<20))
	_ = reader.Close()
	if err != nil {
		probeError(w, 502, err)
		return
	}
	cues, err := subtitles.ParseCues(string(raw))
	if err != nil || len(cues) == 0 {
		probeError(w, 502, fmt.Errorf("Downloaded subtitle has no readable timed cues: %v", err))
		return
	}
	if err = os.RemoveAll(s.dir); err != nil {
		probeError(w, 500, errors.New("The check finished but temporary file cleanup failed."))
		return
	}
	probeJSON(w, 200, map[string]any{"bytes": len(raw), "format": format, "cues": len(cues), "cleaned": true, "playbackVerified": false})
}
