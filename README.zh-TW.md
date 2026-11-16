<!-- readme-theme: phosphor-archive / archetype-c / hero-svg -->
<div align="center">

<img src="assets/readme/hero.svg" alt="pear — 本地 AI 閘道器" width="860">

**一個二進位檔。一份設定。所有 AI 客戶端共用一個埠。**

[English](README.md) · **[簡體中文](README.zh-CN.md)** · [繁體中文](README.zh-TW.md)

[![Go](https://img.shields.io/badge/go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![deps](https://img.shields.io/badge/third--party%20deps-0-3FB950)](go.mod)
[![license](https://img.shields.io/badge/license-MIT-3FB950)](LICENSE)
[![os](https://img.shields.io/badge/windows%20%C2%B7%20linux%20%C2%B7%20macos-amd64%20%C2%B7%20arm64-8b949e)](#安裝)

</div>

## 🍐 pear 是什麼

pear 是跑在你自己機器上的閘道器。把 Claude Code、Codex、Cursor、各種 SDK 或者一句 `curl` 指向 `127.0.0.1:3426`，剩下的事交給 pear：這次請求由哪個上游帳號來服務。

- **三種協議，一個埠** —— Anthropic Messages、OpenAI Chat Completions、OpenAI Responses 都落在同一個監聽上，出口時自動轉換。
- **多個供應商，一個地址** —— `pear.json` 裡每個 provider 有自己的 base URL、金鑰和協議。group 把幾個 provider 打包成一個模型名對外。
- **訂閱不會睡著** —— Codex、Copilot、Claude 這些 OAuth 帳號在後臺自動重新整理，掛了一夜的 CLI 第二天照樣能用。
- **不用裝任何東西** —— 沒有資料庫、沒有內嵌瀏覽器、沒有外掛虛擬機器。一個可以直接複製的可執行檔案，加一份 `~/.pear/pear.json`。

## ✨ 為什麼它這麼小

- **一個檔案就是全部真相。** 所有平臺都用 `~/.pear/pear.json`。寫入走臨時檔案加 rename，崩潰也不會留下寫壞的設定。
- **沒有鎖檔案。** 讀設定從不加鎖：啟動時讀一次，之後按需重讀。
- **沒有外掛系統。** 路由、配額解析、協議轉換都是 `internal/` 裡的普通 Go 程式碼。
- **傳輸層保持無聊。** HTTP/1.1、SSE 透傳、每個請求一個 goroutine。

`go.mod` 裡沒有 `require` 塊 —— 整個專案只用了標準庫的 `net/http`、`crypto/tls` 和 `encoding/json`。

## 📦 安裝

三條路選一條，結果都是同一個可執行檔案。

**1 · 用 mise 從原始碼建置** —— 現在就能用，不用等 release。

```bash
git clone https://github.com/alexxhchen-nu/pear.git
cd pear
mise install          # 按 mise.toml 裝 Go 工具鏈
mise run build        # -> ./pear（Windows 上是 pear.exe）
```

**2 · `go install`** —— 需要 Go 1.24+，以及已經打好的 tag。

```bash
go install github.com/alexxhchen-nu/pear/cmd/pear@latest
```

**3 · 下載預編譯包** —— 去 [Releases](https://github.com/alexxhchen-nu/pear/releases) 取一個，先用 `checksums.txt` 驗證。

```bash
shasum -a 256 pear_darwin_arm64.tar.gz
```

> 套件管理器（`scoop`、Homebrew、winget、AUR）還沒釋出。它們在下面的路線圖裡，這裡不寫假裝能用的命令。

## ⚡ 30 秒跑起來

```bash
pear status          # 看載入了什麼：providers、accounts、groups
pear serve           # 前臺啟動，監聽 127.0.0.1:3426
```

把客戶端指過來 —— pear 同時提供兩種原生 base URL：

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:3426
export OPENAI_BASE_URL=http://127.0.0.1:3426/v1
```

確認它活著：

```bash
curl -s http://127.0.0.1:3426/health
curl -s http://127.0.0.1:3426/v1/models
```

`pear groups` 可以看清每個 group 會怎麼挑上游。

## 🔀 請求是怎麼被路由的

每個請求只做兩個決定，整個系統就這些。

```text
in     /v1/messages · /v1/responses
  │
  ▼
sniff  model · tokens · image · folder
  │
  ▼
GATE 1 rule matched?
  │      yes -> that rule's target list
  │      no  -> the group strategy
  ▼
GATE 2 candidate healthy?
  │      yes -> forward, stream SSE back
  │      429/5xx -> next candidate
  ▼
sticky FNV-64a(session), 60 min
```

- **關卡 1 —— 路由。** 規則只看請求的形狀：token 數、有沒有圖片、時間段、目錄，或者是不是壓縮/子代理呼叫。先命中先贏，都不命中就走 group 策略：`smart_pace`、`session_sticky`、`order`、`round_robin`。
- **關卡 2 —— 健康。** 候選按順序試，直到有一個接單。遇到 `429` 或 `5xx` 就換下一個，失敗的候選在視窗期內跳過。
- **粘性會話。** 會話 id 會雜湊到某個固定成員，同一段對話的連續幾輪落在同一個上游帳號上，提示詞快取一直是熱的。
- **請求清洗。** Anthropic 請求切到非 Anthropic 上游時，`thinking.signature` 和 `cache_control` 會被剝掉 —— 否則上游直接 `400`。

## ⚙️ 設定

全部內容都在 `~/.pear/pear.json`，頂層只有四個鍵。

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

- **`server`** —— `listen`、`default_target`、`compact_target`、`subagent_target`、`ttft_timeout_ms`、`sticky_ttl_min`。
- **`providers[]`** —— `id`、`name`、`protocol`、`base_url`、`api_key`，可選 `models` 和 `off`。協議可選 `openai`、`anthropic`、`responses`、`oauth-codex`、`oauth-copilot`、`oauth-claude`。
- **`accounts[]`** —— OAuth 訂閱：`agent`、`user`、`access_token`、`refresh_token`、`expires_at`，pear 負責輪換。
- **`groups[]`** —— `id`、`strategy`、`members`，可選 `classifier` 和 `rules`。成員寫成 `provider/model`、另一個 group，或者任一家都能提供的裸模型名。

規則描述的是請求本身，而不是提示詞內容：

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

想同時保留幾套設定，就把目錄指到別處：

```bash
PEAR_CONFIG_DIR=~/pear-profiles/work pear status
```

## 🧪 命令列

- `pear serve [--port N]` —— 前臺執行閘道器。
- `pear daemon` —— 同一件事，換成 init 系統習慣的名字。
- `pear status` —— 設定路徑、數量統計、監聽地址。
- `pear providers` —— provider 列表和各自協議。
- `pear groups` —— group、策略、成員。
- `pear quota` —— 從執行中的閘道器取配額快照。
- `pear version` —— 建置時寫入的版本（本地建置是 `dev`）。

## 🌐 HTTP 介面

- `POST /v1/messages` —— Anthropic Messages 格式。
- `POST /v1/chat/completions` —— OpenAI Chat Completions 格式。
- `POST /v1/responses` —— OpenAI Responses 格式。
- `GET /v1/models` —— 聚合後的模型列表。
- `GET /v1/pear/quotas` —— 逐帳號配額快照。
- `GET /health` —— 存活檢查。

## 🗺️ 路線圖

- ✅ 單檔案二進位制、三種協議、四種策略。
- ✅ OAuth 重新整理、會話粘性、rosetta 請求清洗。
- ✅ 配額介面和 `pear quota`。
- 🔜 第一個帶驗證和的正式 release。
- 🔜 `scoop` · Homebrew · winget · AUR 清單。
- 🔜 命令列裡的多套設定。
- 🧪 用量統計與本地遙測。

## 🤝 參與

有請求形狀或供應商相關的問題就開 issue。附上 `pear status` 和失敗時的 `/v1/models` 輸出，一次就能定位。提交 PR 請保持 `mise run check`（vet + build）透過，並說明你動了哪條路由分支。

## 📄 許可

[MIT](LICENSE) © 2026 alexxhchen
