package gateway

import (
	"context"
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

func callCandidate(t *testing.T, s *Server, cand router.TargetCandidate, body string) (error, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	rec := httptest.NewRecorder()
	feat := router.InspectRequest([]byte(body), cand.Model, "")
	return s.tryCandidate(context.Background(), rec, req, []byte(body), feat, cand), rec
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
	if err, rec := callCandidate(t, s, cand, `{"model":"m","stream":true}`); err != nil {
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
	err, _ := callCandidate(t, s, cand, `{"model":"m","stream":true}`)
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
	err, rec := callCandidate(t, s, cand, `{"model":"m","stream":true}`)
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
	if err, _ := callCandidate(t, s, cand, `{"model":"m","stream":true}`); err == nil {
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
	if err, _ := callCandidate(t, s, cand, `{"model":"m","stream":true}`); err == nil {
		t.Fatal("404 must fail the candidate, got nil")
	}
}
