package proxy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// ResponsesRequest is the client-facing OpenAI Responses API request used by
// Codex. Fields that have no Chat Completions equivalent are intentionally
// accepted and ignored by the converter.
type ResponsesRequest struct {
	Model             string          `json:"model"`
	Instructions      json.RawMessage `json:"instructions"`
	Input             json.RawMessage `json:"input"`
	MaxOutputTokens   *int            `json:"max_output_tokens,omitempty"`
	Temperature       *float64        `json:"temperature,omitempty"`
	TopP              *float64        `json:"top_p,omitempty"`
	Stream            bool            `json:"stream,omitempty"`
	Tools             []ResponsesTool `json:"tools,omitempty"`
	ToolChoice        any             `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool           `json:"parallel_tool_calls,omitempty"`
}

// ResponsesTool models function tools. Codex may also send hosted tools such
// as web_search or image_generation; those are omitted because the Zen Chat
// Completions upstream cannot execute OpenAI-hosted tools.
type ResponsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

type responsesInputItem struct {
	Type      string          `json:"type"`
	Role      string          `json:"role,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	ID        string          `json:"id,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments string          `json:"arguments,omitempty"`
	Input     string          `json:"input,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`
}

type responsesContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// ParseResponsesRequest decodes a Responses API request object.
func ParseResponsesRequest(body []byte) (*ResponsesRequest, error) {
	var req ResponsesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("request body is not valid Responses JSON: %w", err)
	}
	if req.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	return &req, nil
}

// ConvertResponsesRequest translates the subset of the Responses API used by
// Codex into an OpenAI Chat Completions request for Zen's /go endpoint.
func ConvertResponsesRequest(
	in *ResponsesRequest,
	resolveModel func(string) string,
) (*OpenAIRequest, error) {
	out := &OpenAIRequest{
		Model:             resolveModel(in.Model),
		MaxTokens:         in.MaxOutputTokens,
		Temperature:       in.Temperature,
		TopP:              in.TopP,
		Stream:            in.Stream,
		ParallelToolCalls: in.ParallelToolCalls,
	}

	if instructions := rawText(in.Instructions); instructions != "" {
		out.Messages = append(out.Messages, OpenAIMessage{
			Role:    "system",
			Content: instructions,
		})
	}

	messages, err := responsesInputToMessages(in.Input)
	if err != nil {
		return nil, err
	}
	out.Messages = append(out.Messages, messages...)

	for _, tool := range in.Tools {
		if tool.Type != "function" || tool.Name == "" {
			continue
		}
		out.Tools = append(out.Tools, OpenAITool{
			Type: "function",
			Function: OpenAIToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  ensureObjectSchema(tool.Parameters),
			},
		})
	}
	if len(out.Tools) == 0 {
		out.ToolChoice = "none"
	} else {
		out.ToolChoice = responsesToolChoice(in.ToolChoice)
	}
	if out.Stream {
		out.StreamOptions = &OpenAIStreamOptions{IncludeUsage: true}
	}
	return out, nil
}

func responsesInputToMessages(raw json.RawMessage) ([]OpenAIMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []OpenAIMessage{{Role: "user", Content: text}}, nil
	}

	var items []responsesInputItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("input must be a string or an array of Responses input items")
	}

	var out []OpenAIMessage
	for _, item := range items {
		switch item.Type {
		case "", "message":
			role := item.Role
			if role == "developer" {
				role = "system"
			}
			if role == "" {
				role = "user"
			}
			content := responsesMessageContent(item.Content, role)
			out = append(out, OpenAIMessage{Role: role, Content: content})
		case "function_call":
			callID := item.CallID
			if callID == "" {
				callID = item.ID
			}
			appendResponsesToolCall(&out, OpenAIToolCall{
				ID:   callID,
				Type: "function",
				Function: OpenAIFunctionCall{
					Name:      item.Name,
					Arguments: canonicalJSONString(item.Arguments),
				},
			})
		case "custom_tool_call":
			callID := item.CallID
			if callID == "" {
				callID = item.ID
			}
			args, _ := json.Marshal(map[string]string{"input": item.Input})
			appendResponsesToolCall(&out, OpenAIToolCall{
				ID:   callID,
				Type: "function",
				Function: OpenAIFunctionCall{
					Name:      item.Name,
					Arguments: string(args),
				},
			})
		case "function_call_output", "custom_tool_call_output":
			callID := item.CallID
			if callID == "" {
				callID = item.ID
			}
			out = append(out, OpenAIMessage{
				Role:       "tool",
				ToolCallID: callID,
				Content:    rawText(item.Output),
			})
		case "reasoning":
			// Reasoning items are model-internal state and have no portable
			// Chat Completions representation.
			continue
		}
	}
	return out, nil
}

