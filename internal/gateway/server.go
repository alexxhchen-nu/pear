package gateway

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alexxhchen-nu/pear/internal/config"
	"github.com/alexxhchen-nu/pear/internal/oauth"
	"github.com/alexxhchen-nu/pear/internal/rosetta"
	"github.com/alexxhchen-nu/pear/internal/router"
)

// maxRequestBodyBytes caps a proxied request body. Exceeding it is an error,
// never a silent truncation.
const maxRequestBodyBytes = 32 << 20

// writeError emits one body that satisfies both client families: OpenAI reads
// error.{type,message}, Anthropic reads type=error with error.{type,message}.
func writeError(w http.ResponseWriter, status int, typ, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"type":    "error",
		"message": msg,
		"error": map[string]any{
			"type":    typ,
			"message": msg,
		},
	})
}

// errTTFTTimeout marks an upstream that accepted the request but never started
// streaming within the configured budget.
var errTTFTTimeout = errors.New("time to first byte exceeded")

// errClientGone marks a request the caller abandoned. It is not an upstream
// failure, so it must not trigger failover or taint the candidate.
var errClientGone = errors.New("client disconnected")

// Server is the core Pear HTTP gateway.
type Server struct {
	cfg            *config.Config
	pool           *oauth.Pool
	engine         *router.Engine
	standardClient *http.Client
	insecureClient *http.Client
}

// upstreamIdleTimeout bounds the wait for the next chunk once a stream is
// running. A stalled stream is terminated instead of hanging a caller forever;
// it is not a total deadline, so a slow-but-alive generation runs as long as it
// keeps sending bytes.
var upstreamIdleTimeout = 10 * time.Minute

// upstreamHeaderTimeout bounds the wait for response headers only. The totals
// are deliberately unbounded: a hard client Timeout used to cut legitimately
// long SSE responses off mid-stream.
const upstreamHeaderTimeout = 90 * time.Second

func upstreamTransport(tlsCfg *tls.Config) *http.Transport {
	return &http.Transport{
		TLSClientConfig:       tlsCfg,
		ForceAttemptHTTP2:     true,
		ResponseHeaderTimeout: upstreamHeaderTimeout,
		TLSHandshakeTimeout:   15 * time.Second,
		IdleConnTimeout:       90 * time.Second,
	}
}

func NewServer(cfg *config.Config, pool *oauth.Pool, engine *router.Engine) *Server {
	return &Server{
		cfg:            cfg,
		pool:           pool,
		engine:         engine,
		standardClient: &http.Client{Transport: upstreamTransport(nil)},
		insecureClient: &http.Client{
			Transport: upstreamTransport(&tls.Config{InsecureSkipVerify: true}),
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
		writeError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed: use POST")
		return
	}
	// MaxBytesReader rejects instead of silently truncating: a 32 MB cap that
	// quietly cut the body produced a malformed prompt upstream.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large",
				fmt.Sprintf("request body exceeds %d bytes", maxRequestBodyBytes))
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_request_error", "failed to read request body: "+err.Error())
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
		writeError(w, http.StatusBadGateway, "routing_error", err.Error())
		return
	}

	var lastErr error
	for idx, cand := range candidates {
		committed, err := s.tryCandidate(r.Context(), w, r, body, feat, cand)
		if err == nil {
			s.engine.RecordSuccess(feat.SessionHash, cand)
			return
		}
		if committed {
			// Headers and part of the body are already on the wire. Failover
			// would append a second response to a half-written stream, so all
			// that is left is to end the request and log the fault.
			log.Printf("[pear-stream-abort] %s/%s after commit: %v", cand.Provider.ID, cand.Model, err)
			return
		}
		if errors.Is(err, errClientGone) {
			// The caller hung up; no failover can help and nothing is left to write.
			return
		}
		lastErr = err
		log.Printf("[pear-failover] candidate #%d (%s/%s) failed: %v", idx+1, cand.Provider.ID, cand.Model, err)
	}

	writeError(w, http.StatusBadGateway, "upstream_error",
		fmt.Sprintf("pear gateway exhausted all %d candidates; last error: %v", len(candidates), lastErr))
}

// doUpstream builds and executes one upstream attempt for a candidate.
func (s *Server) doUpstream(ctx context.Context, inReq *http.Request, cand router.TargetCandidate, body []byte) (*http.Response, error) {
	upstreamURL := buildUpstreamURL(cand.Provider.BaseURL, inReq.URL.Path)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
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
	return client.Do(req)
}

// errUpstreamStalled marks a stream that started and then stopped sending
// within the idle budget.
var errUpstreamStalled = errors.New("upstream stream stalled")

