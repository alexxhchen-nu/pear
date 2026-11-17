package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Config represents the declarative Pear Gateway configuration.
type Config struct {
	Server    ServerConfig     `json:"server"`
	Providers []ProviderConfig `json:"providers"`
	Accounts  []AccountConfig  `json:"accounts"`
	Groups    []GroupConfig    `json:"groups"`
}

type ServerConfig struct {
	Listen         string `json:"listen"`
	DefaultTarget  string `json:"default_target"`
	CompactTarget  string `json:"compact_target"`
	SubagentTarget string `json:"subagent_target"`
	TTFTTimeoutMS  int    `json:"ttft_timeout_ms"`
	StickyTTLMin   int    `json:"sticky_ttl_min"`
}

type ProviderConfig struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Protocol    string   `json:"protocol"` // "openai" | "anthropic" | "responses" | "oauth-codex" | "oauth-copilot" | "oauth-claude"
	BaseURL     string   `json:"base_url"`
	APIKey      string   `json:"api_key"`
	InsecureTLS bool     `json:"insecure_tls,omitempty"`
	Models      []string `json:"models,omitempty"`
	Off         bool     `json:"off,omitempty"`
}

type AccountConfig struct {
	ID           string            `json:"id"`
	Agent        string            `json:"agent"` // "codex", "plugin:github-copilot", "plugin:cursor", "plugin:gcp-ai", "plugin:zed", etc.
	User         string            `json:"user"`
	Plan         string            `json:"plan,omitempty"`
	Enabled      bool              `json:"enabled"`
	AccessToken  string            `json:"access_token,omitempty"`
	RefreshToken string            `json:"refresh_token,omitempty"`
	ExpiresAt    int64             `json:"expires_at,omitempty"`
	Extra        map[string]string `json:"extra,omitempty"`
}

type GroupConfig struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Strategy   string      `json:"strategy"` // "smart_pace" | "session_sticky" | "order" | "round_robin"
	Members    []string    `json:"members"`
	Classifier string      `json:"classifier,omitempty"`
	Rules      []RouteRule `json:"rules,omitempty"`
}

type RouteRule struct {
	Name           string   `json:"name,omitempty"`
	WhenCompact    bool     `json:"when_compact,omitempty"`
	WhenSubagent   bool     `json:"when_subagent,omitempty"`
	WhenHasImage   bool     `json:"when_has_image,omitempty"`
	MinTokens      int      `json:"min_tokens,omitempty"`
	TimeRange      string   `json:"time_range,omitempty"` // e.g., "09:00-18:00"
	FolderContains string   `json:"folder_contains,omitempty"`
	Use            []string `json:"use"`
}

// DefaultConfigDir returns the directory holding pear.json.
// PEAR_CONFIG_DIR overrides it; otherwise ~/.pear is used on every platform.
func DefaultConfigDir() string {
	if dir := strings.TrimSpace(os.Getenv("PEAR_CONFIG_DIR")); dir != "" {
		if home, err := os.UserHomeDir(); err == nil && (dir == "~" || strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `~\`)) {
			dir = filepath.Join(home, strings.Trim(dir[1:], `/\`))
		}
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pear")
}

func DefaultConfigPath() string {
	return filepath.Join(DefaultConfigDir(), "pear.json")
}

func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Listen:        "127.0.0.1:3426",
			DefaultTarget: "group/smart-split",
			TTFTTimeoutMS: 12000,
			StickyTTLMin:  60,
		},
		Providers: []ProviderConfig{},
		Accounts:  []AccountConfig{},
		Groups:    []GroupConfig{},
	}
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, err
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
