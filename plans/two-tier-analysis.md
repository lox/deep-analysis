# Two-Tier Analysis Plan

Goal: cut cost/latency while keeping quality by having GPT-5.2-Pro focus on reasoning while GPT-5.2 handles mechanical file operations.

## Architecture: Scout as Tool Dispatcher

Instead of an upfront scout pass that guesses what files are needed, the scout acts as a **runtime dispatcher** for the researcher's tools.

```
┌─────────────────────────────────────────────────────────────┐
│  Researcher (GPT-5.2-Pro)                                   │
│  - Focuses on reasoning, strategy, analysis                 │
│  - Decides WHAT to look for                                 │
│  - Has 3 high-level tools                                   │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│  Scout Dispatcher (GPT-5.2)                                 │
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
2. **Cost efficient**: GPT-5.2-Pro tokens spent on reasoning, not parsing file listings
3. **Iterative**: Researcher can refine searches based on what it learns
4. **Clear separation**: GPT-5.2-Pro = strategy/reasoning, GPT-5.2 = navigation/mechanics

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
- CLI flags for scout model selection

## Open Questions

- Should `read_files` go through scout at all, or direct to fileops?
- Max files per summarize call? (token limits)
- Should scout have access to manifest for context?
