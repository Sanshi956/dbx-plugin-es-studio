package connection

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/xihonglei/es-studio/backend/internal/core/es"
)

const lifecyclePayload = `{
  "provider": {"id": "local.es.studio.connection"},
  "connection": {
    "id": "dev-es",
    "name": "Dev ES",
    "external_config": {
      "protocol": "https",
      "host": "es.internal",
      "port": 9200,
      "auth_type": "basic",
      "username": "admin",
      "tls_verify": false,
      "timeout_secs": 7,
      "read_only": true,
      "environment": "Production",
      "description": "prod cluster"
    }
  },
  "runtime": {"host": "127.0.0.1", "port": 19200},
  "secrets": {"password": "pass-s3cr3t", "api_key": "key-s3cr3t", "token": "tok-s3cr3t"}
}`

// T8 (RED): lifecycle payload parsing, docs/01-PLAN.md.
func TestManagerParseLifecycle(t *testing.T) {
	cfg, err := ParseLifecycle(json.RawMessage(lifecyclePayload))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ID != "dev-es" || cfg.Name != "Dev ES" {
		t.Fatalf("identity lost: %+v", cfg)
	}
	// runtime (host injected by DBX) wins over the stored external_config.
	if cfg.Host != "127.0.0.1" || cfg.Port != 19200 {
		t.Fatalf("runtime host/port must win: %s:%d", cfg.Host, cfg.Port)
	}
	if cfg.Scheme != "https" {
		t.Fatalf("scheme = %q, want https", cfg.Scheme)
	}
	if cfg.Auth.Kind != es.AuthBasic || cfg.Auth.Username != "admin" || cfg.Auth.Password != "pass-s3cr3t" {
		t.Fatalf("auth not hydrated: %+v", cfg.Auth)
	}
	if cfg.TLSVerify {
		t.Fatal("tls_verify=false must be respected")
	}
	if cfg.Timeout != 7*time.Second {
		t.Fatalf("timeout = %v, want 7s", cfg.Timeout)
	}
	if !cfg.ReadOnly || cfg.Environment != "Production" || cfg.Description != "prod cluster" {
		t.Fatalf("metadata lost: %+v", cfg)
	}
}

const devHostPayload = `{
  "provider": {"id":"local.es.studio.connection","databaseType":"elasticsearch"},
  "connection": {
    "id":"dev-1","db_type":"plugin","plugin_id":"local.es.studio",
    "name":"Local ES","host":"127.0.0.1","port":19200,"username":"",
    "external_config": {
      "display_name":"Local ES","environment":"Development","protocol":"http",
      "auth_type":"basic","tls_verify":true,"timeout_secs":10,
      "description":"local test","read_only":true
    },
    "connection_secrets": {"password":"pass-s3cr3t"},
    "read_only": false,
    "query_timeout_secs": 60
  },
  "runtime": null
}`

func TestManagerParseDevHostLifecycle(t *testing.T) {
	cfg, err := ParseLifecycle(json.RawMessage(devHostPayload))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ID != "dev-1" || cfg.Name != "Local ES" {
		t.Fatalf("identity lost: %+v", cfg)
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != 19200 || cfg.Scheme != "http" {
		t.Fatalf("endpoint mismatch: %s://%s:%d", cfg.Scheme, cfg.Host, cfg.Port)
	}
	if cfg.Auth.Kind != es.AuthBasic || cfg.Auth.Password != "pass-s3cr3t" {
		t.Fatalf("secret not hydrated from connection_secrets: %+v", cfg.Auth)
	}
	if !cfg.ReadOnly {
		t.Fatal("form read_only must override the dev-host record default")
	}
	if cfg.Timeout != 10*time.Second {
		t.Fatalf("form timeout_secs must override query_timeout_secs: %v", cfg.Timeout)
	}
}

func TestManagerParseAuthKinds(t *testing.T) {
	cases := []struct {
		authType string
		want     string
	}{
		{"none", es.AuthNone},
		{"basic", es.AuthBasic},
		{"api_key", es.AuthAPIKey},
		{"bearer", es.AuthBearer},
	}
	for _, c := range cases {
		raw := fmt.Sprintf(`{"connection":{"id":"c","external_config":{"host":"127.0.0.1","port":9200,"auth_type":"%s","username":"u"}},"secrets":{"password":"pass-s3cr3t","api_key":"key-s3cr3t","token":"tok-s3cr3t"}}`, c.authType)
		cfg, err := ParseLifecycle(json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Auth.Kind != c.want {
			t.Fatalf("auth_type %s -> %s, want %s", c.authType, cfg.Auth.Kind, c.want)
		}
	}
}

func TestManagerSessionLifecycle(t *testing.T) {
	m := NewManager()
	cfg := Config{
		ID: "dev-es", Name: "Dev", Scheme: "http", Host: "127.0.0.1", Port: 19200,
		Auth: es.Auth{Kind: es.AuthBasic, Username: "u", Password: "pass-s3cr3t"},
		Timeout: 3 * time.Second, TLSVerify: true,
	}
	s1, err := m.Connect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := m.Connect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if s1 != s2 {
		t.Fatal("Connect must be idempotent for the same connection.id")
	}
	if got, ok := m.Get("dev-es"); !ok || got != s1 {
		t.Fatal("session not cached by connection.id")
	}
	if !m.Disconnect("dev-es") {
		t.Fatal("first disconnect must report a removed session")
	}
	if m.Disconnect("dev-es") {
		t.Fatal("second disconnect must be a no-op, not a duplicate removal")
	}
	if _, ok := m.Get("dev-es"); ok {
		t.Fatal("session still cached after disconnect")
	}
}

func TestManagerNoSecretLeak(t *testing.T) {
	m := NewManager()
	cfg := Config{
		ID: "prod-es", Scheme: "https", Host: "es.internal", Port: 9200,
		Auth: es.Auth{Kind: es.AuthBasic, Username: "admin", Password: "pass-s3cr3t",
			APIKey: "key-s3cr3t", Token: "tok-s3cr3t"},
		Timeout: time.Second,
	}
	s, err := m.Connect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"pass-s3cr3t", "key-s3cr3t", "tok-s3cr3t"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("secret %q leaked via json.Marshal(session)", secret)
		}
		if strings.Contains(fmt.Sprint(s), secret) {
			t.Fatalf("secret %q leaked via String()", secret)
		}
	}
	if s.Endpoint == "" || strings.Contains(s.Endpoint, "pass-s3cr3t") {
		t.Fatalf("endpoint must be a masked, non-empty address: %q", s.Endpoint)
	}
}
