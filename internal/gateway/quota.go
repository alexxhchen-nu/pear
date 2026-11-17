package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/alexxhchen-nu/pear/internal/oauth"
)

// QuotaBaseURL chooses where `pear quota` reads live windows.
// An explicit override wins, then the address the running gateway wrote,
// then server.listen from the config file.
func QuotaBaseURL(listen, runtimeAddr, override string) string {
	if u := strings.TrimSpace(override); u != "" {
		return strings.TrimRight(u, "/")
	}
	if addr := strings.TrimSpace(runtimeAddr); addr != "" {
		return listenBaseURL(addr)
	}
	return listenBaseURL(listen)
}

func listenBaseURL(listen string) string {
	listen = strings.TrimSpace(listen)
	if strings.HasPrefix(listen, "http://") || strings.HasPrefix(listen, "https://") {
		return strings.TrimRight(listen, "/")
	}
	if strings.HasPrefix(listen, ":") {
		listen = "127.0.0.1" + listen
	}
	return "http://" + strings.TrimRight(listen, "/")
}

// FetchQuotas reads GET /v1/pear/quotas from a running gateway.
func FetchQuotas(ctx context.Context, baseURL string) ([]oauth.AccountState, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/pear/quotas", nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gateway at %s is not running: %w", base, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gateway at %s returned HTTP %d", base, resp.StatusCode)
	}
	var snaps []oauth.AccountState
	if err := json.NewDecoder(resp.Body).Decode(&snaps); err != nil {
		return nil, fmt.Errorf("gateway at %s returned invalid quota JSON: %w", base, err)
	}
	return snaps, nil
}
