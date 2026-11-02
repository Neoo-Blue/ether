package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/braedonsaunders/ether/internal/egress"
	"github.com/braedonsaunders/ether/internal/safefetch"
)

// DefaultMaxResponseBytes caps a buffered Call reply (SPEC §5.1.5 sets
// 10 MiB for host fetches; plugin replies get the same default).
const DefaultMaxResponseBytes = 10 << 20

// DefaultCallTimeout bounds a call whose request and context carry
// no deadline. Route deadlines in SPEC §5.1.2 are shorter; callers
// pass them per request.
const DefaultCallTimeout = 30 * time.Second

// URLClient talks to a remote plugin at a base URL (SPEC §5.3 URL
// mode). Every fetch goes through safefetch: public addresses only,
// unless TrustedOrigin names exactly this client's origin as
// owner-confirmed (LAN URL mode). Explicit proxy routing follows egress
// CapPlugins; process environment proxies are not used.
type URLClient struct {
	base       *url.URL
	httpClient *http.Client
	credential string
	locale     string
	trusted    bool
	maxBytes   int64
	maxStream  int64
	timeout    time.Duration
	log        *slog.Logger

	mu     sync.Mutex
	closed bool
}

// URLOptions configures a URLClient.
type URLOptions struct {
	// Base is the plugin base URL, usually the manifest URL's
	// directory (for example https://addons.example/usercfg). Request
	// paths are appended to it. A trailing "/manifest.json" is
	// stripped so a manifest URL may be passed directly.
	Base string
	// Credential is sent as X-Ether-Plugin-Credential on every call.
	// Plain Stremio add-ons have none.
	Credential string
	// Locale is sent as X-Ether-Locale unless a request overrides it.
	Locale string
	// TrustedOrigin allows this exact origin to resolve to private
	// addresses (owner-confirmed LAN URL mode). It must name the same
	// origin as Base; anything else is refused at construction so a
	// client can never be built trusted for one host and aimed at
	// another.
	TrustedOrigin string
	// Guard guards fetches; nil uses a zero safefetch.Guard. Tests
	// inject one with fake DNS.
	Guard *safefetch.Guard
	// BaseTransport is the transport the guard clones. It must be a
	// *http.Transport (the guard refuses anything else). Nil uses
	// egress.Transport(egress.CapPlugins).
	BaseTransport http.RoundTripper
	// MaxResponseBytes caps buffered replies; zero uses the default,
	// negative means the default too (a cap is always enforced).
	MaxResponseBytes int64
	// MaxStreamBytes caps streamed replies; zero means no cap beyond
	// the caller's context (range-relayed media files are large).
	MaxStreamBytes int64
	// Timeout is the default per-call deadline; zero uses
	// DefaultCallTimeout.
	Timeout time.Duration
	Logger  *slog.Logger
}

// NewURLClient builds a URL-mode runtime for one plugin.
func NewURLClient(opts URLOptions) (*URLClient, error) {
	base, err := parseBase(opts.Base)
	if err != nil {
		return nil, err
	}
	trusted := false
	if opts.TrustedOrigin != "" {
		if !safefetch.SameOrigin(opts.TrustedOrigin, base.String()) {
			return nil, fmt.Errorf("%w: trusted origin does not match base", ErrInvalid)
		}
		trusted = true
	}
	transport := opts.BaseTransport
	if transport == nil {
		tr := egress.Transport(egress.CapPlugins)
		tr.Proxy = func(r *http.Request) (*url.URL, error) {
			policy := egress.Current()
			if !policy.Enabled {
				return nil, nil
			}
			return policy.SnapshotClient(egress.CapPlugins, 0).Transport.(*http.Transport).Proxy(r)
		}
		transport = tr
	}
	guard := opts.Guard
	if guard == nil {
		guard = &safefetch.Guard{}
	}
	maxBytes := opts.MaxResponseBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxResponseBytes
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultCallTimeout
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &URLClient{
		base: base,
		// No client-level Timeout: deadlines come from the call
		// context so streamed bodies are not cut at a second,
		// invisible limit.
		httpClient: guard.Client(&http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if !safefetch.SameOrigin(base.String(), req.URL.String()) {
				return fmt.Errorf("%w: cross-origin plugin redirect", safefetch.ErrBlocked)
			}
			return nil
		}}),
		credential: opts.Credential,
		locale:     opts.Locale,
		trusted:    trusted,
		maxBytes:   maxBytes,
		maxStream:  opts.MaxStreamBytes,
		timeout:    timeout,
		log:        log,
	}, nil
}

// Base returns the client's plugin base URL.
func (c *URLClient) Base() string { return c.base.String() }

func parseBase(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: malformed base URL", ErrInvalid)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, fmt.Errorf("%w: base is not an http(s) URL with a host", ErrInvalid)
	}
	if u.User != nil {
		return nil, fmt.Errorf("%w: base URL must not carry credentials", ErrInvalid)
	}
	// A manifest URL names the base by its directory.
	u.Path = strings.TrimSuffix(u.Path, "/manifest.json")
	u.Path = strings.TrimSuffix(u.Path, "/")
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u, nil
}

