# lazygit (personal fork)

Also read AGENTS.md (upstream's agent guide) and follow it.

## Build & Run

- `just build` — build binary with version info to `./lazygit`
- `make build` — debug build (no optimizations)
- `make run` — build and run
- `make run-debug` — run with debug logging (view with `make print-log` in another tab)

## Test

- `make unit-test` — unit tests (`go test ./... -short`)
- `make integration-test-tui` — interactive integration test runner
- `make test` — all tests (unit + integration)

## Lint & Format

- `make lint` — golangci-lint
- `make format` — gofumpt

## Custom Modifications (fork-specific)

- **AI commit messages**: `pkg/gui/ai_commit.go` — calls Anthropic or Gemini API to generate commit messages from staged diff
- **API keys**: stored in `~/.config/lazygit/.env` as `ANTHROPIC_API_KEY` or `GEMINI_API_KEY`
- **Config**: `pkg/config/user_config.go` `AIConfig` struct — enabled, provider, model, prompt, timeout, fallbackOnError
- **UI**: AI indicator icon (󰚩) in Files panel title and information panel
- **Commit age column**: `pkg/gui/presentation/commits.go` — commits panel shows a compact relative age (`4w`, `3h`) in normal screen mode via `utils.UnixToTimeAgoAt`; enlarged mode (`+`) keeps upstream's absolute dates

## Architecture

- `pkg/gui/` — UI layer (gocui-based TUI)
- `pkg/commands/` — git command wrappers
- `pkg/config/` — user/app configuration
- `pkg/integration/tests/` — integration tests (robot-driven TUI tests)
- `pkg/i18n/` — translations

## Gotchas

- Binary at `./lazygit` is what `lg` alias points to — must `just build` after code changes
- Config dir is `~/.config/lazygit/` (XDG), not `~/Library/Application Support/lazygit/`
- Lazygit runs inside other repos — `git rev-parse` resolves to the _target_ repo, not lazygit source. Use `config.ConfigDir()` for lazygit's own files.
- Integration tests need `go generate ./...` if test list changes
- Custom build targets live in `justfile` (upstream's, lowercase — merged with our recipes: `build` is our version-stamped build, upstream's debug build is `build-debug`, plus `smoke-ai`); don't add to Makefile
- Generated docs/schema for master live in `docs-master/` and `schema-master/` (upstream convention); `docs/` tracks the latest release
