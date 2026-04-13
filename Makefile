.PHONY: test build deploy clean run lint

# Variables
BINARY_NAME=duq-gateway
BINARY_LINUX=$(BINARY_NAME)-linux
VPS_HOST=root@90.156.230.49
VPS_PATH=/usr/local/bin/duq-gateway

# Default target
all: test build

# Run tests
test:
	go test ./... -v -race -cover

# Run tests with coverage report
test-coverage:
	go test ./... -v -coverprofile=coverage.out -covermode=atomic
	go tool cover -func=coverage.out
	go tool cover -html=coverage.out -o coverage.html

# Build for current platform
build:
	go build -o $(BINARY_NAME) .

# Build for Linux (VPS)
build-linux:
	GOOS=linux GOARCH=amd64 go build -o $(BINARY_LINUX) .

# Run locally
run:
	go run .

# Lint code
lint:
	go vet ./...
	@which golangci-lint > /dev/null && golangci-lint run || echo "golangci-lint not installed"

# Deploy to VPS
deploy: build-linux
	ssh $(VPS_HOST) "systemctl stop duq-gateway || true"
	scp $(BINARY_LINUX) $(VPS_HOST):$(VPS_PATH)
	ssh $(VPS_HOST) "systemctl start duq-gateway"
	@echo "Deployed! Checking status..."
	@sleep 2
	ssh $(VPS_HOST) "systemctl status duq-gateway --no-pager | head -10"

# Deploy and verify
deploy-verify: deploy
	ssh $(VPS_HOST) "curl -s http://localhost:8082/health"

# View logs on VPS
logs:
	ssh $(VPS_HOST) "journalctl -u duq-gateway -n 50 --no-pager"

# View error logs on VPS
logs-err:
	ssh $(VPS_HOST) "journalctl -u duq-gateway -p err -n 30 --no-pager"

# Clean build artifacts
clean:
	rm -f $(BINARY_NAME) $(BINARY_LINUX) coverage.out coverage.html

# Help
help:
	@echo "Available targets:"
	@echo "  test          - Run tests"
	@echo "  test-coverage - Run tests with coverage report"
	@echo "  build         - Build for current platform"
	@echo "  build-linux   - Build for Linux (VPS)"
	@echo "  run           - Run locally"
	@echo "  lint          - Run linters"
	@echo "  deploy        - Build and deploy to VPS"
	@echo "  deploy-verify - Deploy and verify health"
	@echo "  logs          - View VPS logs"
	@echo "  logs-err      - View VPS error logs"
	@echo "  clean         - Remove build artifacts"
