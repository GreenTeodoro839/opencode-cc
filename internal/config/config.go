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
	DefaultListenAddr         = ":8787"
	DefaultUpstreamBase       = "https://opencode.ai/zen"
	DefaultDefaultModel       = "glm-4.6"
	DefaultDataDir            = "data"
	DefaultConfigFile         = "config.json"
	WebSearchModeAuto         = "auto"
	WebSearchModeNative       = "native"
	WebSearchModeTranslate    = "translate"
	DefaultWebSearchMode      = WebSearchModeAuto
	UpstreamProtocolAuto      = "auto"
	UpstreamProtocolOpenAI    = "openai"
	UpstreamProtocolAnthropic = "anthropic"
)

// ModelMapping maps an incoming Anthropic model name (often "claude-*") to the
// target model that the upstream endpoint understands.
type ModelMapping struct {
	// Match is the pattern to match against the incoming model name.
	// Use "*" to match everything, a prefix like "claude-", or a wildcard
	// pattern like "claude-*".
	Match string `json:"match"`
	// Target is the model name sent to the upstream. If Target contains "*",
	// the wildcard capture from Match is substituted into it.
	Target string `json:"target"`
}

// UpstreamModel declares one target model or wildcard pattern supported by an
// upstream. Client-visible aliases belong in Config.ModelMappings. Alias,
// Match, and Target remain accepted for older config files, but they are
// interpreted only as supported target-model patterns here.
type UpstreamModel struct {
	Name   string `json:"name,omitempty"`
	Alias  string `json:"alias,omitempty"`
	Match  string `json:"match,omitempty"`
	Target string `json:"target,omitempty"`
}

// Upstream is one backend (base URL + API key) the proxy can forward to.
// When Models is non-empty on any enabled upstream, requests are routed to the
// first upstream whose supported target-model list matches the globally mapped
// target model.
type Upstream struct {
	BaseURL  string          `json:"base_url"` // e.g. https://opencode.ai/zen/go or https://opencode.ai/zen/
	APIKey   string          `json:"api_key"`
	Name     string          `json:"name"`               // optional human label
	Enabled  bool            `json:"enabled"`            // skip when false
	Protocol string          `json:"protocol,omitempty"` // auto, openai, anthropic
	Models   []UpstreamModel `json:"models,omitempty"`   // supported target models/patterns for this upstream
}

// ResolvedUpstream is the concrete route for a single request.
type ResolvedUpstream struct {
	BaseURL       string
	APIKey        string
	Name          string
	Protocol      string
	IncomingModel string
	TargetModel   string
	Explicit      bool
}

// ThinkingBudgetMapping maps Anthropic extended-thinking budgets to model-
// specific OpenAI-compatible request fields. Field currently supports
// "thinking", "thinking_budget" and "reasoning_effort"; empty/"none" disables
// forwarding.
type ThinkingBudgetMapping struct {
	Match  string `json:"match"`
	Field  string `json:"field"`
	Low    int    `json:"low,omitempty"`
	Medium int    `json:"medium,omitempty"`
	High   int    `json:"high,omitempty"`
	Max    int    `json:"max,omitempty"`
}

