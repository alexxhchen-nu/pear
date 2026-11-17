<p align="center">
  <img src="assets/readme/pear.svg" alt="pear" width="72">
</p>

# pear

**English** · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md)

[![release](https://img.shields.io/github/v/release/alexxhchen-nu/pear)](https://github.com/alexxhchen-nu/pear/releases)
[![license](https://img.shields.io/github/license/alexxhchen-nu/pear)](LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/alexxhchen-nu/pear)](go.mod)

pear is a local gateway for model APIs. The published binary is one static executable (`CGO_ENABLED=0`). Point Claude Code, Codex, Cursor, or any other OpenAI or Anthropic client at `127.0.0.1:3426`. pear reads `~/.pear/pear.json` and forwards each request to an upstream you listed there.

Use it when you already have API keys, or Codex, GitHub Copilot, or Claude subscription tokens, and you want one localhost address and an ordered failover list. pear rewrites `model` to the chosen upstream model. When the upstream protocol is not `anthropic` or `oauth-claude`, it also drops Anthropic thinking signatures and `cache_control`. A different protocol is translated only for a non-streaming plain-text chat. The gateway itself is standard-library Go. The only direct module requirement, `go.uber.org/goleak`, is imported by tests.

## Install

Install a release with [mise](https://mise.jdx.dev). This does not clone the repo and does not install Go. Your shell needs `mise activate` so the shim is on `PATH`.

```bash
mise use -g github:alexxhchen-nu/pear@0.1.5
pear version
```

That prints `pear v0.1.5 (Pear AI Gateway & OAuth Router)`.

`0.1.5` is the latest release tag at the time of this file. An exact tag installs immediately. `@latest` skips tags newer than `minimum_release_age` (24 hours unless you change it), so the day of a release `@latest` can still select an older tag.

Build from a clone when you want a branch tip. `mise install` takes the Go version from `mise.toml`. The version string stays `dev`, and this task does not set `CGO_ENABLED=0`:

```bash
git clone https://github.com/alexxhchen-nu/pear.git
cd pear
mise install
mise run build
./pear version
```

`./pear version` prints `pear vdev (Pear AI Gateway & OAuth Router)`.

`go install` needs Go 1.24 or newer and also prints `dev`, because only the release build injects the tag:

```bash
go install github.com/alexxhchen-nu/pear/cmd/pear@v0.1.5
```

You can also download an archive from the [v0.1.5 release](https://github.com/alexxhchen-nu/pear/releases/tag/v0.1.5). Assets exist for linux, darwin, and windows, on amd64 and arm64. Linux files are `.tar.gz`. macOS and Windows files are `.zip`. Names look like `pear_0.1.5_linux_amd64.tar.gz` and `pear_0.1.5_darwin_arm64.zip`. The `pear` binary sits next to this README inside the archive. Check the download against `checksums.txt` from the same release:

```bash
sha256sum -c checksums.txt --ignore-missing
```

## Quickstart

```bash
mkdir -p ~/.pear
cat > ~/.pear/pear.json << 'EOF'
{
  "server": {
    "listen": "127.0.0.1:3426",
    "default_target": "group/default"
  },
  "providers": [
    {
      "id": "openai",
      "name": "OpenAI",
      "protocol": "openai",
      "base_url": "https://api.openai.com/v1",
      "api_key": "sk-replace-me",
      "models": ["gpt-4o-mini"]
    }
  ],
  "groups": [
    {
      "id": "default",
      "name": "default",
      "members": ["openai/gpt-4o-mini"]
    }
  ]
}
EOF
```

Put a real key in `api_key`, and a model id that key can call in both `models` and `members`. Start the gateway:

```bash
pear serve
```

`pear serve` reads the file once. Restart it after edits. `pear serve --port 8080` listens on `127.0.0.1:8080` and replaces `server.listen`. `pear daemon` runs the same process.

In another shell:

```bash
curl -sS -D - http://127.0.0.1:3426/v1/chat/completions \
  -H 'content-type: application/json' \
  -d '{"model":"group/default","messages":[{"role":"user","content":"ping"}]}'
```

The upstream body is the response body. pear adds `X-Pear-Routed-Provider: openai` and `X-Pear-Routed-Model: gpt-4o-mini`, and the JSON it sends upstream has `"model":"gpt-4o-mini"`. An empty `model`, or the values `auto` and `default`, uses `default_target`. `GET /health` returns `"status":"ok"`. `GET /v1/models` lists `group/default` and `openai/gpt-4o-mini`.

Point clients at pear:

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:3426
export OPENAI_BASE_URL=http://127.0.0.1:3426/v1
```

Anthropic clients call `/v1/messages` on that host. OpenAI clients append `/chat/completions` to a base that already includes `/v1`.

`PEAR_CONFIG_DIR` selects another directory. A leading `~/` is expanded. The file name stays `pear.json`.

```bash
PEAR_CONFIG_DIR=~/pear-profiles/work pear status
```

If `pear.json` is missing, pear uses listen `127.0.0.1:3426`, default target `group/smart-split`, a 12000 ms first-byte budget, a 60 minute sticky window, and no providers.

## Protocols

The listener accepts three POST paths. When the client path and the upstream wire format are the same, pear forwards that path. When they differ, pear translates a non-streaming plain-text conversation: system, user, and assistant strings, plus `max_tokens` and `temperature`. The translated request goes to the upstream path, not the client path. Streaming, tools, and images across protocols return `translation_unsupported` and are not forwarded.

Wire format: `openai` and `oauth-copilot` are Chat Completions. `anthropic` and `oauth-claude` are Messages. `responses` and `oauth-codex` are Responses.

| Path | Client API |
| --- | --- |
| `/v1/chat/completions` | OpenAI Chat Completions |
| `/v1/messages` | Anthropic Messages |
| `/v1/responses` | OpenAI Responses |

`protocol` chooses credentials and cleanup:

- `openai` and `responses` send `Authorization: Bearer` with `api_key`.
- `anthropic` sends `x-api-key` and `Authorization: Bearer`. If the client omitted `Anthropic-Version`, pear sets `2023-06-01`. `oauth-claude` gets that same version header.
- `oauth-codex`, `oauth-copilot`, and `oauth-claude` send a bearer token from an account. See [OAuth accounts](#oauth-accounts).

When `base_url` ends in `/v1` and the request path starts with `/v1/`, pear joins them without a second `/v1`. `https://api.openai.com/v1` and `/v1/chat/completions` become `https://api.openai.com/v1/chat/completions`.

`insecure_tls: true` skips TLS verification for that provider.

## Routing groups

The requested `model` selects the target. `group/<id>` runs that group. `provider/model` selects that provider even when `model` is absent from `models`. A bare name such as `gpt-4o-mini` uses the first enabled provider whose `models` list contains it.

In a group, the first matching rule replaces `members` with its `use` list. Otherwise `strategy` orders `members`:

- `order`, and an empty or unknown strategy: written order.
- `round_robin`: each new session starts at the next member, then the rest.
- `session_sticky`: the session hash picks the starting member. The same session keeps that start.
- `smart_pace`: a member with an OAuth account is ordered by remaining quota. More remaining, and a sooner reset, comes first. A member with no account keeps its place among equal scores.

Entries may be `provider/model`, `group/<id>` (nested up to four levels), or a bare model name.

After a successful response, pear hashes the first 768 bytes of the body with FNV-64a and keeps the winning `provider/model` for `sticky_ttl_min` minutes. Default is 60, and `0` also means 60. A later request with the same prefix moves that target to the front when it is still a candidate.

```json
{
  "id": "default",
  "name": "default",
  "members": ["openai/gpt-4o-mini", "backup/gpt-4o-mini"],
  "rules": [
    {
      "name": "has-image",
      "when_has_image": true,
      "use": ["backup/gpt-4o-mini"]
    }
  ]
}
```

Each rule field is a requirement. Leaving it out, or setting a bool to false, does not filter those requests out. A rule with no requirement set never matches.

- `when_has_image` matches a body that contains `"image"` or `"image_url"`.
- `min_tokens` compares against `len(body)/4`. That is a size check, not a tokenizer count.
- `time_range` is local time as `HH:MM-HH:MM`. When the start is after the end, the range crosses midnight.
- `folder_contains` is a case-insensitive substring of the `X-Project-Dir` header. The working directory is not read.
- `when_compact` matches a body that contains `Summarize the conversation`, `<context-compaction>`, or `compact the conversation`.
- `when_subagent` matches a body that contains `read-only search agent` or `Generate a concise title`.

If `server.compact_target` or `server.subagent_target` is set, that value replaces the model before group rules run. If no candidate resolves, and some enabled provider lists a model, pear uses that provider's first model.

## OAuth accounts

Use protocol `oauth-codex`, `oauth-copilot`, or `oauth-claude`, and leave `api_key` empty. pear then looks for an enabled account whose `agent` contains the provider `id`. A missing `enabled` is false. If no enabled account matches, pear still forwards to that provider and sends no bearer token.

```json
{
  "server": {
    "default_target": "codex/your-model"
  },
  "providers": [
    {
      "id": "codex",
      "name": "Codex",
      "protocol": "oauth-codex",
      "base_url": "https://api.example.com/v1",
      "models": ["your-model"]
    }
  ],
  "accounts": [
    {
      "id": "work",
      "agent": "codex",
      "user": "you@example.com",
      "enabled": true,
      "access_token": "replace-me",
      "refresh_token": "replace-me",
      "expires_at": 1764000000
    }
  ]
}
```

Set `base_url` to that product's API. `expires_at` is Unix seconds. An agent of `plugin:github-copilot` matches a provider id of `copilot` or `github-copilot`.

While `pear serve` is running, it refreshes tokens about two minutes before they expire, and again after a 401. A failed refresh keeps a token that is still valid and tries again on the next pass. An expired token whose refresh fails is not sent. `expires_at` is Unix seconds. A millisecond value is accepted and stored back as seconds.

- Agent `codex` (an optional `plugin:` prefix is ignored) posts `refresh_token` to `https://auth.openai.com/oauth/token`. A new access token, and a new refresh token when the server sends one, are written back to `pear.json`.
- An agent that is `claude` or starts with `claude` posts to `https://platform.claude.com/v1/oauth/token`. If that host is missing or unreachable, pear tries `https://console.anthropic.com/v1/oauth/token`. Rotated tokens are written back to `pear.json`.
- An agent containing `github-copilot` exchanges `access_token` at `https://api.github.com/copilot_internal/v2/token`. The short-lived bearer stays in memory and is not written to `pear.json`.
- Any other agent sends `access_token` and is not refreshed.

A 401 on an account clears the cached bearer and retries that account once. Codex and Claude refresh, Copilot exchanges, and other agents send the same access token again.

Among matching accounts, a higher remaining quota scores better, and a sooner reset scores better. Accounts in cooldown are skipped. Before any window is recorded, every match scores the same. Windows are updated only inside `pear serve`, and only from Anthropic's unified 5-hour and 7-day utilization headers. `GET /v1/pear/quotas` returns that process's snapshot, with access and refresh tokens masked and `extra` removed. `pear quota` reads that endpoint on the running gateway. It uses `gateway.addr` in the config directory when `pear serve` has written it, otherwise `server.listen`. `--url` overrides both. The command fails if nothing is listening. `pear serve` removes `gateway.addr` when the process exits.

A 403 cools that account down for `Retry-After` seconds, or 15 minutes. A 429 or 529 cools it down for `Retry-After` seconds, or 30 seconds.

## Config reference

Omitted `server` fields keep the defaults from the missing-file case above. `pear status`, `pear providers`, and `pear groups` read the file on every run.

| Key | Role |
| --- | --- |
| `server.listen` | Bind address. Default `127.0.0.1:3426`. |
| `server.default_target` | Used when the model is empty, `auto`, or `default`. |
| `server.compact_target` | Replaces the model on a compaction request. |
| `server.subagent_target` | Replaces the model on a sub-agent request. |
| `server.ttft_timeout_ms` | First upstream byte. Default 12000. `0` means 12000. |
| `server.sticky_ttl_min` | Sticky lifetime in minutes. Default 60. `0` means 60. |
| `providers[].id`, `name`, `protocol`, `base_url`, `api_key` | Upstream identity, URL, and credential. |
| `providers[].models` | Shown by `GET /v1/models`. Also resolves a bare model name. |
| `providers[].off` | `true` skips the provider. |
| `providers[].insecure_tls` | Skip TLS verification. |
| `accounts[].id`, `agent`, `user`, `enabled` | `enabled` must be true or the account is skipped. |
| `accounts[].plan` | Printed by `pear quota`. Routing does not read it. |
| `accounts[].access_token`, `refresh_token`, `expires_at` | Credentials. `expires_at` is Unix seconds, or milliseconds. |
| `accounts[].extra` | Kept in the file. Dropped from quota snapshots. |
| `groups[].id`, `name`, `members`, `rules` | The group the router actually walks. |
| `groups[].strategy` | `order`, `round_robin`, `session_sticky`, or `smart_pace`. Empty or unknown means `order`. |
| `groups[].classifier` | Accepted and ignored. |

## CLI

```text
pear serve [--port N]    Listen. --port forces 127.0.0.1:N
pear daemon [--port N]   Same process as serve
pear status              Config path, listen address, counts
pear providers           id, protocol, base URL, model count, on or off
pear groups              Members in order, then each rule
pear quota [--json] [--url URL]
                         Live windows from the running gateway
pear version             Release tag, or dev
```

`pear` with no arguments prints the banner and this usage text.

## HTTP

`GET /health` reports status, provider count, account count, and group count. `GET /v1/models` lists each group as `group/<id>` and each advertised model as `<provider>/<model>`. `GET /v1/pear/quotas` is the live snapshot. POST routes are `/v1/chat/completions`, `/v1/messages`, and `/v1/responses`. Any other method on those routes returns 405.

Errors pear generates are JSON. OpenAI clients read `error.type` and `error.message`. Anthropic clients also read the top-level `type` of `error`.

```json
{"type":"error","message":"...","error":{"type":"...","message":"..."}}
```

For one request, candidates run in order:

1. A 401 on an OAuth account retries that account once, as described above. A second 401 moves on. A 401 with no account moves on immediately.
2. 403, 404, 429, and any 5xx try the next candidate. 403, 429, and 529 also start the cooldowns above.
3. If no body byte arrives within `ttft_timeout_ms`, pear tries the next candidate. Nothing has been written to the client yet.
4. After the first byte is written, pear stays on that candidate. Ten minutes of silence ends the stream. Bytes that keep arriving have no total deadline. Upstream response headers must arrive within 90 seconds.
5. A body over 32 MiB returns 413 and is not forwarded.

The client disconnecting stops the attempt. pear does not use that as a signal to try another upstream.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). From a clone, `mise run check` runs `go vet ./...` and `go build ./...`.

## License

[MIT](LICENSE). Copyright 2026 alexxhchen-nu.
