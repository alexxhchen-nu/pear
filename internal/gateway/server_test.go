package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