// Config is the full application configuration.
type Config struct {
	ListenAddr string `json:"listen_addr"`
	// UpstreamBase is the Zen base URL without a trailing slash, e.g.
	// "https://opencode.ai/zen".
	UpstreamBase string `json:"upstream_base"`
	// NativeAnthropic enables smart native routing. Anthropic-native target
	// models (claude-*, qwen*) use <upstream>/v1/messages; other target models
	// are translated through OpenAI-compatible endpoints.
	NativeAnthropic bool `json:"native_anthropic"`
	// ZenAPIKey is the bearer token used to authenticate against Zen.
	ZenAPIKey string `json:"zen_api_key"`
	// Upstreams is the configured backend list. Model requests are routed by
	// resolving the global model_mappings target, then selecting the first
	// enabled upstream whose models list supports that target. When no upstream
	// declares models, the first enabled upstream is used for legacy single-
	// backend compatibility.
	Upstreams []Upstream `json:"upstreams"`
	// PanelToken gates access to the web panel and its API. If empty the panel
	// is open (convenient for local use). Set one before exposing the port.
	PanelToken string `json:"panel_token"`
	// RequireAPIKey gates the /v1/* proxy endpoints behind a valid client API
	// key. When true, requests must carry a Bearer key matching one in the
	// api_keys table. When false, access is open for local single-user use.
	RequireAPIKey bool `json:"require_api_key"`
	// DefaultModel is used when the incoming request has no model and no
	// mapping matches.
	DefaultModel string `json:"default_model"`
	// ModelMappings are evaluated in order; first match wins. The final
	// entry should have Match:"*" as a catch-all.
	ModelMappings []ModelMapping `json:"model_mappings"`
	// WebSearchModel optionally overrides the upstream model used by the
	// Anthropic web_search shim. Empty means use the resolved main target model.
	WebSearchModel string `json:"web_search_model"`
	// WebSearchMode controls Anthropic web_search handling:
	// auto uses native Messages routing when the target model is native-capable,
	// native forces pass-through to /v1/messages, and translate uses the proxy shim.
	WebSearchMode string `json:"web_search_mode"`
	// WebSearchBaseURL and WebSearchAPIKey optionally override the upstream
	// used by native web_search requests. Empty means reuse the selected main
	// upstream and key.
	WebSearchBaseURL string `json:"web_search_base_url"`
	WebSearchAPIKey  string `json:"web_search_api_key"`
	// LogRequests records each request/response to SQLite for the panel.
	LogRequests bool `json:"log_requests"`
	// MaxBodyLogBytes caps how much of a request/response body is stored.
	MaxBodyLogBytes int `json:"max_body_log_bytes"`
	// RequestTimeoutSeconds is the upstream timeout. 0 = no timeout (streams
	// can run long); a sane upper bound is still recommended.
	RequestTimeoutSeconds int `json:"request_timeout_seconds"`
	// PromptCacheEnabled enables request normalization and upstream prompt-cache
	// hints that improve cache hits without changing user-visible prompt text.
	PromptCacheEnabled bool `json:"prompt_cache_enabled"`
	// PromptCacheKeyPrefix prefixes automatically generated prompt_cache_key
	// values for OpenAI-compatible upstream requests.
	PromptCacheKeyPrefix string `json:"prompt_cache_key_prefix"`
	// PromptCacheAnthropicControl adds Anthropic cache_control markers when the
	// request has a stable system/tool prefix and no marker was provided.
	PromptCacheAnthropicControl bool `json:"prompt_cache_anthropic_control"`
	// PromptCacheNormalize keeps cacheable request structure stable: system
	// messages first, sorted tools/context blocks, and volatile metadata removed.
	PromptCacheNormalize bool `json:"prompt_cache_normalize"`
	// ThinkingBudgetMappings are evaluated by target model. They translate
	// Anthropic thinking budget_tokens into provider-specific request fields.
	ThinkingBudgetMappings []ThinkingBudgetMapping `json:"thinking_budget_mappings"`

	dataDir    string
	configPath string
	mu         sync.RWMutex
}

