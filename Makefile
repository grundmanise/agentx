# Local checks, identical to .github/workflows/ci.yml. Run `make check` before pushing.

GOLANGCI_LINT_VERSION = v2.13.2
CLI = apps/cli

.PHONY: check fmt fmt-check lint tidy-check build test

check: fmt-check lint tidy-check build test

fmt:
	cd $(CLI) && gofmt -w .

fmt-check:
	@cd $(CLI) && files=$$(gofmt -l .) && if [ -n "$$files" ]; then echo "$$files"; echo "gofmt: the files above are not formatted, run 'make fmt'"; exit 1; fi

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not found, install it with: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)"; exit 1; }
	cd $(CLI) && golangci-lint run ./...

tidy-check:
	cd $(CLI) && go mod tidy && git diff --exit-code go.mod go.sum

build:
	cd $(CLI) && CGO_ENABLED=0 go build ./...

test:
	cd $(CLI) && go test -race -count=1 ./...
