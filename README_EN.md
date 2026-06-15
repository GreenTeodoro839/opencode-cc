# opencode-cc

[简体中文](README.md) | [English](README_EN.md)

> An Anthropic-to-OpenCode Zen multi-protocol bridge proxy with an embedded web dashboard.

`opencode-cc` is a high-performance Go proxy that exposes the **Anthropic Messages API**
(`POST /v1/messages`, with both streaming and non-streaming support) and translates it into the native protocols of the
**[OpenCode Zen](https://opencode.ai/docs/zen/)** gateway. Zen provides 49 models across four protocols. The proxy
automatically selects the correct protocol translator based on the target model ID, allowing Claude Code to run
transparently with GLM, Kimi, DeepSeek, Qwen, Claude, GPT, Gemini, and other models.

Community: [linuxdo](https://linux.do/)

```text
┌────────────┐  POST /v1/messages   ┌──────────────┐  Route by model ↓      ┌──────────┐
│ Claude Code│ ───────────────────> │ opencode-cc  │ ─────────────────────> │ OpenCode │
│            │ <── Anthropic SSE ── │     (Go)     │ <── Protocol SSE ───── │   Zen    │
└────────────┘                      └──────────────┘                         └──────────┘
                                      Embedded React dashboard ▲ /api/*
                                           SQLite (pure Go, no CGO)
```

<img width="2540" height="1193" alt="QQ_1781519790308" src="https://github.com/user-attachments/assets/2854f45f-62a0-463a-9b22-a07a770670b4" />

## Automatic Routing Across Four Protocols

Zen is not a single OpenAI-compatible endpoint. Its models are exposed through four protocol paths. The proxy selects
the correct path automatically based on the model ID prefix:

| Protocol | Path | Models | Translation |
|----------|------|--------|-------------|
| **OpenAI** | `/v1/chat/completions` | GLM, Kimi, DeepSeek, MiniMax, MiMo, Grok, and free models | Bidirectional Anthropic ↔ OpenAI translation |
| **Anthropic** | `/v1/messages` | Claude and Qwen | Near-transparent passthrough with model ID rewriting only |
| **Responses** | `/v1/responses` | The GPT model family | Anthropic ↔ OpenAI Responses API |
| **Google** | `/v1beta/models/{id}` | Gemini | Anthropic ↔ Google Generative Language |

Each protocol implements the `TranslateRequest`, `TranslateResponse`, and `TranslateStream` methods of the
`upstream.Protocol` interface.

## Features

- **Complete tool-call support.** Anthropic `tool_use` and `tool_result` blocks are translated bidirectionally into
  OpenAI `tool_calls` and `role:"tool"` messages. Claude Code tool definitions are converted into OpenAI function
  tools. The `reasoning_content` extension used by several models is translated into Anthropic `thinking` blocks.
- **Automatic routing across four protocols.** Model IDs beginning with `claude-` or `qwen` use Anthropic,
  `gpt-` uses Responses, `gemini-` uses Google, and all other models use OpenAI. No manual protocol configuration is
  required.
- **A built-in catalog of 49 models.** The Models page displays pricing, context limits, capability tags, and protocol
  badges. Models can be added to mappings directly from the Config page.
- **A single static binary.** The React SPA is embedded with `embed.FS`, so Node.js is not required at runtime.
  SQLite uses a pure-Go driver with no CGO dependency, making clean cross-compilation possible.
- **Web dashboard.** Includes Dashboard for traffic and health data, Models for browsing and filtering, Inspector for
  live request details and protocol routing, and Config for Zen settings, proxy authentication, and hot-reloaded model
  mappings.
- **Panel password protection.** When `panel_token` is set in `config.json` or via the Settings page, the web
  dashboard requires password authentication before granting access. After a successful login an HttpOnly session cookie
  (24-hour TTL) is issued to maintain the session. A logout button is available in the sidebar. When `panel_token` is
  empty, the panel remains open — suitable for local, single-user deployments.
- **Client API key management.** Create, disable, and delete client keys from the dashboard, with per-key total token
  quotas, daily token limits, allowed IPs, and automatic usage tracking.
- **Secure defaults.** Constant-time Bearer token authentication, request body size limits, per-request panic recovery,
  and graceful shutdown.

## Quick Start

### Prerequisites

- Go 1.22+
- Node.js 20+ (only required when building the UI)
- An OpenCode Zen API key. Sign in at [opencode.ai/auth](https://opencode.ai/auth), add billing information, and copy
  your API key.

### Build and Run

```bash
make            # Build the frontend and Go binary as ./opencode-cc
./opencode-cc   # Start on :8787 and create config.json and data/opencode-cc.db
```

Open `http://localhost:8787/` and go to the **Config** tab:

1. Enter your **API key** in the "Upstream (OpenCode Zen)" card.
2. Select "Test connection" to verify connectivity.
3. Adjust the Claude Code-to-Zen model mappings under "Model mappings". Several cost-effective defaults are included,
   such as `claude-sonnet-4-5` → `glm-5.1`.

Then point Claude Code at the proxy:

```bash
export ANTHROPIC_BASE_URL=http://localhost:8787
export ANTHROPIC_AUTH_TOKEN=local    # Any value works when proxy auth is disabled
claude
```

### Development Mode with HMR

```bash
make dev   # Vite on :5174 and Go on :8787, with API requests proxied by Vite
```

### Docker

```bash
make docker
docker run -p 8787:8787 -v $PWD/data:/data opencode-cc
```

## How Translation Works

### Request Routing

When a `POST /v1/messages` request arrives, the proxy looks up `req.Model` in the mapping table to determine the target
Zen model ID. Unmapped model strings are passed through unchanged. It then calls `upstream.Router.For(modelID)` to
select the protocol. All four protocols share a unified output component, `anthropicEmitter`, which emits translated
content blocks as a standard Anthropic SSE event sequence.

### Anthropic ↔ OpenAI Translation

This is the most complex translation path:

| Anthropic | OpenAI Chat Completions |
|-----------|-------------------------|
| `tools[]{name, description, input_schema}` | `tools[]{type:"function", function:{name, description, parameters}}` |
| `content[]{type:"tool_use", id, name, input(object)}` | `assistant.tool_calls[]{id, function:{name, arguments(JSON string)}}` |
| `user.content[]{type:"tool_result", tool_use_id, content}` | `{role:"tool", tool_call_id, content}` |
| `content[]{type:"thinking"}` | `delta.reasoning_content` (DeepSeek/GLM/Kimi extension) |
| `stop_reason:"tool_use"` | `finish_reason:"tool_calls"` |
| Streaming `input_json_delta`, accumulated by index | Streaming `delta.tool_calls[].function.arguments`, accumulated by index |

During streaming, tool-call `arguments` fragments are accumulated by `index`. The completed arguments are then emitted
as a single `tool_use` block, ensuring that Claude Code receives a structurally complete tool call.

### Anthropic Passthrough for Claude and Qwen

Zen exposes a native Anthropic Messages API for Claude and Qwen models. The proxy only rewrites the model ID, while the
request body, response body, and SSE stream are forwarded unchanged. This is the simplest path and requires no tool-call
translation.

### Anthropic ↔ Responses for GPT

Anthropic `messages[]` are translated into a Responses API `input` array containing `message`, `function_call`, and
`function_call_output` item types. Streaming events such as `response.output_text.delta` and
`response.function_call_arguments.delta` are translated into Anthropic `content_block_delta` events.

### Anthropic ↔ Google for Gemini

`messages[]` becomes `contents[]` with `user` and `model` roles, while `tools[]` becomes
`tools[].functionDeclarations`. Gemini returns streaming JSON arrays whose chunks contain
`candidates[].content.parts[]`; the proxy parses these chunks and converts them into Anthropic increments.

## Configuration

`config.json` is created automatically on first launch:

```jsonc
{
  "listen_addr": ":8787",
  "upstream_base": "https://opencode.ai/zen",
  "zen_api_key": "",           // Required Bearer token for the Zen gateway
  "panel_token": "",           // Web dashboard password; empty means open access
  "require_api_key": false,    // When true, /v1/* always requires a valid client API key
  "default_model": "glm-4.6",
  "model_mappings": [
    // Claude Code model string → actual Zen model ID
    { "match": "claude-sonnet-4-5", "target": "glm-5.1" },
    { "match": "*", "target": "" }  // Pass-through fallback
  ],
  "log_requests": true,
  "request_timeout_seconds": 0
}
```

Every field can be edited from the **Config** tab. Saving persists the configuration and hot-reloads the bridge by
rebuilding the upstream client, with no restart required. Model strings without a mapping are forwarded to Zen
unchanged.

## API Endpoints

### Anthropic-Compatible API for Claude Code

| Method | Path | Description |
|--------|------|-------------|
| POST | `/v1/messages` | `stream:true` returns SSE; `stream:false` returns JSON |
| POST | `/v1/messages/count_tokens` | Best-effort token estimation |
| GET | `/healthz` | Liveness probe |

### Dashboard API

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/health` | None | Liveness probe |
| GET | `/api/auth/check` | None | Reports whether login is required and whether the current request is authenticated |
| POST | `/api/auth/login` | None | Verifies the panel password and sets an HttpOnly session cookie on success |
| POST | `/api/auth/logout` | None | Destroys the session and clears the cookie |
| GET | `/api/config` | Required | Current configuration snapshot (Zen API key masked) |
| PUT | `/api/config` | Required | Update, persist, and hot-reload the configuration |
| GET | `/api/stats/summary` | Required | Request count and token totals |
| GET | `/api/stats/hourly` | Required | Hourly time series |
| GET | `/api/stats/models` | Required | Per-model usage breakdown |
| GET | `/api/stats/latency` | Required | P50/P95/P99 latency percentiles |
| GET | `/api/logs` | Required | Request log list |
| GET | `/api/logs/{id}` | Required | Individual log entry with request and response bodies |
| GET/POST | `/api/keys` | Required | List or create API keys |
| GET/PUT/DELETE | `/api/keys/{id}` | Required | Read, update, or delete an API key |
| POST | `/api/keys/{id}/reset` | Required | Reset API key usage counters |
| GET | `/api/keys/{id}/usage` | Required | Read API key usage details |
| GET | `/api/test` | Required | Test upstream connectivity |

## Project Structure

```text
opencode-cc/
├── cmd/opencode-cc/        # Entry point
├── internal/
│   ├── config/             # JSON configuration and hot reload
│   ├── anthropic/          # Anthropic Messages API types and SSE writer
│   ├── upstream/           # Zen client and four protocol translators
│   │   ├── protocol.go     # Protocol interface and router
│   │   ├── anthropic.go    # Anthropic passthrough
│   │   ├── openai.go       # OpenAI Chat Completions translation
│   │   ├── responses.go    # OpenAI Responses translation
│   │   ├── google.go       # Google Gemini translation
│   │   ├── stream_emit.go  # Shared Anthropic SSE output
│   │   ├── models.go       # Catalog of 49 Zen models
│   │   └── client.go       # HTTP client with Bearer auth and GET /v1/models
│   ├── bridge/             # /v1/messages handler: route, translate, forward
│   ├── store/              # SQLite through modernc, with no CGO
│   └── web/                # Dashboard API and embedded SPA
├── web/                    # React, Vite, and Tailwind source
│   └── src/pages/{Dashboard,Inspector,Models,Config}.tsx
└── Dockerfile              # Multi-stage Node.js, Go, and distroless build
```

## Notes

- **Protocol-routing tradeoffs.** Translation from Anthropic to OpenAI, Responses, or Google is lossy because some
  Anthropic-specific concepts, such as `cache_control` and complete `thinking` signatures, have no equivalent in the
  target protocols. The Claude and Qwen passthrough path does not have this limitation.
- **Token usage.** The OpenAI path obtains exact counts from streaming `include_usage` chunks. Other protocols use
  upstream usage fields when available and fall back to estimates otherwise.
- **The model catalog may become outdated.** `internal/upstream/models.go` is a manually curated snapshot. Prices and
  capabilities can change. Use `GET https://opencode.ai/zen/v1/models` with an API key to retrieve the latest catalog.
- **Older SQLite databases are incompatible.** The schema changed during refactoring: `session_map` was removed and the
  `requests` fields changed. During development, delete `data/*.db`. Versioned migrations will be added for production.

## License

MIT