// Patch represents a partial update from the control panel. Pointer fields
// distinguish omitted JSON properties from explicit zero values.
type Patch struct {
	ListenAddr                  *string                  `json:"listen_addr"`
	UpstreamBase                *string                  `json:"upstream_base"`
	NativeAnthropic             *bool                    `json:"native_anthropic"`
	ZenAPIKey                   *string                  `json:"zen_api_key"`
	Upstreams                   *[]Upstream              `json:"upstreams"`
	PanelToken                  *string                  `json:"panel_token"`
	RequireAPIKey               *bool                    `json:"require_api_key"`
	DefaultModel                *string                  `json:"default_model"`
	ModelMappings               *[]ModelMapping          `json:"model_mappings"`
	WebSearchModel              *string                  `json:"web_search_model"`
	WebSearchMode               *string                  `json:"web_search_mode"`
	WebSearchBaseURL            *string                  `json:"web_search_base_url"`
	WebSearchAPIKey             *string                  `json:"web_search_api_key"`
	LogRequests                 *bool                    `json:"log_requests"`
	MaxBodyLogBytes             *int                     `json:"max_body_log_bytes"`
	RequestTimeoutSeconds       *int                     `json:"request_timeout_seconds"`
	PromptCacheEnabled          *bool                    `json:"prompt_cache_enabled"`
	PromptCacheKeyPrefix        *string                  `json:"prompt_cache_key_prefix"`
	PromptCacheAnthropicControl *bool                    `json:"prompt_cache_anthropic_control"`
	PromptCacheNormalize        *bool                    `json:"prompt_cache_normalize"`
	ThinkingBudgetMappings      *[]ThinkingBudgetMapping `json:"thinking_budget_mappings"`
}

// Default returns a Config populated with sensible defaults.
func Default() *Config {
	return &Config{
		ListenAddr:                  DefaultListenAddr,
		UpstreamBase:                DefaultUpstreamBase,
		NativeAnthropic:             true,
		RequireAPIKey:               false, // open by default for single-user local use
		DefaultModel:                DefaultDefaultModel,
		ModelMappings:               DefaultModelMappings(),
		WebSearchMode:               DefaultWebSearchMode,
		LogRequests:                 true,
		MaxBodyLogBytes:             1 << 14, // 16 KiB per body side
		RequestTimeoutSeconds:       0,
		PromptCacheEnabled:          true,
		PromptCacheKeyPrefix:        "opencode-cc",
		PromptCacheAnthropicControl: true,
		PromptCacheNormalize:        true,
		ThinkingBudgetMappings:      DefaultThinkingBudgetMappings(),
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

// DefaultThinkingBudgetMappings keeps provider-specific thinking controls off
// by default except where Zen-compatible model families document a matching
// extension field.
func DefaultThinkingBudgetMappings() []ThinkingBudgetMapping {
	return []ThinkingBudgetMapping{
		{Match: "glm-", Field: "thinking"},
		{Match: "kimi-", Field: "thinking_budget", Low: 1024, Medium: 4096, High: 8192, Max: 16384},
		{Match: "moonshot-", Field: "thinking_budget", Low: 1024, Medium: 4096, High: 8192, Max: 16384},
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
	c.migrateLegacyUpstream()
	return c, nil
}

// migrateLegacyUpstream promotes the legacy single UpstreamBase + ZenAPIKey
// pair into the Upstreams pool when the pool is empty. This makes pre-existing
// config.json files upgrade transparently. Idempotent.
func (c *Config) migrateLegacyUpstream() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.Upstreams) > 0 {
		return
	}
	if c.UpstreamBase != "" && c.ZenAPIKey != "" {
		c.Upstreams = []Upstream{{
			BaseURL: strings.TrimRight(c.UpstreamBase, "/"),
			APIKey:  c.ZenAPIKey,
			Enabled: true,
		}}
	}
}

// ResolveRequestRoute returns the upstream selected for one incoming model.
// The global ModelMappings table is the only client-model alias layer. After
// resolving that target model, per-upstream Models entries are used only as
// allowlists for target models. Requests are never round-robined.
func (c *Config) ResolveRequestRoute(in string) (ResolvedUpstream, bool) {
	c.mu.RLock()
	target := c.resolveModelLocked(in)
	hasModelScopedUpstreams := c.hasModelScopedUpstreamsLocked()
	if route, ok := c.routeForTargetLocked(in, target); ok {
		c.mu.RUnlock()
		return route, true
	}

	pool := make([]Upstream, 0, len(c.Upstreams))
	for _, u := range c.Upstreams {
		if u.Enabled && u.APIKey != "" {
			pool = append(pool, u)
		}
	}
	legacyBase, legacyKey := c.UpstreamBase, c.ZenAPIKey
	c.mu.RUnlock()

	if hasModelScopedUpstreams {
		return ResolvedUpstream{
			IncomingModel: in,
			TargetModel:   target,
			Protocol:      UpstreamProtocolAuto,
		}, false
	}

	if len(pool) > 0 {
		u := pool[0]
		return ResolvedUpstream{
			BaseURL:       strings.TrimRight(u.BaseURL, "/"),
			APIKey:        u.APIKey,
			Name:          u.Name,
			Protocol:      routeProtocolForTarget(normalizeUpstreamProtocol(u.Protocol), target),
			IncomingModel: in,
			TargetModel:   target,
		}, true
	}

	if legacyBase != "" && legacyKey != "" {
		return ResolvedUpstream{
			BaseURL:       strings.TrimRight(legacyBase, "/"),
			APIKey:        legacyKey,
			Protocol:      routeProtocolForTarget(UpstreamProtocolAuto, target),
			IncomingModel: in,
			TargetModel:   target,
		}, true
	}

	return ResolvedUpstream{
		IncomingModel: in,
		TargetModel:   target,
		Protocol:      UpstreamProtocolAuto,
	}, false
}

// HasExplicitModelRoutes reports whether at least one usable upstream declares
// supported target models.
func (c *Config) HasExplicitModelRoutes() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hasModelScopedUpstreamsLocked()
}

