BINARY := keyop
BUILD_DIR := output

# Packages to measure for coverage (exclude test helpers).
COVER_PKGS := $(shell go list ./... | grep -Ev '/(testutil)$$')

.PHONY: build test coverage lint lint-fix fmt clean release

build:
	go build -o $(BUILD_DIR)/$(BINARY) .

test:
	go test ./...

# Run tests with coverage and print a per-function summary.
coverage:
	go test -race -timeout 5m -coverprofile=coverage.out $(COVER_PKGS)
	@grep -v -E '^github\.com/wu/keyop/core/testutil' coverage.out > coverage-filtered.out
	go tool cover -func=coverage-filtered.out
	@rm -f coverage-filtered.out

lint:
	golangci-lint run ./...

lint-fix:
	golangci-lint run --fix ./...
	gofmt -w .

fmt:
	gofmt -w .

clean:
	rm -rf $(BUILD_DIR)
	rm -f coverage*.out

release: test lint
	@echo "Release checks passed: tests and lint successful"
