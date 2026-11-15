package mcp

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/xihonglei/es-studio/backend/internal/core/connection"
	"github.com/xihonglei/es-studio/backend/internal/core/es"
)

// Grants are how the independent MCP server reaches a cluster without reading
// DBX's own database (SPEC §5.2 / requirement 8):
//
//	Sidecar (knows the session + credentials)  --seal--> grant file
//	MCP server (separate process, same user)    --open--> grant file
//
// The file is encrypted for the current OS user (DPAPI on Windows), contains at
// most one connection's endpoint + auth, and is addressed by an opaque token the
// user copies into their MCP client config. Nothing else can be reached: the MCP
// server never accepts a URL or a connection id from the client.
const (
	grantFilename = "mcp-grants.json"
	// TokenTTL keeps a leaked config from working forever; the user re-authorises
	// from the plugin UI when it expires.
	TokenTTL = 30 * 24 * time.Hour
)

// grantAuth mirrors es.Auth but *does* serialise the secrets: es.Auth hides them
// behind `json:"-"` so they can never leak into an event or a log — which would
// also hide them from the sealed grant and silently break basic-auth clusters.
// This type only ever exists inside the encrypted blob.
type grantAuth struct {
	Kind     string `json:"kind"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
	Token    string `json:"token,omitempty"`
}

// AuthFromES builds the sealed-payload authentication for a grant. The return
// type is intentionally unexported: only this package can define what a grant
// carries, while the Sidecar still needs a way to hand over a session's auth.
func AuthFromES(auth es.Auth) grantAuth {
	return grantAuth{
		Kind: auth.Kind, Username: auth.Username, Password: auth.Password,
		APIKey: auth.APIKey, Token: auth.Token,
	}
}

func (a grantAuth) toES() es.Auth {
	return es.Auth{
		Kind: a.Kind, Username: a.Username, Password: a.Password,
		APIKey: a.APIKey, Token: a.Token,
	}
}

// Grant is the decrypted payload for one authorised connection.
type Grant struct {
	Token        string    `json:"token"`
	ConnectionID string    `json:"connection_id"`
	Label        string    `json:"label"`
	Scheme       string    `json:"scheme"`
	Host         string    `json:"host"`
	Port         int       `json:"port"`
	Auth         grantAuth `json:"auth"`
	TLSVerify    bool      `json:"tls_verify"`
	TimeoutSecs  int       `json:"timeout_secs"`
	ReadOnly     bool      `json:"read_only"`
	CreatedAt    int64     `json:"created_at"`
	ExpiresAt    int64     `json:"expires_at"`
	SealedBy     string    `json:"sealed_by"`
}

// Expired reports whether the token is past its TTL.
func (g Grant) Expired(now time.Time) bool {
	return g.ExpiresAt > 0 && now.UnixMilli() > g.ExpiresAt
}

// Config turns a grant into the same connection.Config the Sidecar uses, so the
// MCP server and the plugin speak to ES through identical code.
func (g Grant) Config() connection.Config {
	cfg := connection.Config{
		ID:        g.ConnectionID,
		Name:      g.Label,
		Scheme:    g.Scheme,
		Host:      g.Host,
		Port:      g.Port,
		Auth:      g.Auth.toES(),
		TLSVerify: g.TLSVerify,
		ReadOnly:  true, // MCP is read-only regardless of what the grant says
	}
	if g.TimeoutSecs > 0 {
		cfg.Timeout = time.Duration(g.TimeoutSecs) * time.Second
	}
	return cfg
}

// Public is the safe projection shown in the UI: no token, no credentials, only
// a fingerprint the user can match against their client config.
type Public struct {
	ConnectionID string `json:"connection_id"`
	Label        string `json:"label"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Fingerprint  string `json:"fingerprint"`
	CreatedAt    int64  `json:"created_at"`
	ExpiresAt    int64  `json:"expires_at"`
	Expired      bool   `json:"expired"`
	ReadOnly     bool   `json:"read_only"`
	Transport    string `json:"transport"`
}

// ESAuth is the in-memory authentication for this grant. It is deliberately
// exported for the MCP server (a different package) and deliberately never
// serialised by any struct that leaves the process.
func (g Grant) ESAuth() es.Auth { return g.Auth.toES() }

