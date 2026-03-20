# Project variables
BINARY_NAME=gh-orbit
CMD_PATH=./cmd/gh-orbit
GOLANGCI_LINT_VERSION=v2.11.3

# Sandbox-native development environment
PROJECT_TMP ?= $(CURDIR)/tmp
export GOCACHE ?= $(PROJECT_TMP)/go-cache
export GOLANGCI_LINT_CACHE ?= $(PROJECT_TMP)/lint-cache

# OS-specific sed handling
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
    SED_INPLACE := sed -i ''
else
    SED_INPLACE := sed -i
endif

.PHONY: all build release-build test lint vulncheck fmt clean clean-tmp help generate serena coverage coverage-summary artifacts roadmap task

all: build

# Ensure sandbox directory structure
$(PROJECT_TMP):
	@mkdir -p $(PROJECT_TMP)

build: $(PROJECT_TMP)
	go build -o bin/$(BINARY_NAME) $(CMD_PATH)
	@if [ "$$(uname)" = "Darwin" ]; then \
		echo "Ad-hoc signing binary for macOS..."; \
		codesign -f -s - bin/$(BINARY_NAME); \
	fi

coverage: $(PROJECT_TMP)
	go test -coverprofile=$(PROJECT_TMP)/coverage.out ./...
	grep -vE "mock_|types/|cmd/gh-orbit" $(PROJECT_TMP)/coverage.out > $(PROJECT_TMP)/coverage.filtered.out
	go tool cover -html=$(PROJECT_TMP)/coverage.filtered.out -o $(PROJECT_TMP)/coverage.html
	@echo "Coverage report generated at $(PROJECT_TMP)/coverage.html (filtered)"

coverage-summary: coverage
	go tool cover -func=$(PROJECT_TMP)/coverage.filtered.out

artifacts: $(PROJECT_TMP)
	@mkdir -p artifacts
	go test -v -artifacts ./...

release-build: $(PROJECT_TMP)
	GOOS=darwin GOARCH=amd64 go build -o bin/$(BINARY_NAME)-darwin-amd64 $(CMD_PATH)
	@if [ "$$(uname)" = "Darwin" ]; then codesign -f -s - bin/$(BINARY_NAME)-darwin-amd64; fi
	GOOS=darwin GOARCH=arm64 go build -o bin/$(BINARY_NAME)-darwin-arm64 $(CMD_PATH)
	@if [ "$$(uname)" = "Darwin" ]; then codesign -f -s - bin/$(BINARY_NAME)-darwin-arm64; fi
	GOOS=linux GOARCH=amd64 go build -o bin/$(BINARY_NAME)-linux-amd64 $(CMD_PATH)
	GOOS=linux GOARCH=arm64 go build -o bin/$(BINARY_NAME)-linux-arm64 $(CMD_PATH)
	GOOS=windows GOARCH=amd64 go build -o bin/$(BINARY_NAME)-windows-amd64.exe $(CMD_PATH)

test: $(PROJECT_TMP)
	go test -v ./...

lint: $(PROJECT_TMP)
	golangci-lint run ./...
	$(MAKE) lint-docs

lint-docs:
	markdownlint-cli2

vulncheck: $(PROJECT_TMP)
	govulncheck ./...

fmt: $(PROJECT_TMP)
	gofumpt -l -w .

generate:
	@echo "Generating mocks using packages configuration..."
	@go run github.com/vektra/mockery/v2

serena:
	@echo "Starting Serena MCP server..."
	@uvx --from git+https://github.com/oraios/serena serena start-mcp-server --transport streamable-http --host localhost --port 9121 --project . --context ide-assistant

roadmap:
	@echo "--- Project Roadmap (GitHub Milestones) ---"
	@gh api repos/:owner/:repo/milestones --jq '.[] | "[\(.number)] \(.title) (\(.state)) - \(.open_issues) open, \(.closed_issues) closed"'
	@echo ""
	@echo "--- Active Issues by Milestone ---"
	@gh issue list --search "state:open" --json milestone,number,title --jq '.[] | "[\(.milestone.title)] #\(.number) \(.title)"' | sort

task:
	@if [ -z "$(ID)" ]; then echo "Usage: make task ID=<issue-number>"; exit 1; fi
	@echo "Initializing workbench for Issue #$(ID)..."
	@rm -f .agents/issue.md .agents/proposal.md .agents/feedback.md
	@gh issue view "$(ID)" --json title,body,state,labels,milestone --template 'Title: {{.title}}\n\nBody: {{.body}}\n\nLabels: {{range .labels}}{{.name}} {{end}}\nMilestone: {{if .milestone}}{{.milestone.title}}{{else}}None{{end}}\nState: {{.state}}\n' > .agents/issue.md || (echo "Error: Issue #$(ID) not found."; exit 1)
	@cp .agents/workflows/strategy-review/TEMPLATE.md .agents/proposal.md
	@$(SED_INPLACE) "s/\[ID\]/$(ID)/g" .agents/proposal.md
	@echo "Workbench ready: .agents/issue.md and .agents/proposal.md initialized."

clean-tmp:
	@echo "Cleaning up local sandbox directory..."
	rm -rf $(PROJECT_TMP)/*
	@touch $(PROJECT_TMP)/.gitkeep

clean: clean-tmp
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
	@echo "  clean-tmp     - Remove project-local sandbox files"
