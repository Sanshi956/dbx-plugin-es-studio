package connection

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/xihonglei/es-studio/backend/internal/core/es"
)

// Config is the resolved connection. Credentials live only in Auth's
// json:"-" fields and never leave the Sidecar process.
type Config struct {
	ID          string
	Name        string
	Scheme      string
	Host        string
	Port        int
	Environment string
	Description string
	Auth        es.Auth
	TLSVerify   bool
	Timeout     time.Duration
	ReadOnly    bool
}

// BaseURL is the only place a host/port ever becomes a URL.
func (c Config) BaseURL() string {
	scheme := c.Scheme
	if scheme == "" {
		scheme = "http"
	}
	host := c.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := c.Port
	if port == 0 {
		port = 9200
	}
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(port))
}

// Session is the public, safe projection of a live connection.
type Session struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	ReadOnly  bool   `json:"readOnly"`
	Connected bool   `json:"connected"`

	cfg    Config     `json:"-"`
	client *es.Client `json:"-"`
}

// String never includes credentials (requirement 10.1).
func (s *Session) String() string {
	return fmt.Sprintf("session(%s, %s)", s.ID, s.Endpoint)
}

// Config returns a copy for the permission layer / core services.
func (s *Session) Config() Config { return s.cfg }

// Client returns the shared ES adapter for this session.
func (s *Session) Client() *es.Client { return s.client }

// Manager caches sessions by DBX connection.id. Connect/Disconnect are
// idempotent because the host may replay lifecycle events.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewManager creates an empty session registry.
func NewManager() *Manager {
	return &Manager{sessions: make(map[string]*Session)}
}

// Connect returns the existing session for an id, or builds one.
func (m *Manager) Connect(cfg Config) (*Session, error) {
	if cfg.ID == "" {
		return nil, fmt.Errorf("connection.id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.sessions[cfg.ID]; ok {
		return existing, nil
	}
	client, err := es.NewClient(es.Config{
		BaseURL:   cfg.BaseURL(),
		Auth:      cfg.Auth,
		Timeout:   cfg.Timeout,
		TLSVerify: cfg.TLSVerify,
	})
	if err != nil {
		return nil, err
	}
	session := &Session{
		ID:        cfg.ID,
		Name:      cfg.Name,
		Endpoint:  cfg.BaseURL(),
		ReadOnly:  cfg.ReadOnly,
		Connected: true,
		cfg:       cfg,
		client:    client,
	}
	m.sessions[cfg.ID] = session
	return session, nil
}

// Get looks up a cached session.
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

// Disconnect removes a session; returns false when it was already gone.
func (m *Manager) Disconnect(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[id]; !ok {
		return false
	}
	delete(m.sessions, id)
	return true
}

// ParseLifecycle resolves the DBX lifecycle payload. The host may place values
// in root / connection / external_config / runtime / secrets; later layers win
// so runtime-injected host/port always override stored config.
func ParseLifecycle(raw json.RawMessage) (Config, error) {
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return Config{}, fmt.Errorf("invalid lifecycle payload: %w", err)
	}
	conn := asMap(root["connection"])
	ext := asMap(conn["external_config"])
	if len(ext) == 0 {
		ext = asMap(root["external_config"])
	}
	runtime := asMap(root["runtime"])
	secrets := asMap(root["secrets"])
	connSecrets := asMap(conn["connection_secrets"])

	// Layer order matters: `pick` lets later layers win.
	// Dev host puts host/port/username on `connection`, config fields on
	// `connection.external_config`, and secrets on `connection.connection_secrets`.
	// Runtime (host-injected endpoint) must always win over stored config.
	cfg := Config{
		ID:          asString(pick([]string{"id", "connection_id"}, root, conn)),
		Name:        asString(pick([]string{"name", "display_name"}, root, ext, conn)),
		Scheme:      asString(pick([]string{"scheme", "protocol"}, conn, ext, runtime, root)),
		Host:        asString(pick([]string{"host", "hostname"}, conn, ext, runtime, root)),
		Port:        asInt(pick([]string{"port"}, conn, ext, runtime, root)),
		Environment: asString(pick([]string{"environment", "env"}, conn, ext, root)),
		Description: asString(pick([]string{"description"}, conn, ext, root)),
		TLSVerify:   asBool(pick([]string{"tls_verify", "verify_tls", "tlsVerify"}, conn, ext, runtime, root), true),
		ReadOnly:    asBool(pick([]string{"read_only", "readonly", "readOnly"}, conn, ext, root), false),
		Timeout:     time.Duration(asInt(pick([]string{"query_timeout_secs", "timeout_secs", "timeout"}, conn, ext, runtime, root))) * time.Second,
	}
	if cfg.Scheme == "" {
		cfg.Scheme = "http"
	}
	if cfg.Port == 0 {
		cfg.Port = 9200
	}
	if cfg.Timeout <= 0 {
		// Large clusters (thousands of shards) need more than 10s for O(indices)
		// endpoints; connections can still override via query_timeout_secs.
		cfg.Timeout = 60 * time.Second
	}
	if cfg.Name == "" {
		cfg.Name = cfg.ID
	}
	if cfg.ID == "" {
		return Config{}, fmt.Errorf("lifecycle payload has no connection.id")
	}

	authType := asString(pick([]string{"auth_type", "authentication", "authType"}, conn, ext, root))
	username := asString(pick([]string{"username", "user"}, conn, ext, connSecrets, secrets, root))
	secretLayers := []map[string]any{conn, ext, secrets, connSecrets, root}
	cfg.Auth = es.Auth{Kind: es.AuthNone, Username: username}
	switch authType {
	case es.AuthBasic, "basic_auth", "password":
		cfg.Auth.Kind = es.AuthBasic
		cfg.Auth.Password = asString(pick([]string{"password"}, secretLayers...))
	case es.AuthAPIKey, "apikey", "apiKey":
		cfg.Auth.Kind = es.AuthAPIKey
		cfg.Auth.APIKey = asString(pick([]string{"api_key", "apiKey"}, secretLayers...))
	case es.AuthBearer, "token":
		cfg.Auth.Kind = es.AuthBearer
		cfg.Auth.Token = asString(pick([]string{"token", "bearer_token"}, secretLayers...))
	case "", es.AuthNone, "no_auth":
		cfg.Auth.Kind = es.AuthNone
	default:
		return Config{}, fmt.Errorf("unsupported authentication type %q", authType)
	}
	return cfg, nil
}

// pick returns the last non-nil value across layers (later layers win).
func pick(keys []string, layers ...map[string]any) any {
	var result any
	for _, layer := range layers {
		for _, key := range keys {
			if value, ok := layer[key]; ok && value != nil {
				result = value
			}
		}
	}
	return result
}

func asMap(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func asString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func asInt(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}

func asBool(value any, fallback bool) bool {
	if b, ok := value.(bool); ok {
		return b
	}
	if s, ok := value.(string); ok {
		if b, err := strconv.ParseBool(s); err == nil {
			return b
		}
	}
	return fallback
}