// ExplicitModelAliases returns concrete client-visible names from the global
// model mapping table. If no concrete aliases are configured, it falls back to
// concrete upstream target models. Wildcards are deliberately omitted.
func (c *Config) ExplicitModelAliases() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	seen := map[string]bool{}
	out := make([]string, 0)
	for _, m := range c.ModelMappings {
		alias := stripProviderPrefix(strings.TrimSpace(m.Match))
		if alias == "" || alias == "*" || strings.Contains(alias, "*") || seen[alias] {
			continue
		}
		seen[alias] = true
		out = append(out, alias)
	}
	if len(out) > 0 {
		return out
	}
	for _, u := range c.Upstreams {
		if !u.Enabled || u.APIKey == "" {
			continue
		}
		for _, m := range u.Models {
			model := upstreamModelPattern(m)
			if model == "" || strings.Contains(model, "*") || seen[model] {
				continue
			}
			seen[model] = true
			out = append(out, model)
		}
	}
	return out
}

// NextUpstream returns the first usable backend. It is retained for legacy
// callers such as live /v1/models lookup, but it no longer round-robins.
func (c *Config) NextUpstream() (base, key string, ok bool) {
	c.mu.RLock()
	for _, u := range c.Upstreams {
		if u.Enabled && u.APIKey != "" {
			c.mu.RUnlock()
			return strings.TrimRight(u.BaseURL, "/"), u.APIKey, true
		}
	}
	legacyBase, legacyKey := c.UpstreamBase, c.ZenAPIKey
	c.mu.RUnlock()

	if legacyBase != "" && legacyKey != "" {
		return strings.TrimRight(legacyBase, "/"), legacyKey, true
	}
	return "", "", false
}

