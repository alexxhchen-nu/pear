<p align="center">
  <img src="assets/readme/pear.svg" alt="pear" width="72">
</p>

# pear

[English](README.md) · **简体中文** · [繁體中文](README.zh-TW.md)

[![release](https://img.shields.io/github/v/release/alexxhchen-nu/pear)](https://github.com/alexxhchen-nu/pear/releases)
[![license](https://img.shields.io/github/license/alexxhchen-nu/pear)](LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/alexxhchen-nu/pear)](go.mod)

pear 是本机的模型 API 网关。发布出来的文件是一个静态可执行文件（`CGO_ENABLED=0`）。把 Claude Code、Codex、Cursor，或其他 OpenAI、Anthropic 客户端指到 `127.0.0.1:3426`。pear 读取 `~/.pear/pear.json`，把每个请求转到你写在里面的上游。

适合已经有 API key，或者 Codex、GitHub Copilot、Claude 订阅 token 的人。你要的是一个本机地址，加上一份排好顺序的失败切换名单。pear 会把 `model` 改成选中的上游模型。目标协议不是 `anthropic` 或 `oauth-claude` 时，还会去掉 Anthropic 的 thinking 签名和 `cache_control`。协议不同时，只翻译非流式的纯文本对话。网关代码用的是 Go 标准库。`go.mod` 里唯一的直接依赖 `go.uber.org/goleak` 只被测试引用。

## 安装

用 [mise](https://mise.jdx.dev) 装发布版。不用克隆仓库，也不用装 Go。shell 里要先 `mise activate`，shim 才会出现在 `PATH` 上。

```bash
mise use -g github:alexxhchen-nu/pear@0.1.5
pear version
```

输出是 `pear v0.1.5 (Pear AI Gateway & OAuth Router)`。

写这份说明时，最新发布标签是 `0.1.5`。写明标签会立刻安装。`@latest` 会跳过比 `minimum_release_age` 更新的标签（默认 24 小时），所以发布当天 `@latest` 仍可能选中更旧的标签。

要跟踪某个分支时，从克隆构建。`mise install` 按 `mise.toml` 安装 Go。版本字符串保持 `dev`，这个任务也不设置 `CGO_ENABLED=0`：

```bash
git clone https://github.com/alexxhchen-nu/pear.git
cd pear
mise install
mise run build
./pear version
```

`./pear version` 输出 `pear vdev (Pear AI Gateway & OAuth Router)`。

`go install` 需要 Go 1.24 或更新版本，打印的同样是 `dev`，因为只有发布构建会写入标签：

```bash
go install github.com/alexxhchen-nu/pear/cmd/pear@v0.1.5
```

也可以从 [v0.1.5 发布页](https://github.com/alexxhchen-nu/pear/releases/tag/v0.1.5) 下载压缩包。linux、darwin、windows 都有 amd64 和 arm64。Linux 是 `.tar.gz`，macOS 和 Windows 是 `.zip`。文件名类似 `pear_0.1.5_linux_amd64.tar.gz` 和 `pear_0.1.5_darwin_arm64.zip`。解压后 `pear` 和本说明放在一起。用同一发布里的 `checksums.txt` 核对：

```bash
sha256sum -c checksums.txt --ignore-missing
```

## 快速开始

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

把 `api_key` 换成真实的 key。`models` 和 `members` 里的模型 id 也换成这个 key 能调用的名字。然后启动：

```bash
pear serve
```

`pear serve` 只在启动时读文件。改完要重启。`pear serve --port 8080` 监听 `127.0.0.1:8080`，并盖过 `server.listen`。`pear daemon` 是同一个进程。

另开一个 shell：

```bash
curl -sS -D - http://127.0.0.1:3426/v1/chat/completions \
  -H 'content-type: application/json' \
  -d '{"model":"group/default","messages":[{"role":"user","content":"ping"}]}'
```

响应正文是上游自己的正文。pear 加上 `X-Pear-Routed-Provider: openai` 和 `X-Pear-Routed-Model: gpt-4o-mini`，转发出去的 JSON 里 `"model"` 是 `"gpt-4o-mini"`。`model` 为空、`auto` 或 `default` 时，走 `default_target`。`GET /health` 里有 `"status":"ok"`。`GET /v1/models` 会列出 `group/default` 和 `openai/gpt-4o-mini`。

客户端这样指过来：

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:3426
export OPENAI_BASE_URL=http://127.0.0.1:3426/v1
```

Anthropic 客户端在这个主机上访问 `/v1/messages`。OpenAI 客户端会在已经带 `/v1` 的 base 后面接 `/chat/completions`。

`PEAR_CONFIG_DIR` 换一个目录。开头的 `~/` 会展开。文件名仍是 `pear.json`。

```bash
PEAR_CONFIG_DIR=~/pear-profiles/work pear status
```

没有 `pear.json` 时，监听地址是 `127.0.0.1:3426`，默认目标是 `group/smart-split`，首字节等待 12000 毫秒，粘性窗口 60 分钟，provider 为空。

## 协议

监听端接受三种 POST 路径。客户端路径和上游报文格式相同时，pear 按该路径转发。不同时，只翻译非流式的纯文本对话：system、user、assistant 字符串，以及 `max_tokens` 和 `temperature`。译过的请求发到上游自己的路径，不是客户端路径。跨协议的流式、工具和图片返回 `translation_unsupported`，不会转发。

报文格式：`openai` 和 `oauth-copilot` 是 Chat Completions。`anthropic` 和 `oauth-claude` 是 Messages。`responses` 和 `oauth-codex` 是 Responses。

| 路径 | 客户端 API |
| --- | --- |
| `/v1/chat/completions` | OpenAI Chat Completions |
| `/v1/messages` | Anthropic Messages |
| `/v1/responses` | OpenAI Responses |

`protocol` 决定凭证和清理方式：

- `openai` 和 `responses` 用 `api_key` 发送 `Authorization: Bearer`。
- `anthropic` 同时发送 `x-api-key` 和 `Authorization: Bearer`。客户端没带 `Anthropic-Version` 时，pear 设为 `2023-06-01`。`oauth-claude` 也会带上这个版本头。
- `oauth-codex`、`oauth-copilot`、`oauth-claude` 使用账号里的 bearer token。见 [OAuth 账号](#oauth-账号)。

`base_url` 以 `/v1` 结尾，且请求路径以 `/v1/` 开头时，拼起来不会出现第二个 `/v1`。`https://api.openai.com/v1` 加上 `/v1/chat/completions` 得到 `https://api.openai.com/v1/chat/completions`。

`insecure_tls: true` 会跳过该 provider 的 TLS 证书校验。

## 路由组

请求里的 `model` 决定目标。`group/<id>` 进入该组。`provider/model` 选中该 provider，即使 `models` 里没有这个模型。`gpt-4o-mini` 这种裸名字，用第一个启用且 `models` 包含它的 provider。

组内按顺序看 `rules`。第一条命中的规则用它的 `use` 替换 `members`。都没命中时，由 `strategy` 排列 `members`：

- `order`，以及空的或未知的策略：按写好的顺序。
- `round_robin`：每个新 session 从下一个成员开始，再走剩下的。
- `session_sticky`：用 session 哈希选起始成员。同一个 session 保持这个起点。
- `smart_pace`：带 OAuth 账号的成员按剩余额度排序。剩余越多、重置越近，越靠前。没有账号的成员在分数相同时留在原位。

每一项可以是 `provider/model`、`group/<id>`（最多嵌套四层），或裸模型名。

一次成功的响应之后，pear 用 FNV-64a 对正文前 768 字节做哈希，把胜出的 `provider/model` 记住 `sticky_ttl_min` 分钟。默认 60，写成 `0` 也是 60。之后前缀相同的请求，如果这个目标还在候选里，就把它移到最前。

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

规则里的每个字段都是必要条件。缺省，或把布尔值设为 false，不会反过来排除那些请求。一个条件都没写的规则永远不命中。

- `when_has_image`：正文里有 `"image"` 或 `"image_url"`。
- `min_tokens`：和 `len(body)/4` 比较。这是体积检查，不是分词器算出来的 token 数。
- `time_range`：本地时间，格式 `HH:MM-HH:MM`。开始晚于结束时，区间跨过午夜。
- `folder_contains`：对请求头 `X-Project-Dir` 做不区分大小写的子串匹配。不读进程的工作目录。
- `when_compact`：正文含有 `Summarize the conversation`、`<context-compaction>` 或 `compact the conversation`。
- `when_subagent`：正文含有 `read-only search agent` 或 `Generate a concise title`。

如果设置了 `server.compact_target` 或 `server.subagent_target`，会在组规则之前替换模型。一个候选都解析不出来，而又有启用的 provider 列出了模型时，pear 用该 provider 的第一个模型。

## OAuth 账号

协议用 `oauth-codex`、`oauth-copilot` 或 `oauth-claude`，并把 `api_key` 留空。pear 会找 `agent` 包含该 provider `id`、且已启用的账号。不写 `enabled` 时视为 false。没有启用的账号匹配时，pear 仍会把请求转到这个 provider，并且不带 bearer token。

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

`base_url` 填该产品自己的 API。`expires_at` 是 Unix 秒。`agent` 为 `plugin:github-copilot` 时，可以匹配 id 为 `copilot` 或 `github-copilot` 的 provider。

`pear serve` 运行时，会在到期前大约两分钟续期，401 之后也会再续一次。续期失败时，只要当前 token 还没过期就继续用，下一轮再试。已经过期且续期失败的 token 不会发出去。`expires_at` 是 Unix 秒。毫秒值也能读，写回文件时改成秒。

- `agent` 为 `codex`（可带 `plugin:` 前缀）时，把 `refresh_token` POST 到 `https://auth.openai.com/oauth/token`。新的 access token，以及服务器返回的新 refresh token，会写回 `pear.json`。
- `agent` 为 `claude` 或以 `claude` 开头时，POST 到 `https://platform.claude.com/v1/oauth/token`。这个主机不存在或连不上时，再试 `https://console.anthropic.com/v1/oauth/token`。轮换后的 token 写回 `pear.json`。
- `agent` 包含 `github-copilot` 时，用 `access_token` 向 `https://api.github.com/copilot_internal/v2/token` 换 bearer。这张短期 bearer 只留在内存里，不写入 `pear.json`。
- 其他 agent 直接发送 `access_token`，不会续期。

账号遇到 401 时，pear 清掉缓存的 bearer，并对该账号重试一次。Codex 和 Claude 会刷新，Copilot 会交换，其他 agent 会再送一次原来的 access token。

多个账号都匹配时，剩余额度更高的分数更高，重置更早的分数也更高。冷却中的账号会被跳过。还没有任何额度窗口时，匹配的账号分数相同。窗口只在 `pear serve` 进程里更新，而且只认 Anthropic 统一的 5 小时和 7 天利用率响应头。`GET /v1/pear/quotas` 返回这个进程的快照，access token 和 refresh token 会被遮住，`extra` 会被去掉。`pear quota` 读运行中网关的这个接口。`pear serve` 写过配置目录里的 `gateway.addr` 时用那个地址，否则用 `server.listen`。`--url` 覆盖两者。没有进程在听就失败。`pear serve` 退出时删掉 `gateway.addr`。

403 会按 `Retry-After` 的秒数冷却该账号，没有这个头就冷却 15 分钟。429 或 529 按 `Retry-After` 冷却，没有就冷却 30 秒。

## 配置参考

省略的 `server` 字段保持上面“文件不存在”时的默认值。`pear status`、`pear providers`、`pear groups` 每次执行都会读文件。

| 键 | 作用 |
| --- | --- |
| `server.listen` | 监听地址。默认 `127.0.0.1:3426`。 |
| `server.default_target` | `model` 为空、`auto` 或 `default` 时使用。 |
| `server.compact_target` | 压缩请求到来时替换模型。 |
| `server.subagent_target` | 子代理请求到来时替换模型。 |
| `server.ttft_timeout_ms` | 等待上游第一个字节。默认 12000。`0` 也是 12000。 |
| `server.sticky_ttl_min` | 粘性时间，单位分钟。默认 60。`0` 也是 60。 |
| `providers[].id`、`name`、`protocol`、`base_url`、`api_key` | 上游的标识、地址和凭证。 |
| `providers[].models` | 出现在 `GET /v1/models` 里，也用来解析裸模型名。 |
| `providers[].off` | `true` 时停用该 provider。 |
| `providers[].insecure_tls` | 跳过 TLS 校验。 |
| `accounts[].id`、`agent`、`user`、`enabled` | `enabled` 必须为 true，否则跳过。 |
| `accounts[].plan` | `pear quota` 会打印。路由不读它。 |
| `accounts[].access_token`、`refresh_token`、`expires_at` | 凭证。`expires_at` 是 Unix 秒，或毫秒。 |
| `accounts[].extra` | 留在文件里。快照里会去掉。 |
| `groups[].id`、`name`、`members`、`rules` | 路由器真正会走的组。 |
| `groups[].strategy` | `order`、`round_robin`、`session_sticky` 或 `smart_pace`。空的或未知的等于 `order`。 |
| `groups[].classifier` | 接受这个字段，然后忽略。 |

## 命令

```text
pear serve [--port N]    开始监听。--port 强制使用 127.0.0.1:N
pear daemon [--port N]   与 serve 相同的进程
pear status              配置路径、监听地址、数量
pear providers           id、协议、base URL、模型个数、开或关
pear groups              按顺序打印成员，然后是每条规则
pear quota [--json] [--url URL]
                         运行中网关的实时窗口
pear version             发布标签，或 dev
```

不带参数运行 `pear` 会打印图案和这段用法。

## HTTP

`GET /health` 返回状态、provider 数、账号数和组数。`GET /v1/models` 把每个组列成 `group/<id>`，把每个对外模型列成 `<provider>/<model>`。`GET /v1/pear/quotas` 是进程内的实时快照。POST 路由是 `/v1/chat/completions`、`/v1/messages` 和 `/v1/responses`。这些路径上的其他方法返回 405。

pear 自己产生的错误是 JSON。OpenAI 客户端读 `error.type` 和 `error.message`。Anthropic 客户端还会读顶层的 `type`，其值为 `error`。

```json
{"type":"error","message":"...","error":{"type":"...","message":"..."}}
```

同一次请求里，候选按顺序尝试：

1. OAuth 账号返回 401 时，按上一节重试该账号一次。第二次 401 才换下一个。没有账号的 401 立刻换下一个。
2. 403、404、429 和所有 5xx 都会试下一个候选。403、429 和 529 同时开始上面的冷却。
3. `ttft_timeout_ms` 内没有正文的第一个字节，就试下一个候选。这时还没有向客户端写出任何内容。
4. 写出第一个字节之后，pear 留在这个候选上。静默十分钟会结束这条流。字节一直在来，就没有总时限。上游响应头必须在 90 秒内到达。
5. 正文超过 32 MiB 时返回 413，不会转发。

客户端断开就停止这次尝试。pear 不会因此去试另一个上游。

## 参与

见 [CONTRIBUTING.md](CONTRIBUTING.md)。在克隆出来的仓库里，`mise run check` 会执行 `go vet ./...` 和 `go build ./...`。

## 许可

[MIT](LICENSE)。Copyright 2026 alexxhchen-nu.
