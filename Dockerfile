# Stage 1: Build lightweight Go HTTP Server binary
FROM golang:1.23-alpine AS builder

WORKDIR /build
COPY go.mod ./
COPY cmd/ ./cmd/

# Compile static Go binary without CGO, strip symbols to minimize size
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /build/server ./cmd/server

# Stage 2: Minimal Runtime Image
FROM node:20-alpine

WORKDIR /app

# 1. Install Node production dependencies for on-demand CLI worker
COPY package.json package-lock.json* ./
RUN npm install --omit=dev

# 2. Copy compiled Go binary to system PATH to avoid volume mount overwrite
COPY --from=builder /build/server /usr/local/bin/server
RUN chmod +x /usr/local/bin/server

# 3. Copy application assets, WASM engine, web, and tests
COPY cordis.patch.yml ./
COPY src/ ./src/
COPY vendor/ ./vendor/
COPY public/ ./public/
COPY test/ ./test/

ENV PORT=3000
ENV HOST=0.0.0.0
EXPOSE 3000

# Go native net/http server runs as PID 1 daemon
CMD ["/usr/local/bin/server"]