// applyEnv overlays environment variables on top of the loaded config.
func (c *Config) applyEnv() {
	if v := os.Getenv("OPENCODE_CC_LISTEN"); v != "" {
		c.ListenAddr = v
	}
	if v := os.Getenv("OPENCODE_CC_UPSTREAM"); v != "" {
		c.UpstreamBase = strings.TrimRight(v, "/")
	}
	if v := os.Getenv("OPENCODE_CC_NATIVE_ANTHROPIC"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.NativeAnthropic = b
		}
	}
	if v := os.Getenv("ZEN_API_KEY"); v != "" {
		c.ZenAPIKey = v
	}
	if v := os.Getenv("OPENCODE_CC_PANEL_TOKEN"); v != "" {
		c.PanelToken = v
	}
	if v := os.Getenv("OPENCODE_CC_REQUIRE_API_KEY"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.RequireAPIKey = b
		}
	}
	if v := os.Getenv("OPENCODE_CC_DEFAULT_MODEL"); v != "" {
		c.DefaultModel = v
	}
	if v := os.Getenv("OPENCODE_CC_WEB_SEARCH_MODEL"); v != "" {
		c.WebSearchModel = stripProviderPrefix(strings.TrimSpace(v))
	}
	if v := os.Getenv("OPENCODE_CC_WEB_SEARCH_MODE"); v != "" {
		c.WebSearchMode = normalizeWebSearchMode(v)
	}
	if v := os.Getenv("OPENCODE_CC_WEB_SEARCH_BASE_URL"); v != "" {
		c.WebSearchBaseURL = strings.TrimRight(strings.TrimSpace(v), "/")
	}
	if v := os.Getenv("OPENCODE_CC_WEB_SEARCH_API_KEY"); v != "" {
		c.WebSearchAPIKey = strings.TrimSpace(v)
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
	if v := os.Getenv("OPENCODE_CC_PROMPT_CACHE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.PromptCacheEnabled = b
		}
	}
	if v := os.Getenv("OPENCODE_CC_PROMPT_CACHE_KEY_PREFIX"); v != "" {
		c.PromptCacheKeyPrefix = v
	}
	if v := os.Getenv("OPENCODE_CC_PROMPT_CACHE_ANTHROPIC_CONTROL"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.PromptCacheAnthropicControl = b
		}
	}
	if v := os.Getenv("OPENCODE_CC_PROMPT_CACHE_NORMALIZE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			c.PromptCacheNormalize = b
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
		ListenAddr:                  c.ListenAddr,
		UpstreamBase:                c.UpstreamBase,
		NativeAnthropic:             c.NativeAnthropic,
		ZenAPIKey:                   c.ZenAPIKey,
		Upstreams:                   cloneUpstreams(c.Upstreams),
		PanelToken:                  c.PanelToken,
		RequireAPIKey:               c.RequireAPIKey,
		DefaultModel:                c.DefaultModel,
		WebSearchModel:              c.WebSearchModel,
		WebSearchMode:               normalizeWebSearchMode(c.WebSearchMode),
		WebSearchBaseURL:            c.WebSearchBaseURL,
		WebSearchAPIKey:             c.WebSearchAPIKey,
		LogRequests:                 c.LogRequests,
		MaxBodyLogBytes:             c.MaxBodyLogBytes,
		RequestTimeoutSeconds:       c.RequestTimeoutSeconds,
		PromptCacheEnabled:          c.PromptCacheEnabled,
		PromptCacheKeyPrefix:        c.PromptCacheKeyPrefix,
		PromptCacheAnthropicControl: c.PromptCacheAnthropicControl,
		PromptCacheNormalize:        c.PromptCacheNormalize,
	}
	if c.ModelMappings != nil {
		cp.ModelMappings = append([]ModelMapping(nil), c.ModelMappings...)
	}
	if c.ThinkingBudgetMappings != nil {
		cp.ThinkingBudgetMappings = append([]ThinkingBudgetMapping(nil), c.ThinkingBudgetMappings...)
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
	return c.resolveModelLocked(in)
}

func (c *Config) resolveModelLocked(in string) string {
	bare := stripProviderPrefix(in)
	for _, m := range c.ModelMappings {
		if wildcard, ok := modelMappingMatches(m.Match, in, bare); ok {
			target := expandModelMappingTarget(m.Target, bare, wildcard)
			if target == "" {
				// Pass-through: use the (de-prefixed) incoming model name.
				if bare != "" {
					return bare
				}
				break // fall through to DefaultModel
			}
			return target
		}
	}
	if c.DefaultModel != "" {
		return c.DefaultModel
	}
	return DefaultDefaultModel
}

// ResolveWebSearchModel returns the configured upstream model for proxy-side
// web_search work. Empty config falls back to the main resolved target model.
func (c *Config) ResolveWebSearchModel(fallback string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	model := strings.TrimSpace(c.WebSearchModel)
	if model == "" {
		return fallback
	}
	return stripProviderPrefix(model)
}