func appendResponsesToolCall(messages *[]OpenAIMessage, call OpenAIToolCall) {
	if len(*messages) > 0 {
		last := &(*messages)[len(*messages)-1]
		if last.Role == "assistant" {
			last.ToolCalls = append(last.ToolCalls, call)
			return
		}
	}
	*messages = append(*messages, OpenAIMessage{
		Role:      "assistant",
		Content:   "",
		ToolCalls: []OpenAIToolCall{call},
	})
}

func responsesMessageContent(raw json.RawMessage, role string) any {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []responsesContentPart
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	if role == "system" {
		var text strings.Builder
		for _, part := range parts {
			if isResponsesTextPart(part.Type) {
				text.WriteString(part.Text)
			}
		}
		return text.String()
	}
	converted := make([]OpenAIContentPart, 0, len(parts))
	for _, part := range parts {
		switch {
		case isResponsesTextPart(part.Type):
			converted = append(converted, OpenAIContentPart{Type: "text", Text: part.Text})
		case part.Type == "input_image" && part.ImageURL != "":
			converted = append(converted, OpenAIContentPart{
				Type: "image_url",
				ImageURL: &OpenAIImageURL{
					URL:    part.ImageURL,
					Detail: part.Detail,
				},
			})
		}
	}
	if len(converted) == 1 && converted[0].Type == "text" {
		return converted[0].Text
	}
	return converted
}

func isResponsesTextPart(kind string) bool {
	switch kind {
	case "input_text", "output_text", "text":
		return true
	default:
		return false
	}
}

func rawText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []responsesContentPart
	if json.Unmarshal(raw, &parts) == nil {
		var out strings.Builder
		for _, part := range parts {
			if isResponsesTextPart(part.Type) {
				out.WriteString(part.Text)
			}
		}
		return out.String()
	}
	return string(raw)
}

func responsesToolChoice(choice any) any {
	if choice == nil {
		return "auto"
	}
	typed, ok := choice.(map[string]any)
	if !ok {
		return choice
	}
	if typ, _ := typed["type"].(string); typ == "function" {
		if name, _ := typed["name"].(string); name != "" {
			return map[string]any{
				"type":     "function",
				"function": map[string]string{"name": name},
			}
		}
	}
	return "auto"
}

func canonicalJSONString(value string) string {
	if strings.TrimSpace(value) == "" {
		return "{}"
	}
	canonical, ok := canonicalJSON(json.RawMessage(value))
	if !ok {
		return value
	}
	return string(canonical)
}

// ResponsesResponse is a Responses API response object.
type ResponsesResponse struct {
	ID                string                `json:"id"`
	Object            string                `json:"object"`
	CreatedAt         int64                 `json:"created_at"`
	CompletedAt       *int64                `json:"completed_at"`
	Status            string                `json:"status"`
	Error             any                   `json:"error"`
	IncompleteDetails any                   `json:"incomplete_details"`
	Instructions      any                   `json:"instructions"`
	MaxOutputTokens   *int                  `json:"max_output_tokens"`
	Model             string                `json:"model"`
	Output            []ResponsesOutputItem `json:"output"`
	ParallelToolCalls bool                  `json:"parallel_tool_calls"`
	PreviousResponse  any                   `json:"previous_response_id"`
	Reasoning         map[string]any        `json:"reasoning"`
	Store             bool                  `json:"store"`
	Temperature       any                   `json:"temperature"`
	Text              map[string]any        `json:"text"`
	ToolChoice        any                   `json:"tool_choice"`
	Tools             []any                 `json:"tools"`
	TopP              any                   `json:"top_p"`
	Truncation        string                `json:"truncation"`
	Usage             *ResponsesUsage       `json:"usage"`
	User              any                   `json:"user"`
	Metadata          map[string]any        `json:"metadata"`
}

