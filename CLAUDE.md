# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

New API is a next-generation AI model gateway and asset management system built as a fork of [One API](https://github.com/songquanpeng/one-api). It serves as a unified API gateway that aggregates 30+ AI service providers (OpenAI, Claude, Gemini, AWS Bedrock, etc.) into a single OpenAI-compatible interface with advanced management, billing, and routing capabilities.

**Tech Stack:**
- Backend: Go 1.25.1 + Gin framework + GORM
- Frontend: React 18.2.0 + Semi Design + Vite
- Database: SQLite (default), MySQL 5.7.8+, or PostgreSQL 9.6+
- Caching: Redis (optional) or in-memory cache
- Deployment: Single binary with embedded frontend

## Development Commands

### Backend (Go)

```bash
# Run backend development server
go run main.go

# Build backend binary
go build -o new-api main.go

# Run with environment variables
DEBUG=true go run main.go

# Enable profiling
ENABLE_PPROF=true go run main.go
```

### Frontend (React)

```bash
cd web

# Install dependencies (uses Bun)
bun install

# Run development server (proxies to backend on :3000)
bun run dev

# Build for production
bun run build

# Lint code
bun run lint          # Check formatting
bun run lint:fix      # Auto-fix formatting
bun run eslint        # Check ESLint rules
bun run eslint:fix    # Auto-fix ESLint issues

# Preview production build
bun run preview

# i18n management
bun run i18n:extract  # Extract translation keys
bun run i18n:status   # Check translation status
bun run i18n:sync     # Sync translations
bun run i18n:lint     # Lint translation files
```

### Full Stack Development

```bash
# Build frontend and start backend (uses Makefile)
make all

# Or separately:
make build-frontend
make start-backend
```

### Docker

```bash
# Build Docker image
docker build -t new-api:latest .

# Run with Docker Compose
docker-compose up -d

# View logs
docker-compose logs -f

# Stop services
docker-compose down
```

## Architecture

### Backend Structure (Go)

The backend follows a **layered architecture** pattern:

```
HTTP Layer (Gin Router)
    ↓
Middleware Layer (Auth, Rate Limiting, Channel Distribution)
    ↓
Controller Layer (Request validation, orchestration)
    ↓
Service Layer (Business logic)
    ↓
Relay Layer (Provider-specific adapters)
    ↓
Model/Data Layer (GORM, caching)
```

**Key directories:**

- `main.go` - Application entry point, initializes resources and starts server
- `router/` - Route definitions (API routes, relay routes, dashboard)
- `middleware/` - HTTP middleware chain
  - `distributor.go` - **Critical**: Channel selection and load balancing logic
  - `auth.go` - Token/user authentication
  - `rate-limit.go` - Request throttling
- `controller/` - HTTP request handlers
  - `relay.go` - Main relay handler that routes to appropriate helper
  - `channel.go`, `token.go`, `user.go` - CRUD operations
- `service/` - Business logic (quota management, token counting, webhooks)
- `relay/` - Provider adapters (30+ providers)
  - `relay/channel/*/adaptor.go` - Provider-specific request/response transformation
  - Each adapter implements common interface for streaming, error handling
- `model/` - GORM database models and caching
  - `main.go` - Database initialization (supports SQLite, MySQL, PostgreSQL)
  - `channel_cache.go` - In-memory channel caching for performance
- `common/` - Shared utilities (HTTP client, helpers, constants)
- `dto/` - Data transfer objects for API requests/responses
- `types/` - Type definitions and enums
- `setting/` - Configuration management with hot-reload

### Frontend Structure (React)

```
web/src/
├── App.jsx              # Main routing component
├── index.jsx            # Application entry point
├── pages/               # Page-level components (24 pages)
│   ├── Dashboard.jsx    # Admin dashboard
│   ├── Channel.jsx      # Channel management
│   ├── Token.jsx        # API token management
│   ├── Log.jsx          # Request logs
│   └── Playground.jsx   # API testing interface
├── components/          # Reusable UI components
├── context/             # React Context for global state
├── helpers/             # Utility functions and route guards
├── hooks/               # Custom React hooks
├── services/            # API client layer
└── i18n/                # Internationalization (CN, EN, FR, JP)
```

**Frontend patterns:**
- Lazy loading with code splitting for pages
- Route protection with PrivateRoute/AdminRoute wrappers
- Context API for global state (user status, settings)
- Semi Design UI library for components

### Data Flow Example (Chat Completion)

1. Client sends `POST /v1/chat/completions`
2. Middleware chain:
   - `CORS()` - Handle cross-origin requests
   - `TokenAuth()` - Validate API token from database
   - `ModelRequestRateLimit()` - Check user/model rate limits
   - `Distribute()` - Select appropriate channel based on model, user group, channel weight
3. Controller `Relay()` routes to `TextHelper()` in relay layer
4. Relay layer:
   - Transforms request to provider-specific format (e.g., OpenAI → Claude Messages)
   - Sends HTTP request to upstream provider
   - Streams or buffers response
   - Transforms response back to OpenAI format
5. Logging records usage, quota consumption to database
6. Response returned to client

### Key Architectural Patterns

**Adapter Pattern** - Each AI provider has an adapter in `relay/channel/*/adaptor.go` that implements request/response transformation, streaming, and error handling.

**Middleware Chain** - Requests flow through a pipeline: CORS → Stats → TokenAuth → RateLimit → Distribute → Controller.

**Repository Pattern** - Database access abstracted through GORM models with caching layer (`model.GetChannelById()`, `model.CacheGetRandomSatisfiedChannel()`).

**Strategy Pattern** - Dynamic channel selection based on model availability, channel priority/weight, user group permissions, and health status.

**Embedded Resources** - Frontend built as embedded filesystem (`//go:embed web/dist`) for single binary deployment.

## Configuration

### Environment Variables

Key environment variables (see [.env.example](.env.example) for full list):

**Database:**
- `SQL_DSN` - Database connection string (MySQL/PostgreSQL)
- `LOG_SQL_DSN` - Separate database for logs (optional)
- `SQLITE_PATH` - SQLite database path (default: `./data/new-api.db`)

**Caching:**
- `REDIS_CONN_STRING` - Redis connection string (recommended for production)
- `MEMORY_CACHE_ENABLED` - Enable in-memory caching (default: true)
- `CHANNEL_UPDATE_FREQUENCY` - Channel cache refresh interval in seconds

**Security:**
- `SESSION_SECRET` - **Required for multi-instance deployment**
- `CRYPTO_SECRET` - **Required when using Redis**

**Timeouts:**
- `RELAY_TIMEOUT` - All request timeout in seconds (0 = unlimited)
- `STREAMING_TIMEOUT` - Stream mode timeout in seconds (default: 300)

**Features:**
- `DEBUG` - Enable debug logging
- `ENABLE_PPROF` - Enable pprof profiling endpoint
- `ERROR_LOG_ENABLED` - Enable error logging (default: false)

### Database Setup

**SQLite (default):**
```bash
# No setup needed, database created automatically at ./data/new-api.db
```

**MySQL:**
```bash
# Set environment variable
export SQL_DSN="user:password@tcp(localhost:3306)/dbname?parseTime=true"
```

**PostgreSQL:**
```bash
export SQL_DSN="host=localhost user=postgres password=secret dbname=newapi port=5432 sslmode=disable"
```

## Important Implementation Details

### Channel System

Channels represent upstream AI service providers. The channel selection logic in [middleware/distributor.go](middleware/distributor.go) is critical:

- Channels are cached in memory for performance
- Selection uses weighted random algorithm based on channel priority
- Automatic failover and retry on channel failure
- Model-specific channel mapping (e.g., `gpt-4` → OpenAI channel)
- User group-based access control

### Relay Adapters

When adding support for a new AI provider:

1. Create adapter in `relay/channel/<provider>/adaptor.go`
2. Implement request transformation (OpenAI format → Provider format)
3. Implement response transformation (Provider format → OpenAI format)
4. Handle streaming responses if supported
5. Add error handling and retry logic
6. Register adapter in relay routing logic

### Billing & Quota

- Token-based quota system (users have quota balance)
- Per-model pricing configured in database (`model.Pricing`)
- Supports prompt caching billing (OpenAI, Claude, DeepSeek, etc.)
- Token counting uses tiktoken-go library
- Quota deducted after successful request completion

### Authentication Flow

1. User logs in via `/api/user/login` (username/password, OAuth2, WebAuthn)
2. Server creates session with JWT token
3. Client stores token and sends in `Authorization: Bearer <token>` header
4. `TokenAuth()` middleware validates token on each request
5. Token has associated user, quota, and model permissions

### Multi-Instance Deployment

For running multiple instances:

1. **Must set** `SESSION_SECRET` - ensures consistent session encryption across instances
2. **Must set** `CRYPTO_SECRET` if using shared Redis - ensures data can be decrypted
3. Use shared database (MySQL/PostgreSQL, not SQLite)
4. Use Redis for caching and session storage
5. Use load balancer in front of instances

## Testing

The project has minimal test coverage. Existing tests:

```bash
# Run Go tests
go test ./...

# Run specific test file
go test ./controller -run TestChannelTest
```

Test files found:
- [controller/misc.go](controller/misc.go) - Miscellaneous tests
- [controller/channel-test.go](controller/channel-test.go) - Channel testing logic

## Common Development Workflows

### Adding a New API Endpoint

1. Define route in `router/api-router.go` or `router/relay-router.go`
2. Create handler in appropriate controller file
3. Add DTO in `dto/` if needed
4. Implement business logic in `service/` if complex
5. Update frontend API client in `web/src/services/`

### Adding a New AI Provider

1. Create adapter directory: `relay/channel/<provider>/`
2. Implement `adaptor.go` with request/response transformation
3. Add provider constant in `constant/channel.go`
4. Register adapter in relay routing logic
5. Add model mappings in database
6. Test with channel testing tool in admin dashboard

### Modifying Database Schema

1. Update model in `model/*.go`
2. GORM auto-migrates on startup (see `model/main.go`)
3. For complex migrations, add migration logic in `model/main.go`
4. Test with SQLite first, then MySQL/PostgreSQL

### Adding Frontend Features

1. Create component in `web/src/components/` or page in `web/src/pages/`
2. Add route in `web/src/App.jsx` if new page
3. Use Semi Design components for UI consistency
4. Add translations in `web/src/i18n/locales/`
5. Build frontend: `cd web && bun run build`
6. Rebuild backend to embed new frontend

## Documentation

- Official docs: https://docs.newapi.pro/
- API documentation: https://docs.newapi.pro/api
- Environment variables: https://docs.newapi.pro/installation/environment-variables
- FAQ: https://docs.newapi.pro/support/faq

## Notes

- The project uses Bun for frontend package management (not npm/yarn)
- Frontend is embedded in Go binary via `//go:embed web/dist`
- Default admin credentials created on first run (check logs)
- Channel health monitoring runs automatically in background
- Configuration hot-reloads via `model.SyncOptions()` goroutine
- LinuxDO OAuth endpoints are hardcoded in `.env.example`
