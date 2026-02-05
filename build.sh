#!/usr/bin/env bash

set -o nounset -o errexit -o pipefail -o errtrace

echo "Building keyop for macos"
go build -o output/keyop

echo "Building keyop for linux amd64"
env GOOS=linux GOARCH=amd64 go build -o output/keyop-linux-amd64

echo "Building keyop for linux arm64"
env GOOS=linux GOARCH=arm64 go build -o output/keyop-linux-arm64

echo "Building keyop for linux arm"
env GOARM=6 GOOS=linux GOARCH=arm go build -o output/keyop-linux-arm

