# Strangler Fig Reverse Proxy

[![Test](https://github.com/vitorbaptista/strangler-fig-proxy/actions/workflows/test.yml/badge.svg)](https://github.com/vitorbaptista/strangler-fig-proxy/actions/workflows/test.yml)

A reverse proxy designed for zero-downtime migration between service versions using the strangler fig pattern. Routes requests to both legacy and new systems, compares responses, and provides visibility into behavioral differences.

## Features

- **Zero-downtime migration** - Gradual traffic shifting between service versions
- **Response comparison** - Automatic detection of behavioral differences
- **Configurable routing** - Route specific URL patterns to new service
- **Built-in dashboard** - Monitor differences and system behavior at `/__strangler_fig`
- **High performance** - Minimal latency overhead with concurrent processing
- **Configurable sampling** - Control what percentage of requests to log and compare

## Quick Start

### Option 1: Docker (Recommended)

```bash
docker run -p 8080:8080 \
  -e MAIN_SERVER_URL=http://legacy-service:8080 \
  -e NEW_SERVER_URL=http://new-service:8080 \
  strangler-fig-proxy
```

### Option 2: Build from Source

#### Prerequisites
- Go 1.21 or higher
- SQLite3 (for CGO compilation)

#### Installation
```bash
git clone <repository-url>
cd strangler-fig-proxy
make build
```

#### Running
```bash
export MAIN_SERVER_URL=http://legacy-service:8080
export NEW_SERVER_URL=http://new-service:8080
./bin/strangler-fig-proxy
```

### Quick Demo
```bash
# Run with example servers
make run-example
```

## Configuration

### Required Environment Variables
- `MAIN_SERVER_URL` - Legacy server URL
- `NEW_SERVER_URL` - New server URL being validated

### Optional Configuration
| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | Proxy listen port | `8080` |
| `SAMPLING_RATE` | Fraction of requests to log (0.0-1.0) | `1.0` |
| `DATABASE_PATH` | SQLite database file path | `./strangler_fig.db` |
| `DATABASE_MAX_SIZE_MB` | Maximum database size | `1000` |
| `DATABASE_RETENTION_DAYS` | Data retention period | `7` |
| `NEW_SERVER_ROUTES` | Comma-separated URL prefixes to route to new server, optionally with a traffic percentage (`/api/v2=25`) | - |

### Example Configuration
```bash
export MAIN_SERVER_URL=http://legacy-api:8080
export NEW_SERVER_URL=http://new-api:8080
export PORT=8080
export SAMPLING_RATE=0.1
# /api/v2: 25% of traffic served by the new server; /health: 100%
export NEW_SERVER_ROUTES=/api/v2=25,/health
```

## How It Works

1. **Request Reception** - Proxy receives all incoming HTTP requests
2. **Dual Forwarding** - Forwards requests to both main and new servers
3. **Response Handling** - Returns appropriate server response to client
4. **Comparison** - Compares responses from both servers asynchronously
5. **Logging** - Stores request/response data and comparison results in SQLite
6. **Monitoring** - Provides dashboard for analyzing differences

### Routing Logic
- **Default**: Returns main server response, logs comparison with new server
- **With `NEW_SERVER_ROUTES`**: For matching URL prefixes, returns new server response and logs comparison with main server
- **Percentage splitting**: A route like `/api/v2=25` serves 25% of matching requests from the new server and the rest from the main server, so traffic can be shifted gradually as confidence grows
- **Fallback**: If a request routed to the new server fails, the proxy transparently serves the main server's response instead
- **Sampling**: Only logs the configured percentage of requests to reduce overhead

### Gradual Migration Workflow

1. Deploy the proxy in front of your existing app with no routes configured. All traffic is served by the old app while every response is compared against the new one.
2. Watch the dashboard until a path's responses consistently match.
3. Start shifting traffic for that path: `/api/users=10`, then `25`, `50`, `100` — either via `NEW_SERVER_ROUTES` or live through the dashboard / routes API (no restart needed).
4. Repeat per path until the new app serves 100% of traffic, then remove the proxy and the old codebase.

#### Routes API

The routing table can be inspected and changed at runtime:

```bash
# View current routes
curl http://localhost:8080/__strangler_fig/api/routes

# Serve 50% of /api/v2 traffic from the new server, 100% of /health
curl -X PUT http://localhost:8080/__strangler_fig/api/routes \
  -H 'Content-Type: application/json' \
  -d '[{"prefix": "/api/v2", "percentage": 50}, {"prefix": "/health", "percentage": 100}]'
```

## Dashboard

Access the monitoring dashboard at:
```
http://localhost:8080/__strangler_fig
```

View:
- Request statistics and match/mismatch ratios
- Migration progress (% of traffic served by the new server)
- Per-path statistics: request counts, match %, traffic split, and average response times for each path — so you can see which paths are safe to migrate
- Recent requests, each linking to a detail page with a side-by-side response diff (JSON bodies are pretty-printed before diffing)
- Response time metrics
- Live routing table editor — change traffic percentages without restarting

## Docker Usage

### Build Docker Image
```bash
docker build -t strangler-fig-proxy .
```

### Run with Docker
```bash
docker run -p 8080:8080 \
  -e MAIN_SERVER_URL=http://legacy-service:8080 \
  -e NEW_SERVER_URL=http://new-service:8080 \
  -e SAMPLING_RATE=0.1 \
  -v $(pwd)/data:/app/data \
  -e DATABASE_PATH=/app/data/strangler_fig.db \
  strangler-fig-proxy
```

### Docker Compose Example
```yaml
version: '3.8'
services:
  strangler-proxy:
    image: strangler-fig-proxy
    ports:
      - "8080:8080"
    environment:
      - MAIN_SERVER_URL=http://legacy-service:8080
      - NEW_SERVER_URL=http://new-service:8080
      - SAMPLING_RATE=0.1
      - NEW_SERVER_ROUTES=/api/v2,/health
    volumes:
      - ./data:/app/data
```

## CLI Usage

### Available Make Commands
```bash
make build          # Build binary to bin/
make test           # Run integration tests
make run            # Run with go run
make run-example    # Demo with httpbin.org
make build-prod     # Production build with optimizations
make clean          # Clean build artifacts
make deps           # Download dependencies
```

### Manual Build
```bash
# Download dependencies
go mod download

# Build for current platform
go build -o bin/strangler-fig-proxy ./cmd/strangler-fig-proxy

# Build for production (Linux)
CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o bin/strangler-fig-proxy ./cmd/strangler-fig-proxy
```

## Testing

### Run All Tests
```bash
make test
```

### Run Specific Tests
```bash
go test -v ./test/... -run TestBasicProxyFlow
```

### Test Coverage
```bash
go test -cover ./...
```

## Use Cases

- **Service Migration** - Gradually migrate traffic from legacy to new service
- **A/B Testing** - Compare behavior between service versions
- **Regression Testing** - Validate new deployments against production traffic
- **Performance Monitoring** - Compare response times between versions
- **Canary Deployments** - Route subset of traffic to new version

## Database Schema

The proxy stores request/response data in SQLite with the following structure:
- Request details (method, path, headers, body)
- Response data from both servers (status, headers, body, timing)
- Comparison results (match/mismatch type)
- Timestamps and performance metrics

## Troubleshooting

### Common Issues

**Proxy won't start**
- Check that `MAIN_SERVER_URL` and `NEW_SERVER_URL` are set
- Verify port is not already in use
- Ensure SQLite permissions for database file

**High memory usage**
- Reduce `SAMPLING_RATE` to log fewer requests
- Check `DATABASE_MAX_SIZE_MB` setting
- Monitor database size and retention

**Response mismatches**
- Check dashboard at `/__strangler_fig` for details
- Verify both servers are receiving identical requests
- Consider response timing differences

## Contributing

1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Ensure all tests pass with `make test`
5. Submit a pull request

## Support

For issues and questions, please use the GitHub issue tracker.
