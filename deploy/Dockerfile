# syntax=docker/dockerfile:1
# Pulsar — multi-stage Dockerfile.
# Stage 1 builds the templ-generated sources and the Go binary.
# Stage 2 embeds static assets and ships a minimal runtime image.

############################
# Builder
############################
FROM golang:1.26-alpine AS builder

# Build-time dependencies: git for private modules, ca-certificates for TLS.
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src

# Cache module downloads first.
COPY go.mod go.sum ./
RUN go mod download

# templ is required to generate Go code from .templ files.
RUN go install github.com/a-h/templ/cmd/templ@latest

# Copy the rest of the source.
COPY . .

# Generate templ sources and build the static binary.
RUN templ generate && \
    CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags "-s -w -X main.version=docker" \
        -o /out/pulsar \
        ./cmd/pulsar

############################
# Runtime
############################
FROM alpine:3.20 AS runtime

RUN apk add --no-cache ca-certificates tzdata curl && \
    addgroup -S pulsar && adduser -S -G pulsar pulsar

# Static assets and migrations are embedded into the binary (//go:embed), so
# the runtime image needs only the single binary.
COPY --from=builder /out/pulsar /usr/local/bin/pulsar

ENV TZ=UTC \
    HTTP_ADDR=:8080

USER pulsar
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -fsS http://localhost:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/pulsar"]
