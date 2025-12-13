# ============================================================================
# GITHUB ACTIONS RUNNER AUTOSCALER CONTROLLER MAKEFILE
# ============================================================================
# This Makefile provides automation for building, testing, and developing
# the GitHub Actions Runner Autoscaler Controller. Run 'make help' to see all available commands.
# ============================================================================

# Default target - show help when running 'make' without arguments
.DEFAULT_GOAL := help

# ============================================================================
# SETUP & INSTALLATION
# ============================================================================

## Initialize project for development (installs all dependencies)
#
# This command sets up your development environment by:
# - Installing system dependencies via Homebrew (if available on macOS)
# - Installing Go module dependencies
# - Preparing the project for development
#
# Run this once when you first clone the repository.
.PHONY: init
init:
	@if [ "$$(uname)" = "Darwin" ]; then \
		echo "Darwin detected."; \
		$(MAKE) init-darwin; \
	elif [ "$$(uname)" = "Linux" ]; then \
		echo "Linux detected."; \
		$(MAKE) init-linux; \
	else \
		echo "Not running on Darwin or Linux."; \
		exit 1; \
	fi
	$(MAKE) install

.PHONY: init-darwin
init-darwin:
	@if ! command -v brew >/dev/null 2>&1; then \
		echo "Homebrew not detected. Skipping system dependency installation."; \
	else \
		echo "Homebrew detected. Installing system dependencies..."; \
		brew bundle; \
	fi

.PHONY: init-linux
init-linux:
	@if ! command -v dprint >/dev/null 2>&1; then \
		echo "dprint not detected. Please install: curl -fsSL https://dprint.dev/install.sh | sh"; \
	fi

## Install and tidy Go module dependencies
#
# Downloads and installs all Go module dependencies and removes
# unused modules. Safe to run multiple times.
.PHONY: install
install:
	go mod tidy

# ============================================================================
# BUILDING
# ============================================================================

## Build production controller binary optimized for deployment
#
# Creates an optimized binary in dist/gha-runner-autoscaler-controller suitable for production
# deployment in Kubernetes. The binary includes all optimizations.
.PHONY: build
build:
	mkdir -p dist
	go build -o dist/gha-runner-autoscaler-controller ./cmd/controller

## Build Docker image for controller
#
# Builds the controller Docker image for linux/amd64 platform.
# This creates a Docker image suitable for deployment to Kubernetes.
#
# Prerequisites:
# - Docker with buildx support
.PHONY: build-docker
build-docker:
	@echo "Building controller Docker image..."
	docker buildx build \
		--platform linux/amd64 \
		-f Dockerfile \
		-t ghcr.io/kula-app/gha-runner-autoscaler-controller:latest \
		--load \
		.
	@echo "Docker image built successfully: ghcr.io/kula-app/gha-runner-autoscaler-controller:latest"

# ============================================================================
# DEVELOPMENT & RUNNING
# ============================================================================

## Run controller with hot reload for development
#
# Starts the controller using Air for hot reloading. The controller will:
# - Automatically rebuild when Go files change
# - Restart the controller process
# - Watch for file changes in real-time
#
# Air configuration is in .air.toml file.
# Press Ctrl+C to stop the development server.
#
# Note: This requires a valid Kubernetes config (kubeconfig) to connect to a cluster.
.PHONY: dev
dev:
	go tool air -c .air.toml

## Run controller with hot reload in dry-run mode
#
# Same as 'make dev' but with --dry-run flag enabled.
# The controller will calculate changes but NOT apply them to the cluster.
# Perfect for safely testing the allocation logic.
#
# Air configuration is in .air-dry-run.toml file.
.PHONY: dev-dry-run
dev-dry-run:
	go tool air -c .air-dry-run.toml

## Run controller directly without hot reload
#
# Runs the controller directly using 'go run'. Useful for:
# - Quick testing without setting up hot reload
# - Running with specific environment variables
# - One-off execution
#
# The controller will run until manually stopped with Ctrl+C.
#
# Note: This requires a valid Kubernetes config (kubeconfig) to connect to a cluster.
.PHONY: run
run:
	go run ./cmd/controller

## Run controller directly in dry-run mode
#
# Same as 'make run' but with --dry-run flag enabled.
# The controller will calculate changes but NOT apply them to the cluster.
.PHONY: run-dry-run
run-dry-run:
	go run ./cmd/controller --dry-run

# ============================================================================
# TESTING & QUALITY ASSURANCE
# ============================================================================

## Run all tests in the project
#
# Executes all unit tests, integration tests, and benchmarks.
# Tests are run with Go's built-in testing framework.
#
# Use 'go test -v ./...' for verbose output.
# Use 'go test -race ./...' to check for race conditions.
.PHONY: test
test:
	go test ./...

## Run tests with coverage report
#
# Executes all tests and generates a coverage report.
# Coverage data is saved to coverage.out and a summary is displayed.
.PHONY: test-coverage
test-coverage:
	go test -v -coverprofile=tmp/coverage.out ./...
	go tool cover -func=tmp/coverage.out

## Run comprehensive static analysis and security checks
#
# Performs multiple code quality checks:
# - go vet: Examines Go source code for suspicious constructs
# - staticcheck: Advanced static analysis for bugs and performance issues
# - govulncheck: Scans for known security vulnerabilities
#
# Fix any issues reported before committing code.
.PHONY: analyze
analyze:
	go vet ./...
	go tool staticcheck ./...
	go tool govulncheck ./...

## Format code and organize imports
#
# Automatically formats all code in the project:
# - go mod tidy: Cleans up module dependencies
# - go fmt: Formats Go source code to standard style
# - dprint fmt: Formats other files (JSON, YAML, etc.) using dprint
#
# Run this before committing to ensure consistent code style.
.PHONY: format
format:
	go mod tidy
	go fmt ./...
	dprint fmt

# ============================================================================
# MAINTENANCE
# ============================================================================

## Update all dependencies to latest compatible versions
#
# Updates all Go module dependencies to their latest minor/patch versions
# while respecting semantic versioning constraints. After updating:
# - Dependencies are updated to latest compatible versions
# - Code is automatically formatted
# - Module files are tidied
#
# Review changes carefully before committing dependency updates.
.PHONY: upgrade-deps
upgrade-deps:
	go get -u ./...
	$(MAKE) format

# ============================================================================
# HELP & DOCUMENTATION
# ============================================================================

## Show this help message with all available commands
#
# Displays a formatted list of all available make targets with descriptions.
# Commands are organized by topic for easy navigation.
.PHONY: help
help:
	@echo "============================================================================="
	@echo "🚀 GITHUB ACTIONS RUNNER AUTOSCALER CONTROLLER DEVELOPMENT COMMANDS"
	@echo "============================================================================="
	@echo ""
	@awk 'BEGIN { desc = ""; target = "" } \
	/^## / { desc = substr($$0, 4) } \
	/^\.PHONY: / && desc != "" { \
		target = $$2; \
		printf "\033[36m%-20s\033[0m %s\n", target, desc; \
		desc = ""; target = "" \
	}' $(MAKEFILE_LIST)
	@echo ""
	@echo "💡 Use 'make <command>' to run any command above."
	@echo "📖 For detailed information, see comments in the Makefile."
	@echo ""
