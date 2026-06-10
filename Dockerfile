# Multi-stage build for Go application with SQLite
FROM golang:1.21-alpine AS builder

# Install build dependencies for CGO and SQLite
RUN apk add --no-cache gcc musl-dev sqlite-dev

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application with CGO enabled for SQLite
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o strangler-fig-proxy ./cmd/strangler-fig-proxy

# Final stage - minimal runtime image
FROM alpine:latest

# Install SQLite runtime
RUN apk add --no-cache sqlite

# Create non-root user
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/strangler-fig-proxy .

# Create directory for database and logs
RUN mkdir -p /app/data && \
    chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/__strangler_fig || exit 1

# Set default environment variables
ENV DATABASE_PATH=/app/data/strangler_fig.db
ENV PORT=8080
ENV SAMPLING_RATE=1.0

# Run the application
CMD ["./strangler-fig-proxy"]
