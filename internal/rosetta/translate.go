package rosetta

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrUnsupported marks a request or response pear will not translate.
// The gateway returns it to the client instead of forwarding a body the
// other protocol would reject.
var ErrUnsupported = errors.New("translation unsupported")

func unsupported(reason string) error {
	return fmt.Errorf("%w: %s", ErrUnsupported, reason)
}

// WireProtocol maps a provider protocol onto the HTTP API it speaks.
func WireProtocol(providerProtocol string) string {
	switch providerProtocol {
	case "anthropic", "oauth-claude":
		return "anthropic"
	case "responses", "oauth-codex":
		return "responses"
	case "openai", "oauth-copilot":
		return "openai"
	default:
		return ""
	}
}

// ProtocolFromPath reports the client API from the request path.
func ProtocolFromPath(path string) string {
	switch {
	case strings.HasSuffix(path, "/messages"):
		return "anthropic"
	case strings.HasSuffix(path, "/responses"):
		return "responses"
	case strings.Contains(path, "/chat/completions"):
		return "openai"
	default:
		return ""
	}
}

// PathFor is the upstream path for a wire protocol.
func PathFor(wire string) string {
	switch wire {
	case "anthropic":
		return "/v1/messages"
	case "responses":
		return "/v1/responses"
	default:
		return "/v1/chat/completions"
	}
}

type turn struct {
	Role string
	Text string
}

type conversation struct {
	System      string
	Turns       []turn
	MaxTokens   int
	Temperature *float64
}

// TranslateRequest rewrites a non-streaming plain-text conversation from one
// wire protocol into another and sets model to the upstream id.
func TranslateRequest(body []byte, model, from, to string) ([]byte, error) {
	conv, err := parseConversation(body, from)
	if err != nil {
		return nil, err
	}
	return emitConversation(conv, model, to)
}

// TranslateResponse rewrites a non-streaming text completion from the
// upstream protocol into the client protocol.
func TranslateResponse(body []byte, from, to string) ([]byte, error) {
	rep, err := parseReply(body, from)
	if err != nil {
		return nil, err
	}
	return emitReply(rep, to)
}

func parseConversation(body []byte, from string) (conversation, error) {
	switch from {
	case "openai":
		return parseOpenAIRequest(body)
	case "anthropic":
		return parseAnthropicRequest(body)
	case "responses":
		return parseResponsesRequest(body)
	default:
		return conversation{}, unsupported("unknown client protocol " + from)
	}
}

