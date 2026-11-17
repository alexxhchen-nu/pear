package router

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	"github.com/alexxhchen-nu/pear/internal/config"
	"github.com/alexxhchen-nu/pear/internal/oauth"
)

// RequestFeatures holds fast zero-copy extracted signals from an incoming LLM request.
type RequestFeatures struct {
	RequestedModel string
	SessionHash    uint64
	EstTokens      int
	HasImage       bool
	IsCompact      bool
	IsSubagent     bool
	Folder         string
}

// TargetCandidate represents one resolved upstream target (`provider/model`).
type TargetCandidate struct {
	Provider config.ProviderConfig
	Model    string
	Account  *oauth.AccountState
}

type stickyEntry struct {
	Target    string
	ExpiresAt time.Time
}

// Engine executes routing rules, session stickiness, and OAuth Smart Pace selection.
type Engine struct {
	mu        sync.RWMutex
	cfg       *config.Config
	pool      *oauth.Pool
	stickyMap map[uint64]stickyEntry
	rrIndex   map[string]uint64
}

func NewEngine(cfg *config.Config, pool *oauth.Pool) *Engine {
	return &Engine{
		cfg:       cfg,
		pool:      pool,
		stickyMap: make(map[uint64]stickyEntry),
		rrIndex:   make(map[string]uint64),
	}
}

// InspectRequest extracts routing features in <0.1ms using byte scanning without full JSON DOM allocation.
func InspectRequest(body []byte, requestedModel string, projectDir string) RequestFeatures {
	estTokens := len(body) / 4

	hasImage := bytes.Contains(body, []byte(`"image"`)) || bytes.Contains(body, []byte(`"image_url"`))
	isCompact := bytes.Contains(body, []byte(`Summarize the conversation`)) ||
		bytes.Contains(body, []byte(`<context-compaction>`)) ||
		bytes.Contains(body, []byte(`compact the conversation`))
	isSubagent := bytes.Contains(body, []byte(`read-only search agent`)) ||
		bytes.Contains(body, []byte(`Generate a concise title`))

	// Compute a deterministic session hash from the first ~512 bytes of messages/system
	h := fnv.New64a()
	sampleLen := len(body)
	if sampleLen > 768 {
		sampleLen = 768
	}
	h.Write(body[:sampleLen])

	return RequestFeatures{
		RequestedModel: requestedModel,
		SessionHash:    h.Sum64(),
		EstTokens:      estTokens,
		HasImage:       hasImage,
		IsCompact:      isCompact,
		IsSubagent:     isSubagent,
		Folder:         projectDir,
	}
}

// ResolveCandidates returns an ordered failover list of upstream targets for the given request.
func (e *Engine) ResolveCandidates(feat RequestFeatures) ([]TargetCandidate, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	modelSpec := strings.TrimSpace(feat.RequestedModel)
	if modelSpec == "" || modelSpec == "auto" || modelSpec == "default" {
		modelSpec = e.cfg.Server.DefaultTarget
	}

	// Global fast-path overrides for compaction / subagents
	if feat.IsCompact && e.cfg.Server.CompactTarget != "" {
		modelSpec = e.cfg.Server.CompactTarget
	} else if feat.IsSubagent && e.cfg.Server.SubagentTarget != "" {
		modelSpec = e.cfg.Server.SubagentTarget
	}

	rawTargets := e.expandTargetList(modelSpec, feat, 0)
	if len(rawTargets) == 0 && len(e.cfg.Providers) > 0 {
		// Fallback to first enabled provider
		for _, p := range e.cfg.Providers {
			if !p.Off && len(p.Models) > 0 {
				rawTargets = append(rawTargets, p.ID+"/"+p.Models[0])
				break
			}
		}
	}

	// Check session stickiness: if the sticky target is in rawTargets, move it to index 0
	now := time.Now()
	if sticky, ok := e.stickyMap[feat.SessionHash]; ok && now.Before(sticky.ExpiresAt) {
		for i, t := range rawTargets {
			if t == sticky.Target && i > 0 {
				rawTargets[0], rawTargets[i] = rawTargets[i], rawTargets[0]
				break
			}
		}
	}

	var candidates []TargetCandidate
	for _, spec := range rawTargets {
		cand, err := e.resolveSingleSpec(spec)
		if err == nil {
			candidates = append(candidates, cand)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no healthy upstream candidates resolved for %q", modelSpec)
	}
	return candidates, nil
}

// RecordSuccess binds the session hash to the winning provider/model so Prompt Cache stays warm.
func (e *Engine) RecordSuccess(sessionHash uint64, cand TargetCandidate) {
	e.mu.Lock()
	defer e.mu.Unlock()
	ttl := time.Duration(e.cfg.Server.StickyTTLMin) * time.Minute
	if ttl <= 0 {
		ttl = 60 * time.Minute
	}
	e.stickyMap[sessionHash] = stickyEntry{
		Target:    cand.Provider.ID + "/" + cand.Model,
		ExpiresAt: time.Now().Add(ttl),
	}
}

func (e *Engine) expandTargetList(spec string, feat RequestFeatures, depth int) []string {
	if depth > 4 {
		return nil
	}
	if strings.HasPrefix(spec, "group/") {
		groupID := strings.TrimPrefix(spec, "group/")
		for _, g := range e.cfg.Groups {
			if g.ID == groupID || g.Name == groupID {
				for _, rule := range g.Rules {
					if matchesRule(rule, feat) && len(rule.Use) > 0 {
						var out []string
						for _, u := range rule.Use {
							out = append(out, e.expandTargetList(u, feat, depth+1)...)
						}
						return out
					}
				}
				var out []string
				for _, m := range e.orderMembers(g, feat) {
					out = append(out, e.expandTargetList(m, feat, depth+1)...)
				}
				return out
			}
		}
	}
	return []string{spec}
}

// orderMembers applies the group strategy when no rule matched.
// order and any unknown name keep the written member list.
func (e *Engine) orderMembers(g config.GroupConfig, feat RequestFeatures) []string {
	members := append([]string(nil), g.Members...)
	switch g.Strategy {
	case "round_robin":
		if len(members) == 0 {
			return members
		}
		start := int(e.rrIndex[g.ID] % uint64(len(members)))
		e.rrIndex[g.ID]++
		return rotate(members, start)
	case "session_sticky":
		if len(members) == 0 {
			return members
		}
		start := int(feat.SessionHash % uint64(len(members)))
		return rotate(members, start)
	case "smart_pace":
		return e.paceOrder(members)
	default:
		return members
	}
}

func rotate(members []string, start int) []string {
	if len(members) == 0 || start%len(members) == 0 {
		return members
	}
	start %= len(members)
	out := make([]string, 0, len(members))
	out = append(out, members[start:]...)
	out = append(out, members[:start]...)
	return out
}

// paceOrder ranks members by the best OAuth account linked to that provider.
// A member with no account scores the same neutral value as an account that
// has not reported a window yet, so equal scores stay in written order.
func (e *Engine) paceOrder(members []string) []string {
	if len(members) < 2 || e.pool == nil {
		return members
	}
	now := time.Now()
	type scored struct {
		spec  string
		score float64
	}
	rows := make([]scored, len(members))
	for i, spec := range members {
		rows[i] = scored{spec: spec, score: e.memberPace(spec, now)}
	}
	// Stable by original index when scores tie.
	for i := 1; i < len(rows); i++ {
		row := rows[i]
		j := i
		for j > 0 && rows[j-1].score < row.score {
			rows[j] = rows[j-1]
			j--
		}
		rows[j] = row
	}
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = row.spec
	}
	return out
}

