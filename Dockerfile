FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /memory-server ./cmd/server
RUN go build -o /memory-admin ./cmd/admin
RUN go build -o /memory-cli ./cmd/cli

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /memory-server /app/memory-server
COPY --from=builder /memory-admin /app/memory-admin
COPY --from=builder /memory-cli /app/memory-cli

EXPOSE 8080

ENTRYPOINT ["/app/memory-server"]
