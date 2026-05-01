BINARY := keyop
BUILD_DIR := output

.PHONY: build test lint lint-fix fmt clean release

build:
	go build -o $(BUILD_DIR)/$(BINARY) .

test:
	go test ./...

lint:
	golangci-lint run ./...

lint-fix:
	golangci-lint run --fix ./...
	gofmt -w .

fmt:
	gofmt -w .

clean:
	rm -rf $(BUILD_DIR)

release: test lint
	@echo "Release checks passed: tests and lint successful"
