package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Kiowx/opencode-cc/internal/config"
)

// testUpstream sends a minimal chat completion to the configured Zen endpoint
// and reports whether it succeeded. Used by the panel's "Test connection"
// button. It never streams. With multiple upstreams configured, it tests each
// enabled upstream and returns a per-upstream result.
func (a *API) testUpstream(w http.ResponseWriter, r *http.Request) {
	// Collect the enabled upstream pool (or fall back to the legacy single pair).
	a.cfg.RLock()
	pool := make([]upstreamProbe, 0, len(a.cfg.Upstreams))
	for _, u := range a.cfg.Upstreams {
		if u.Enabled && u.APIKey != "" {
			pool = append(pool, upstreamProbe{
				base:     u.BaseURL,
				key:      u.APIKey,
				name:     u.Name,
				protocol: u.Protocol,
				models:   append([]config.UpstreamModel(nil), u.Models...),
			})
		}
	}
	if len(pool) == 0 && a.cfg.UpstreamBase != "" && a.cfg.ZenAPIKey != "" {
		pool = append(pool, upstreamProbe{base: a.cfg.UpstreamBase, key: a.cfg.ZenAPIKey})
	}
	defaultModel := a.cfg.DefaultModel
	a.cfg.RUnlock()

	model := r.URL.Query().Get("model")
	if model == "" {
		model = defaultModel
	}
	if model == "" {
		model = "glm-4.6"
	}

	if len(pool) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         false,
			"model":      model,
			"elapsed_ms": 0,
			"error":      "no upstream API key configured",
		})
		return
	}

	// Test each upstream; report a list of results. The top-level fields are
	// kept for the UI's single-result display; upstreams carries the detailed
	// per-account breakdown.
	start := time.Now()
	results := make([]map[string]any, 0, len(pool))
	for _, up := range pool {
		results = append(results, a.probeOne(up, model))
	}
	elapsed := time.Since(start).Milliseconds()
	// Overall ok = all succeeded (panel can show per-upstream detail).
	overallOK := true
	var preview string
	var firstErr string
	var promptTokens, completionTokens int
	for _, rr := range results {
		if ok, _ := rr["ok"].(bool); !ok {
			overallOK = false
			if firstErr == "" {
				firstErr, _ = rr["error"].(string)
			}
			continue
		}
		if preview == "" {
			preview, _ = rr["preview"].(string)
		}
		if n, ok := rr["prompt_tokens"].(int); ok {
			promptTokens += n
		}
		if n, ok := rr["completion_tokens"].(int); ok {
			completionTokens += n
		}
	}
	out := map[string]any{
		"ok":                overallOK,
		"model":             model,
		"elapsed_ms":        elapsed,
		"prompt_tokens":     promptTokens,
		"completion_tokens": completionTokens,
		"upstreams":         results,
	}
	if preview != "" {
		out["preview"] = preview
	}
	if firstErr != "" {
		out["error"] = firstErr
	}
	writeJSON(w, http.StatusOK, out)
}

// upstreamProbe is a single upstream to test.
type upstreamProbe struct {
	base, key, name, protocol string
	models                    []config.UpstreamModel
}

// probeOne tests a single upstream and returns its result map.
func (a *API) probeOne(up upstreamProbe, model string) map[string]any {
	targetModel := probeTargetModel(up.models, model)
	if targetModel == "" {
		targetModel = model
	}
	result := map[string]any{
		"ok":         false,
		"model":      targetModel,
		"name":       up.name,
		"base_url":   up.base,
		"protocol":   probeProtocol(up.protocol),
		"elapsed_ms": int64(0),
	}
	if probeProtocol(up.protocol) == config.UpstreamProtocolAnthropic {
		return a.probeAnthropic(up, targetModel, result)
	}
	payload := map[string]any{
		"model":       targetModel,
		"max_tokens":  16,
		"temperature": 0,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
	}
	b, _ := json.Marshal(payload)

	url := strings.TrimRight(up.base, "/") + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		result["error"] = err.Error()
		return result
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+up.key)

	client := &http.Client{Timeout: 30 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start).Milliseconds()
	result["elapsed_ms"] = elapsed
	if err != nil {
		result["error"] = err.Error()
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := readN(resp.Body, 2048)
		result["error"] = fmt.Sprintf("upstream status %d: %s", resp.StatusCode, strings.TrimSpace(body))
		return result
	}

	// Decode enough to confirm a usable response.
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		result["error"] = "could not decode response: " + err.Error()
		return result
	}
	result["ok"] = true
	result["prompt_tokens"] = parsed.Usage.PromptTokens
	result["completion_tokens"] = parsed.Usage.CompletionTokens
	if len(parsed.Choices) > 0 {
		result["preview"] = truncate(parsed.Choices[0].Message.Content, 200)
	}
	return result
}

