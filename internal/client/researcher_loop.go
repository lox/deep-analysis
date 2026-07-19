package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/log"
	harness "github.com/lox/agent-harness"
)

func (c *DeepAnalysisClient) analyzeWithHarness(ctx context.Context, document string, opts AnalysisOptions) (AnalysisResult, error) {
	log.Debug("Starting analysis", "bytes", len(document), "provider", c.providerName)

	providerOptions := map[string]any{}
	switch c.providerName {
	case OpenAIProvider:
		providerOptions["prompt_cache_key"] = fmt.Sprintf("deep-analysis:researcher:%s:%s", strings.ToLower(c.researcherModel), promptCacheVersion)
		providerOptions["strict_tools"] = true
	case AnthropicProvider:
		providerOptions["max_tokens"] = maxAnthropicOutputTokens
		providerOptions["prompt_cache"] = true
	}
	reasoning := harness.ReasoningOptions{Effort: opts.ReasoningEffort}
	if c.providerName == OpenAIProvider && isGPT56Model(c.researcherModel) {
		reasoning.Mode = "pro"
	}

	result, err := harness.Run(ctx, c.provider,
		harness.WithSystem(c.buildSystemPrompt()),
		harness.WithMessages(harness.Message{Role: harness.RoleUser, Content: document}),
		harness.WithTools(c.tools...),
		harness.WithModel(c.researcherModel),
		harness.WithMaxSteps(maxIterations+1),
		harness.WithPreviousResponseID(opts.PreviousResponseID),
		harness.WithReasoning(reasoning),
		harness.WithProviderOptions(providerOptions),
	)
	if err != nil {
		log.Error("Researcher API call failed", "provider", c.providerName, "error", err)
		return AnalysisResult{}, fmt.Errorf("%s API error: %w", c.providerErrorName(), err)
	}

	switch result.StopReason {
	case harness.StopMaxSteps:
		log.Error("Max iterations reached", "max", maxIterations)
		return AnalysisResult{}, fmt.Errorf("max function call iterations (%d) reached", maxIterations)
	case harness.StopRefusal:
		return AnalysisResult{}, c.refusalError(result.FinishDetails)
	case harness.StopIncomplete:
		if c.providerName == AnthropicProvider && result.FinishReason == harness.FinishReasonMaxTokens {
			return AnalysisResult{}, fmt.Errorf("anthropic response reached the %d-token output limit before completing", maxAnthropicOutputTokens)
		}
		if result.FinishDetails != "" {
			return AnalysisResult{}, fmt.Errorf("%s response incomplete (%s)", c.providerErrorName(), result.FinishDetails)
		}
		return AnalysisResult{}, fmt.Errorf("%s response incomplete", c.providerErrorName())
	case harness.StopCancelled:
		if err := ctx.Err(); err != nil {
			return AnalysisResult{}, err
		}
		return AnalysisResult{}, fmt.Errorf("analysis cancelled")
	case harness.StopError:
		return AnalysisResult{}, fmt.Errorf("%s analysis failed", c.providerErrorName())
	}

	text := lastAssistantText(result.Messages)
	if text == "" {
		if c.providerName == AnthropicProvider {
			return AnalysisResult{}, fmt.Errorf("no text content in Anthropic response (stop reason: %s)", result.FinishReason)
		}
		return AnalysisResult{}, fmt.Errorf("no text content in response")
	}

	c.logHarnessUsage(result.TotalUsage, result.CallUsage)
	return AnalysisResult{Text: text, ResponseID: result.ResponseID}, nil
}

