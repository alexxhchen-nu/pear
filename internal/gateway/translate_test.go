package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alexxhchen-nu/pear/internal/config"
	"github.com/alexxhchen-nu/pear/internal/oauth"
	"github.com/alexxhchen-nu/pear/internal/router"
)

func TestCrossProtocolTranslatesPlainText(t *testing.T) {
	var gotPath, gotModel, gotSystem string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Model  string `json:"model"`
			System string `json:"system"`
		}
		_ = json.Unmarshal(body, &payload)
		gotModel = payload.Model
		gotSystem = payload.System
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg_9","type":"message","role":"assistant","content":[{"type":"text","text":"pong"}],"stop_reason":"end_turn"}`))
	}))
	defer up.Close()

	cfg := &config.Config{
		Server: config.ServerConfig{Listen: "127.0.0.1:0", DefaultTarget: "anth/claude"},
		Providers: []config.ProviderConfig{{
			ID: "anth", Protocol: "anthropic", BaseURL: up.URL, Models: []string{"claude"},
		}},
	}
	pool := oauth.NewPool(nil)
	s := NewServer(cfg, pool, router.NewEngine(cfg, pool))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
		`{"model":"anth/claude","messages":[{"role":"system","content":"brief"},{"role":"user","content":"ping"}]}`,
	))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/messages" {
		t.Fatalf("upstream path = %s", gotPath)
	}
	if gotModel != "claude" || gotSystem != "brief" {
		t.Fatalf("upstream model=%s system=%s", gotModel, gotSystem)
	}
	var client struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &client); err != nil {
		t.Fatalf("client body %s: %v", rec.Body.String(), err)
	}
	if len(client.Choices) != 1 || client.Choices[0].Message.Content != "pong" {
		t.Fatalf("client = %+v", client)
	}
	if rec.Header().Get("X-Pear-Routed-Provider") != "anth" {
		t.Fatalf("routed header %q", rec.Header().Get("X-Pear-Routed-Provider"))
	}
}

func TestCrossProtocolStreamIsRejectedBeforeUpstream(t *testing.T) {
	called := false
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()
	cfg := &config.Config{
		Server: config.ServerConfig{Listen: "127.0.0.1:0", DefaultTarget: "anth/claude"},
		Providers: []config.ProviderConfig{{
			ID: "anth", Protocol: "anthropic", BaseURL: up.URL, Models: []string{"claude"},
		}},
	}
	s := NewServer(cfg, oauth.NewPool(nil), router.NewEngine(cfg, oauth.NewPool(nil)))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
		`{"model":"anth/claude","stream":true,"messages":[{"role":"user","content":"ping"}]}`,
	))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if called {
		t.Fatal("streaming cross-protocol request reached the upstream")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "translation_unsupported") {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestSameProtocolKeepsClientPath(t *testing.T) {
	var gotPath string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"x","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer up.Close()
	cfg := &config.Config{
		Server: config.ServerConfig{Listen: "127.0.0.1:0", DefaultTarget: "oa/gpt"},
		Providers: []config.ProviderConfig{{
			ID: "oa", Protocol: "openai", BaseURL: up.URL, Models: []string{"gpt"},
		}},
	}
	s := NewServer(cfg, oauth.NewPool(nil), router.NewEngine(cfg, oauth.NewPool(nil)))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
		`{"model":"oa/gpt","messages":[{"role":"user","content":"ping"}]}`,
	))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("same-protocol path = %s", gotPath)
	}
	if !strings.Contains(rec.Body.String(), `"content":"ok"`) {
		t.Fatalf("body changed: %s", rec.Body.String())
	}
}
