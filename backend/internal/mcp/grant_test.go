package mcp

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xihonglei/es-studio/backend/internal/core/es"
)

func testGrant(t *testing.T, store *GrantStore, connectionID string) Grant {
	t.Helper()
	token, err := NewToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	grant := Grant{
		Token: token, ConnectionID: connectionID, Label: "Local ES",
		Scheme: "http", Host: "127.0.0.1", Port: 19200,
		Auth:      AuthFromES(es.Auth{Kind: es.AuthBasic, Username: "elastic", Password: "s3cret-pass"}),
		TLSVerify: true, TimeoutSecs: 10, ReadOnly: true,
		CreatedAt: time.Now().UnixMilli(),
		ExpiresAt: time.Now().Add(TokenTTL).UnixMilli(),
	}
	if err := store.Put(grant); err != nil {
		t.Fatalf("put: %v", err)
	}
	return grant
}

func TestGrantRoundTripAndFind(t *testing.T) {
	store := NewGrantStore(t.TempDir())
	grant := testGrant(t, store, "local-es")

	found, err := store.Find(grant.Token)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found.ConnectionID != "local-es" || found.Auth.Username != "elastic" || found.Auth.Password != "s3cret-pass" {
		t.Fatalf("grant round-trip lost data: %+v", found)
	}
	cfg := found.Config()
	if !cfg.ReadOnly {
		t.Fatal("MCP connections are always read-only")
	}
	if cfg.BaseURL() != "http://127.0.0.1:19200" {
		t.Fatalf("base URL = %s", cfg.BaseURL())
	}
	if _, err := store.Find("not-a-token"); err == nil {
		t.Fatal("unknown token must be rejected")
	}
}

func TestGrantFileHoldsNoPlaintextSecrets(t *testing.T) {
	dir := t.TempDir()
	store := NewGrantStore(dir)
	grant := testGrant(t, store, "local-es")

	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("read grant file: %v", err)
	}
	text := string(raw)
	for _, secret := range []string{grant.Token, "s3cret-pass", "elastic"} {
		if strings.Contains(text, secret) {
			t.Fatalf("grant file exposes %q in plaintext", secret)
		}
	}
	if !strings.Contains(text, `"alg"`) {
		t.Fatal("grant file should record which seal was used")
	}
}

func TestGrantRotatesInsteadOfAccumulating(t *testing.T) {
	store := NewGrantStore(t.TempDir())
	first := testGrant(t, store, "local-es")
	second := testGrant(t, store, "local-es")

	grants, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("grants = %d, want 1 (re-authorising must rotate)", len(grants))
	}
	if grants[0].Token != second.Token || grants[0].Token == first.Token {
		t.Fatal("re-authorising must replace the old token")
	}
	if _, err := store.Find(first.Token); err == nil {
		t.Fatal("the old token must stop working immediately")
	}
}

func TestGrantRevoke(t *testing.T) {
	store := NewGrantStore(t.TempDir())
	grant := testGrant(t, store, "local-es")

	removed, err := store.Revoke("local-es")
	if err != nil || !removed {
		t.Fatalf("revoke = %v, %v", removed, err)
	}
	if _, err := store.Find(grant.Token); err == nil {
		t.Fatal("revoked token must stop working")
	}
	if removed, err := store.Revoke("local-es"); err != nil || removed {
		t.Fatalf("second revoke = %v, %v", removed, err)
	}
}

func TestExpiredGrantIsRejectedWithAnActionableMessage(t *testing.T) {
	store := NewGrantStore(t.TempDir())
	token, _ := NewToken()
	grant := Grant{
		Token: token, ConnectionID: "local-es", Host: "127.0.0.1", Port: 9200,
		CreatedAt: time.Now().Add(-40 * 24 * time.Hour).UnixMilli(),
		ExpiresAt: time.Now().Add(-time.Hour).UnixMilli(),
	}
	if err := store.Put(grant); err != nil {
		t.Fatalf("put: %v", err)
	}
	_, err := store.Find(token)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("err = %v, want an expiry message", err)
	}
	public := grant.Public(time.Now())
	if public.Expired != true || public.Fingerprint == "" {
		t.Fatalf("public = %+v", public)
	}
	if strings.Contains(public.Fingerprint, token) {
		t.Fatal("the public projection must never contain the token")
	}
}

func TestGrantFromDifferentUserIsSkippedNotFatal(t *testing.T) {
	dir := t.TempDir()
	store := NewGrantStore(dir)
	testGrant(t, store, "local-es")
	// Simulate a grant sealed by another user / older key: garbage payload.
	corrupt := grantsFile{Version: 1, Sealed: []entry{{Algorithm: sealAlgorithm, Data: []byte("not-a-real-blob")}}}
	if err := store.save(corrupt); err != nil {
		t.Fatalf("save: %v", err)
	}
	grants, err := store.List()
	if err != nil {
		t.Fatalf("list must tolerate unreadable entries: %v", err)
	}
	if len(grants) != 0 {
		t.Fatalf("grants = %d, want 0", len(grants))
	}
}
