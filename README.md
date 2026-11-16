<!-- readme-theme: phosphor-archive / archetype-b / rail-74 -->
<div align="center">

```text
                   ███████ ███████ ███████ ███████ ████
                  ██   ██ ██      ██   ██ ██   ██ ██  █
                  ███████ █████   ███████ ███████ █████
                  ██      ██      ██   ██ ██   ██ ██  █
                  ██      ███████ ██   ██ ██   ██ ██   █
```

**P E A R   G A T E W A Y**

**[English](README.md)** · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md)

[![Go](https://img.shields.io/badge/go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![deps](https://img.shields.io/badge/third--party%20deps-0-3FB950)](go.mod)
[![binary](https://img.shields.io/badge/binary-7.1%20MB-3FB950)](https://github.com/alexxhchen-nu/pear/releases)
[![license](https://img.shields.io/badge/license-MIT-3FB950)](LICENSE)
![platform](https://img.shields.io/badge/os-windows%20%7C%20linux%20%7C%20macos-8b949e)

</div>

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                        █▓▒░  ◆  OVERVIEW  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

Pear is a single-binary local AI gateway. It accepts Anthropic, OpenAI and
Responses wire formats on one port, routes each request to a declared
provider or a load-balanced group, and keeps OAuth subscription accounts
alive with automatic token refresh.

It exists because desktop AI gateways grew into apps: embedded webviews,
bundled databases, plugin VMs. Pear is the same routing capability with
**zero third-party Go dependencies** — `go.mod` has no `require` block — so
the whole surface is `net/http` plus `crypto/tls`.

```text
+- WHAT YOU GET ---------------------------------------------------------+
| single config  ·  ~/.pear/pear.json  ·  atomic write  ·  mode 0600     |
| inbound  /v1/messages  ·  /v1/responses  ·  /v1/chat/…                 |
| upstream  openai  ·  anthropic  ·  responses  ·  any base URL          |
| oauth pool  codex  ·  copilot  ·  claude  ·  token refresh             |
| groups  smart_pace  ·  session_sticky  ·  order  ·  round_robin        |
| rules  compact · subagent · image · min_tokens · time_range · folder   |
| sticky  FNV-64a of session id  ·  60 min TTL  ·  drift only on 429/5xx |
| quota  /v1/pear/quotas  ·  per-account windows  ·  reset times         |
+------------------------------------------------------------------------+
```

Why it stays small:

- **One file, one truth.** Configuration is `~/.pear/pear.json` on every
  platform. Writes go through `.tmp` + `rename`, so a crash never leaves a
  half-written config.
- **No lock files.** Concurrent readers never take a lock; the process reads
  the config once at boot and re-reads on demand.
- **No plugin VM.** OAuth refresh, routing and protocol translation are
  ordinary Go code inside `internal/`.
- **Boring transport.** HTTP/1.1, SSE passthrough, one goroutine per request.

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                        █▓▒░  ◆  INSTALL  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

Pick one. All three produce the same single executable.

**1 · Prebuilt binary** (Windows / Linux / macOS, amd64 + arm64) — once the
first `v*` tag is pushed, files appear under
[Releases](https://github.com/alexxhchen-nu/pear/releases). Every archive is
covered by `checksums.txt`; verify before running:

```bash
sha256sum -c checksums.txt --ignore-missing -i   # linux
shasum -a 256 pear_darwin_arm64.tar.gz          # macOS
```

**2 · `go install`** (needs Go 1.24+; same caveat — the module must be tagged):

```bash
go install github.com/alexxhchen-nu/pear/cmd/pear@latest
```

**3 · From source with mise** (works today, no tag required):

```bash
git clone https://github.com/alexxhchen-nu/pear.git
cd pear
mise install          # Go toolchain from mise.toml
mise run build        # -> ./pear(.exe); `pear version` reports "dev"
mise run release:check   # goreleaser config is valid
mise run snapshot     # stamped cross-build into dist/, nothing published
```

Package managers (scoop, Homebrew, AUR, winget) are **not published yet** —
see the roadmap below.

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                      █▓▒░  ◆  QUICK START  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

```bash
pear status            # confirm what got loaded: providers, accounts, groups
pear providers        # confirm what was loaded
pear serve --port 3426
```

Then point clients at the gateway — either the Anthropic-native or the
OpenAI-native base URL:

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:3426
export OPENAI_BASE_URL=http://127.0.0.1:3426/v1
```

Sanity checks:

```bash
curl -s http://127.0.0.1:3426/health
curl -s http://127.0.0.1:3426/v1/models | head
```

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                     █▓▒░  ◆  CONFIG SCHEMA  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

`~/.pear/pear.json` — the entire configuration, four top-level keys.

| Key | Type | Meaning |
| :-- | :-- | :-- |
| `server.listen` | string | bind address, default `127.0.0.1:3426` |
| `server.default_target` | string | fallback route when no rule matches |
| `server.compact_target` | string | route used when the request is a compaction call |
| `server.subagent_target` | string | route used for sub-agent traffic |
| `server.ttft_timeout_ms` | int | time-to-first-token budget before failover |
| `server.sticky_ttl_min` | int | session stickiness window, default 60 |
| `providers[]` | array | `id`, `name`, `protocol`, `base_url`, `api_key`, optional `models`, `off` |
| `accounts[]` | array | OAuth subscriptions: `agent`, `user`, `access_token`, `refresh_token`, `expires_at` |
| `groups[]` | array | `id`, `strategy`, `members`, optional `classifier`, `rules` |

`providers[].protocol` accepts `openai`, `anthropic`, `responses`,
`oauth-codex`, `oauth-copilot`, `oauth-claude`.
`groups[].strategy` accepts `smart_pace`, `session_sticky`, `order`,
`round_robin`.

A rule matches on request shape, not on the prompt text:

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

`PEAR_CONFIG_DIR` overrides the config directory (handy for tests and for
keeping several profiles side by side):

```bash
PEAR_CONFIG_DIR=~/pear-profiles/work pear status
```

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                          █▓▒░  ◆  CLI  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

| Command | What it does |
| :-- | :-- |
| `pear serve [--port N]` | run the gateway in the foreground |
| `pear daemon` | same as `serve` |
| `pear status` | config path, provider/group counts, listen address |
| `pear providers` | list providers and their protocol |
| `pear groups` | list groups, strategy and members |
| `pear quota` / `quotas` | query the live quota endpoint of a running gateway |
| `pear version` | print the version stamped at build time |

HTTP surface:

| Path | Purpose |
| :-- | :-- |
| `POST /v1/messages` | Anthropic messages format |
| `POST /v1/chat/completions` | OpenAI chat format |
| `POST /v1/responses` | OpenAI Responses format |
| `GET /v1/models` | aggregated model list |
| `GET /v1/pear/quotas` | per-account quota snapshot |
| `GET /health` | liveness |

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                     █▓▒░  ◆  ROUTING LOGIC  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

Two decisions per request, nothing else:

```text
              +--------------------------------------------+
              | inbound request  ·  :3426                  |
              | /v1/messages · /v1/responses               |
              +----------------------+---------------------+
                                     |
                                     ▼
              +--------------------------------------------+
              | sniff: model · tokens · image · folder     |
              | flags: compact · subagent · time window    |
              +----------------------+---------------------+
                                     |
                                     ▼
              +--------------------------------------------+
              | ▓ GATE 1  route rule matched?              |
              +-----+---------------------------------+----+
                yes |                                 | no
                    ▼                                 ▼
      +---------------------------+     +---------------------------+
      | rule target list          |     | group strategy            |
      | (ordered, may nest)       |     | smart_pace · sticky · rr  |
      +---------------------------+     +---------------------------+
                    |                                 |
                    +----------------+----------------+
                                     ▼
              +--------------------------------------------+
              | ▓ GATE 2  candidate usable?  (429 / 5xx)   |
              +-----+---------------------------------+----+
                yes |                                 | no
                    ▼                                 ▼
      +---------------------------+     +---------------------------+
      | rosetta sanitize + SSE    |     | next candidate in list    |
      | record sticky hash 60m    |     | retry until empty         |
      +---------------------------+     +---------------------------+
```

`GATE 1` picks the target list from the matched rule or the group strategy.
`GATE 2` walks candidates in order until one accepts the request. A session
id hashes (FNV-64a) onto a preferred member, so consecutive turns of one
conversation stay on the same upstream account — which keeps the upstream
prompt cache warm. The hash only changes when the candidate answers 429 or
5xx, and the new choice is recorded for the rest of the TTL.

Before forwarding, `internal/rosetta` normalises the body: when a request
originating from Anthropic fails over to a non-Anthropic provider,
`thinking.signature` and `cache_control` are stripped, because upstreams
reject them with `400`.

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                    █▓▒░  ◆  QUOTAS & STATUS  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

```text
▰▰ [1] BOOT  ·  load config  ·  validate providers  ·  bind 127.0.0.1:3426
   │
▰▰ [2] SERVE  ·  route  ·  upstream  ·  SSE passthrough  ·  sticky record
   │
▰▰ [3] DRIFT  ·  429 / 5xx  ·  drop candidate  ·  rehash  ·  retry bounded
   │
▰▰ [4] REFRESH  ·  oauth expiry  ·  rotate token  ·  persist
```

Quota data comes from two places: whatever the provider returns in
rate-limit headers, and the OAuth token state Pear tracks itself. Both are
exposed read-only — Pear never spends a subscription silently.

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                   █▓▒░  ◆  ROADMAP & SUPPORT  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

| Area | State |
| :-- | :-- |
| Single binary, all three protocols | done |
| OAuth refresh (codex, copilot, claude) | done |
| Session stickiness + rosetta sanitizer | done |
| Quota endpoint | done |
| First tagged release + checksums | next |
| `scoop` / Homebrew / winget manifests | planned |
| Per-user config profiles in the CLI | planned |
| Usage accounting / local telemetry | under evaluation |

Contributions: open an issue with the request shape and the providers
involved. Bug reports that include `pear status` and the failing
`/v1/models` output are actionable in one pass.

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                        █▓▒░  ◆  LICENSE  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

MIT — see [LICENSE](LICENSE).

```text
▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄
█ P E A R   ·   single binary   ·   zero third-party deps                █
█ source  github.com/alexxhchen-nu/pear   license  MIT                   █
▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
```
