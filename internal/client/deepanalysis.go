package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/log"
	harness "github.com/lox/agent-harness"
	harnessopenai "github.com/lox/agent-harness/provider/openai"
	"github.com/lox/deep-analysis/internal/agent"
)

const (
	// renovate: depName=openai/gpt-latest-sol
	DefaultResearcherModel = "gpt-5.6-sol"
	AnthropicProvider      = "anthropic"
	OpenAIProvider         = "openai"
	maxIterations          = 50
	promptCacheVersion     = "v4"
)

// Analyzer runs the researcher/scout analysis workflow.
type Analyzer interface {
	Analyze(ctx context.Context, document string, opts AnalysisOptions) (AnalysisResult, error)
}

// DefaultModelsForProvider returns the two-tier defaults for a provider.
func DefaultModelsForProvider(provider string) (researcherModel, scoutModel string, err error) {
	switch provider {
	case OpenAIProvider:
		return DefaultResearcherModel, agent.DefaultScoutModel, nil
	case AnthropicProvider:
		return DefaultAnthropicResearcherModel, DefaultAnthropicScoutModel, nil
	default:
		return "", "", fmt.Errorf("unsupported provider %q", provider)
	}
}

// NewForProviders creates an analyzer with independently selected researcher and scout providers.
func NewForProviders(researcherProvider, researcherAPIKey, scoutProvider, scoutAPIKey string, fileOps agent.FileOps, researcherModel, scoutModel, scoutEffort string) (Analyzer, error) {
	if researcherModel == "" {
		defaultResearcherModel, _, err := DefaultModelsForProvider(researcherProvider)
		if err != nil {
			return nil, err
		}
		researcherModel = defaultResearcherModel
	}
	if scoutModel == "" {
		_, defaultScoutModel, err := DefaultModelsForProvider(scoutProvider)
		if err != nil {
			return nil, err
		}
		scoutModel = defaultScoutModel
	}

	var scout *agent.Scout
	switch scoutProvider {
	case OpenAIProvider:
		scout = agent.NewScout(scoutAPIKey, scoutModel, scoutEffort, fileOps)
	case AnthropicProvider:
		scout = agent.NewAnthropicScout(scoutAPIKey, scoutModel, scoutEffort, fileOps)
	default:
		return nil, fmt.Errorf("unsupported scout provider %q", scoutProvider)
	}

	switch researcherProvider {
	case OpenAIProvider:
		return newOpenAIWithScout(researcherAPIKey, fileOps, researcherModel, scoutModel, scout), nil
	case AnthropicProvider:
		return newAnthropicWithScout(researcherAPIKey, fileOps, researcherModel, scoutModel, scout), nil
	default:
		return nil, fmt.Errorf("unsupported researcher provider %q", researcherProvider)
	}
}

// DeepAnalysisClient owns the deep-analysis researcher policy and runs it through agent-harness.
type DeepAnalysisClient struct {
	provider        harness.Provider
	providerName    string
	scout           *agent.Scout
	researcherModel string
	scoutModel      string
	tools           []harness.Tool
	toolCache       map[string]string
	cacheMu         sync.Mutex
}

// AnalysisOptions controls request behavior.
type AnalysisOptions struct {
	PreviousResponseID string
	ReasoningEffort    string // Reasoning effort: low, medium, high, xhigh; empty uses the provider default.
}

// AnalysisResult contains the final model output and metadata.
type AnalysisResult struct {
	Text       string
	ResponseID string
}

// New creates a new DeepAnalysisClient instance.
func New(apiKey string, fileOps agent.FileOps, researcherModel, scoutModel string) *DeepAnalysisClient {
	return newOpenAIWithScout(apiKey, fileOps, researcherModel, scoutModel, agent.NewScout(apiKey, scoutModel, "", fileOps))
}

func newOpenAIWithScout(apiKey string, fileOps agent.FileOps, researcherModel, scoutModel string, scout *agent.Scout) *DeepAnalysisClient {
	provider := harnessopenai.New(
		harnessopenai.WithAPIKey(apiKey),
		harnessopenai.WithDefaultModel(researcherModel),
	)
	return newOpenAIWithProvider(provider, researcherModel, scoutModel, scout)
}