// readChunk reads one chunk, giving up after timeout or when the caller goes
// away. The reader goroutine exits once the body is closed.
func readChunk(ctx context.Context, body io.Reader, timeout time.Duration) ([]byte, error) {
	type chunk struct {
		b   []byte
		n   int
		err error
	}
	ch := make(chan chunk, 1)
	go func() {
		b := make([]byte, 16*1024)
		n, err := body.Read(b)
		ch <- chunk{b, n, err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case got := <-ch:
		return got.b[:got.n], got.err
	case <-timer.C:
		return nil, errTTFTTimeout
	case <-ctx.Done():
		return nil, errClientGone
	}
}

// drainAndClose releases a response we are about to abandon so the connection
// can be reused instead of being dropped.
func drainAndClose(b io.ReadCloser) {
	io.CopyN(io.Discard, b, 4096)
	b.Close()
}

// isCandidateFailure reports statuses that mean "this candidate is wrong",
// never "the caller did something wrong".
func isCandidateFailure(code int) bool {
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests:
		return true
	}
	return code >= 500
}

// cooldownFromHeaders honours Retry-After (seconds) and falls back to def.
func cooldownFromHeaders(h http.Header, def time.Duration) time.Duration {
	if ra := h.Get("Retry-After"); ra != "" {
		if v, err := strconv.Atoi(ra); err == nil && v > 0 {
			return time.Duration(v) * time.Second
		}
	}
	return def
}

// tryCandidate reports whether any part of a response reached the client. Once
// committed, the caller must not attempt another candidate.
func (s *Server) tryCandidate(
	ctx context.Context,
	w http.ResponseWriter,
	inReq *http.Request,
	rawBody []byte,
	feat router.RequestFeatures,
	cand router.TargetCandidate,
) (bool, error) {
	rewritten := rosetta.SanitizeAndRewriteRequest(rawBody, cand.Model, cand.Provider.Protocol)

	resp, err := s.doUpstream(ctx, inReq, cand, rewritten)
	if err != nil {
		return false, err
	}

	// 401: the credential may have been rotated upstream. Refresh once and
	// retry the same candidate; only a second 401 counts as a failure.
	if resp.StatusCode == http.StatusUnauthorized && cand.Account != nil {
		drainAndClose(resp.Body)
		s.pool.ForceRefresh(cand.Account)
		retry, retryErr := s.doUpstream(ctx, inReq, cand, rewritten)
		if retryErr != nil {
			return false, retryErr
		}
		resp = retry
	}
	defer resp.Body.Close()

	if cand.Account != nil {
		s.pool.ObserveResponseHeaders(cand.Account.Account.ID, resp.Header, resp.StatusCode)
		if resp.StatusCode == http.StatusForbidden {
			// Scope revoked or account restricted: bench it rather than have
			// every following request pay for the same rejection.
			s.pool.MarkCooldown(cand.Account.Account.ID, cooldownFromHeaders(resp.Header, 15*time.Minute))
		}
	}

	// Any auth/route/limit failure fails this candidate over before headers
	// reach the client, so the caller never sees a bare 401/403/404.
	if isCandidateFailure(resp.StatusCode) {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return false, fmt.Errorf("upstream HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
	}

	// Prime the first chunk before committing headers: an upstream that accepts
	// the request and then sends nothing must fail over, not hand the caller an
	// empty 200. TTFTTimeoutMS is the budget for that first byte.
	ttft := time.Duration(s.cfg.Server.TTFTTimeoutMS) * time.Millisecond
	if ttft <= 0 {
		ttft = 12 * time.Second
	}
	first, firstErr := readChunk(ctx, resp.Body, ttft)
	if firstErr != nil && !errors.Is(firstErr, io.EOF) {
		if ctx.Err() != nil {
			return false, errClientGone
		}
		return false, fmt.Errorf("no first byte from upstream: %w", firstErr)
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
	if len(first) > 0 {
		if _, writeErr := w.Write(first); writeErr != nil {
			if ctx.Err() != nil {
				return true, errClientGone
			}
			return true, fmt.Errorf("client write failed: %w", writeErr)
		}
		if canFlush {
			flusher.Flush()
		}
	}
	if firstErr != nil {
		if errors.Is(firstErr, io.EOF) {
			return true, nil
		}
		if ctx.Err() != nil {
			return true, errClientGone
		}
		return true, fmt.Errorf("upstream stream fault: %w", firstErr)
	}

	for {
		// Every wait is bounded, no total is: a stalled stream is terminated,
		// a long one that keeps producing bytes is not.
		next, readErr := readChunk(ctx, resp.Body, upstreamIdleTimeout)
		if len(next) > 0 {
			if _, writeErr := w.Write(next); writeErr != nil {
				if ctx.Err() != nil {
					return true, errClientGone
				}
				return true, fmt.Errorf("client write failed: %w", writeErr)
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return true, nil
			}
			if errors.Is(readErr, errTTFTTimeout) {
				readErr = errUpstreamStalled
			}
			// A cut-off upstream stream must never be recorded as success:
			// the client received a partial body and the candidate is unhealthy.
			if ctx.Err() != nil {
				return true, errClientGone
			}
			return true, fmt.Errorf("upstream stream fault: %w", readErr)
		}
	}
}

func buildUpstreamURL(baseURL, reqPath string) string {
	base := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(base, "/v1") && strings.HasPrefix(reqPath, "/v1/") {
		return base + strings.TrimPrefix(reqPath, "/v1")
	}
	return base + reqPath
}
