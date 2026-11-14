package es

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	// ErrUnsafePath rejects absolute URLs, traversal, userinfo and header injection.
	ErrUnsafePath = errors.New("unsafe request path")
	// ErrTimeout is a normalized timeout so callers never see transport internals.
	ErrTimeout = errors.New("elasticsearch request timed out")
	// ErrResponseTooLarge protects the sidecar (and DBX UI) from unbounded bodies.
	ErrResponseTooLarge = errors.New("elasticsearch response too large")
)

// Auth holds credentials in memory only. Secret fields are json:"-" so a
// session / config can never be serialized into an event, log or history row.
type Auth struct {
	Kind     string `json:"kind"`
	Username string `json:"username,omitempty"`
	Password string `json:"-"`
	APIKey   string `json:"-"`
	Token    string `json:"-"`
}

const (
	AuthNone   = "none"
	AuthBasic  = "basic"
	AuthAPIKey = "api_key"
	AuthBearer = "bearer"
)

// Config configures one ES HTTP client. BaseURL is the ONLY authority for the
// target host; REST Console paths are always relative and validated below.
type Config struct {
	BaseURL          string
	Auth             Auth
	Timeout          time.Duration
	MaxResponseBytes int64
	TLSVerify        bool
}

// DefaultConfig uses safe defaults: verify TLS, 10s timeout, 8 MiB cap.
func DefaultConfig() Config {
	return Config{
		Timeout:          10 * time.Second,
		MaxResponseBytes: 8 << 20,
		TLSVerify:        true,
	}
}

// Request is a relative-path ES request. Query/Headers are explicit so no
// caller can smuggle a different host through a raw URL string.
type Request struct {
	Method  string      `json:"method"`
	Path    string      `json:"path"`
	Query   url.Values  `json:"query,omitempty"`
	Body    []byte      `json:"body,omitempty"`
	Headers http.Header `json:"headers,omitempty"`
}

// Response preserves upstream status/body verbatim; errors are never swallowed.
type Response struct {
	Status   int           `json:"status"`
	Headers  http.Header   `json:"headers"`
	Body     []byte        `json:"body"`
	Duration time.Duration `json:"duration"`
}

// Client is a thin, guarded ES HTTP adapter shared by UI, Sidecar and MCP.
type Client struct {
	base string
	auth Auth
	http *http.Client
	max  int64
}

// NewClient validates the base URL (the only place a host is ever accepted).
func NewClient(cfg Config) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid elasticsearch base URL")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	max := cfg.MaxResponseBytes
	if max <= 0 {
		max = 8 << 20
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !cfg.TLSVerify}, // #nosec G402 - explicit user setting
	}
	return &Client{
		base: base,
		auth: cfg.Auth,
		http: &http.Client{Timeout: timeout, Transport: transport},
		max:  max,
	}, nil
}

// ValidatePath is the service-layer security boundary (requirement 10.2).
func ValidatePath(path string) error {
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return fmt.Errorf("%w: %q", ErrUnsafePath, path)
	}
	if strings.Contains(path, "://") || strings.Contains(path, "..") ||
		strings.Contains(path, "@") || strings.ContainsAny(path, "\r\n\\") {
		return fmt.Errorf("%w: %q", ErrUnsafePath, path)
	}
	if _, err := url.ParseRequestURI(path); err != nil {
		return fmt.Errorf("%w: %q", ErrUnsafePath, path)
	}
	return nil
}

// Do performs the request. Errors contain only method + path, never headers
// or credentials (requirement 10.1).
func (c *Client) Do(ctx context.Context, r Request) (*Response, error) {
	if err := ValidatePath(r.Path); err != nil {
		return nil, err
	}
	method := strings.ToUpper(strings.TrimSpace(r.Method))
	if method == "" {
		method = http.MethodGet
	}
	target := c.base + r.Path
	if len(r.Query) > 0 {
		target += "?" + r.Query.Encode()
	}
	var body io.Reader
	if len(r.Body) > 0 {
		body = bytes.NewReader(r.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, fmt.Errorf("cannot build %s %s request", method, r.Path)
	}
	for key, values := range r.Headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if len(r.Body) > 0 && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	c.applyAuth(req)

	start := time.Now()
	res, err := c.http.Do(req)
	duration := time.Since(start)
	if err != nil {
		if isTimeout(err) {
			return nil, fmt.Errorf("%w: %s %s", ErrTimeout, method, r.Path)
		}
		return nil, fmt.Errorf("request %s %s failed: %w", method, r.Path, err)
	}
	defer res.Body.Close()

	data, err := io.ReadAll(io.LimitReader(res.Body, c.max+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s %s response: %w", method, r.Path, err)
	}
	if int64(len(data)) > c.max {
		return nil, fmt.Errorf("%w: %s %s exceeded %d bytes", ErrResponseTooLarge, method, r.Path, c.max)
	}
	return &Response{
		Status:   res.StatusCode,
		Headers:  res.Header.Clone(),
		Body:     data,
		Duration: duration,
	}, nil
}

func (c *Client) applyAuth(req *http.Request) {
	switch c.auth.Kind {
	case AuthBasic:
		req.SetBasicAuth(c.auth.Username, c.auth.Password)
	case AuthAPIKey:
		req.Header.Set("Authorization", "ApiKey "+c.auth.APIKey)
	case AuthBearer:
		req.Header.Set("Authorization", "Bearer "+c.auth.Token)
	}
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return strings.Contains(err.Error(), "Client.Timeout exceeded") ||
		strings.Contains(err.Error(), "context deadline exceeded")
}