// ResponsesOutputItem is either an assistant message or function call.
type ResponsesOutputItem struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Status    string                 `json:"status,omitempty"`
	Role      string                 `json:"role,omitempty"`
	Content   []ResponsesContentPart `json:"content,omitempty"`
	CallID    string                 `json:"call_id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Arguments string                 `json:"arguments,omitempty"`
}

// ResponsesContentPart is an output_text part.
type ResponsesContentPart struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Annotations []any  `json:"annotations"`
}

// ResponsesUsage mirrors the token accounting fields used by Codex.
type ResponsesUsage struct {
	InputTokens         int                        `json:"input_tokens"`
	InputTokensDetails  ResponsesInputTokenDetail  `json:"input_tokens_details"`
	OutputTokens        int                        `json:"output_tokens"`
	OutputTokensDetails ResponsesOutputTokenDetail `json:"output_tokens_details"`
	TotalTokens         int                        `json:"total_tokens"`
}

type ResponsesInputTokenDetail struct {
	CachedTokens int `json:"cached_tokens"`
}

type ResponsesOutputTokenDetail struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

// ConvertResponsesResponse translates a non-streaming Chat Completions response
// into a Responses API response.
func ConvertResponsesResponse(in *OpenAIResponse, requestModel string) *ResponsesResponse {
	created := time.Now().Unix()
	out := newResponsesResponse("resp_"+randHex(24), requestModel, created, "completed")
	completed := created
	out.CompletedAt = &completed
	out.Usage = responsesUsage(in.Usage)

	if len(in.Choices) == 0 || in.Choices[0].Message == nil {
		return out
	}
	choice := in.Choices[0]
	message := choice.Message
	if text := messageContentString(message); text != "" {
		out.Output = append(out.Output, completedMessageItem("msg_"+randHex(24), text))
	}
	for _, tool := range message.ToolCalls {
		callID := tool.ID
		if callID == "" {
			callID = "call_" + randHex(24)
		}
		out.Output = append(out.Output, ResponsesOutputItem{
			ID:        "fc_" + randHex(24),
			Type:      "function_call",
			Status:    "completed",
			CallID:    callID,
			Name:      tool.Function.Name,
			Arguments: tool.Function.Arguments,
		})
	}
	if choice.FinishReason != nil && *choice.FinishReason == "length" {
		out.Status = "incomplete"
		out.CompletedAt = nil
		out.IncompleteDetails = map[string]string{"reason": "max_output_tokens"}
	}
	return out
}

func newResponsesResponse(id, model string, created int64, status string) *ResponsesResponse {
	return &ResponsesResponse{
		ID:                id,
		Object:            "response",
		CreatedAt:         created,
		Status:            status,
		Error:             nil,
		IncompleteDetails: nil,
		Instructions:      nil,
		MaxOutputTokens:   nil,
		Model:             model,
		Output:            []ResponsesOutputItem{},
		ParallelToolCalls: true,
		PreviousResponse:  nil,
		Reasoning:         map[string]any{"effort": nil, "summary": nil},
		Store:             false,
		Temperature:       nil,
		Text:              map[string]any{"format": map[string]string{"type": "text"}},
		ToolChoice:        "auto",
		Tools:             []any{},
		TopP:              nil,
		Truncation:        "disabled",
		Usage:             nil,
		User:              nil,
		Metadata:          map[string]any{},
	}
}

func completedMessageItem(id, text string) ResponsesOutputItem {
	return ResponsesOutputItem{
		ID:     id,
		Type:   "message",
		Status: "completed",
		Role:   "assistant",
		Content: []ResponsesContentPart{{
			Type:        "output_text",
			Text:        text,
			Annotations: []any{},
		}},
	}
}

func responsesUsage(usage OpenAIUsage) *ResponsesUsage {
	total := usage.TotalTokens
	if total == 0 {
		total = usage.PromptTokens + usage.CompletionTokens
	}
	return &ResponsesUsage{
		InputTokens:         usage.PromptTokens,
		InputTokensDetails:  ResponsesInputTokenDetail{CachedTokens: usage.CachedPromptTokens()},
		OutputTokens:        usage.CompletionTokens,
		OutputTokensDetails: ResponsesOutputTokenDetail{},
		TotalTokens:         total,
	}
}

type responsesTextState struct {
	id          string
	outputIndex int
	text        strings.Builder
}

type responsesToolState struct {
	item        ResponsesOutputItem
	outputIndex int
}

// ResponsesStreamConverter converts Chat Completions chunks to Responses API
// streaming events.
type ResponsesStreamConverter struct {
	writer    *bufio.Writer
	model     string
	id        string
	createdAt int64
	sequence  int
	nextIndex int

	text  *responsesTextState
	tools map[int]*responsesToolState
	order []int

	inputTokens  int
	outputTokens int
	finishReason string
	finalized    bool
}

// NewResponsesStreamConverter emits response.created and response.in_progress.
func NewResponsesStreamConverter(w io.Writer, model string) (*ResponsesStreamConverter, error) {
	converter := &ResponsesStreamConverter{
		writer:    bufio.NewWriter(w),
		model:     model,
		id:        "resp_" + randHex(24),
		createdAt: time.Now().Unix(),
		tools:     make(map[int]*responsesToolState),
	}
	if err := converter.writeEvent("response.created", map[string]any{
		"type":     "response.created",
		"response": newResponsesResponse(converter.id, model, converter.createdAt, "in_progress"),
	}); err != nil {
		return nil, err
	}
	if err := converter.writeEvent("response.in_progress", map[string]any{
		"type":     "response.in_progress",
		"response": newResponsesResponse(converter.id, model, converter.createdAt, "in_progress"),
	}); err != nil {
		return nil, err
	}
	return converter, nil
}

// HandleChunk emits Responses events for one Chat Completions chunk.
func (c *ResponsesStreamConverter) HandleChunk(chunk *OpenAIStreamChunk) error {
	if chunk == nil {
		return nil
	}
	if chunk.Usage != nil {
		c.inputTokens = chunk.Usage.PromptTokens
		c.outputTokens = chunk.Usage.CompletionTokens
	}
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != "" {
			if err := c.handleText(choice.Delta.Content); err != nil {
				return err
			}
		}
		for _, tool := range choice.Delta.ToolCalls {
			if err := c.handleTool(tool); err != nil {
				return err
			}
		}
		if choice.FinishReason != nil {
			c.finishReason = *choice.FinishReason
		}
	}
	return nil
}

func (c *ResponsesStreamConverter) handleText(delta string) error {
	if c.text == nil {
		c.text = &responsesTextState{
			id:          "msg_" + randHex(24),
			outputIndex: c.nextIndex,
		}
		c.nextIndex++
		if err := c.writeEvent("response.output_item.added", map[string]any{
			"type":         "response.output_item.added",
			"output_index": c.text.outputIndex,
			"item": map[string]any{
				"id":      c.text.id,
				"type":    "message",
				"status":  "in_progress",
				"role":    "assistant",
				"content": []any{},
			},
		}); err != nil {
			return err
		}
		if err := c.writeEvent("response.content_part.added", map[string]any{
			"type":          "response.content_part.added",
			"item_id":       c.text.id,
			"output_index":  c.text.outputIndex,
			"content_index": 0,
			"part": ResponsesContentPart{
				Type:        "output_text",
				Text:        "",
				Annotations: []any{},
			},
		}); err != nil {
			return err
		}
	}
	c.text.text.WriteString(delta)
	return c.writeEvent("response.output_text.delta", map[string]any{
		"type":          "response.output_text.delta",
		"item_id":       c.text.id,
		"output_index":  c.text.outputIndex,
		"content_index": 0,
		"delta":         delta,
		"logprobs":      []any{},
	})
}

func (c *ResponsesStreamConverter) handleTool(tool OpenAIToolCall) error {
	state := c.tools[tool.Index]
	if state == nil {
		callID := tool.ID
		if callID == "" {
			callID = "call_" + randHex(24)
		}
		state = &responsesToolState{
			item: ResponsesOutputItem{
				ID:     "fc_" + randHex(24),
				Type:   "function_call",
				Status: "in_progress",
				CallID: callID,
				Name:   tool.Function.Name,
			},
			outputIndex: c.nextIndex,
		}
		c.nextIndex++
		c.tools[tool.Index] = state
		c.order = append(c.order, tool.Index)
		if err := c.writeEvent("response.output_item.added", map[string]any{
			"type":         "response.output_item.added",
			"output_index": state.outputIndex,
			"item": map[string]any{
				"id":        state.item.ID,
				"type":      "function_call",
				"status":    "in_progress",
				"call_id":   state.item.CallID,
				"name":      state.item.Name,
				"arguments": "",
			},
		}); err != nil {
			return err
		}
	}
	if tool.ID != "" {
		state.item.CallID = tool.ID
	}
	if tool.Function.Name != "" {
		state.item.Name = tool.Function.Name
	}
	if tool.Function.Arguments == "" {
		return nil
	}
	state.item.Arguments += tool.Function.Arguments
	return c.writeEvent("response.function_call_arguments.delta", map[string]any{
		"type":         "response.function_call_arguments.delta",
		"item_id":      state.item.ID,
		"output_index": state.outputIndex,
		"delta":        tool.Function.Arguments,
	})
}

// Finalize emits all done events followed by response.completed or
// response.incomplete. It is safe to call more than once.
func (c *ResponsesStreamConverter) Finalize() error {
	if c.finalized {
		return nil
	}
	c.finalized = true
	if c.text == nil && len(c.tools) == 0 {
		if err := c.handleText(""); err != nil {
			return err
		}
	}

	output := make([]ResponsesOutputItem, c.nextIndex)
	if c.text != nil {
		text := c.text.text.String()
		part := ResponsesContentPart{
			Type:        "output_text",
			Text:        text,
			Annotations: []any{},
		}
		item := completedMessageItem(c.text.id, text)
		output[c.text.outputIndex] = item
		if err := c.writeEvent("response.output_text.done", map[string]any{
			"type":          "response.output_text.done",
			"item_id":       c.text.id,
			"output_index":  c.text.outputIndex,
			"content_index": 0,
			"text":          text,
			"logprobs":      []any{},
		}); err != nil {
			return err
		}
		if err := c.writeEvent("response.content_part.done", map[string]any{
			"type":          "response.content_part.done",
			"item_id":       c.text.id,
			"output_index":  c.text.outputIndex,
			"content_index": 0,
			"part":          part,
		}); err != nil {
			return err
		}
		if err := c.writeEvent("response.output_item.done", map[string]any{
			"type":         "response.output_item.done",
			"output_index": c.text.outputIndex,
			"item":         item,
		}); err != nil {
			return err
		}
	}
	for _, index := range c.order {
		state := c.tools[index]
		state.item.Status = "completed"
		output[state.outputIndex] = state.item
		if err := c.writeEvent("response.function_call_arguments.done", map[string]any{
			"type":         "response.function_call_arguments.done",
			"item_id":      state.item.ID,
			"output_index": state.outputIndex,
			"name":         state.item.Name,
			"arguments":    state.item.Arguments,
		}); err != nil {
			return err
		}
		if err := c.writeEvent("response.output_item.done", map[string]any{
			"type":         "response.output_item.done",
			"output_index": state.outputIndex,
			"item":         state.item,
		}); err != nil {
			return err
		}
	}

	status := "completed"
	event := "response.completed"
	if c.finishReason == "length" {
		status = "incomplete"
		event = "response.incomplete"
	}
	response := newResponsesResponse(c.id, c.model, c.createdAt, status)
	response.Output = output
	response.Usage = responsesUsage(OpenAIUsage{
		PromptTokens:     c.inputTokens,
		CompletionTokens: c.outputTokens,
		TotalTokens:      c.inputTokens + c.outputTokens,
	})
	if status == "completed" {
		completed := time.Now().Unix()
		response.CompletedAt = &completed
	} else {
		response.IncompleteDetails = map[string]string{"reason": "max_output_tokens"}
	}
	return c.writeEvent(event, map[string]any{
		"type":     event,
		"response": response,
	})
}

// EmitError terminates a started stream with a Responses API failure event.
func (c *ResponsesStreamConverter) EmitError(message string) error {
	if c.finalized {
		return nil
	}
	c.finalized = true
	response := newResponsesResponse(c.id, c.model, c.createdAt, "failed")
	response.Error = map[string]string{
		"code":    "server_error",
		"message": message,
	}
	return c.writeEvent("response.failed", map[string]any{
		"type":     "response.failed",
		"response": response,
	})
}

// InputTokens returns usage observed in the final upstream usage chunk.
func (c *ResponsesStreamConverter) InputTokens() int { return c.inputTokens }

// OutputTokens returns usage observed in the final upstream usage chunk.
func (c *ResponsesStreamConverter) OutputTokens() int { return c.outputTokens }

// FinishReason returns the upstream Chat Completions finish reason.
func (c *ResponsesStreamConverter) FinishReason() string { return c.finishReason }

func (c *ResponsesStreamConverter) writeEvent(event string, payload map[string]any) error {
	payload["sequence_number"] = c.sequence
	c.sequence++
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.writer, "event: %s\ndata: %s\n\n", event, body); err != nil {
		return err
	}
	return c.writer.Flush()
}