// ResolveWebSearchMode returns the configured web_search routing mode.
func (c *Config) ResolveWebSearchMode() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return normalizeWebSearchMode(c.WebSearchMode)
}

// ResolveWebSearchUpstream returns the native web_search upstream and key.
// Empty web_search override fields fall back to the already-selected main
// upstream so existing deployments keep working.
func (c *Config) ResolveWebSearchUpstream(fallbackBaseURL, fallbackAPIKey string) (string, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	baseURL := strings.TrimRight(strings.TrimSpace(c.WebSearchBaseURL), "/")
	if baseURL == "" {
		baseURL = fallbackBaseURL
	}
	apiKey := strings.TrimSpace(c.WebSearchAPIKey)
	if apiKey == "" {
		apiKey = fallbackAPIKey
	}
	return baseURL, apiKey
}

// ResolveThinkingBudgetMapping returns the first budget mapping that matches
// the target model id. Provider prefixes are stripped before matching.
func (c *Config) ResolveThinkingBudgetMapping(model string) (ThinkingBudgetMapping, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	bare := stripProviderPrefix(model)
	for _, m := range c.ThinkingBudgetMappings {
		if m.Match == "*" || strings.HasPrefix(model, m.Match) || strings.HasPrefix(bare, m.Match) {
			return m, true
		}
	}
	return ThinkingBudgetMapping{}, false
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

func normalizeWebSearchMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", WebSearchModeAuto:
		return WebSearchModeAuto
	case WebSearchModeNative, "direct", "passthrough", "pass-through":
		return WebSearchModeNative
	case WebSearchModeTranslate, "shim", "proxy":
		return WebSearchModeTranslate
	default:
		return DefaultWebSearchMode
	}
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

// ApplyPatch merges an explicitly partial config update from the panel.
func (c *Config) ApplyPatch(src *Patch) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if src.ListenAddr != nil && *src.ListenAddr != "" {
		c.ListenAddr = *src.ListenAddr
	}
	if src.UpstreamBase != nil && *src.UpstreamBase != "" {
		c.UpstreamBase = strings.TrimRight(*src.UpstreamBase, "/")
	}
	if src.NativeAnthropic != nil {
		c.NativeAnthropic = *src.NativeAnthropic
	}
	// ZenAPIKey: empty means "don't change" so the panel never clobbers it.
	if src.ZenAPIKey != nil && *src.ZenAPIKey != "" {
		c.ZenAPIKey = *src.ZenAPIKey
	}
	// Upstreams pool: when provided, replace wholesale. Per-item APIKey uses
	// the "empty = keep existing key" sentinel so masked edits don't wipe keys.
	if src.Upstreams != nil {
		next := *src.Upstreams
		// Preserve existing keys where the patch left them blank, matching by
		// position (the panel sends the full ordered list back).
		prev := c.Upstreams
		for i := range next {
			if next[i].APIKey == "" && i < len(prev) && prev[i].APIKey != "" {
				next[i].APIKey = prev[i].APIKey
			}
			next[i].BaseURL = strings.TrimRight(next[i].BaseURL, "/")
			next[i].Protocol = normalizeUpstreamProtocol(next[i].Protocol)
			next[i].Models = normalizeUpstreamModels(next[i].Models)
		}
		c.Upstreams = cloneUpstreams(next)
	}
	if src.PanelToken != nil {
		c.PanelToken = *src.PanelToken
	}
	if src.RequireAPIKey != nil {
		c.RequireAPIKey = *src.RequireAPIKey
	}
	if src.DefaultModel != nil && *src.DefaultModel != "" {
		c.DefaultModel = *src.DefaultModel
	}
	if src.ModelMappings != nil {
		c.ModelMappings = append([]ModelMapping(nil), (*src.ModelMappings)...)
	}
	if src.WebSearchModel != nil {
		c.WebSearchModel = stripProviderPrefix(strings.TrimSpace(*src.WebSearchModel))
	}
	if src.WebSearchMode != nil {
		c.WebSearchMode = normalizeWebSearchMode(*src.WebSearchMode)
	}
	if src.WebSearchBaseURL != nil {
		c.WebSearchBaseURL = strings.TrimRight(strings.TrimSpace(*src.WebSearchBaseURL), "/")
	}
	if src.WebSearchAPIKey != nil && strings.TrimSpace(*src.WebSearchAPIKey) != "" {
		c.WebSearchAPIKey = strings.TrimSpace(*src.WebSearchAPIKey)
	}
	if src.LogRequests != nil {
		c.LogRequests = *src.LogRequests
	}
	if src.MaxBodyLogBytes != nil && *src.MaxBodyLogBytes >= 0 {
		c.MaxBodyLogBytes = *src.MaxBodyLogBytes
	}
	if src.RequestTimeoutSeconds != nil && *src.RequestTimeoutSeconds >= 0 {
		c.RequestTimeoutSeconds = *src.RequestTimeoutSeconds
	}
	if src.PromptCacheEnabled != nil {
		c.PromptCacheEnabled = *src.PromptCacheEnabled
	}
	if src.PromptCacheKeyPrefix != nil {
		c.PromptCacheKeyPrefix = *src.PromptCacheKeyPrefix
	}
	if src.PromptCacheAnthropicControl != nil {
		c.PromptCacheAnthropicControl = *src.PromptCacheAnthropicControl
	}
	if src.PromptCacheNormalize != nil {
		c.PromptCacheNormalize = *src.PromptCacheNormalize
	}
	if src.ThinkingBudgetMappings != nil {
		c.ThinkingBudgetMappings = append([]ThinkingBudgetMapping(nil), (*src.ThinkingBudgetMappings)...)
	}
}

