package etherplugin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/coder/websocket"
)

const (
	// TunnelProtocol is the WebSocket subprotocol for Ether remote plugin tunnels.
	TunnelProtocol = "ether-tunnel-v1"
	// DefaultMaxChunkBytes is the default chunk size for streamed file responses.
	DefaultMaxChunkBytes = 64 * 1024
)

// TunnelError carries an error payload inside a TunnelFrame.
type TunnelError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// TunnelFrame is one JSON message sent or received over the `ether-tunnel-v1` WebSocket.
type TunnelFrame struct {
	Type          string              `json:"type"`
	ID            string              `json:"id,omitempty"`
	RequestID     string              `json:"requestId,omitempty"`
	Protocol      string              `json:"protocol,omitempty"`
	Plugin        string              `json:"plugin,omitempty"`
	PluginID      string              `json:"pluginId,omitempty"`
	ConnectionID  string              `json:"connectionId,omitempty"`
	Features      []string            `json:"features,omitempty"`
	MaxChunkBytes int                 `json:"maxChunkBytes,omitempty"`
	Method        string              `json:"method,omitempty"`
	Path          string              `json:"path,omitempty"`
	Query         map[string][]string `json:"query,omitempty"`
	Headers       map[string][]string `json:"headers,omitempty"`
	Body          string              `json:"body,omitempty"`
	Data          string              `json:"data,omitempty"`
	Status        int                 `json:"status,omitempty"`
	Deadline      string              `json:"deadline,omitempty"`
	Error         *TunnelError        `json:"error,omitempty"`
}

// DispatchTunnelFrame executes a `"request"` TunnelFrame against a Plugin and
// returns the outgoing response frames (either a single `"response"` frame or a
// `"response_start"` / `"response_chunk"` / `"response_end"` sequence for
// `/ether/file/*` binary responses).
func (p *Plugin) DispatchTunnelFrame(ctx context.Context, frame TunnelFrame, maxChunkBytes int) []TunnelFrame {
	if frame.Type != "request" || frame.ID == "" {
		return nil
	}
	if maxChunkBytes <= 0 {
		maxChunkBytes = DefaultMaxChunkBytes
	}

	var decodedBody []byte
	if frame.Body != "" {
		b, err := base64.StdEncoding.DecodeString(frame.Body)
		if err != nil {
			return []TunnelFrame{{
				Type:  "error",
				ID:    frame.ID,
				Error: &TunnelError{Code: "bad_request", Message: "Invalid base64 request body."},
			}}
		}
		decodedBody = b
	}

	hdrs := map[string]string{}
	for k, vals := range frame.Headers {
		if len(vals) > 0 {
			hdrs[k] = vals[0]
		}
	}
	if frame.RequestID != "" && hdrs["X-Ether-Request-ID"] == "" {
		hdrs["X-Ether-Request-ID"] = frame.RequestID
	}
	query := map[string]string{}
	for k, vals := range frame.Query {
		if len(vals) > 0 {
			query[k] = vals[0]
		}
	}

	res := p.Dispatch(ctx, RawRequest{
		Method:  frame.Method,
		Path:    frame.Path,
		Query:   query,
		Headers: hdrs,
		Body:    decodedBody,
	})

	multiHeaders := map[string][]string{}
	for k, v := range res.Headers {
		multiHeaders[k] = []string{v}
	}

	if res.BinaryBody != nil && strings.HasPrefix(frame.Path, "/ether/file/") {
		out := []TunnelFrame{{
			Type:    "response_start",
			ID:      frame.ID,
			Status:  res.Status,
			Headers: multiHeaders,
		}}
		limit := maxChunkBytes
		if limit < 1024 {
			limit = 1024
		}
		for offset := 0; offset < len(res.BinaryBody); offset += limit {
			end := offset + limit
			if end > len(res.BinaryBody) {
				end = len(res.BinaryBody)
			}
			out = append(out, TunnelFrame{
				Type: "response_chunk",
				ID:   frame.ID,
				Data: base64.StdEncoding.EncodeToString(res.BinaryBody[offset:end]),
			})
		}
		out = append(out, TunnelFrame{
			Type: "response_end",
			ID:   frame.ID,
		})
		return out
	}

	payload := res.Body
	if res.BinaryBody != nil {
		payload = res.BinaryBody
	}
	var encodedBody string
	if len(payload) > 0 {
		encodedBody = base64.StdEncoding.EncodeToString(payload)
	}
	return []TunnelFrame{{
		Type:    "response",
		ID:      frame.ID,
		Status:  res.Status,
		Headers: multiHeaders,
		Body:    encodedBody,
	}}
}

