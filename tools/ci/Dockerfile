# syntax=docker/dockerfile:1
# Toolchains run in Docker, not on the dedicated self-hosted LXC.
FROM golang:1.27.1-bookworm AS go
ENV GOTOOLCHAIN=local
WORKDIR /workspace

# @playwright/test 1.60.0: Chromium/WebKit, Node and OS libraries are included.
FROM mcr.microsoft.com/playwright:v1.60.0-noble AS browser
COPY --from=go /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:$PATH" GOTOOLCHAIN=local CGO_ENABLED=0
RUN apt-get update \
    && apt-get install -y --no-install-recommends git \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /workspace

# Matches release workflow; bundled Go, Docker CLI and Buildx.
FROM goreleaser/goreleaser:v2.18.3 AS release
RUN apk add --no-cache bash python3 jq git coreutils github-cli curl
ENV GOTOOLCHAIN=local
WORKDIR /workspace

# The bash image installs bash outside /bin; runner.sh requires /bin/bash.
FROM alpine:3.22 AS title
RUN apk add --no-cache bash
WORKDIR /workspace

FROM curlimages/curl:8.16.0 AS smoke
COPY tools/ci/smoke.sh /ci/smoke.sh
