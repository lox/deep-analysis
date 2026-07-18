# Repository Guidelines

## Project Overview

Deep Analysis CLI - a two-tier AI tool for codebase analysis:
- **Researcher**: Reasoning and analysis
- **Scout**: File discovery and summarization

## Project Structure

```
main.go                         # CLI entrypoint
internal/
  agent/
    scout.go                    # Scout dispatcher
    manifest.go                 # Project file listing
    fileops.go                  # FileOps interface
    file_search.go              # Legacy file search (unused)
  client/
    deepanalysis.go             # Researcher client
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

# Use lower researcher effort for faster responses
deep-analysis task.md --researcher openai.gpt-5.6-sol@high

# Check install/config state
deep-analysis doctor

# Use Fable research with a Sonnet scout
deep-analysis task.md --researcher anthropic.claude-fable-5@xhigh --scout anthropic.claude-sonnet-5@low

# Select Opus explicitly
deep-analysis task.md --researcher anthropic.claude-opus-4-8@xhigh --scout anthropic.claude-sonnet-5@low

# Mix Fable research with an OpenAI scout
deep-analysis task.md --researcher anthropic.claude-fable-5@xhigh --scout openai.gpt-5.5@low
```

## Coding Style

- Go 1.25.3 via `mise`; format with `gofmt`; lint must pass
- Accept `context.Context` first in functions
- Return wrapped errors with `%w`
- Log with `charmbracelet/log`

## Architecture Notes

### Tools exposed to Researcher

1. **find_files(query, paths)** - Scout translates NL to glob/grep
2. **summarize_files(paths, focus)** - Scout reads and summarizes
3. **read_files(paths)** - Direct read with limits (10 files, 200KB)

### Cost Controls

- `find_files` returns file sizes
- `read_files` enforces limits with clear error messages
- System prompt guides find → summarize → read workflow

### Session Continuity

- Sessions stored in `~/.local/state/deep-analysis/sessions/`
- `--continue <id>` resumes context with the same researcher/scout provider pair
- Continuation note injected to guide researcher on follow-ups

## Commit Guidelines

- Format: `feat:`, `fix:`, or `chore:`
- Run `mise run build`, `mise run test`, `mise run lint` before committing
- One logical change per commit

## Configuration

Required: `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` for the selected provider. Keys can also live in `~/.config/deep-analysis/config.yaml` or the provider-specific `~/.config/{openai,anthropic}/config.yaml`. Global `researcher` and `scout` defaults live in `~/.config/deep-analysis/config.yaml`; CLI flags override them, and `setup` preserves them.
