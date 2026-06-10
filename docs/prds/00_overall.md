# Strangler Fig Reverse Proxy - Product Requirements Document

## 1. Executive Summary

### 1.1 Purpose
This document outlines the requirements for a strangler fig reverse proxy system designed to facilitate gradual migration from a legacy system to a new version. The proxy will route requests to both systems, compare responses, and provide insights into behavioral differences between versions.

### 1.2 Goals
- Enable zero-downtime migration between service versions
- Identify discrepancies between legacy and new systems before cutover
- Provide configurable traffic routing based on URL patterns
- Offer visibility into system behavior through an internal dashboard

## 2. System Overview

### 2.1 Core Functionality
The reverse proxy acts as an intermediary that:
1. Receives all incoming HTTP requests
2. Forwards requests to both the main (legacy) server and the new version server
3. Returns the response from the main server to the client
4. Compares responses from both servers
5. Logs all requests, responses, and comparison results to a SQLite database
6. Provides an internal dashboard for monitoring differences

### 2.2 Architecture Diagram
```
Client Request → Strangler Fig Proxy → Main Server (returns response)
                         ↓
                    New Server (response compared)
                         ↓
                   SQLite Database
                         ↓
                  Internal Dashboard
```

## 3. Functional Requirements

### 3.1 Request Handling
- **FR-1.1**: Accept all HTTP methods (GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS, etc.)
- **FR-1.2**: Forward requests with all headers, query parameters, and body content intact
- **FR-1.3**: Support standard request/response patterns (non-streaming)
- **FR-1.4**: Maintain transparent authentication passthrough
- **FR-1.5**: Preserve query strings exactly as received for forwarding
- **FR-1.6**: Handle all content types for POST/PUT/PATCH bodies (JSON, form data, multipart, etc.)
- **FR-1.7**: Forward raw request body without modification
- **FR-1.8**: Normalize URL paths before storage (clean path without query parameters)

### 3.2 Response Processing
- **FR-2.1**: Always return the main server's response to the client
- **FR-2.2**: Capture both servers' responses for comparison
- **FR-2.3**: Perform exact byte-for-byte comparison of responses
- **FR-2.4**: Handle new server errors gracefully (errors should not affect client response)

### 3.3 Data Storage
- **FR-3.1**: Store in SQLite database:
  - Request details (method, normalized URL path, query parameters as separate field, headers, body)
  - Main server response (status, headers, body)
  - New server response (status, headers, body)
  - Comparison result (match/mismatch)
  - Timestamps
  - Response times for both servers
- **FR-3.2**: Implement configurable sampling rate for request logging (e.g., log 10% of requests)
- **FR-3.3**: Normalize URLs before storage (remove double slashes, resolve . and .., etc.)

### 3.4 Routing Configuration
- **FR-4.1**: Configure static URL prefixes for routing specific requests to new server
- **FR-4.2**: Default behavior: return main server response unless URL starts with configured prefix
- **FR-4.3**: Configuration via environment variable with comma-separated values

### 3.5 Internal Dashboard
- **FR-5.1**: Accessible at `/__strangler_fig` endpoint
- **FR-5.2**: Display:
  - Total requests processed
  - Match/mismatch statistics
  - Recent mismatches with details
  - Response time comparisons
  - Filter by URL pattern, time range, match status
- **FR-5.3**: Diff viewer for response comparisons
- **FR-5.4**: No authentication required (internal use only)

## 4. Non-Functional Requirements

### 4.1 Performance
- **NFR-1.1**: Minimal latency overhead (<10ms added to request processing)
- **NFR-1.2**: Asynchronous forwarding to new server (non-blocking)
- **NFR-1.3**: Efficient database writes (batch inserts where possible)

### 4.2 Reliability
- **NFR-2.1**: Continue operation if new server is unavailable or returns error
- **NFR-2.2**: Database errors should not affect proxy functionality
- **NFR-2.3**: Log new server failures without affecting main server response

### 4.3 Scalability
- **NFR-3.1**: Handle concurrent requests efficiently using Go routines
- **NFR-3.2**: SQLite WAL mode for concurrent reads/writes
- **NFR-3.3**: Configurable connection pooling

### 4.4 Observability
- **NFR-4.1**: Structured logging (JSON format)

## 5. Technical Specifications

### 5.1 Technology Stack
- **Language**: Go (Golang)
- **HTTP Framework**: Standard library httputil.ReverseProxy
- **Database**: SQLite with WAL mode
- **Frontend**: Simple HTML/JavaScript for dashboard (embedded in binary)
- **Deployment**: Docker container with multi-stage build

### 5.2 Database Schema
```sql
CREATE TABLE requests (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    method TEXT NOT NULL,
    url_path TEXT NOT NULL,           -- Normalized URL path without query string
    query_params TEXT,                 -- Query string parameters
    request_headers TEXT,
    request_body TEXT,
    main_status INTEGER,
    main_headers TEXT,
    main_body TEXT,
    main_response_time_ms INTEGER,
    new_status INTEGER,
    new_headers TEXT,
    new_body TEXT,
    new_response_time_ms INTEGER,
    responses_match BOOLEAN,
    mismatch_type TEXT -- 'status', 'headers', 'body', or NULL
);

CREATE INDEX idx_timestamp ON requests(timestamp);
CREATE INDEX idx_url_path ON requests(url_path);
CREATE INDEX idx_responses_match ON requests(responses_match);
```

