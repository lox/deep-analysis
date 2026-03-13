package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/log"
	"github.com/lox/deep-analysis/internal/agent"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/responses"
)

const (
	DefaultResearcherModel = "gpt-5.4-pro"
	maxIterations          = 50
)

// DeepAnalysisClient handles communication with OpenAI's Responses API
type DeepAnalysisClient struct {
	apiKey          string
	client          *openai.Client
	fileOps         agent.FileOps
	scout           *agent.Scout
	researcherModel string
	scoutModel      string
	tools           []responses.ToolUnionParam
	toolCache       map[string]string
	cacheMu         sync.Mutex
}

// AnalysisOptions controls request behavior.
type AnalysisOptions struct {
	PreviousResponseID string
	ReasoningEffort    string // Reasoning effort: low, medium, high, xhigh (default: xhigh)
}

// AnalysisResult contains the final model output and metadata.
type AnalysisResult struct {
	Text       string
	ResponseID string
}

// New creates a new DeepAnalysisClient instance.
func New(apiKey string, fileOps agent.FileOps, researcherModel, scoutModel string) *DeepAnalysisClient {
	client := openai.NewClient(
		option.WithAPIKey(apiKey),
	)

	if researcherModel == "" {
		researcherModel = DefaultResearcherModel
	}
	if scoutModel == "" {
		scoutModel = agent.DefaultScoutModel
	}

	c := &DeepAnalysisClient{
		apiKey:          apiKey,
		client:          &client,
		fileOps:         fileOps,
		scout:           agent.NewScout(apiKey, scoutModel, fileOps),
		researcherModel: researcherModel,
		scoutModel:      scoutModel,
		toolCache:       make(map[string]string),
	}
	c.tools = c.buildTools()

	return c
}

// Analyze processes a markdown document and returns the analysis result
func (c *DeepAnalysisClient) Analyze(ctx context.Context, document string, opts AnalysisOptions) (AnalysisResult, error) {
	log.Debug("Starting analysis", "bytes", len(document))

	// Track total usage across all API calls
	var totalInputTokens int64
	var totalOutputTokens int64
	var totalCachedTokens int64
	var apiCalls int

	cacheKey := "deep-analysis-v3"

	params := responses.ResponseNewParams{
		Model:          c.researcherModel,
		Instructions:   openai.Opt(c.buildSystemPrompt()),
		Tools:          c.tools,
		PromptCacheKey: openai.Opt(cacheKey),
		Reasoning:      buildReasoningParam(opts.ReasoningEffort),
	}

	inputItems := responses.ResponseInputParam{
		responses.ResponseInputItemParamOfMessage(document, responses.EasyInputMessageRoleUser),
	}
	params.Input = responses.ResponseNewParamsInputUnion{
		OfInputItemList: inputItems,
	}
	if opts.PreviousResponseID != "" {
		params.PreviousResponseID = openai.Opt(opts.PreviousResponseID)
	}

	log.Debug("Calling OpenAI Responses API", "model", c.researcherModel, "previous_response_id", opts.PreviousResponseID)
	response, err := c.client.Responses.New(ctx, params)
	if err != nil {
		log.Error("OpenAI API call failed", "error", err)
		return AnalysisResult{}, fmt.Errorf("OpenAI API error: %w", err)
	}

	apiCalls++
	totalInputTokens += response.Usage.InputTokens
	totalOutputTokens += response.Usage.OutputTokens
	totalCachedTokens += response.Usage.InputTokensDetails.CachedTokens

	log.Debug("Received response", "id", response.ID, "status", response.Status,
		"input_tokens", response.Usage.InputTokens,
		"output_tokens", response.Usage.OutputTokens,
		"cached_tokens", response.Usage.InputTokensDetails.CachedTokens)

	// Handle tool calls in a loop
	for i := 0; i < maxIterations; i++ {
		toolCalls := extractToolCalls(response)
		log.Debug("Iteration progress", "iteration", i+1, "tool_calls", len(toolCalls))

		if len(toolCalls) == 0 {
			text := extractTextContent(response)
			log.Debug("Analysis complete", "response_length", len(text))
			if text == "" {
				log.Error("No text content in response")
				return AnalysisResult{}, fmt.Errorf("no text content in response")
			}

			// Get scout usage
			scoutUsage := c.scout.Usage()

			// Calculate costs
			researcherCost := estimateCost(c.researcherModel, totalInputTokens, totalCachedTokens, totalOutputTokens)
			scoutCost := estimateCost(c.scoutModel, scoutUsage.InputTokens, 0, scoutUsage.OutputTokens)
			totalCost := researcherCost + scoutCost

			cacheHitRate := 0.0
			if totalInputTokens > 0 {
				cacheHitRate = (float64(totalCachedTokens) / float64(totalInputTokens)) * 100
			}

			log.Info("Researcher usage",
				"model", c.researcherModel,
				"api_calls", apiCalls,
				"input_tokens", totalInputTokens,
				"output_tokens", totalOutputTokens,
				"cached_tokens", totalCachedTokens,
				"cache_hit_rate", fmt.Sprintf("%.1f%%", cacheHitRate),
				"cost_usd", fmt.Sprintf("$%.4f", researcherCost))

			log.Info("Scout usage",
				"model", c.scoutModel,
				"api_calls", scoutUsage.Calls,
				"input_tokens", scoutUsage.InputTokens,
				"output_tokens", scoutUsage.OutputTokens,
				"cost_usd", fmt.Sprintf("$%.4f", scoutCost))

			log.Info("Total cost", "usd", fmt.Sprintf("$%.4f", totalCost))

			return AnalysisResult{
				Text:       text,
				ResponseID: response.ID,
			}, nil
		}

		// Execute tool calls
		toolOutputs := make(responses.ResponseInputParam, 0, len(toolCalls))
		for _, toolCall := range toolCalls {
			log.Info("Executing tool", "tool", toolCall.Name, "args", toolCall.Arguments)
			result, err := c.executeFunction(ctx, toolCall.Name, toolCall.Arguments)
			if err != nil {
				log.Warn("Tool execution error", "tool", toolCall.Name, "error", err)
				result = fmt.Sprintf("Error: %v", err)
			} else {
				log.Info("Tool execution success", "tool", toolCall.Name, "result_bytes", len(result))
			}

			toolOutputs = append(toolOutputs, responses.ResponseInputItemParamOfFunctionCallOutput(toolCall.ID, result))
		}

		log.Debug("Continuing with tool outputs", "count", len(toolOutputs))
		params := responses.ResponseNewParams{
			Model:              c.researcherModel,
			PreviousResponseID: openai.Opt(response.ID),
			Input: responses.ResponseNewParamsInputUnion{
				OfInputItemList: toolOutputs,
			},
			Tools: c.tools,
		}

		response, err = c.client.Responses.New(ctx, params)
		if err != nil {
			log.Error("Follow-up API call failed", "error", err)
			return AnalysisResult{}, fmt.Errorf("OpenAI API error: %w", err)
		}

		apiCalls++
		totalInputTokens += response.Usage.InputTokens
		totalOutputTokens += response.Usage.OutputTokens
		totalCachedTokens += response.Usage.InputTokensDetails.CachedTokens

		log.Debug("Received follow-up response", "id", response.ID, "status", response.Status,
			"input_tokens", response.Usage.InputTokens,
			"output_tokens", response.Usage.OutputTokens,
			"cached_tokens", response.Usage.InputTokensDetails.CachedTokens)
	}

	log.Error("Max iterations reached", "max", maxIterations)
	return AnalysisResult{}, fmt.Errorf("max function call iterations (%d) reached", maxIterations)
}

