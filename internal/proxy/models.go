package proxy

import (
	"encoding/json"
	"net/http"
	"time"
)

// AnthropicModelInfo describes one entry in the model list.
type AnthropicModelInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Type        string `json:"type"`
	CreatedAt   int64  `json:"created_at,omitempty"`
}

// AnthropicModelList is the body of GET /v1/models.
type AnthropicModelList struct {
	Data    []AnthropicModelInfo `json:"data"`
	FirstID string               `json:"first_id,omitempty"`
	LastID  string `json:"last_id,omitempty"`
	HasMore bool   `json:"has_more"`
}

// fetchZenModels queries the upstream <base>/v1/models and returns the ids. It
// is used by the /v1/models handler so Claude Code sees the real Zen model ids.
func fetchZenModels(client *http.Client, base, apiKey string) ([]AnthropicModelInfo, error) {
	url := base + "/v1/models"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, nil // fall back to empty list
	}

	var raw struct {
		Data []struct {
			ID      string `json:"id"`
			Created int64  `json:"created"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]AnthropicModelInfo, 0, len(raw.Data))
	for _, m := range raw.Data {
		out = append(out, AnthropicModelInfo{
			ID:        m.ID,
			Type:      "model",
			CreatedAt: m.Created,
		})
	}
	return out, nil
}

// ModelsHandler returns an http.HandlerFunc that dynamically lists the upstream
// (Zen) models. It caches the result for a minute so we don't hit Zen on every
// call. If the upstream is unreachable it returns a minimal fallback list.
func ModelsHandler(client *http.Client, upstreamBase, apiKey func() string) http.HandlerFunc {
	var (
		cache    []AnthropicModelInfo
		cachedAt time.Time
	)
	return func(w http.ResponseWriter, r *http.Request) {
		// Serve from cache if fresh (< 60s).
		if len(cache) > 0 && time.Since(cachedAt) < time.Minute {
			list := AnthropicModelList{Data: cache, HasMore: false}
			writeJSON(w, http.StatusOK, list)
			return
		}
		// Fetch live from upstream.
		base := upstreamBase()
		if base != "" && client != nil {
			if models, err := fetchZenModels(client, base, apiKey()); err == nil && len(models) > 0 {
				cache = models
				cachedAt = time.Now()
			}
		}
		if len(cache) == 0 {
			// Minimal fallback so the endpoint never errors.
			cache = []AnthropicModelInfo{{ID: "glm-5.1", Type: "model"}}
		}
		writeJSON(w, http.StatusOK, AnthropicModelList{Data: cache, HasMore: false})
	}
}

// writeJSON is shared here because the server package also needs it; to avoid
// an import cycle we keep a local copy in helpers.go for the proxy package and
// the server package has its own.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
