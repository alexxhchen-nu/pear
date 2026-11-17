package oauth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexxhchen-nu/pear/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func httpResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestRefreshDueBeforeExpiryUpdatesCodexToken(t *testing.T) {
	p := NewPool([]config.AccountConfig{{
		ID: "work", Agent: "codex", Enabled: true,
		AccessToken: "old-access", RefreshToken: "old-refresh",
		ExpiresAt: time.Now().Add(45 * time.Second).Unix(),
	}})
	var saved TokenUpdate
	p.SetTokenSaver(func(u TokenUpdate) { saved = u })
	p.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "auth.openai.com" || r.URL.Path != "/oauth/token" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), "old-refresh") || !strings.Contains(string(raw), "refresh_token") {
			t.Errorf("refresh body = %s", raw)
		}
		return httpResp(http.StatusOK, `{"access_token":"new-access","expires_in":3600,"refresh_token":"new-refresh"}`), nil
	})}

	p.RefreshDue(time.Now())

	st := p.accounts["work"]
	if st.Account.AccessToken != "new-access" || st.Account.RefreshToken != "new-refresh" {
		t.Fatalf("token = %s / %s", st.Account.AccessToken, st.Account.RefreshToken)
	}
	if st.Account.ExpiresAt <= time.Now().Add(30*time.Minute).Unix() {
		t.Fatalf("expires_at not extended: %d", st.Account.ExpiresAt)
	}
	if saved.ID != "work" || saved.AccessToken != "new-access" || saved.RefreshToken != "new-refresh" {
		t.Fatalf("saver = %+v", saved)
	}
	tok, err := p.ResolveBearerToken(st)
	if err != nil || tok != "new-access" {
		t.Fatalf("bearer = %q %v", tok, err)
	}
}

func TestRefreshDueSkipsTokenThatIsNotNearExpiry(t *testing.T) {
	p := NewPool([]config.AccountConfig{{
		ID: "work", Agent: "codex", Enabled: true,
		AccessToken: "keep", RefreshToken: "r",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}})
	p.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Errorf("refresh called for a token an hour from expiry: %s", r.URL)
		return httpResp(http.StatusOK, `{"access_token":"nope","expires_in":10}`), nil
	})}
	p.RefreshDue(time.Now())
	if p.accounts["work"].Account.AccessToken != "keep" {
		t.Fatalf("token changed: %s", p.accounts["work"].Account.AccessToken)
	}
}

func TestRefreshFailureKeepsStillValidToken(t *testing.T) {
	p := NewPool([]config.AccountConfig{{
		ID: "work", Agent: "codex", Enabled: true,
		AccessToken: "still-good", RefreshToken: "r",
		ExpiresAt: time.Now().Add(50 * time.Second).Unix(),
	}})
	p.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return httpResp(http.StatusServiceUnavailable, `{"error":"down"}`), nil
	})}
	now := time.Now()
	p.RefreshDue(now)
	if p.accounts["work"].Account.AccessToken != "still-good" {
		t.Fatalf("valid token was replaced: %s", p.accounts["work"].Account.AccessToken)
	}
	if p.RefreshError("work") == "" {
		t.Fatal("failed refresh left no error")
	}
	tok, err := p.ResolveBearerToken(p.accounts["work"])
	if err != nil || tok != "still-good" {
		t.Fatalf("still-valid token = %q %v", tok, err)
	}
}

func TestRefreshFailureOfExpiredTokenReturnsError(t *testing.T) {
	p := NewPool([]config.AccountConfig{{
		ID: "work", Agent: "codex", Enabled: true,
		AccessToken: "dead", RefreshToken: "r",
		ExpiresAt: time.Now().Add(-time.Minute).Unix(),
	}})
	p.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return httpResp(http.StatusUnauthorized, `{"error":"invalid_grant"}`), nil
	})}
	_, err := p.ResolveBearerToken(p.accounts["work"])
	if err == nil {
		t.Fatal("expired token with a failed refresh must error")
	}
	if p.accounts["work"].Account.AccessToken != "dead" {
		t.Fatalf("dead token changed: %s", p.accounts["work"].Account.AccessToken)
	}
	if !strings.Contains(err.Error(), "401") && !strings.Contains(err.Error(), "HTTP") {
		t.Fatalf("error should name the HTTP failure, got %v", err)
	}
}

