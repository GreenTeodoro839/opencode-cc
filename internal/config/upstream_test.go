package config

import (
	"testing"
)

// TestNextUpstreamRoundRobin verifies requests cycle through the enabled pool
// in order, skipping disabled/empty-key entries.
func TestNextUpstreamRoundRobin(t *testing.T) {
	c := Default()
	c.Upstreams = []Upstream{
		{BaseURL: "https://a.example", APIKey: "ka", Enabled: true},
		{BaseURL: "https://b.example", APIKey: "kb", Enabled: true},
		{BaseURL: "https://c.example", APIKey: "kc", Enabled: false}, // disabled, skipped
		{BaseURL: "https://d.example", APIKey: "", Enabled: true},    // empty key, skipped
	}

	seen := map[string]int{}
	for i := 0; i < 6; i++ {
		base, key, ok := c.NextUpstream()
		if !ok {
			t.Fatalf("request %d: expected ok", i)
		}
		seen[base]++
		// key must match the base
		want := "ka"
		if base == "https://b.example" {
			want = "kb"
		}
		if key != want {
			t.Errorf("base %s: got key %q want %q", base, key, want)
		}
	}
	// 6 requests over 2 enabled upstreams → 3 each
	if seen["https://a.example"] != 3 || seen["https://b.example"] != 3 {
		t.Errorf("expected 3/3 across a and b, got %v", seen)
	}
	// disabled/empty must never be selected
	if _, hit := seen["https://c.example"]; hit {
		t.Errorf("disabled upstream c was selected")
	}
	if _, hit := seen["https://d.example"]; hit {
		t.Errorf("empty-key upstream d was selected")
	}
}

// TestNextUpstreamLegacyFallback confirms the pool-empty case falls back to the
// legacy single UpstreamBase/ZenAPIKey fields (so existing configs keep working).
func TestNextUpstreamLegacyFallback(t *testing.T) {
	c := Default()
	c.Upstreams = nil
	c.UpstreamBase = "https://legacy.example/"
	c.ZenAPIKey = "legacy-key"

	base, key, ok := c.NextUpstream()
	if !ok {
		t.Fatalf("expected ok via legacy fallback")
	}
	if base != "https://legacy.example" {
		t.Errorf("base = %q, want https://legacy.example (trailing slash trimmed)", base)
	}
	if key != "legacy-key" {
		t.Errorf("key = %q, want legacy-key", key)
	}
}

// TestNextUpstreamNoneConfigured returns ok=false when nothing is set.
func TestNextUpstreamNoneConfigured(t *testing.T) {
	c := Default()
	c.Upstreams = nil
	c.UpstreamBase = ""
	c.ZenAPIKey = ""
	if _, _, ok := c.NextUpstream(); ok {
		t.Errorf("expected ok=false with no upstream configured")
	}
}

// TestMigrateLegacyUpstream promotes the legacy pair into the pool exactly once.
func TestMigrateLegacyUpstream(t *testing.T) {
	c := Default()
	c.UpstreamBase = "https://opencode.ai/zen/go"
	c.ZenAPIKey = "sk-test"
	c.Upstreams = nil

	c.migrateLegacyUpstream()
	if len(c.Upstreams) != 1 {
		t.Fatalf("expected 1 upstream after migration, got %d", len(c.Upstreams))
	}
	u := c.Upstreams[0]
	if u.BaseURL != "https://opencode.ai/zen/go" || u.APIKey != "sk-test" || !u.Enabled {
		t.Errorf("migrated upstream wrong: %+v", u)
	}

	// Idempotent: running again must not duplicate.
	c.migrateLegacyUpstream()
	if len(c.Upstreams) != 1 {
		t.Errorf("migration not idempotent: got %d upstreams", len(c.Upstreams))
	}
}

// TestMigrateLegacyUpstreamSkipsWhenPoolPresent ensures we never clobber an
// existing pool with the legacy fields.
func TestMigrateLegacyUpstreamSkipsWhenPoolPresent(t *testing.T) {
	c := Default()
	c.UpstreamBase = "https://legacy.example"
	c.ZenAPIKey = "legacy-key"
	c.Upstreams = []Upstream{{BaseURL: "https://pool.example", APIKey: "pk", Enabled: true}}

	c.migrateLegacyUpstream()
	if len(c.Upstreams) != 1 || c.Upstreams[0].BaseURL != "https://pool.example" {
		t.Errorf("pool clobbered by migration: %+v", c.Upstreams)
	}
}

func TestApplyPatchPreservesAPIKeyWhenBlank(t *testing.T) {
	c := Default()
	c.Upstreams = []Upstream{{
		BaseURL: "https://old.example/",
		APIKey:  "old-api-key",
		Enabled: true,
	}}

	next := []Upstream{{
		BaseURL: "https://new.example/",
		APIKey:  "",
		Enabled: true,
	}}
	c.ApplyPatch(&Patch{Upstreams: &next})

	got := c.Snapshot().Upstreams[0]
	if got.APIKey != "old-api-key" {
		t.Fatalf("API key was not preserved: %+v", got)
	}
	if got.BaseURL != "https://new.example" {
		t.Fatalf("base URL was not trimmed/updated: %+v", got)
	}
}

