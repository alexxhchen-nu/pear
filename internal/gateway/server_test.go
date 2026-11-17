package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alexxhchen-nu/pear/internal/config"
	"github.com/alexxhchen-nu/pear/internal/oauth"
	"github.com/alexxhchen-nu/pear/internal/router"
)

// fixture builds a Server whose only provider points at the given upstream.
func fixture(t *testing.T, upstream string) (*Server, router.TargetCandidate) {
	t.Helper()
	cfg := &config.Config{
		Server: config.ServerConfig{Listen: "127.0.0.1:0"},
		Providers: []config.ProviderConfig{{
			ID: "fake", Protocol: "openai", BaseURL: upstream, Models: []string{"m"},
		}},
	}
	pool := oauth.NewPool(nil)
	s := NewServer(cfg, pool, router.NewEngine(cfg, pool))
	return s, router.TargetCandidate{Provider: cfg.Providers[0], Model: "m"}
}

func callCandidate(t *testing.T, s *Server, cand router.TargetCandidate, body string) (bool, error, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	rec := httptest.NewRecorder()
	feat := router.InspectRequest([]byte(body), cand.Model, "")
	committed, err := s.tryCandidate(context.Background(), rec, req, []byte(body), feat, cand)
	return committed, err, rec
}

func TestTryCandidateHealthyUpstream(t *testing.T) {
	var up *httptest.Server
	up = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: hello\n\n"))
	}))
	defer up.Close()

	s, cand := fixture(t, up.URL)
	if _, err, rec := callCandidate(t, s, cand, `{"model":"m","stream":true}`); err != nil {
		t.Fatalf("healthy stream must succeed, got %v", err)
	} else if !strings.Contains(rec.Body.String(), "data: hello") {
		t.Fatalf("body not forwarded, got %q", rec.Body.String())
	}
}

// TestTryCandidateTruncatedStreamMustFail covers the defect where a read error
// mid-stream was reported as success, so a cut-off SSE response looked healthy
// and the candidate was never recorded as failed.
func TestTryCandidateTruncatedStreamMustFail(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Promise 4096 bytes, then hang up after 8. The client sees
		// io.ErrUnexpectedEOF, not io.EOF.
		w.Header().Set("Content-Length", "4096")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: hi"))
	}))
	defer up.Close()

	s, cand := fixture(t, up.URL)
	_, err, _ := callCandidate(t, s, cand, `{"model":"m","stream":true}`)
	if err == nil {
		t.Fatal("truncated upstream stream must return an error, got nil")
	}
	if !strings.Contains(err.Error(), "unexpected EOF") {
		t.Fatalf("error must name the stream fault, got %v", err)
	}
}

// fixtureAccount is fixture plus one OAuth account wired into the pool, so
// cooldown and token-refresh paths can be observed.
func fixtureAccount(t *testing.T, upstream string) (*Server, router.TargetCandidate, *oauth.Pool) {
	t.Helper()
	acct := config.AccountConfig{ID: "a1", Agent: "claude-code", AccessToken: "tok-1", Enabled: true}
	cfg := &config.Config{
		Server:    config.ServerConfig{Listen: "127.0.0.1:0"},
		Providers: []config.ProviderConfig{{ID: "fake", Protocol: "openai", BaseURL: upstream, Models: []string{"m"}}},
		Accounts:  []config.AccountConfig{acct},
	}
	pool := oauth.NewPool(cfg.Accounts)
	s := NewServer(cfg, pool, router.NewEngine(cfg, pool))
	st, err := pool.SelectBestAccount("claude-code")
	if err != nil {
		t.Fatalf("account not selectable: %v", err)
	}
	return s, router.TargetCandidate{Provider: cfg.Providers[0], Model: "m", Account: st}, pool
}

// TestTryCandidate401RefreshRetriesOnce covers the defect where a 401 was
// passed straight through to the caller instead of refreshing the token and
// retrying the same candidate once.
func TestTryCandidate401RetriesOnceAfterRefresh(t *testing.T) {
	hits := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: recovered\n\n"))
	}))
	defer up.Close()

	s, cand, _ := fixtureAccount(t, up.URL)
	_, err, rec := callCandidate(t, s, cand, `{"model":"m","stream":true}`)
	if err != nil {
		t.Fatalf("401 must be retried once, got %v", err)
	}
	if hits != 2 {
		t.Fatalf("want exactly 2 upstream attempts, got %d", hits)
	}
	if !strings.Contains(rec.Body.String(), "data: recovered") {
		t.Fatalf("retried body not forwarded, got %q", rec.Body.String())
	}
}

// TestTryCandidate403BenchesAccount covers the defect where a 403 was passed
// through to the caller and the account kept being selected.
func TestTryCandidate403BenchesAccount(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer up.Close()

	s, cand, pool := fixtureAccount(t, up.URL)
	if _, err, _ := callCandidate(t, s, cand, `{"model":"m","stream":true}`); err == nil {
		t.Fatal("403 must fail the candidate, got nil")
	}
	snap := pool.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("want 1 account snapshot, got %d", len(snap))
	}
	if !snap[0].CooldownTil.After(time.Now().Add(time.Minute)) {
		t.Fatalf("403 must bench the account, CooldownTil=%v", snap[0].CooldownTil)
	}
	if _, err := pool.SelectBestAccount("claude-code"); err == nil {
		t.Fatal("benched account must not be selectable")
	}
}

// TestTryCandidate404FailsOver covers the defect where a bad upstream route
// was forwarded to the caller instead of trying the next candidate.
func TestTryCandidate404FailsOver(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer up.Close()

	s, cand := fixture(t, up.URL)
	if _, err, _ := callCandidate(t, s, cand, `{"model":"m","stream":true}`); err == nil {
		t.Fatal("404 must fail the candidate, got nil")
	}
}

