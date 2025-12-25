# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

New API is a next-generation AI model gateway and asset management system built as a fork of [One API](https://github.com/songquanpeng/one-api). It serves as a unified API gateway that aggregates 30+ AI service providers into a single OpenAI-compatible interface with advanced management, billing, and routing capabilities.

**Tech Stack:**
- Backend: Go + Gin framework + GORM
- Frontend: React 18 + Semi Design + Vite + TailwindCSS
- Database: SQLite (default), MySQL 5.7.8+, or PostgreSQL 9.6+
- Caching: Redis (optional) or in-memory cache
- Deployment: Single binary with embedded frontend (default port: 3000)

## Development Commands

### Backend (Go)

```bash
go run main.go                    # Run development server
go build -o new-api main.go       # Build binary
DEBUG=true go run main.go         # Enable debug logging
go test ./...                     # Run tests (minimal coverage)
go test -v ./path/to/package      # Run tests for specific package
go test -run TestName ./...       # Run specific test by name
```

### Frontend (React)

```bash
cd web
bun install                       # Install dependencies
bun run dev                       # Dev server (proxies to backend :3000)
bun run build                     # Production build
bun run lint && bun run eslint    # Check formatting and linting
bun run lint:fix && bun run eslint:fix  # Auto-fix issues
```

### Full Stack

```bash
make all              # Build frontend and start backend
make build-frontend   # Build frontend only
make start-backend    # Start backend only
```

### Docker

```bash
docker build -t new-api:latest .
docker-compose up -d
```

## Architecture

### Request Flow

```
HTTP Request → Gin Router → Middleware Chain → Controller → Relay Layer → Upstream Provider
                              ↓
                    TokenAuth → RateLimit → Distribute (channel selection)
```

### Backend Structure

- `main.go` - Entry point, initializes resources and starts server
- `router/` - Route definitions (API, relay, dashboard, video)
- `middleware/` - HTTP middleware chain
  - `distributor.go` - **Critical**: Channel selection and load balancing
  - `auth.go` - Token/user authentication
  - `rate-limit.go` - Request throttling
- `controller/` - HTTP request handlers
  - `relay.go` - Main relay handler routing to appropriate helper based on RelayMode
- `service/` - Business logic
  - `channel_select.go` - Channel selection algorithm (weighted random)
  - `quota.go` - Quota management and billing
  - `convert.go` - Format conversion (Claude ⇄ OpenAI, Gemini ⇄ OpenAI)
  - `token_counter.go` - Token counting with tiktoken-go
- `relay/` - Provider adapters (30+ providers)
  - `relay/channel/adapter.go` - Core `Adaptor` and `TaskAdaptor` interfaces
  - `relay/channel/*/adaptor.go` - Provider-specific implementations
- `model/` - GORM database models and caching
- `dto/` - Data transfer objects for API requests/responses
- `constant/channel.go` - Channel type constants (ChannelTypeOpenAI, ChannelTypeAnthropic, etc.)

### Frontend Structure

```
web/src/
├── App.jsx          # Main routing
├── pages/           # Page components (Dashboard, Channel, Token, Log, Playground)
├── components/      # Reusable UI components
├── context/         # React Context for global state
├── services/        # API client layer
└── i18n/            # Internationalization (CN, EN, FR, JP)
```

### Key Patterns

**Adaptor Interface** - Core interface in `relay/channel/adapter.go`:
- `Adaptor` - For synchronous requests (chat, embeddings, audio, images)
- `TaskAdaptor` - For async tasks (video generation, Midjourney, Suno)

**Middleware Chain** - Requests flow through: CORS → Stats → TokenAuth → RateLimit → Distribute → Controller.

**Channel Selection** - `service/channel_select.go` uses weighted random algorithm based on channel priority, model availability, user group permissions, and health status.

**Format Conversion** - Automatic conversion between API formats:
- Claude Messages ⇄ OpenAI Chat Completions
- Gemini ⇄ OpenAI
- Supports thinking/reasoning mode conversion

**Embedded Resources** - Frontend built as embedded filesystem (`//go:embed web/dist`) for single binary deployment.

## Configuration

### Key Environment Variables

**Database:**
- `SQL_DSN` - MySQL/PostgreSQL connection string
- `SQLITE_PATH` - SQLite database path (default: `./data/new-api.db`)

**Caching:**
- `REDIS_CONN_STRING` - Redis connection (recommended for production)
- `MEMORY_CACHE_ENABLED` - Enable in-memory caching (default: true)

**Security (required for multi-instance):**
- `SESSION_SECRET` - Session encryption key
- `CRYPTO_SECRET` - Required when using Redis

**Timeouts:**
- `RELAY_TIMEOUT` - Request timeout in seconds (0 = unlimited)
- `STREAMING_TIMEOUT` - Stream mode timeout (default: 300s)

## Implementation Details

### Adding a New AI Provider

1. Add channel type constant in `constant/channel.go`:
   - Add `ChannelType<Provider>` constant with next available ID
   - Add base URL to `ChannelBaseURLs` array
   - Add name to `ChannelTypeNames` map

2. Create adapter in `relay/channel/<provider>/adaptor.go`:
   - Implement the `Adaptor` interface from `relay/channel/adapter.go`
   - Key methods: `Init`, `GetRequestURL`, `SetupRequestHeader`, `ConvertOpenAIRequest`, `DoRequest`, `DoResponse`
   - For async tasks (video, music), implement `TaskAdaptor` instead

3. Register adapter in `relay/channel/adapter_selector.go` (or equivalent routing logic)

4. Add provider-specific constants in `relay/channel/<provider>/constants.go`:
   - Model lists, API versions, special configurations

### Adding a New API Endpoint

1. Define route in `router/api-router.go` or `router/relay-router.go`
2. Create handler in appropriate controller file
3. Add DTO in `dto/` if needed
4. Update frontend API client in `web/src/services/`

### Database Schema Changes

GORM auto-migrates on startup. Update models in `model/*.go` and restart.

### Billing & Quota

- Token-based quota system with per-model pricing in `model.Pricing`
- Supports prompt caching billing (OpenAI, Claude, DeepSeek)
- Token counting uses tiktoken-go library
- Quota deducted after successful request completion