func newOpenAIWithProvider(provider harness.Provider, researcherModel, scoutModel string, scout *agent.Scout) *DeepAnalysisClient {
	if researcherModel == "" {
		researcherModel = DefaultResearcherModel
	}
	if scoutModel == "" {
		scoutModel = agent.DefaultScoutModel
	}

	c := &DeepAnalysisClient{
		provider:        provider,
		providerName:    OpenAIProvider,
		scout:           scout,
		researcherModel: researcherModel,
		scoutModel:      scoutModel,
		toolCache:       make(map[string]string),
	}
	c.tools = c.buildTools()

	return c
}

// Analyze processes a markdown document and returns the analysis result
func (c *DeepAnalysisClient) Analyze(ctx context.Context, document string, opts AnalysisOptions) (AnalysisResult, error) {
	return c.analyzeWithHarness(ctx, document, opts)
}

// executeFunction executes a function call requested by the model
func (c *DeepAnalysisClient) executeFunction(ctx context.Context, name, argsJSON string) (string, error) {
	cacheKey := name + "|" + argsJSON
	if cached, ok := c.getCachedToolOutput(cacheKey); ok {
		log.Debug("Tool cache hit", "tool", name)
		return cached, nil
	}

	var result string
	var err error

	switch name {
	case "find_files":
		var args struct {
			Query string   `json:"query"`
			Paths []string `json:"paths"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		findResult, err := c.scout.FindFiles(ctx, args.Query, args.Paths)
		if err != nil {
			return "", err
		}
		result = formatFindFilesResult(findResult)

	case "summarize_files":
		var args struct {
			Paths []string `json:"paths"`
			Focus string   `json:"focus"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		sumResult, err := c.scout.SummarizeFiles(ctx, args.Paths, args.Focus)
		if err != nil {
			return "", err
		}
		result = formatSummarizeFilesResult(sumResult)

	case "read_files":
		var args struct {
			Paths []string `json:"paths"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		readResult, err := c.scout.ReadFiles(ctx, args.Paths, nil)
		if err != nil {
			return "", err
		}
		result = formatReadFilesResult(readResult)

	default:
		return "", fmt.Errorf("unknown function: %s", name)
	}

	if err == nil && result != "" {
		c.setCachedToolOutput(cacheKey, result)
	}
	return result, err
}

func formatFindFilesResult(r *agent.FindFilesResult) string {
	if len(r.Files) == 0 {
		return "No files found matching the query."
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d files (%s total):\n\n", len(r.Files), formatBytes(r.TotalBytes))
	for _, f := range r.Files {
		sizeStr := formatBytes(f.Size)
		if f.Context != "" {
			fmt.Fprintf(&sb, "- %s (%s): %s\n", f.Path, sizeStr, f.Context)
		} else {
			fmt.Fprintf(&sb, "- %s (%s)\n", f.Path, sizeStr)
		}
	}
	if r.Notes != "" {
		fmt.Fprintf(&sb, "\nNotes: %s\n", r.Notes)
	}
	return sb.String()
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func formatSummarizeFilesResult(r *agent.SummarizeFilesResult) string {
	if len(r.Summaries) == 0 {
		return "No summaries generated."
	}

	var sb strings.Builder
	for _, s := range r.Summaries {
		fmt.Fprintf(&sb, "## %s\n\n%s\n\n", s.Path, s.Summary)
	}
	return sb.String()
}

func formatReadFilesResult(r *agent.ReadFilesResult) string {
	if len(r.Files) == 0 {
		return "No files read."
	}

	var sb strings.Builder
	for _, f := range r.Files {
		fmt.Fprintf(&sb, "## %s\n\n", f.Path)
		if f.Error != "" {
			fmt.Fprintf(&sb, "Error: %s\n\n", f.Error)
		} else {
			sb.WriteString("```\n")
			sb.WriteString(f.Content)
			sb.WriteString("\n```\n\n")
			if f.Truncated {
				sb.WriteString("(file truncated)\n\n")
			}
		}
	}
	return sb.String()
}

func (c *DeepAnalysisClient) getCachedToolOutput(key string) (string, bool) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	val, ok := c.toolCache[key]
	return val, ok
}

func (c *DeepAnalysisClient) setCachedToolOutput(key, val string) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	c.toolCache[key] = val
}

