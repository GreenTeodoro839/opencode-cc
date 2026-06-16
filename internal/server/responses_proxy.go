package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Kiowx/opencode-cc/internal/proxy"
)

// ResponsesProxy handles POST /v1/responses for Codex and other Responses API
// clients. Zen's /go endpoint is Chat Completions-only, so this handler
// translates both request and response wire formats.
func (s *Server) ResponsesProxy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeOpenAIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method must be POST")
			return
		}

		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
		if err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error",
				"could not read request body: "+err.Error())
			return
		}
		in, err := proxy.ParseResponsesRequest(body)
		if err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
		chatReq, err := proxy.ConvertResponsesRequest(in, s.cfg.ResolveModel)
		if err != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
		upBody, err := json.Marshal(chatReq)
		if err != nil {
			writeOpenAIError(w, http.StatusInternalServerError, "api_error",
				"could not encode upstream request: "+err.Error())
			return
		}

		incomingModel := in.Model
		targetModel := chatReq.Model
		cfg := s.cfg.Snapshot()
		if cfg.ZenAPIKey == "" {
			const msg = "no Zen API key configured. Set ZEN_API_KEY or configure it in the web panel."
			writeOpenAIError(w, http.StatusUnauthorized, "authentication_error", msg)
			s.logFailed(r.Context(), r, incomingModel, targetModel, in.Stream,
				http.StatusUnauthorized, "no zen api key", body, time.Since(start))
			return
		}

		upURL := strings.TrimRight(cfg.UpstreamBase, "/") + "/v1/chat/completions"
		upReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upURL, bytes.NewReader(upBody))
		if err != nil {
			writeOpenAIError(w, http.StatusInternalServerError, "api_error",
				"could not build upstream request: "+err.Error())
			return
		}
		upReq.Header.Set("Authorization", "Bearer "+cfg.ZenAPIKey)
		upReq.Header.Set("Content-Type", "application/json")
		upReq.Header.Set("User-Agent", "opencode-cc/1.2")
		if in.Stream {
			upReq.Header.Set("Accept", "text/event-stream")
		} else {
			upReq.Header.Set("Accept", "application/json")
		}

		httpClient := s.httpClient
		if cfg.RequestTimeoutSeconds > 0 {
			httpClient = &http.Client{Timeout: time.Duration(cfg.RequestTimeoutSeconds) * time.Second}
		}
		resp, err := httpClient.Do(upReq)
		if err != nil {
			writeOpenAIError(w, http.StatusBadGateway, "api_error", "upstream request failed: "+err.Error())
			s.logFailed(r.Context(), r, incomingModel, targetModel, in.Stream,
				http.StatusBadGateway, err.Error(), body, time.Since(start))
			return
		}

		contentType := strings.ToLower(resp.Header.Get("Content-Type"))
		if in.Stream && resp.StatusCode < http.StatusBadRequest &&
			(contentType == "" || strings.Contains(contentType, "text/event-stream")) {
			s.relayResponsesStream(w, resp, r, incomingModel, targetModel, body, start)
			return
		}
		s.relayResponsesJSON(w, resp, r, incomingModel, targetModel, in.Stream, body, start)
	}
}

func (s *Server) relayResponsesJSON(
	w http.ResponseWriter,
	resp *http.Response,
	r *http.Request,
	incomingModel, targetModel string,
	stream bool,
	reqBody []byte,
	start time.Time,
) {
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "api_error", "could not read upstream body: "+err.Error())
		s.logFailed(r.Context(), r, incomingModel, targetModel, stream,
			http.StatusBadGateway, err.Error(), reqBody, time.Since(start))
		return
	}
	if len(raw) > maxResponseBytes {
		const msg = "upstream response exceeded the maximum allowed size"
		writeOpenAIError(w, http.StatusBadGateway, "api_error", msg)
		s.logFailed(r.Context(), r, incomingModel, targetModel, stream,
			http.StatusBadGateway, msg, reqBody, time.Since(start))
		return
	}
	if resp.StatusCode >= http.StatusBadRequest {
		copyOpenAIHeaders(w.Header(), resp.Header, false)
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(raw)
		message := extractOpenAIError(raw)
		if message == "" {
			message = strings.TrimSpace(string(raw))
		}
		s.logFailed(r.Context(), r, incomingModel, targetModel, stream,
			resp.StatusCode, message, reqBody, time.Since(start))
		return
	}

	chatResp, err := proxy.ParseOpenAIResponse(raw)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "api_error", err.Error())
		s.logFailed(r.Context(), r, incomingModel, targetModel, stream,
			http.StatusBadGateway, err.Error(), reqBody, time.Since(start))
		return
	}
	out := proxy.ConvertResponsesResponse(chatResp, incomingModel)
	writeJSON(w, http.StatusOK, out)

	stopReason := ""
	if len(chatResp.Choices) > 0 && chatResp.Choices[0].FinishReason != nil {
		stopReason = *chatResp.Choices[0].FinishReason
	}
	s.logSuccess(r.Context(), r, incomingModel, targetModel, stream, http.StatusOK,
		chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, stopReason,
		string(reqBody), mustJSON(out), time.Since(start))
}

func (s *Server) relayResponsesStream(
	w http.ResponseWriter,
	resp *http.Response,
	r *http.Request,
	incomingModel, targetModel string,
	reqBody []byte,
	start time.Time,
) {
	defer resp.Body.Close()
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeOpenAIError(w, http.StatusInternalServerError, "api_error",
			"streaming not supported by this server")
		return
	}

	reader := io.Reader(resp.Body)
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gzipReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			writeOpenAIError(w, http.StatusBadGateway, "api_error",
				"could not decompress upstream stream: "+err.Error())
			return
		}
		defer gzipReader.Close()
		reader = gzipReader
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	responseLog := &limitedLogWriter{limit: s.cfg.Snapshot().MaxBodyLogBytes}
	converter, err := proxy.NewResponsesStreamConverter(io.MultiWriter(w, responseLog), incomingModel)
	if err != nil {
		s.logFailed(r.Context(), r, incomingModel, targetModel, true,
			http.StatusBadGateway, err.Error(), reqBody, time.Since(start))
		return
	}
	flusher.Flush()

	scanErr := proxy.ScanOpenAIStream(reader, func(chunk *proxy.OpenAIStreamChunk) error {
		if err := converter.HandleChunk(chunk); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	})
	if scanErr != nil && !errors.Is(scanErr, io.EOF) {
		_ = converter.EmitError("upstream stream error: " + scanErr.Error())
		flusher.Flush()
		s.logFailed(r.Context(), r, incomingModel, targetModel, true,
			http.StatusBadGateway, scanErr.Error(), reqBody, time.Since(start))
		return
	}
	if err := converter.Finalize(); err != nil {
		s.logFailed(r.Context(), r, incomingModel, targetModel, true,
			http.StatusBadGateway, err.Error(), reqBody, time.Since(start))
		return
	}
	flusher.Flush()

	s.logSuccess(r.Context(), r, incomingModel, targetModel, true, http.StatusOK,
		converter.InputTokens(), converter.OutputTokens(), converter.FinishReason(),
		string(reqBody), responseLog.String(), time.Since(start))
}

type limitedLogWriter struct {
	builder strings.Builder
	limit   int
}

func (w *limitedLogWriter) Write(data []byte) (int, error) {
	appendLimited(&w.builder, string(data), w.limit)
	return len(data), nil
}

func (w *limitedLogWriter) String() string {
	return w.builder.String()
}