func (p *Plugin) startTunnel(ctx context.Context, tunnelURL, credential string) (*ServeHandle, error) {
	if tunnelURL == "" {
		return nil, fmt.Errorf("tunnel mode requires TunnelURL or ETHER_TUNNEL_URL")
	}
	dialOpts := &websocket.DialOptions{
		Subprotocols: []string{TunnelProtocol},
		HTTPHeader:   http.Header{},
	}
	if credential != "" {
		dialOpts.HTTPHeader.Set("X-Ether-Plugin-Credential", credential)
	}
	conn, _, err := websocket.Dial(ctx, tunnelURL, dialOpts)
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(16 << 20)

	hello := TunnelFrame{
		Type:     "hello",
		Protocol: TunnelProtocol,
		Plugin:   p.manifest.ID,
		Features: []string{"range", "stream"},
	}
	helloBytes, _ := json.Marshal(hello)
	if err := conn.Write(ctx, websocket.MessageText, helloBytes); err != nil {
		_ = conn.Close(websocket.StatusInternalError, "failed to send hello")
		return nil, err
	}

	_, welcomeBytes, err := conn.Read(ctx)
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, "failed to read welcome")
		return nil, err
	}
	var welcome TunnelFrame
	if err := json.Unmarshal(welcomeBytes, &welcome); err != nil || welcome.Type != "welcome" {
		_ = conn.Close(websocket.StatusPolicyViolation, "expected welcome frame")
		return nil, fmt.Errorf("unexpected tunnel handshake response: %s", string(welcomeBytes))
	}
	maxChunk := welcome.MaxChunkBytes
	if maxChunk <= 0 {
		maxChunk = DefaultMaxChunkBytes
	}

	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	var mu sync.Mutex
	active := make(map[string]context.CancelFunc)
	go func() {
		defer close(done)
		defer cancel()
		defer func() {
			mu.Lock()
			defer mu.Unlock()
			for _, stop := range active {
				stop()
			}
		}()
		for {
			_, msgBytes, err := conn.Read(loopCtx)
			if err != nil {
				return
			}
			var frame TunnelFrame
			if json.Unmarshal(msgBytes, &frame) != nil {
				continue
			}
			if frame.Type == "cancel" {
				mu.Lock()
				stop := active[frame.ID]
				mu.Unlock()
				if stop != nil {
					stop()
				}
				continue
			}
			if frame.Type != "request" {
				continue
			}
			requestCtx, stop := context.WithCancel(loopCtx)
			mu.Lock()
			active[frame.ID] = stop
			mu.Unlock()
			go func(frame TunnelFrame) {
				defer func() { mu.Lock(); delete(active, frame.ID); mu.Unlock(); stop() }()
				for _, reply := range p.DispatchTunnelFrame(requestCtx, frame, maxChunk) {
					if requestCtx.Err() != nil {
						return
					}
					b, err := json.Marshal(reply)
					if err != nil {
						return
					}
					if err := conn.Write(requestCtx, websocket.MessageText, b); err != nil {
						return
					}
				}
			}(frame)
		}
	}()

	return &ServeHandle{
		Done: done,
		Mode: "tunnel",
		URL:  tunnelURL,
		Close: func() error {
			cancel()
			return conn.Close(websocket.StatusNormalClosure, "shutting down")
		},
	}, nil
}
