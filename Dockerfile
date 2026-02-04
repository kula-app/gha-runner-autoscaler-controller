# ============================================================================
# GitHub Actions Runner Autoscaler Controller Dockerfile
# ============================================================================
# Multi-stage build for optimized production container
# - Stage 1: Build the Go binary
# - Stage 2: Create minimal runtime image with distroless base
# ============================================================================

# Build stage
FROM golang:1.25.7-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . ./

# Build the binary
# - CGO_ENABLED=0: Build static binary without C dependencies
# - -ldflags "-s -w": Strip debug information to reduce binary size
# - -trimpath: Remove file system paths from binary for reproducibility
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -trimpath \
    -o controller \
    ./cmd/controller

# Runtime stage - use distroless for minimal attack surface
FROM gcr.io/distroless/static:nonroot

# Copy binary from builder
COPY --from=builder /build/controller /controller

# Use non-root user (from distroless)
USER nonroot:nonroot

# Run the controller
ENTRYPOINT ["/controller"]
