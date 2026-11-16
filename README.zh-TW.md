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

[English](README.md) · [简体中文](README.zh-CN.md) · **[繁體中文](README.zh-TW.md)**

[![Go](https://img.shields.io/badge/go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![deps](https://img.shields.io/badge/third--party%20deps-0-3FB950)](go.mod)
[![binary](https://img.shields.io/badge/binary-7.1%20MB-3FB950)](https://github.com/alexxhchen/pear/releases)
[![license](https://img.shields.io/badge/license-MIT-3FB950)](LICENSE)
![platform](https://img.shields.io/badge/os-windows%20%7C%20linux%20%7C%20macos-8b949e)

</div>

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                        █▓▒░  ◆  OVERVIEW  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

Pear 是一支單檔案的本地 AI 閘道。它在同一個連接埠上同時接受 Anthropic、
OpenAI 與 Responses 三種封包格式，依宣告式規則或負載平衡群組把請求轉送到
上游，並用自動續期讓 OAuth 訂閱帳號保持可用。

它會存在，是因為桌面版 AI 閘道長成了「應用程式」——內嵌 WebView、打包資料庫、
外掛虛擬機器。Pear 保留同樣的路由能力，但**沒有任何第三方 Go 相依**：
`go.mod` 裡沒有 `require` 區塊，整個實作就是 `net/http` 加 `crypto/tls`。

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

為什麼它一直維持這個體積：

- **一個檔案就是全部真相。** 所有平台都用 `~/.pear/pear.json`。寫入走
  `.tmp` + `rename`，行程掛掉也不會留下寫一半的設定檔。
- **沒有鎖定檔。** 並行讀取不需要加鎖；行程啟動時讀一次，需要時再讀。
- **沒有外掛虛擬機器。** OAuth 續期、路由、協定轉換都是 `internal/` 裡的普通 Go 程式碼。
- **傳輸層保持無聊。** HTTP/1.1、SSE 直通、每個請求一條 goroutine。

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                        █▓▒░  ◆  INSTALL  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

三種方式產出的都是同一個可執行檔，任選其一。

**1 · 預編譯二進位檔**（Windows / Linux / macOS，amd64 與 arm64）——
推送第一個 `v*` tag 後檔案會出現在
[Releases](https://github.com/alexxhchen/pear/releases)。每個壓縮檔都有
`checksums.txt` 涵蓋，執行前先校驗：

```bash
sha256sum -c checksums.txt --ignore-missing -i   # Linux
shasum -a 256 pear_darwin_arm64.tar.gz          # macOS
```

**2 · `go install`**（需要 Go 1.24+；同樣依賴倉庫已打 tag）：

```bash
go install github.com/alexxhchen/pear/cmd/pear@latest
```

**3 · 用 mise 從原始碼建置**（現在就能用，不需要 tag）：

```bash
git clone https://github.com/alexxhchen/pear.git
cd pear
mise install          # 依 mise.toml 安裝 Go 工具鏈
mise run build        # 產生 ./pear(.exe)，此時 `pear version` 顯示 dev
mise run release:check   # 校驗 goreleaser 設定
mise run snapshot     # 帶版本號交叉建置到 dist/，不發布
```

套件管理器（scoop、Homebrew、AUR、winget）**目前尚未發布**，
見下方路線圖。

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                      █▓▒░  ◆  QUICK START  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

```bash
pear status            # 確認已載入的 provider、帳號與路由組
pear providers        # 確認匯入了什麼
pear serve --port 3426
```

接著讓用戶端指向閘道，使用 Anthropic 原生或 OpenAI 原生任一 base URL：

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:3426
export OPENAI_BASE_URL=http://127.0.0.1:3426/v1
```

自我檢查：

```bash
curl -s http://127.0.0.1:3426/health
curl -s http://127.0.0.1:3426/v1/models | head
```

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                     █▓▒░  ◆  CONFIG SCHEMA  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

`~/.pear/pear.json` —— 全部設定，四個頂層鍵。

| 鍵 | 型別 | 意義 |
| :-- | :-- | :-- |
| `server.listen` | string | 監聽位址，預設 `127.0.0.1:3426` |
| `server.default_target` | string | 沒有規則命中時的備援路由 |
| `server.compact_target` | string | 壓縮（compact）呼叫的路由 |
| `server.subagent_target` | string | 子代理流量的路由 |
| `server.ttft_timeout_ms` | int | 首 token 逾時預算，超過即切換 |
| `server.sticky_ttl_min` | int | 工作階段黏著視窗，預設 60 分鐘 |
| `providers[]` | array | `id`、`name`、`protocol`、`base_url`、`api_key`，可選 `models`、`off` |
| `accounts[]` | array | OAuth 訂閱：`agent`、`user`、`access_token`、`refresh_token`、`expires_at` |
| `groups[]` | array | `id`、`strategy`、`members`，可選 `classifier`、`rules` |

`providers[].protocol` 取值：`openai`、`anthropic`、`responses`、
`oauth-codex`、`oauth-copilot`、`oauth-claude`。
`groups[].strategy` 取值：`smart_pace`、`session_sticky`、`order`、
`round_robin`。

規則只看請求形態，不看提示詞內容：

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

`PEAR_CONFIG_DIR` 可以覆寫設定目錄（方便測試，也方便並排放多套設定）：

```bash
PEAR_CONFIG_DIR=~/pear-profiles/work pear status
```

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                          █▓▒░  ◆  CLI  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

| 指令 | 作用 |
| :-- | :-- |
| `pear serve [--port N]` | 前景執行閘道 |
| `pear daemon` | 等同 `serve` |
| `pear status` | 設定路徑、provider/group 數量、監聽位址 |
| `pear providers` | 列出 provider 及其協定 |
| `pear groups` | 列出 group、策略與成員 |
| `pear quota` / `quotas` | 查詢執行中閘道的即時配額 |
| `pear version` | 印出建置時寫入的版本號 |

HTTP 介面：

| 路徑 | 用途 |
| :-- | :-- |
| `POST /v1/messages` | Anthropic messages 格式 |
| `POST /v1/chat/completions` | OpenAI chat 格式 |
| `POST /v1/responses` | OpenAI Responses 格式 |
| `GET /v1/models` | 彙整後的模型清單 |
| `GET /v1/pear/quotas` | 每帳號配額快照 |
| `GET /health` | 存活探測 |

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                     █▓▒░  ◆  ROUTING LOGIC  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

每個請求只有兩次決策：

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

`GATE 1` 用命中的規則或群組策略決定目標清單；`GATE 2` 依序嘗試候選，
直到某個上游接受請求。工作階段 id 經 FNV-64a 雜湊對應到首選成員，因此同一段
對話的連續輪次會落在同一個上游帳號上——上游的 prompt cache 因此保持命中。
只有候選回傳 429 或 5xx 時雜湊才會漂移，且新選擇會在 TTL 內固定。

轉送前 `internal/rosetta` 會正規化請求本體：來自 Anthropic 的請求故障轉移到
非 Anthropic 上游時，會剝離 `thinking.signature` 與 `cache_control`，
否則上游會以 `400` 拒絕。

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

配額資料來自兩處：上游在限流回應標頭回傳的部分，以及 Pear 自己記錄的
OAuth token 狀態。兩者都只讀暴露——Pear 不會偷偷消耗訂閱額度。

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                   █▓▒░  ◆  ROADMAP & SUPPORT  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

| 範圍 | 狀態 |
| :-- | :-- |
| 單一二進位檔、三種協定 | 已完成 |
| OAuth 續期（codex、copilot、claude） | 已完成 |
| 工作階段黏著 + rosetta 淨化 | 已完成 |
| 配額介面 | 已完成 |
| 首個正式 tag + 校驗碼 | 下一步 |
| `scoop` / Homebrew / winget 清單 | 規劃中 |
| CLI 多設定檔 | 規劃中 |
| 用量統計 / 本地遙測 | 評估中 |

貢獻方式：開 issue 時附上請求形態與涉及的上游。帶 `pear status` 輸出與
出錯的 `/v1/models` 回應的問題回報，通常一輪就能定位。

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                        █▓▒░  ◆  LICENSE  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

MIT —— 見 [LICENSE](LICENSE)。

```text
▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄
█ P E A R   ·   single binary   ·   zero third-party deps                █
█ source  github.com/alexxhchen/pear      license  MIT                   █
▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
```
