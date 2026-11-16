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
	Listen          string `json:"listen"`
	DefaultTarget   string `json:"default_target"`
	CompactTarget   string `json:"compact_target"`
	SubagentTarget  string `json:"subagent_target"`
	TTFTTimeoutMS   int    `json:"ttft_timeout_ms"`
	StickyTTLMin    int    `json:"sticky_ttl_min"`
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
	Name            string   `json:"name,omitempty"`
	WhenCompact     bool     `json:"when_compact,omitempty"`
	WhenSubagent    bool     `json:"when_subagent,omitempty"`
	WhenHasImage    bool     `json:"when_has_image,omitempty"`
	MinTokens       int      `json:"min_tokens,omitempty"`
	TimeRange       string   `json:"time_range,omitempty"` // e.g., "09:00-18:00"
	FolderContains  string   `json:"folder_contains,omitempty"`
	Use             []string `json:"use"`
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

// ImportFromMagpie imports providers, groups, and OAuth accounts from ~/.config/magpie
func ImportFromMagpie(magpieDir string) (*Config, error) {
	cfg := DefaultConfig()

	// 1. Read providers.json (Magpie stores chat/responses/anthropic/gemini/decide as STRING URLs,
	//    and baseAPI as the default protocol key — not booleans.)
	provPath := filepath.Join(magpieDir, "providers.json")
	if raw, err := os.ReadFile(provPath); err == nil {
		var mp struct {
			Providers []magpieProvider `json:"providers"`
			Groups    []struct {
				ID         string   `json:"id"`
				Name       string   `json:"name"`
				Members    []string `json:"members"`
				Classifier string   `json:"classifier"`
			} `json:"groups"`
		}
		if err := json.Unmarshal(raw, &mp); err == nil {
			for _, p := range mp.Providers {
				cfg.Providers = append(cfg.Providers, p.toPear())
			}
			for _, g := range mp.Groups {
				gc := GroupConfig{
					ID:         g.ID,
					Name:       g.Name,
					Strategy:   "smart_pace",
					Members:    g.Members,
					Classifier: g.Classifier,
				}
				if len(g.Members) > 0 {
					gc.Rules = []RouteRule{{
						Name:        "auto-compact-cheap",
						WhenCompact: true,
						Use:         g.Members[len(g.Members)-1:],
					}}
				}
				cfg.Groups = append(cfg.Groups, gc)
			}
		}
	}

	// 2. Read logins.json — top-level ARRAY. Two shapes:
	//    (a) native OAuth (codex): auth.tokens.{access_token,refresh_token,id_token,account_id}
	//    (b) plugin-managed (plugin:*): auth == null, creds live in the plugin's own store (home)
	loginsPath := filepath.Join(magpieDir, "logins.json")
	if raw, err := os.ReadFile(loginsPath); err == nil {
		var logins []magpieLogin
		if err := json.Unmarshal(raw, &logins); err == nil {
			for _, l := range logins {
				cfg.Accounts = append(cfg.Accounts, l.toPear())
			}
		}
	}

	return cfg, nil
}

// magpieProvider mirrors one entry in Magpie's providers.json. URL fields are strings;
// baseAPI names the default protocol. OAuth providers carry no URL fields.
type magpieProvider struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Key         string          `json:"key"`
	BaseAPI     string          `json:"baseAPI"`
	Chat        string          `json:"chat"`
	Responses   string          `json:"responses"`
	Anthropic   string          `json:"anthropic"`
	Gemini      string          `json:"gemini"`
	Decide      string          `json:"decide"`
	Preset      string          `json:"preset"`
	InsecureTLS bool            `json:"insecureTLS"`
	Off         bool            `json:"off"`
	Models      json.RawMessage `json:"models"`
}

// oauthProviderIDs maps Magpie subscription-provider IDs to a Pear oauth-* protocol + base URL.
var oauthProviderIDs = map[string]struct{ proto, base string }{
	"codex":          {"oauth-codex", "https://chatgpt.com/backend-api/codex"},
	"github-copilot": {"oauth-copilot", "https://api.githubcopilot.com"},
	"gcp-ai":         {"oauth-gemini", ""},
	"cursor":         {"oauth-cursor", ""},
	"zed":            {"oauth-zed", ""},
	"grok":           {"oauth-grok", ""},
}

