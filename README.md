<!-- readme-theme: phosphor-archive / archetype-c / hero-svg -->
<div align="center">

<img src="assets/readme/hero.svg" alt="pear — local AI gateway" width="860">

**One binary. One config file. Every AI client on one port.**

**[English](README.md)** · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md)

[![Go](https://img.shields.io/badge/go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![deps](https://img.shields.io/badge/third--party%20deps-0-3FB950)](go.mod)
[![license](https://img.shields.io/badge/license-MIT-3FB950)](LICENSE)
[![os](https://img.shields.io/badge/windows%20%C2%B7%20linux%20%C2%B7%20macos-amd64%20%C2%B7%20arm64-8b949e)](#install)

</div>

## 🍐 What pear is

pear is a gateway that runs on your machine. Point Claude Code, Codex, Cursor,
an SDK or a `curl` call at `127.0.0.1:3426` and pear decides which upstream
account serves the request.

- **Three wire formats, one port** — Anthropic Messages, OpenAI Chat
  Completions and OpenAI Responses all land on the same listener and are
  translated on the way out.
- **Many providers, one URL** — each provider in `pear.json` gets its own base
  URL, key and protocol. Groups bundle several providers behind one model name.
- **Subscriptions stay alive** — OAuth accounts (Codex, Copilot, Claude) are
  refreshed in the background, so a CLI that idles overnight still works.
- **Nothing to install** — no database, no webview, no plugin VM. One file
  you can copy, plus `~/.pear/pear.json`.

## ✨ Why it stays small

- **One file, one truth.** Configuration is `~/.pear/pear.json` on every
  platform. Writes go through a temp file + rename, so a crash never leaves a
  half-written config behind.
- **No lock files.** Readers never take a lock; the config is read at boot and
  re-read on demand.
- **No plugin system.** Routing, quota parsing and protocol translation are
  ordinary Go code in `internal/`.
- **Boring transport.** HTTP/1.1, SSE passthrough, one goroutine per request.

`go.mod` has one requirement, `go.uber.org/goleak`, and only the tests
import it — the binary is still `net/http`, `crypto/tls` and `encoding/json`.

## 📦 Install

Pick one path. All four end with a single executable.

**1 · mise, straight from GitHub Releases** — no clone, no Go toolchain.

```bash
mise use -g "github:alexxhchen-nu/pear@0.1.2"
pear version          # pear v0.1.2
```

> mise hides releases younger than 24 hours (`minimum_release_age`). Pin
> the version as above, or come back tomorrow for `@latest`.

**2 · From source with mise** — works today, no tag needed.

```bash
git clone https://github.com/alexxhchen-nu/pear.git
cd pear
mise install          # Go toolchain from mise.toml
mise run build        # -> ./pear (or pear.exe)
```

**3 · `go install`** — needs Go 1.24+ and a tagged release.

```bash
go install github.com/alexxhchen-nu/pear/cmd/pear@latest
```

**4 · Prebuilt archive** — grab one from
[Releases](https://github.com/alexxhchen-nu/pear/releases) and verify it
against `checksums.txt` first.

```bash
shasum -a 256 pear_darwin_arm64.tar.gz
```

> Package managers (`scoop`, Homebrew, winget, AUR) are not published yet.
> They are listed on the roadmap below, not faked here.

## ⚡ Start it in 30 seconds

```bash
pear status          # what got loaded: providers, accounts, groups
pear serve           # foreground gateway on 127.0.0.1:3426
```

Point a client at it — pear speaks both native base URLs:

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:3426
export OPENAI_BASE_URL=http://127.0.0.1:3426/v1
```

Then confirm it is alive:

```bash
curl -s http://127.0.0.1:3426/health
curl -s http://127.0.0.1:3426/v1/models
```

`pear groups` shows how each group will pick an upstream.

## 🔀 How a request is routed

Two decisions per request. That is the whole system.

```text
inbound  /v1/messages · /v1/responses
   │
   ▼
sniff    model · tokens · image · folder
   │
   ▼
GATE 1   rule matched?
   │        yes -> that rule's target list
   │        no  -> the group strategy
   ▼
GATE 2   candidate healthy?
   │        yes -> forward, stream SSE back
   │        429/5xx -> next candidate
   ▼
sticky   FNV-64a(session) for 60 min
```

- **GATE 1 — routing.** A rule matches on request *shape*: token count, image
  presence, time window, folder, or a compaction / sub-agent call. First match
  wins, otherwise the group strategy runs: `smart_pace`, `session_sticky`,
  `order` or `round_robin`.
- **GATE 2 — health.** Candidates are tried in order until one accepts the
  request. A `429` or `5xx` moves to the next candidate; the failed one is
  skipped for the rest of the window.
- **Sticky sessions.** A session id hashes onto a preferred member, so
  consecutive turns of one conversation hit the same upstream account and keep
  its prompt cache warm.
- **Sanitising.** When an Anthropic request fails over to a non-Anthropic
  upstream, `thinking.signature` and `cache_control` are stripped — upstreams
  otherwise reject the body with `400`.

## ⚙️ Configure it

Everything lives in `~/.pear/pear.json`, four top-level keys.

```json
{
  "server": {
    "listen": "127.0.0.1:3426",
    "default_target": "my-group",
    "sticky_ttl_min": 60
  },
  "providers": [ ... ],
  "accounts": [ ... ],
  "groups": [ ... ]
}
```

- **`server`** — `listen`, `default_target`, `compact_target`,
  `subagent_target`, `ttft_timeout_ms`, `sticky_ttl_min`.
- **`providers[]`** — `id`, `name`, `protocol`, `base_url`, `api_key`, optional
  `models` and `off`. Protocols: `openai`, `anthropic`, `responses`,
  `oauth-codex`, `oauth-copilot`, `oauth-claude`.
- **`accounts[]`** — OAuth subscriptions: `agent`, `user`, `access_token`,
  `refresh_token`, `expires_at`. pear rotates them for you.
- **`groups[]`** — `id`, `strategy`, `members`, optional `classifier` and
  `rules`. Members are `provider/model`, another group, or a bare model name
  that any provider may serve.

A rule describes the request, never the prompt text:

```json
{
  "name": "large-context",
  "min_tokens": 120000,
  "when_has_image": false,
  "time_range": "09:00-18:00",
  "folder_contains": "work/",
  "use": ["long-context", "fallback"]
}
```

Keep several profiles side by side by pointing at another directory:

```bash
PEAR_CONFIG_DIR=~/pear-profiles/work pear status
```

## 🧪 CLI

- `pear serve [--port N]` — run the gateway in the foreground.
- `pear daemon` — same thing, under the name your init system expects.
- `pear status` — config path, counts, listen address.
- `pear providers` — providers and their protocol.
- `pear groups` — groups, strategy, members.
- `pear quota` — quota snapshot from a running gateway.
- `pear version` — build stamp (`dev` for local builds).

## 🌐 HTTP surface

- `POST /v1/messages` — Anthropic Messages.
- `POST /v1/chat/completions` — OpenAI Chat Completions.
- `POST /v1/responses` — OpenAI Responses.
- `GET /v1/models` — aggregated model list.
- `GET /v1/pear/quotas` — per-account quota snapshot.
- `GET /health` — liveness.

Failures are the gateway's job, not the client's: `401` triggers one token
refresh and a single retry, `403` benches the account for `Retry-After`,
`404`/`429`/`5xx` fail over to the next candidate, and an upstream that sends
no first byte within `ttft_timeout_ms` counts as a failed candidate. A request
body above 32 MB is rejected with `413` instead of being truncated. Every
error pear generates is JSON shaped
`{"type":"error","error":{"type":...,"message":...}}`, which both OpenAI and
Anthropic SDKs can parse.

## 🗺️ Roadmap

- ✅ Single binary, three protocols, four strategies.
- ✅ OAuth refresh, session stickiness, rosetta sanitiser.
- ✅ Quota endpoint and `pear quota`.
- 🔜 First tagged release with checksums.
- 🔜 `scoop` · Homebrew · winget · AUR manifests.
- 🔜 Config profiles in the CLI.
- 🧪 Usage accounting and local telemetry.

## 🤝 Contributing

Open an issue with the request shape and the providers involved. A bug report
that includes `pear status` and the failing `/v1/models` output is actionable
in one pass. Pull requests: keep `mise run check` green (vet + build) and
explain the routing case you touched.

## 📄 License

[MIT](LICENSE) © 2026 alexxhchen