func cloneUpstreams(in []Upstream) []Upstream {
	if in == nil {
		return nil
	}
	out := append([]Upstream(nil), in...)
	for i := range out {
		out[i].Protocol = normalizeUpstreamProtocol(out[i].Protocol)
		out[i].Models = append([]UpstreamModel(nil), out[i].Models...)
	}
	return out
}

func (c *Config) hasModelScopedUpstreamsLocked() bool {
	for _, u := range c.Upstreams {
		if u.Enabled && u.APIKey != "" && len(u.Models) > 0 {
			return true
		}
	}
	return false
}

func (c *Config) routeForTargetLocked(in, target string) (ResolvedUpstream, bool) {
	for _, u := range c.Upstreams {
		if !u.Enabled || u.APIKey == "" || len(u.Models) == 0 {
			continue
		}
		if !upstreamSupportsTarget(u.Models, target) {
			continue
		}
		protocol := routeProtocolForTarget(normalizeUpstreamProtocol(u.Protocol), target)
		return ResolvedUpstream{
			BaseURL:       strings.TrimRight(u.BaseURL, "/"),
			APIKey:        u.APIKey,
			Name:          u.Name,
			Protocol:      protocol,
			IncomingModel: in,
			TargetModel:   target,
			Explicit:      true,
		}, true
	}
	return ResolvedUpstream{}, false
}

func modelMappingMatches(pattern, in, bare string) (string, bool) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", false
	}
	for _, value := range modelMatchCandidates(pattern, in, bare) {
		if wildcard, ok := modelWildcardCapture(pattern, value); ok {
			return wildcard, true
		}
	}
	if strings.Contains(pattern, "*") {
		barePattern := stripProviderPrefix(pattern)
		for _, value := range modelMatchCandidates(barePattern, in, bare) {
			if wildcard, ok := modelWildcardCapture(barePattern, value); ok {
				return wildcard, true
			}
		}
		return "", false
	}
	barePattern := stripProviderPrefix(pattern)
	if strings.HasPrefix(in, pattern) || strings.HasPrefix(bare, pattern) ||
		strings.HasPrefix(in, barePattern) || strings.HasPrefix(bare, barePattern) {
		return "", true
	}
	return "", false
}

