package connection

import (
	"net/url"
	"strconv"
	"time"

	"github.com/xihonglei/es-studio/backend/internal/core/es"
)

// Direct is a test/preview-only connection override. It is also what the
// smoke and integration tests use to point the Sidecar at a real cluster.
// It is never used to bypass the connection whitelist in production paths.
type Direct struct {
	URL         string `json:"url"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	APIKey      string `json:"api_key"`
	Token       string `json:"token"`
	ReadOnly    bool   `json:"read_only"` // test/preview only: production read-only comes from the host lifecycle
	TLSVerify   *bool  `json:"tls_verify"`
	TimeoutSecs int    `json:"timeout_secs"`
}

// Config builds a Config from a direct URL without ever exposing secrets in
// serialized form.
func (d Direct) Config(id string) Config {
	cfg := Config{
		ID:        id,
		Name:      id,
		Scheme:    "http",
		Host:      "127.0.0.1",
		Port:      9200,
		TLSVerify: true,
		Timeout:   10 * time.Second,
		ReadOnly:  d.ReadOnly,
		Auth:      es.Auth{Kind: es.AuthNone, Username: d.Username},
	}
	if parsed, err := url.Parse(d.URL); err == nil && parsed.Host != "" {
		if parsed.Scheme == "https" || parsed.Scheme == "http" {
			cfg.Scheme = parsed.Scheme
		}
		cfg.Host = parsed.Hostname()
		if p := parsed.Port(); p != "" {
			if n, err := strconv.Atoi(p); err == nil {
				cfg.Port = n
			}
		}
	}
	if d.TLSVerify != nil {
		cfg.TLSVerify = *d.TLSVerify
	}
	if d.TimeoutSecs > 0 {
		cfg.Timeout = time.Duration(d.TimeoutSecs) * time.Second
	}
	switch {
	case d.APIKey != "":
		cfg.Auth.Kind, cfg.Auth.APIKey = es.AuthAPIKey, d.APIKey
	case d.Token != "":
		cfg.Auth.Kind, cfg.Auth.Token = es.AuthBearer, d.Token
	case d.Password != "" || d.Username != "":
		cfg.Auth.Kind, cfg.Auth.Password = es.AuthBasic, d.Password
	}
	return cfg
}