// buildSystemPrompt creates the system prompt for the researcher
func (c *DeepAnalysisClient) buildSystemPrompt() string {
	return `You are an expert deep analysis AI consulted for the most challenging and complex problems.

You will receive a markdown document containing:
- The current task or question
- Context and background information
- Previous analysis and conversation history (if any)

Your role is to provide deep, systematic analysis through multi-step reasoning.

## Available Tools

You have three tools for exploring the codebase. Use them in order: find → summarize → read.

### 1. find_files(query, paths)
Discover files matching natural language intent. Returns file paths with sizes.
- query: Describe what you're looking for
- paths: Optional directories to search within

Example: find_files("error handling code", ["src"])
Returns: List of matching files with sizes (e.g., "src/errors.zig (4.2KB)")

### 2. summarize_files(paths, focus)
Get AI-generated summaries of file contents. CHEAP - use liberally for triage.
- paths: List of file paths to summarize
- focus: Optional focus area (e.g., "public API", "error handling")

Example: summarize_files(["src/engine.zig", "src/player.zig"], "game state management")
Returns: 2-4 sentence summaries of each file focused on the specified area.

**Use this to decide which files need full reads.** Don't skip this step.

### 3. read_files(paths)
Read full file contents. EXPENSIVE - use sparingly.
- paths: List of file paths to read
- **LIMIT: Max 10 files or 200KB per call**

If you exceed the limit, you'll get an error asking you to use summarize_files first.

## Required Workflow

**IMPORTANT: Follow this workflow to avoid errors and control costs.**

1. **find_files** - Discover relevant files. Note the sizes returned.
2. **summarize_files** - Get summaries of found files. Use the focus parameter.
3. **Analyze summaries** - Decide which files actually need full content.
4. **read_files** - Read only the files you truly need (max 10 at a time).
5. **Synthesize** - Draw conclusions based on evidence.

## Example

Task: "How does error handling work in this codebase?"

1. find_files("error handling") → Returns 15 files totaling 180KB
2. summarize_files(all 15 paths, "error handling patterns") → Get quick summaries
3. From summaries, identify 3 files that are central to error handling
4. read_files(those 3 files) → Get full content for detailed analysis
5. Write analysis citing specific code

## Guidelines

- **Always summarize before reading** when you find multiple files
- Check file sizes from find_files - if total > 200KB, you must summarize first
- Use the focus parameter in summarize_files to get relevant summaries
- Make multiple smaller read_files calls rather than one large one
- Cite specific files and line numbers in your analysis

## Output Format

Structure your response with:
- Clear headings and sections
- Code blocks with syntax highlighting
- Bullet points for key findings
- Numbered lists for step-by-step recommendations

You are being consulted because standard approaches have proven insufficient. Bring your full analytical capabilities to bear.`
}

// estimateCost estimates the cost in USD based on model and token usage.
// Pricing checked Jul 2026:
// - gpt-5.6-sol: $5/1M input, $0.50/1M cached input, $6.25/1M cache write, $30/1M output
// - gpt-5.6-terra: $2.50/1M input, $0.25/1M cached input, $3.125/1M cache write, $15/1M output
// - gpt-5.6-luna: $1/1M input, $0.10/1M cached input, $1.25/1M cache write, $6/1M output
// GPT-5.6 requests above 272K input tokens cost 2x input and 1.5x output for the full request.
// - gpt-5.5-pro: $30/1M input, $180/1M output
// - gpt-5.5: $5/1M input, $0.50/1M cached input, $30/1M output
// - gpt-5.4-pro: $30/1M input, $180/1M output
// - gpt-5.4: $2.50/1M input, $0.25/1M cached input, $15/1M output
// - gpt-5.4-mini: $0.75/1M input, $0.075/1M cached input, $4.50/1M output
// - gpt-5-pro: $15/1M input, $120/1M output
// - gpt-5 / gpt-5.1: $1.25/1M input, $0.125/1M cached input, $10/1M output
// - gpt-5-mini: $0.25/1M input, $0.025/1M cached input, $2/1M output
// - gpt-5-nano: $0.05/1M input, $0.005/1M cached input, $0.4/1M output
func estimateCost(model string, inputTokens, cachedInputTokens, cacheWriteInputTokens, outputTokens int64) float64 {
	inputCostPer1M, cachedInputCostPer1M, outputCostPer1M := pricingForModel(model)
	if cachedInputTokens < 0 {
		cachedInputTokens = 0
	}
	if cachedInputTokens > inputTokens {
		cachedInputTokens = inputTokens
	}
	if cacheWriteInputTokens < 0 {
		cacheWriteInputTokens = 0
	}
	if cacheWriteInputTokens > inputTokens-cachedInputTokens {
		cacheWriteInputTokens = inputTokens - cachedInputTokens
	}

	uncachedInputTokens := inputTokens - cachedInputTokens - cacheWriteInputTokens
	cacheWriteCostPer1M := inputCostPer1M
	if isGPT56Model(model) {
		cacheWriteCostPer1M *= 1.25
		if inputTokens > 272_000 {
			inputCostPer1M *= 2
			cachedInputCostPer1M *= 2
			cacheWriteCostPer1M *= 2
			outputCostPer1M *= 1.5
		}
	}
	inputCost := (float64(uncachedInputTokens) / 1_000_000.0) * inputCostPer1M
	cachedInputCost := (float64(cachedInputTokens) / 1_000_000.0) * cachedInputCostPer1M
	cacheWriteInputCost := (float64(cacheWriteInputTokens) / 1_000_000.0) * cacheWriteCostPer1M
	outputCost := (float64(outputTokens) / 1_000_000.0) * outputCostPer1M

	return inputCost + cachedInputCost + cacheWriteInputCost + outputCost
}