func parseOpenAIRequest(body []byte) (conversation, error) {
	var req struct {
		Messages            []openAIMessage `json:"messages"`
		MaxTokens           int             `json:"max_tokens"`
		MaxCompletionTokens int             `json:"max_completion_tokens"`
		Temperature         *float64        `json:"temperature"`
		Stream              *bool           `json:"stream"`
		Tools               json.RawMessage `json:"tools"`
		Functions           json.RawMessage `json:"functions"`
		ToolChoice          json.RawMessage `json:"tool_choice"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return conversation{}, unsupported("request is not JSON")
	}
	if err := rejectStreamAndTools(req.Stream, req.Tools, req.Functions, req.ToolChoice); err != nil {
		return conversation{}, err
	}
	var conv conversation
	conv.Temperature = req.Temperature
	conv.MaxTokens = req.MaxTokens
	if conv.MaxTokens == 0 {
		conv.MaxTokens = req.MaxCompletionTokens
	}
	for _, m := range req.Messages {
		if len(m.ToolCalls) > 0 && string(bytes.TrimSpace(m.ToolCalls)) != "null" {
			return conversation{}, unsupported("tool_calls")
		}
		text, err := contentText(m.Content)
		if err != nil {
			return conversation{}, err
		}
		switch m.Role {
		case "system", "developer":
			conv.System = joinSystem(conv.System, text)
		case "user", "assistant":
			conv.Turns = append(conv.Turns, turn{Role: m.Role, Text: text})
		default:
			return conversation{}, unsupported("message role " + m.Role)
		}
	}
	if len(conv.Turns) == 0 {
		return conversation{}, unsupported("no messages")
	}
	return conv, nil
}

type openAIMessage struct {
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	ToolCalls json.RawMessage `json:"tool_calls"`
}

func parseAnthropicRequest(body []byte) (conversation, error) {
	var req struct {
		System      json.RawMessage `json:"system"`
		Messages    []openAIMessage `json:"messages"`
		MaxTokens   int             `json:"max_tokens"`
		Temperature *float64        `json:"temperature"`
		Stream      *bool           `json:"stream"`
		Tools       json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return conversation{}, unsupported("request is not JSON")
	}
	if err := rejectStreamAndTools(req.Stream, req.Tools, nil, nil); err != nil {
		return conversation{}, err
	}
	var conv conversation
	conv.MaxTokens = req.MaxTokens
	conv.Temperature = req.Temperature
	if len(bytes.TrimSpace(req.System)) > 0 && string(bytes.TrimSpace(req.System)) != "null" {
		text, err := contentText(req.System)
		if err != nil {
			return conversation{}, err
		}
		conv.System = text
	}
	for _, m := range req.Messages {
		text, err := contentText(m.Content)
		if err != nil {
			return conversation{}, err
		}
		if m.Role != "user" && m.Role != "assistant" {
			return conversation{}, unsupported("message role " + m.Role)
		}
		conv.Turns = append(conv.Turns, turn{Role: m.Role, Text: text})
	}
	if len(conv.Turns) == 0 {
		return conversation{}, unsupported("no messages")
	}
	return conv, nil
}

func parseResponsesRequest(body []byte) (conversation, error) {
	var req struct {
		Instructions    string          `json:"instructions"`
		Input           json.RawMessage `json:"input"`
		MaxOutputTokens int             `json:"max_output_tokens"`
		Temperature     *float64        `json:"temperature"`
		Stream          *bool           `json:"stream"`
		Tools           json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return conversation{}, unsupported("request is not JSON")
	}
	if err := rejectStreamAndTools(req.Stream, req.Tools, nil, nil); err != nil {
		return conversation{}, err
	}
	conv := conversation{System: req.Instructions, MaxTokens: req.MaxOutputTokens, Temperature: req.Temperature}
	turns, err := parseResponsesInput(req.Input)
	if err != nil {
		return conversation{}, err
	}
	conv.Turns = turns
	if len(conv.Turns) == 0 {
		return conversation{}, unsupported("no messages")
	}
	return conv, nil
}

func parseResponsesInput(raw json.RawMessage) ([]turn, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, unsupported("missing input")
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return []turn{{Role: "user", Text: asString}}, nil
	}
	var items []struct {
		Type    string          `json:"type"`
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, unsupported("input is not text")
	}
	var turns []turn
	for _, item := range items {
		switch item.Type {
		case "", "message":
		default:
			return nil, unsupported("input item " + item.Type)
		}
		text, err := contentText(item.Content)
		if err != nil {
			return nil, err
		}
		role := item.Role
		if role == "" {
			role = "user"
		}
		if role != "user" && role != "assistant" && role != "system" && role != "developer" {
			return nil, unsupported("input role " + role)
		}
		if role == "system" || role == "developer" {
			return nil, unsupported("system input item; use instructions")
		}
		turns = append(turns, turn{Role: role, Text: text})
	}
	return turns, nil
}

func rejectStreamAndTools(stream *bool, tools, functions, toolChoice json.RawMessage) error {
	if stream != nil && *stream {
		return unsupported("streaming")
	}
	if jsonPresent(tools) || jsonPresent(functions) || jsonPresent(toolChoice) {
		return unsupported("tools")
	}
	return nil
}

func jsonPresent(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null" && s != "[]"
}

func contentText(raw json.RawMessage) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "", unsupported("empty content")
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", unsupported("content is not text")
	}
	var b strings.Builder
	for _, p := range parts {
		switch p.Type {
		case "text", "input_text", "output_text":
			b.WriteString(p.Text)
		default:
			return "", unsupported("content part " + p.Type)
		}
	}
	return b.String(), nil
}

func joinSystem(cur, next string) string {
	if cur == "" {
		return next
	}
	if next == "" {
		return cur
	}
	return cur + "\n" + next
}

func emitConversation(conv conversation, model, to string) ([]byte, error) {
	switch to {
	case "openai":
		return emitOpenAIRequest(conv, model)
	case "anthropic":
		return emitAnthropicRequest(conv, model)
	case "responses":
		return emitResponsesRequest(conv, model)
	default:
		return nil, unsupported("unknown upstream protocol " + to)
	}
}

func emitOpenAIRequest(conv conversation, model string) ([]byte, error) {
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	var msgs []msg
	if conv.System != "" {
		msgs = append(msgs, msg{Role: "system", Content: conv.System})
	}
	for _, t := range conv.Turns {
		msgs = append(msgs, msg{Role: t.Role, Content: t.Text})
	}
	out := map[string]any{"model": model, "messages": msgs}
	if conv.MaxTokens > 0 {
		out["max_tokens"] = conv.MaxTokens
	}
	if conv.Temperature != nil {
		out["temperature"] = *conv.Temperature
	}
	return json.Marshal(out)
}

func emitAnthropicRequest(conv conversation, model string) ([]byte, error) {
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	msgs := make([]msg, 0, len(conv.Turns))
	for _, t := range conv.Turns {
		msgs = append(msgs, msg{Role: t.Role, Content: t.Text})
	}
	maxTokens := conv.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	out := map[string]any{
		"model":      model,
		"messages":   msgs,
		"max_tokens": maxTokens,
	}
	if conv.System != "" {
		out["system"] = conv.System
	}
	if conv.Temperature != nil {
		out["temperature"] = *conv.Temperature
	}
	return json.Marshal(out)
}

func emitResponsesRequest(conv conversation, model string) ([]byte, error) {
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	input := make([]msg, 0, len(conv.Turns))
	for _, t := range conv.Turns {
		input = append(input, msg{Role: t.Role, Content: t.Text})
	}
	out := map[string]any{"model": model, "input": input}
	if conv.System != "" {
		out["instructions"] = conv.System
	}
	if conv.MaxTokens > 0 {
		out["max_output_tokens"] = conv.MaxTokens
	}
	if conv.Temperature != nil {
		out["temperature"] = *conv.Temperature
	}
	return json.Marshal(out)
}

type reply struct {
	ID     string
	Text   string
	Stop   string
	InTok  int
	OutTok int
}

func parseReply(body []byte, from string) (reply, error) {
	switch from {
	case "openai":
		return parseOpenAIReply(body)
	case "anthropic":
		return parseAnthropicReply(body)
	case "responses":
		return parseResponsesReply(body)
	default:
		return reply{}, unsupported("unknown upstream protocol " + from)
	}
}

func parseOpenAIReply(body []byte) (reply, error) {
	var res struct {
		ID      string `json:"id"`
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			Prompt     int `json:"prompt_tokens"`
			Completion int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return reply{}, unsupported("response is not JSON")
	}
	if len(res.Choices) == 0 {
		return reply{}, unsupported("no choices")
	}
	text, err := contentText(res.Choices[0].Message.Content)
	if err != nil {
		return reply{}, err
	}
	return reply{ID: res.ID, Text: text, Stop: res.Choices[0].FinishReason, InTok: res.Usage.Prompt, OutTok: res.Usage.Completion}, nil
}

func parseAnthropicReply(body []byte) (reply, error) {
	var res struct {
		ID      string `json:"id"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return reply{}, unsupported("response is not JSON")
	}
	var b strings.Builder
	saw := false
	for _, part := range res.Content {
		if part.Type != "text" {
			return reply{}, unsupported("response part " + part.Type)
		}
		b.WriteString(part.Text)
		saw = true
	}
	if !saw {
		return reply{}, unsupported("no text content")
	}
	return reply{ID: res.ID, Text: b.String(), Stop: res.StopReason, InTok: res.Usage.Input, OutTok: res.Usage.Output}, nil
}