func (a *API) probeAnthropic(up upstreamProbe, model string, result map[string]any) map[string]any {
	payload := map[string]any{
		"model":       model,
		"max_tokens":  16,
		"temperature": 0,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
	}
	b, _ := json.Marshal(payload)

	url := strings.TrimRight(up.base, "/") + "/v1/messages"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		result["error"] = err.Error()
		return result
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+up.key)
	req.Header.Set("x-api-key", up.key)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 30 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start).Milliseconds()
	result["elapsed_ms"] = elapsed
	if err != nil {
		result["error"] = err.Error()
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := readN(resp.Body, 2048)
		result["error"] = fmt.Sprintf("upstream status %d: %s", resp.StatusCode, strings.TrimSpace(body))
		return result
	}

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		result["error"] = "could not decode response: " + err.Error()
		return result
	}
	result["ok"] = true
	result["prompt_tokens"] = parsed.Usage.InputTokens
	result["completion_tokens"] = parsed.Usage.OutputTokens
	for _, block := range parsed.Content {
		if block.Type == "text" && block.Text != "" {
			result["preview"] = truncate(block.Text, 200)
			break
		}
	}
	return result
}

func probeTargetModel(models []config.UpstreamModel, requested string) string {
	requested = stripProbePrefix(strings.TrimSpace(requested))
	if len(models) == 0 {
		return requested
	}
	for _, m := range models {
		target := probeModelTarget(m)
		alias := stripProbePrefix(strings.TrimSpace(m.Alias))
		if alias == "" {
			alias = target
		}
		match := stripProbePrefix(strings.TrimSpace(m.Match))
		var wildcard string
		var ok bool
		switch {
		case requested == "":
			ok = true
		case match != "":
			wildcard, ok = probePatternMatch(match, requested, true)
		default:
			wildcard, ok = probePatternMatch(alias, requested, false)
		}
		if ok {
			return expandProbeTarget(target, requested, wildcard)
		}
	}
	return expandProbeTarget(probeModelTarget(models[0]), requested, "")
}

func probeModelTarget(m config.UpstreamModel) string {
	if name := stripProbePrefix(strings.TrimSpace(m.Name)); name != "" {
		return name
	}
	return stripProbePrefix(strings.TrimSpace(m.Target))
}

func expandProbeTarget(target, requested, wildcard string) string {
	target = stripProbePrefix(strings.TrimSpace(target))
	if !strings.Contains(target, "*") {
		return target
	}
	if wildcard == "" {
		wildcard = requested
	}
	return strings.ReplaceAll(target, "*", wildcard)
}

func probePatternMatch(pattern, requested string, prefixMatch bool) (string, bool) {
	pattern = stripProbePrefix(strings.TrimSpace(pattern))
	if pattern == "" {
		return "", false
	}
	if wildcard, ok := probeWildcardCapture(pattern, requested); ok {
		return wildcard, true
	}
	if strings.Contains(pattern, "*") {
		return "", false
	}
	if prefixMatch {
		return "", strings.HasPrefix(requested, pattern)
	}
	return "", requested == pattern
}

func probeWildcardCapture(pattern, value string) (string, bool) {
	if !strings.Contains(pattern, "*") {
		return "", false
	}
	if pattern == "*" {
		return value, true
	}
	if strings.Count(pattern, "*") != 1 {
		return "", probeGlobMatches(pattern, value)
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

func probeGlobMatches(pattern, value string) bool {
	parts := strings.Split(pattern, "*")
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

func probeProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case config.UpstreamProtocolAnthropic, "messages", "anthropic-messages", "claude":
		return config.UpstreamProtocolAnthropic
	case config.UpstreamProtocolOpenAI, "chat", "chat_completions", "chat-completions", "openai-chat":
		return config.UpstreamProtocolOpenAI
	default:
		return config.UpstreamProtocolOpenAI
	}
}

func stripProbePrefix(in string) string {
	if i := strings.IndexByte(in, '/'); i >= 0 {
		return in[i+1:]
	}
	return in
}

func readN(r interface{ Read([]byte) (int, error) }, n int) (string, error) {
	buf := make([]byte, n)
	m, err := r.Read(buf)
	if m > 0 {
		return string(buf[:m]), nil
	}
	return "", err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
