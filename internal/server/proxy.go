package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Kiowx/opencode-cc/internal/proxy"
	"github.com/Kiowx/opencode-cc/internal/store"
)

// Proxy handles POST /v1/messages. It converts the Anthropic request to an
// OpenAI Chat Completions request, forwards it to Zen, and converts the
// response back (streaming or not).
func (s *Server) Proxy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ctx := r.Context()

		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
		if err != nil {
			writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "could not read request body: "+err.Error())
			return
		}

		var areq proxy.AnthropicRequest
		if err := json.Unmarshal(body, &areq); err != nil {
			writeAnthropicError(w, http.StatusBadRequest, "invalid_request_error", "request body is not valid Anthropic JSON: "+err.Error())
			return
		}

		// Resolve target model under a short read lock.
		s.cfg.RLock()
		upstream := s.cfg.UpstreamBase
		zenKey := s.cfg.ZenAPIKey
		timeout := time.Duration(s.cfg.RequestTimeoutSeconds) * time.Second
		s.cfg.RUnlock()

		targetModel := s.cfg.ResolveModel(areq.Model)
		oreq := proxy.ConvertRequest(&areq, func(string) string { return targetModel })

		if zenKey == "" {
			writeAnthropicError(w, http.StatusUnauthorized, "authentication_error", "no Zen API key configured. Set ZEN_API_KEY or configure it in the web panel.")
			s.logFailed(ctx, r, areq.Model, targetModel, areq.Stream, http.StatusUnauthorized, "no zen api key", body, time.Since(start))
			return
		}

		// Marshal the upstream request.
		upBody, err := json.Marshal(oreq)
		if err != nil {
			writeAnthropicError(w, http.StatusInternalServerError, "api_error", "could not encode upstream request: "+err.Error())
			return
		}

		upURL := strings.TrimRight(upstream, "/") + "/v1/chat/completions"
		upReq, err := http.NewRequestWithContext(ctx, http.MethodPost, upURL, bytes.NewReader(upBody))
		if err != nil {
			writeAnthropicError(w, http.StatusInternalServerError, "api_error", "could not build upstream request: "+err.Error())
			return
		}
		upReq.Header.Set("Content-Type", "application/json")
		upReq.Header.Set("Authorization", "Bearer "+zenKey)
		// Match the Accept header to the request mode: Zen (and most OpenAI-
		// compatible servers) gate SSE delivery on Accept: text/event-stream.
		// Sending application/json on a stream:true request makes some upstreams
		// refuse with "streaming not supported".
		if areq.Stream {
			upReq.Header.Set("Accept", "text/event-stream")
		} else {
			upReq.Header.Set("Accept", "application/json")
		}
		// Some upstreams prefer a UA.
		upReq.Header.Set("User-Agent", "opencode-cc/1.0")
		// Propagate the anthropic-version / anthropic-beta for observability
		// on the upstream side (Zen ignores them for the OpenAI path).
		if v := r.Header.Get("anthropic-version"); v != "" {
			upReq.Header.Set("anthropic-version", v)
		}

		httpClient := s.httpClient
		if timeout > 0 {
			httpClient = &http.Client{Timeout: timeout}
		}

		resp, err := httpClient.Do(upReq)
		if err != nil {
			writeAnthropicError(w, http.StatusBadGateway, "api_error", "upstream request failed: "+err.Error())
			s.logFailed(ctx, r, areq.Model, targetModel, areq.Stream, http.StatusBadGateway, err.Error(), body, time.Since(start))
			return
		}

		// Non-2xx: pass the upstream error back in Anthropic shape.
		if resp.StatusCode >= 400 {
			s.passUpstreamError(w, resp, r, areq.Model, targetModel, body, start)
			return
		}

		if areq.Stream {
			s.handleStreamResponse(w, resp, r, areq.Model, targetModel, body, start)
		} else {
			s.handleNonStreamResponse(w, resp, r, areq.Model, targetModel, body, start)
		}
	}
}

