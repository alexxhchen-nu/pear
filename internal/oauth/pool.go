package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

	// forceRefresh makes the next ResolveBearerToken ignore the cache. Set by
	// ForceRefresh after an upstream 401, cleared once a token is fetched.
	forceRefresh   bool
	lastRefreshErr string
}

// TokenUpdate is a rotated OAuth credential the pool wants written back.
// An empty RefreshToken means the previous refresh token is still current.
type TokenUpdate struct {
	ID           string
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64
}

// Pool manages all OAuth subscription accounts and passive quota windows.
type Pool struct {
	mu       sync.RWMutex
	accounts map[string]*AccountState
	client   *http.Client
	onSave   func(TokenUpdate)
}

// refreshLead is how early a token is renewed, both in the background and on
// the request path. Two minutes matches the previous inline Codex check.
const refreshLead = 2 * time.Minute

const refreshEvery = 30 * time.Second

const (
	codexTokenURL        = "https://auth.openai.com/oauth/token"
	codexClientID        = "app_EMoamEEZ73f0CkXaXp7hrann"
	claudeTokenURL       = "https://platform.claude.com/v1/oauth/token"
	claudeTokenURLLegacy = "https://console.anthropic.com/v1/oauth/token"
	claudeClientID       = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	copilotTokenURL      = "https://api.github.com/copilot_internal/v2/token"
)

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

// BestPace is the smart-pace score of the best account whose agent contains
// providerID. The second result is false when no enabled account matches.
// Every matching account being in cooldown still returns true, with a score
// that sorts that member last.
func (p *Pool) BestPace(providerID string, now time.Time) (float64, bool) {
	if providerID == "" {
		return 0, false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()

	best := -1e9
	selectable := false
	matched := false
	for _, st := range p.accounts {
		if !st.Account.Enabled || !strings.Contains(st.Account.Agent, providerID) {
			continue
		}
		matched = true
		if now.Before(st.CooldownTil) {
			continue
		}
		selectable = true
		if score := computePaceScore(st, now); score > best {
			best = score
		}
	}
	if selectable {
		return best, true
	}
	if matched {
		return -1000, true
	}
	return 0, false
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

// SetTokenSaver receives rotated access and refresh tokens. The pool calls it
// without holding its lock. Copilot bearer exchanges are not persisted.
func (p *Pool) SetTokenSaver(fn func(TokenUpdate)) {
	p.mu.Lock()
	p.onSave = fn
	p.mu.Unlock()
}

// RefreshError is the last background or inline refresh failure for an account.
func (p *Pool) RefreshError(id string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	st := p.accounts[id]
	if st == nil {
		return ""
	}
	return st.lastRefreshErr
}

// StartRefresher renews tokens that are inside the lead time until ctx ends.
// The first pass runs immediately, so an already-due token does not wait for
// a client request or for the first tick.
func (p *Pool) StartRefresher(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	go p.refreshLoop(ctx, refreshEvery)
}

func (p *Pool) refreshLoop(ctx context.Context, every time.Duration) {
	p.RefreshDue(time.Now())
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			p.RefreshDue(now)
		}
	}
}

// RefreshDue refreshes every enabled account whose token is inside the lead
// time or already expired. A failed refresh leaves a still-valid token in
// place and records the error for the next pass.
func (p *Pool) RefreshDue(now time.Time) {
	type job struct {
		id   string
		acct config.AccountConfig
	}
	p.mu.Lock()
	var jobs []job
	for _, st := range p.accounts {
		if p.tokenDueLocked(st, now) {
			jobs = append(jobs, job{id: st.Account.ID, acct: st.Account})
		}
	}
	p.mu.Unlock()

	for _, j := range jobs {
		upd, err := p.refreshSnapshot(j.acct)
		p.mu.Lock()
		st := p.accounts[j.id]
		if st == nil {
			p.mu.Unlock()
			continue
		}
		if err != nil {
			st.lastRefreshErr = err.Error()
			p.mu.Unlock()
			log.Printf("[pear] refresh %s: %v", j.id, err)
			continue
		}
		p.applyLocked(st, upd)
		save := p.onSave
		p.mu.Unlock()
		log.Printf("[pear] refreshed %s", j.id)
		if upd.Persist && save != nil {
			save(upd.Update)
		}
	}
}

