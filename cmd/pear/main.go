package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/alexxhchen-nu/pear/internal/config"
	"github.com/alexxhchen-nu/pear/internal/gateway"
	"github.com/alexxhchen-nu/pear/internal/oauth"
	"github.com/alexxhchen-nu/pear/internal/router"
	"github.com/alexxhchen-nu/pear/internal/ui"
)

// version is stamped at build time with
//
//	go build -ldflags "-X main.version=$(git describe --tags --always)"
//
// Local `go build` / `go run` keep "dev"; release binaries carry the tag.
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
  pear quota [--json]      Show OAuth subscription windows, remaining % and reset times
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

func runQuota(args []string) {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}
	pool := oauth.NewPool(cfg.Accounts)
	snaps := pool.Snapshot()

	for _, a := range args {
		if a == "--json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(snaps)
			return
		}
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
	engine := router.NewEngine(cfg, pool)
	srv := gateway.NewServer(cfg, pool, engine)

	fmt.Print(ui.Banner())
	fmt.Printf("%s  %s\n\n",
		ui.Green("▶ gateway listening on"),
		ui.Accent("http://"+cfg.Server.Listen),
	)
	log.Printf("[Pear] Starting gateway on http://%s (%d providers, %d OAuth accounts)...",
		cfg.Server.Listen, len(cfg.Providers), len(cfg.Accounts))
	if err := http.ListenAndServe(cfg.Server.Listen, srv.Handler()); err != nil {
		log.Fatalf("[Pear] Server stopped: %v", err)
	}
}
