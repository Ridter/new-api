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
- `router/` - Route definitions (API, relay, dashboard)
- `middleware/` - HTTP middleware chain
  - `distributor.go` - **Critical**: Channel selection and load balancing
  - `auth.go` - Token/user authentication
  - `rate-limit.go` - Request throttling
- `controller/` - HTTP request handlers
  - `relay.go` - Main relay handler routing to appropriate helper
- `service/` - Business logic (quota management, token counting)
- `relay/` - Provider adapters (30+ providers including OpenAI, Claude, Gemini, AWS Bedrock, Azure, etc.)
  - `relay/channel/*/adaptor.go` - Provider-specific request/response transformation
- `model/` - GORM database models and caching
  - `channel_cache.go` - In-memory channel caching
- `dto/` - Data transfer objects for API requests/responses
- `constant/` - Channel type constants and configuration

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

**Adapter Pattern** - Each AI provider has an adapter in `relay/channel/*/adaptor.go` implementing request/response transformation and streaming.

**Middleware Chain** - Requests flow through: CORS → Stats → TokenAuth → RateLimit → Distribute → Controller.

**Channel Selection** - `middleware/distributor.go` uses weighted random algorithm based on channel priority, model availability, user group permissions, and health status.

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

1. Create adapter in `relay/channel/<provider>/adaptor.go`
2. Implement request transformation (OpenAI format → Provider format)
3. Implement response transformation (Provider format → OpenAI format)
4. Handle streaming responses if supported
5. Add provider constant in `constant/channel.go`
6. Register adapter in relay routing logic

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
