package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConvertResponsesRequestForCodex(t *testing.T) {
	body := []byte(`{
		"model":"client-model",
		"instructions":"You are a coding agent.",
		"input":[
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"Use PowerShell."}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Inspect the repo."}]},
			{"type":"function_call","call_id":"call_123","name":"shell_command","arguments":"{\"command\":\"Get-ChildItem\"}"},
			{"type":"function_call_output","call_id":"call_123","output":"README.md"}
		],
		"tools":[
			{"type":"function","name":"shell_command","description":"Run a command","parameters":{"type":"object","properties":{"command":{"type":"string"}}}},
			{"type":"web_search"}
		],
		"tool_choice":"auto",
		"parallel_tool_calls":false,
		"stream":true
	}`)
	req, err := ParseResponsesRequest(body)
	if err != nil {
		t.Fatalf("parse request: %v", err)
	}
	out, err := ConvertResponsesRequest(req, func(string) string { return "kimi-k2.7-code" })
	if err != nil {
		t.Fatalf("convert request: %v", err)
	}

	if out.Model != "kimi-k2.7-code" || !out.Stream {
		t.Fatalf("unexpected request header fields: %+v", out)
	}
	if out.StreamOptions == nil || !out.StreamOptions.IncludeUsage {
		t.Fatal("stream_options.include_usage was not enabled")
	}
	if out.ParallelToolCalls == nil || *out.ParallelToolCalls {
		t.Fatalf("parallel_tool_calls = %v, want false", out.ParallelToolCalls)
	}
	if len(out.Tools) != 1 || out.Tools[0].Function.Name != "shell_command" {
		t.Fatalf("function tools = %+v", out.Tools)
	}
	if len(out.Messages) != 5 {
		t.Fatalf("messages = %+v", out.Messages)
	}
	if out.Messages[0].Role != "system" || out.Messages[0].Content != "You are a coding agent." {
		t.Fatalf("instructions message = %+v", out.Messages[0])
	}
	if out.Messages[1].Role != "system" || out.Messages[1].Content != "Use PowerShell." {
		t.Fatalf("developer message = %+v", out.Messages[1])
	}
	if out.Messages[3].Role != "assistant" || len(out.Messages[3].ToolCalls) != 1 {
		t.Fatalf("function call message = %+v", out.Messages[3])
	}
	if out.Messages[4].Role != "tool" || out.Messages[4].ToolCallID != "call_123" {
		t.Fatalf("function output message = %+v", out.Messages[4])
	}
}

func TestConvertResponsesResponse(t *testing.T) {
	finish := "tool_calls"
	in := &OpenAIResponse{
		ID: "chatcmpl-test",
		Choices: []OpenAIChoice{{
			Index: 0,
			Message: &OpenAIMessage{
				Role:    "assistant",
				Content: "checking",
				ToolCalls: []OpenAIToolCall{{
					ID:   "call_123",
					Type: "function",
					Function: OpenAIFunctionCall{
						Name:      "shell_command",
						Arguments: `{"command":"Get-ChildItem"}`,
					},
				}},
			},
			FinishReason: &finish,
		}},
		Usage: OpenAIUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}

	out := ConvertResponsesResponse(in, "client-model")
	if out.Object != "response" || out.Status != "completed" || out.Model != "client-model" {
		t.Fatalf("unexpected response: %+v", out)
	}
	if len(out.Output) != 2 {
		t.Fatalf("output = %+v", out.Output)
	}
	if out.Output[0].Type != "message" || out.Output[0].Content[0].Text != "checking" {
		t.Fatalf("message output = %+v", out.Output[0])
	}
	if out.Output[1].Type != "function_call" || out.Output[1].CallID != "call_123" {
		t.Fatalf("tool output = %+v", out.Output[1])
	}
	if out.Usage == nil || out.Usage.TotalTokens != 15 {
		t.Fatalf("usage = %+v", out.Usage)
	}
}

func TestResponsesStreamConverterTextAndTool(t *testing.T) {
	var stream strings.Builder
	converter, err := NewResponsesStreamConverter(&stream, "client-model")
	if err != nil {
		t.Fatalf("new converter: %v", err)
	}
	if err := converter.HandleChunk(&OpenAIStreamChunk{
		Choices: []OpenAIChoice{{
			Delta: OpenAIDelta{Content: "I will inspect."},
		}},
	}); err != nil {
		t.Fatalf("text chunk: %v", err)
	}
	if err := converter.HandleChunk(&OpenAIStreamChunk{
		Choices: []OpenAIChoice{{
			Delta: OpenAIDelta{ToolCalls: []OpenAIToolCall{{
				Index: 0,
				ID:    "call_123",
				Type:  "function",
				Function: OpenAIFunctionCall{
					Name:      "shell_command",
					Arguments: `{"command":`,
				},
			}}},
		}},
	}); err != nil {
		t.Fatalf("tool start: %v", err)
	}
	finish := "tool_calls"
	if err := converter.HandleChunk(&OpenAIStreamChunk{
		Choices: []OpenAIChoice{{
			Delta: OpenAIDelta{ToolCalls: []OpenAIToolCall{{
				Index: 0,
				Function: OpenAIFunctionCall{
					Arguments: `"Get-ChildItem"}`,
				},
			}}},
			FinishReason: &finish,
		}},
		Usage: &OpenAIUsage{PromptTokens: 20, CompletionTokens: 8, TotalTokens: 28},
	}); err != nil {
		t.Fatalf("tool continuation: %v", err)
	}
	if err := converter.Finalize(); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	out := stream.String()
	for _, want := range []string{
		"event: response.created",
		"event: response.output_text.delta",
		`"delta":"I will inspect."`,
		"event: response.function_call_arguments.delta",
		`"arguments":"{\"command\":\"Get-ChildItem\"}"`,
		"event: response.completed",
		`"input_tokens":20`,
		`"output_tokens":8`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stream missing %q:\n%s", want, out)
		}
	}

	for _, block := range strings.Split(strings.TrimSpace(out), "\n\n") {
		lines := strings.Split(block, "\n")
		if len(lines) != 2 || !strings.HasPrefix(lines[1], "data: ") {
			t.Fatalf("invalid SSE block: %q", block)
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[1], "data: ")), &event); err != nil {
			t.Fatalf("invalid event JSON: %v", err)
		}
		if _, ok := event["sequence_number"]; !ok {
			t.Fatalf("event has no sequence_number: %v", event)
		}
	}
}
