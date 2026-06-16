package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Kiowx/opencode-cc/internal/store"
)

func TestResponsesNonStream(t *testing.T) {
	var upstreamBody struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("upstream path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"chatcmpl-response",
			"choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":6,"completion_tokens":1,"total_tokens":7}
		}`)
	})
	_, st, httpSrv := newOpenAITestServer(t, upstream)

	body := `{
		"model":"client-model",
		"instructions":"Be concise.",
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Say OK"}]}],
		"stream":false
	}`
	resp, err := http.Post(httpSrv.URL+"/v1/responses", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, raw)
	}
	if upstreamBody.Model != "glm-5.1" {
		t.Fatalf("mapped model = %q", upstreamBody.Model)
	}
	if len(upstreamBody.Messages) != 2 || upstreamBody.Messages[0].Role != "system" {
		t.Fatalf("upstream messages = %+v", upstreamBody.Messages)
	}

	var out struct {
		Object string `json:"object"`
		Status string `json:"status"`
		Model  string `json:"model"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Object != "response" || out.Status != "completed" || out.Model != "client-model" {
		t.Fatalf("unexpected response: %s", raw)
	}
	if len(out.Output) != 1 || out.Output[0].Content[0].Text != "OK" {
		t.Fatalf("unexpected output: %s", raw)
	}
	if out.Usage.InputTokens != 6 || out.Usage.OutputTokens != 1 {
		t.Fatalf("unexpected usage: %+v", out.Usage)
	}

	waitForRequestLog(t, st, func(row store.RequestRow) bool {
		return row.Path == "/v1/responses" &&
			row.IncomingModel == "client-model" &&
			row.TargetModel == "glm-5.1" &&
			row.InputTokens == 6 &&
			row.OutputTokens == 1
	})
}

func TestResponsesStream(t *testing.T) {
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model         string `json:"model"`
			Stream        bool   `json:"stream"`
			StreamOptions struct {
				IncludeUsage bool `json:"include_usage"`
			} `json:"stream_options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		if body.Model != "glm-5.1" || !body.Stream || !body.StreamOptions.IncludeUsage {
			t.Errorf("unexpected upstream request: %+v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`{"id":"chatcmpl-stream","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-stream","choices":[{"index":0,"delta":{"content":"OK"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-stream","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			`{"id":"chatcmpl-stream","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`,
		}
		for _, chunk := range chunks {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", chunk)
			if flusher != nil {
				flusher.Flush()
			}
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	})
	_, st, httpSrv := newOpenAITestServer(t, upstream)

	body := `{
		"model":"client-model",
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Say OK"}]}],
		"stream":true
	}`
	resp, err := http.Post(httpSrv.URL+"/v1/responses", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, raw)
	}
	out := string(raw)
	for _, want := range []string{
		"event: response.created",
		"event: response.output_text.delta",
		`"delta":"OK"`,
		"event: response.output_text.done",
		"event: response.completed",
		`"input_tokens":5`,
		`"output_tokens":1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stream missing %q:\n%s", want, out)
		}
	}

	waitForRequestLog(t, st, func(row store.RequestRow) bool {
		return row.Path == "/v1/responses" &&
			row.Stream &&
			row.InputTokens == 5 &&
			row.OutputTokens == 1 &&
			row.StopReason == "stop"
	})
}

func TestResponsesRejectsInvalidJSON(t *testing.T) {
	_, _, httpSrv := newOpenAITestServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("upstream should not be called")
	}))
	resp, err := http.Post(httpSrv.URL+"/v1/responses", "application/json", strings.NewReader(`{"model":`))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d: %s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), `"type":"invalid_request_error"`) {
		t.Fatalf("unexpected error: %s", raw)
	}
}
