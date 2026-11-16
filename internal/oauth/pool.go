package oauth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alexxhchen-nu/pear/internal/config"
)

// QuotaWindow tracks a single usage window (e.g. "5h" or "7d").
type QuotaWindow struct {
	Name      string    `json:"name"`
	UsedPct   float64   `json:"used"`
	RemainPct float64   `json:"remaining"`
	ResetsAt  time.Time `json:"resets_at"`
}

// AccountState holds live token and rate-limit state for one OAuth or API account.
type AccountState struct {
	Account      config.AccountConfig `json:"account"`
	Windows      []QuotaWindow        `json:"windows"`
	CooldownTil  time.Time            `json:"cooldown_until,omitempty"`
	LastUsed     time.Time            `json:"last_used,omitempty"`
	BearerCached string               `json:"-"`
	BearerExpiry time.Time            `json:"-"`
}

// Pool manages all OAuth subscription accounts and passive quota windows.
type Pool struct {
	mu       sync.RWMutex
	accounts map[string]*AccountState
	client   *http.Client
}

func NewPool(accounts []config.AccountConfig) *Pool {
	p := &Pool{
		accounts: make(map[string]*AccountState, len(accounts)),
		client:   &http.Client{Timeout: 15 * time.Second},
	}
	for _, a := range accounts {
		ac := a
		p.accounts[a.ID] = &AccountState{
			Account: ac,
			Windows: []QuotaWindow{},
		}
	}
	return p
}

// matchAccount finds the account whose agent matches a quota provider key,
// tolerating the "plugin:" prefix (e.g. provider "gcp-ai" <-> agent "plugin:gcp-ai").
func (p *Pool) matchAccount(provKey string) *AccountState {
	if provKey == "" {
		return nil
	}
	for _, st := range p.accounts {
		agent := strings.TrimPrefix(st.Account.Agent, "plugin:")
		if agent == provKey || st.Account.Agent == provKey {
			return st
		}
	}
	return nil
}

// SelectBestAccount picks the healthiest OAuth account for a given agent type using Smart Pace.
func (p *Pool) SelectBestAccount(agentPrefix string) (*AccountState, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	now := time.Now()
	var best *AccountState
	bestScore := -1e9

	for _, st := range p.accounts {
		if !st.Account.Enabled {
			continue
		}
		if !strings.Contains(st.Account.Agent, agentPrefix) {
			continue
		}
		if now.Before(st.CooldownTil) {
			continue
		}
		score := computePaceScore(st, now)
		if score > bestScore {
			bestScore = score
			best = st
		}
	}
	if best == nil {
		return nil, fmt.Errorf("no available OAuth account for %s", agentPrefix)
	}
	return best, nil
}

// computePaceScore prioritizes accounts with ample remaining quota whose reset window is soonest.
func computePaceScore(st *AccountState, now time.Time) float64 {
	if len(st.Windows) == 0 {
		return 50.0
	}
	minRemain := 100.0
	soonestResetHours := 168.0
	for _, w := range st.Windows {
		if w.RemainPct < minRemain {
			minRemain = w.RemainPct
		}
		if !w.ResetsAt.IsZero() && w.ResetsAt.After(now) {
			hrs := w.ResetsAt.Sub(now).Hours()
			if hrs < soonestResetHours {
				soonestResetHours = hrs
			}
		}
	}
	if minRemain <= 1.0 {
		return -1000.0
	}
	// Higher remaining % + sooner reset window = higher priority ("burn before it resets")
	urgencyBonus := 24.0 / (soonestResetHours + 1.0)
	return minRemain + urgencyBonus*10.0
}

// ResolveBearerToken returns a valid Authorization Bearer token, exchanging or refreshing if needed.
func (p *Pool) ResolveBearerToken(st *AccountState) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	if st.BearerCached != "" && now.Add(2*time.Minute).Before(st.BearerExpiry) {
		return st.BearerCached, nil
	}

	// GitHub Copilot token exchange: exchange ghu_/gho_ OAuth token for short-lived Copilot bearer
	if strings.Contains(st.Account.Agent, "github-copilot") {
		tok, exp, err := p.exchangeCopilotToken(st.Account.AccessToken)
		if err == nil && tok != "" {
			st.BearerCached = tok
			st.BearerExpiry = exp
			return tok, nil
		}
	}

	// OpenAI Codex OAuth refresh if expired
	if st.Account.Agent == "codex" && st.Account.ExpiresAt > 0 && now.Unix() > st.Account.ExpiresAt-120 && st.Account.RefreshToken != "" {
		if tok, exp, err := p.refreshCodexToken(st.Account.RefreshToken); err == nil {
			st.Account.AccessToken = tok
			st.Account.ExpiresAt = exp.Unix()
			st.BearerCached = tok
			st.BearerExpiry = exp
			return tok, nil
		}
	}

	st.BearerCached = st.Account.AccessToken
	st.BearerExpiry = now.Add(30 * time.Minute)
	return st.Account.AccessToken, nil
}