// ResolveBearerToken returns a valid Authorization Bearer token, exchanging or refreshing if needed.
func (p *Pool) ResolveBearerToken(st *AccountState) (string, error) {
	if st == nil {
		return "", fmt.Errorf("nil account")
	}
	p.mu.Lock()
	now := time.Now()
	force := st.forceRefresh
	st.forceRefresh = false
	if !force && st.BearerCached != "" && now.Add(refreshLead).Before(st.BearerExpiry) {
		tok := st.BearerCached
		p.mu.Unlock()
		return tok, nil
	}
	due := force || p.tokenDueLocked(st, now)
	acct := st.Account
	kind := accountKind(acct.Agent)
	p.mu.Unlock()

	if !due || kind == "" {
		p.mu.Lock()
		tok, ok := usableToken(st, now)
		p.mu.Unlock()
		if ok {
			return tok, nil
		}
		return "", fmt.Errorf("no usable token for %s", acct.ID)
	}

	upd, err := p.refreshSnapshot(acct)
	p.mu.Lock()
	if err != nil {
		st.lastRefreshErr = err.Error()
		tok, ok := usableToken(st, now)
		p.mu.Unlock()
		if ok {
			return tok, nil
		}
		return "", err
	}
	p.applyLocked(st, upd)
	tok, _ := usableToken(st, now)
	save := p.onSave
	p.mu.Unlock()
	if upd.Persist && save != nil {
		save(upd.Update)
	}
	if tok == "" {
		return "", fmt.Errorf("refresh produced an empty token for %s", acct.ID)
	}
	return tok, nil
}

func (p *Pool) applyLocked(st *AccountState, upd tokenResult) {
	st.lastRefreshErr = ""
	if upd.Persist {
		if upd.Update.AccessToken != "" {
			st.Account.AccessToken = upd.Update.AccessToken
		}
		if upd.Update.RefreshToken != "" {
			st.Account.RefreshToken = upd.Update.RefreshToken
		}
		if upd.Update.ExpiresAt > 0 {
			st.Account.ExpiresAt = upd.Update.ExpiresAt
		}
		st.BearerCached = st.Account.AccessToken
		st.BearerExpiry = time.Unix(st.Account.ExpiresAt, 0)
		return
	}
	st.BearerCached = upd.Bearer
	st.BearerExpiry = upd.BearerExpiry
}

type tokenResult struct {
	Update       TokenUpdate
	Persist      bool
	Bearer       string
	BearerExpiry time.Time
}

func (p *Pool) refreshSnapshot(acct config.AccountConfig) (tokenResult, error) {
	switch accountKind(acct.Agent) {
	case "codex":
		return p.refreshOAuth(acct, "codex", codexTokenURL, codexClientID)
	case "claude":
		res, err := p.refreshOAuth(acct, "claude", claudeTokenURL, claudeClientID)
		if err == nil || !claudeShouldFallback(err) {
			return res, err
		}
		return p.refreshOAuth(acct, "claude", claudeTokenURLLegacy, claudeClientID)
	case "copilot":
		return p.exchangeCopilot(acct.AccessToken)
	default:
		return tokenResult{}, fmt.Errorf("no refresh for %s", acct.Agent)
	}
}

func claudeShouldFallback(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "HTTP 404") || strings.Contains(msg, "HTTP 405") {
		return true
	}
	// A numbered HTTP status means the token host answered. Anything else
	// (DNS, connection reset) tries the legacy host.
	return !strings.Contains(msg, "HTTP ")
}

func (p *Pool) refreshOAuth(acct config.AccountConfig, kind, url, clientID string) (tokenResult, error) {
	if acct.RefreshToken == "" {
		return tokenResult{}, fmt.Errorf("%s refresh: empty refresh token", kind)
	}
	payload := map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     clientID,
		"refresh_token": acct.RefreshToken,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return tokenResult{}, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return tokenResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "pear")
	if kind == "claude" {
		req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return tokenResult{}, fmt.Errorf("%s refresh: %w", kind, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return tokenResult{}, fmt.Errorf("%s oauth refresh HTTP %d", kind, resp.StatusCode)
	}
	var res struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return tokenResult{}, err
	}
	if res.AccessToken == "" {
		return tokenResult{}, fmt.Errorf("%s refresh returned an empty access token", kind)
	}
	exp := time.Now().Add(time.Duration(res.ExpiresIn) * time.Second)
	if res.ExpiresIn <= 0 {
		exp = time.Now().Add(time.Hour)
	}
	return tokenResult{
		Persist: true,
		Update: TokenUpdate{
			ID:           acct.ID,
			AccessToken:  res.AccessToken,
			RefreshToken: res.RefreshToken,
			ExpiresAt:    exp.Unix(),
		},
	}, nil
}

