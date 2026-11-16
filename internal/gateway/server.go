package gateway

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/alexxhchen-nu/pear/internal/config"
	"github.com/alexxhchen-nu/pear/internal/oauth"
	"github.com/alexxhchen-nu/pear/internal/rosetta"
	"github.com/alexxhchen-nu/pear/internal/router"
)

// Server is the core Pear HTTP gateway.
type Server struct {
	cfg            *config.Config
	pool           *oauth.Pool
	engine         *router.Engine
	standardClient *http.Client
	insecureClient *http.Client
}

func NewServer(cfg *config.Config, pool *oauth.Pool, engine *router.Engine) *Server {
	return &Server{
		cfg:    cfg,
		pool:   pool,
		engine: engine,
		standardClient: &http.Client{
			Timeout: 10 * time.Minute,
		},
		insecureClient: &http.Client{
			Timeout: 10 * time.Minute,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", s.handleProxy)
	mux.HandleFunc("/v1/chat/completions", s.handleProxy)
	mux.HandleFunc("/v1/responses", s.handleProxy)
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/pear/quotas", s.handleQuotas)
	mux.HandleFunc("/health", s.handleHealth)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":    "ok",
		"ecosystem": "Pear",
		"providers": len(s.cfg.Providers),
		"accounts":  len(s.pool.Snapshot()),
		"groups":    len(s.cfg.Groups),
	})
}

func (s *Server) handleQuotas(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.pool.Snapshot())
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	var data []map[string]any
	for _, g := range s.cfg.Groups {
		data = append(data, map[string]any{
			"id":       "group/" + g.ID,
			"object":   "model",
			"owned_by": "pear-group",
		})
	}
	for _, p := range s.cfg.Providers {
		if p.Off {
			continue
		}
		for _, m := range p.Models {
			data = append(data, map[string]any{
				"id":       p.ID + "/" + m,
				"object":   "model",
				"owned_by": p.ID,
			})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   data,
	})
}

func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 32*1024*1024))
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Extract requested model quickly
	var meta struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &meta)

	projectDir := r.Header.Get("X-Project-Dir")
	feat := router.InspectRequest(body, meta.Model, projectDir)

	candidates, err := s.engine.ResolveCandidates(feat)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	var lastErr error
	for idx, cand := range candidates {
		err := s.tryCandidate(r.Context(), w, r, body, feat, cand)
		if err == nil {
			s.engine.RecordSuccess(feat.SessionHash, cand)
			return
		}
		lastErr = err
		log.Printf("[pear-failover] candidate #%d (%s/%s) failed: %v", idx+1, cand.Provider.ID, cand.Model, err)
	}

	http.Error(w, fmt.Sprintf("Pear gateway exhausted all %d candidates; last error: %v", len(candidates), lastErr), http.StatusBadGateway)
}

func (s *Server) tryCandidate(
	ctx context.Context,
	w http.ResponseWriter,
	inReq *http.Request,
	rawBody []byte,
	feat router.RequestFeatures,
	cand router.TargetCandidate,
) error {
	rewritten := rosetta.SanitizeAndRewriteRequest(rawBody, cand.Model, cand.Provider.Protocol)
	upstreamURL := buildUpstreamURL(cand.Provider.BaseURL, inReq.URL.Path)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(rewritten))
	if err != nil {
		return err
	}

	// Copy safe headers
	req.Header.Set("Content-Type", "application/json")
	if accept := inReq.Header.Get("Accept"); accept != "" {
		req.Header.Set("Accept", accept)
	}
	if beta := inReq.Header.Get("Anthropic-Beta"); beta != "" {
		req.Header.Set("Anthropic-Beta", beta)
	}
	if ver := inReq.Header.Get("Anthropic-Version"); ver != "" {
		req.Header.Set("Anthropic-Version", ver)
	} else if cand.Provider.Protocol == "anthropic" {
		req.Header.Set("Anthropic-Version", "2023-06-01")
	}

	// Attach credentials (OAuth Bearer or API Key)
	token := cand.Provider.APIKey
	if cand.Account != nil {
		if resolved, err := s.pool.ResolveBearerToken(cand.Account); err == nil && resolved != "" {
			token = resolved
		}
	}
	if token != "" {
		if cand.Provider.Protocol == "anthropic" {
			req.Header.Set("x-api-key", token)
			req.Header.Set("Authorization", "Bearer "+token)
		} else {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	client := s.standardClient
	if cand.Provider.InsecureTLS {
		client = s.insecureClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if cand.Account != nil {
		s.pool.ObserveResponseHeaders(cand.Account.Account.ID, resp.Header, resp.StatusCode)
	}

	// Failover on 429 / 502 / 503 / 529 before writing headers to client
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("upstream HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
	}

	// Forward response headers & stream body
	for k, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("X-Pear-Routed-Provider", cand.Provider.ID)
	w.Header().Set("X-Pear-Routed-Model", cand.Model)
	w.WriteHeader(resp.StatusCode)

	flusher, canFlush := w.(http.Flusher)
	buf := make([]byte, 16*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return nil
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if readErr != nil {
			break
		}
	}
	return nil
}

func buildUpstreamURL(baseURL, reqPath string) string {
	base := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(base, "/v1") && strings.HasPrefix(reqPath, "/v1/") {
		return base + strings.TrimPrefix(reqPath, "/v1")
	}
	return base + reqPath
}
