package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// testUpstream sends a minimal chat completion to the configured Zen endpoint
// and reports whether it succeeded. Used by the panel's "Test connection"
// button. It never streams.
func (a *API) testUpstream(w http.ResponseWriter, r *http.Request) {
	a.cfg.RLock()
	upstream := a.cfg.UpstreamBase
	key := a.cfg.ZenAPIKey
	defaultModel := a.cfg.DefaultModel
	a.cfg.RUnlock()

	model := r.URL.Query().Get("model")
	if model == "" {
		model = defaultModel
	}
	if model == "" {
		model = "glm-4.6"
	}

	result := map[string]any{"ok": false, "model": model, "elapsed_ms": 0}

	if key == "" {
		result["error"] = "no Zen API key configured"
		writeJSON(w, http.StatusOK, result)
		return
	}

	payload := map[string]any{
		"model":       model,
		"max_tokens":  16,
		"temperature": 0,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
	}
	b, _ := json.Marshal(payload)

	url := strings.TrimRight(upstream, "/") + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		result["error"] = err.Error()
		writeJSON(w, http.StatusOK, result)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	client := &http.Client{Timeout: 30 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start).Milliseconds()
	result["elapsed_ms"] = elapsed
	if err != nil {
		result["error"] = err.Error()
		writeJSON(w, http.StatusOK, result)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := readN(resp.Body, 2048)
		result["error"] = fmt.Sprintf("upstream status %d: %s", resp.StatusCode, strings.TrimSpace(body))
		writeJSON(w, http.StatusOK, result)
		return
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
		writeJSON(w, http.StatusOK, result)
		return
	}
	result["ok"] = true
	result["prompt_tokens"] = parsed.Usage.PromptTokens
	result["completion_tokens"] = parsed.Usage.CompletionTokens
	if len(parsed.Choices) > 0 {
		result["preview"] = truncate(parsed.Choices[0].Message.Content, 200)
	}
	writeJSON(w, http.StatusOK, result)
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