// passUpstreamError reads the upstream error body and returns an Anthropic-
// shaped error to the client, while logging the failed request.
func (s *Server) passUpstreamError(w http.ResponseWriter, resp *http.Response, r *http.Request, inModel, target string, reqBody []byte, start time.Time) {
	defer resp.Body.Close()
	eb, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	msg := strings.TrimSpace(string(eb))
	// Try to extract OpenAI error message.
	if em := extractOpenAIError(eb); em != "" {
		msg = em
	}
	if msg == "" {
		msg = fmt.Sprintf("upstream returned status %d", resp.StatusCode)
	}
	errType := "api_error"
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		errType = "authentication_error"
	case http.StatusTooManyRequests:
		errType = "rate_limit_error"
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		errType = "timeout_error"
	}
	writeAnthropicError(w, resp.StatusCode, errType, msg)
	s.logFailed(r.Context(), r, inModel, target, false, resp.StatusCode, msg, reqBody, time.Since(start))
}

// handleNonStreamResponse decodes the upstream JSON, converts it, and writes
// the Anthropic response.
func (s *Server) handleNonStreamResponse(w http.ResponseWriter, resp *http.Response, r *http.Request, inModel, target string, reqBody []byte, start time.Time) {
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		writeAnthropicError(w, http.StatusBadGateway, "api_error", "could not read upstream body: "+err.Error())
		s.logFailed(r.Context(), r, inModel, target, false, http.StatusBadGateway, err.Error(), reqBody, time.Since(start))
		return
	}
	oresp, err := proxy.ParseOpenAIResponse(raw)
	if err != nil {
		writeAnthropicError(w, http.StatusBadGateway, "api_error", "could not parse upstream response: "+err.Error())
		s.logFailed(r.Context(), r, inModel, target, false, http.StatusBadGateway, err.Error(), reqBody, time.Since(start))
		return
	}
	aresp := proxy.ConvertResponse(oresp, inModel)
	writeJSON(w, http.StatusOK, aresp)

	// Log a success row.
	s.logSuccess(r.Context(), r, inModel, target, false, http.StatusOK,
		aresp.Usage.InputTokens, aresp.Usage.OutputTokens, stopReasonStr(aresp.StopReason),
		string(reqBody), mustJSON(aresp), time.Since(start))
}

// handleStreamResponse proxies the SSE stream, converting each OpenAI chunk to
// Anthropic events. It flushes continuously so the client sees real-time data.
func (s *Server) handleStreamResponse(w http.ResponseWriter, resp *http.Response, r *http.Request, inModel, target string, reqBody []byte, start time.Time) {
	defer resp.Body.Close()

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAnthropicError(w, http.StatusInternalServerError, "api_error", "streaming not supported by this server")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	var stopSeq *string
	stopReason := ""
	var inputTok, outputTok int

	conv, err := proxy.NewStreamConverter(w, inModel, stopSeq)
	if err != nil {
		return
	}

	// Read the upstream body, transparently decompressing gzip if needed.
	bodyReader := io.Reader(resp.Body)
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gz, gerr := gzip.NewReader(resp.Body)
		if gerr == nil {
			defer gz.Close()
			bodyReader = gz
		}
	}

	streamErr := proxy.ScanOpenAIStream(bodyReader, func(chunk *proxy.OpenAIStreamChunk) error {
		if chunk.Usage != nil {
			inputTok = chunk.Usage.PromptTokens
			outputTok = chunk.Usage.CompletionTokens
		}
		return conv.HandleChunk(chunk)
	})

	// Finalize the stream: emit content_block_stop (if open) + message_delta
	// (carrying usage) + message_stop. If the upstream errored mid-stream,
	// surface it as an error event first.
	if streamErr != nil && streamErr.Error() != "EOF" {
		_ = conv.EmitError("api_error", "upstream stream error: "+streamErr.Error())
		stopReason = "stream_error"
		_ = conv.Finalize(stopReason)
	} else {
		_ = conv.Finalize("end_turn")
		stopReason = "end_turn"
	}
	_ = conv.Flush()
	flusher.Flush()

	// Log.
	s.logSuccess(r.Context(), r, inModel, target, true, http.StatusOK,
		inputTok, outputTok, stopReason, string(reqBody), "[streamed]", time.Since(start))
}

