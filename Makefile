# Local checks, identical to .github/workflows/ci.yml. Run `make check` before pushing.

CLI = apps/cli
# Pinned in one place; CI reads the same file through the golangci-lint action.
GOLANGCI_LINT_VERSION = $(shell cat $(CLI)/.golangci-lint-version)
# Static on Linux; cgo on macOS, where the serve watcher uses FSEvents.
CGO_ENABLED ?= $(if $(filter Darwin,$(shell uname -s)),1,0)

.PHONY: check fmt fmt-check lint tidy-check build test

check: fmt-check lint tidy-check build test

fmt:
	cd $(CLI) && gofmt -w .

fmt-check:
	@cd $(CLI) && files=$$(gofmt -l .) && if [ -n "$$files" ]; then echo "$$files"; echo "gofmt: the files above are not formatted, run 'make fmt'"; exit 1; fi

lint:
	cd $(CLI) && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_LINT_VERSION) run ./...

tidy-check:
	cd $(CLI) && go mod tidy && git diff --exit-code go.mod go.sum

build:
	cd $(CLI) && CGO_ENABLED=$(CGO_ENABLED) go build ./...

test:
	cd $(CLI) && go test -race -count=1 ./...
