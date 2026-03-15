# Project variables
BINARY_NAME=gh-orbit
CMD_PATH=./cmd/gh-orbit
GOLANGCI_LINT_VERSION=v2.11.3

# OS-specific sed handling
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
    SED_INPLACE := sed -i ''
else
    SED_INPLACE := sed -i
endif

.PHONY: all build release-build test lint vulncheck fmt clean help generate serena coverage coverage-summary artifacts roadmap task

all: build

build:
	go build -o bin/$(BINARY_NAME) $(CMD_PATH)
	@if [ "$$(uname)" = "Darwin" ]; then \
		echo "Ad-hoc signing binary for macOS..."; \
		codesign -f -s - bin/$(BINARY_NAME); \
	fi

coverage:
	go test -coverprofile=coverage.out ./...
	grep -vE "mock_|types/|cmd/gh-orbit" coverage.out > coverage.filtered.out
	go tool cover -html=coverage.filtered.out -o coverage.html
	@echo "Coverage report generated at coverage.html (filtered)"

coverage-summary: coverage
	go tool cover -func=coverage.filtered.out

artifacts:
	@mkdir -p artifacts
	go test -v -artifacts ./...

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
