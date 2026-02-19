#!/usr/bin/env bash

set -o nounset -o errexit -o pipefail -o errtrace

GIT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
GIT_HASH=$(git rev-parse --short HEAD)
GIT_VERSION=$(git describe --tags --always --dirty)
BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS="-X keyop/cmd.Branch=${GIT_BRANCH} -X keyop/cmd.Commit=${GIT_HASH} -X keyop/cmd.Version=${GIT_VERSION} -X keyop/cmd.BuildTime=${BUILD_TIME}"

echo "Building keyop for macos arm"
env GOOS=darwin GOARCH=arm64 go build -ldflags "${LDFLAGS}" -o output/keyop-darwin-arm64
cd plugins/helloWorldPlugin
make
cd ../..

echo "Building keyop for macos intel"
env GOOS=darwin GOARCH=amd64 go build -ldflags "${LDFLAGS}" -o output/keyop-darwin-amd64

echo "Building keyop for linux amd64"
env GOOS=linux GOARCH=amd64 go build -ldflags "${LDFLAGS}" -o output/keyop-linux-amd64

echo "Building keyop for linux arm64"
env GOOS=linux GOARCH=arm64 go build -ldflags "${LDFLAGS}" -o output/keyop-linux-arm64

echo "Building keyop for linux arm"
env GOARM=6 GOOS=linux GOARCH=arm go build -ldflags "${LDFLAGS}" -o output/keyop-linux-arm