func TestResolveRequestRouteUsesExplicitUpstreamModels(t *testing.T) {
	c := Default()
	c.Upstreams = []Upstream{
		{
			BaseURL:  "https://deepseek.example/",
			APIKey:   "deepseek-key",
			Enabled:  true,
			Protocol: UpstreamProtocolAnthropic,
			Models: []UpstreamModel{{
				Name:  "deepseek-chat",
				Alias: "claude-sonnet",
			}},
		},
		{
			BaseURL:  "https://glm.example",
			APIKey:   "glm-key",
			Enabled:  true,
			Protocol: UpstreamProtocolOpenAI,
			Models: []UpstreamModel{{
				Name:  "glm-4.6",
				Alias: "glm-coder",
			}},
		},
	}
	c.ModelMappings = []ModelMapping{{Match: "*", Target: "fallback-model"}}

	for i := 0; i < 4; i++ {
		route, ok := c.ResolveRequestRoute("anthropic/claude-sonnet")
		if !ok {
			t.Fatalf("route %d not found", i)
		}
		if route.BaseURL != "https://deepseek.example" ||
			route.APIKey != "deepseek-key" ||
			route.TargetModel != "deepseek-chat" ||
			route.Protocol != UpstreamProtocolAnthropic ||
			!route.Explicit {
			t.Fatalf("unexpected explicit route: %+v", route)
		}
	}

	route, ok := c.ResolveRequestRoute("glm-coder")
	if !ok {
		t.Fatal("glm route not found")
	}
	if route.BaseURL != "https://glm.example" ||
		route.APIKey != "glm-key" ||
		route.TargetModel != "glm-4.6" ||
		route.Protocol != UpstreamProtocolOpenAI {
		t.Fatalf("unexpected glm route: %+v", route)
	}

	if _, ok := c.ResolveRequestRoute("not-configured"); ok {
		t.Fatal("unconfigured model should not fall back when explicit routes exist")
	}
}

func TestResolveRequestRouteExpandsWildcardModelName(t *testing.T) {
	c := Default()
	c.Upstreams = []Upstream{{
		BaseURL:  "https://wildcard.example",
		APIKey:   "wildcard-key",
		Enabled:  true,
		Protocol: UpstreamProtocolAnthropic,
		Models: []UpstreamModel{{
			Name: "*",
		}},
	}}

	route, ok := c.ResolveRequestRoute("anthropic/deepseek-chat")
	if !ok {
		t.Fatal("wildcard route not found")
	}
	if route.TargetModel != "deepseek-chat" {
		t.Fatalf("target model = %q, want deepseek-chat", route.TargetModel)
	}
	if route.BaseURL != "https://wildcard.example" || route.APIKey != "wildcard-key" {
		t.Fatalf("unexpected route: %+v", route)
	}
}

func TestResolveRequestRouteExpandsWildcardAliasIntoTarget(t *testing.T) {
	c := Default()
	c.Upstreams = []Upstream{{
		BaseURL: "https://template.example",
		APIKey:  "template-key",
		Enabled: true,
		Models: []UpstreamModel{{
			Alias: "claude-*",
			Name:  "deepseek-*",
		}},
	}}

	route, ok := c.ResolveRequestRoute("claude-sonnet")
	if !ok {
		t.Fatal("template route not found")
	}
	if route.TargetModel != "deepseek-sonnet" {
		t.Fatalf("target model = %q, want deepseek-sonnet", route.TargetModel)
	}
	if _, ok := c.ResolveRequestRoute("glm-4.6"); ok {
		t.Fatal("non-matching model should not use wildcard template route")
	}
}

func TestExplicitModelAliases(t *testing.T) {
	c := Default()
	c.Upstreams = []Upstream{
		{
			BaseURL: "https://a.example",
			APIKey:  "ka",
			Enabled: true,
			Models: []UpstreamModel{
				{Name: "deepseek-chat", Alias: "claude-sonnet"},
				{Name: "glm-4.6"},
			},
		},
		{
			BaseURL: "https://b.example",
			APIKey:  "kb",
			Enabled: true,
			Models:  []UpstreamModel{{Name: "deepseek-chat", Alias: "claude-sonnet"}},
		},
	}

	got := c.ExplicitModelAliases()
	if len(got) != 2 || got[0] != "claude-sonnet" || got[1] != "glm-4.6" {
		t.Fatalf("aliases = %+v", got)
	}
}

func TestExplicitModelAliasesSkipsWildcardRoutes(t *testing.T) {
	c := Default()
	c.Upstreams = []Upstream{{
		BaseURL: "https://wildcard.example",
		APIKey:  "wildcard-key",
		Enabled: true,
		Models: []UpstreamModel{
			{Name: "*"},
			{Alias: "claude-*", Name: "deepseek-*"},
			{Alias: "glm-coder", Name: "glm-4.6"},
		},
	}}

	got := c.ExplicitModelAliases()
	if len(got) != 1 || got[0] != "glm-coder" {
		t.Fatalf("aliases = %+v, want only glm-coder", got)
	}
}
