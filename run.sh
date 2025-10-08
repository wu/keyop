#!/bin/bash

set -o nounset -o errexit -o pipefail -o errtrace

go build -o keyop
./keyop "$@"