func TestRefreshRetriesAfterFailure(t *testing.T) {
	p := NewPool([]config.AccountConfig{{
		ID: "work", Agent: "codex", Enabled: true,
		AccessToken: "old", RefreshToken: "r",
		ExpiresAt: time.Now().Add(20 * time.Second).Unix(),
	}})
	var hits atomic.Int32
	p.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if hits.Add(1) == 1 {
			return httpResp(http.StatusBadGateway, `nope`), nil
		}
		return httpResp(http.StatusOK, `{"access_token":"recovered","expires_in":1800}`), nil
	})}
	p.RefreshDue(time.Now())
	if p.RefreshError("work") == "" || p.accounts["work"].Account.AccessToken != "old" {
		t.Fatalf("first failure: token=%s err=%q", p.accounts["work"].Account.AccessToken, p.RefreshError("work"))
	}
	p.RefreshDue(time.Now())
	if p.accounts["work"].Account.AccessToken != "recovered" || p.RefreshError("work") != "" {
		t.Fatalf("retry: token=%s err=%q", p.accounts["work"].Account.AccessToken, p.RefreshError("work"))
	}
}

func TestClaudeRefreshFallsBackWhenPrimaryIsMissing(t *testing.T) {
	p := NewPool([]config.AccountConfig{{
		ID: "c", Agent: "claude", Enabled: true,
		AccessToken: "old", RefreshToken: "claude-refresh",
		ExpiresAt: time.Now().Add(30 * time.Second).Unix(),
	}})
	p.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), "9d1c250a-e61b-44d9-88ed-5944d1962f5e") {
			t.Errorf("client id missing: %s", raw)
		}
		switch r.URL.Host {
		case "platform.claude.com":
			return httpResp(http.StatusNotFound, `missing`), nil
		case "console.anthropic.com":
			return httpResp(http.StatusOK, `{"access_token":"claude-new","expires_in":28800,"refresh_token":"claude-r2"}`), nil
		default:
			t.Errorf("unexpected host %s", r.URL.Host)
			return httpResp(http.StatusTeapot, ``), nil
		}
	})}
	p.RefreshDue(time.Now())
	st := p.accounts["c"]
	if st.Account.AccessToken != "claude-new" || st.Account.RefreshToken != "claude-r2" {
		t.Fatalf("claude token = %s / %s", st.Account.AccessToken, st.Account.RefreshToken)
	}
}

func TestCopilotExchangeBeforeBearerExpiry(t *testing.T) {
	p := NewPool([]config.AccountConfig{{
		ID: "gh", Agent: "plugin:github-copilot", Enabled: true,
		AccessToken: "ghu_github",
	}})
	st := p.accounts["gh"]
	st.BearerCached = "old-bearer"
	st.BearerExpiry = time.Now().Add(20 * time.Second)
	saved := false
	p.SetTokenSaver(func(TokenUpdate) { saved = true })
	p.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.github.com" {
			t.Errorf("host %s", r.URL.Host)
		}
		if got := r.Header.Get("Authorization"); got != "token ghu_github" {
			t.Errorf("authorization %q", got)
		}
		exp := time.Now().Add(30 * time.Minute).Unix()
		return httpResp(http.StatusOK, `{"token":"cpt-new","expires_at":`+strconv.FormatInt(exp, 10)+`}`), nil
	})}
	p.RefreshDue(time.Now())
	if st.BearerCached != "cpt-new" {
		t.Fatalf("bearer = %s", st.BearerCached)
	}
	if st.Account.AccessToken != "ghu_github" {
		t.Fatalf("github token changed: %s", st.Account.AccessToken)
	}
	if saved {
		t.Fatal("copilot bearer exchange must not rewrite pear.json")
	}
	tok, err := p.ResolveBearerToken(st)
	if err != nil || tok != "cpt-new" {
		t.Fatalf("resolved = %q %v", tok, err)
	}
}

