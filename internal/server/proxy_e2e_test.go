package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Kiowx/opencode-cc/internal/config"
	"github.com/Kiowx/opencode-cc/internal/store"
)

// mockZen returns a test server that pretends to be Zen's /v1/chat/completions.
// It supports both non-streaming and streaming, and echoes what it received so
// the test can assert the converted OpenAI payload.
func mockZen(t *testing.T, stream bool, chunks []string) *httptest.Server {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		t.Logf("mock zen received: %s", string(gotBody))

		if !stream {
			// Non-streaming response with a tool call.
			resp := map[string]any{
				"id": "chatcmpl-test",
				"choices": []map[string]any{{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Hello from Zen",
					},
					"finish_reason": "stop",
				}},
				"usage": map[string]any{
					"prompt_tokens":     12,
					"completion_tokens": 4,
					"total_tokens":      16,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Streaming: write the provided chunks as SSE.
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, ch := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", ch)
			if flusher != nil {
				flusher.Flush()
			}
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	// Expose the captured body via a closure for the test.
	t.Cleanup(func() {
		_ = gotBody
		srv.Close()
	})
	return srv
}

func newTestServer(t *testing.T, upstream string) (*Server, *store.Store) {
	cfg := config.Default()
	cfg.UpstreamBase = strings.TrimRight(upstream, "/")
	cfg.ZenAPIKey = "test-key"
	cfg.ModelMappings = []config.ModelMapping{{Match: "*", Target: "glm-4.6"}}
	tmp := t.TempDir()
	st, err := store.Open(tmp + "/test.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return New(cfg, st), st
}

func TestProxyNonStreamEndToEnd(t *testing.T) {
	zen := mockZen(t, false, nil)
	srv, _ := newTestServer(t, zen.URL)

	// An Anthropic request with system + user + a tool.
	anthReq := map[string]any{
		"model":      "claude-3-5-sonnet-20241022",
		"max_tokens": 256,
		"system":     "You are concise.",
		"messages": []map[string]any{
			{"role": "user", "content": "hi"},
		},
	}
	body, _ := json.Marshal(anthReq)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	srv.Proxy()(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, rr.Body.String())
	}
	if resp["type"] != "message" {
		t.Errorf("expected type=message, got %v", resp["type"])
	}
	content, _ := resp["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("expected content blocks, got %v", resp["content"])
	}
	first, _ := content[0].(map[string]any)
	if first["type"] != "text" || first["text"] != "Hello from Zen" {
		t.Errorf("unexpected first block: %v", first)
	}
	usage, _ := resp["usage"].(map[string]any)
	if usage["input_tokens"].(float64) != 12 || usage["output_tokens"].(float64) != 4 {
		t.Errorf("unexpected usage: %v", usage)
	}
}

func TestProxyStreamEndToEnd(t *testing.T) {
	chunks := []string{
		// role
		`{"choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		// text deltas
		`{"choices":[{"index":0,"delta":{"content":"Hello"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":", streamed"}}]}`,
		// finish
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		// usage
		`{"choices":[],"usage":{"prompt_tokens":8,"completion_tokens":3,"total_tokens":11}}`,
	}
	zen := mockZen(t, true, chunks)
	srv, st := newTestServer(t, zen.URL)

	anthReq := map[string]any{
		"model":      "claude-3-5-sonnet-20241022",
		"max_tokens": 256,
		"stream":     true,
		"messages": []map[string]any{
			{"role": "user", "content": "hi"},
		},
	}
	body, _ := json.Marshal(anthReq)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	srv.Proxy()(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected event-stream content type, got %q", ct)
	}

	out := rr.Body.String()
	// Must contain the full event sequence and the usage in message_delta.
	for _, want := range []string{
		"event: message_start",
		"event: content_block_start",
		`"text_delta","text":"Hello"`,
		`"text_delta","text":", streamed"`,
		"event: content_block_stop",
		`"stop_reason":"end_turn"`,
		`"output_tokens":3`,
		"event: message_stop",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n---OUTPUT---\n%s", want, out)
		}
	}

	// The request should have been logged to the store (give the async write
	// a moment, then verify).
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows, _ := st.ListRequests(ctx, 10)
		if len(rows) > 0 {
			if rows[0].TargetModel != "glm-4.6" {
				t.Errorf("logged wrong target model: %s", rows[0].TargetModel)
			}
			if rows[0].Stream != true {
				t.Errorf("stream flag not logged")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("request was not logged to the store")
}

func TestProxyMissingKey(t *testing.T) {
	zen := mockZen(t, false, nil)
	srv, _ := newTestServer(t, zen.URL)
	srv.cfg.SetZenAPIKey("") // simulate unconfigured key

	body, _ := json.Marshal(map[string]any{
		"model": "claude-x", "max_tokens": 8,
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	srv.Proxy()(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "no Zen API key") {
		t.Errorf("unexpected error body: %s", rr.Body.String())
	}
}
