# The checks CI runs: .github/workflows/ci.yml calls these targets. Run `make check` before pushing.

CLI = apps/cli
# Pinned in one place.
GOLANGCI_LINT_VERSION = $(shell cat $(CLI)/.golangci-lint-version)
# `go run pkg@version` resolves its toolchain from that package's own go.mod and
# ignores ours, so a machine whose `go` predates the linter's minimum switches
# down to that minimum, and the binary it builds then refuses this module's
# newer `go` directive. Build the linter with the Go version the module targets;
# `+auto` still upgrades if the linter ever asks for more.
GO_TOOLCHAIN = go$(shell awk '$$1 == "go" { print $$2; exit }' $(CLI)/go.mod)+auto
# Static on Linux; cgo on macOS, where the serve watcher uses FSEvents.
CGO_ENABLED ?= $(if $(filter Darwin,$(shell uname -s)),1,0)

.PHONY: check fmt fmt-check lint tidy-check build test

check: fmt-check lint tidy-check build test

fmt:
	cd $(CLI) && gofmt -w .

fmt-check:
	@cd $(CLI) && files=$$(gofmt -l .) && if [ -n "$$files" ]; then echo "$$files"; echo "gofmt: the files above are not formatted, run 'make fmt'"; exit 1; fi

lint:
	cd $(CLI) && GOTOOLCHAIN=$(GO_TOOLCHAIN) go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_LINT_VERSION) run ./...

tidy-check:
	cd $(CLI) && go mod tidy && git diff --exit-code go.mod go.sum

build:
	cd $(CLI) && CGO_ENABLED=$(CGO_ENABLED) go build ./...

test:
	cd $(CLI) && go test -race -count=1 ./...
