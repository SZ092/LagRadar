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
DOCKER_DIR := ./deployments/docker
K8S_DIR := ./deployments/k8s
CONFIG_DIR := ./configs

# Docker Compose file path
COMPOSE_FILE := $(DOCKER_DIR)/docker-compose.yaml

# Default target
.DEFAULT_GOAL := help

# Build variables
DOCKER_IMAGE=lagradar:latest
NAMESPACE=monitoring

## build: Build the application binary
.PHONY: build
build:
	@echo "Building ${APP_NAME}..."
	@mkdir -p ${BUILD_DIR}
	@go build ${LDFLAGS} -o ${BUILD_DIR}/${APP_NAME} ./main.go
	@echo "Built ${BUILD_DIR}/${APP_NAME}"

## run: Run the application locally
.PHONY: run
run: build
	@echo "Running ${APP_NAME} locally..."
	CONFIG_FILE=$(CONFIG_DIR)/config.dev.yaml ${BUILD_DIR}/${APP_NAME}

## docker-build: Build Docker image
.PHONY: docker-build
docker-build:
	@echo "Building Docker image..."
	@docker build --no-cache -t $(DOCKER_IMAGE) -f $(DOCKER_DIR)/Dockerfile .

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
	@docker-compose -f $(COMPOSE_FILE) up

## compose-up-d: Start all services in detached mode
.PHONY: compose-up-d
compose-up-d: docker-build
	@echo "Starting services in detached mode..."
	@docker-compose -f $(COMPOSE_FILE) up -d

## compose-down: Stop all services
.PHONY: compose-down
compose-down:
	@echo "Stopping services..."
	@docker-compose -f $(COMPOSE_FILE) down

## compose-logs: View logs from all services
.PHONY: compose-logs
compose-logs:
	@docker-compose -f $(COMPOSE_FILE) logs -f

## compose-ps: List running services
.PHONY: compose-ps
compose-ps:
	@docker-compose -f $(COMPOSE_FILE) ps

## compose-restart: Stop and restart all services, removing volumes
.PHONY: compose-restart
compose-restart:
	@docker-compose -f $(COMPOSE_FILE) down -v
	@docker-compose -f $(COMPOSE_FILE) up -d

## compose-rebuild: Rebuild images without cache and restart services
.PHONY: compose-rebuild
compose-rebuild:
	@echo "Rebuilding services(no cache)..."
	@docker-compose -f $(COMPOSE_FILE) build --no-cache
	@docker-compose -f $(COMPOSE_FILE) up -d

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
	@echo "COMPOSE_FILE:  ${COMPOSE_FILE}"
	@echo "DOCKER_IMAGE:  ${DOCKER_IMAGE}"

# Deploy to local Minikube
.PHONY: k8s-local
k8s-local: docker-build
	@echo "Deploying to Minikube..."

	minikube image load $(DOCKER_IMAGE)

	kubectl create namespace $(NAMESPACE) --dry-run=client -o yaml | kubectl apply -f -

	kubectl create configmap lagradar-config \
		--from-file=config.yaml=configs/config.local-k8s.yaml \
		-n $(NAMESPACE) \
		--dry-run=client -o yaml | kubectl apply -f -

	kubectl apply -f deployments/kubernetes/rbac.yaml
	kubectl apply -f deployments/kubernetes/deployment.yaml
	kubectl apply -f deployments/kubernetes/service.yaml

	kubectl rollout status deployment/lagradar -n $(NAMESPACE)

	@echo "Deployment complete!"
	kubectl get pods -n $(NAMESPACE) -l app=lagradar

# Clean up K8s resources
.PHONY: k8s-clean
k8s-clean:
	@echo "Cleaning up K8s resources..."
	kubectl delete -f deployments/k8s/base/ --ignore-not-found=true
	kubectl delete configmap lagradar-config -n $(NAMESPACE) --ignore-not-found=true

# Development shortcuts
.PHONY: dev-run
dev-run:
	CONFIG_FILE=$(CONFIG_DIR)/config.dev.yaml go run main.go

.PHONY: dev-k8s
dev-k8s:
	CONFIG_FILE=$(CONFIG_DIR)/config.local-k8s.yaml go run main.go

## help: Show this help message
.PHONY: help
help:
	@echo "╔════════════════════════════════════════════════════════════╗"
	@echo "║                    LagRadar Makefile                       ║"
	@echo "╚════════════════════════════════════════════════════════════╝"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "🔨  Build & Run"
	@echo "  make build               Build the application binary"
	@echo "  make docker-build        Build Docker image"
	@echo "  make clean               Clean build artifacts"
	@echo ""
	@echo "🧪  Testing & Quality"
	@echo "  make test                Run tests with coverage"
	@echo "  make lint                Run linters"
	@echo "  make fmt                 Format code"
	@echo ""
	@echo "🐳  Docker Compose"
	@echo "  make compose-up          Start all services"
	@echo "  make compose-down        Stop all services"
	@echo "  make compose-logs        View service logs"
	@echo "  make compose-ps          List running services"
	@echo "  make compose-restart     Stop and restart all services, removing volumes"
	@echo "  make compose-rebuild     Rebuild images without cache and restart services"
	@echo ""
	@echo "☸️  Kubernetes"
	@echo "  make k8s-local           Deploy to local Minikube"
	@echo "  make k8s-clean           Clean up K8s resources"
	@echo ""
	@echo "🛠️  Development"
	@echo "  make dev-deps            Install dev dependencies"
	@echo ""
	@echo "📋  Other"
	@echo "  make info                Show Makefile variables"
	@echo "  make help                Show this help message"
	@echo ""