// urlFor joins a plugin route onto the base.
func (c *URLClient) urlFor(req Request) (string, error) {
	if err := validatePath(req.Path); err != nil {
		return "", err
	}
	u := *c.base
	u.Path = c.base.Path + req.Path
	u.RawPath = ""
	if len(req.Query) > 0 {
		u.RawQuery = cloneQuery(req.Query).Encode()
	}
	return u.String(), nil
}

func (c *URLClient) checkOpen() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	return nil
}

// do builds and sends one HTTP request.
func (c *URLClient) do(ctx context.Context, req Request) (*http.Response, context.CancelFunc, error) {
	if err := c.checkOpen(); err != nil {
		return nil, nil, err
	}
	if req.Path == "/ether/configure" && c.base.Scheme != "https" {
		return nil, nil, fmt.Errorf("%w: plugin configuration requires TLS", ErrInvalid)
	}
	target, err := c.urlFor(req)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel, deadline := deadlineFor(ctx, req, c.timeout)
	if c.trusted {
		ctx = safefetch.WithTrustedOriginFor(ctx, c.base.String(), target)
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	var body io.Reader
	if len(req.Body) > 0 {
		body = bytes.NewReader(req.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	requestID := req.RequestID
	if requestID == "" {
		requestID = newRequestID()
	}
	locale := req.Locale
	if locale == "" {
		locale = c.locale
	}
	for k, vs := range etherHeaders(req, requestID, locale, c.credential, deadline) {
		httpReq.Header.Del(k)
		for _, v := range vs {
			httpReq.Header.Add(k, v)
		}
	}
	if len(req.Body) > 0 && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	if httpReq.Header.Get("Accept") == "" {
		httpReq.Header.Set("Accept", "application/json")
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		cancel()
		return nil, nil, &requestError{err: err}
	}
	return resp, cancel, nil
}

// HTTP errors can contain the complete URL, including add-on credentials
// embedded in its path. Keep them out of health records and logs while
// retaining the cause for errors.Is and errors.As.
type requestError struct{ err error }

func (e *requestError) Error() string { return "remote: plugin request failed" }
func (e *requestError) Unwrap() error { return e.err }

// Call sends one request and buffers the reply up to the size cap.
func (c *URLClient) Call(ctx context.Context, req Request) (Response, error) {
	resp, cancel, err := c.do(ctx, req)
	if err != nil {
		return Response{}, err
	}
	defer cancel()
	defer resp.Body.Close()
	body, err := readCapped(resp.Body, c.maxBytes)
	if err != nil {
		return Response{}, err
	}
	return Response{
		Status:  resp.StatusCode,
		Headers: map[string][]string(resp.Header.Clone()),
		Body:    body,
	}, nil
}

// CallStream sends one request and streams the reply. Closing the
// returned body cancels the call. Range requests relay byte for byte
// because the body is never buffered or re-encoded.
func (c *URLClient) CallStream(ctx context.Context, req Request) (*StreamResponse, error) {
	resp, cancel, err := c.do(ctx, req)
	if err != nil {
		return nil, err
	}
	body := &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
	var r io.ReadCloser = body
	if c.maxStream > 0 {
		r = &cappedReadCloser{r: resp.Body, left: c.maxStream, cancel: cancel}
	}
	return &StreamResponse{
		Status:  resp.StatusCode,
		Headers: map[string][]string(resp.Header.Clone()),
		Body:    r,
	}, nil
}

// Close releases idle connections. Calls after Close fail.
func (c *URLClient) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.httpClient.CloseIdleConnections()
	return nil
}

// readCapped reads at most max+1 bytes and fails past the cap.
func readCapped(r io.Reader, max int64) ([]byte, error) {
	if max < 0 {
		max = DefaultMaxResponseBytes
	}
	body, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > max {
		return nil, fmt.Errorf("%w: limit %d bytes", ErrTooLarge, max)
	}
	return body, nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
	once   sync.Once
}

func (b *cancelOnClose) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.cancel)
	return err
}

// cappedReadCloser fails a stream that runs past its byte budget
// instead of silently truncating it.
type cappedReadCloser struct {
	r      io.ReadCloser
	left   int64
	cancel context.CancelFunc
	once   sync.Once
}

func (c *cappedReadCloser) Read(p []byte) (int, error) {
	if c.left <= 0 {
		// One extra byte decides: EOF here means the stream fit
		// exactly, data means it exceeded the cap.
		var one [1]byte
		n, err := c.r.Read(one[:])
		if n > 0 {
			return 0, ErrTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > c.left+1 {
		p = p[:c.left+1]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	if c.left < 0 {
		return n - 1, ErrTooLarge
	}
	return n, err
}

func (c *cappedReadCloser) Close() error {
	err := c.r.Close()
	c.once.Do(c.cancel)
	return err
}

var _ Runtime = (*URLClient)(nil)
var _ Streamer = (*URLClient)(nil)

// IsBlocked reports whether err is a safefetch refusal, so callers
// can show the owner a plain "address not allowed" reason.
func IsBlocked(err error) bool { return errors.Is(err, safefetch.ErrBlocked) }