func (e *Engine) memberPace(spec string, now time.Time) float64 {
	const neutral = 50.0
	if strings.HasPrefix(spec, "group/") || e.pool == nil {
		return neutral
	}
	provID := spec
	if i := strings.IndexByte(spec, '/'); i >= 0 {
		provID = spec[:i]
	}
	if score, ok := e.pool.BestPace(provID, now); ok {
		return score
	}
	return neutral
}

func matchesRule(r config.RouteRule, f RequestFeatures) bool {
	if r.WhenCompact && !f.IsCompact {
		return false
	}
	if r.WhenSubagent && !f.IsSubagent {
		return false
	}
	if r.WhenHasImage && !f.HasImage {
		return false
	}
	if r.MinTokens > 0 && f.EstTokens < r.MinTokens {
		return false
	}
	if r.FolderContains != "" && !strings.Contains(strings.ToLower(f.Folder), strings.ToLower(r.FolderContains)) {
		return false
	}
	if r.TimeRange != "" && !inTimeRange(r.TimeRange, time.Now()) {
		return false
	}
	return r.WhenCompact || r.WhenSubagent || r.WhenHasImage || r.MinTokens > 0 || r.FolderContains != "" || r.TimeRange != ""
}

func inTimeRange(tr string, now time.Time) bool {
	parts := strings.Split(tr, "-")
	if len(parts) != 2 {
		return false
	}
	cur := now.Format("15:04")
	if parts[0] <= parts[1] {
		return cur >= parts[0] && cur <= parts[1]
	}
	return cur >= parts[0] || cur <= parts[1]
}

func (e *Engine) resolveSingleSpec(spec string) (TargetCandidate, error) {
	parts := strings.SplitN(spec, "/", 2)
	if len(parts) == 2 {
		provID, modelID := parts[0], parts[1]
		for _, p := range e.cfg.Providers {
			if p.ID == provID && !p.Off {
				cand := TargetCandidate{Provider: p, Model: modelID}
				if strings.HasPrefix(p.Protocol, "oauth-") || p.APIKey == "" {
					if acct, err := e.pool.SelectBestAccount(p.ID); err == nil {
						cand.Account = acct
					}
				}
				return cand, nil
			}
		}
	}
	// If requested as a bare model name (e.g. "claude-opus-5-5" or "glm-5.3"), search providers that offer it
	for _, p := range e.cfg.Providers {
		if p.Off {
			continue
		}
		for _, m := range p.Models {
			if m == spec || strings.HasSuffix(m, "/"+spec) {
				cand := TargetCandidate{Provider: p, Model: m}
				if strings.HasPrefix(p.Protocol, "oauth-") || p.APIKey == "" {
					if acct, err := e.pool.SelectBestAccount(p.ID); err == nil {
						cand.Account = acct
					}
				}
				return cand, nil
			}
		}
	}
	return TargetCandidate{}, fmt.Errorf("provider/model %q not found", spec)
}
