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

# Build static linux binary (no git calls inside the Dockerfile)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w" -o /keyop ./

# Final image: use distroless static Debian to include CA certs for TLS-heavy apps
FROM gcr.io/distroless/static-debian11

# Expose web UI port
EXPOSE 8823

# Create expected config directories and copy example config
# Copy to root and /root for safety so the app can find ~/.keyop/conf
COPY --chown=0:0 example-conf /root/.keyop/conf
COPY --chown=0:0 example-conf /.keyop/conf

# Copy any prepared .keyop files (plugins, etc) if present
COPY --chown=0:0 .keyop /root/.keyop

# Copy web UI static assets into the image root so the web UI can serve them
COPY --chown=0:0 plugins/webUiPlugin/static /webui-static

# Ensure webui directory exists so the web UI can store its DB file
RUN mkdir -p /root/.keyop/webui /.keyop/webui && \
    chown -R 0:0 /root/.keyop/webui /.keyop/webui || true

COPY --from=builder /keyop /keyop

ENTRYPOINT ["/keyop"]
CMD ["--help"]
