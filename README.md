# gh-webhook-handler

A lightweight, opinionated Go service for centralizing GitHub webhook handling and dispatch. Uses **config-as-code** (YAML in a Git repo) for route registration — fully auditable, version-controlled, and PR-reviewable.

## Architecture

```
┌─────────────┐         ┌──────────────────────────────────┐
│  GitHub App  │────────▶│   gh-webhook-handler (Go)        │
│  (webhooks)  │  POST   │                                  │
└─────────────┘         │  1. Signature validation          │
                        │  2. Event type matching           │
                        │  3. Route lookup (from config)    │
                        │  4. Forward to destinations       │
                        │  5. Record delivery in SQLite     │
                        │  6. Retry on failure (exp backoff)│
                        └──────────────────────────────────┘
```

### How It Works

1. A **GitHub App** is installed on target orgs/repos and configured with this service's URL as its webhook endpoint
2. All webhook events are received at `POST /webhook`
3. The service validates the **HMAC-SHA256 signature** against the App's webhook secret
4. Incoming events are matched against **YAML route definitions** (by org, repo, and event type)
5. Matched events are forwarded to destination URLs with optional outbound HMAC signing
6. Deliveries are tracked in an **embedded SQLite database** for failure recovery
7. A background **retry engine** re-delivers failed events with configurable backoff

## Quick Start

### Prerequisites

- Go 1.22+
- A [GitHub App](https://docs.github.com/en/apps/creating-github-apps) configured with your webhook events

### Build & Run

```bash
# Build
make build

# Run
./bin/gh-webhook-handler \
  --config configs/ \
  --addr :8080 \
  --webhook-secret "$GITHUB_WEBHOOK_SECRET"

# Or via environment variable
export GITHUB_WEBHOOK_SECRET=your-secret
make run
```

### Docker

```bash
docker build -t gh-webhook-handler .
docker run -p 8080:8080 \
  -e GITHUB_WEBHOOK_SECRET=your-secret \
  -v $(pwd)/configs:/app/configs \
  gh-webhook-handler
```

## Configuration

Route definitions are YAML files in a config directory. Each file can contain multiple routes.

```yaml
# configs/team-a.yaml
routes:
  - name: "ci-trigger"
    description: "Forward push and PR events to CI system"
    sources:
      - org: "my-org"
        repos: ["api-service", "web-app"]  # empty = all repos in org
    events:
      - push
      - pull_request
    destination:
      url: "https://ci.team-a.internal/github-webhook"
      secret: "$CI_WEBHOOK_SECRET"  # resolved from environment variable
      headers:
        X-Source: "gh-webhook-handler"
    retry:
      max_attempts: 5
      backoff: "exponential"  # exponential | linear | fixed
```

### Config Fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | Yes | Unique route identifier |
| `description` | No | Human-readable description |
| `sources[].org` | Yes | GitHub organization to match |
| `sources[].repos` | No | Specific repos (empty = all repos in org) |
| `events` | Yes | GitHub event types to match |
| `destination.url` | Yes | URL to forward events to |
| `destination.secret` | No | Secret for outbound HMAC signing (`$VAR` = env var) |
| `destination.headers` | No | Additional HTTP headers |
| `retry.max_attempts` | No | Max retry attempts (default: 3) |
| `retry.backoff` | No | Backoff strategy: `exponential`, `linear`, `fixed` |

### Config Hot-Reload

The service polls the config directory every 30 seconds. When YAML files change, routes are reloaded atomically without restart. Changes are best managed via Git pull requests for audit trail.

## API Endpoints

### Webhook Receiver

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/webhook` | Receives GitHub webhook events |
| `GET` | `/health` | Health check |

### Admin API

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/deliveries` | List deliveries (filters: `route`, `event`, `status`, `limit`, `offset`) |
| `GET` | `/api/deliveries/{id}` | Get delivery details |
| `POST` | `/api/deliveries/{id}/retrigger` | Retrigger a failed delivery |
| `GET` | `/api/routes` | List currently loaded routes |

## Retry & Recovery

Failed deliveries are automatically retried by a background engine:

- **Exponential backoff**: 10s, 20s, 40s, 80s, ... (capped at 1 hour)
- **Linear backoff**: 10s, 20s, 30s, 40s, ... (capped at 1 hour)
- **Fixed backoff**: 30s intervals

The original payload is stored in SQLite, enabling retries even after service restart. Deliveries that exhaust all retry attempts are marked as `permanently_failed` and can be manually retriggered via the admin API.

## GitHub App Setup

1. [Create a GitHub App](https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/registering-a-github-app)
2. Set the **Webhook URL** to your service's `/webhook` endpoint
3. Set a **Webhook secret** and configure the same secret via `--webhook-secret` or `GITHUB_WEBHOOK_SECRET`
4. Subscribe to the events you need
5. Install the App on target organizations/repositories

## Project Structure

```
gh-webhook-handler/
├── cmd/server/main.go           # Entry point
├── internal/
│   ├── config/                  # YAML config parser, loader, watcher
│   ├── webhook/                 # HTTP handler, HMAC signature validation
│   ├── router/                  # Event-to-route matching
│   ├── forwarder/               # HTTP forwarding, outbound signing
│   ├── store/                   # SQLite delivery tracking
│   ├── retry/                   # Background retry engine, backoff strategies
│   ├── admin/                   # Admin REST API
│   └── github/                  # GitHub App auth (JWT, installation tokens)
├── configs/                     # Example route configurations
├── migrations/                  # SQLite schema
├── Dockerfile
├── Makefile
└── .github/workflows/ci.yaml   # CI pipeline
```

## Production Roadmap

This is a reference implementation. For production use, consider adding:

- **Observability**: Structured logging (slog), OpenTelemetry tracing, Prometheus metrics
- **Deployment**: Helm chart, Kubernetes manifests, health/readiness probes
- **Rate limiting**: Per-destination rate limits to protect downstream services
- **Dead letter queue**: Dedicated handling for permanently failed deliveries
- **Secret management**: Integration with HashiCorp Vault or cloud secret managers
- **Horizontal scaling**: Leader election for the retry engine
- **mTLS**: Mutual TLS for internal endpoint communication

## License

MIT
