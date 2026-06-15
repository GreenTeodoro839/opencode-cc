// Package config holds all runtime configuration for opencode-cc.
// Configuration is loaded with the following precedence (highest first):
//  1. environment variables
//  2. config.json (persisted from the web panel)
//  3. built-in defaults
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Default values used when nothing else is configured.
const (
	DefaultListenAddr   = ":8787"
	DefaultUpstreamBase = "https://opencode.ai/zen"
	DefaultDefaultModel = "glm-4.6"
	DefaultDataDir      = "data"
	DefaultConfigFile   = "config.json"
)

// ModelMapping maps an incoming Anthropic model name (often "claude-*") to the
// target model that Zen's /v1/chat/completions endpoint understands.
type ModelMapping struct {
	// Match is the pattern to match against the incoming model name.
	// Use "*" to match everything, or a prefix like "claude-".
	Match string `json:"match"`
	// Target is the model name sent to the upstream.
	Target string `json:"target"`
}

// Config is the full application configuration.
type Config struct {
	ListenAddr string `json:"listen_addr"`
	// UpstreamBase is the Zen base URL without a trailing slash, e.g.
	// "https://opencode.ai/zen". Requests go to <base>/v1/chat/completions.
	UpstreamBase string `json:"upstream_base"`
	// ZenAPIKey is the bearer token used to authenticate against Zen.
	ZenAPIKey string `json:"zen_api_key"`
	// PanelToken gates access to the web panel and its API. If empty the panel
	// is open (convenient for local use). Set one before exposing the port.
	PanelToken string `json:"panel_token"`
	// DefaultModel is used when the incoming request has no model and no
	// mapping matches.
	DefaultModel string `json:"default_model"`
	// ModelMappings are evaluated in order; first match wins. The final
	// entry should have Match:"*" as a catch-all.
	ModelMappings []ModelMapping `json:"model_mappings"`
	// LogRequests records each request/response to SQLite for the panel.
	LogRequests bool `json:"log_requests"`
	// MaxBodyLogBytes caps how much of a request/response body is stored.
	MaxBodyLogBytes int `json:"max_body_log_bytes"`
	// RequestTimeoutSeconds is the upstream timeout. 0 = no timeout (streams
	// can run long); a sane upper bound is still recommended.
	RequestTimeoutSeconds int `json:"request_timeout_seconds"`

	dataDir    string
	configPath string
	mu         sync.RWMutex
}

// Default returns a Config populated with sensible defaults.
func Default() *Config {
	return &Config{
		ListenAddr:            DefaultListenAddr,
		UpstreamBase:          DefaultUpstreamBase,
		DefaultModel:          DefaultDefaultModel,
		ModelMappings:         DefaultModelMappings(),
		LogRequests:           true,
		MaxBodyLogBytes:       1 << 14, // 16 KiB per body side
		RequestTimeoutSeconds: 0,
	}
}

// DefaultModelMappings returns the built-in mapping table.
//
// The default is a single pass-through rule: the incoming model name is
// forwarded to the upstream verbatim. This means you send the real Zen model
// id (e.g. glm-5.1, kimi-k2.7-code) as the "model" field and it's used as-is.
// Add specific rules if you want to rename models.
func DefaultModelMappings() []ModelMapping {
	return []ModelMapping{
		{Match: "*", Target: ""}, // pass-through
	}
}

// DataDir returns the directory used for SQLite + config persistence.
func (c *Config) DataDir() string { return c.dataDir }

// Load reads config.json from dataDir, applies env overrides, and returns the
// merged Config. If the file does not exist a default config is returned (and
// the file is not created here — call Save to persist).
func Load(dataDir string) (*Config, error) {
	c := Default()
	c.dataDir = dataDir
	c.configPath = filepath.Join(dataDir, DefaultConfigFile)

	if b, err := os.ReadFile(c.configPath); err == nil {
		// Merge onto defaults so newly-added fields keep their defaults.
		if err := json.Unmarshal(b, c); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	c.applyEnv()
	return c, nil
}

// applyEnv overlays environment variables on top of the loaded config.
func (c *Config) applyEnv() {
	if v := os.Getenv("OPENCODE_CC_LISTEN"); v != "" {
		c.ListenAddr = v
	}
	if v := os.Getenv("OPENCODE_CC_UPSTREAM"); v != "" {
		c.UpstreamBase = strings.TrimRight(v, "/")
	}
	if v := os.Getenv("ZEN_API_KEY"); v != "" {
		c.ZenAPIKey = v
	}
	if v := os.Getenv("OPENCODE_CC_PANEL_TOKEN"); v != "" {
		c.PanelToken = v
	}
	if v := os.Getenv("OPENCODE_CC_DEFAULT_MODEL"); v != "" {
		c.DefaultModel = v
	}
	if v := os.Getenv("OPENCODE_CC_LOG_REQUESTS"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.LogRequests = b
		}
	}
	if v := os.Getenv("OPENCODE_CC_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.RequestTimeoutSeconds = n
		}
	}
}

