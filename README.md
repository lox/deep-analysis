# Deep Analysis CLI

A CLI tool for systematic deep analysis of markdown documents and codebases using a two-tier AI architecture: GPT-5-Pro for reasoning and GPT-5.1 for file discovery.

## Features

- **Two-Tier Architecture**: GPT-5-Pro focuses on reasoning while GPT-5.1 handles file discovery
- **Three High-Level Tools**: `find_files`, `summarize_files`, `read_files` with cost controls
- **Session Continuity**: Continue conversations with `--continue <session-id>`
- **Cost Tracking**: Separate usage reporting for researcher and scout models

## Prerequisites

- Go 1.25.1 or later
- [OpenAI API Key](https://platform.openai.com/) with access to GPT-5-Pro and GPT-5.1

## Installation

```bash
# Build the CLI
go build -o dist/deep-analysis .

# Or with task
task build
```

## Configuration

Set your OpenAI API key:

```bash
export OPENAI_API_KEY="your-api-key-here"
```

## Usage

### Basic Analysis

```bash
# Analyze a markdown document (results appended in place)
./dist/deep-analysis notes.md

# Write output to a different file
./dist/deep-analysis notes.md --output annotated.md

# Analyze a project in a different directory
./dist/deep-analysis --cwd /path/to/project task.md
```

### Follow-up Questions

Each run generates a session ID logged to stderr:

```
INFO Saved session session=f1736654e6d5a7c1b58d14ac response_id=resp_xxx
```

To continue a conversation:

1. Add your follow-up question to the document
2. Run with `--continue`:

```bash
./dist/deep-analysis notes.md --continue f1736654e6d5a7c1b58d14ac
```

The AI will see your previous analysis and focus on new questions.

### CLI Flags

| Flag | Description |
|------|-------------|
| `--output` | Output file path (defaults to input file) |
| `--continue` | Session ID to continue a previous conversation |
| `--reset` | Start fresh, ignoring stored session state |
| `--cwd` | Working directory for file operations |
| `--scout-model` | Model for scout dispatcher (default: gpt-5.1) |
| `--debug` | Enable debug logging |

## How It Works

### Two-Tier Architecture

```
Researcher (GPT-5-Pro)     →  Reasoning, analysis, conclusions
        ↓
    find_files / summarize_files / read_files
        ↓
Scout (GPT-5.1)            →  Translates queries to glob/grep
        ↓
File System                →  Actual file access
```

### Tools Available to the Researcher

1. **find_files(query, paths)** - Discover files matching natural language intent
   - Returns file paths with sizes
   - Scout translates to glob/grep patterns

2. **summarize_files(paths, focus)** - Get AI-generated summaries (cheap, use liberally)
   - Scout reads and summarizes files
   - Use for triage before full reads

3. **read_files(paths)** - Read full file contents (expensive, use sparingly)
   - Limited to 10 files or 200KB per call
   - Exceeding limits returns an error with guidance

### Workflow

The researcher follows: **find → summarize → read**

1. `find_files("error handling")` → Returns 15 files (180KB total)
2. `summarize_files(all paths, "error patterns")` → Quick summaries
3. Identify 3 key files from summaries
4. `read_files(those 3)` → Full content for analysis
5. Write analysis citing specific code

### Cost Tracking

Each run reports usage for both models:

```
INFO Researcher usage (GPT-5-Pro) api_calls=5 input_tokens=12000 output_tokens=3000 cost_usd=$0.2400
INFO Scout usage (GPT-5.1)        api_calls=8 input_tokens=45000 output_tokens=800  cost_usd=$0.0964
INFO Total cost                   usd=$0.3364
```

## Development

```bash
task build   # Build to dist/deep-analysis
task test    # Run tests
task lint    # Run linter
```

## Architecture

```
.
├── main.go                      # CLI entrypoint
├── internal/
│   ├── agent/
│   │   ├── scout.go            # Scout dispatcher (GPT-5.1)
│   │   ├── manifest.go         # Project file listing
│   │   └── file_search.go      # Legacy file search
│   ├── client/
│   │   ├── deepanalysis.go     # Researcher client (GPT-5-Pro)
│   │   └── session_store.go    # Session persistence
│   ├── fileops/
│   │   └── fileops.go          # File operations (read, grep, glob)
│   └── server/                 # MCP server (optional)
└── plans/
    └── two-tier-analysis.md    # Architecture documentation
```

## License

MIT
