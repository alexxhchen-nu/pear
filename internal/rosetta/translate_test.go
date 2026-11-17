package rosetta

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestTranslateOpenAIChatToAnthropicMessages(t *testing.T) {
	in := []byte(`{
		"model":"gpt-4o-mini",
		"temperature":0.2,
		"max_tokens":32,
		"messages":[
			{"role":"system","content":"be brief"},
			{"role":"user","content":"ping"},
			{"role":"assistant","content":"pong"},
			{"role":"user","content":[{"type":"text","text":"again"}]}
		]
	}`)
	out, err := TranslateRequest(in, "claude-x", "openai", "anthropic")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Model       string  `json:"model"`
		System      string  `json:"system"`
		MaxTokens   int     `json:"max_tokens"`
		Temperature float64 `json:"temperature"`
		Messages    []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Model != "claude-x" || got.System != "be brief" || got.MaxTokens != 32 || got.Temperature != 0.2 {
		t.Fatalf("header = %+v", got)
	}
	if len(got.Messages) != 3 || got.Messages[0].Content != "ping" || got.Messages[2].Content != "again" {
		t.Fatalf("messages = %+v", got.Messages)
	}
	if strings.Contains(string(out), `"role":"system"`) {
		t.Fatalf("system role leaked into anthropic messages: %s", out)
	}
}

func TestTranslateAnthropicTextResponseToOpenAI(t *testing.T) {
	in := []byte(`{
		"id":"msg_1",
		"type":"message",
		"role":"assistant",
		"content":[{"type":"text","text":"hello"}],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":3,"output_tokens":1}
	}`)
	out, err := TranslateResponse(in, "anthropic", "openai")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			Prompt     int `json:"prompt_tokens"`
			Completion int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v body %s", err, out)
	}
	if got.ID != "msg_1" || got.Object != "chat.completion" || len(got.Choices) != 1 {
		t.Fatalf("envelope = %+v", got)
	}
	if got.Choices[0].Message.Content != "hello" || got.Choices[0].FinishReason != "stop" {
		t.Fatalf("choice = %+v", got.Choices[0])
	}
	if got.Usage.Prompt != 3 || got.Usage.Completion != 1 {
		t.Fatalf("usage = %+v", got.Usage)
	}
}

func TestTranslateResponsesInputToOpenAIMessages(t *testing.T) {
	in := []byte(`{"model":"r","instructions":"sys","input":"ping"}`)
	out, err := TranslateRequest(in, "gpt", "responses", "openai")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Model != "gpt" || len(got.Messages) != 2 || got.Messages[0].Role != "system" || got.Messages[1].Content != "ping" {
		t.Fatalf("messages = %+v", got)
	}
}

func TestTranslateRejectsStreamingToolsAndImages(t *testing.T) {
	cases := [][]byte{
		[]byte(`{"model":"m","stream":true,"messages":[{"role":"user","content":"hi"}]}`),
		[]byte(`{"model":"m","tools":[{"type":"function"}],"messages":[{"role":"user","content":"hi"}]}`),
		[]byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"http://x"}}]}]}`),
	}
	for _, in := range cases {
		_, err := TranslateRequest(in, "m", "openai", "anthropic")
		if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("body %s error = %v", in, err)
		}
	}
}

func TestAnthropicDefaultsMaxTokensWhenMissing(t *testing.T) {
	in := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	out, err := TranslateRequest(in, "claude", "openai", "anthropic")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		MaxTokens int `json:"max_tokens"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.MaxTokens != 4096 {
		t.Fatalf("max_tokens = %d", got.MaxTokens)
	}
}

func TestWireProtocolAndPath(t *testing.T) {
	if WireProtocol("oauth-claude") != "anthropic" || WireProtocol("oauth-codex") != "responses" || WireProtocol("oauth-copilot") != "openai" {
		t.Fatalf("wire map broken: %s %s %s", WireProtocol("oauth-claude"), WireProtocol("oauth-codex"), WireProtocol("oauth-copilot"))
	}
	if ProtocolFromPath("/v1/messages") != "anthropic" || ProtocolFromPath("/v1/chat/completions") != "openai" || ProtocolFromPath("/v1/responses") != "responses" {
		t.Fatal("path map broken")
	}
	if PathFor("anthropic") != "/v1/messages" || PathFor("responses") != "/v1/responses" || PathFor("openai") != "/v1/chat/completions" {
		t.Fatal("upstream path map broken")
	}
}
