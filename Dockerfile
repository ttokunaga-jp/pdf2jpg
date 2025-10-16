# syntax=docker/dockerfile:1.7

FROM golang:1.25.2-bookworm AS base

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    pkg-config \
    netcat-traditional \
    ca-certificates \
    mupdf \
    mupdf-tools \
    libmupdf-dev \
    libjpeg62-turbo \
    libopenjp2-7 \
    libfreetype6 \
    libjbig2dec0 \
    libharfbuzz0b \
    libstdc++6 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

FROM base AS dev
COPY . .

FROM dev AS builder
RUN go mod tidy
RUN go build ./...
RUN GOBIN=/workspace/bin go install ./cmd
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