// TestTryCandidateTTFTTimeout covers the dead ttft_timeout_ms knob: an upstream
// that accepts the request and then stalls past the budget must fail the
// candidate over instead of handing the caller an empty 200.
func TestTryCandidateTTFTTimeout(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(300 * time.Millisecond)
		w.Write([]byte("data: late\n\n"))
	}))
	defer up.Close()

	s, cand := fixture(t, up.URL)
	s.cfg.Server.TTFTTimeoutMS = 60
	_, err, rec := callCandidate(t, s, cand, `{"model":"m","stream":true}`)
	if err == nil {
		t.Fatal("stalled upstream must fail the candidate, got nil")
	}
	if !strings.Contains(err.Error(), "time to first byte") {
		t.Fatalf("error must name the TTFT fault, got %v", err)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("no body may be forwarded after a stall, got %q", rec.Body.String())
	}
}

// TestTryCandidateTTFTWithinBudget proves the budget does not cut healthy
// streams that are merely slow to start.
func TestTryCandidateTTFTWithinBudget(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		time.Sleep(40 * time.Millisecond)
		w.Write([]byte("data: ok\n\n"))
	}))
	defer up.Close()

	s, cand := fixture(t, up.URL)
	s.cfg.Server.TTFTTimeoutMS = 2000
	if _, err, rec := callCandidate(t, s, cand, `{"model":"m","stream":true}`); err != nil {
		t.Fatalf("slow-but-in-budget stream must succeed, got %v", err)
	} else if !strings.Contains(rec.Body.String(), "data: ok") {
		t.Fatalf("body not forwarded, got %q", rec.Body.String())
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

// TestProxyRejectsOversizedBody covers the defect where io.LimitReader silently
// truncated an oversized body and forwarded a malformed prompt upstream.
func TestProxyRejectsOversizedBody(t *testing.T) {
	s, _ := fixture(t, "http://127.0.0.1:1")
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", io.LimitReader(zeroReader{}, maxRequestBodyBytes+1))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d: %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Type  string `json:"type"`
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("error body must be JSON, got %q", rec.Body.String())
	}
	if env.Type != "error" || env.Error.Type != "request_too_large" {
		t.Fatalf("wrong envelope: %+v", env)
	}
}

// TestProxyExhaustedIsJSONError covers the defect where an all-candidates-fail
// run returned a plaintext 502 that no SDK could parse.
func TestProxyExhaustedIsJSONError(t *testing.T) {
	cfg := &config.Config{Server: config.ServerConfig{Listen: "127.0.0.1:0"}}
	pool := oauth.NewPool(nil)
	s := NewServer(cfg, pool, router.NewEngine(cfg, pool))
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"nope"}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", rec.Code)
	}
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("error body must be JSON, got %q", rec.Body.String())
	}
	if env["type"] != "error" {
		t.Fatalf("want type=error, got %+v", env)
	}
	if _, ok := env["error"].(map[string]any); !ok {
		t.Fatalf("want nested error object, got %+v", env)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("want application/json, got %q", ct)
	}
}

// TestTryCandidateStallAfterCommitIsReported covers the other half of the SSE
// problem: once bytes are on the wire the request cannot fail over any more, so
// a stalled body must end the request as a failure and never be recorded as a
// clean finish.
func TestTryCandidateStallAfterCommitIsReported(t *testing.T) {
	release := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: first\n\n"))
		w.(http.Flusher).Flush()
		<-release
	}))
	defer up.Close()
	defer close(release)

	oldIdle := upstreamIdleTimeout
	upstreamIdleTimeout = 120 * time.Millisecond
	defer func() { upstreamIdleTimeout = oldIdle }()

	s, cand := fixture(t, up.URL)
	committed, err, rec := callCandidate(t, s, cand, `{"model":"m","stream":true}`)
	if !committed {
		t.Fatal("a stream already writing bytes must report as committed")
	}
	if err == nil {
		t.Fatal("a stalled stream must not be recorded as success")
	}
	if !strings.Contains(err.Error(), "stalled") {
		t.Fatalf("error must name the stall, got %v", err)
	}
	if got := rec.Body.String(); got != "data: first\n\n" {
		t.Fatalf("only the delivered bytes may appear, got %q", got)
	}
}

// TestHandleProxyNeverWritesAfterCommit guards the double-write path: a stream
// fault after headers reached the caller must not append a JSON error body.
func TestHandleProxyNeverWritesAfterCommit(t *testing.T) {
	release := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: partial\n\n"))
		w.(http.Flusher).Flush()
		<-release
	}))
	defer up.Close()
	defer close(release)

	oldIdle := upstreamIdleTimeout
	upstreamIdleTimeout = 120 * time.Millisecond
	defer func() { upstreamIdleTimeout = oldIdle }()

	s, _ := fixture(t, up.URL)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"m","stream":true}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "data: partial") {
		t.Fatalf("delivered bytes missing, got %q", body)
	}
	if strings.Contains(body, `"type":"error"`) {
		t.Fatalf("no error envelope may follow a committed stream, got %q", body)
	}
}

// TestUpstreamClientHasNoTotalTimeout pins the fix for the hard 10-minute
// client Timeout that cut legitimate long generations off mid-stream.
func TestUpstreamClientHasNoTotalTimeout(t *testing.T) {
	s, _ := fixture(t, "http://127.0.0.1:1")
	if s.standardClient.Timeout != 0 {
		t.Fatalf("total client timeout must be unset, got %v", s.standardClient.Timeout)
	}
	tr, ok := s.standardClient.Transport.(*http.Transport)
	if !ok || tr.ResponseHeaderTimeout == 0 {
		t.Fatal("header wait must stay bounded")
	}
}