// Public returns the secret-free view of a grant.
func (g Grant) Public(now time.Time) Public {
	return Public{
		ConnectionID: g.ConnectionID,
		Label:        g.Label,
		Host:         g.Host,
		Port:         g.Port,
		Fingerprint:  Fingerprint(g.Token),
		CreatedAt:    g.CreatedAt,
		ExpiresAt:    g.ExpiresAt,
		Expired:      g.Expired(now),
		ReadOnly:     true,
		Transport:    "stdio",
	}
}

// Fingerprint is a short, non-reversible id for a token (shown in the UI/logs).
func Fingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:4])
}

type grantsFile struct {
	Version int     `json:"version"`
	Sealed  []entry `json:"entries"`
}

type entry struct {
	Algorithm string `json:"alg"`
	Data      []byte `json:"data"`
}

// GrantStore persists sealed grants in the plugin data dir.
type GrantStore struct {
	path string
}

// NewGrantStore resolves the grants file. An empty dir falls back to the same
// location the Sidecar uses for its series, so both processes agree.
func NewGrantStore(dir string) *GrantStore {
	if dir == "" {
		dir = os.Getenv("DBX_PLUGIN_DATA_DIR")
	}
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "es-studio")
	}
	return &GrantStore{path: filepath.Join(dir, grantFilename)}
}

// Path is exposed for diagnostics (the UI shows where the grant file lives).
func (s *GrantStore) Path() string { return s.path }

// NewToken returns a 256-bit URL-safe token.
func NewToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// Put seals and stores a grant, replacing any earlier grant for the connection
// (re-authorising rotates the token instead of accumulating them).
func (s *GrantStore) Put(g Grant) error {
	existing, err := s.load()
	if err != nil {
		return err
	}
	kept := make([]entry, 0, len(existing))
	for _, item := range existing {
		plain, err := unseal(item.Data, item.Algorithm)
		if err != nil {
			continue // unreadable entry (another user / rotated key): drop it
		}
		var previous Grant
		if json.Unmarshal(plain, &previous) != nil {
			continue
		}
		if previous.ConnectionID == g.ConnectionID {
			continue
		}
		kept = append(kept, item)
	}
	plain, err := json.Marshal(g)
	if err != nil {
		return err
	}
	sealed, algorithm, err := seal(plain)
	if err != nil {
		return fmt.Errorf("cannot seal grant: %w", err)
	}
	kept = append(kept, entry{Algorithm: algorithm, Data: sealed})
	return s.save(grantsFile{Version: 1, Sealed: kept})
}

// List decrypts every readable grant.
func (s *GrantStore) List() ([]Grant, error) {
	entries, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]Grant, 0, len(entries))
	for _, item := range entries {
		plain, err := unseal(item.Data, item.Algorithm)
		if err != nil {
			continue
		}
		var grant Grant
		if json.Unmarshal(plain, &grant) != nil {
			continue
		}
		out = append(out, grant)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

// Find resolves a token to its grant. Expired grants are reported separately so
// the server can answer with a clear message instead of "unauthorised".
func (s *GrantStore) Find(token string) (Grant, error) {
	if token == "" {
		return Grant{}, errors.New("no token supplied")
	}
	grants, err := s.List()
	if err != nil {
		return Grant{}, err
	}
	for _, grant := range grants {
		if grant.Token == token {
			if grant.Expired(time.Now()) {
				return grant, errors.New("authorisation expired; re-authorise this connection in ES Studio")
			}
			return grant, nil
		}
	}
	return Grant{}, errors.New("token is not authorised for any connection")
}

// Revoke removes the grant for one connection and reports whether it existed.
func (s *GrantStore) Revoke(connectionID string) (bool, error) {
	entries, err := s.load()
	if err != nil {
		return false, err
	}
	kept := make([]entry, 0, len(entries))
	removed := false
	for _, item := range entries {
		plain, err := unseal(item.Data, item.Algorithm)
		if err != nil {
			continue
		}
		var grant Grant
		if json.Unmarshal(plain, &grant) != nil {
			continue
		}
		if grant.ConnectionID == connectionID {
			removed = true
			continue
		}
		kept = append(kept, item)
	}
	if !removed {
		return false, nil
	}
	return true, s.save(grantsFile{Version: 1, Sealed: kept})
}

func (s *GrantStore) load() ([]entry, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var file grantsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("grant file is corrupt: %w", err)
	}
	return file.Sealed, nil
}

func (s *GrantStore) save(file grantsFile) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
