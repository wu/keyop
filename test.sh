#!/bin/bash

set -o nounset -o errexit -o pipefail -o errtrace

go test ./... && echo SUCCESS || echo FAIL

