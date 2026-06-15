// Package api implements the web panel's backend REST API, mounted under
// /api. It exposes: auth check, config get/update, summary stats, hourly
// series, model usage, latency percentiles, and the request log + detail.
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Kiowx/opencode-cc/internal/config"
	"github.com/Kiowx/opencode-cc/internal/metrics"
	"github.com/Kiowx/opencode-cc/internal/store"
)

// API holds dependencies shared by all panel handlers.
type API struct {
	cfg   *config.Config
	store *store.Store
}

// New constructs an API.
func New(cfg *config.Config, st *store.Store) *API {
	return &API{cfg: cfg, store: st}
}

// Mount registers the API routes on mux under /api.
func (a *API) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/api/health", a.health)
	mux.Handle("/api/config", a.auth(http.HandlerFunc(a.configHandler)))
	mux.Handle("/api/test", a.auth(http.HandlerFunc(a.testUpstream)))
	mux.Handle("/api/stats/summary", a.auth(http.HandlerFunc(a.summary)))
	mux.Handle("/api/stats/hourly", a.auth(http.HandlerFunc(a.hourly)))
	mux.Handle("/api/stats/models", a.auth(http.HandlerFunc(a.modelUsage)))
	mux.Handle("/api/stats/latency", a.auth(http.HandlerFunc(a.latency)))
	mux.Handle("/api/logs", a.auth(http.HandlerFunc(a.logs)))
	mux.Handle("/api/logs/", a.auth(http.HandlerFunc(a.logDetail)))
}

// auth gates the panel API behind the configured panel token. If no token is
// configured, access is open (convenient for localhost-only use).
func (a *API) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.cfg.RLock()
		token := a.cfg.PanelToken
		a.cfg.RUnlock()
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}
		// Accept either a Bearer header or ?token= / cookie for browser use.
		provided := r.Header.Get("Authorization")
		provided = strings.TrimPrefix(provided, "Bearer ")
		if provided == "" {
			provided = r.URL.Query().Get("token")
		}
		if provided == "" {
			if c, _ := r.Cookie("opencode_cc_token"); c != nil {
				provided = c.Value
			}
		}
		if subtleEqual(provided, token) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
	})
}

// subtleEqual is a constant-time-ish string compare for tokens.
func subtleEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// health is an unauthenticated liveness probe.
func (a *API) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"time":    time.Now().Format(time.RFC3339),
		"version": "1.0.0",
	})
}

// configHandler GET returns the current config (with sensitive fields masked),
// PUT updates it. The Zen API key is never returned in full; PUT uses the
// sentinel "__unchanged__" to mean "leave as-is".
func (a *API) configHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		snap := a.cfg.Snapshot()
		writeJSON(w, http.StatusOK, publicConfig(snap))
	case http.MethodPut:
		var in config.Config
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		a.cfg.Apply(&in)
		if err := a.cfg.Save(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, publicConfig(a.cfg.Snapshot()))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// publicConfig masks the Zen API key so it is never echoed to the browser.
func publicConfig(c *config.Config) map[string]any {
	key := c.ZenAPIKey
	masked := ""
	if key != "" {
		if len(key) <= 8 {
			masked = strings.Repeat("*", len(key))
		} else {
			masked = key[:4] + strings.Repeat("*", len(key)-8) + key[len(key)-4:]
		}
	}
	return map[string]any{
		"listen_addr":             c.ListenAddr,
		"upstream_base":           c.UpstreamBase,
		"zen_api_key_masked":      masked,
		"zen_api_key_set":         key != "",
		"panel_token_set":         c.PanelToken != "",
		"default_model":           c.DefaultModel,
		"model_mappings":          c.ModelMappings,
		"log_requests":            c.LogRequests,
		"max_body_log_bytes":      c.MaxBodyLogBytes,
		"request_timeout_seconds": c.RequestTimeoutSeconds,
	}
}

// summary returns lifetime + last-24h aggregate numbers.
func (a *API) summary(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		writeJSON(w, http.StatusOK, &store.StatsSummary{})
		return
	}
	s, err := a.store.Summary(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// hourly returns the per-hour series for the requested window (?hours=24).
func (a *API) hourly(w http.ResponseWriter, r *http.Request) {
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 {
		hours = 24
	}
	if a.store == nil {
		writeJSON(w, http.StatusOK, []store.HourPoint{})
		return
	}
	pts, err := a.store.HourlySeries(r.Context(), hours)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, pts)
}

// modelUsage returns per-target-model request/token counts.
func (a *API) modelUsage(w http.ResponseWriter, r *http.Request) {
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 {
		hours = 24
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour).UnixMilli()
	if a.store == nil {
		writeJSON(w, http.StatusOK, []store.ModelUsagePoint{})
		return
	}
	pts, err := a.store.ModelUsage(r.Context(), since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, pts)
}

// latency returns p50/p95/p99 latency in ms over recent requests.
func (a *API) latency(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		writeJSON(w, http.StatusOK, map[string]int64{"p50": 0, "p95": 0, "p99": 0})
		return
	}
	vals, err := a.store.RecentLatency(r.Context(), 300)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{
		"p50": metrics.Percentile(vals, 50),
		"p95": metrics.Percentile(vals, 95),
		"p99": metrics.Percentile(vals, 99),
	})
}

// logs returns the recent request list (without full bodies).
func (a *API) logs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	if a.store == nil {
		writeJSON(w, http.StatusOK, []store.RequestRow{})
		return
	}
	rows, err := a.store.ListRequests(r.Context(), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Strip large bodies from the list view.
	type lite struct {
		store.RequestRow
		ReqBody  string `json:"req_body"`
		RespBody string `json:"resp_body"`
	}
	out := make([]lite, 0, len(rows))
	for _, row := range rows {
		row.ReqBody = ""
		row.RespBody = ""
		out = append(out, lite{RequestRow: row})
	}
	writeJSON(w, http.StatusOK, out)
}

// logDetail returns a single request with full bodies.
func (a *API) logDetail(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/logs/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad id"})
		return
	}
	if a.store == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no store"})
		return
	}
	row, err := a.store.GetRequest(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if row == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, row)
}
