# Multi-stage Dockerfile for building a minimal Linux binary for keyop
# Use a simple ARG for Go version to allow overrides.
ARG GO_VERSION=1.26
FROM golang:${GO_VERSION} AS builder

WORKDIR /src

# Cache go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the sources
COPY . .

# Build static linux binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w" -o /keyop ./

FROM ubuntu:24.04

# Avoid interactive prompts during package install
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Expose web UI port
EXPOSE 8823

# Copy selected example config files
COPY --chown=0:0 example-conf/heartbeat.yaml      /root/.keyop/conf/heartbeat.yaml
COPY --chown=0:0 example-conf/moon.yaml            /root/.keyop/conf/moon.yaml
COPY --chown=0:0 example-conf/cpu-monitor.yaml     /root/.keyop/conf/cpu-monitor.yaml
COPY --chown=0:0 example-conf/memory-monitor.yaml  /root/.keyop/conf/memory-monitor.yaml
COPY --chown=0:0 example-conf/webui.yaml           /root/.keyop/conf/webui.yaml

RUN mkdir -p /root/.keyop/webui

COPY --from=builder /keyop /keyop

ENTRYPOINT ["/keyop"]
CMD ["--help"]