func (p magpieProvider) toPear() ProviderConfig {
	// Decode model list (strings or {id} objects).
	var modelIDs []string
	var mList []any
	if json.Unmarshal(p.Models, &mList) == nil {
		for _, item := range mList {
			switch v := item.(type) {
			case string:
				modelIDs = append(modelIDs, v)
			case map[string]any:
				if id, ok := v["id"].(string); ok {
					modelIDs = append(modelIDs, id)
				}
			}
		}
	}

	// OAuth subscription provider (no URL fields).
	if o, ok := oauthProviderIDs[p.ID]; ok {
		return ProviderConfig{
			ID: p.ID, Name: p.Name, Protocol: o.proto, BaseURL: o.base,
			APIKey: "", InsecureTLS: p.InsecureTLS, Models: modelIDs, Off: p.Off,
		}
	}

	// Standard keyed provider: pick protocol + base URL from baseAPI preference.
	proto, base := resolveProtoAndBase(p)
	return ProviderConfig{
		ID: p.ID, Name: p.Name, Protocol: proto, BaseURL: base,
		APIKey: p.Key, InsecureTLS: p.InsecureTLS, Models: modelIDs, Off: p.Off,
	}
}

// resolveProtoAndBase maps Magpie's per-protocol URL fields + baseAPI to a single (protocol, baseURL).
func resolveProtoAndBase(p magpieProvider) (string, string) {
	type pb struct{ proto, url string }
	byKey := map[string]pb{
		"chat":      {"openai", p.Chat},
		"responses": {"responses", p.Responses},
		"anthropic": {"anthropic", p.Anthropic},
		"gemini":    {"gemini", p.Gemini},
		"decide":    {"decide", p.Decide},
	}
	// 1. honor explicit baseAPI if its URL is present
	if sel, ok := byKey[p.BaseAPI]; ok && sel.url != "" {
		return sel.proto, strings.TrimRight(sel.url, "/")
	}
	// 2. otherwise first non-empty in priority order
	for _, k := range []string{"anthropic", "chat", "responses", "gemini", "decide"} {
		if sel := byKey[k]; sel.url != "" {
			return sel.proto, strings.TrimRight(sel.url, "/")
		}
	}
	// 3. local proxy-managed or URL-less entry
	return "openai", ""
}

// magpieLogin mirrors one entry in Magpie's logins.json (top-level array).
type magpieLogin struct {
	ID     string `json:"id"`
	Agent  string `json:"agent"`
	User   string `json:"user"`
	Plan   string `json:"plan"`
	On     *bool  `json:"on"`
	Lapsed string `json:"lapsed"` // non-empty = an expiry notice string, not a bool
	Home   string `json:"home"`
	Auth   *struct {
		AuthMode string `json:"auth_mode"`
		Tokens   *struct {
			IDToken      string `json:"id_token"`
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			AccountID    string `json:"account_id"`
		} `json:"tokens"`
		LastRefresh string `json:"last_refresh"`
	} `json:"auth"`
}

func (l magpieLogin) toPear() AccountConfig {
	enabled := l.Lapsed == ""
	if l.On != nil {
		enabled = *l.On && l.Lapsed == ""
	}
	acc := AccountConfig{
		ID: l.ID, Agent: l.Agent, User: l.User, Plan: l.Plan,
		Enabled: enabled, Extra: make(map[string]string),
	}
	if l.Home != "" {
		acc.Extra["home"] = l.Home
	}
	if l.Auth != nil {
		acc.Extra["auth_mode"] = l.Auth.AuthMode
		if l.Auth.LastRefresh != "" {
			acc.Extra["last_refresh"] = l.Auth.LastRefresh
		}
		if t := l.Auth.Tokens; t != nil {
			acc.AccessToken = t.AccessToken
			acc.RefreshToken = t.RefreshToken
			if t.IDToken != "" {
				acc.Extra["id_token"] = t.IDToken
			}
			if t.AccountID != "" {
				acc.Extra["account_id"] = t.AccountID
			}
		}
	}
	return acc
}
