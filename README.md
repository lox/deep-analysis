# Deep Analysis CLI

A CLI tool for systematic deep analysis of markdown documents and codebases using a two-tier AI architecture: a researcher model for reasoning and a scout model for file discovery.

> 🤖  **Note:** This project was ["vibe engineered"](https://simonwillison.net/2025/Oct/7/vibe-engineering/) with [Amp](https://ampcode.com) and Claude Opus 4.5 and others as part of my ongoing effort to demonstrate that AI-assisted development can produce high-quality software when paired with rigorous design documentation, comprehensive tests, and careful human review.

## Features

- **Two-Tier Architecture**: The researcher model focuses on reasoning while the scout model handles file discovery
- **OpenAI and Anthropic**: Run the same workflow with GPT or Claude models, including Fable and Opus
- **Three High-Level Tools**: `find_files`, `summarize_files`, `read_files` with cost controls
- **Session Continuity**: Continue conversations with `--continue <session-id>`
- **Cost Tracking**: Separate usage reporting for researcher and scout models

## Prerequisites

- [mise](https://mise.jdx.dev/) for the pinned Go and CLI toolchain in `mise.toml`
- Go 1.25.3 or later if building without `mise`
- An [OpenAI API key](https://platform.openai.com/) or [Anthropic API key](https://platform.claude.com/), depending on the selected provider

## Installation

```bash
mise install

# Build the CLI with the pinned toolchain
mise run build

# Install into your Go bin dir
mise run install

# Install into ~/bin
mise run install:global

# Or directly with Go if you already have it installed
mkdir -p dist
go build -o dist/deep-analysis .
```

## Configuration

Set your OpenAI API key with an environment variable:

```bash
export OPENAI_API_KEY="your-api-key-here"
```

For Anthropic models, use:

```bash
export ANTHROPIC_API_KEY="your-api-key-here"
```

Or store it in an XDG config file:

```bash
./dist/deep-analysis setup

# Or configure Anthropic
./dist/deep-analysis setup --provider anthropic
```

The shared config stores keys as `openai_api_key` and `anthropic_api_key`. `deep-analysis` also checks the provider-specific `${XDG_CONFIG_HOME:-$HOME/.config}/openai/config.yaml` and `anthropic/config.yaml` files, where `api_key` is accepted. Environment variables take precedence for credentials.

Set global researcher and scout defaults in the shared config so every run can use the same provider mix:

```yaml
researcher: anthropic.claude-opus-4-8
scout: openai.gpt-5.5
```

These fields can live alongside the API keys. Model selection precedence is command-line flag, then global config, then the compiled OpenAI defaults. Running `setup` to add or replace a provider key preserves the configured model defaults.

Check the installed binary, effective models, and credential sources:

```bash
./dist/deep-analysis doctor
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

# Use Fable 5 for research and Sonnet 5 for scouting
./dist/deep-analysis task.md \
  --researcher anthropic.claude-fable-5 \
  --scout anthropic.claude-sonnet-5

# Use Opus as the researcher instead
./dist/deep-analysis task.md \
  --researcher anthropic.claude-opus-4-8 \
  --scout anthropic.claude-sonnet-5

# Mix providers: Fable researcher with an OpenAI scout
./dist/deep-analysis task.md \
  --researcher anthropic.claude-fable-5 \
  --scout openai.gpt-5.5

# Or GPT researcher with an Anthropic scout
./dist/deep-analysis task.md \
  --researcher openai.gpt-5.5-pro \
  --scout anthropic.claude-sonnet-5
```

Qualified model values split at the first dot. The provider must be `openai` or `anthropic`; the remaining model ID is passed through unchanged, so new model names do not require a CLI release.

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
| `--researcher` | Researcher as `provider.model` (overrides global config; compiled default: `openai.gpt-5.5-pro`) |
| `--scout` | Scout as `provider.model` (overrides global config; compiled default: `openai.gpt-5.5`) |
| `--reasoning-effort` | Reasoning effort: low, medium, high, xhigh (default: xhigh) |
| `--debug` | Enable debug logging |

### Commands

| Command | Description |
|---------|-------------|
| `analyze <input>` | Analyze a markdown document (default command) |
| `setup [--provider openai\|anthropic]` | Prompt for a provider API key and write XDG config |
| `doctor` | Check the installed binary, effective models, and credential configuration |

## How It Works

### Two-Tier Architecture

```
Researcher model           →  Reasoning, analysis, conclusions
        ↓
    find_files / summarize_files / read_files
        ↓
Scout model                →  Translates queries to glob/grep
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
INFO Researcher usage model=<researcher-model> api_calls=5 input_tokens=12000 output_tokens=3000 cost_usd=$0.9000
INFO Scout usage      model=<scout-model>      api_calls=8 input_tokens=45000 output_tokens=800  cost_usd=$0.2490
INFO Total cost                         usd=$1.1490
```

## Development

```bash
mise run build  # Build to dist/deep-analysis
mise run install  # Install to GOBIN or GOPATH/bin
mise run install:global  # Install to ~/bin
mise run test   # Run tests
mise run lint   # Run linter
mise run run notes.md
mise run run notes.md --output annotated.md
```

## Architecture

```
.
├── main.go                      # CLI entrypoint
├── internal/
│   ├── agent/
│   │   ├── scout.go            # Scout dispatcher default
│   │   ├── manifest.go         # Project file listing
│   │   └── file_search.go      # Legacy file search
│   ├── client/
│   │   ├── deepanalysis.go     # OpenAI researcher and shared workflow
│   │   ├── anthropic.go        # Anthropic researcher tool loop
│   │   └── session_store.go    # Session persistence
│   ├── fileops/
│   │   └── fileops.go          # File operations (read, grep, glob)
│   └── server/                 # MCP server (optional)
└── plans/
    └── two-tier-analysis.md    # Architecture documentation
```

## License

MIT
