package router

import (
	"net/http"
	"testing"

	"github.com/alexxhchen-nu/pear/internal/config"
	"github.com/alexxhchen-nu/pear/internal/oauth"
)

func groupEngine(t *testing.T, strategy string, members []string, rules []config.RouteRule, pool *oauth.Pool) *Engine {
	t.Helper()
	providers := make([]config.ProviderConfig, 0, len(members))
	seen := map[string]bool{}
	for _, m := range members {
		id := m
		if i := indexSlash(m); i >= 0 {
			id = m[:i]
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		providers = append(providers, config.ProviderConfig{
			ID: id, Protocol: "openai", BaseURL: "http://" + id, Models: []string{"m"},
		})
	}
	cfg := &config.Config{
		Server:    config.ServerConfig{DefaultTarget: "group/g"},
		Providers: providers,
		Groups: []config.GroupConfig{{
			ID: "g", Name: "g", Strategy: strategy, Members: members, Rules: rules,
		}},
	}
	if pool == nil {
		pool = oauth.NewPool(nil)
	}
	return NewEngine(cfg, pool)
}

func indexSlash(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}

func providerIDs(cands []TargetCandidate) []string {
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.Provider.ID
	}
	return out
}

func resolve(t *testing.T, e *Engine, feat RequestFeatures) []string {
	t.Helper()
	if feat.RequestedModel == "" {
		feat.RequestedModel = "group/g"
	}
	cands, err := e.ResolveCandidates(feat)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return providerIDs(cands)
}

func TestStrategyOrderKeepsWrittenMemberOrder(t *testing.T) {
	e := groupEngine(t, "order", []string{"a/m", "b/m", "c/m"}, nil, nil)
	feat := RequestFeatures{RequestedModel: "group/g"}
	want := []string{"a", "b", "c"}
	if got := resolve(t, e, feat); !sameIDs(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	if got := resolve(t, e, feat); !sameIDs(got, want) {
		t.Fatalf("second order call = %v, want %v", got, want)
	}
}

func TestEmptyStrategyIsOrder(t *testing.T) {
	e := groupEngine(t, "", []string{"b/m", "a/m"}, nil, nil)
	got := resolve(t, e, RequestFeatures{RequestedModel: "group/g"})
	if !sameIDs(got, []string{"b", "a"}) {
		t.Fatalf("empty strategy = %v, want [b a]", got)
	}
}

func TestUnknownStrategyIsOrder(t *testing.T) {
	e := groupEngine(t, "weighted", []string{"a/m", "b/m"}, nil, nil)
	got := resolve(t, e, RequestFeatures{RequestedModel: "group/g"})
	if !sameIDs(got, []string{"a", "b"}) {
		t.Fatalf("unknown strategy = %v, want [a b]", got)
	}
}

func TestRoundRobinRotatesStartEachRequest(t *testing.T) {
	e := groupEngine(t, "round_robin", []string{"a/m", "b/m", "c/m"}, nil, nil)
	feat := RequestFeatures{RequestedModel: "group/g"}
	want := [][]string{
		{"a", "b", "c"},
		{"b", "c", "a"},
		{"c", "a", "b"},
		{"a", "b", "c"},
	}
	for i, w := range want {
		if got := resolve(t, e, feat); !sameIDs(got, w) {
			t.Fatalf("round robin call %d = %v, want %v", i+1, got, w)
		}
	}
}

func TestSessionStickyStartsAtHashedMember(t *testing.T) {
	e := groupEngine(t, "session_sticky", []string{"a/m", "b/m"}, nil, nil)
	first := resolve(t, e, RequestFeatures{RequestedModel: "group/g", SessionHash: 1})
	if !sameIDs(first, []string{"b", "a"}) {
		t.Fatalf("hash 1 = %v, want [b a]", first)
	}
	again := resolve(t, e, RequestFeatures{RequestedModel: "group/g", SessionHash: 1})
	if !sameIDs(again, []string{"b", "a"}) {
		t.Fatalf("same session changed order: %v", again)
	}
	other := resolve(t, e, RequestFeatures{RequestedModel: "group/g", SessionHash: 2})
	if !sameIDs(other, []string{"a", "b"}) {
		t.Fatalf("hash 2 = %v, want [a b]", other)
	}
}

func TestSmartPacePutsHigherRemainingQuotaFirst(t *testing.T) {
	pool := oauth.NewPool([]config.AccountConfig{
		{ID: "slow", Agent: "slow", Enabled: true, AccessToken: "t-slow"},
		{ID: "fast", Agent: "fast", Enabled: true, AccessToken: "t-fast"},
	})
	low := http.Header{}
	low.Set("anthropic-ratelimit-unified-5h-utilization", "0.90")
	pool.ObserveResponseHeaders("slow", low, http.StatusOK)
	high := http.Header{}
	high.Set("anthropic-ratelimit-unified-5h-utilization", "0.10")
	pool.ObserveResponseHeaders("fast", high, http.StatusOK)

	// Listed slow first. Pace must still try the account with more remaining quota first.
	e := groupEngine(t, "smart_pace", []string{"slow/m", "fast/m"}, nil, pool)
	got := resolve(t, e, RequestFeatures{RequestedModel: "group/g"})
	if !sameIDs(got, []string{"fast", "slow"}) {
		t.Fatalf("smart_pace = %v, want [fast slow]", got)
	}
}

func TestSmartPaceWithoutAccountsKeepsWrittenOrder(t *testing.T) {
	e := groupEngine(t, "smart_pace", []string{"b/m", "a/m"}, nil, nil)
	got := resolve(t, e, RequestFeatures{RequestedModel: "group/g"})
	if !sameIDs(got, []string{"b", "a"}) {
		t.Fatalf("smart_pace with no accounts = %v, want [b a]", got)
	}
}

func TestRecordedSuccessLeadsEvenUnderRoundRobin(t *testing.T) {
	e := groupEngine(t, "round_robin", []string{"a/m", "b/m", "c/m"}, nil, nil)
	feat := RequestFeatures{RequestedModel: "group/g", SessionHash: 7}
	cands, err := e.ResolveCandidates(feat)
	if err != nil {
		t.Fatal(err)
	}
	e.RecordSuccess(feat.SessionHash, cands[0])
	got := resolve(t, e, feat)
	if len(got) == 0 || got[0] != "a" {
		t.Fatalf("sticky session = %v, want a first", got)
	}
}

func TestMatchingRuleIgnoresStrategy(t *testing.T) {
	rules := []config.RouteRule{{
		Name: "img", WhenHasImage: true, Use: []string{"b/m"},
	}}
	e := groupEngine(t, "round_robin", []string{"a/m", "b/m", "c/m"}, rules, nil)
	got := resolve(t, e, RequestFeatures{RequestedModel: "group/g", HasImage: true})
	if !sameIDs(got, []string{"b"}) {
		t.Fatalf("rule = %v, want [b]", got)
	}
	// The ruled request must not consume the round-robin cursor.
	next := resolve(t, e, RequestFeatures{RequestedModel: "group/g"})
	if !sameIDs(next, []string{"a", "b", "c"}) {
		t.Fatalf("after rule, round robin = %v, want [a b c]", next)
	}
}

func sameIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
