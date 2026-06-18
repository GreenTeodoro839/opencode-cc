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
	if u.OpenCodeGoWorkspaceID != DefaultOpenCodeGoWorkspaceID {
		t.Errorf("migrated OpenCode Go workspace = %q, want %q", u.OpenCodeGoWorkspaceID, DefaultOpenCodeGoWorkspaceID)
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

func TestApplyPatchPreservesOpenCodeGoQuotaSecretsAndVisibility(t *testing.T) {
	showRolling := true
	showWeekly := false
	showMonthly := true
	c := Default()
	c.Upstreams = []Upstream{{
		BaseURL:               "https://old.example/",
		APIKey:                "old-api-key",
		Enabled:               true,
		OpenCodeGoWorkspaceID: "old-workspace",
		OpenCodeGoAuthCookie:  "auth=old-cookie",
		OpenCodeGoShowRolling: &showRolling,
		OpenCodeGoShowWeekly:  &showWeekly,
		OpenCodeGoShowMonthly: &showMonthly,
	}}

	next := []Upstream{{
		BaseURL:               "https://new.example/",
		APIKey:                "",
		Enabled:               true,
		OpenCodeGoWorkspaceID: " new-workspace ",
		OpenCodeGoAuthCookie:  "",
	}}
	c.ApplyPatch(&Patch{Upstreams: &next})

	got := c.Snapshot().Upstreams[0]
	if got.APIKey != "old-api-key" {
		t.Fatalf("API key was not preserved: %+v", got)
	}
	if got.OpenCodeGoAuthCookie != "auth=old-cookie" {
		t.Fatalf("OpenCode Go cookie was not preserved: %+v", got)
	}
	if got.OpenCodeGoWorkspaceID != "new-workspace" {
		t.Fatalf("workspace was not trimmed/updated: %+v", got)
	}
	if got.OpenCodeGoShowRolling == nil || *got.OpenCodeGoShowRolling != true ||
		got.OpenCodeGoShowWeekly == nil || *got.OpenCodeGoShowWeekly != false ||
		got.OpenCodeGoShowMonthly == nil || *got.OpenCodeGoShowMonthly != true {
		t.Fatalf("OpenCode Go visibility flags were not preserved: %+v", got)
	}
}

func TestApplyPatchDefaultsEmptyOpenCodeGoWorkspace(t *testing.T) {
	c := Default()
	next := []Upstream{{
		BaseURL:               "https://pool.example/",
		APIKey:                "pk",
		Enabled:               true,
		OpenCodeGoWorkspaceID: " ",
		OpenCodeGoAuthCookie:  "auth=cookie",
	}}

	c.ApplyPatch(&Patch{Upstreams: &next})
	got := c.Snapshot().Upstreams[0]
	if got.OpenCodeGoWorkspaceID != DefaultOpenCodeGoWorkspaceID {
		t.Fatalf("workspace = %q, want %q", got.OpenCodeGoWorkspaceID, DefaultOpenCodeGoWorkspaceID)
	}
}

func TestSnapshotDeepCopiesOpenCodeGoVisibilityPointers(t *testing.T) {
	showRolling := true
	c := Default()
	c.Upstreams = []Upstream{{
		BaseURL:               "https://pool.example",
		APIKey:                "pk",
		Enabled:               true,
		OpenCodeGoShowRolling: &showRolling,
	}}

	snap := c.Snapshot()
	*snap.Upstreams[0].OpenCodeGoShowRolling = false

	got := c.Snapshot().Upstreams[0]
	if got.OpenCodeGoShowRolling == nil || *got.OpenCodeGoShowRolling != true {
		t.Fatalf("snapshot mutation leaked into config: %+v", got)
	}
}

func TestApplyPatchDeepCopiesOpenCodeGoVisibilityPointers(t *testing.T) {
	showRolling := true
	c := Default()
	next := []Upstream{{
		BaseURL:               "https://pool.example",
		APIKey:                "pk",
		Enabled:               true,
		OpenCodeGoShowRolling: &showRolling,
	}}

	c.ApplyPatch(&Patch{Upstreams: &next})
	showRolling = false

	got := c.Snapshot().Upstreams[0]
	if got.OpenCodeGoShowRolling == nil || *got.OpenCodeGoShowRolling != true {
		t.Fatalf("patch pointer mutation leaked into config: %+v", got)
	}
}
