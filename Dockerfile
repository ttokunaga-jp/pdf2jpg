# syntax=docker/dockerfile:1.7

FROM golang:1.22-bookworm AS builder

WORKDIR /workspace

# Enable Go module proxy caching
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go mod tidy

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go build ./...

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    GOBIN=/workspace/bin go install ./cmd

RUN mv /workspace/bin/cmd /workspace/bin/main

FROM debian:bookworm-slim AS runtime

ENV PORT=8080

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    mupdf \
    mupdf-tools \
    libjpeg62-turbo \
    libopenjp2-7 \
    libfreetype6 \
    libjbig2dec0 \
    libharfbuzz0b \
    libstdc++6 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /workspace/bin/main ./main

EXPOSE 8080

ENTRYPOINT ["./main"]
