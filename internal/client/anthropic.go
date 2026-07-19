package client

import (
	"context"
	"fmt"

	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/charmbracelet/log"
	harness "github.com/lox/agent-harness"
	harnessanthropic "github.com/lox/agent-harness/provider/anthropic"
	"github.com/lox/deep-analysis/internal/agent"
)

const (
	DefaultAnthropicResearcherModel = "claude-fable-5"
	DefaultAnthropicScoutModel      = agent.DefaultAnthropicScoutModel
	maxAnthropicOutputTokens        = 65536
)

// AnthropicDeepAnalysisClient runs the analysis workflow through Anthropic's Messages API.
type AnthropicDeepAnalysisClient struct {
	runtime *DeepAnalysisClient
}

type anthropicUsageTotals struct {
	inputTokens                int64
	cacheCreation5mInputTokens int64
	cacheCreation1hInputTokens int64
	cacheReadInputTokens       int64
	outputTokens               int64
	apiCalls                   int
}

func (u anthropicUsageTotals) cacheCreationInputTokens() int64 {
	return u.cacheCreation5mInputTokens + u.cacheCreation1hInputTokens
}

func (u anthropicUsageTotals) totalInputTokens() int64 {
	return u.inputTokens + u.cacheCreationInputTokens() + u.cacheReadInputTokens
}

// NewAnthropic creates an Anthropic-backed analysis client.
func NewAnthropic(apiKey string, fileOps agent.FileOps, researcherModel, scoutModel string) *AnthropicDeepAnalysisClient {
	if researcherModel == "" {
		researcherModel = DefaultAnthropicResearcherModel
	}
	if scoutModel == "" {
		scoutModel = DefaultAnthropicScoutModel
	}
	return newAnthropicWithScout(apiKey, fileOps, researcherModel, scoutModel, agent.NewAnthropicScout(apiKey, scoutModel, "", fileOps))
}

func newAnthropicWithScout(apiKey string, fileOps agent.FileOps, researcherModel, scoutModel string, scout *agent.Scout) *AnthropicDeepAnalysisClient {
	provider := harnessanthropic.New(
		harnessanthropic.WithAPIKey(apiKey),
		harnessanthropic.WithDefaultModel(researcherModel),
		harnessanthropic.WithRequestOption(anthropicoption.WithJSONSet("tools.0.strict", true)),
		harnessanthropic.WithRequestOption(anthropicoption.WithJSONSet("tools.1.strict", true)),
		harnessanthropic.WithRequestOption(anthropicoption.WithJSONSet("tools.2.strict", true)),
	)
	return newAnthropicWithProvider(provider, researcherModel, scoutModel, scout)
}

func newAnthropicWithProvider(provider harness.Provider, researcherModel, scoutModel string, scout *agent.Scout) *AnthropicDeepAnalysisClient {
	if researcherModel == "" {
		researcherModel = DefaultAnthropicResearcherModel
	}
	if scoutModel == "" {
		scoutModel = DefaultAnthropicScoutModel
	}

	runtime := &DeepAnalysisClient{
		provider:        provider,
		providerName:    AnthropicProvider,
		scout:           scout,
		researcherModel: researcherModel,
		scoutModel:      scoutModel,
		toolCache:       make(map[string]string),
	}
	runtime.tools = runtime.buildTools()

	return &AnthropicDeepAnalysisClient{runtime: runtime}
}

// Analyze processes a markdown document through the shared agent-harness loop.
func (c *AnthropicDeepAnalysisClient) Analyze(ctx context.Context, document string, opts AnalysisOptions) (AnalysisResult, error) {
	return c.runtime.analyzeWithHarness(ctx, document, opts)
}

func (c *DeepAnalysisClient) logAnthropicUsage(harnessUsage harness.Usage, apiCalls int) {
	usage := anthropicUsageFromHarness(harnessUsage, apiCalls)
	scoutUsage := c.scout.Usage()
	researcherCost := estimateAnthropicCost(c.researcherModel, usage)
	scoutCost := estimateCost(c.scoutModel, scoutUsage.InputTokens, 0, 0, scoutUsage.OutputTokens)
	cacheHitRate := 0.0
	if totalInputTokens := usage.totalInputTokens(); totalInputTokens > 0 {
		cacheHitRate = float64(usage.cacheReadInputTokens) / float64(totalInputTokens) * 100
	}

	log.Info("Researcher usage",
		"model", c.researcherModel,
		"api_calls", usage.apiCalls,
		"input_tokens", usage.totalInputTokens(),
		"uncached_input_tokens", usage.inputTokens,
		"cache_creation_input_tokens", usage.cacheCreationInputTokens(),
		"cache_creation_5m_input_tokens", usage.cacheCreation5mInputTokens,
		"cache_creation_1h_input_tokens", usage.cacheCreation1hInputTokens,
		"cached_tokens", usage.cacheReadInputTokens,
		"cache_read_input_tokens", usage.cacheReadInputTokens,
		"cache_hit_rate", fmt.Sprintf("%.1f%%", cacheHitRate),
		"output_tokens", usage.outputTokens,
		"cost_usd", fmt.Sprintf("$%.4f", researcherCost))
	c.logScoutUsage(scoutUsage.Calls, scoutUsage.InputTokens, scoutUsage.OutputTokens, scoutCost)
	log.Info("Total cost", "usd", fmt.Sprintf("$%.4f", researcherCost+scoutCost))
}

func anthropicUsageFromHarness(harnessUsage harness.Usage, apiCalls int) anthropicUsageTotals {
	cacheCreation5m := int64(harnessUsage.CacheCreation5mInputTokens)
	cacheCreation1h := int64(harnessUsage.CacheCreation1hInputTokens)
	if cacheCreation5m+cacheCreation1h == 0 {
		cacheCreation5m = int64(harnessUsage.CacheCreationInputTokens)
	}
	return anthropicUsageTotals{
		inputTokens:                int64(harnessUsage.InputTokens),
		cacheCreation5mInputTokens: cacheCreation5m,
		cacheCreation1hInputTokens: cacheCreation1h,
		cacheReadInputTokens:       int64(harnessUsage.CacheReadInputTokens),
		outputTokens:               int64(harnessUsage.OutputTokens),
		apiCalls:                   apiCalls,
	}
}

func estimateAnthropicCost(model string, usage anthropicUsageTotals) float64 {
	inputCostPer1M, _, outputCostPer1M := pricingForModel(model)

	inputCost := float64(usage.inputTokens) / 1_000_000.0 * inputCostPer1M
	cacheCreation5mCost := float64(usage.cacheCreation5mInputTokens) / 1_000_000.0 * inputCostPer1M * 1.25
	cacheCreation1hCost := float64(usage.cacheCreation1hInputTokens) / 1_000_000.0 * inputCostPer1M * 2.0
	cacheReadCost := float64(usage.cacheReadInputTokens) / 1_000_000.0 * inputCostPer1M * 0.1
	outputCost := float64(usage.outputTokens) / 1_000_000.0 * outputCostPer1M

	return inputCost + cacheCreation5mCost + cacheCreation1hCost + cacheReadCost + outputCost
}
