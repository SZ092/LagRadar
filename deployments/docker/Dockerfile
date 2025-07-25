# Build stage
FROM golang:1.22-alpine AS builder

# Install build dependencies for confluent-kafka-go
RUN apk add --no-cache git gcc g++ libc-dev librdkafka-dev pkgconf

WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy all source code from project root
COPY . .

# Build the application with CGO enabled (required for confluent-kafka-go)
RUN go build -tags musl -o lagradar ./main.go

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates librdkafka

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/lagradar .

# Create non-root user
RUN addgroup -g 1000 -S lagradar && \
    adduser -u 1000 -S lagradar -G lagradar

# Change ownership
RUN chown -R lagradar:lagradar /app

# Switch to non-root user
USER lagradar

# Expose port
EXPOSE 8080

# Run the application
ENTRYPOINT ["./lagradar"]