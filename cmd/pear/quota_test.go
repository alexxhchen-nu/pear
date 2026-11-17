package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/alexxhchen-nu/pear/internal/config"
	"github.com/alexxhchen-nu/pear/internal/gateway"
	"github.com/alexxhchen-nu/pear/internal/oauth"
	"github.com/alexxhchen-nu/pear/internal/router"
)

func TestQuotaCommandReadsRunningGateway(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PEAR_CONFIG_DIR", dir)
	cfg := config.DefaultConfig()
	cfg.Server.Listen = "127.0.0.1:1"
	cfg.Accounts = []config.AccountConfig{{
		ID: "work", Agent: "claude", Enabled: true, AccessToken: "secret-token",
	}}
	if err := config.Save(filepath.Join(dir, "pear.json"), cfg); err != nil {
		t.Fatal(err)
	}

	pool := oauth.NewPool(cfg.Accounts)
	h := http.Header{}
	h.Set("anthropic-ratelimit-unified-5h-utilization", "0.40")
	pool.ObserveResponseHeaders("work", h, http.StatusOK)
	srv := gateway.NewServer(cfg, pool, router.NewEngine(cfg, pool))
	up := httptest.NewServer(srv.Handler())
	defer up.Close()

	if err := config.WriteRuntimeAddr(up.URL); err != nil {
		t.Fatal(err)
	}

	snaps, err := liveQuotas("")
	if err != nil {
		t.Fatalf("pear quota: %v", err)
	}
	if len(snaps) != 1 || len(snaps[0].Windows) != 1 {
		t.Fatalf("snapshots = %+v", snaps)
	}
	if snaps[0].Windows[0].RemainPct != 60 {
		t.Fatalf("remain = %v, want 60 from the running gateway", snaps[0].Windows[0].RemainPct)
	}
	if snaps[0].Account.AccessToken == "secret-token" {
		t.Fatal("quota output included the access token")
	}
}
