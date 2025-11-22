# Repository Guidelines

## Project Structure & Modules
- `main.go` wires the CLI and orchestrates analysis.
- `internal/client/` OpenAI Responses API client; `internal/server/` MCP server setup; `internal/fileops/` read/grep/glob/ck helpers plus tests; `internal/capture/` capture utilities.
- `Taskfile.yaml` task runner; `dist/` build outputs; `captures/` sample data; `bin/` Hermit bootstrap scripts.

## Build, Test, and Development Commands
- Activate Hermit (optional): `. bin/activate-hermit`
- Build binary: `task build` (outputs `dist/deep-analysis`) or `go build -o dist/deep-analysis .`
- Run tests: `task test` or `go test -v ./...`
- Lint: `task lint` (golangci-lint)
- Tidy deps: `task tidy`
- Run on a doc: `task run INPUT=notes.md [OUTPUT=annotated.md]`

## Coding Style & Naming Conventions
- Go 1.25.1; format with `gofmt`; lint must pass `golangci-lint run`.
- Packages and files are lower_snakecase; exported names use Go casing and include context (e.g., `Handler`, `SemanticSearch`).
- Keep functions small; accept `context.Context` first; fail fast with descriptive errors and log context via `charmbracelet/log`.
- Avoid swallowing errors; return wrapped errors with `%w`; respect file size and safety limits already enforced in `fileops`.

## Testing Guidelines
- Place tests alongside code (`*_test.go`); prefer table-driven cases.
- Name tests `TestFunctionName_Scenario`; cover new branches and regressions.
- Add a failing test before fixes when possible; keep `go test ./...` passing.
- For file/regex behavior, include edge cases: large files, invalid patterns, canceled contexts.

## Commit & PR Guidelines
- Commit format: `feat: ...`, `fix: ...`, or `chore: ...`; one logical change per commit explaining *why*.
- Run `task build`, `task test`, and `task lint` before submitting; never use `--no-verify` or `--no-gpg-sign`.
- PRs: clear description, linked issue/ticket, mention affected modules, and include before/after notes or logs if behavior changes.

## Configuration & Security
- Required env: `OPENAI_API_KEY` before running the CLI or server.
- Do not commit secrets or generated artifacts in `dist/` or `captures/`.
- Logging goes to stderr; scrub sensitive data from examples and test fixtures.
