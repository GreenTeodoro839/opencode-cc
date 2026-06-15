package config

import "testing"

func TestResolveModelStripsProviderPrefix(t *testing.T) {
	c := Default()
	c.DefaultModel = "glm-5.1"
	c.ModelMappings = []ModelMapping{{Match: "*", Target: ""}} // pass-through

	cases := []struct{ in, want string }{
		{"anthropic/kimi-k2.7-code", "kimi-k2.7-code"}, // Claude Code style
		{"openai/gpt-5.2", "gpt-5.2"},
		{"kimi-k2.7-code", "kimi-k2.7-code"},          // already bare
		{"glm-5.1", "glm-5.1"},                         // already bare
		{"anthropic/claude-sonnet-4-5", "claude-sonnet-4-5"},
	}
	for _, tc := range cases {
		got := c.ResolveModel(tc.in)
		if got != tc.want {
			t.Errorf("ResolveModel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveModelExplicitMappingWins(t *testing.T) {
	c := Default()
	c.ModelMappings = []ModelMapping{
		{Match: "claude-3-5-sonnet", Target: "glm-5.1"},
		{Match: "*", Target: ""},
	}
	if got := c.ResolveModel("claude-3-5-sonnet-20241022"); got != "glm-5.1" {
		t.Errorf("explicit mapping: got %q, want glm-5.1", got)
	}
	if got := c.ResolveModel("anthropic/kimi-k2.7-code"); got != "kimi-k2.7-code" {
		t.Errorf("pass-through w/ prefix: got %q, want kimi-k2.7-code", got)
	}
}