func (c *DeepAnalysisClient) buildTools() []harness.Tool {
	definitions := researcherToolDefinitions(c.providerName)
	tools := make([]harness.Tool, 0, len(definitions))
	for _, definition := range definitions {
		parameters, err := json.Marshal(definition.parameters())
		if err != nil {
			panic(fmt.Sprintf("marshal researcher tool %q schema: %v", definition.name, err))
		}
		tools = append(tools, harness.Tool{
			ToolDef: harness.ToolDef{
				Name:        definition.name,
				Description: definition.description,
				Parameters:  parameters,
			},
			Execute: func(ctx context.Context, call harness.ToolCall) (*harness.ToolResult, error) {
				arguments := string(call.Arguments)
				log.Info("Executing tool", "tool", call.Name, "args", arguments)
				content, err := c.executeFunction(ctx, call.Name, arguments)
				if err != nil {
					log.Warn("Tool execution error", "tool", call.Name, "error", err)
					return &harness.ToolResult{Content: fmt.Sprintf("Error: %v", err), IsError: true}, nil
				}
				log.Info("Tool execution success", "tool", call.Name, "result_bytes", len(content))
				return &harness.ToolResult{Content: content}, nil
			},
		})
	}
	return tools
}

func (c *DeepAnalysisClient) logHarnessUsage(usage harness.Usage, callUsage []harness.Usage) {
	if c.providerName == AnthropicProvider {
		c.logAnthropicUsage(usage, len(callUsage))
		return
	}

	uncachedInputTokens := int64(usage.InputTokens)
	cachedTokens := int64(usage.CachedInputTokens)
	cacheWriteTokens := int64(usage.CacheCreationInputTokens)
	inputTokens := uncachedInputTokens + cachedTokens + cacheWriteTokens
	outputTokens := int64(usage.OutputTokens)
	researcherCost := estimateHarnessRunCost(c.researcherModel, callUsage)
	scoutUsage := c.scout.Usage()
	scoutCost := estimateCost(c.scoutModel, scoutUsage.InputTokens, 0, 0, scoutUsage.OutputTokens)
	cacheHitRate := 0.0
	if inputTokens > 0 {
		cacheHitRate = float64(cachedTokens) / float64(inputTokens) * 100
	}

	log.Info("Researcher usage",
		"model", c.researcherModel,
		"api_calls", len(callUsage),
		"input_tokens", inputTokens,
		"uncached_input_tokens", uncachedInputTokens,
		"output_tokens", outputTokens,
		"cached_tokens", cachedTokens,
		"cache_write_tokens", cacheWriteTokens,
		"cache_hit_rate", fmt.Sprintf("%.1f%%", cacheHitRate),
		"cost_usd", fmt.Sprintf("$%.4f", researcherCost))
	c.logScoutUsage(scoutUsage.Calls, scoutUsage.InputTokens, scoutUsage.OutputTokens, scoutCost)
	log.Info("Total cost", "usd", fmt.Sprintf("$%.4f", researcherCost+scoutCost))
}

func estimateHarnessRunCost(model string, callUsage []harness.Usage) float64 {
	var total float64
	for _, call := range callUsage {
		total += estimateHarnessCallCost(model, call)
	}
	return total
}

func (c *DeepAnalysisClient) logScoutUsage(apiCalls int, inputTokens, outputTokens int64, cost float64) {
	log.Info("Scout usage",
		"model", c.scoutModel,
		"api_calls", apiCalls,
		"input_tokens", inputTokens,
		"output_tokens", outputTokens,
		"cost_usd", fmt.Sprintf("$%.4f", cost))
}

func (c *DeepAnalysisClient) providerErrorName() string {
	if c.providerName == OpenAIProvider {
		return "OpenAI"
	}
	return c.providerName
}

func (c *DeepAnalysisClient) refusalError(details string) error {
	prefix := c.providerName + " model refused the request"
	if c.providerName == OpenAIProvider {
		prefix = "OpenAI model refused the request"
	}
	if details == "" {
		return fmt.Errorf("%s", prefix)
	}
	return fmt.Errorf("%s (%s)", prefix, details)
}

func lastAssistantText(messages []harness.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == harness.RoleAssistant && messages[i].Content != "" {
			return messages[i].Content
		}
	}
	return ""
}
