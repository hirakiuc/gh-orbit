# Configuration
TARGET ?= .

.PHONY: all lint lint-markdown

all: init

# Lint markdown files and links
lint: lint-markdown

# Lint markdown files
lint-markdown:
	@echo "Running markdownlint (via Docker)..."
	@docker run --rm -v $(CURDIR):/workdir -w /workdir davidanson/markdownlint-cli2:v0.21.0 "**/*.md" "#node_modules"

