<!-- readme-theme: phosphor-archive / archetype-c / hero-svg -->
<div align="center">

<img src="assets/readme/hero.svg" alt="pear — 本地 AI 閘道" width="860">

**裝一個二進位制，讓所有 AI 客戶端指向 `127.0.0.1:3426`。**

**[English](README.md)** · [簡體中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md)

[![Go](https://img.shields.io/badge/go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![deps](https://img.shields.io/badge/third--party%20deps-0-3FB950)](go.mod)
[![license](https://img.shields.io/badge/license-MIT-3FB950)](LICENSE)
[![os](https://img.shields.io/badge/windows%20%C2%B7%20linux%20%C2%B7%20macos-amd64%20%C2%B7%20arm64-8b949e)](#install)

</div>

## 📦 安裝

選一條路徑，裝完跑 `pear version`。約 1 分鐘。

**1 · 用 mise 從 GitHub Releases 裝** — 不用 clone，不用 Go 工具鏈。

```bash
mise use -g "github:alexxhchen-nu/pear@0.1.5"
pear version          # pear v0.1.5
```

> mise 會隱藏釋出不滿 24 小時的版本（`minimum_release_age`）。
> 照上面顯式寫版本號，或明天再用 `@latest`。

**2 · 用 mise 從原始碼構建** — 任何 commit 都能用，不需要 tag。

```bash
git clone https://github.com/alexxhchen-nu/pear.git
cd pear
mise install          # 由 mise.toml 提供 Go 工具鏈
mise run build        # -> ./pear（或 pear.exe）
```

**3 · `go install`** — 需要 Go 1.24+ 和已打 tag 的釋出。

```bash
go install github.com/alexxhchen-nu/pear/cmd/pear@latest
```

**4 · 預編譯包** — 從
[Releases](https://github.com/alexxhchen-nu/pear/releases) 下載，
先拿 `checksums.txt` 核對。

```bash
shasum -a 256 pear_darwin_arm64.tar.gz
```

> `scoop`、Homebrew、winget、AUR 還沒釋出。它們只寫在下面的路線圖裡，
> 這裡不假裝已經有了。

## ⚡ 啟動

四步，約 30 秒。

1. `pear status` — 看載入了什麼：providers、accounts、groups。
2. `pear serve` — 前景閘道，監聽 `127.0.0.1:3426`。
3. 把客戶端指過來（兩種原生 base URL 都認）：

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:3426
export OPENAI_BASE_URL=http://127.0.0.1:3426/v1
```

4. 確認它活著：

```bash
curl -s http://127.0.0.1:3426/health
curl -s http://127.0.0.1:3426/v1/models
```

`pear groups` 顯示每個 group 會怎麼挑上游。

## 🍐 pear 是什麼

pear 跑在你自己機器上。Claude Code、Codex、Cursor、各種 SDK 或者一條
`curl`，都只跟 `127.0.0.1:3426` 說話，由 pear 決定哪個上游賬號來服務。

- **三種協議，一個埠** — Anthropic Messages、OpenAI Chat Completions、
  OpenAI Responses 共用一個 listener，出口時自動翻譯。
- **多個 provider，一個 URL** — `pear.json` 裡每個 provider 有自己的
  base URL、key 和協議。group 把多個 provider 收在一個模型名後面。
- **訂閱不會斷** — OAuth 賬號（Codex、Copilot、Claude）後臺續期，
  CLI 掛一夜也還能用。
- **不用裝別的東西** — 沒有資料庫，沒有 webview，沒有外掛 VM。
  一個可執行檔案加 `~/.pear/pear.json`。

## ✨ 為什麼它一直很小

- **一個檔案，一個真相。** 所有平臺的配置都是 `~/.pear/pear.json`。
  寫入走臨時檔案加 rename，崩潰不會留下寫了一半的配置。
- **沒有鎖檔案。** 讀取方從不加鎖；啟動時讀一次，需要時再讀。
- **沒有外掛系統。** 路由、額度解析、協議翻譯都是 `internal/` 裡的普通
  Go 程式碼。
- **傳輸層很無聊。** HTTP/1.1、SSE 直通、每請求一個 goroutine。

`go.mod` 只有一個依賴 `go.uber.org/goleak`，且只有測試 import 它 ——
二進位制仍然是 `net/http`、`crypto/tls` 和 `encoding/json`。

## 🔀 請求怎麼路由

每個請求做兩個決定，這就是全部。

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

- **GATE 1 — 路由。** 規則匹配的是請求*形狀*：token 數、是否帶圖、
  時間段、所在目錄，或者 compaction / sub-agent 呼叫。第一條命中就贏，
  沒有命中就走 group 策略：`smart_pace`、`session_sticky`、`order`
  或 `round_robin`。
- **GATE 2 — 健康。** 候選按順序試，直到有一個接下請求。`429` 或 `5xx`
  換下一個；失敗的在整個視窗內跳過。
- **會話粘性。** session id 會雜湊到一個偏好的成員上，同一段對話的連續
  回合打到同一個上游賬號，prompt cache 保持熱的。
- **淨化。** Anthropic 請求轉移到非 Anthropic 上游時，
  `thinking.signature` 和 `cache_control` 會被剝掉 —— 否則上游回 `400`。

## ⚙️ 配置

全部在 `~/.pear/pear.json`，四個頂層鍵。

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

- **`server`** — `listen`、`default_target`、`compact_target`、
  `subagent_target`、`ttft_timeout_ms`、`sticky_ttl_min`。
- **`providers[]`** — `id`、`name`、`protocol`、`base_url`、`api_key`，
  可選 `models` 和 `off`。協議：`openai`、`anthropic`、`responses`、
  `oauth-codex`、`oauth-copilot`、`oauth-claude`。
- **`accounts[]`** — OAuth 訂閱：`agent`、`user`、`access_token`、
  `refresh_token`、`expires_at`。pear 自動輪換。
- **`groups[]`** — `id`、`strategy`、`members`，可選 `classifier` 和
  `rules`。成員可以是 `provider/model`、另一個 group，或者任何 provider
  都能供的裸模型名。

規則描述請求，不描述提示詞：

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

想並排放幾套配置，就指向別的目錄：

```bash
PEAR_CONFIG_DIR=~/pear-profiles/work pear status
```

## 🧪 CLI

- `pear serve [--port N]` / `pear daemon` — 同一個閘道，前景或在
  init 系統期望的名字下。
- `pear status` — 配置路徑、計數、監聽地址。
- `pear providers` / `pear groups` — 協議、策略、成員。
- `pear quota` — 向執行中的閘道取額度快照。
- `pear version` — 構建標記（本地構建是 `dev`）。

## 🌐 HTTP 介面

- `POST /v1/messages` — Anthropic Messages。
- `POST /v1/chat/completions` — OpenAI Chat Completions。
- `POST /v1/responses` — OpenAI Responses。
- `GET /v1/models` / `GET /v1/pear/quotas` — 模型列表、額度快照。
- `GET /health` — 存活檢查。

## 🩺 出事的時候

失敗由閘道負責，不推給客戶端。

1. `401` → 重新整理一次 token，再重試一次。
2. `403` → 該賬號按 `Retry-After` 冷卻。
3. `404` / `429` / `5xx` → 換下一個候選；失敗的在整個視窗內跳過。
4. `ttft_timeout_ms` 內沒收到上游第一個位元組 → 算失敗候選，換下一個。
5. 請求體超過 32 MB → `413`，絕不截斷。

pear 自己產生的錯誤一律是 JSON：
`{"type":"error","error":{"type":...,"message":...}}`，
OpenAI 和 Anthropic 的 SDK 都能解析。沒有總響應期限：一直在出位元組的流
會跑到上游自己結束；靜默十分鐘的流會被關閉，呼叫方不會無限等。

## 🗺️ 路線圖

- ✅ 單二進位制、三種協議、四種策略。
- ✅ OAuth 續期、會話粘性、rosetta 淨化。
- ✅ 額度介面、`pear quota`、帶檢查碼的 tag 釋出。
- 🔜 `scoop` · Homebrew · winget · AUR 清單。
- 🔜 CLI 裡的配置檔。
- 🧪 用量統計與本地遙測。

## 🤝 參與

開 issue 時帶上請求形狀和涉及的 provider。附上 `pear status` 和失敗的
`/v1/models` 輸出，一輪就能定位。提 PR：保持 `mise run check` 綠色
（vet + build），並說明你動的是哪條路由分支。

## 📄 授權

[MIT](LICENSE) © 2026 alexxhchen