func (p *Pool) exchangeCopilotToken(oauthToken string) (string, time.Time, error) {
	if oauthToken == "" {
		return "", time.Time{}, fmt.Errorf("empty copilot oauth token")
	}
	req, err := http.NewRequest("GET", "https://api.github.com/copilot_internal/v2/token", nil)
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Authorization", "token "+oauthToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GithubCopilot/1.250.0")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("copilot token exchange HTTP %d", resp.StatusCode)
	}
	var res struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", time.Time{}, err
	}
	return res.Token, time.Unix(res.ExpiresAt, 0), nil
}

func (p *Pool) refreshCodexToken(refreshToken string) (string, time.Time, error) {
	payload := map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     "app_EMoamEEZ73f0CkXaXp7hrann",
		"refresh_token": refreshToken,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", "https://auth.openai.com/oauth/token", bytes.NewReader(body))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return "", time.Time{}, fmt.Errorf("codex oauth refresh HTTP %d", resp.StatusCode)
	}
	var res struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", time.Time{}, err
	}
	return res.AccessToken, time.Now().Add(time.Duration(res.ExpiresIn) * time.Second), nil
}

// ObserveResponseHeaders passively updates quota windows from Anthropic / OpenAI / Codex response headers.
func (p *Pool) ObserveResponseHeaders(accountID string, h http.Header, statusCode int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	st, ok := p.accounts[accountID]
	if !ok {
		return
	}
	st.LastUsed = time.Now()
	if statusCode == http.StatusTooManyRequests || statusCode == 529 {
		retrySec := 30
		if ra := h.Get("Retry-After"); ra != "" {
			if v, err := strconv.Atoi(ra); err == nil && v > 0 {
				retrySec = v
			}
		}
		st.CooldownTil = time.Now().Add(time.Duration(retrySec) * time.Second)
	}

	// Anthropic unified 5h & 7d utilization headers
	if u5h := h.Get("anthropic-ratelimit-unified-5h-utilization"); u5h != "" {
		if val, err := strconv.ParseFloat(u5h, 64); err == nil {
			pct := val * 100.0
			var resetTime time.Time
			if r := h.Get("anthropic-ratelimit-unified-5h-reset"); r != "" {
				if ts, err := strconv.ParseInt(r, 10, 64); err == nil {
					resetTime = time.Unix(ts, 0)
				}
			}
			upsertWindow(st, "5 hours", pct, 100.0-pct, resetTime)
		}
	}
	if u7d := h.Get("anthropic-ratelimit-unified-7d-utilization"); u7d != "" {
		if val, err := strconv.ParseFloat(u7d, 64); err == nil {
			pct := val * 100.0
			var resetTime time.Time
			if r := h.Get("anthropic-ratelimit-unified-7d-reset"); r != "" {
				if ts, err := strconv.ParseInt(r, 10, 64); err == nil {
					resetTime = time.Unix(ts, 0)
				}
			}
			upsertWindow(st, "Weekly", pct, 100.0-pct, resetTime)
		}
	}
}

func upsertWindow(st *AccountState, name string, used, remain float64, resetsAt time.Time) {
	for i := range st.Windows {
		if st.Windows[i].Name == name {
			st.Windows[i].UsedPct = used
			st.Windows[i].RemainPct = remain
			if !resetsAt.IsZero() {
				st.Windows[i].ResetsAt = resetsAt
			}
			return
		}
	}
	st.Windows = append(st.Windows, QuotaWindow{
		Name:      name,
		UsedPct:   used,
		RemainPct: remain,
		ResetsAt:  resetsAt,
	})
}

// Snapshot returns a copy of all account states for CLI/API inspection.
func (p *Pool) Snapshot() []AccountState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]AccountState, 0, len(p.accounts))
	for _, st := range p.accounts {
		cp := *st
		cp.Account.AccessToken = maskSecret(cp.Account.AccessToken)
		cp.Account.RefreshToken = maskSecret(cp.Account.RefreshToken)
		cp.Account.Extra = nil
		out = append(out, cp)
	}
	return out
}

func maskSecret(s string) string {
	if len(s) <= 8 {
		if s == "" {
			return ""
		}
		return "****"
	}
	return s[:4] + "…" + s[len(s)-4:]
}
