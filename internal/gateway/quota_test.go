package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexxhchen-nu/pear/internal/config"
	"github.com/alexxhchen-nu/pear/internal/oauth"
	"github.com/alexxhchen-nu/pear/internal/router"
)

func TestQuotaBaseURLPrefersOverrideThenRuntimeThenListen(t *testing.T) {
	if got := QuotaBaseURL("127.0.0.1:3426", "127.0.0.1:3491", "http://127.0.0.1:9"); got != "http://127.0.0.1:9" {
		t.Fatalf("override = %s", got)
	}
	if got := QuotaBaseURL("127.0.0.1:3426", "127.0.0.1:3491", ""); got != "http://127.0.0.1:3491" {
		t.Fatalf("runtime = %s", got)
	}
	if got := QuotaBaseURL(":3426", "", ""); got != "http://127.0.0.1:3426" {
		t.Fatalf("listen = %s", got)
	}
}

func TestFetchQuotasReadsLiveWindowsFromRunningGateway(t *testing.T) {
	acct := config.AccountConfig{ID: "work", Agent: "claude", User: "a@b.c", Enabled: true, AccessToken: "secret-token", RefreshToken: "secret-refresh"}
	cfg := &config.Config{
		Server:   config.ServerConfig{Listen: "127.0.0.1:0"},
		Accounts: []config.AccountConfig{acct},
	}
	pool := oauth.NewPool(cfg.Accounts)
	h := http.Header{}
	h.Set("anthropic-ratelimit-unified-5h-utilization", "0.25")
	pool.ObserveResponseHeaders("work", h, http.StatusOK)

	s := NewServer(cfg, pool, router.NewEngine(cfg, pool))
	up := httptest.NewServer(s.Handler())
	defer up.Close()

	snaps, err := FetchQuotas(context.Background(), up.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 {
		t.Fatalf("snapshots = %d", len(snaps))
	}
	if len(snaps[0].Windows) != 1 || snaps[0].Windows[0].Name != "5 hours" {
		t.Fatalf("windows = %+v", snaps[0].Windows)
	}
	if snaps[0].Windows[0].RemainPct != 75 {
		t.Fatalf("remain = %v, want 75", snaps[0].Windows[0].RemainPct)
	}
	// A pool built from the file has never seen a response, so it cannot
	// be the source of this window.
	cold := oauth.NewPool(cfg.Accounts).Snapshot()
	if len(cold) != 1 || len(cold[0].Windows) != 0 {
		t.Fatalf("cold pool should have no windows, got %+v", cold)
	}
	raw, _ := json.Marshal(snaps[0].Account)
	if string(raw) == "" {
		t.Fatal("empty account")
	}
	if snaps[0].Account.AccessToken == "secret-token" || snaps[0].Account.RefreshToken == "secret-refresh" {
		t.Fatalf("live snapshot leaked tokens: %+v", snaps[0].Account)
	}
}

func TestFetchQuotasReportsGatewayDown(t *testing.T) {
	_, err := FetchQuotas(context.Background(), "http://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error talking to a closed port")
	}
}
