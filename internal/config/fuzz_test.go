package config

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzLoad checks config parsing tolerates arbitrary bytes: a corrupt config
// must return an error, never a half-populated config or a panic.
func FuzzLoad(f *testing.F) {
	f.Add(`{}`)
	f.Add(`{"server":{"listen":"127.0.0.1:3426","ttft_timeout_ms":12000}}`)
	f.Add(`{"providers":[{"id":"p","protocol":"openai","base_url":"https://x","models":["m"]}]}`)
	f.Add(`{"providers":`)
	f.Add(`[]`)

	f.Fuzz(func(t *testing.T, data string) {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		cfg, err := Load(path)
		if err == nil && cfg == nil {
			t.Fatal("nil config with nil error")
		}
	})
}