// Save persists the config to disk. Caller is responsible for holding any
// higher-level lock; this method takes the write lock around file I/O.
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.dataDir == "" {
		return nil
	}
	if err := os.MkdirAll(c.dataDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.configPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.configPath)
}

// Snapshot returns a deep copy safe for handing to callers / JSON marshalling.
// It builds a fresh Config field-by-field to avoid copying the mutex.
func (c *Config) Snapshot() *Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cp := &Config{
		ListenAddr:            c.ListenAddr,
		UpstreamBase:          c.UpstreamBase,
		ZenAPIKey:             c.ZenAPIKey,
		PanelToken:            c.PanelToken,
		DefaultModel:          c.DefaultModel,
		LogRequests:           c.LogRequests,
		MaxBodyLogBytes:       c.MaxBodyLogBytes,
		RequestTimeoutSeconds: c.RequestTimeoutSeconds,
	}
	if c.ModelMappings != nil {
		cp.ModelMappings = append([]ModelMapping(nil), c.ModelMappings...)
	}
	// dataDir / configPath are deliberately left zero (unpersisted bookkeeping).
	return cp
}

// ResolveModel maps an incoming model name to the upstream target.
//
// Pass-through: if a rule matches with an empty Target (e.g. the catch-all
// {"match":"*","target":""}), the incoming model name is returned unchanged
// (after stripping any provider prefix). This lets you send Zen model ids
// (glm-5.1, kimi-k2.7-code, ...) directly as the "model" field.
//
// Provider prefixes that clients add — "anthropic/", "openai/", "provider/" —
// are always stripped: Claude Code sends e.g. "anthropic/kimi-k2.7-code" but
// Zen expects the bare "kimi-k2.7-code". This happens for both pass-through
// and explicit mapping matches.
func (c *Config) ResolveModel(in string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	bare := stripProviderPrefix(in)
	for _, m := range c.ModelMappings {
		if m.Match == "*" || strings.HasPrefix(in, m.Match) || strings.HasPrefix(bare, m.Match) {
			if m.Target == "" {
				// Pass-through: use the (de-prefixed) incoming model name.
				if bare != "" {
					return bare
				}
				break // fall through to DefaultModel
			}
			return m.Target
		}
	}
	if c.DefaultModel != "" {
		return c.DefaultModel
	}
	return DefaultDefaultModel
}

// stripProviderPrefix removes a leading "provider/" segment that clients add.
// Claude Code sends model names like "anthropic/kimi-k2.7-code"; Zen only
// accepts the bare id "kimi-k2.7-code". We strip a single segment before the
// first "/" if the part after it looks like the real model (i.e. the slash
// isn't part of the model id itself). Zen model ids never contain "/", so this
// is safe.
func stripProviderPrefix(in string) string {
	if i := strings.IndexByte(in, '/'); i >= 0 {
		return in[i+1:]
	}
	return in
}

// RLock / RUnlock expose the read lock for hot-path callers that want to read
// several fields consistently.
func (c *Config) RLock()   { c.mu.RLock() }
func (c *Config) RUnlock() { c.mu.RUnlock() }

// SetZenAPIKey is a convenience setter used by the panel API.
func (c *Config) SetZenAPIKey(v string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ZenAPIKey = v
}

// Apply merges a partial config coming from the panel. Only fields the caller
// is allowed to change are taken from src.
func (c *Config) Apply(src *Config) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if src.ListenAddr != "" {
		c.ListenAddr = src.ListenAddr
	}
	if src.UpstreamBase != "" {
		c.UpstreamBase = strings.TrimRight(src.UpstreamBase, "/")
	}
	// ZenAPIKey: empty means "don't change" so the panel never clobbers it.
	if src.ZenAPIKey != "" {
		c.ZenAPIKey = src.ZenAPIKey
	}
	if src.PanelTokenSet() {
		c.PanelToken = src.PanelToken
	}
	if src.DefaultModel != "" {
		c.DefaultModel = src.DefaultModel
	}
	if src.ModelMappings != nil {
		c.ModelMappings = append([]ModelMapping(nil), src.ModelMappings...)
	}
	c.LogRequests = src.LogRequests
	if src.MaxBodyLogBytes > 0 {
		c.MaxBodyLogBytes = src.MaxBodyLogBytes
	}
	c.RequestTimeoutSeconds = src.RequestTimeoutSeconds
}

// PanelTokenSet reports whether src carries a panel token change.
func (c *Config) PanelTokenSet() bool { return c != nil && c.PanelToken != "__unchanged__" }
