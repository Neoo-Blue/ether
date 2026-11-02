package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/braedonsaunders/ether/internal/config"
)

func fixtureServer(t *testing.T, fixture string, wantSubstr ...string) *httptest.Server {
	t.Helper()
	body, err := os.ReadFile("testdata/" + fixture)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		for _, s := range wantSubstr {
			if !strings.Contains(string(raw), s) {
				t.Errorf("request to %s missing %q: %s", r.URL.Path, s, raw)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(body)
	}))
}

func collect(t *testing.T, ch <-chan Event) []Event {
	t.Helper()
	var evs []Event
	for ev := range ch {
		evs = append(evs, ev)
		if ev.Kind == EventError {
			t.Fatalf("stream error: %v", ev.Err)
		}
	}
	return evs
}

func assertToolStream(t *testing.T, evs []Event) {
	t.Helper()
	var text string
	var call *Event
	usage := false
	for i := range evs {
		switch evs[i].Kind {
		case EventTextDelta:
			text += evs[i].Text
		case EventToolCall:
			call = &evs[i]
		case EventUsage:
			if evs[i].InputTokens > 0 {
				usage = true
			}
		}
	}
	if text != "Checking. " {
		t.Errorf("text = %q", text)
	}
	if call == nil || call.ToolName != "server_status" {
		t.Fatalf("no server_status call in %+v", evs)
	}
	var in map[string]any
	if json.Unmarshal(call.Input, &in) != nil || in["verbose"] != true {
		t.Errorf("call input = %s", call.Input)
	}
	if !usage {
		t.Errorf("no usage event")
	}
	if len(evs) == 0 || evs[len(evs)-1].Kind != EventDone {
		t.Errorf("last event not done: %+v", evs)
	}
}

func toolReq() ToolRequest {
	return ToolRequest{
		System:      "You are Ask Ether.",
		Temperature: 0.3,
		Messages:    []StreamMessage{{Role: "user", Content: "status?"}},
		Tools: []ToolDef{{Name: "server_status", Description: "Server status",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"verbose":{"type":"boolean"}}}`)}},
	}
}

func TestStreamToolsOpenAI(t *testing.T) {
	srv := fixtureServer(t, "openai_tools.sse", `"tools"`, `"server_status"`, `"stream":true`)
	defer srv.Close()
	c := NewClient(ClientOptions{Config: config.LLM{Enabled: true, Provider: "custom", BaseURL: srv.URL, Model: "gpt-x"}, HTTPClient: srv.Client()})
	ch, err := c.StreamTools(context.Background(), toolReq())
	if err != nil {
		t.Fatal(err)
	}
	assertToolStream(t, collect(t, ch))
}

func TestStreamToolsAnthropic(t *testing.T) {
	srv := fixtureServer(t, "anthropic_tools.sse", `"input_schema"`, `"server_status"`)
	defer srv.Close()
	cfg := config.LLM{Enabled: true, Provider: "anthropic", Model: "claude-x", APIKey: "k"}
	// Point the provider base at the test server via custom transport rewrite.
	c := NewClient(ClientOptions{Config: cfg, HTTPClient: rewriteClient(srv)})
	ch, err := c.StreamTools(context.Background(), toolReq())
	if err != nil {
		t.Fatal(err)
	}
	assertToolStream(t, collect(t, ch))
}

func TestStreamToolsGemini(t *testing.T) {
	srv := fixtureServer(t, "gemini_tools.sse", `"functionDeclarations"`, `"server_status"`)
	defer srv.Close()
	c := NewClient(ClientOptions{Config: config.LLM{Enabled: true, Provider: "google", Model: "gemini-x", APIKey: "k"}, HTTPClient: rewriteClient(srv)})
	ch, err := c.StreamTools(context.Background(), toolReq())
	if err != nil {
		t.Fatal(err)
	}
	assertToolStream(t, collect(t, ch))
}

// rewriteClient rewrites every request to the test server, keeping path.
func rewriteClient(srv *httptest.Server) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme = "http"
		r.URL.Host = strings.TrimPrefix(srv.URL, "http://")
		return http.DefaultTransport.RoundTrip(r)
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestStreamToolsRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"tools are not supported by this model"}}`))
	}))
	defer srv.Close()
	c := NewClient(ClientOptions{Config: config.LLM{Enabled: true, Provider: "custom", BaseURL: srv.URL, Model: "gpt-x"}, HTTPClient: srv.Client()})
	ch, err := c.StreamTools(context.Background(), toolReq())
	if err != nil {
		t.Fatal(err)
	}
	var gotErr error
	for ev := range ch {
		if ev.Kind == EventError {
			gotErr = ev.Err
		}
	}
	if !errors.Is(gotErr, ErrToolsUnsupported) {
		t.Fatalf("want ErrToolsUnsupported, got %v", gotErr)
	}
}

func TestStreamOpenAIRetriesWithoutStreamOptions(t *testing.T) {
	fixture, err := os.ReadFile("testdata/openai_tools.sse")
	if err != nil {
		t.Fatal(err)
	}
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(string(raw), "stream_options") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"unknown field stream_options"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()
	c := NewClient(ClientOptions{Config: config.LLM{Enabled: true, Provider: "custom", BaseURL: srv.URL, Model: "gpt-x"}, HTTPClient: srv.Client()})
	ch, err := c.StreamTools(context.Background(), toolReq())
	if err != nil {
		t.Fatal(err)
	}
	assertToolStream(t, collect(t, ch))
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
}

func TestToolsSupported(t *testing.T) {
	c := NewClient(ClientOptions{Config: config.LLM{}})
	if ok, _ := c.ToolsSupported(); ok {
		t.Error("disabled llm reported tools supported")
	}
	c = NewClient(ClientOptions{Config: config.LLM{Enabled: true, Provider: "custom", BaseURL: "http://x/v1", Model: "text-embedding-3"}})
	if ok, reason := c.ToolsSupported(); ok || reason == "" {
		t.Error("embedding model reported tools supported")
	}
}
