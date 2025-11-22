# Deep Analysis CLI

A CLI tool (`deep-analysis`) for systematic deep analysis of markdown documents and codebases using OpenAI GPT-5-Pro. It reads files, searches with grep/glob, and appends structured findings to your documents. MCP server pieces exist in the repo, but the primary interface is the CLI.

## Features

- **Deep Analysis**: Systematic, multi-step reasoning for complex problems
- **File Operations**: Read files, search with grep, discover files with glob patterns
- **Automatic Conversation Continuity**: Server-side conversation state management
- **Multiple Transports**: stdio, SSE, or HTTP streaming
- **Comprehensive Logging**: Stderr logging for debugging and monitoring

## Prerequisites

- Go 1.25.1 or later
- [OpenAI API Key](https://platform.openai.com/) with access to GPT-5-Pro (used as the underlying model)
- [Hermit](https://cashapp.github.io/hermit/) (optional, for environment management)

## Installation

### Using Hermit (Recommended)

```bash
# Activate hermit environment
. bin/activate-hermit

# Install dependencies
go mod download

# Build the CLI
task build   # outputs dist/deep-analysis
```

### Without Hermit

```bash
# Install dependencies
go mod download

# Build the CLI
go build -o dist/deep-analysis .
```

## Configuration

Set your OpenAI API key as an environment variable:

```bash
export OPENAI_API_KEY="your-api-key-here"
```

Or use direnv with `.envrc`:

```bash
export OPENAI_API_KEY="your-api-key-here"
```

## Usage (CLI)

```bash
# Build (or use task build)
go build -o dist/deep-analysis .

# Run analysis on a markdown file in place
./dist/deep-analysis --input notes.md

# Write output to a different file
./dist/deep-analysis --input notes.md --output annotated.md
```

## The `deep-analysis` Parameters

### Parameters

- **task** (required): The specific question or analysis you want performed
- **context** (optional): Background information, current situation, what you've tried
- **files** (optional): Array of file paths to automatically read and attach
- **continue** (optional, default: `true`): Continue previous conversation or start fresh
- **conversation_id** (optional): Identifier to continue a specific conversation

### Available Tools for the AI

The CLI-backed AI has access to these tools to gather information:

- **glob_files(pattern)**: Discover files matching glob patterns (e.g., `**/*.go`, `internal/**/test_*.go`)
- **read_file(path)**: Read contents of any file from the filesystem
- **grep_files(pattern, path, ignore_case)**: Search for regex patterns in files

The AI will automatically use these tools when it needs to examine code or gather context.

### Conversation Flow

Conversation state is maintained in the OpenAI thread; the CLI continues previous context unless you set `continue: false`.

### Examples

**Single Query:**
```json
{
  "task": "Explain the performance implications of using channels vs mutexes in Go"
}
```

**With Context:**
```json
{
  "task": "Review this authentication implementation for security issues",
  "context": "We're using JWT tokens but getting intermittent auth failures. The tokens are validated in middleware but some requests still fail."
}
```

**With Attached Files:**
```json
{
  "task": "Review the conversation continuity implementation and suggest improvements",
  "context": "Added default conversation ID but want to ensure thread-safety is robust",
  "files": ["internal/client/deepanalysis.go", "internal/server/mcp.go"]
}
```

**Multi-Turn Conversation:**

Query 1:
```json
{
  "task": "I'm investigating a memory leak in a Go application. The heap grows continuously."
}
```

Query 2 (automatically continues):
```json
{
  "task": "pprof shows 10,000+ goroutines blocked on channel receive. What should I check?"
}
```

**Starting Fresh:**
```json
{
  "task": "New question about database indexing strategies",
  "continue": false
}
```

## How It Works

The tool uses OpenAI's Responses API with GPT-5-Pro. The system prompt guides the model to:

1. **Discover**: Use glob_files to find relevant files
2. **Review**: Read pre-attached files and discovered files
3. **Investigate**: Read additional files and search for patterns
4. **Search**: Use grep_files to find code patterns across the codebase
5. **Verify**: Gather evidence before drawing conclusions
6. **Analyze**: Provide systematic, evidence-based recommendations

The AI proactively uses file operations to gather evidence when analyzing code, without requiring explicit user requests.

## Development

```bash
# Build
task build

# Run tests
task test

# Run linter
task lint

# Clean build artifacts
task clean

# Tidy dependencies
task tidy

# Start server in tmux
task start:tmux

# Stop server
task stop
```

## Architecture

```
.
├── main.go                      # CLI entrypoint
├── internal/
│   ├── client/
│   │   └── deepanalysis.go     # OpenAI Responses API client
│   ├── server/                 # MCP server wiring (secondary, optional)
│   └── fileops/
│       └── fileops.go          # File operation handlers (read, grep, glob)
└── Taskfile.yaml               # Build and development tasks
```

## Model Information

- **Underlying Model**: `gpt-5-pro`
- **Provider**: OpenAI
- **API**: OpenAI Responses API
- **Capabilities**: Advanced reasoning, function calling, extended context

## Logging

The server logs to stderr with timestamps. Logs include:

- Request details (task length, context length, files count)
- API calls and responses
- Tool executions (file reads, grep operations, glob searches)
- Response processing details

View logs when testing or check logs at `~/Library/Logs/` for your client; CLI logs to stderr.

## Pricing

Pricing is determined by OpenAI. Check current rates at https://platform.openai.com/docs/models/gpt-5-pro

## License

MIT

## Contributing

Issues and pull requests welcome.
