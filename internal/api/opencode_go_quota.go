package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Kiowx/opencode-cc/internal/config"
)

const (
	openCodeGoDashboardBase      = "https://opencode.ai/workspace"
	openCodeGoWorkspaceServerID  = "def39973159c7f0483d8793a822b8dbb10d067e12c65455fcb4608459ba0234f"
	openCodeGoDefaultWorkspaceID = "Default"
	openCodeGoUserAgent          = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Gecko/20100101 Firefox/148.0"
	openCodeGoTimeout            = 10 * time.Second
	openCodeGoMaxHTMLBytes       = 4 << 20

	openCodeGoLabelRolling = "5h Rolling"
	openCodeGoLabelWeekly  = "Weekly"
	openCodeGoLabelMonthly = "Monthly"
)

var (
	reRollingPctFirst          = regexp.MustCompile(`rollingUsage:\s*\$R\[\d+\]\s*=\s*\{[^}]*usagePercent\s*:\s*(-?\d+(?:\.\d+)?)[^}]*resetInSec\s*:\s*(-?\d+(?:\.\d+)?)[^}]*\}`)
	reRollingResetFirst        = regexp.MustCompile(`rollingUsage:\s*\$R\[\d+\]\s*=\s*\{[^}]*resetInSec\s*:\s*(-?\d+(?:\.\d+)?)[^}]*usagePercent\s*:\s*(-?\d+(?:\.\d+)?)[^}]*\}`)
	reWeeklyPctFirst           = regexp.MustCompile(`weeklyUsage:\s*\$R\[\d+\]\s*=\s*\{[^}]*usagePercent\s*:\s*(-?\d+(?:\.\d+)?)[^}]*resetInSec\s*:\s*(-?\d+(?:\.\d+)?)[^}]*\}`)
	reWeeklyResetFirst         = regexp.MustCompile(`weeklyUsage:\s*\$R\[\d+\]\s*=\s*\{[^}]*resetInSec\s*:\s*(-?\d+(?:\.\d+)?)[^}]*usagePercent\s*:\s*(-?\d+(?:\.\d+)?)[^}]*\}`)
	reMonthlyPctFirst          = regexp.MustCompile(`monthlyUsage:\s*\$R\[\d+\]\s*=\s*\{[^}]*usagePercent\s*:\s*(-?\d+(?:\.\d+)?)[^}]*resetInSec\s*:\s*(-?\d+(?:\.\d+)?)[^}]*\}`)
	reMonthlyResetFirst        = regexp.MustCompile(`monthlyUsage:\s*\$R\[\d+\]\s*=\s*\{[^}]*resetInSec\s*:\s*(-?\d+(?:\.\d+)?)[^}]*usagePercent\s*:\s*(-?\d+(?:\.\d+)?)[^}]*\}`)
	reOpenCodeGoWorkspaceID    = regexp.MustCompile(`wrk_[A-Za-z0-9]+`)
	reOpenCodeGoWorkspaceEntry = regexp.MustCompile(`(?s)id\s*:\s*"(wrk_[^"]+)"[^{}]*?name\s*:\s*"([^"]*)"`)
)

type openCodeGoScrapedWindow struct {
	usagePercent float64
	resetInSec   int64
}

type openCodeGoQuotaWindow struct {
	Label      string  `json:"label"`
	Used       float64 `json:"used"`
	Remaining  float64 `json:"remaining"`
	Total      float64 `json:"total"`
	Unit       string  `json:"unit"`
	ResetAt    string  `json:"reset_at"`
	ResetInSec int64   `json:"reset_in_sec"`
}

type openCodeGoQuotaAccount struct {
	Index     int                     `json:"index"`
	Name      string                  `json:"name"`
	BaseURL   string                  `json:"base_url"`
	Enabled   bool                    `json:"enabled"`
	Success   bool                    `json:"success"`
	Error     string                  `json:"error,omitempty"`
	Windows   []openCodeGoQuotaWindow `json:"windows,omitempty"`
	UpdatedAt string                  `json:"updated_at"`
}

type openCodeGoWorkspaceRef struct {
	ID   string
	Name string
}

func (a *API) opencodeGoQuota(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	snap := a.cfg.Snapshot()
	out := make([]openCodeGoQuotaAccount, 0, len(snap.Upstreams))
	now := time.Now().UTC()
	for i, up := range snap.Upstreams {
		workspaceID := strings.TrimSpace(up.OpenCodeGoWorkspaceID)
		authCookie := strings.TrimSpace(up.OpenCodeGoAuthCookie)
		if !up.Enabled {
			continue
		}
		// Quota display is optional. If neither field is present, the upstream
		// is simply not configured for quota display. If the user saved a cookie
		// but left Workspace ID blank, OpenCode Go's dashboard default is
		// /workspace/Default/go.
		if workspaceID == "" && authCookie == "" {
			continue
		}
		workspaceID = openCodeGoWorkspaceOrDefault(workspaceID)

		account := openCodeGoQuotaAccount{
			Index:     i,
			Name:      up.Name,
			BaseURL:   up.BaseURL,
			Enabled:   up.Enabled,
			UpdatedAt: now.Format(time.RFC3339),
		}
		if authCookie == "" {
			account.Error = "OpenCode Go auth cookie is empty"
			out = append(out, account)
			continue
		}

		windows, err := fetchOpenCodeGoQuota(r.Context(), workspaceID, authCookie, now)
		if err != nil {
			account.Error = err.Error()
			out = append(out, account)
			continue
		}
		account.Windows = filterOpenCodeGoWindows(windows, up)
		account.Success = true
		out = append(out, account)
	}

	writeJSON(w, http.StatusOK, out)
}

