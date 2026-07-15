package client

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/charmbracelet/log"
	"github.com/lox/deep-analysis/internal/agent"
)

const (
	DefaultAnthropicResearcherModel = "claude-fable-5"
	DefaultAnthropicScoutModel      = agent.DefaultAnthropicScoutModel
	maxAnthropicOutputTokens        = 65536
)

// AnthropicDeepAnalysisClient runs the analysis workflow through Anthropic's Messages API.
type AnthropicDeepAnalysisClient struct {
	client          *anthropic.Client
	runtime         *DeepAnalysisClient
	researcherModel string
	scoutModel      string
	tools           []anthropic.ToolUnionParam
}

// NewAnthropic creates an Anthropic-backed analysis client.
func NewAnthropic(apiKey string, fileOps agent.FileOps, researcherModel, scoutModel string) *AnthropicDeepAnalysisClient {
	if researcherModel == "" {
		researcherModel = DefaultAnthropicResearcherModel
	}
	if scoutModel == "" {
		scoutModel = DefaultAnthropicScoutModel
	}
	return newAnthropicWithScout(apiKey, fileOps, researcherModel, scoutModel, agent.NewAnthropicScout(apiKey, scoutModel, fileOps))
}

func newAnthropicWithScout(apiKey string, fileOps agent.FileOps, researcherModel, scoutModel string, scout *agent.Scout) *AnthropicDeepAnalysisClient {
	apiClient := anthropic.NewClient(option.WithAPIKey(apiKey))
	return newAnthropicWithClient(&apiClient, researcherModel, scoutModel, scout)
}

func newAnthropicWithClient(apiClient *anthropic.Client, researcherModel, scoutModel string, scout *agent.Scout) *AnthropicDeepAnalysisClient {
	if researcherModel == "" {
		researcherModel = DefaultAnthropicResearcherModel
	}
	if scoutModel == "" {
		scoutModel = DefaultAnthropicScoutModel
	}

	runtime := &DeepAnalysisClient{
		scout:           scout,
		researcherModel: researcherModel,
		scoutModel:      scoutModel,
		toolCache:       make(map[string]string),
		cacheMu:         sync.Mutex{},
	}

	return &AnthropicDeepAnalysisClient{
		client:          apiClient,
		runtime:         runtime,
		researcherModel: researcherModel,
		scoutModel:      scoutModel,
		tools:           buildAnthropicTools(),
	}
}

// Analyze processes a markdown document with Anthropic's tool-use loop.
func (c *AnthropicDeepAnalysisClient) Analyze(ctx context.Context, document string, opts AnalysisOptions) (AnalysisResult, error) {
	log.Debug("Starting Anthropic analysis", "bytes", len(document))

	params := anthropic.MessageNewParams{
		Model:     c.researcherModel,
		MaxTokens: maxAnthropicOutputTokens,
		System: []anthropic.TextBlockParam{
			{Text: c.runtime.buildSystemPrompt()},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(document)),
		},
		Tools: c.tools,
		OutputConfig: anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffort(opts.ReasoningEffort),
		},
	}
	if requiresExplicitAnthropicAdaptiveThinking(c.researcherModel) {
		params.Thinking = anthropic.ThinkingConfigParamUnion{
			OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{},
		}
	}

	var totalInputTokens int64
	var totalOutputTokens int64
	var totalCachedTokens int64
	var apiCalls int

	message, err := c.newMessage(ctx, params)
	if err != nil {
		return AnalysisResult{}, fmt.Errorf("anthropic API error: %w", err)
	}
	addAnthropicUsage(message, &totalInputTokens, &totalOutputTokens, &totalCachedTokens, &apiCalls)

	for i := 0; i < maxIterations; i++ {
		switch message.StopReason {
		case anthropic.StopReasonRefusal:
			return AnalysisResult{}, anthropicRefusalError(message)
		case anthropic.StopReasonMaxTokens:
			return AnalysisResult{}, fmt.Errorf("anthropic response reached the %d-token output limit before completing", maxAnthropicOutputTokens)
		case anthropic.StopReasonPauseTurn:
			params.Messages = append(params.Messages, message.ToParam())
			message, err = c.newMessage(ctx, params)
			if err != nil {
				return AnalysisResult{}, fmt.Errorf("anthropic API error: %w", err)
			}
			addAnthropicUsage(message, &totalInputTokens, &totalOutputTokens, &totalCachedTokens, &apiCalls)
			continue
		}

		toolCalls := extractAnthropicToolCalls(message)
		log.Debug("Anthropic iteration progress", "iteration", i+1, "tool_calls", len(toolCalls))

		if len(toolCalls) == 0 {
			text := extractAnthropicText(message)
			if text == "" {
				return AnalysisResult{}, fmt.Errorf("no text content in Anthropic response (stop reason: %s)", message.StopReason)
			}

			c.logUsage(apiCalls, totalInputTokens, totalOutputTokens, totalCachedTokens)
			return AnalysisResult{Text: text, ResponseID: message.ID}, nil
		}

		params.Messages = append(params.Messages, message.ToParam())
		toolResults := make([]anthropic.ContentBlockParamUnion, 0, len(toolCalls))
		for _, toolCall := range toolCalls {
			log.Info("Executing tool", "tool", toolCall.Name, "args", toolCall.Arguments)
			result, toolErr := c.runtime.executeFunction(ctx, toolCall.Name, toolCall.Arguments)
			if toolErr != nil {
				log.Warn("Tool execution error", "tool", toolCall.Name, "error", toolErr)
				result = fmt.Sprintf("Error: %v", toolErr)
			} else {
				log.Info("Tool execution success", "tool", toolCall.Name, "result_bytes", len(result))
			}
			toolResults = append(toolResults, anthropic.NewToolResultBlock(toolCall.ID, result, toolErr != nil))
		}
		params.Messages = append(params.Messages, anthropic.NewUserMessage(toolResults...))

		message, err = c.newMessage(ctx, params)
		if err != nil {
			return AnalysisResult{}, fmt.Errorf("anthropic API error: %w", err)
		}
		addAnthropicUsage(message, &totalInputTokens, &totalOutputTokens, &totalCachedTokens, &apiCalls)
	}

	return AnalysisResult{}, fmt.Errorf("max function call iterations (%d) reached", maxIterations)
}