func TestCopilotExchangeFailureKeepsUnexpiredBearer(t *testing.T) {
	p := NewPool([]config.AccountConfig{{
		ID: "gh", Agent: "plugin:github-copilot", Enabled: true, AccessToken: "ghu",
	}})
	st := p.accounts["gh"]
	st.BearerCached = "still-bearer"
	st.BearerExpiry = time.Now().Add(40 * time.Second)
	p.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return httpResp(http.StatusBadGateway, ``), nil
	})}
	tok, err := p.ResolveBearerToken(st)
	if err != nil || tok != "still-bearer" {
		t.Fatalf("bearer = %q %v", tok, err)
	}
	st.BearerExpiry = time.Now().Add(-time.Second)
	if _, err := p.ResolveBearerToken(st); err == nil {
		t.Fatal("expired copilot bearer with a failed exchange must error")
	}
}

func TestClaudeRefreshFallsBackWhenPrimaryUnreachable(t *testing.T) {
	p := NewPool([]config.AccountConfig{{
		ID: "c", Agent: "claude", Enabled: true,
		AccessToken: "old", RefreshToken: "r",
		ExpiresAt: time.Now().Add(20 * time.Second).Unix(),
	}})
	p.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "platform.claude.com" {
			return nil, fmt.Errorf("dial tcp: connection refused")
		}
		return httpResp(http.StatusOK, `{"access_token":"from-legacy","expires_in":3600}`), nil
	})}
	p.RefreshDue(time.Now())
	if p.accounts["c"].Account.AccessToken != "from-legacy" {
		t.Fatalf("token = %s", p.accounts["c"].Account.AccessToken)
	}
}

func TestExpiresAtMillisecondsStillRefreshes(t *testing.T) {
	p := NewPool([]config.AccountConfig{{
		ID: "c", Agent: "claude", Enabled: true,
		AccessToken: "old", RefreshToken: "r",
		ExpiresAt: time.Now().Add(25 * time.Second).UnixMilli(),
	}})
	p.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "platform.claude.com" {
			return httpResp(http.StatusOK, `{"access_token":"from-ms","expires_in":3600}`), nil
		}
		return httpResp(http.StatusNotFound, ``), nil
	})}
	p.RefreshDue(time.Now())
	if p.accounts["c"].Account.AccessToken != "from-ms" {
		t.Fatalf("millisecond expiry was not treated as due, token=%s", p.accounts["c"].Account.AccessToken)
	}
	if p.accounts["c"].Account.ExpiresAt > 1_000_000_000_000 {
		t.Fatalf("stored expiry should be unix seconds, got %d", p.accounts["c"].Account.ExpiresAt)
	}
}

func TestRefresherRunsWithoutARequest(t *testing.T) {
	p := NewPool([]config.AccountConfig{{
		ID: "work", Agent: "codex", Enabled: true,
		AccessToken: "old", RefreshToken: "r",
		ExpiresAt: time.Now().Add(15 * time.Second).Unix(),
	}})
	var hits atomic.Int32
	p.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		hits.Add(1)
		return httpResp(http.StatusOK, `{"access_token":"bg","expires_in":3600}`), nil
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.StartRefresher(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.RLock()
		tok := p.accounts["work"].Account.AccessToken
		p.mu.RUnlock()
		if tok == "bg" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	time.Sleep(20 * time.Millisecond)
	p.mu.RLock()
	got := p.accounts["work"].Account.AccessToken
	p.mu.RUnlock()
	if got != "bg" {
		t.Fatalf("background refresher did not update token (hits=%d)", hits.Load())
	}
}
