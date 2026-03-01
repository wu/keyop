#!/usr/bin/env bash
set -euo pipefail

# Usage: ./scripts/docker-build.sh IMAGE TAG [DOCKERFILE]
# Set PRESERVE_TMPDIR=1 in the environment to keep the temporary build context for inspection.
IMAGE=${1:-}
TAG=${2:-latest}
DOCKERFILE_ARG=${3:-Dockerfile.prebuilt}
PRESERVE_TMPDIR=${PRESERVE_TMPDIR:-0}

if [ -z "$IMAGE" ]; then
  echo "Usage: $0 IMAGE TAG [DOCKERFILE]"
  exit 2
fi

REPO_ROOT=$(pwd)
PLUGIN_SRC="$REPO_ROOT/../keyop-webUiPlugin"
PLUGIN_DIR="$REPO_ROOT/plugins/webUiPlugin"
BACKUP=""
PLUGIN_COPIED=0
TMPDIR=""

# Default Go version for in-container builds
GO_VERSION=${GO_VERSION:-1.25}

cleanup() {
  # remove temporary dir if created (unless PRESERVE_TMPDIR=1)
  if [ "$PRESERVE_TMPDIR" != "1" ]; then
    if [ -n "$TMPDIR" ] && [ -d "$TMPDIR" ]; then
      rm -rf "$TMPDIR"
    fi
  else
    if [ -n "$TMPDIR" ]; then
      echo "Preserving temporary build context: $TMPDIR"
    fi
  fi
  # Restore plugin backup if necessary
  if [ "$PLUGIN_COPIED" = "1" ]; then
    echo "Restoring plugin directory state"
    rm -rf "$PLUGIN_DIR"
    if [ -n "$BACKUP" ] && [ -d "$BACKUP" ]; then
      mv "$BACKUP" "$PLUGIN_DIR"
    fi
  fi
}

# Register cleanup only if we will actually clean up; if preserving tmpdir, still need to restore plugin backup on EXIT
if [ "$PRESERVE_TMPDIR" = "1" ]; then
  trap 'if [ "$PLUGIN_COPIED" = "1" ]; then echo "Restoring plugin directory state"; rm -rf "$PLUGIN_DIR"; if [ -n "$BACKUP" ] && [ -d "$BACKUP" ]; then mv "$BACKUP" "$PLUGIN_DIR"; fi; fi; ' EXIT
else
  trap cleanup EXIT
fi

echo "Preparing docker build... using Dockerfile: $DOCKERFILE_ARG"

if [ -d "$PLUGIN_SRC" ]; then
  echo "Found external plugin at $PLUGIN_SRC, copying into $REPO_ROOT/plugins"
  if [ -d "$PLUGIN_DIR" ]; then
    BACKUP="$PLUGIN_DIR.backup.$(date +%s)"
    echo "Backing up existing $PLUGIN_DIR to $BACKUP"
    mv "$PLUGIN_DIR" "$BACKUP"
  fi
  cp -a "$PLUGIN_SRC" "$PLUGIN_DIR"
  PLUGIN_COPIED=1

  # Prefer using 'go mod edit' to adjust replace directives so they resolve when plugin
  # is placed under plugins/. This is more robust than sed-based text rewriting.
  echo "Adjusting go.mod replace directives inside $PLUGIN_DIR (if present)..."
  find "$PLUGIN_DIR" -type f -name 'go.mod' -print0 | while IFS= read -r -d '' gmod; do
    dir=$(dirname "$gmod")
    echo "Processing go.mod in $dir"
    if command -v go >/dev/null 2>&1; then
      # Run in the module directory so go mod edit updates the correct file
      (cd "$dir" && go mod edit -replace keyop=../.. ) || true
    else
      echo "go tool not found; skipping 'go mod edit' for $gmod"
    fi
  done
else
  echo "No external plugin found at $PLUGIN_SRC"
fi

# If Docker is available, build both the main linux binary and plugin inside a golang container
if command -v docker >/dev/null 2>&1; then
  echo "Docker available — building main binary and plugin inside golang:${GO_VERSION} container (CGO_ENABLED=1)"
  docker run --rm -v "$REPO_ROOT":/work -w /work golang:${GO_VERSION} bash -lc \
    "export PATH=/usr/local/go/bin:$PATH; export DEBIAN_FRONTEND=noninteractive; apt-get update >/dev/null && apt-get install -y --no-install-recommends build-essential make ca-certificates >/dev/null && \
     export CGO_ENABLED=1 GOOS=linux GOARCH=amd64 && \
     make output/keyop-linux-amd64 && \
     if [ -d plugins/webUiPlugin ]; then cd plugins/webUiPlugin && export CGO_ENABLED=1 GOOS=linux GOARCH=amd64 && go mod download && go build -buildmode=plugin -o webUiPlugin.so .; fi"
