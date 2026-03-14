# Repository Guidelines

## Project Overview

Deep Analysis CLI - a two-tier AI tool for codebase analysis:
- **Researcher** (GPT-5.4-Pro): Reasoning and analysis
- **Scout** (GPT-5.4): File discovery and summarization

## Project Structure

```
main.go                         # CLI entrypoint
internal/
  agent/
    scout.go                    # Scout dispatcher (GPT-5.4)
    manifest.go                 # Project file listing
    fileops.go                  # FileOps interface
    file_search.go              # Legacy file search (unused)
  client/
    deepanalysis.go             # Researcher client (GPT-5.4-Pro)
    session_store.go            # Session persistence (XDG state)
  fileops/
    fileops.go                  # File operations (read, grep, glob)
  server/                       # MCP server (optional, secondary)
plans/
  two-tier-analysis.md          # Architecture documentation
```

## Build, Test, and Lint

```bash
mise install
mise run build          # Build to dist/deep-analysis
mise run install        # Install to GOBIN or GOPATH/bin
mise run install:global # Install to ~/bin
mise run test           # Run tests
mise run lint           # Run golangci-lint
```

## Usage

```bash
# Basic analysis (uses xhigh reasoning by default)
deep-analysis task.md

# Analyze external project
deep-analysis --cwd /path/to/project task.md

# Continue a session
deep-analysis task.md --continue <session-id>

# Use lower reasoning effort for faster responses
deep-analysis --reasoning-effort high task.md
```

## Coding Style

- Go 1.25.3 via `mise`; format with `gofmt`; lint must pass
- Accept `context.Context` first in functions
- Return wrapped errors with `%w`
- Log with `charmbracelet/log`

## Architecture Notes

### Tools exposed to Researcher (GPT-5.4-Pro)

1. **find_files(query, paths)** - Scout translates NL to glob/grep
2. **summarize_files(paths, focus)** - Scout reads and summarizes
3. **read_files(paths)** - Direct read with limits (10 files, 200KB)

### Cost Controls

- `find_files` returns file sizes
- `read_files` enforces limits with clear error messages
- System prompt guides find → summarize → read workflow

### Session Continuity

- Sessions stored in `~/.local/state/deep-analysis/sessions/`
- `--continue <id>` loads `PreviousResponseID` for conversation context
- Continuation note injected to guide researcher on follow-ups

## Commit Guidelines

- Format: `feat:`, `fix:`, or `chore:`
- Run `mise run build`, `mise run test`, `mise run lint` before committing
- One logical change per commit

## Configuration

Required: `OPENAI_API_KEY` environment variable
