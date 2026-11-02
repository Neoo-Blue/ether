package plugins

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type subtitlePick struct {
	plugin, title, subtitle string
	expires                 time.Time
}

// MintSubtitlePick is called only after a viewer selects a listed subtitle.
func (m *Manager) MintSubtitlePick(plugin, title, subtitle string) (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b[:])
	m.pickMu.Lock()
	defer m.pickMu.Unlock()
	if m.picks == nil {
		m.picks = make(map[string]subtitlePick)
	}
	now := m.now()
	for k, v := range m.picks {
		if !now.Before(v.expires) {
			delete(m.picks, k)
		}
	}
	m.picks[token] = subtitlePick{plugin, title, subtitle, now.Add(time.Minute)}
	return token, nil
}

func (m *Manager) consumeSubtitlePick(token, plugin, title, subtitle string) bool {
	m.pickMu.Lock()
	defer m.pickMu.Unlock()
	p, ok := m.picks[token]
	if ok {
		delete(m.picks, token)
	}
	return ok && m.now().Before(p.expires) && p.plugin == plugin && p.title == title && p.subtitle == subtitle
}

// FetchPickedSubtitle consumes the same one-shot authorization as a plugin
// download call. Every redirect is checked against the declared hosts.
func (m *Manager) FetchPickedSubtitle(ctx context.Context, plugin, title, subtitle, token, rawURL string) ([]byte, error) {
	if !m.consumeSubtitlePick(token, plugin, title, subtitle) {
		return nil, ErrPermissionDenied
	}
	r, g, err := (&host{m: m, plugin: plugin}).record(ctx)
	if err != nil {
		return nil, err
	}
	if r.Manifest.Ether == nil || g.revoked("network") {
		return nil, ErrPermissionDenied
	}
	allowed := func(u *url.URL) bool {
		return (u.Scheme == "http" || u.Scheme == "https") && u.User == nil && hostAllowed(r.Manifest.Ether.Permissions.Network, u.Hostname())
	}
	u, err := url.Parse(rawURL)
	if err != nil || !allowed(u) {
		return nil, ErrPermissionDenied
	}
	client := *m.client
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 || !allowed(req.URL) {
			return ErrPermissionDenied
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("subtitle download HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 2<<20 {
		return nil, fmt.Errorf("subtitle download exceeds 2 MiB")
	}
	return b, nil
}