func (p *Pool) exchangeCopilot(oauthToken string) (tokenResult, error) {
	if oauthToken == "" {
		return tokenResult{}, fmt.Errorf("empty copilot oauth token")
	}
	req, err := http.NewRequest(http.MethodGet, copilotTokenURL, nil)
	if err != nil {
		return tokenResult{}, err
	}
	req.Header.Set("Authorization", "token "+oauthToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GithubCopilot/1.250.0")
	resp, err := p.client.Do(req)
	if err != nil {
		return tokenResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return tokenResult{}, fmt.Errorf("copilot token exchange HTTP %d", resp.StatusCode)
	}
	var res struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return tokenResult{}, err
	}
	if res.Token == "" {
		return tokenResult{}, fmt.Errorf("copilot token exchange returned an empty token")
	}
	exp := time.Unix(res.ExpiresAt, 0)
	if res.ExpiresAt > 1_000_000_000_000 {
		exp = time.UnixMilli(res.ExpiresAt)
	}
	return tokenResult{Bearer: res.Token, BearerExpiry: exp}, nil
}

func accountKind(agent string) string {
	a := strings.TrimPrefix(agent, "plugin:")
	switch {
	case a == "codex":
		return "codex"
	case strings.Contains(agent, "github-copilot"):
		return "copilot"
	case a == "claude" || strings.HasPrefix(a, "claude"):
		return "claude"
	default:
		return ""
	}
}

func expiryTime(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	if v > 1_000_000_000_000 {
		return time.UnixMilli(v)
	}
	return time.Unix(v, 0)
}

func (p *Pool) tokenDueLocked(st *AccountState, now time.Time) bool {
	if st == nil || !st.Account.Enabled {
		return false
	}
	switch accountKind(st.Account.Agent) {
	case "codex", "claude":
		if st.Account.RefreshToken == "" {
			return false
		}
		exp := expiryTime(st.Account.ExpiresAt)
		if exp.IsZero() {
			return false
		}
		return !exp.After(now.Add(refreshLead))
	case "copilot":
		if st.Account.AccessToken == "" {
			return false
		}
		if st.BearerExpiry.IsZero() {
			return true
		}
		return !st.BearerExpiry.After(now.Add(refreshLead))
	default:
		return false
	}
}

func usableToken(st *AccountState, now time.Time) (string, bool) {
	if accountKind(st.Account.Agent) == "copilot" {
		if st.BearerCached != "" && now.Before(st.BearerExpiry) {
			return st.BearerCached, true
		}
		return "", false
	}
	if st.BearerCached != "" && now.Before(st.BearerExpiry) {
		return st.BearerCached, true
	}
	if tokenExpired(st, now) || st.Account.AccessToken == "" {
		return "", false
	}
	return st.Account.AccessToken, true
}

func tokenExpired(st *AccountState, now time.Time) bool {
	exp := expiryTime(st.Account.ExpiresAt)
	if exp.IsZero() {
		return false
	}
	return !now.Before(exp)
}

// ObserveResponseHeaders passively updates quota windows from Anthropic / OpenAI / Codex response headers.
// ForceRefresh drops the cached bearer token so the next ResolveBearerToken
// performs a real exchange/refresh. Called when the upstream answers 401.
func (p *Pool) ForceRefresh(st *AccountState) {
	if st == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	st.BearerCached = ""
	st.BearerExpiry = time.Time{}
	st.forceRefresh = true
}

// MarkCooldown benches an account for d. Called on 403 so a revoked or
// geo-blocked account stops being selected instead of absorbing every request.
func (p *Pool) MarkCooldown(accountID string, d time.Duration) {
	if accountID == "" || d <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	st, ok := p.accounts[accountID]
	if !ok {
		return
	}
	if until := time.Now().Add(d); until.After(st.CooldownTil) {
		st.CooldownTil = until
	}
}

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