// CountTokens handles POST /v1/messages/count_tokens with a rough estimate
// (~4 chars/token), which is all Claude Code uses it for.
func (s *Server) CountTokens() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, maxRequestBytes))
		var areq proxy.AnthropicRequest
		_ = json.Unmarshal(body, &areq)
		tokens := estimateTokens(&areq)
		writeJSON(w, http.StatusOK, proxy.CountTokensResponse{InputTokens: tokens})
	}
}

// estimateTokens returns a rough token count for the request (~4 chars/tok).
func estimateTokens(areq *proxy.AnthropicRequest) int {
	total := 0
	for _, b := range areq.System.Blocks {
		total += len(b.Text)
	}
	for _, m := range areq.Messages {
		if m.Content.IsStr {
			total += len(m.Content.Text)
			continue
		}
		for _, b := range m.Content.Blocks {
			total += len(b.Text)
		}
	}
	if total == 0 {
		return 1
	}
	return total/4 + 1
}

// ---- logging helpers ----

func (s *Server) logSuccess(ctx context.Context, r *http.Request, inModel, target string, stream bool, status, inputTok, outputTok int, stopReason, reqBody, respBody string, dur time.Duration) {
	if !s.shouldLog() {
		return
	}
	if s.cfg.Snapshot().MaxBodyLogBytes > 0 {
		reqBody = truncate(reqBody, s.cfg.Snapshot().MaxBodyLogBytes)
		respBody = truncate(respBody, s.cfg.Snapshot().MaxBodyLogBytes)
	}
	go func() {
		_ = s.store.InsertRequest(context.Background(), &store.RequestRow{
			Ts:            time.Now(),
			Method:        r.Method,
			Path:          r.URL.Path,
			IncomingModel: inModel,
			TargetModel:   target,
			Stream:        stream,
			Status:        status,
			DurationMs:    dur.Milliseconds(),
			InputTokens:   inputTok,
			OutputTokens:  outputTok,
			StopReason:    stopReason,
			ReqBody:       reqBody,
			RespBody:      respBody,
		})
	}()
}

func (s *Server) logFailed(ctx context.Context, r *http.Request, inModel, target string, stream bool, status int, errMsg string, reqBody []byte, dur time.Duration) {
	if !s.shouldLog() {
		return
	}
	mb := s.cfg.Snapshot().MaxBodyLogBytes
	reqStr := string(reqBody)
	if mb > 0 {
		reqStr = truncate(reqStr, mb)
	}
	go func() {
		_ = s.store.InsertRequest(context.Background(), &store.RequestRow{
			Ts:            time.Now(),
			Method:        r.Method,
			Path:          r.URL.Path,
			IncomingModel: inModel,
			TargetModel:   target,
			Stream:        stream,
			Status:        status,
			DurationMs:    dur.Milliseconds(),
			StopReason:    "error",
			Error:         truncate(errMsg, 2048),
			ReqBody:       reqStr,
		})
	}()
}

func (s *Server) shouldLog() bool {
	s.cfg.RLock()
	defer s.cfg.RUnlock()
	return s.cfg.LogRequests
}

func stopReasonStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// extractOpenAIError pulls the human message out of an OpenAI error envelope.
func extractOpenAIError(b []byte) string {
	var env struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		return ""
	}
	if env.Error.Message != "" {
		return env.Error.Message
	}
	return env.Message
}

const (
	maxRequestBytes  = 32 << 20 // 32 MiB
	maxResponseBytes = 32 << 20
)
