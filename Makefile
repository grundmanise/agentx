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

.PHONY: check fmt fmt-check lint tidy-check build test test-shard

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

# The race detector runs the CLI package's tests for longer than go test's
# default ten minutes on the slower macOS runners, so allow more.
TEST_FLAGS = -race -count=1 -timeout 30m

test:
	cd $(CLI) && go test $(TEST_FLAGS) ./...

# CI runs the tests in SHARDS parallel parts. Part SHARD takes every SHARDS-th
# top-level test of the CLI package in sorted order; part 1 also runs every
# other package.
SHARD ?= 1
SHARDS ?= 3

test-shard:
	cd $(CLI) && tests=$$(go test -list . ./internal/cli | grep -E '^(Test|Example|Fuzz)' | LC_ALL=C sort | \
		awk -v i=$(SHARD) -v n=$(SHARDS) '(NR - 1) % n == i - 1' | paste -s -d '|' -) && \
		if [ -z "$$tests" ]; then echo "test-shard: no tests in part $(SHARD) of $(SHARDS)"; exit 1; fi && \
		if [ $(SHARD) = 1 ]; then go test $(TEST_FLAGS) $$(go list ./... | grep -v '/internal/cli$$'); fi && \
		go test $(TEST_FLAGS) -run "^($$tests)$$" ./internal/cli