func estimateHarnessCallCost(model string, usage harness.Usage) float64 {
	return estimateCost(
		model,
		int64(usage.InputTokens+usage.CachedInputTokens+usage.CacheCreationInputTokens),
		int64(usage.CachedInputTokens),
		int64(usage.CacheCreationInputTokens),
		int64(usage.OutputTokens),
	)
}

func pricingForModel(model string) (inputCostPer1M, cachedInputCostPer1M, outputCostPer1M float64) {
	normalized := strings.ToLower(model)

	switch {
	case matchesModelOrSnapshot(normalized, "claude-fable-5"):
		return 10.0, 1.0, 50.0
	case matchesModelOrSnapshot(normalized, "claude-opus-4-8"):
		return 5.0, 0.5, 25.0
	case matchesModelOrSnapshot(normalized, "claude-sonnet-5"):
		return 3.0, 0.3, 15.0
	case matchesModelOrSnapshot(normalized, "claude-sonnet-4-6"):
		return 3.0, 0.3, 15.0
	case matchesModelOrSnapshot(normalized, "claude-haiku-4-5"):
		return 1.0, 0.1, 5.0
	case matchesModelOrSnapshot(normalized, "gpt-5.6-sol"), matchesModelOrSnapshot(normalized, "gpt-5.6"):
		return 5.0, 0.5, 30.0
	case matchesModelOrSnapshot(normalized, "gpt-5.6-terra"):
		return 2.5, 0.25, 15.0
	case matchesModelOrSnapshot(normalized, "gpt-5.6-luna"):
		return 1.0, 0.1, 6.0
	case matchesModelOrSnapshot(normalized, "gpt-5.5-pro"):
		return 30.0, 30.0, 180.0
	case matchesModelOrSnapshot(normalized, "gpt-5.5"):
		return 5.0, 0.5, 30.0
	case matchesModelOrSnapshot(normalized, "gpt-5.4-pro"):
		return 30.0, 30.0, 180.0
	case matchesModelOrSnapshot(normalized, "gpt-5.4-mini"):
		return 0.75, 0.075, 4.5
	case matchesModelOrSnapshot(normalized, "gpt-5.4"):
		return 2.5, 0.25, 15.0
	case matchesModelOrSnapshot(normalized, "gpt-5.2-pro"):
		return 21.0, 21.0, 168.0
	case matchesModelOrSnapshot(normalized, "gpt-5.2"):
		return 1.75, 1.75, 14.0
	case matchesModelOrSnapshot(normalized, "gpt-5-pro"):
		return 15.0, 15.0, 120.0
	case matchesModelOrSnapshot(normalized, "gpt-5-mini"):
		return 0.25, 0.025, 2.0
	case matchesModelOrSnapshot(normalized, "gpt-5-nano"):
		return 0.05, 0.005, 0.4
	case matchesModelOrSnapshot(normalized, "gpt-5.1"), matchesModelOrSnapshot(normalized, "gpt-5"):
		return 1.25, 0.125, 10.0
	default:
		// Conservative fallback to the highest legacy rate tracked here.
		return 30.0, 30.0, 180.0
	}
}

func matchesModelOrSnapshot(model, base string) bool {
	return model == base || strings.HasPrefix(model, base+"-20")
}
func isGPT56Model(model string) bool {
	normalized := strings.ToLower(model)
	return normalized == "gpt-5.6" || strings.HasPrefix(normalized, "gpt-5.6-")
}