// buildTools defines the three high-level tools for the researcher
func (c *DeepAnalysisClient) buildTools() []responses.ToolUnionParam {
	return []responses.ToolUnionParam{
		responses.ToolParamOfFunction(
			"find_files",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Natural language description of what files to find. Examples: 'CFR trainer tests', 'all zig files', 'error handling code', 'main entry point', 'configuration files'",
						"minLength":   1,
					},
					"paths": map[string]any{
						"type":        "array",
						"description": "Optional directories to search within. Defaults to entire project.",
						"items": map[string]any{
							"type": "string",
						},
						"default": []string{},
					},
				},
				"required":             []string{"query", "paths"},
				"additionalProperties": false,
			},
			true,
		),
		responses.ToolParamOfFunction(
			"summarize_files",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"paths": map[string]any{
						"type":        "array",
						"description": "List of file paths to summarize",
						"items": map[string]any{
							"type": "string",
						},
						"minItems": 1,
					},
					"focus": map[string]any{
						"type":        "string",
						"description": "Optional focus for the summaries. Examples: 'error handling patterns', 'public API', 'test coverage', 'dependencies'",
						"default":     "",
					},
				},
				"required":             []string{"paths", "focus"},
				"additionalProperties": false,
			},
			true,
		),
		responses.ToolParamOfFunction(
			"read_files",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"paths": map[string]any{
						"type":        "array",
						"description": "List of file paths to read in full",
						"items": map[string]any{
							"type": "string",
						},
						"minItems": 1,
					},
				},
				"required":             []string{"paths"},
				"additionalProperties": false,
			},
			true,
		),
	}
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

