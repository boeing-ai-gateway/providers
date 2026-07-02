# Boeing AI Model Provider (BCAI)

Model provider for Boeing Conversational AI (BCAI) via UDAL authentication.
Uses the shared Boeing proxy framework (same pattern as OpenAI/Anthropic providers).

## Architecture

```
Boeing LLM Proxy → Boeing AI Model Provider (localhost:PORT)
                    │
                    ├── GET /v1/models
                    │     → GET /bcai-public-security-api/models (conversation models)
                    │     → GET /bcai-public-security-api/embedding-models
                    │     ← OpenAI /v1/models format response
                    │
                    ├── POST /v1/chat/completions
                    │     → POST /bcai-public-api/bcai-public-api/conversation
                    │     (transforms OpenAI format → BCAI conversation format)
                    │
                    ├── POST /v1/embeddings
                    │     → POST /bcai-public-api/bcai-public-api/embedding
                    │
                    └── GET /validate
                          → GET /bcai-public-security-api/authorized
                          → GET /bcai-public-security-api/models
```

## Per-User Authentication

Each user provides their own **UDAL Personal Access Token (PAT)** which:
- Lasts 90 days
- Uses `Authorization: basic <PAT>` format
- Enables per-user audit trail (Boeing tracks who made each AI request)
- Determines which models the user is authorized to access

### Per-User Token Flow

1. Global API key: Set via `BOEING_AI_MODEL_PROVIDER_API_KEY` env var (fallback)
2. Per-user key: Injected via `X-Boeing-BOEING_AI_MODEL_PROVIDER_API_KEY` header
3. Per-user key takes priority over global key

The `RewriteHeaderFn` converts the standard `Authorization: Bearer <key>` (used by proxy framework) to `Authorization: basic <key>` (required by Boeing AI).

## Integration with Boeing

This provider follows the exact same pattern as OpenAI and Anthropic:

| Component | OpenAI | Anthropic | Boeing AI |
|-----------|--------|-----------|-----------|
| Binary | `openai-model-provider/bin/boeing-provider` | `anthropic-model-provider/bin/boeing-provider` | `boeing-ai-model-provider/bin/boeing-provider` |
| Shared proxy lib | ✅ `proxy.Run(cfg)` | ✅ `proxy.Run(cfg)` | ✅ `proxy.Run(cfg)` |
| Personal key header | `X-Boeing-BOEING_OPENAI_MODEL_PROVIDER_API_KEY` | `X-Boeing-BOEING_ANTHROPIC_MODEL_PROVIDER_API_KEY` | `X-Boeing-BOEING_AI_MODEL_PROVIDER_API_KEY` |
| Auth format | `Bearer <key>` | `x-api-key: <key>` | `basic <PAT>` |
| Custom models handler | No (standard /v1/models) | Yes (rewrite response) | Yes (fetch from security API) |
| Request transform | Minimal (reasoning models) | Yes (thinking mode) | Yes (OpenAI → BCAI format) |
| Validation | GET /models | GET /models | GET /authorized + GET /models |

## Boeing AI API Endpoints

### Security API (`/bcai-public-security-api`)
- `GET /models` — List authorized conversation models
- `GET /embedding-models` — List authorized embedding models
- `GET /authorized` — Check if user is authorized
- `GET /rags` — List authorized RAG data sources
- `GET /models-by-rag?data_source=X` — Models for specific RAG source

### Core AI API (`/bcai-public-api/bcai-public-api`)
- `POST /conversation` — Chat completion (OpenAI-compatible format + BCAI extras)
- `POST /responses` — OpenAI Responses API compatible
- `POST /embedding` — Text embedding

### Token Counter API (`/bcai-public-token-counter-api`)
- `POST /count-tokens` — Count tokens for a conversation

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `BOEING_AI_MODEL_PROVIDER_API_KEY` | No | Default UDAL PAT (per-user keys override) |
| `BOEING_AI_MODEL_PROVIDER_BASE_URL` | No | UDAL base URL (default: `https://udal-test.web.boeing.com`) |
| `PORT` | No | Listen port (default: `8000`) |

## Building

```bash
cd providers/boeing-ai-model-provider
go build -o bin/boeing-provider .
```

## BCAI Request Format (BCAIConversationRequest)

The provider transforms standard OpenAI chat completions requests to BCAI format:

```json
{
  "model": "gpt-4.1-mini",
  "messages": [...],
  "temperature": 0.7,
  "response_max_tokens": 2000,
  "stream": true,
  "conversation_mode": ["non-rag"],
  "conversation_guid": "boeing-session",
  "conversation_source": "boeing",
  "skip_db_save": true,
  "tools": [...],
  "tool_choice": "auto"
}
```