func openCodeGoWorkspaceOrDefault(workspaceID string) string {
	ws := strings.TrimSpace(workspaceID)
	if ws == "" {
		return openCodeGoDefaultWorkspaceID
	}
	return ws
}

func resolveOpenCodeGoWorkspaceID(ctx context.Context, workspaceHint, authCookie string) (string, error) {
	if resolved := extractOpenCodeGoWorkspaceID(workspaceHint); resolved != "" {
		return resolved, nil
	}

	refs, err := fetchOpenCodeGoWorkspaceRefs(ctx, authCookie)
	if err != nil {
		return "", err
	}
	hint := strings.TrimSpace(workspaceHint)
	if hint != "" {
		for _, ref := range refs {
			if strings.EqualFold(ref.ID, hint) || strings.EqualFold(ref.Name, hint) {
				return ref.ID, nil
			}
		}
	}
	if len(refs) > 0 {
		return refs[0].ID, nil
	}
	if hint != "" {
		return "", fmt.Errorf("could not resolve workspace ID from %q", hint)
	}
	return "", fmt.Errorf("could not resolve OpenCode Go workspace ID")
}

func fetchOpenCodeGoWorkspaceRefs(ctx context.Context, authCookie string) ([]openCodeGoWorkspaceRef, error) {
	cookieHeader := buildOpenCodeGoCookieHeader(authCookie)
	if cookieHeader == "" {
		return nil, fmt.Errorf("OpenCode Go auth cookie is empty")
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://opencode.ai/_server?id="+url.QueryEscape(openCodeGoWorkspaceServerID),
		nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build workspace lookup request: %w", err)
	}
	req.Header.Set("Cookie", cookieHeader)
	req.Header.Set("X-Server-Id", openCodeGoWorkspaceServerID)
	req.Header.Set("X-Server-Instance", fmt.Sprintf("server-fn:%d", time.Now().UnixNano()))
	req.Header.Set("User-Agent", openCodeGoUserAgent)
	req.Header.Set("Origin", "https://opencode.ai")
	req.Header.Set("Referer", "https://opencode.ai")
	req.Header.Set("Accept", "text/javascript, application/json;q=0.9, */*;q=0.8")

	client := &http.Client{Timeout: openCodeGoTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error resolving workspace ID: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("authentication failed (HTTP %d). Check your auth cookie", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("workspace lookup returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, openCodeGoMaxHTMLBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read workspace lookup response: %w", err)
	}
	refs := parseOpenCodeGoWorkspaceRefs(string(body))
	if len(refs) == 0 {
		return nil, fmt.Errorf("could not resolve OpenCode Go workspace ID from account data")
	}
	return refs, nil
}

func parseOpenCodeGoWorkspaceRefs(text string) []openCodeGoWorkspaceRef {
	matches := reOpenCodeGoWorkspaceEntry.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	refs := make([]openCodeGoWorkspaceRef, 0, len(matches))
	for _, match := range matches {
		if len(match) != 3 {
			continue
		}
		id := match[1]
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		refs = append(refs, openCodeGoWorkspaceRef{
			ID:   id,
			Name: strings.TrimSpace(match[2]),
		})
	}
	return refs
}

func extractOpenCodeGoWorkspaceID(raw string) string {
	if raw = strings.TrimSpace(raw); raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "wrk_") && len(raw) > len("wrk_") {
		return raw
	}
	return reOpenCodeGoWorkspaceID.FindString(raw)
}