func anthropicRefusalError(message *anthropic.Message) error {
	details := strings.TrimSpace(message.StopDetails.Explanation)
	if category := strings.TrimSpace(string(message.StopDetails.Category)); category != "" {
		if details != "" {
			details = category + ": " + details
		} else {
			details = category
		}
	}
	if details == "" {
		return fmt.Errorf("anthropic model refused the request")
	}
	return fmt.Errorf("anthropic model refused the request (%s)", details)
}

func (c *AnthropicDeepAnalysisClient) newMessage(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {
	stream := c.client.Messages.NewStreaming(ctx, params)
	message := &anthropic.Message{}
	for stream.Next() {
		if err := message.Accumulate(stream.Current()); err != nil {
			return nil, fmt.Errorf("accumulate anthropic response: %w", err)
		}
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}
	return message, nil
}

func requiresExplicitAnthropicAdaptiveThinking(model string) bool {
	return strings.HasPrefix(model, "claude-opus-4-8") ||
		strings.HasPrefix(model, "claude-opus-4-7") ||
		strings.HasPrefix(model, "claude-opus-4-6") ||
		strings.HasPrefix(model, "claude-sonnet-5") ||
		strings.HasPrefix(model, "claude-sonnet-4-6")
}

func (c *AnthropicDeepAnalysisClient) logUsage(apiCalls int, inputTokens, outputTokens, cachedTokens int64) {
	scoutUsage := c.runtime.scout.Usage()
	researcherCost := estimateCost(c.researcherModel, inputTokens, cachedTokens, outputTokens)
	scoutCost := estimateCost(c.scoutModel, scoutUsage.InputTokens, 0, scoutUsage.OutputTokens)

	log.Info("Researcher usage",
		"model", c.researcherModel,
		"api_calls", apiCalls,
		"input_tokens", inputTokens,
		"output_tokens", outputTokens,
		"cached_tokens", cachedTokens,
		"cost_usd", fmt.Sprintf("$%.4f", researcherCost))
	log.Info("Scout usage",
		"model", c.scoutModel,
		"api_calls", scoutUsage.Calls,
		"input_tokens", scoutUsage.InputTokens,
		"output_tokens", scoutUsage.OutputTokens,
		"cost_usd", fmt.Sprintf("$%.4f", scoutCost))
	log.Info("Total cost", "usd", fmt.Sprintf("$%.4f", researcherCost+scoutCost))
}

func addAnthropicUsage(message *anthropic.Message, inputTokens, outputTokens, cachedTokens *int64, apiCalls *int) {
	*inputTokens += message.Usage.InputTokens + message.Usage.CacheCreationInputTokens + message.Usage.CacheReadInputTokens
	*outputTokens += message.Usage.OutputTokens
	*cachedTokens += message.Usage.CacheReadInputTokens
	*apiCalls++
}

func extractAnthropicToolCalls(message *anthropic.Message) []ToolCall {
	var calls []ToolCall
	for _, block := range message.Content {
		if block.Type == "tool_use" {
			calls = append(calls, ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: string(block.Input),
			})
		}
	}
	return calls
}

func extractAnthropicText(message *anthropic.Message) string {
	var parts []string
	for _, block := range message.Content {
		if block.Type == "text" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func buildAnthropicTools() []anthropic.ToolUnionParam {
	definitions := []struct {
		name        string
		description string
		properties  map[string]any
		required    []string
	}{
		{
			name:        "find_files",
			description: "Discover files matching a natural-language query.",
			properties: map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Natural language description of what files to find.",
					"minLength":   1,
				},
				"paths": map[string]any{
					"type":        "array",
					"description": "Directories to search within. Use an empty array for the entire project.",
					"items":       map[string]any{"type": "string"},
				},
			},
			required: []string{"query", "paths"},
		},
		{
			name:        "summarize_files",
			description: "Generate concise summaries of source files before reading them in full.",
			properties: map[string]any{
				"paths": map[string]any{
					"type":     "array",
					"items":    map[string]any{"type": "string"},
					"minItems": 1,
				},
				"focus": map[string]any{
					"type":        "string",
					"description": "What the summaries should focus on. Use an empty string for a general summary.",
				},
			},
			required: []string{"paths", "focus"},
		},
		{
			name:        "read_files",
			description: "Read selected files in full after they have been triaged.",
			properties: map[string]any{
				"paths": map[string]any{
					"type":     "array",
					"items":    map[string]any{"type": "string"},
					"minItems": 1,
				},
			},
			required: []string{"paths"},
		},
	}

	tools := make([]anthropic.ToolUnionParam, 0, len(definitions))
	for _, definition := range definitions {
		tool := anthropic.ToolUnionParamOfTool(anthropic.ToolInputSchemaParam{
			Properties: definition.properties,
			Required:   definition.required,
			ExtraFields: map[string]any{
				"additionalProperties": false,
			},
		}, definition.name)
		tool.OfTool.Description = anthropic.String(definition.description)
		tool.OfTool.Strict = anthropic.Bool(true)
		tools = append(tools, tool)
	}
	return tools
}
