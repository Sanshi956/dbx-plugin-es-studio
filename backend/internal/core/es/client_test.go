package es

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func mustClient(t *testing.T, base string, cfg Config) *Client {
	t.Helper()
	cfg.BaseURL = base
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// T6 (RED): HTTP client contract, docs/01-PLAN.md.
func TestClientRequestAndQuery(t *testing.T) {
	var gotPath, gotRawQuery, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotRawQuery, gotMethod = r.URL.Path, r.URL.RawQuery, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := mustClient(t, srv.URL, DefaultConfig())
	resp, err := c.Do(context.Background(), Request{
		Method: "GET",
		Path:   "/_cluster/health",
		Query:  url.Values{"level": {"indices"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/_cluster/health" || gotMethod != "GET" {
		t.Fatalf("path/method not preserved: %s %s", gotMethod, gotPath)
	}
	if gotRawQuery != "level=indices" {
		t.Fatalf("query not preserved: %q", gotRawQuery)
	}
	if resp.Status != 200 || !strings.Contains(string(resp.Body), `"ok":true`) {
		t.Fatalf("bad response: %+v", resp)
	}
	if resp.Duration <= 0 {
		t.Fatal("response duration must be measured")
	}
}

func TestClientRejectsUnsafePaths(t *testing.T) {
	c := mustClient(t, "http://127.0.0.1:1", DefaultConfig())
	unsafe := []string{
		"http://evil.example/_search",
		"//evil.example/_search",
		"/_cat/../_cluster/settings",
		"/_search@evil.example",
		"/index\r\nX-Injected: 1",
		"",
		"relative/path",
	}
	for _, path := range unsafe {
		if _, err := c.Do(context.Background(), Request{Method: "GET", Path: path}); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("path %q must be rejected with ErrUnsafePath, got %v", path, err)
		}
	}
}

func TestClientAuthHeadersAndNoLeak(t *testing.T) {
	secrets := []string{"pass-s3cr3t", "key-s3cr3t", "tok-s3cr3t"}
	cases := []struct {
		name string
		auth Auth
		want string
	}{
		{"basic", Auth{Kind: AuthBasic, Username: "admin", Password: "pass-s3cr3t"},
			"Basic " + base64.StdEncoding.EncodeToString([]byte("admin:pass-s3cr3t"))},
		{"api_key", Auth{Kind: AuthAPIKey, APIKey: "key-s3cr3t"}, "ApiKey key-s3cr3t"},
		{"bearer", Auth{Kind: AuthBearer, Token: "tok-s3cr3t"}, "Bearer tok-s3cr3t"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Get("Authorization")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"error":"unauthorized"}`)
			}))
			defer srv.Close()
			cfg := DefaultConfig()
			cfg.Auth = tc.auth
			c := mustClient(t, srv.URL, cfg)
			resp, err := c.Do(context.Background(), Request{Method: "GET", Path: "/_cluster/health"})
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("Authorization = %q, want %q", got, tc.want)
			}
			if resp.Status != http.StatusUnauthorized {
				t.Fatalf("401 must be passed through, got %d", resp.Status)
			}
			// Credentials must never appear in serialized auth, errors or responses.
			encoded, err := json.Marshal(tc.auth)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range secrets {
				if strings.Contains(string(encoded), secret) ||
					strings.Contains(string(resp.Body), secret) ||
					strings.Contains(errText(err), secret) {
					t.Fatalf("secret %q leaked", secret)
				}
			}
		})
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestClientPassthroughStatus(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404, 500} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			_, _ = io.WriteString(w, `{"status":"upstream-error"}`)
		}))
		c := mustClient(t, srv.URL, DefaultConfig())
		resp, err := c.Do(context.Background(), Request{Method: "GET", Path: "/_cluster/health"})
		srv.Close()
		if err != nil {
			t.Fatalf("status %d must not be swallowed: %v", code, err)
		}
		if resp.Status != code || !strings.Contains(string(resp.Body), "upstream-error") {
			t.Fatalf("status %d not passed through: %+v", code, resp)
		}
	}
}

func TestClientTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = io.WriteString(w, "late")
	}))
	defer srv.Close()
	cfg := DefaultConfig()
	cfg.Timeout = 60 * time.Millisecond
	c := mustClient(t, srv.URL, cfg)
	_, err := c.Do(context.Background(), Request{Method: "GET", Path: "/_search"})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("expected ErrTimeout, got %v", err)
	}
}

func TestClientResponseTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 8192))
	}))
	defer srv.Close()
	cfg := DefaultConfig()
	cfg.MaxResponseBytes = 128
	c := mustClient(t, srv.URL, cfg)
	_, err := c.Do(context.Background(), Request{Method: "GET", Path: "/_search"})
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("expected ErrResponseTooLarge, got %v", err)
	}
}
