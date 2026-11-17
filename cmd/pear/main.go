package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/alexxhchen-nu/pear/internal/config"
	"github.com/alexxhchen-nu/pear/internal/gateway"
	"github.com/alexxhchen-nu/pear/internal/oauth"
	"github.com/alexxhchen-nu/pear/internal/router"
	"github.com/alexxhchen-nu/pear/internal/ui"
)

// version is stamped at build time via -X main.version=...
// (.goreleaser.yaml is the only copy of those flags). Release binaries
// carry the tag; `mise run build` carries git describe. A plain
// `go build` / `go run` keeps "dev".
var version = "dev"

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		printHelp()
		return
	}

	switch args[0] {
	case "version", "--version", "-v":
		fmt.Printf("pear v%s (Pear AI Gateway & OAuth Router)\n", version)
	case "status":
		runStatus()
	case "quota", "quotas":
		runQuota(args[1:])
	case "providers":
		runProviders()
	case "groups":
		runGroups()
	case "serve", "daemon":
		runServe(args[1:])
	default:
		printHelp()
	}
}

func printHelp() {
	fmt.Print(ui.Banner())
	fmt.Println(`Pear (v` + version + `) — Ultra-lightweight AI Router & OAuth Subscription Pool

Usage:
  pear serve [--port N]    Start the Pear Gateway HTTP/SSE server (default 127.0.0.1:3426)
  pear status              Show configured providers, OAuth accounts, and routing groups
  pear quota [--json] [--url URL]
                       Read live quota windows from the running gateway
  pear providers           List all upstream API & OAuth providers
  pear groups              List all smart routing groups and rules`)
}

func loadConfig() (*config.Config, error) {
	return config.Load(config.DefaultConfigPath())
}

func runStatus() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}
	activeProv := 0
	totalModels := 0
	for _, p := range cfg.Providers {
		if !p.Off {
			activeProv++
			totalModels += len(p.Models)
		}
	}
	activeAcct := 0
	for _, a := range cfg.Accounts {
		if a.Enabled {
			activeAcct++
		}
	}
	fmt.Print(ui.Banner())
	fmt.Print(ui.SectionHeader("网关状态 Gateway Status"))
	fmt.Println(ui.KV("Config File", config.DefaultConfigPath()))
	fmt.Println(ui.KV("Listen Address", ui.Accent("http://"+cfg.Server.Listen)))
	fmt.Println(ui.KV("Default Route", ui.Green(cfg.Server.DefaultTarget)))
	fmt.Println(ui.KV("Providers", fmt.Sprintf("%s active / %d total (%d models)", ui.Green(fmt.Sprintf("%d", activeProv)), len(cfg.Providers), totalModels)))
	fmt.Println(ui.KV("OAuth Accounts", fmt.Sprintf("%s active / %d total", ui.Green(fmt.Sprintf("%d", activeAcct)), len(cfg.Accounts))))
	fmt.Println(ui.KV("Routing Groups", fmt.Sprintf("%d", len(cfg.Groups))))
	fmt.Println()
}

func parseQuotaArgs(args []string) (jsonOut bool, url string, err error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			jsonOut = true
		case "--url":
			if i+1 >= len(args) {
				return false, "", fmt.Errorf("--url needs an address")
			}
			i++
			url = args[i]
		default:
			return false, "", fmt.Errorf("unknown argument %s", args[i])
		}
	}
	return jsonOut, url, nil
}

// liveQuotas reads the running gateway. It does not build a pool from the file,
// because that pool has never observed a response.
func liveQuotas(override string) ([]oauth.AccountState, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	runtime, _ := config.ReadRuntimeAddr()
	base := gateway.QuotaBaseURL(cfg.Server.Listen, runtime, override)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return gateway.FetchQuotas(ctx, base)
}

