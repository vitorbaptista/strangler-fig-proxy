.PHONY: build test run clean deps

# Build the proxy binary
build:
	go build -o bin/strangler-fix-proxy ./cmd/strangler-fix-proxy

# Run tests
test:
	go test -v ./test/...

# Run the proxy with default settings
run:
	go run ./cmd/strangler-fix-proxy

# Clean build artifacts
clean:
	rm -rf bin/
	rm -f *.db

# Download dependencies
deps:
	go mod download
	go mod tidy

# Install dependencies and build
install: deps build

# Run with example configuration
run-example:
	MAIN_SERVER_URL=http://httpbin.org \
	NEW_SERVER_URL=http://httpbin.org \
	SAMPLING_RATE=1.0 \
	PORT=8080 \
	go run ./cmd/strangler-fix-proxy

# Build for production
build-prod:
	CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o bin/strangler-fix-proxy ./cmd/strangler-fix-proxy