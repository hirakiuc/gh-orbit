# Project variables
BINARY_NAME=gh-orbit
CMD_PATH=./cmd/gh-orbit
GOLANGCI_LINT_VERSION=v1.64.5

.PHONY: all build release-build test lint vulncheck fmt clean help

all: build

build:
	go build -o bin/$(BINARY_NAME) $(CMD_PATH)

release-build:
	GOOS=darwin GOARCH=amd64 go build -o bin/$(BINARY_NAME)-darwin-amd64 $(CMD_PATH)
	GOOS=darwin GOARCH=arm64 go build -o bin/$(BINARY_NAME)-darwin-arm64 $(CMD_PATH)
	GOOS=linux GOARCH=amd64 go build -o bin/$(BINARY_NAME)-linux-amd64 $(CMD_PATH)
	GOOS=linux GOARCH=arm64 go build -o bin/$(BINARY_NAME)-linux-arm64 $(CMD_PATH)
	GOOS=windows GOARCH=amd64 go build -o bin/$(BINARY_NAME)-windows-amd64.exe $(CMD_PATH)

test:
	go test -v ./...

lint:
	golangci-lint run ./...

vulncheck:
	govulncheck ./...

fmt:
	gofumpt -l -w .

clean:
	rm -rf bin/
	go clean

help:
	@echo "Available targets:"
	@echo "  build         - Build the local binary"
	@echo "  release-build - Cross-compile for Darwin, Linux, Windows"
	@echo "  test          - Run tests"
	@echo "  lint          - Run golangci-lint"
	@echo "  vulncheck     - Run govulncheck for security"
	@echo "  fmt           - Format code with gofumpt"
	@echo "  clean         - Remove build artifacts"
