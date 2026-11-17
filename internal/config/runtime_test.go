package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateAccountTokensWritesBackOneAccount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pear.json")
	cfg := DefaultConfig()
	cfg.Accounts = []AccountConfig{
		{ID: "keep", Agent: "codex", User: "a", Enabled: true, AccessToken: "a1", RefreshToken: "r1", ExpiresAt: 10},
		{ID: "work", Agent: "claude", User: "b", Enabled: true, AccessToken: "old", RefreshToken: "old-r", ExpiresAt: 20, Plan: "max"},
	}
	cfg.Providers = []ProviderConfig{{ID: "p", Protocol: "openai", BaseURL: "http://up", APIKey: "sk"}}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	if err := UpdateAccountTokens(path, "work", "new-access", "new-refresh", 99); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Accounts[0].AccessToken != "a1" || got.Accounts[0].RefreshToken != "r1" {
		t.Fatalf("other account changed: %+v", got.Accounts[0])
	}
	work := got.Accounts[1]
	if work.AccessToken != "new-access" || work.RefreshToken != "new-refresh" || work.ExpiresAt != 99 {
		t.Fatalf("updated account = %+v", work)
	}
	if work.Plan != "max" || work.User != "b" || !work.Enabled {
		t.Fatalf("unrelated fields dropped: %+v", work)
	}
	if len(got.Providers) != 1 || got.Providers[0].APIKey != "sk" {
		t.Fatalf("providers changed: %+v", got.Providers)
	}
}

func TestUpdateAccountTokensKeepsRefreshWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pear.json")
	cfg := DefaultConfig()
	cfg.Accounts = []AccountConfig{{ID: "work", AccessToken: "old", RefreshToken: "keep-r", ExpiresAt: 5, Enabled: true}}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	if err := UpdateAccountTokens(path, "work", "new", "", 8); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Accounts[0].RefreshToken != "keep-r" || got.Accounts[0].AccessToken != "new" {
		t.Fatalf("account = %+v", got.Accounts[0])
	}
}

func TestRuntimeAddrRoundTrip(t *testing.T) {
	t.Setenv("PEAR_CONFIG_DIR", t.TempDir())
	if _, ok := ReadRuntimeAddr(); ok {
		t.Fatal("missing addr file should not count as an address")
	}
	if err := WriteRuntimeAddr("127.0.0.1:3491"); err != nil {
		t.Fatal(err)
	}
	got, ok := ReadRuntimeAddr()
	if !ok || got != "127.0.0.1:3491" {
		t.Fatalf("addr = %q ok=%v", got, ok)
	}
	if _, err := os.Stat(RuntimeAddrPath()); err != nil {
		t.Fatal(err)
	}
}
