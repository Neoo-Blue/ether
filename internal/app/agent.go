package app

import (
	"context"
	"net/http"

	"github.com/braedonsaunders/ether/internal/agent"
	"github.com/braedonsaunders/ether/internal/config"
	"github.com/braedonsaunders/ether/internal/llm"
)

// agentRuntime is Ask Ether: the agent loop, transcripts and approvals (WS4).
type agentRuntime struct {
	svc *agent.Service
}

func (a *App) buildAgent(ctx context.Context, cfg config.Config) (*agentRuntime, error) {
	var client *llm.Client
	if a.llm != nil && a.llm.Client() != nil {
		// Share the personalization client so applyConfig updates reach the agent.
		client = a.llm.Client()
	} else {
		client = llm.NewClient(llm.ClientOptions{Config: cfg.LLM, Logger: a.log.With("component", "llm")})
	}
	svc := agent.New(agent.Options{
		Model:     client,
		Catalogue: a.extendRT.tools,
		Store:     agent.NewStore(a.db),
		Logger:    a.log.With("component", "agent"),
	})
	return &agentRuntime{svc: svc}, nil
}

// agentHandlers mounts /api/agent (WS4).
func (a *App) agentHandlers(m map[string]http.Handler) {
	if a.agentRT == nil || a.agentRT.svc == nil {
		return
	}
	m["agent"] = a.agentRT.svc.Handler()
}
