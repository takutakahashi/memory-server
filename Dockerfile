FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /memory-server ./cmd/server
RUN go build -o /memory-admin ./cmd/admin
RUN go build -o /memory-cli ./cmd/cli

# Install bun and build the TypeScript curator agent
FROM oven/bun:1-alpine AS scripts-builder

WORKDIR /app/scripts
COPY scripts/package.json scripts/bun.lockb* ./
RUN bun install --frozen-lockfile
COPY scripts/ ./

# Final image
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /memory-server /app/memory-server
COPY --from=builder /memory-admin /app/memory-admin
COPY --from=builder /memory-cli /app/memory-cli

# Copy bun binary
COPY --from=oven/bun:1-alpine /usr/local/bin/bun /usr/local/bin/bun

# Copy scripts with installed node_modules
COPY --from=scripts-builder /app/scripts /app/scripts

EXPOSE 8080

ENTRYPOINT ["/app/memory-server"]
