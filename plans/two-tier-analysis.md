# Two-Tier Analysis Plan

Goal: cut latency and context churn while keeping quality by separating a researcher model from a scout model for mechanical file operations.

The workflow is provider-neutral, and the researcher and scout providers can be selected independently. OpenAI uses the Responses API; Anthropic uses the Messages API with the same three researcher tools and structured scout outputs. Global role defaults live in the XDG app config and can be overridden independently for one run, with optional per-model effort encoded as `provider.model@effort`.

## Architecture: Scout as Tool Dispatcher

Instead of an upfront scout pass that guesses what files are needed, the scout acts as a **runtime dispatcher** for the researcher's tools.

```
┌─────────────────────────────────────────────────────────────┐
│  Researcher model                                            │
│  - Focuses on reasoning, strategy, analysis                 │
│  - Decides WHAT to look for                                 │
│  - Has 3 high-level tools                                   │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│  Scout Dispatcher                                            │
│  - Translates NL queries to file operations                 │
│  - Handles HOW to find things                               │
│  - Summarizes file contents on demand                       │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│  Low-level tools: glob, grep, read, semantic_search         │
└─────────────────────────────────────────────────────────────┘
```

## Researcher Tools

### 1. find_files
Discover files matching natural language intent.

**Parameters:**
- `query` (string, required): Natural language description of what to find
  - Examples: "CFR trainer tests", "all zig files", "error handling code", "main entry point"
- `paths` ([]string, optional): Directories to search, defaults to ["."]

**Returns:** List of matching file paths with brief context (matching lines, relevance).

**Scout behavior:** Interprets query and dispatches to:
- `glob` for file patterns ("all zig files" → `**/*.zig`)
- `grep` for code patterns ("functions that return errors" → `grep "error"`)
- `semantic_search` for conceptual queries (if available)

### 2. summarize_files
Get scout-generated summaries of file contents.

**Parameters:**
- `paths` ([]string, required): Files to summarize
- `focus` (string, optional): What to focus on in the summary
  - Examples: "error handling patterns", "public API", "test coverage"

**Returns:** Summaries of each file, tailored to focus if provided.

**Scout behavior:** Reads files and generates concise summaries. With `focus`, filters to relevant parts.

### 3. read_files
Get full file contents for detailed analysis.

**Parameters:**
- `paths` ([]string, required): Files to read
- `ranges` (map[string][2]int, optional): Line ranges per file, e.g. `{"main.go": [1, 100]}`

**Returns:** Full file contents, with truncation notes for very large files.

**Scout behavior:** Minimal - just batch reads with intelligent truncation.

## Why This Is Better

1. **On-demand vs upfront**: Scout responds to what researcher actually needs, not predictions
2. **Cost efficient**: Researcher model context is spent on reasoning, not parsing file listings; deployments can override the scout model for lower-cost navigation
3. **Iterative**: Researcher can refine searches based on what it learns
4. **Clear separation**: Researcher model = strategy/reasoning, scout model = navigation/mechanics

## Implementation

### Phase 1: Scout Dispatcher
- Refactor `internal/agent/scout.go` to be a tool dispatcher
- Add `FindFiles`, `SummarizeFiles` methods
- Keep `ReadFiles` simple (maybe no scout needed)

### Phase 2: Researcher Tools
- Replace low-level tools in `DeepAnalysisClient` with three high-level tools
- Update system prompt to guide usage

### Phase 3: Polish
- Caching of scout results per session
- Streaming for long operations
- Automatic five-minute prompt caching and cache-aware cost reporting for Anthropic researcher runs (completed)
- Provider-qualified researcher/scout flags and global role defaults (completed)

## Decisions

- `read_files` uses the shared scout dispatcher for limits and formatting but does not make a scout-model call.
- `summarize_files` is constrained by per-file truncation rather than a fixed file-count limit.
- The scout receives a bounded project manifest for file-discovery context.