else
  echo "Docker not available: building main binary on host (Makefile) and attempting host plugin build (may not be linux-compatible)"
  # Build main linux binary via Makefile target (host cross-compile)
  make output/keyop-linux-amd64
  # Attempt plugin build on host
  if [ -d "$PLUGIN_DIR" ]; then
    (cd "$PLUGIN_DIR" && go mod download && go build -buildmode=plugin -o webUiPlugin.so .) || true
  fi
fi

BIN="$REPO_ROOT/output/keyop-linux-amd64"
if [ ! -f "$BIN" ]; then
  echo "Expected built binary $BIN not found"
  exit 1
fi

PLUGIN_SO="$PLUGIN_DIR/webUiPlugin.so"
if [ -f "$PLUGIN_SO" ]; then
  echo "Built plugin found at $PLUGIN_SO"
else
  echo "Warning: plugin .so not found at $PLUGIN_SO; plugin may not be included in image"
fi

TMPDIR=$(mktemp -d)
trap 'if [ "$PRESERVE_TMPDIR" != "1" ]; then rm -rf "$TMPDIR"; fi' EXIT
cp "$BIN" "$TMPDIR/keyop"
cp -a "$REPO_ROOT/example-conf" "$TMPDIR/example-conf" || true
# Copy the requested Dockerfile into the context
if [ -f "$REPO_ROOT/$DOCKERFILE_ARG" ]; then
  cp "$REPO_ROOT/$DOCKERFILE_ARG" "$TMPDIR/Dockerfile"
else
  echo "Requested Dockerfile $DOCKERFILE_ARG does not exist; using Dockerfile.prebuilt"
  cp "$REPO_ROOT/Dockerfile.prebuilt" "$TMPDIR/Dockerfile"
fi

# If the plugin's static assets exist in the repo (or were copied from external plugin), include them in the docker context
if [ -d "$REPO_ROOT/plugins/webUiPlugin/static" ]; then
  echo "Including web UI static assets in docker context"
  mkdir -p "$TMPDIR/plugins/webUiPlugin"
  cp -a "$REPO_ROOT/plugins/webUiPlugin/static" "$TMPDIR/plugins/webUiPlugin/static"
fi

# If plugin shared object exists, copy it into the docker context and update config files
if [ -f "$PLUGIN_SO" ]; then
  echo "Including built plugin binary in docker context"
  mkdir -p "$TMPDIR/.keyop/plugins/webUiPlugin"
  cp "$PLUGIN_SO" "$TMPDIR/.keyop/plugins/webUiPlugin/webUiPlugin.so"

  # Update plugins.yaml in the context to point to the in-container plugin path
  PLUGINS_YAML="$TMPDIR/example-conf/plugins.yaml"
  if [ -f "$PLUGINS_YAML" ]; then
    echo "Patching $PLUGINS_YAML to reference /root/.keyop/plugins/webUiPlugin/webUiPlugin.so"
    sed -E 's|path:[[:space:]]*.*|path: /root/.keyop/plugins/webUiPlugin/webUiPlugin.so|' "$PLUGINS_YAML" > "$PLUGINS_YAML.tmp" && mv "$PLUGINS_YAML.tmp" "$PLUGINS_YAML"
  fi

  # Update plugin-webui.yaml (add soPath under config if missing)
  PLUGIN_WEBUI="$TMPDIR/example-conf/plugin-webui.yaml"
  if [ -f "$PLUGIN_WEBUI" ]; then
    if ! grep -q 'soPath:' "$PLUGIN_WEBUI"; then
      # find config: line number
      lineno=$(grep -n '^config:' "$PLUGIN_WEBUI" | head -n1 | cut -d: -f1 || true)
      if [ -n "$lineno" ]; then
        awk -v ln="$lineno" 'NR==ln{print; print "  soPath: /root/.keyop/plugins/webUiPlugin/webUiPlugin.so"; next} {print}' "$PLUGIN_WEBUI" > "$PLUGIN_WEBUI.tmp" && mv "$PLUGIN_WEBUI.tmp" "$PLUGIN_WEBUI"
      else
        printf "\nconfig:\n  soPath: /root/.keyop/plugins/webUiPlugin/webUiPlugin.so\n" >> "$PLUGIN_WEBUI"
      fi
      echo "Patched $PLUGIN_WEBUI to include soPath"
    else
      echo "$PLUGIN_WEBUI already contains soPath; skipping"
    fi
  fi
fi

# Conditionally build image if docker is available
DOCKER_AVAILABLE=0
if command -v docker >/dev/null 2>&1; then
  DOCKER_AVAILABLE=1
fi

if [ "$DOCKER_AVAILABLE" -eq 1 ]; then
  docker build -t "$IMAGE:$TAG" "$TMPDIR"
  BUILD_RC=$?
else
  echo "docker CLI not found; skipping docker build (environment lacks docker)"
  BUILD_RC=0
fi

# cleanup() trap will run to restore plugin and remove TMPDIR (unless PRESERVE_TMPDIR=1)
if [ "$PRESERVE_TMPDIR" = "1" ]; then
  echo "Temporary context preserved at: $TMPDIR"
fi
exit $BUILD_RC

