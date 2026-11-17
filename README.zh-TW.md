<p align="center">
  <img src="assets/readme/pear.svg" alt="pear" width="72">
</p>

# pear

[English](README.md) · [简体中文](README.zh-CN.md) · **繁體中文**

[![release](https://img.shields.io/github/v/release/alexxhchen-nu/pear)](https://github.com/alexxhchen-nu/pear/releases)
[![license](https://img.shields.io/github/license/alexxhchen-nu/pear)](LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/alexxhchen-nu/pear)](go.mod)

pear 是在你這台電腦上跑的模型 API 閘道。發行的檔案是一支靜態執行檔（`CGO_ENABLED=0`）。把 Claude Code、Codex、Cursor，或其他 OpenAI、Anthropic 用戶端指到 `127.0.0.1:3426`。pear 讀取 `~/.pear/pear.json`，再把每個請求轉到你寫在裡面的上游。

適合已經有 API 金鑰，或是 Codex、GitHub Copilot、Claude 訂閱 token 的人。你要的是一個本機位址，加上一份排好順序的失敗切換名單。pear 會把 `model` 改成選中的上游模型。目標協定不是 `anthropic` 或 `oauth-claude` 時，也會拿掉 Anthropic 的 thinking 簽章和 `cache_control`。協定不同時，只翻譯非串流的純文字對話。閘道程式用的是 Go 標準庫。`go.mod` 裡唯一的直接依賴 `go.uber.org/goleak` 只有測試會引用。

## 安裝

用 [mise](https://mise.jdx.dev) 安裝發行版。不用複製倉庫，也不用安裝 Go。shell 要先 `mise activate`，shim 才會出現在 `PATH` 上。

```bash
mise use -g github:alexxhchen-nu/pear@0.1.5
pear version
```

輸出是 `pear v0.1.5 (Pear AI Gateway & OAuth Router)`。

寫這份說明時，最新的發行標籤是 `0.1.5`。寫明標籤會立刻安裝。`@latest` 會跳過比 `minimum_release_age` 還新的標籤（預設 24 小時），所以發行當天 `@latest` 仍可能選到較舊的標籤。

要跟著某個分支走，就從複製下來的原始碼建置。`mise install` 依照 `mise.toml` 安裝 Go。版本字串維持 `dev`，這個工作也不會設定 `CGO_ENABLED=0`：

```bash
git clone https://github.com/alexxhchen-nu/pear.git
cd pear
mise install
mise run build
./pear version
```

`./pear version` 輸出 `pear vdev (Pear AI Gateway & OAuth Router)`。

`go install` 需要 Go 1.24 或更新的版本，印出來的同樣是 `dev`，因為只有發行建置會寫入標籤：

```bash
go install github.com/alexxhchen-nu/pear/cmd/pear@v0.1.5
```

也可以從 [v0.1.5 發行頁](https://github.com/alexxhchen-nu/pear/releases/tag/v0.1.5) 下載壓縮檔。linux、darwin、windows 都有 amd64 和 arm64。Linux 是 `.tar.gz`，macOS 和 Windows 是 `.zip`。檔名類似 `pear_0.1.5_linux_amd64.tar.gz` 和 `pear_0.1.5_darwin_arm64.zip`。解開之後，`pear` 和這份說明放在一起。用同一發行裡的 `checksums.txt` 核對：

```bash
sha256sum -c checksums.txt --ignore-missing
```

## 快速開始

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

把 `api_key` 換成真正的金鑰。`models` 和 `members` 裡的模型 id 也換成這把金鑰可以呼叫的名字。然後啟動：

```bash
pear serve
```

`pear serve` 只在啟動時讀檔。改完要重新啟動。`pear serve --port 8080` 監聽 `127.0.0.1:8080`，並蓋過 `server.listen`。`pear daemon` 是同一個行程。

另開一個 shell：

```bash
curl -sS -D - http://127.0.0.1:3426/v1/chat/completions \
  -H 'content-type: application/json' \
  -d '{"model":"group/default","messages":[{"role":"user","content":"ping"}]}'
```

回應本文就是上游自己的本文。pear 會加上 `X-Pear-Routed-Provider: openai` 和 `X-Pear-Routed-Model: gpt-4o-mini`，轉出去的 JSON 裡 `"model"` 是 `"gpt-4o-mini"`。`model` 是空的、`auto` 或 `default` 時，走 `default_target`。`GET /health` 裡有 `"status":"ok"`。`GET /v1/models` 會列出 `group/default` 和 `openai/gpt-4o-mini`。

用戶端這樣指過來：

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:3426
export OPENAI_BASE_URL=http://127.0.0.1:3426/v1
```

Anthropic 用戶端在這個主機上存取 `/v1/messages`。OpenAI 用戶端會在已經帶 `/v1` 的 base 後面接 `/chat/completions`。

`PEAR_CONFIG_DIR` 換成別的目錄。開頭的 `~/` 會展開。檔名仍是 `pear.json`。

```bash
PEAR_CONFIG_DIR=~/pear-profiles/work pear status
```

沒有 `pear.json` 時，監聽位址是 `127.0.0.1:3426`，預設目標是 `group/smart-split`，第一個位元組等待 12000 毫秒，黏著視窗 60 分鐘，provider 是空的。

## 協定

監聽端接受三種 POST 路徑。用戶端路徑和上游線路格式相同時，pear 照該路徑轉送。不同時，只翻譯非串流的純文字對話：system、user、assistant 字串，以及 `max_tokens` 和 `temperature`。譯過的請求發到上游自己的路徑，不是用戶端路徑。跨協定的串流、工具和圖片回傳 `translation_unsupported`，不會轉送。

線路格式：`openai` 和 `oauth-copilot` 是 Chat Completions。`anthropic` 和 `oauth-claude` 是 Messages。`responses` 和 `oauth-codex` 是 Responses。

| 路徑 | 用戶端 API |
| --- | --- |
| `/v1/chat/completions` | OpenAI Chat Completions |
| `/v1/messages` | Anthropic Messages |
| `/v1/responses` | OpenAI Responses |

`protocol` 決定憑證和清理方式：

- `openai` 和 `responses` 用 `api_key` 送出 `Authorization: Bearer`。
- `anthropic` 同時送出 `x-api-key` 和 `Authorization: Bearer`。用戶端沒帶 `Anthropic-Version` 時，pear 設成 `2023-06-01`。`oauth-claude` 也會帶上這個版本標頭。
- `oauth-codex`、`oauth-copilot`、`oauth-claude` 使用帳號裡的 bearer token。見 [OAuth 帳號](#oauth-帳號)。

`base_url` 以 `/v1` 結尾，而且請求路徑以 `/v1/` 開頭時，接起來不會多出一個 `/v1`。`https://api.openai.com/v1` 加上 `/v1/chat/completions` 會變成 `https://api.openai.com/v1/chat/completions`。

`insecure_tls: true` 會略過該 provider 的 TLS 憑證檢查。

## 路由組

請求裡的 `model` 決定目標。`group/<id>` 進入該組。`provider/model` 選中該 provider，就算 `models` 裡沒有這個模型也一樣。`gpt-4o-mini` 這種裸名，用第一個啟用中且 `models` 含有它的 provider。

組內依序看 `rules`。第一條命中的規則用它的 `use` 換掉 `members`。都沒命中時，由 `strategy` 排列 `members`：

- `order`，以及空的或未知的策略：照寫好的順序。
- `round_robin`：每個新 session 從下一個成員開始，再走剩下的。
- `session_sticky`：用 session 雜湊選起始成員。同一個 session 保持這個起點。
- `smart_pace`：帶 OAuth 帳號的成員按剩餘額度排序。剩餘越多、重設越近，越靠前。沒有帳號的成員在分數相同時留在原位。

每一項可以是 `provider/model`、`group/<id>`（最多巢狀四層），或裸模型名。

一次成功的回應之後，pear 用 FNV-64a 對本文前 768 個位元組做雜湊，把勝出的 `provider/model` 記住 `sticky_ttl_min` 分鐘。預設 60，寫成 `0` 也是 60。之後前綴相同的請求，如果這個目標還在候選裡，就把它移到最前面。

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

規則裡的每個欄位都是必要條件。省略，或把布林值設成 false，不會反過來排除那些請求。一個條件都沒寫的規則永遠不命中。

- `when_has_image`：本文含有 `"image"` 或 `"image_url"`。
- `min_tokens`：和 `len(body)/4` 比較。這是大小檢查，不是分詞器算出來的 token 數。
- `time_range`：本機時間，格式 `HH:MM-HH:MM`。開始晚於結束時，區間跨過午夜。
- `folder_contains`：對標頭 `X-Project-Dir` 做不分大小寫的子字串比對。不讀行程的工作目錄。
- `when_compact`：本文含有 `Summarize the conversation`、`<context-compaction>` 或 `compact the conversation`。
- `when_subagent`：本文含有 `read-only search agent` 或 `Generate a concise title`。

若設定了 `server.compact_target` 或 `server.subagent_target`，會在組規則之前換掉模型。一個候選都解不出來，而又有啟用的 provider 列出模型時，pear 用該 provider 的第一個模型。

## OAuth 帳號

協定用 `oauth-codex`、`oauth-copilot` 或 `oauth-claude`，並把 `api_key` 留空。pear 會找 `agent` 含有該 provider `id`、而且已啟用的帳號。沒寫 `enabled` 就當成 false。沒有啟用的帳號對上時，pear 仍會把請求轉到這個 provider，而且不帶 bearer token。

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

`base_url` 填該產品自己的 API。`expires_at` 是 Unix 秒。`agent` 為 `plugin:github-copilot` 時，可以對上 id 為 `copilot` 或 `github-copilot` 的 provider。

`pear serve` 執行時，會在到期前大約兩分鐘續期，401 之後也會再續一次。續期失敗時，只要目前 token 還沒過期就繼續用，下一輪再試。已經過期且續期失敗的 token 不會送出去。`expires_at` 是 Unix 秒。毫秒值也能讀，寫回檔案時改成秒。

- `agent` 為 `codex`（可帶 `plugin:` 前綴）時，把 `refresh_token` POST 到 `https://auth.openai.com/oauth/token`。新的 access token，以及伺服器回傳的新 refresh token，會寫回 `pear.json`。
- `agent` 為 `claude` 或以 `claude` 開頭時，POST 到 `https://platform.claude.com/v1/oauth/token`。這個主機不存在或連不上時，再試 `https://console.anthropic.com/v1/oauth/token`。輪換後的 token 寫回 `pear.json`。
- `agent` 含有 `github-copilot` 時，用 `access_token` 向 `https://api.github.com/copilot_internal/v2/token` 換 bearer。這張短期 bearer 只留在記憶體裡，不寫入 `pear.json`。
- 其他 agent 直接送出 `access_token`，不會續期。

帳號遇到 401 時，pear 清掉快取的 bearer，並對該帳號重試一次。Codex 和 Claude 會重新整理，Copilot 會交換，其他 agent 會再送一次原來的 access token。

多個帳號都對上時，剩餘額度較高的分數較高，重設較早的分數也較高。冷卻中的帳號會被跳過。還沒有任何額度區間時，對上的帳號分數相同。區間只在 `pear serve` 行程裡更新，而且只認 Anthropic 統一的 5 小時和 7 天使用率標頭。`GET /v1/pear/quotas` 回傳這個行程的快照，access token 和 refresh token 會被遮住，`extra` 會被拿掉。`pear quota` 讀執行中閘道的這個介面。`pear serve` 寫過設定目錄裡的 `gateway.addr` 時用那個位址，否則用 `server.listen`。`--url` 覆蓋兩者。沒有行程在聽就失敗。`pear serve` 結束時刪掉 `gateway.addr`。

403 會依 `Retry-After` 的秒數冷卻該帳號，沒有這個標頭就冷卻 15 分鐘。429 或 529 依 `Retry-After` 冷卻，沒有就冷卻 30 秒。

## 設定參考

省略的 `server` 欄位維持上面「檔案不存在」時的預設。`pear status`、`pear providers`、`pear groups` 每次執行都會讀檔。

| 鍵 | 作用 |
| --- | --- |
| `server.listen` | 監聽位址。預設 `127.0.0.1:3426`。 |
| `server.default_target` | `model` 為空、`auto` 或 `default` 時使用。 |
| `server.compact_target` | 壓縮請求來時換掉模型。 |
| `server.subagent_target` | 子代理請求來時換掉模型。 |
| `server.ttft_timeout_ms` | 等待上游第一個位元組。預設 12000。`0` 也是 12000。 |
| `server.sticky_ttl_min` | 黏著時間，單位是分鐘。預設 60。`0` 也是 60。 |
| `providers[].id`、`name`、`protocol`、`base_url`、`api_key` | 上游的識別、位址和憑證。 |
| `providers[].models` | 出現在 `GET /v1/models`，也用來解析裸模型名。 |
| `providers[].off` | `true` 時停用該 provider。 |
| `providers[].insecure_tls` | 略過 TLS 檢查。 |
| `accounts[].id`、`agent`、`user`、`enabled` | `enabled` 必須為 true，否則跳過。 |
| `accounts[].plan` | `pear quota` 會印出。路由不讀它。 |
| `accounts[].access_token`、`refresh_token`、`expires_at` | 憑證。`expires_at` 是 Unix 秒，或毫秒。 |
| `accounts[].extra` | 留在檔案裡。快照裡會拿掉。 |
| `groups[].id`、`name`、`members`、`rules` | 路由器真正會走的組。 |
| `groups[].strategy` | `order`、`round_robin`、`session_sticky` 或 `smart_pace`。空的或未知的等於 `order`。 |
| `groups[].classifier` | 接受這個欄位，然後略過。 |

## 命令

```text
pear serve [--port N]    開始監聽。--port 強制使用 127.0.0.1:N
pear daemon [--port N]   與 serve 相同的行程
pear status              設定路徑、監聽位址、數量
pear providers           id、協定、base URL、模型個數、開或關
pear groups              依序印出成員，然後是每條規則
pear quota [--json] [--url URL]
                         執行中閘道的即時區間
pear version             發行標籤，或 dev
```

不帶參數執行 `pear` 會印出圖案和這段用法。

## HTTP

`GET /health` 回傳狀態、provider 數、帳號數和組數。`GET /v1/models` 把每個組列成 `group/<id>`，把每個對外模型列成 `<provider>/<model>`。`GET /v1/pear/quotas` 是行程內的即時快照。POST 路由是 `/v1/chat/completions`、`/v1/messages` 和 `/v1/responses`。這些路徑上的其他方法回傳 405。

pear 自己產生的錯誤是 JSON。OpenAI 用戶端讀 `error.type` 和 `error.message`。Anthropic 用戶端還會讀頂層的 `type`，其值為 `error`。

```json
{"type":"error","message":"...","error":{"type":"...","message":"..."}}
```

同一次請求裡，候選依序嘗試：

1. OAuth 帳號回傳 401 時，依上一節重試該帳號一次。第二次 401 才換下一個。沒有帳號的 401 立刻換下一個。
2. 403、404、429 和所有 5xx 都會試下一個候選。403、429 和 529 同時開始上面的冷卻。
3. `ttft_timeout_ms` 內沒有本文的第一個位元組，就試下一個候選。這時還沒向用戶端寫出任何內容。
4. 寫出第一個位元組之後，pear 留在這個候選上。靜默十分鐘會結束這條串流。位元組一直在來，就沒有總時限。上游回應標頭必須在 90 秒內到達。
5. 本文超過 32 MiB 時回傳 413，不會轉送。

用戶端斷線就停止這次嘗試。pear 不會因此去試另一個上游。

## 參與

見 [CONTRIBUTING.md](CONTRIBUTING.md)。在複製下來的倉庫裡，`mise run check` 會執行 `go vet ./...` 和 `go build ./...`。

## 授權

[MIT](LICENSE)。Copyright 2026 alexxhchen-nu.
