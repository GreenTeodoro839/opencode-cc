# opencode-cc

> Anthropic → OpenCode Zen 多协议桥接代理，附带嵌入式 Web 控制面板。

`opencode-cc` 是一个高性能 Go 代理，对外暴露 **Anthropic Messages API**
（`POST /v1/messages`，支持流式与非流式），并将其翻译为 **[OpenCode Zen](https://opencode.ai/docs/zen/)**
网关的原生协议。Zen 上有 49 个模型分属 4 种协议——本代理按目标 model id 自动路由到正确的协议翻译器，让 Claude Code 透明地跑在 GLM / Kimi / DeepSeek / Qwen / Claude / GPT / Gemini 等模型上。

友情链接：[linuxdo](https://linux.do/)

```
┌────────────┐  POST /v1/messages   ┌──────────────┐  按 model 路由 ↓       ┌──────────┐
│ Claude Code│ ───────────────────> │ opencode-cc  │ ──────────────────────> │ OpenCode │
│            │ <── Anthropic SSE ── │   (Go)       │ <── 各协议 SSE ─────── │   Zen    │
└────────────┘                      └──────────────┘                         └──────────┘
                                       嵌入式 React 控制面板 ▲ /api/*
                                          SQLite（纯 Go，无 CGO）
```

## 4 种协议自动路由

Zen 不是单一 OpenAI 兼容端点——按模型来源分成 4 条路径。代理根据 model id 前缀自动选择：

| 协议 | 路径 | 适用模型 | 翻译方式 |
|------|------|----------|----------|
| **OpenAI** | `/v1/chat/completions` | GLM、Kimi、DeepSeek、MiniMax、MiMo、Grok、免费模型 | Anthropic ↔ OpenAI 双向翻译 |
| **Anthropic** | `/v1/messages` | Claude、Qwen | 近乎透传（仅改写 model id） |
| **Responses** | `/v1/responses` | GPT 全家桶 | Anthropic ↔ OpenAI Responses API |
| **Google** | `/v1beta/models/{id}` | Gemini | Anthropic ↔ Google Generative Language |

每种协议实现 `upstream.Protocol` 接口的 `TranslateRequest` / `TranslateResponse` / `TranslateStream` 三个方法。

## 特性

- **完整工具调用支持。** Anthropic `tool_use` / `tool_result` 块在 OpenAI 协议下双向翻译为 `tool_calls` / `role:"tool"` 消息；Claude Code 的工具定义转成 OpenAI function tools。国产模型的 `reasoning_content` 扩展字段翻译为 Anthropic `thinking` 块。
- **4 协议自动路由。** 根据 model id 前缀（`claude-`/`qwen` → Anthropic、`gpt-` → Responses、`gemini-` → Google、其余 → OpenAI），无需手动配置。
- **49 个预置模型目录。** Models 页展示所有模型的价格、context、能力标签、协议徽章；Config 页可从目录里一键选模型加入映射。
- **单一静态二进制。** React SPA 通过 `embed.FS` 内嵌，运行时无需 Node；SQLite 用纯 Go 驱动，无 CGO，可干净交叉编译。
- **Web 控制面板。** Dashboard（流量图、健康状态、模型分布）、Models（模型库浏览 + 筛选）、Inspector（实时请求列表，显示走的哪个协议）、Config（Zen 配置、代理鉴权、模型映射——热更新）。
- **默认安全。** 恒定时间 Bearer token 鉴权、请求体大小限制、单请求 panic 恢复、优雅关闭。

## 快速开始

### 前置条件

- Go 1.22+
- Node 20+（仅构建 UI 时需要；运行时不需要）
- 一个 OpenCode Zen API key——到 [opencode.ai/auth](https://opencode.ai/auth) 登录、添加账单信息、复制 API key

### 构建与运行

```bash
make            # 构建前端 + Go 二进制 -> ./opencode-cc
./opencode-cc   # 启动于 :8787，自动创建 config.json 与 data/opencode-cc.db
```

打开控制面板 `http://localhost:8787/`，进入 **Config** 标签页：
1. 在 "Upstream (OpenCode Zen)" 卡片填入你的 **API key**
2. 点 "Test connection" 确认能连通
3. 在 "Model mappings" 里调整 Claude Code 模型名到 Zen 模型的映射（默认已预置几条省钱组合，例如 `claude-sonnet-4-5` → `glm-5.1`）

然后指向 Claude Code：

```bash
export ANTHROPIC_BASE_URL=http://localhost:8787
export ANTHROPIC_AUTH_TOKEN=local    # 未设 token 时任意值都行
claude
```

### 开发模式（HMR 热更新）

```bash
make dev   # Vite 跑在 :5174 + Go 跑在 :8787，API 通过 Vite 代理
```

### Docker

```bash
make docker
docker run -p 8787:8787 -v $PWD/data:/data opencode-cc
```

## 翻译原理

### 请求路由

`POST /v1/messages` 进来后，代理根据 `req.Model` 在映射表里找到目标 Zen model id（找不到则原样透传），再用 `upstream.Router.For(modelID)` 选协议。所有 4 种协议共享一个统一的输出侧（`anthropicEmitter`），把翻译后的内容块以 Anthropic SSE 标准事件序列发出。

### Anthropic ↔ OpenAI 翻译（最复杂）

| Anthropic | OpenAI Chat Completions |
|-----------|-------------------------|
| `tools[]{name, description, input_schema}` | `tools[]{type:"function", function:{name, description, parameters}}` |
| `content[]{type:"tool_use", id, name, input(object)}` | `assistant.tool_calls[]{id, function:{name, arguments(JSON 字符串)}}` |
| `user.content[]{type:"tool_result", tool_use_id, content}` | `{role:"tool", tool_call_id, content}` |
| `content[]{type:"thinking"}` | `delta.reasoning_content`（DeepSeek/GLM/Kimi 扩展字段） |
| `stop_reason:"tool_use"` | `finish_reason:"tool_calls"` |
| 流式 `input_json_delta`（按 index 累积） | 流式 `delta.tool_calls[].function.arguments`（按 index 累积） |

流式输出时，工具调用的 `arguments` 分片按 `index` 累积，完整后再作为单个 `tool_use` 块发出，保证 Claude Code 收到结构完整的工具调用。

### Anthropic 透传（Claude/Qwen）

Zen 对 Claude 和 Qwen 模型直接提供原生 Anthropic Messages API。代理只改写 model id，请求体、响应体、SSE 流全部原样转发。这是最简单的一条路径，连工具调用都无需翻译。

### Anthropic ↔ Responses（GPT）

把 Anthropic `messages[]` 翻译成 Responses API 的 `input` 数组（`message` / `function_call` / `function_call_output` 三种 item 类型）。流式事件 `response.output_text.delta` / `response.function_call_arguments.delta` 等翻译为 Anthropic `content_block_delta`。

### Anthropic ↔ Google（Gemini）

`messages[]` → `contents[]`（role: user/model），`tools[]` → `tools[].functionDeclarations`。Gemini 流式返回 JSON 数组（每个 chunk 含 `candidates[].content.parts[]`），代理解析后翻译为 Anthropic 增量。

## 配置

`config.json`（首次运行自动创建）：

```jsonc
{
  "upstream": {
    "base_url": "https://opencode.ai/zen",
    "api_key": ""              // Bearer token，必填
  },
  "proxy": {
    "listen_addr": ":8787",
    "auth_token": ""           // 客户端鉴权 token；"" = 开放（本地用）
  },
  "web": { "listen_addr": "", "enabled": true },
  "model_mappings": [
    // Claude Code 发来的 model 字符串 → Zen 真实 model id
    { "claude_model": "claude-sonnet-4-5", "zen_model": "glm-5.1" },
    { "claude_model": "claude-haiku-4-5", "zen_model": "kimi-k2.5" }
  ]
}
```

所有字段都可在 **Config** 标签页编辑，保存后桥接器热更新（重建上游客户端），无需重启。未在映射表里的 model 字符串原样转发给 Zen。

## API 接口

### Anthropic 兼容（给 Claude Code 用）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/messages` | `stream:true` → SSE；`stream:false` → JSON |
| POST | `/v1/messages/count_tokens` | 尽力而为的 token 估算 |
| GET | `/healthz` | 存活探针 |

### 控制面板（给 UI 用）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/config` | 当前配置快照 |
| PUT | `/api/config` | 更新 + 持久化 + 热更新 |
| GET | `/api/status` | 代理 / Zen 上游健康状态 |
| GET | `/api/requests` | 最近的 `/v1/messages` 调用（含路由协议） |
| GET | `/api/models` | Zen 模型目录（49 个） |
| GET | `/api/stats` | 小时级 + 分模型 + 汇总 |
| GET | `/api/test` | ping Zen（GET /v1/models） |

## 项目结构

```
opencode-cc/
├── cmd/opencode-cc/        # 入口
├── internal/
│   ├── config/             # JSON 配置 + 热更新
│   ├── anthropic/          # Anthropic Messages API 线路类型 + SSE writer
│   ├── upstream/           # Zen 网关客户端 + 4 个协议翻译器
│   │   ├── protocol.go     # Protocol 接口 + 路由器
│   │   ├── anthropic.go    # Anthropic 透传
│   │   ├── openai.go       # OpenAI Chat Completions 翻译（国产模型）
│   │   ├── responses.go    # OpenAI Responses 翻译（GPT）
│   │   ├── google.go       # Google Gemini 翻译
│   │   ├── stream_emit.go  # 共享的 Anthropic SSE 输出侧
│   │   ├── models.go       # 49 个 Zen 模型目录
│   │   └── client.go       # HTTP 客户端（Bearer + GET /v1/models）
│   ├── bridge/             # /v1/messages handler：路由 → 翻译 → 转发
│   ├── store/              # SQLite（modernc，纯 Go）
│   └── web/                # 控制面板 API + 嵌入式 SPA
├── web/                    # React + Vite + Tailwind 源码
│   └── src/pages/{Dashboard,Inspector,Models,Config}.tsx
└── Dockerfile              # 多阶段：node → go → distroless
```

## 注意事项

- **协议路由的代价。** Anthropic → OpenAI / Responses / Google 是有损翻译——某些 Anthropic 特有概念（如 `cache_control`、`thinking` 的 signature 完整性）在后端协议里没有对应物。Claude/Qwen 透传路径无此问题。
- **token 用量。** OpenAI 协议下用流式的 `include_usage` chunk 拿真实计数；其他协议按上游返回的 usage 字段读取，缺失时回退到估算。
- **模型目录可能过时。** `internal/upstream/models.go` 是手工整理的快照，价格和能力会变。可通过 `GET https://opencode.ai/zen/v1/models`（需 API key）拉取最新清单校准。
- **旧 SQLite 库不兼容。** 重构后 schema 变了（删了 session_map 表，requests 改字段）。开发期直接删 `data/*.db`；正式版会加版本化迁移。

## 许可证

MIT