### 5.3 Configuration Format
Environment variables:
```bash
# Required
MAIN_SERVER_URL=http://legacy-service:8080
NEW_SERVER_URL=http://new-service:8080

# Optional with defaults
SAMPLING_RATE=0.1                    # Log 10% of requests (default: 1.0)
DATABASE_PATH=./strangler_fig.db     # SQLite file location (default: ./strangler_fig.db)
DATABASE_MAX_SIZE_MB=1000           # Max database size (default: 1000)
DATABASE_RETENTION_DAYS=7           # Data retention period (default: 7)
PORT=8080                          # Proxy listen port (default: 8080)

# Routing configuration (comma-separated prefixes)
NEW_SERVER_ROUTES=/api/v2,/health  # URL prefixes for new server (default: empty)
```

## 6. Testing Strategy

### 6.1 Testing Approach
Given the proxy nature of the system, integration tests will be the primary testing method, with unit tests where applicable for isolated functions.

### 6.2 Integration Tests
- **IT-1**: End-to-end request flow
  - Start test instances of main and new servers
  - Send various HTTP methods through proxy
  - Verify correct response returned
  - Verify both servers received requests
  - Check database for logged entries

- **IT-2**: Response comparison scenarios
  - Test with identical responses (should mark as match)
  - Test with different response bodies (should mark as mismatch)
  - Test with different status codes
  - Test with different headers

- **IT-3**: Routing logic
  - Test requests matching NEW_SERVER_ROUTES prefixes
  - Test requests not matching prefixes
  - Test empty routing configuration

- **IT-4**: Error handling
  - Test main server unavailable (should fail)
  - Test new server unavailable (should continue)
  - Test database write failures (should continue)

- **IT-5**: POST data and query strings
  - Test various content types (JSON, form data, multipart)
  - Test query string preservation
  - Test large request bodies

### 6.3 Unit Tests
- **UT-1**: URL prefix matching logic
- **UT-2**: Environment variable parsing
- **UT-3**: Sampling rate logic
- **UT-4**: Response comparison function
- **UT-5**: URL normalization (path.Clean, etc.)

### 6.4 Test Infrastructure
```go
// Test utilities
- httptest servers for main and new backends
- In-memory SQLite for database tests
- Test fixtures for various request/response types
```

## 7. Implementation Phases

### Phase 1: Core Proxy (MVP)
- Basic proxy using httputil.ReverseProxy
- Request logging to SQLite
- Concurrent forwarding to new server
- Response comparison and storage
- Comprehensive integration test suite

### Phase 2: Dashboard
- Internal web interface at `/__strangler_fig`
- Basic statistics and mismatch viewing

### Phase 3: Advanced Routing
- URL pattern-based routing to new server
- Percentage-based traffic splitting per route prefix (`NEW_SERVER_ROUTES=/api/v2=25`)
- Runtime routing table updates via `/__strangler_fig/api/routes` (GET/PUT) and the dashboard, no restart required
- Automatic fallback to the main server when a request routed to the new server fails
- `served_by` tracking and migration progress stat in the dashboard

### Phase 4: Production Hardening (in progress)
- Database maintenance tasks: hourly enforcement of `DATABASE_RETENTION_DAYS` and `DATABASE_MAX_SIZE_MB` (done)
- Per-path statistics in the dashboard: counts, match %, traffic split, avg response times (done)
- Response diff viewer at `/__strangler_fig/requests/{id}` with side-by-side line diff, JSON pretty-printed before diffing (done)
- Structured JSON logging via log/slog (done)
- Proxy correctness: hop-by-hop headers stripped, X-Forwarded-For/Host/Proto set, shared HTTP transport with connection pooling, comparison request mirrored in the background (done)
- Response transformation capabilities (pending)

### Phase 5: Agent-Driven Migration (in progress)
- JSON stats API at `/__strangler_fig/api/stats`: overall progress, routing table, per-path work queue (done)
- Request records API at `/__strangler_fig/api/requests`: recorded request/response pairs filterable by path, match result, method (done)
- Hurl test-suite export at `/__strangler_fig/api/tests.hurl`: recorded GET/HEAD traffic as runnable regression tests asserting legacy behavior; JSON bodies flattened to structural jsonpath asserts, other bodies byte-exact (done)
- Write-endpoint verification strategy (pending; candidates: DB-call instrumentation comparison, transaction dry-runs, shadow databases)
- Auto-promotion/rollback policy in the proxy (pending)
- Packaged agent runner + CLI (pending)

## 8. Success Criteria

1. **Zero Impact**: Proxy adds minimal latency to requests
2. **Accuracy**: 100% of configured requests are compared
3. **Visibility**: All mismatches are easily identifiable
4. **Reliability**: Continues operating even when new server fails
5. **Migration Success**: Ability to gradually shift traffic with confidence
6. **Test Coverage**: >80% code coverage through integration tests

## 8. Future Considerations

- Response transformation rules for known differences
- WebSocket support
- Distributed tracing integration
- Multi-environment support
- A/B testing capabilities
- Automated mismatch analysis with ML

## 9. Glossary

- **Strangler Fig Pattern**: A gradual migration approach where new functionality slowly replaces old
- **Main Server**: The current production server (legacy)
- **New Server**: The replacement server being validated
- **Mismatch**: When responses from main and new servers differ
