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
    ca-certificates git \
    && rm -rf /var/lib/apt/lists/*

# Create a non-privileged user and group
RUN groupadd --gid 10001 keyop && \
    useradd --uid 10001 --gid keyop --no-create-home --shell /sbin/nologin keyop

# Expose web UI port
EXPOSE 8823

# Copy selected example config files
COPY --chown=keyop:keyop example-conf/heartbeat.yaml      /home/keyop/.keyop/conf/heartbeat.yaml
COPY --chown=keyop:keyop example-conf/moon.yaml            /home/keyop/.keyop/conf/moon.yaml
COPY --chown=keyop:keyop example-conf/cpu-monitor.yaml     /home/keyop/.keyop/conf/cpu-monitor.yaml
COPY --chown=keyop:keyop example-conf/memory-monitor.yaml  /home/keyop/.keyop/conf/memory-monitor.yaml
COPY --chown=keyop:keyop example-conf/webui.yaml           /home/keyop/.keyop/conf/webui.yaml

RUN mkdir -p /home/keyop/.keyop/data && \
    chown -R keyop:keyop /home/keyop

COPY --from=builder /keyop /keyop

USER keyop

ENTRYPOINT ["/keyop"]
CMD ["--help"]