func runQuota(args []string) {
	asJSON, override, err := parseQuotaArgs(args)
	if err != nil {
		log.Fatalf("pear quota: %v", err)
	}
	snaps, err := liveQuotas(override)
	if err != nil {
		log.Fatalf("pear quota: %v", err)
	}

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(snaps)
		return
	}

	fmt.Print(ui.Banner())
	fmt.Print(ui.SectionHeader("OAuth 订阅池配额 Quota Windows"))
	if len(snaps) == 0 {
		fmt.Println(ui.Muted("  (no OAuth accounts configured — add an oauth-* provider to ~/.pear/pear.json)"))
		return
	}
	for _, s := range snaps {
		status := ui.Green("● ready")
		if !s.Account.Enabled {
			status = ui.Muted("○ off")
		}
		head := fmt.Sprintf("  %s  %s  %s  %s",
			ui.Bold(s.Account.ID),
			ui.Accent(s.Account.Agent),
			ui.Muted(s.Account.User),
			status,
		)
		if s.Account.Plan != "" {
			head += "  " + ui.Muted("["+s.Account.Plan+"]")
		}
		fmt.Println(head)
		if len(s.Windows) == 0 {
			fmt.Println(ui.Muted("      (no quota windows observed yet)"))
		}
		for _, win := range s.Windows {
			line := fmt.Sprintf("      %-6s %s", win.Name, ui.Bar(win.RemainPct, 24))
			if !win.ResetsAt.IsZero() {
				line += ui.Muted(fmt.Sprintf("  resets %s", win.ResetsAt.Format("01-02 15:04")))
			}
			fmt.Println(line)
		}
		fmt.Println()
	}
}

func runProviders() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tPROTOCOL\tBASE URL\tMODELS\tSTATE")
	for _, p := range cfg.Providers {
		state := "on"
		if p.Off {
			state = "off"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", p.ID, p.Protocol, p.BaseURL, len(p.Models), state)
	}
	w.Flush()
}

func runGroups() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}
	for _, g := range cfg.Groups {
		fmt.Printf("Group: group/%s (%s) [strategy=%s]\n", g.ID, g.Name, g.Strategy)
		fmt.Printf("  Members : %s\n", strings.Join(g.Members, " -> "))
		for _, r := range g.Rules {
			fmt.Printf("  Rule [%s]: compact=%v subagent=%v -> %s\n",
				r.Name, r.WhenCompact, r.WhenSubagent, strings.Join(r.Use, ", "))
		}
	}
}

func runServe(args []string) {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}
	for i := 0; i < len(args); i++ {
		if args[i] == "--port" && i+1 < len(args) {
			cfg.Server.Listen = "127.0.0.1:" + args[i+1]
			i++
		}
	}
	pool := oauth.NewPool(cfg.Accounts)
	var saveMu sync.Mutex
	pool.SetTokenSaver(func(u oauth.TokenUpdate) {
		saveMu.Lock()
		defer saveMu.Unlock()
		path := config.DefaultConfigPath()
		if err := config.UpdateAccountTokens(path, u.ID, u.AccessToken, u.RefreshToken, u.ExpiresAt); err != nil {
			log.Printf("[pear] saving refreshed token for %s: %v", u.ID, err)
		}
	})
	refreshCtx, stopRefresh := context.WithCancel(context.Background())
	defer stopRefresh()
	pool.StartRefresher(refreshCtx)

	engine := router.NewEngine(cfg, pool)
	srv := gateway.NewServer(cfg, pool, engine)

	// Header and idle waits are bounded; the response write is not, because a
	// streaming completion is allowed to run as long as the upstream keeps
	// sending bytes.
	ln, err := net.Listen("tcp", cfg.Server.Listen)
	if err != nil {
		log.Fatalf("[Pear] listen %s: %v", cfg.Server.Listen, err)
	}
	if err := config.WriteRuntimeAddr(ln.Addr().String()); err != nil {
		log.Printf("[pear] writing %s: %v", config.RuntimeAddrPath(), err)
	}
	defer os.Remove(config.RuntimeAddrPath())

	fmt.Print(ui.Banner())
	fmt.Printf("%s  %s\n\n",
		ui.Green("▶ gateway listening on"),
		ui.Accent("http://"+ln.Addr().String()),
	)
	log.Printf("[Pear] Starting gateway on http://%s (%d providers, %d OAuth accounts)...",
		ln.Addr().String(), len(cfg.Providers), len(cfg.Accounts))

	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		stopRefresh()
		httpSrv.Close()
	}()
	if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[Pear] Server stopped: %v", err)
	}
}
