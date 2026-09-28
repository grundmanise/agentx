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

# The race detector is on unless RACE is empty. `make check` keeps it, as
# does the Linux job in CI. The macOS job runs `make test RACE=`: it is there
# for the serve watcher's FSEvents backend (cgo), which only builds on macOS,
# and the Linux job finds the data races, while -race costs the most on the
# slower macOS runners.
RACE ?= -race
# The CLI package's tests can run for longer than go test's default ten
# minutes on a slow runner, so allow more.
TEST_FLAGS = $(RACE) -count=1 -timeout 30m
# A binary built with -race that exits with status 0 sleeps for a second
# first, in case a goroutine still running races with the exit: every test
# binary would. The CLI's TestMain passes this on to the test binary's runs
# as a child process.
GORACE ?= atexit_sleep_ms=0

test:
	cd $(CLI) && GORACE='$(GORACE)' go test $(TEST_FLAGS) ./...