func fetchOpenCodeGoQuota(ctx context.Context, workspaceID, authCookie string, now time.Time) ([]openCodeGoQuotaWindow, error) {
	resolvedWorkspaceID, err := resolveOpenCodeGoWorkspaceID(ctx, workspaceID, authCookie)
	if err != nil {
		return nil, err
	}

	dashboardURL, err := buildOpenCodeGoDashboardURL(resolvedWorkspaceID)
	if err != nil {
		return nil, err
	}
	cookieHeader := buildOpenCodeGoCookieHeader(authCookie)
	if cookieHeader == "" {
		return nil, fmt.Errorf("OpenCode Go auth cookie is empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dashboardURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", cookieHeader)
	req.Header.Set("User-Agent", openCodeGoUserAgent)
	req.Header.Set("Accept", "text/html, application/xhtml+xml")

	client := &http.Client{
		Timeout: openCodeGoTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error fetching dashboard: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		loc := resp.Header.Get("Location")
		if loc != "" {
			return nil, fmt.Errorf("dashboard redirected (HTTP %d to %s). Check your OpenCode Go workspace ID and auth cookie", resp.StatusCode, loc)
		}
		return nil, fmt.Errorf("dashboard redirected (HTTP %d). Check your OpenCode Go workspace ID and auth cookie", resp.StatusCode)
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("authentication failed (HTTP %d). Check your auth cookie", resp.StatusCode)
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("workspace not found (HTTP %d). Verify your workspace ID", resp.StatusCode)
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return nil, fmt.Errorf("dashboard returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, openCodeGoMaxHTMLBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	windows := parseOpenCodeGoQuotaHTML(string(body), now)
	if len(windows) == 0 {
		return nil, fmt.Errorf("could not parse usage data from dashboard HTML")
	}
	return windows, nil
}

func buildOpenCodeGoDashboardURL(workspaceID string) (string, error) {
	ws := strings.TrimSpace(workspaceID)
	if ws == "" {
		return "", fmt.Errorf("OpenCode Go workspace ID is empty")
	}
	return strings.TrimRight(openCodeGoDashboardBase, "/") + "/" + url.PathEscape(ws) + "/go", nil
}

func buildOpenCodeGoCookieHeader(authCookie string) string {
	cookie := strings.TrimSpace(authCookie)
	if strings.HasPrefix(strings.ToLower(cookie), "cookie:") {
		cookie = strings.TrimSpace(cookie[len("cookie:"):])
	}
	if cookie == "" {
		return ""
	}
	for _, part := range strings.Split(cookie, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "auth=") {
			return part
		}
	}
	return "auth=" + cookie
}

func parseOpenCodeGoQuotaHTML(html string, now time.Time) []openCodeGoQuotaWindow {
	var windows []openCodeGoQuotaWindow
	if rolling, ok := parseOpenCodeGoWindow(reRollingPctFirst, reRollingResetFirst, html); ok {
		windows = append(windows, normalizeOpenCodeGoWindow(openCodeGoLabelRolling, rolling, now))
	}
	if weekly, ok := parseOpenCodeGoWindow(reWeeklyPctFirst, reWeeklyResetFirst, html); ok {
		windows = append(windows, normalizeOpenCodeGoWindow(openCodeGoLabelWeekly, weekly, now))
	}
	if monthly, ok := parseOpenCodeGoWindow(reMonthlyPctFirst, reMonthlyResetFirst, html); ok {
		windows = append(windows, normalizeOpenCodeGoWindow(openCodeGoLabelMonthly, monthly, now))
	}
	return windows
}

func parseOpenCodeGoWindow(pctFirst, resetFirst *regexp.Regexp, html string) (openCodeGoScrapedWindow, bool) {
	if caps := pctFirst.FindStringSubmatch(html); len(caps) == 3 {
		return openCodeGoScrapedWindow{
			usagePercent: parseFloat(caps[1]),
			resetInSec:   parseSeconds(caps[2]),
		}, true
	}
	if caps := resetFirst.FindStringSubmatch(html); len(caps) == 3 {
		return openCodeGoScrapedWindow{
			usagePercent: parseFloat(caps[2]),
			resetInSec:   parseSeconds(caps[1]),
		}, true
	}
	return openCodeGoScrapedWindow{}, false
}

func normalizeOpenCodeGoWindow(label string, scraped openCodeGoScrapedWindow, now time.Time) openCodeGoQuotaWindow {
	used := clampPercent(scraped.usagePercent)
	remaining := 100 - used
	resetAt := now.Add(time.Duration(scraped.resetInSec) * time.Second)
	return openCodeGoQuotaWindow{
		Label:      label,
		Used:       used,
		Remaining:  remaining,
		Total:      100,
		Unit:       "%",
		ResetAt:    resetAt.Format(time.RFC3339),
		ResetInSec: scraped.resetInSec,
	}
}

func filterOpenCodeGoWindows(windows []openCodeGoQuotaWindow, up config.Upstream) []openCodeGoQuotaWindow {
	out := make([]openCodeGoQuotaWindow, 0, len(windows))
	for _, win := range windows {
		switch win.Label {
		case openCodeGoLabelRolling:
			if !boolDefault(up.OpenCodeGoShowRolling, true) {
				continue
			}
		case openCodeGoLabelWeekly:
			if !boolDefault(up.OpenCodeGoShowWeekly, true) {
				continue
			}
		case openCodeGoLabelMonthly:
			if !boolDefault(up.OpenCodeGoShowMonthly, true) {
				continue
			}
		}
		out = append(out, win)
	}
	return out
}

func boolDefault(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}

func parseFloat(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

func parseSeconds(s string) int64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int64(v)
}

func clampPercent(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
