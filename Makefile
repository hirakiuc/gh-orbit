# Project variables
BINARY_NAME=gh-orbit
CMD_PATH=./cmd/gh-orbit
GOLANGCI_LINT_VERSION=v2.10.1

.PHONY: all build release-build test lint vulncheck fmt clean help generate serena

all: build

build:
	go build -o bin/$(BINARY_NAME) $(CMD_PATH)
	@if [ "$$(uname)" = "Darwin" ]; then \
		echo "Ad-hoc signing binary for macOS..."; \
		codesign -f -s - bin/$(BINARY_NAME); \
	fi

release-build:
	GOOS=darwin GOARCH=amd64 go build -o bin/$(BINARY_NAME)-darwin-amd64 $(CMD_PATH)
	@if [ "$$(uname)" = "Darwin" ]; then codesign -f -s - bin/$(BINARY_NAME)-darwin-amd64; fi
	GOOS=darwin GOARCH=arm64 go build -o bin/$(BINARY_NAME)-darwin-arm64 $(CMD_PATH)
	@if [ "$$(uname)" = "Darwin" ]; then codesign -f -s - bin/$(BINARY_NAME)-darwin-arm64; fi
	GOOS=linux GOARCH=amd64 go build -o bin/$(BINARY_NAME)-linux-amd64 $(CMD_PATH)
	GOOS=linux GOARCH=arm64 go build -o bin/$(BINARY_NAME)-linux-arm64 $(CMD_PATH)
	GOOS=windows GOARCH=amd64 go build -o bin/$(BINARY_NAME)-windows-amd64.exe $(CMD_PATH)

test:
	go test -v ./...

lint:
	golangci-lint run ./...
	$(MAKE) lint-docs

lint-docs:
	markdownlint-cli2

vulncheck:
	govulncheck ./...

fmt:
	gofumpt -l -w .

generate:
	@echo "Generating mocks using packages configuration..."
	@go run github.com/vektra/mockery/v2

serena:
	@echo "Starting Serena MCP server..."
	@uvx --from git+https://github.com/oraios/serena serena start-mcp-server

clean:
	rm -rf bin/
	rm -rf internal/api/mocks/
	go clean

help:
	@echo "Available targets:"
	@echo "  build         - Build the local binary"
	@echo "  release-build - Cross-compile for Darwin, Linux, Windows"
	@echo "  generate      - Generate mocks using mockery"
	@echo "  serena        - Start Serena MCP server"
	@echo "  test          - Run tests"
	@echo "  lint          - Run golangci-lint"
	@echo "  vulncheck     - Run govulncheck for security"
	@echo "  fmt           - Format code with gofumpt"
	@echo "  clean         - Remove build artifacts"
