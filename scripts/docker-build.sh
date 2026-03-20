#!/usr/bin/env bash
set -euo pipefail

# Usage: ./scripts/docker-build.sh IMAGE TAG [DOCKERFILE]
IMAGE=${1:-}
TAG=${2:-latest}
DOCKERFILE_ARG=${3:-Dockerfile.prebuilt}
TMPDIR=""

if [ -z "$IMAGE" ]; then
  echo "Usage: $0 IMAGE TAG [DOCKERFILE]"
  exit 2
fi

REPO_ROOT=$(pwd)
GO_VERSION=${GO_VERSION:-1.26}

cleanup() {
  if [ -n "$TMPDIR" ] && [ -d "$TMPDIR" ]; then
    rm -rf "$TMPDIR"
  fi
}
trap cleanup EXIT

echo "Preparing docker build... using Dockerfile: $DOCKERFILE_ARG"

# Build the main linux binary
if command -v docker >/dev/null 2>&1; then
  echo "Docker available — building main binary inside golang:${GO_VERSION} container"
  docker run --rm -v "$REPO_ROOT":/work -w /work golang:${GO_VERSION} bash -lc \
    "export PATH=/usr/local/go/bin:$PATH && \
     CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
     go build -trimpath -ldflags '-s -w' -o output/keyop-linux-amd64 ./"
else
  echo "Docker not available — building main binary on host via Makefile"
  make output/keyop-linux-amd64
fi

BIN="$REPO_ROOT/output/keyop-linux-amd64"
if [ ! -f "$BIN" ]; then
  echo "Expected built binary $BIN not found"
  exit 1
fi

TMPDIR=$(mktemp -d)
cp "$BIN" "$TMPDIR/keyop"

# Copy only the selected example config files into the build context
mkdir -p "$TMPDIR/example-conf"
for f in heartbeat.yaml moon.yaml cpu-monitor.yaml memory-monitor.yaml webui.yaml; do
  cp "$REPO_ROOT/example-conf/$f" "$TMPDIR/example-conf/$f"
done

if [ -f "$REPO_ROOT/$DOCKERFILE_ARG" ]; then
  cp "$REPO_ROOT/$DOCKERFILE_ARG" "$TMPDIR/Dockerfile"
else
  echo "Requested Dockerfile $DOCKERFILE_ARG does not exist; using Dockerfile.prebuilt"
  cp "$REPO_ROOT/Dockerfile.prebuilt" "$TMPDIR/Dockerfile"
fi

if command -v docker >/dev/null 2>&1; then
  docker build -t "$IMAGE:$TAG" "$TMPDIR"
else
  echo "docker CLI not found; skipping docker build"
fi