func parseResponsesReply(body []byte) (reply, error) {
	var res struct {
		ID         string `json:"id"`
		OutputText string `json:"output_text"`
		Status     string `json:"status"`
		Output     []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return reply{}, unsupported("response is not JSON")
	}
	if res.OutputText != "" {
		return reply{ID: res.ID, Text: res.OutputText, Stop: res.Status, InTok: res.Usage.Input, OutTok: res.Usage.Output}, nil
	}
	var b strings.Builder
	saw := false
	for _, item := range res.Output {
		if item.Type != "" && item.Type != "message" {
			return reply{}, unsupported("output item " + item.Type)
		}
		for _, part := range item.Content {
			if part.Type != "output_text" && part.Type != "text" {
				return reply{}, unsupported("output part " + part.Type)
			}
			b.WriteString(part.Text)
			saw = true
		}
	}
	if !saw {
		return reply{}, unsupported("no output text")
	}
	return reply{ID: res.ID, Text: b.String(), Stop: res.Status, InTok: res.Usage.Input, OutTok: res.Usage.Output}, nil
}

func emitReply(rep reply, to string) ([]byte, error) {
	if rep.ID == "" {
		rep.ID = "pear"
	}
	switch to {
	case "openai":
		out := map[string]any{
			"id":     rep.ID,
			"object": "chat.completion",
			"choices": []any{map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": rep.Text,
				},
				"finish_reason": openAIFinish(rep.Stop),
			}},
		}
		if rep.InTok != 0 || rep.OutTok != 0 {
			out["usage"] = map[string]any{"prompt_tokens": rep.InTok, "completion_tokens": rep.OutTok}
		}
		return json.Marshal(out)
	case "anthropic":
		out := map[string]any{
			"id":   rep.ID,
			"type": "message",
			"role": "assistant",
			"content": []any{map[string]any{
				"type": "text",
				"text": rep.Text,
			}},
			"stop_reason": anthropicStop(rep.Stop),
		}
		if rep.InTok != 0 || rep.OutTok != 0 {
			out["usage"] = map[string]any{"input_tokens": rep.InTok, "output_tokens": rep.OutTok}
		}
		return json.Marshal(out)
	case "responses":
		out := map[string]any{
			"id":          rep.ID,
			"object":      "response",
			"status":      "completed",
			"output_text": rep.Text,
			"output": []any{map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []any{map[string]any{
					"type": "output_text",
					"text": rep.Text,
				}},
			}},
		}
		if rep.InTok != 0 || rep.OutTok != 0 {
			out["usage"] = map[string]any{"input_tokens": rep.InTok, "output_tokens": rep.OutTok}
		}
		return json.Marshal(out)
	default:
		return nil, unsupported("unknown client protocol " + to)
	}
}

func openAIFinish(stop string) string {
	switch stop {
	case "", "stop", "end_turn", "completed":
		return "stop"
	case "length", "max_tokens":
		return "length"
	default:
		return stop
	}
}

func anthropicStop(stop string) string {
	switch stop {
	case "", "stop", "end_turn", "completed":
		return "end_turn"
	case "length", "max_tokens":
		return "max_tokens"
	default:
		return stop
	}
}