func expandModelMappingTarget(target, bare, wildcard string) string {
	target = stripProviderPrefix(strings.TrimSpace(target))
	if target == "" || !strings.Contains(target, "*") {
		return target
	}
	if wildcard == "" {
		wildcard = bare
	}
	if wildcard == "" {
		return ""
	}
	return strings.ReplaceAll(target, "*", wildcard)
}

func upstreamSupportsTarget(models []UpstreamModel, target string) bool {
	target = strings.TrimSpace(target)
	bare := stripProviderPrefix(target)
	if target == "" && bare == "" {
		return false
	}
	for _, m := range models {
		if modelPatternMatchesTarget(upstreamModelPattern(m), target, bare) {
			return true
		}
	}
	return false
}

func upstreamModelPattern(m UpstreamModel) string {
	for _, value := range []string{m.Name, m.Target, m.Alias, m.Match} {
		if pattern := stripProviderPrefix(strings.TrimSpace(value)); pattern != "" {
			return pattern
		}
	}
	return ""
}

func modelPatternMatchesTarget(pattern, target, bare string) bool {
	pattern = stripProviderPrefix(strings.TrimSpace(pattern))
	if pattern == "" {
		return false
	}
	for _, value := range modelMatchCandidates(pattern, target, bare) {
		if strings.Contains(pattern, "*") {
			if modelGlobMatches(pattern, value) {
				return true
			}
			continue
		}
		if value == pattern {
			return true
		}
	}
	return false
}

func modelMatchCandidates(pattern, in, bare string) []string {
	if strings.Contains(pattern, "/") {
		return []string{in, bare}
	}
	return []string{bare, in}
}

func modelWildcardCapture(pattern, value string) (string, bool) {
	pattern = strings.TrimSpace(pattern)
	if !strings.Contains(pattern, "*") {
		return "", false
	}
	if pattern == "*" {
		return value, true
	}
	if strings.Count(pattern, "*") != 1 {
		return "", modelGlobMatches(pattern, value)
	}
	parts := strings.SplitN(pattern, "*", 2)
	prefix, suffix := parts[0], parts[1]
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, suffix) {
		return "", false
	}
	if len(value) < len(prefix)+len(suffix) {
		return "", false
	}
	return value[len(prefix) : len(value)-len(suffix)], true
}

func modelGlobMatches(pattern, value string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 0 {
		return value == ""
	}
	pos := 0
	for i, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(value[pos:], part)
		if idx < 0 {
			return false
		}
		if i == 0 && !strings.HasPrefix(value, part) {
			return false
		}
		pos += idx + len(part)
	}
	last := parts[len(parts)-1]
	return last == "" || strings.HasSuffix(value, last)
}

func normalizeUpstreamModels(in []UpstreamModel) []UpstreamModel {
	if in == nil {
		return nil
	}
	out := make([]UpstreamModel, 0, len(in))
	for _, m := range in {
		m.Name = stripProviderPrefix(strings.TrimSpace(m.Name))
		m.Alias = stripProviderPrefix(strings.TrimSpace(m.Alias))
		m.Match = strings.TrimSpace(m.Match)
		m.Target = stripProviderPrefix(strings.TrimSpace(m.Target))
		if m.Name == "" && m.Target == "" && m.Alias == "" && m.Match == "" {
			continue
		}
		out = append(out, m)
	}
	return out
}

func normalizeUpstreamProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "", UpstreamProtocolAuto:
		return UpstreamProtocolAuto
	case UpstreamProtocolAnthropic, "messages", "anthropic-messages", "claude":
		return UpstreamProtocolAnthropic
	case UpstreamProtocolOpenAI, "chat", "chat_completions", "chat-completions", "openai-chat":
		return UpstreamProtocolOpenAI
	default:
		return UpstreamProtocolAuto
	}
}

func routeProtocolForTarget(protocol, target string) string {
	_ = target
	return normalizeUpstreamProtocol(protocol)
}

func isNativeAnthropicTarget(model string) bool {
	bare := stripProviderPrefix(strings.ToLower(strings.TrimSpace(model)))
	return strings.HasPrefix(bare, "claude-") || strings.HasPrefix(bare, "qwen")
}