// ToolCall represents a function tool call
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// extractToolCalls extracts tool calls from a response
func extractToolCalls(response *responses.Response) []ToolCall {
	var toolCalls []ToolCall

	log.Debug("Extracting tool calls", "output_items", len(response.Output))
	for i, item := range response.Output {
		log.Debug("Processing output item", "index", i, "type", item.Type)
		if item.Type == "function_call" {
			toolCalls = append(toolCalls, ToolCall{
				ID:        item.CallID,
				Name:      item.Name,
				Arguments: item.Arguments,
			})
			log.Debug("Found function call", "name", item.Name, "id", item.CallID)
		}
	}

	return toolCalls
}

// extractTextContent extracts text content from a response
func extractTextContent(response *responses.Response) string {
	var textParts []string

	log.Debug("Extracting text content", "output_items", len(response.Output))
	for i, item := range response.Output {
		log.Debug("Processing output item", "index", i, "type", item.Type, "content_items", len(item.Content))
		if item.Type == "message" {
			for j, contentItem := range item.Content {
				log.Debug("Processing content item", "index", j, "type", contentItem.Type)
				if contentItem.Type == "text" || contentItem.Type == "output_text" {
					textParts = append(textParts, contentItem.Text)
					log.Debug("Found text content", "length", len(contentItem.Text))
				}
			}
		}
	}

	result := ""
	for _, part := range textParts {
		if result != "" {
			result += "\n"
		}
		result += part
	}

	log.Debug("Extracted text", "parts", len(textParts), "total_length", len(result))
	return result
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
// Pricing as of Mar 2026:
// - gpt-5.4-pro: $30/1M input, $180/1M output
// - gpt-5.4: $2.50/1M input, $0.25/1M cached input, $15/1M output
// - gpt-5-pro: $15/1M input, $120/1M output
// - gpt-5 / gpt-5.1: $1.25/1M input, $0.125/1M cached input, $10/1M output
// - gpt-5-mini: $0.25/1M input, $0.025/1M cached input, $2/1M output
// - gpt-5-nano: $0.05/1M input, $0.005/1M cached input, $0.4/1M output
func estimateCost(model string, inputTokens, cachedInputTokens, outputTokens int64) float64 {
	inputCostPer1M, cachedInputCostPer1M, outputCostPer1M := pricingForModel(model)
	if cachedInputTokens < 0 {
		cachedInputTokens = 0
	}
	if cachedInputTokens > inputTokens {
		cachedInputTokens = inputTokens
	}

	uncachedInputTokens := inputTokens - cachedInputTokens
	inputCost := (float64(uncachedInputTokens) / 1_000_000.0) * inputCostPer1M
	cachedInputCost := (float64(cachedInputTokens) / 1_000_000.0) * cachedInputCostPer1M
	outputCost := (float64(outputTokens) / 1_000_000.0) * outputCostPer1M

	return inputCost + cachedInputCost + outputCost
}

func pricingForModel(model string) (inputCostPer1M, cachedInputCostPer1M, outputCostPer1M float64) {
	normalized := strings.ToLower(model)

	switch {
	case strings.HasPrefix(normalized, "gpt-5.4-pro"):
		return 30.0, 30.0, 180.0
	case strings.HasPrefix(normalized, "gpt-5.4"):
		return 2.5, 0.25, 15.0
	case strings.HasPrefix(normalized, "gpt-5.2-pro"):
		return 21.0, 21.0, 168.0
	case strings.HasPrefix(normalized, "gpt-5.2"):
		return 1.75, 1.75, 14.0
	case strings.HasPrefix(normalized, "gpt-5-pro"):
		return 15.0, 15.0, 120.0
	case strings.HasPrefix(normalized, "gpt-5-mini"):
		return 0.25, 0.025, 2.0
	case strings.HasPrefix(normalized, "gpt-5-nano"):
		return 0.05, 0.005, 0.4
	case strings.HasPrefix(normalized, "gpt-5.1"), strings.HasPrefix(normalized, "gpt-5"):
		return 1.25, 0.125, 10.0
	default:
		// Conservative fallback to the most expensive currently supported model.
		return 30.0, 30.0, 180.0
	}
}

// buildReasoningParam creates a ReasoningParam from a string effort level.
// Supports: low, medium, high, xhigh. Defaults to high if unrecognized.
func buildReasoningParam(effort string) responses.ReasoningParam {
	var e responses.ReasoningEffort
	switch effort {
	case "low":
		e = responses.ReasoningEffortLow
	case "medium":
		e = responses.ReasoningEffortMedium
	case "high":
		e = responses.ReasoningEffortHigh
	case "xhigh":
		// SDK doesn't have xhigh constant yet, but API accepts it
		e = responses.ReasoningEffort("xhigh")
	default:
		e = responses.ReasoningEffortHigh
	}
	return responses.ReasoningParam{Effort: e}
}
