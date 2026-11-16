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

[English](README.md) · **[简体中文](README.zh-CN.md)** · [繁體中文](README.zh-TW.md)

[![Go](https://img.shields.io/badge/go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![deps](https://img.shields.io/badge/third--party%20deps-0-3FB950)](go.mod)
[![binary](https://img.shields.io/badge/binary-6.3%20MB-3FB950)](https://github.com/alexxhchen-nu/pear/releases)
[![license](https://img.shields.io/badge/license-MIT-3FB950)](LICENSE)
![platform](https://img.shields.io/badge/os-windows%20%7C%20linux%20%7C%20macos-8b949e)

</div>

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                        █▓▒░  ◆  OVERVIEW  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

Pear 是一个单文件的本地 AI 网关。它在一个端口上同时接受 Anthropic、
OpenAI 与 Responses 三种报文格式，按声明式规则或负载均衡组把请求转发到
上游，并用自动续期让 OAuth 订阅账号保持可用。

它存在的原因是：桌面版 AI 网关长成了"应用"——内嵌 WebView、打包数据库、
插件虚拟机。Pear 保留同样的路由能力，但**没有任何第三方 Go 依赖**：
`go.mod` 里没有 `require` 块，整个实现就是 `net/http` 加 `crypto/tls`。

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

为什么它一直保持这个体积：

- **一个文件即全部事实。** 所有平台都用 `~/.pear/pear.json`。写入走
  `.tmp` + `rename`，进程崩了也不会留下写了一半的配置。
- **没有锁文件。** 并发读取不需要加锁；进程启动时读一次，需要时重读。
- **没有插件虚拟机。** OAuth 续期、路由、协议转换都是 `internal/` 里的普通 Go 代码。
- **传输层保持无聊。** HTTP/1.1、SSE 直通、每请求一个 goroutine。

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                        █▓▒░  ◆  INSTALL  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

三种方式产出的都是同一个可执行文件，任选其一。

**1 · 预编译二进制**（Windows / Linux / macOS，amd64 与 arm64）——
推送第一个 `v*` tag 后文件会出现在
[Releases](https://github.com/alexxhchen-nu/pear/releases)。每个压缩包都由
`checksums.txt` 覆盖，运行前先校验：

```bash
sha256sum -c checksums.txt --ignore-missing -i   # Linux
shasum -a 256 pear_darwin_arm64.tar.gz          # macOS
```

**2 · `go install`**（需要 Go 1.24+；同样依赖仓库已打 tag）：

```bash
go install github.com/alexxhchen-nu/pear/cmd/pear@latest
```

**3 · 用 mise 从源码构建**（现在就能用，不需要 tag）：

```bash
git clone https://github.com/alexxhchen-nu/pear.git
cd pear
mise install          # 按 mise.toml 装 Go 工具链
mise run build        # 生成 ./pear(.exe)，此时 `pear version` 显示 dev
mise run release:check   # 校验 goreleaser 配置
mise run snapshot     # 带版本号交叉构建到 dist/，不发布
```

包管理器（scoop、Homebrew、AUR、winget）**目前尚未发布**，
见下方路线图。

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                      █▓▒░  ◆  QUICK START  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

```bash
pear status            # 确认已加载的 provider、账号与路由组
pear providers        # 确认导入了什么
pear serve --port 3426
```

然后让客户端指向网关，用 Anthropic 原生或 OpenAI 原生任一 base URL：

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:3426
export OPENAI_BASE_URL=http://127.0.0.1:3426/v1
```

自检：

```bash
curl -s http://127.0.0.1:3426/health
curl -s http://127.0.0.1:3426/v1/models | head
```

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                     █▓▒░  ◆  CONFIG SCHEMA  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

`~/.pear/pear.json` —— 全部配置，四个顶层键。

| 键 | 类型 | 含义 |
| :-- | :-- | :-- |
| `server.listen` | string | 监听地址，默认 `127.0.0.1:3426` |
| `server.default_target` | string | 没有规则命中时的兜底路由 |
| `server.compact_target` | string | 压缩（compact）调用的路由 |
| `server.subagent_target` | string | 子代理流量的路由 |
| `server.ttft_timeout_ms` | int | 首 token 超时预算，超过即切换 |
| `server.sticky_ttl_min` | int | 会话粘性窗口，默认 60 分钟 |
| `providers[]` | array | `id`、`name`、`protocol`、`base_url`、`api_key`，可选 `models`、`off` |
| `accounts[]` | array | OAuth 订阅：`agent`、`user`、`access_token`、`refresh_token`、`expires_at` |
| `groups[]` | array | `id`、`strategy`、`members`，可选 `classifier`、`rules` |

`providers[].protocol` 取值：`openai`、`anthropic`、`responses`、
`oauth-codex`、`oauth-copilot`、`oauth-claude`。
`groups[].strategy` 取值：`smart_pace`、`session_sticky`、`order`、
`round_robin`。

规则只看请求形态，不看提示词内容：

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

`PEAR_CONFIG_DIR` 可以覆盖配置目录（方便测试，也方便并排放多套配置）：

```bash
PEAR_CONFIG_DIR=~/pear-profiles/work pear status
```

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                          █▓▒░  ◆  CLI  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

| 命令 | 作用 |
| :-- | :-- |
| `pear serve [--port N]` | 前台运行网关 |
| `pear daemon` | 等同 `serve` |
| `pear status` | 配置路径、provider/group 数量、监听地址 |
| `pear providers` | 列出 provider 及其协议 |
| `pear groups` | 列出 group、策略与成员 |
| `pear quota` / `quotas` | 查询运行中网关的实时配额 |
| `pear version` | 打印构建时写入的版本号 |

HTTP 接口：

| 路径 | 用途 |
| :-- | :-- |
| `POST /v1/messages` | Anthropic messages 格式 |
| `POST /v1/chat/completions` | OpenAI chat 格式 |
| `POST /v1/responses` | OpenAI Responses 格式 |
| `GET /v1/models` | 汇总后的模型列表 |
| `GET /v1/pear/quotas` | 每账号配额快照 |
| `GET /health` | 存活探针 |

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                     █▓▒░  ◆  ROUTING LOGIC  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

每个请求只有两次决策：

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

`GATE 1` 用命中的规则或组策略决定目标列表；`GATE 2` 按顺序尝试候选，
直到某个上游接受请求。会话 id 经 FNV-64a 哈希映射到首选成员，因此同一段
对话的连续轮次会落在同一个上游账号上——上游的 prompt cache 因此保持命中。
只有当候选返回 429 或 5xx 时哈希才会漂移，并且新选择会在 TTL 内固定。

转发前 `internal/rosetta` 会规范化请求体：来自 Anthropic 的请求故障转移到
非 Anthropic 上游时，会剥离 `thinking.signature` 与 `cache_control`，
否则上游会以 `400` 拒绝。

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

配额数据来自两处：上游在限流响应头里返回的部分，以及 Pear 自己记录的
OAuth token 状态。两者都只读暴露——Pear 不会偷偷消耗订阅额度。

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                   █▓▒░  ◆  ROADMAP & SUPPORT  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

| 范围 | 状态 |
| :-- | :-- |
| 单二进制、三种协议 | 已完成 |
| OAuth 续期（codex、copilot、claude） | 已完成 |
| 会话粘性 + rosetta 净化 | 已完成 |
| 配额接口 | 已完成 |
| 首个正式 tag + 校验和 | 下一步 |
| `scoop` / Homebrew / winget 清单 | 计划中 |
| CLI 多配置文件 | 计划中 |
| 用量统计 / 本地遥测 | 评估中 |

贡献方式：开 issue 时附上请求形态和涉及的上游。带 `pear status` 输出与
出错的 `/v1/models` 响应的问题报告，通常一轮就能定位。

```text
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
                        █▓▒░  ◆  LICENSE  ◆  ░▒▓█
░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
```

MIT —— 见 [LICENSE](LICENSE)。

```text
▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄
█ P E A R   ·   single binary   ·   zero third-party deps                █
█ source  github.com/alexxhchen-nu/pear   license  MIT                   █
▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
```
