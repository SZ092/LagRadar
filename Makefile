# Makefile for LagRadar

# Variables
APP_NAME := lagradar
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GO_VERSION := $(shell go version | awk '{print $$3}')

# Go build flags
LDFLAGS := -ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.GitCommit=${GIT_COMMIT}"


# Directories
BUILD_DIR := ./build
DIST_DIR := ./dist
CMD_DIR := ./cmd

# Default target
.DEFAULT_GOAL := help


## build: Build the application binary
.PHONY: build
build:
	@echo "Building ${APP_NAME}..."
	@mkdir -p ${BUILD_DIR}
	@go build ${LDFLAGS} -o ${BUILD_DIR}/${APP_NAME} ./cmd
	@echo "Built ${BUILD_DIR}/${APP_NAME}"

## test: Run tests
.PHONY: test
test:
	@echo "Running tests..."
	@go test -v -race -coverprofile=coverage.txt -covermode=atomic ./...

## lint: Run linters
.PHONY: lint
lint:
	@echo "Running linters..."
	@if ! command -v golangci-lint &> /dev/null; then \
		echo "golangci-lint not found. Installing..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
	fi
	@golangci-lint run ./...

## fmt: Format code
.PHONY: fmt
fmt:
	@echo "Formatting code..."
	@go fmt ./...
	@go mod tidy

## clean: Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning..."
	@rm -rf ${BUILD_DIR} ${DIST_DIR} coverage.txt coverage.html

## deps: Download dependencies
.PHONY: deps
deps:
	@echo "Downloading dependencies..."
	@go mod download
	@go mod verify

## update-deps: Update dependencies
.PHONY: update-deps
update-deps:
	@echo "Updating dependencies..."
	@go get -u ./...
	@go mod tidy

## compose-up: Start all services with docker-compose
.PHONY: compose-up
compose-up:
	@echo "Starting services with docker-compose..."
	@docker-compose up -d

## compose-down: Stop all services
.PHONY: compose-down
compose-down:
	@echo "Stopping services..."
	@docker-compose down

## compose-logs: View logs from all services
.PHONY: compose-logs
compose-logs:
	@docker-compose logs -f

## compose-ps: List running services
.PHONY: compose-ps
compose-ps:
	@docker-compose ps

## compose-restart: Restart all services
.PHONY: compose-restart
compose-restart: compose-down compose-up

## compose-rebuild: Rebuild and restart services
.PHONY: compose-rebuild
compose-rebuild:
	@echo "Rebuilding services..."
	@docker-compose up -d --build

## verify: Verify project (test, lint, fmt)
.PHONY: verify
verify: deps fmt lint test

# Development helpers

## dev-deps: Install development dependencies
.PHONY: dev-deps
dev-deps:
	@echo "Installing development dependencies..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install github.com/cosmtrek/air@latest
	@go install github.com/go-delve/delve/cmd/dlv@latest


# Print variables (for debugging Makefile)
.PHONY: info
info:
	@echo "APP_NAME:      ${APP_NAME}"
	@echo "VERSION:       ${VERSION}"
	@echo "BUILD_TIME:    ${BUILD_TIME}"
	@echo "GIT_COMMIT:    ${GIT_COMMIT}"
	@echo "GO_VERSION:    ${GO_VERSION}"

## help: Show this help message
.PHONY: help
help:
	@echo "╔════════════════════════════════════════════════════════════╗"
	@echo "║                    LagRadar Makefile                       ║"
	@echo "╚════════════════════════════════════════════════════════════╝"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "🔨 Build & Run"
	@echo "  make build          Build the application binary"
	@echo "  make clean          Clean build artifacts"
	@echo ""
	@echo "🧪 Testing & Quality"
	@echo "  make test           Run tests with coverage"
	@echo "  make lint           Run linters"
	@echo "  make fmt            Format code"
	@echo ""
	@echo "🚀 Docker Compose"
	@echo "  make compose-up          Start all services"
	@echo "  make compose-down        Stop all services"
	@echo "  make compose-logs        View service logs"
	@echo "  make compose-ps          List running services"
	@echo "  make compose-restart     Restart all services"
	@echo ""
	@echo "🛠️  Development"
	@echo "  make dev-deps       Install dev dependencies"
	@echo ""
	@echo "📋 Other"
	@echo "  make info           Show Makefile variables"
	@echo "  make help           Show this help message"
	@echo ""
