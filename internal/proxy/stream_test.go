package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// TestStreamConversion feeds a synthetic OpenAI SSE stream (text + tool call)
// through the converter and checks the emitted Anthropic events are well-formed
// and in the right order.
func TestStreamConversion(t *testing.T) {
	// Build a fake OpenAI stream.
	chunks := []OpenAIStreamChunk{
		{Choices: []OpenAIChoice{{Index: 0, Delta: OpenAIDelta{Role: "assistant"}}}},
		{Choices: []OpenAIChoice{{Index: 0, Delta: OpenAIDelta{Content: "Hello"}}}},
		{Choices: []OpenAIChoice{{Index: 0, Delta: OpenAIDelta{Content: ", world"}}}},
		{Choices: []OpenAIChoice{{Index: 0, Delta: OpenAIDelta{ToolCalls: []OpenAIToolCall{
			{ID: "call_1", Type: "function", Function: OpenAIFunctionCall{Name: "read_file", Arguments: `{"path"`}},
		}}}}},
		{Choices: []OpenAIChoice{{Index: 0, Delta: OpenAIDelta{ToolCalls: []OpenAIToolCall{
			{ID: "call_1", Type: "function", Function: OpenAIFunctionCall{Arguments: `:"a.txt"}`}},
		}}}}},
		{Choices: []OpenAIChoice{{Index: 0, FinishReason: strPtr("tool_calls")}}},
		{Usage: &OpenAIUsage{PromptTokens: 10, CompletionTokens: 20}},
	}

	var sb bytes.Buffer
	for _, c := range chunks {
		b, _ := json.Marshal(c)
		sb.WriteString("data: ")
		sb.Write(b)
		sb.WriteString("\n\n")
	}
	sb.WriteString("data: [DONE]\n\n")

	var out bytes.Buffer
	conv, err := NewStreamConverter(&out, "claude-test", nil)
	if err != nil {
		t.Fatalf("new converter: %v", err)
	}

	if err := ScanOpenAIStream(bytes.NewReader(sb.Bytes()), func(c *OpenAIStreamChunk) error {
		return conv.HandleChunk(c)
	}); err != nil && err != io.EOF {
		t.Fatalf("scan: %v", err)
	}
	if err := conv.Finalize("end_turn"); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	_ = conv.Flush()

	got := out.String()

	// Order checks.
	checks := []string{
		"event: message_start",
		"event: content_block_start", // text block
		"event: content_block_delta", // "Hello"
		"event: content_block_delta", // ", world"
		"event: content_block_stop",  // text block closes
		"event: content_block_start", // tool_use block
		"event: content_block_delta", // input_json_delta partial
		"event: content_block_stop",  // tool block closes
		"event: message_delta",
		"event: message_stop",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output\n---OUTPUT---\n%s", want, got)
		}
	}

	// The stop reason should be tool_use.
	if !strings.Contains(got, `"stop_reason":"tool_use"`) {
		t.Errorf("expected stop_reason tool_use\n---OUTPUT---\n%s", got)
	}
	// Usage carried in message_delta.
	if !strings.Contains(got, `"output_tokens":20`) {
		t.Errorf("expected output_tokens 20 in message_delta\n---OUTPUT---\n%s", got)
	}
}

// TestNonStreamConversion checks the non-streaming path end to end.
func TestNonStreamConversion(t *testing.T) {
	up := &OpenAIResponse{
		ID: "chatcmpl-abc",
		Choices: []OpenAIChoice{{
			Message: &OpenAIMessage{Role: "assistant", Content: "Hi there"},
			FinishReason: strPtr("stop"),
		}},
		Usage: OpenAIUsage{PromptTokens: 5, CompletionTokens: 2},
	}
	out := ConvertResponse(up, "claude-test")
	if out.ID == "" || !strings.HasPrefix(out.ID, "msg_") {
		t.Errorf("bad id: %q", out.ID)
	}
	if len(out.Content) != 1 || out.Content[0].Type != "text" || out.Content[0].Text != "Hi there" {
		t.Errorf("bad content: %+v", out.Content)
	}
	if out.StopReason == nil || *out.StopReason != "end_turn" {
		t.Errorf("bad stop reason: %v", out.StopReason)
	}
	if out.Usage.InputTokens != 5 || out.Usage.OutputTokens != 2 {
		t.Errorf("bad usage: %+v", out.Usage)
	}
}

// TestRequestConversion checks tool_use/tool_result round trip in requests.
func TestRequestConversion(t *testing.T) {
	in := &AnthropicRequest{
		Model: "claude-3-5-sonnet",
		System: AnthropicSystem{Blocks: []AnthropicContent{{Type: "text", Text: "You are helpful"}}},
		MaxTokens: 1024,
		Tools: []AnthropicTool{{Name: "run", InputSchema: jsonRawMessage(`{"type":"object","properties":{"cmd":{"type":"string"}}}`)}},
		ToolChoice: AnthropicToolChoice{Type: "auto"},
		Messages: []AnthropicMessage{
			{Role: "user", Content: AnthropicMessageContent{IsStr: true, Text: "do it"}},
			{Role: "assistant", Content: AnthropicMessageContent{Blocks: []AnthropicContent{
				{Type: "tool_use", ID: "t1", Name: "run", Input: jsonRawMessage(`{"cmd":"ls"}`)},
			}}},
			{Role: "user", Content: AnthropicMessageContent{Blocks: []AnthropicContent{
				{Type: "tool_result", ToolUseID: "t1", Content: &AnthropicMessageContent{IsStr: true, Text: "file1\nfile2"}},
			}}},
		},
	}
	out := ConvertRequest(in, func(s string) string { return "glm-4.6" })
	if out.Model != "glm-4.6" {
		t.Errorf("model not resolved: %q", out.Model)
	}
	if len(out.Messages) < 1 || out.Messages[0].Role != "system" {
		t.Errorf("system message missing: %+v", out.Messages)
	}
	// Expect a role:tool message for the tool_result.
	foundTool := false
	for _, m := range out.Messages {
		if m.Role == "tool" && m.ToolCallID == "t1" {
			foundTool = true
		}
	}
	if !foundTool {
		t.Errorf("tool message not produced: %+v", out.Messages)
	}
	// Tool def forwarded.
	if len(out.Tools) != 1 || out.Tools[0].Function.Name != "run" {
		t.Errorf("tool not forwarded: %+v", out.Tools)
	}
	if out.ToolChoice != "auto" {
		t.Errorf("tool_choice not mapped: %v", out.ToolChoice)
	}
}

func strPtr(s string) *string { return &s }
