# The checks CI runs: .github/workflows/ci.yml calls these targets. Run `make check` before pushing.
#
# Naming: `<action>-<app>`. `check-cli` and `check-desktop` run every check for one app, and `check`
# runs both. The CLI's single steps follow the same scheme: fmt-cli, fmt-check-cli, lint-cli,
# tidy-check-cli, build-cli and test-cli.

CLI = apps/cli
DESKTOP = apps/desktop
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

.PHONY: check check-cli check-desktop fmt-cli fmt-check-cli lint-cli tidy-check-cli build-cli test-cli

check: check-cli check-desktop

# The CLI in apps/cli. Needs Go.
check-cli: fmt-check-cli lint-cli tidy-check-cli build-cli test-cli

fmt-cli:
	cd $(CLI) && gofmt -w .

fmt-check-cli:
	@cd $(CLI) && files=$$(gofmt -l .) && if [ -n "$$files" ]; then echo "$$files"; echo "gofmt: the files above are not formatted, run 'make fmt-cli'"; exit 1; fi

lint-cli:
	cd $(CLI) && GOTOOLCHAIN=$(GO_TOOLCHAIN) go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_LINT_VERSION) run ./...

tidy-check-cli:
	cd $(CLI) && go mod tidy && git diff --exit-code go.mod go.sum

build-cli:
	cd $(CLI) && CGO_ENABLED=$(CGO_ENABLED) go build ./...

# `make test-cli RACE=` runs the tests without the race detector. CI does that on
# macOS, where the race detector makes the tests take too long; the Linux job
# still finds data races.
RACE ?= -race

test-cli:
	cd $(CLI) && go test $(RACE) -count=1 ./...

# The desktop app in apps/desktop: the pnpm install at the root, the frontend's types, formatting,
# lint, end-to-end tests and build, then the Tauri shell's formatting and Clippy. Needs Node 24 or
# later, pnpm 12, Playwright's Chromium and Rust; on Linux also the WebKitGTK development packages.
check-desktop:
	pnpm install --frozen-lockfile
	cd $(DESKTOP) && pnpm typecheck && pnpm format-check && pnpm lint && pnpm test && pnpm build
	cd $(DESKTOP)/src-tauri && cargo fmt --check && cargo clippy --locked -- -D warnings
