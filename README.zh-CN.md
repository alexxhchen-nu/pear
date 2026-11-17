<!-- readme-theme: phosphor-archive / archetype-c / hero-svg -->
<div align="center">

<img src="assets/readme/hero.svg" alt="pear — 本地 AI 网关" width="860">

**一个二进制文件。一份配置。所有 AI 客户端共用一个端口。**

[English](README.md) · **[简体中文](README.zh-CN.md)** · [繁體中文](README.zh-TW.md)

[![Go](https://img.shields.io/badge/go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![deps](https://img.shields.io/badge/third--party%20deps-0-3FB950)](go.mod)
[![license](https://img.shields.io/badge/license-MIT-3FB950)](LICENSE)
[![os](https://img.shields.io/badge/windows%20%C2%B7%20linux%20%C2%B7%20macos-amd64%20%C2%B7%20arm64-8b949e)](#安装)

</div>

## 🍐 pear 是什么

pear 是跑在你自己机器上的网关。把 Claude Code、Codex、Cursor、各种 SDK 或者一句 `curl` 指向 `127.0.0.1:3426`，剩下的事交给 pear：这次请求由哪个上游账号来服务。

- **三种协议，一个端口** —— Anthropic Messages、OpenAI Chat Completions、OpenAI Responses 都落在同一个监听上，出口时自动转换。
- **多个供应商，一个地址** —— `pear.json` 里每个 provider 有自己的 base URL、密钥和协议。group 把几个 provider 打包成一个模型名对外。
- **订阅不会睡着** —— Codex、Copilot、Claude 这些 OAuth 账号在后台自动刷新，挂了一夜的 CLI 第二天照样能用。
- **不用装任何东西** —— 没有数据库、没有内嵌浏览器、没有插件虚拟机。一个可以直接拷贝的可执行文件，加一份 `~/.pear/pear.json`。

## ✨ 为什么它这么小

- **一个文件就是全部真相。** 所有平台都用 `~/.pear/pear.json`。写入走临时文件加 rename，崩溃也不会留下写坏的配置。
- **没有锁文件。** 读配置从不加锁：启动时读一次，之后按需重读。
- **没有插件系统。** 路由、配额解析、协议转换都是 `internal/` 里的普通 Go 代码。
- **传输层保持无聊。** HTTP/1.1、SSE 透传、每个请求一个 goroutine。

`go.mod` 只有一个依赖 `go.uber.org/goleak`，且仅测试代码引用 —— 二进制仍然只用标准库的 `net/http`、`crypto/tls` 和 `encoding/json`。

## 📦 安装

四条路选一条，结果都是同一个可执行文件。

**1 · 用 mise 直接装 GitHub Release** —— 不用 clone，不用 Go 工具链。

```bash
mise use -g "github:alexxhchen-nu/pear@0.1.2"
pear version          # pear v0.1.2
```

> mise 默认隐藏发布不足 24 小时的版本（`minimum_release_age`）。要么像上面一样锁版本，要么等一天再用 `@latest`。

**2 · 用 mise 从源码构建** —— 现在就能用，不用等 release。

```bash
git clone https://github.com/alexxhchen-nu/pear.git
cd pear
mise install          # 按 mise.toml 装 Go 工具链
mise run build        # -> ./pear（Windows 上是 pear.exe）
```

**3 · `go install`** —— 需要 Go 1.24+，以及已经打好的 tag。

```bash
go install github.com/alexxhchen-nu/pear/cmd/pear@latest
```

**4 · 下载预编译包** —— 去 [Releases](https://github.com/alexxhchen-nu/pear/releases) 取一个，先用 `checksums.txt` 校验。

```bash
shasum -a 256 pear_darwin_arm64.tar.gz
```

> 包管理器（`scoop`、Homebrew、winget、AUR）还没发布。它们在下面的路线图里，这里不写假装能用的命令。

## ⚡ 30 秒跑起来

```bash
pear status          # 看加载了什么：providers、accounts、groups
pear serve           # 前台启动，监听 127.0.0.1:3426
```

把客户端指过来 —— pear 同时提供两种原生 base URL：

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:3426
export OPENAI_BASE_URL=http://127.0.0.1:3426/v1
```

确认它活着：

```bash
curl -s http://127.0.0.1:3426/health
curl -s http://127.0.0.1:3426/v1/models
```

`pear groups` 可以看清每个 group 会怎么挑上游。

## 🔀 请求是怎么被路由的

每个请求只做两个决定，整个系统就这些。

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

- **关卡 1 —— 路由。** 规则只看请求的形状：token 数、有没有图片、时间段、目录，或者是不是压缩/子代理调用。先命中先赢，都不命中就走 group 策略：`smart_pace`、`session_sticky`、`order`、`round_robin`。
- **关卡 2 —— 健康。** 候选按顺序试，直到有一个接单。遇到 `429` 或 `5xx` 就换下一个，失败的候选在窗口期内跳过。
- **粘性会话。** 会话 id 会哈希到某个固定成员，同一段对话的连续几轮落在同一个上游账号上，提示词缓存一直是热的。
- **请求清洗。** Anthropic 请求切到非 Anthropic 上游时，`thinking.signature` 和 `cache_control` 会被剥掉 —— 否则上游直接 `400`。

## ⚙️ 配置

全部内容都在 `~/.pear/pear.json`，顶层只有四个键。

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
- **`providers[]`** —— `id`、`name`、`protocol`、`base_url`、`api_key`，可选 `models` 和 `off`。协议可选 `openai`、`anthropic`、`responses`、`oauth-codex`、`oauth-copilot`、`oauth-claude`。
- **`accounts[]`** —— OAuth 订阅：`agent`、`user`、`access_token`、`refresh_token`、`expires_at`，pear 负责轮换。
- **`groups[]`** —— `id`、`strategy`、`members`，可选 `classifier` 和 `rules`。成员写成 `provider/model`、另一个 group，或者任一家都能提供的裸模型名。

规则描述的是请求本身，而不是提示词内容：

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

想同时保留几套配置，就把目录指到别处：

```bash
PEAR_CONFIG_DIR=~/pear-profiles/work pear status
```

## 🧪 命令行

- `pear serve [--port N]` —— 前台运行网关。
- `pear daemon` —— 同一件事，换成 init 系统习惯的名字。
- `pear status` —— 配置路径、数量统计、监听地址。
- `pear providers` —— provider 列表和各自协议。
- `pear groups` —— group、策略、成员。
- `pear quota` —— 从运行中的网关取配额快照。
- `pear version` —— 构建时写入的版本（本地构建是 `dev`）。

## 🌐 HTTP 接口

- `POST /v1/messages` —— Anthropic Messages 格式。
- `POST /v1/chat/completions` —— OpenAI Chat Completions 格式。
- `POST /v1/responses` —— OpenAI Responses 格式。
- `GET /v1/models` —— 聚合后的模型列表。
- `GET /v1/pear/quotas` —— 逐账号配额快照。
- `GET /health` —— 存活检查。
故障由网关负责，不交给客户端：`401` 先刷新一次令牌并原候选重试一次，
`403` 按 `Retry-After` 让该账号冷却，`404`/`429`/`5xx` 切换到下一个候选，
上游在 `ttft_timeout_ms` 内没吐出首字节也算候选失败。请求体超过 32 MB
直接返回 `413`，不再静默截断。pear 自己产生的错误统一是 JSON
`{"type":"error","error":{"type":...,"message":...}}`，
OpenAI 与 Anthropic 的 SDK 都能解析。响应没有总超时:只要上游持续出字节,
流就一直送到上游结束;上游静默十分钟才判定卡死并关闭,避免调用方无限等待。

## 🗺️ 路线图

- ✅ 单文件二进制、三种协议、四种策略。
- ✅ OAuth 刷新、会话粘性、rosetta 请求清洗。
- ✅ 配额接口和 `pear quota`。
- 🔜 第一个带校验和的正式 release。
- 🔜 `scoop` · Homebrew · winget · AUR 清单。
- 🔜 命令行里的多套配置。
- 🧪 用量统计与本地遥测。

## 🤝 参与

有请求形状或供应商相关的问题就开 issue。附上 `pear status` 和失败时的 `/v1/models` 输出，一次就能定位。提交 PR 请保持 `mise run check`（vet + build）通过，并说明你动了哪条路由分支。

## 📄 许可

[MIT](LICENSE) © 2026 alexxhchen
