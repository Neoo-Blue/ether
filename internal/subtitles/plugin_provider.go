package subtitles

import (
	"context"
	"github.com/braedonsaunders/ether/internal/core"
)

// PluginProvider is the viewer-driven plugin subtitle bridge.
type PluginProvider interface {
	ID() string
	Search(context.Context, *core.Title, string, string, int64) ([]Candidate, error)
	Pick(context.Context, *core.Title, Candidate) (context.Context, error)
	Download(context.Context, Candidate) ([]byte, error)
}

func (s *Service) RegisterPluginProvider(p PluginProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pluginProviders == nil {
		s.pluginProviders = make(map[string]PluginProvider)
	}
	s.pluginProviders[p.ID()] = p
}
func (s *Service) UnregisterPluginProvider(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pluginProviders, id)
}
