.PHONY: build test run clean deps

# Build the proxy binary
build:
	go build -o bin/strangler-fig-proxy ./cmd/strangler-fig-proxy

# Run tests
test:
	go test -v ./...

# Run the proxy with default settings
run:
	go run ./cmd/strangler-fig-proxy

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
	go run ./cmd/strangler-fig-proxy

# Build for production
build-prod:
	CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o bin/strangler-fig-proxy ./cmd/strangler-fig-proxy

# Docker targets
docker-build:
	docker build -t strangler-fig-proxy:latest .

docker-run:
	docker run -p 8080:8080 \
		-e MAIN_SERVER_URL=http://host.docker.internal:8081 \
		-e NEW_SERVER_URL=http://host.docker.internal:8082 \
		-e SAMPLING_RATE=1.0 \
		-v $(PWD)/data:/app/data \
		strangler-fig-proxy:latest

docker-run-example:
	docker run -p 8080:8080 \
		-e MAIN_SERVER_URL=http://httpbin.org \
		-e NEW_SERVER_URL=http://httpbin.org \
		-e SAMPLING_RATE=1.0 \
		strangler-fig-proxy:latest

docker-stop:
	docker stop $$(docker ps -q --filter ancestor=strangler-fig-proxy:latest)

docker-clean:
	docker rmi strangler-fig-proxy:latest
