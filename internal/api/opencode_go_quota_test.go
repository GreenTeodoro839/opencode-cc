package api

import (
	"testing"
	"time"

	"github.com/Kiowx/opencode-cc/internal/config"
)

func TestParseOpenCodeGoQuotaHTMLAllWindows(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	html := `<script>
rollingUsage:$R[0]={usagePercent:65.5,resetInSec:3600};
weeklyUsage:$R[1]={resetInSec:7200,usagePercent:42};
monthlyUsage:$R[2]={usagePercent:10.2,resetInSec:86400};
</script>`

	got := parseOpenCodeGoQuotaHTML(html, now)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3: %#v", len(got), got)
	}
	if got[0].Label != openCodeGoLabelRolling || got[0].Used != 65.5 || got[0].Remaining != 34.5 || got[0].ResetInSec != 3600 {
		t.Fatalf("rolling parsed wrong: %#v", got[0])
	}
	if got[1].Label != openCodeGoLabelWeekly || got[1].Used != 42 || got[1].ResetInSec != 7200 {
		t.Fatalf("weekly parsed wrong: %#v", got[1])
	}
	if got[2].Label != openCodeGoLabelMonthly || got[2].Used != 10.2 || got[2].ResetInSec != 86400 {
		t.Fatalf("monthly parsed wrong: %#v", got[2])
	}
}

func TestBuildOpenCodeGoDashboardURLEncodesWorkspaceAsOneSegment(t *testing.T) {
	got, err := buildOpenCodeGoDashboardURL("team/foo bar?x=1#frag")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://opencode.ai/workspace/team%2Ffoo%20bar%3Fx=1%23frag/go"
	if got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
}

func TestOpenCodeGoWorkspaceOrDefault(t *testing.T) {
	if got := openCodeGoWorkspaceOrDefault(" "); got != openCodeGoDefaultWorkspaceID {
		t.Fatalf("empty workspace = %q, want %q", got, openCodeGoDefaultWorkspaceID)
	}
	if got := openCodeGoWorkspaceOrDefault(" custom "); got != "custom" {
		t.Fatalf("workspace = %q, want custom", got)
	}
}

func TestExtractOpenCodeGoWorkspaceID(t *testing.T) {
	cases := map[string]string{
		"wrk_01KVCFA6E59VXGSMPPB08M3S8N":              "wrk_01KVCFA6E59VXGSMPPB08M3S8N",
		"https://opencode.ai/workspace/wrk_ABC123/go": "wrk_ABC123",
		"workspace=wrk_DEF456":                        "wrk_DEF456",
		"Default":                                     "",
	}
	for in, want := range cases {
		if got := extractOpenCodeGoWorkspaceID(in); got != want {
			t.Fatalf("extractOpenCodeGoWorkspaceID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseOpenCodeGoWorkspaceRefs(t *testing.T) {
	text := `;0x00000095;((self.$R=self.$R||{})["server-fn:test"]=[],($R=>$R[0]=[$R[1]={id:"wrk_01",name:"Default",slug:null},$R[2]={id:"wrk_02",name:"Team",slug:null}])($R["server-fn:test"]))`
	got := parseOpenCodeGoWorkspaceRefs(text)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %#v", len(got), got)
	}
	if got[0].ID != "wrk_01" || got[0].Name != "Default" || got[1].ID != "wrk_02" || got[1].Name != "Team" {
		t.Fatalf("refs parsed wrong: %#v", got)
	}
}

func TestBuildOpenCodeGoCookieHeader(t *testing.T) {
	if got := buildOpenCodeGoCookieHeader("secret"); got != "auth=secret" {
		t.Fatalf("bare cookie = %q", got)
	}
	full := "foo=bar; auth=secret; theme=dark"
	if got := buildOpenCodeGoCookieHeader(full); got != "auth=secret" {
		t.Fatalf("full cookie = %q", got)
	}
	withHeader := "Cookie: auth=secret"
	if got := buildOpenCodeGoCookieHeader(withHeader); got != "auth=secret" {
		t.Fatalf("cookie header = %q", got)
	}
}

func TestFilterOpenCodeGoWindowsRespectsVisibility(t *testing.T) {
	no := false
	windows := []openCodeGoQuotaWindow{
		{Label: openCodeGoLabelRolling},
		{Label: openCodeGoLabelWeekly},
		{Label: openCodeGoLabelMonthly},
	}
	got := filterOpenCodeGoWindows(windows, config.Upstream{
		OpenCodeGoShowWeekly: &no,
	})
	if len(got) != 2 || got[0].Label != openCodeGoLabelRolling || got[1].Label != openCodeGoLabelMonthly {
		t.Fatalf("filtered windows wrong: %#v", got)
	}
}
