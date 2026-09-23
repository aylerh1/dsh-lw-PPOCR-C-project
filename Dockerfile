# Stage 1: Build C static library and Go CGO server
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache build-base cmake

WORKDIR /build

# 1. Build lw.PPOCR.C native C11 static library with AVX2 & SIMD
COPY csrc/ ./csrc/
WORKDIR /build/csrc
RUN rm -rf build && \
    cmake -B build -DCMAKE_BUILD_TYPE=Release -DLW_RUNTIME_ONLY=ON -DLW_BUILD_HTTP_DEMO=OFF && \
    cmake --build build --target lw_ppocr_c -j4

# 2. Compile Go CGO server statically
WORKDIR /build
COPY go.mod ./
COPY cmd/ ./cmd/
COPY test/ ./test/
COPY vendor/ ./vendor/
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /build/server ./cmd/server
RUN CGO_ENABLED=1 GOOS=linux go test -v ./cmd/server

# Stage 2: Ultra-Minimal Native Runtime (~25MB total image size)
FROM alpine:3.21

# Install runtime dependencies (musl libc already includes pthread/m, add ca-certificates)
RUN apk add --no-cache ca-certificates

WORKDIR /app

# Copy compiled native server binary to system PATH (not overwritten by volume mount)
COPY --from=builder /build/server /usr/local/bin/server
RUN chmod +x /usr/local/bin/server

# Copy application assets, models, and public web files
COPY vendor/ ./vendor/
COPY public/ ./public/
COPY test/ ./test/

ENV PORT=3000
ENV HOST=0.0.0.0
ENV MODEL_DIR=/app/vendor/lw-ppocr-wasm
ENV PUBLIC_DIR=/app/public
ENV SAMPLE_PATH=/app/test/fixtures/sample.png

EXPOSE 3000

# Run native C/Go server
CMD ["/usr/local/bin/server"]
