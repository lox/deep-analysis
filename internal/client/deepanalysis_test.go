package client

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/lox/deep-analysis/internal/agent"
	"github.com/openai/openai-go/responses"
)

func TestEstimateCostSupportsCurrentAndLegacyModels(t *testing.T) {
	testCases := []struct {
		name         string
		model        string
		inputTokens  int64
		outputTokens int64
		want         float64
	}{
		{
			name:         "claude-fable-5",
			model:        "claude-fable-5",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         60.0,
		},
		{
			name:         "claude-opus-4-8",
			model:        "claude-opus-4-8",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         30.0,
		},
		{
			name:         "claude-sonnet-5",
			model:        "claude-sonnet-5",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         18.0,
		},
		{
			name:         "gpt-5.6-sol",
			model:        "gpt-5.6-sol",
			inputTokens:  100_000,
			outputTokens: 100_000,
			want:         3.5,
		},
		{
			name:         "gpt-5.6-sol snapshot",
			model:        "gpt-5.6-sol-2026-07-16",
			inputTokens:  100_000,
			outputTokens: 100_000,
			want:         3.5,
		},
		{
			name:         "gpt-5.6-terra",
			model:        "gpt-5.6-terra",
			inputTokens:  100_000,
			outputTokens: 100_000,
			want:         1.75,
		},
		{
			name:         "gpt-5.6-luna",
			model:        "gpt-5.6-luna",
			inputTokens:  100_000,
			outputTokens: 100_000,
			want:         0.7,
		},
		{
			name:         "gpt-5.5-pro",
			model:        "gpt-5.5-pro",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         210.0,
		},
		{
			name:         "gpt-5.5-pro snapshot",
			model:        "gpt-5.5-pro-2026-06-18",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         210.0,
		},
		{
			name:         "gpt-5.5",
			model:        "gpt-5.5",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         35.0,
		},
		{
			name:         "gpt-5.5 snapshot",
			model:        "gpt-5.5-2026-06-18",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         35.0,
		},
		{
			name:         "gpt-5.4",
			model:        "gpt-5.4",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         17.5,
		},
		{
			name:         "gpt-5.4-mini",
			model:        "gpt-5.4-mini",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         5.25,
		},
		{
			name:         "gpt-5.4-pro",
			model:        "gpt-5.4-pro",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         210.0,
		},
		{
			name:         "gpt-5.4 snapshot",
			model:        "gpt-5.4-2026-03-10",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         17.5,
		},
		{
			name:         "gpt-5.4-pro snapshot",
			model:        "gpt-5.4-pro-2026-03-10",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         210.0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := estimateCost(tc.model, tc.inputTokens, 0, 0, tc.outputTokens)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("estimateCost(%q) = %v, want %v", tc.model, got, tc.want)
			}
		})
	}
}

func TestEstimateCostUsesCachedInputPricingWhenAvailable(t *testing.T) {
	got := estimateCost("gpt-5.5", 1_000_000, 400_000, 0, 1_000_000)

	const want = 33.2 // 600k uncached @ $5 + 400k cached @ $0.50 + 1M output @ $30
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("estimateCost(gpt-5.5) with cached input = %v, want %v", got, want)
	}
}

func TestEstimateCostIncludesGPT56CacheWrites(t *testing.T) {
	got := estimateCost("gpt-5.6-sol", 200_000, 0, 80_000, 100_000)

	const want = 4.1 // 120k uncached @ $5 + 80k cache write @ $6.25 + 100k output @ $30
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("estimateCost(gpt-5.6-sol) with cache writes = %v, want %v", got, want)
	}
}

func TestEstimateCostAppliesGPT56LongContextPricingPerResponse(t *testing.T) {
	longRequestCost := estimateCost("gpt-5.6-sol", 300_000, 100_000, 50_000, 100_000)
	const wantLongRequest = 6.725 // 150k uncached @ $10 + 100k cached @ $1 + 50k write @ $12.50 + 100k output @ $45
	if math.Abs(longRequestCost-wantLongRequest) > 1e-9 {
		t.Fatalf("long GPT-5.6 request cost = %v, want %v", longRequestCost, wantLongRequest)
	}

	var first, second responses.ResponseUsage
	for _, usage := range []*responses.ResponseUsage{&first, &second} {
		usage.InputTokens = 200_000
		usage.OutputTokens = 100_000
	}
	gotSeparateRequests := estimateResponseCost("gpt-5.6-sol", first) + estimateResponseCost("gpt-5.6-sol", second)
	const wantSeparateRequests = 8.0 // Each request stays below the 272K threshold.
	if math.Abs(gotSeparateRequests-wantSeparateRequests) > 1e-9 {
		t.Fatalf("separate GPT-5.6 request cost = %v, want %v", gotSeparateRequests, wantSeparateRequests)
	}
}

func TestCacheWriteTokensReadsNewUsageField(t *testing.T) {
	var usage responses.ResponseUsage
	err := json.Unmarshal([]byte(`{
		"input_tokens": 1234,
		"input_tokens_details": {"cached_tokens": 500, "cache_write_tokens": 321},
		"output_tokens": 100,
		"output_tokens_details": {"reasoning_tokens": 50},
		"total_tokens": 1334
	}`), &usage)
	if err != nil {
		t.Fatalf("unmarshal usage: %v", err)
	}
	if got := cacheWriteTokens(usage); got != 321 {
		t.Fatalf("cacheWriteTokens = %d, want 321", got)
	}
}

func TestGPT56ResearcherRequestsUseProModeAndStablePrefix(t *testing.T) {
	c := New("test-key", nil, "gpt-5.6-sol", "gpt-5.5")
	input := responses.ResponseInputParam{
		responses.ResponseInputItemParamOfMessage("analyze this", responses.EasyInputMessageRoleUser),
	}

	initial := decodeResearcherRequest(t, c.newResponseParams(input, "", AnalysisOptions{ReasoningEffort: "xhigh"}))
	followUp := decodeResearcherRequest(t, c.newResponseParams(input, "resp_123", AnalysisOptions{ReasoningEffort: "xhigh"}))

	if initial.Model != "gpt-5.6-sol" || initial.Reasoning.Mode != "pro" || initial.Reasoning.Effort != "xhigh" {
		t.Fatalf("initial model/reasoning = %q/%q/%q", initial.Model, initial.Reasoning.Mode, initial.Reasoning.Effort)
	}
	if initial.Instructions == "" || initial.PromptCacheKey != "deep-analysis:researcher:gpt-5.6-sol:v4" || len(initial.Tools) != 3 {
		t.Fatalf("initial stable prefix is incomplete: instructions=%t cache_key=%q tools=%d", initial.Instructions != "", initial.PromptCacheKey, len(initial.Tools))
	}
	if followUp.PreviousResponseID != "resp_123" {
		t.Fatalf("follow-up previous_response_id = %q, want resp_123", followUp.PreviousResponseID)
	}
	if followUp.Instructions != initial.Instructions || followUp.PromptCacheKey != initial.PromptCacheKey ||
		followUp.Reasoning != initial.Reasoning || !reflect.DeepEqual(followUp.Tools, initial.Tools) {
		t.Fatal("follow-up request did not preserve the cacheable researcher prefix")
	}
}

func TestLegacyResearcherDoesNotReceiveGPT56ProMode(t *testing.T) {
	c := New("test-key", nil, "gpt-5.5", "gpt-5.5")
	input := responses.ResponseInputParam{
		responses.ResponseInputItemParamOfMessage("analyze this", responses.EasyInputMessageRoleUser),
	}

	request := decodeResearcherRequest(t, c.newResponseParams(input, "", AnalysisOptions{ReasoningEffort: "high"}))
	if request.Reasoning.Mode != "" {
		t.Fatalf("legacy reasoning mode = %q, want empty", request.Reasoning.Mode)
	}
}

type researcherRequestShape struct {
	Model              string            `json:"model"`
	Instructions       string            `json:"instructions"`
	PromptCacheKey     string            `json:"prompt_cache_key"`
	PreviousResponseID string            `json:"previous_response_id"`
	Tools              []json.RawMessage `json:"tools"`
	Reasoning          struct {
		Effort string `json:"effort"`
		Mode   string `json:"mode"`
	} `json:"reasoning"`
}

func decodeResearcherRequest(t *testing.T, params responses.ResponseNewParams) researcherRequestShape {
	t.Helper()

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal response params: %v", err)
	}
	var request researcherRequestShape
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatalf("unmarshal response params: %v", err)
	}
	return request
}

func TestNewUsesConfiguredResearcherAndScoutModels(t *testing.T) {
	c := New("test-key", nil, "gpt-5.4", "gpt-5.4-pro")

	if c.researcherModel != "gpt-5.4" {
		t.Fatalf("researcherModel = %q, want %q", c.researcherModel, "gpt-5.4")
	}
	if c.scoutModel != "gpt-5.4-pro" {
		t.Fatalf("scoutModel = %q, want %q", c.scoutModel, "gpt-5.4-pro")
	}
}

func TestNewUsesDefaultModelsWhenUnset(t *testing.T) {
	c := New("test-key", nil, "", "")

	if c.researcherModel != DefaultResearcherModel {
		t.Fatalf("researcherModel = %q, want %q", c.researcherModel, DefaultResearcherModel)
	}
	if c.scoutModel != agent.DefaultScoutModel {
		t.Fatalf("scoutModel = %q, want %q", c.scoutModel, agent.DefaultScoutModel)
	}
}

func TestDefaultModelsForAnthropic(t *testing.T) {
	researcher, scout, err := DefaultModelsForProvider(AnthropicProvider)
	if err != nil {
		t.Fatalf("DefaultModelsForProvider: %v", err)
	}
	if researcher != DefaultAnthropicResearcherModel || scout != DefaultAnthropicScoutModel {
		t.Fatalf("models = %q, %q", researcher, scout)
	}
}

func TestNewForProvidersMixesResearcherAndScout(t *testing.T) {
	testCases := []struct {
		name               string
		researcherProvider string
		scoutProvider      string
		wantAnalyzer       string
	}{
		{"anthropic researcher with openai scout", AnthropicProvider, OpenAIProvider, "anthropic"},
		{"openai researcher with anthropic scout", OpenAIProvider, AnthropicProvider, "openai"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			analyzer, err := NewForProviders(tc.researcherProvider, "researcher-key", tc.scoutProvider, "scout-key", nil, "", "", "low")
			if err != nil {
				t.Fatalf("NewForProviders: %v", err)
			}

			switch typed := analyzer.(type) {
			case *AnthropicDeepAnalysisClient:
				if tc.wantAnalyzer != "anthropic" || typed.runtime.scout.Provider() != tc.scoutProvider {
					t.Fatalf("analyzer/scout providers = anthropic/%s", typed.runtime.scout.Provider())
				}
			case *DeepAnalysisClient:
				if tc.wantAnalyzer != "openai" || typed.scout.Provider() != tc.scoutProvider {
					t.Fatalf("analyzer/scout providers = openai/%s", typed.scout.Provider())
				}
			default:
				t.Fatalf("unexpected analyzer type %T", analyzer)
			}
		})
	}
}

func TestAnthropicToolsMatchResearcherWorkflow(t *testing.T) {
	tools := buildAnthropicTools()
	if len(tools) != 3 {
		t.Fatalf("len(tools) = %d, want 3", len(tools))
	}
	wantNames := []string{"find_files", "summarize_files", "read_files"}
	for i, want := range wantNames {
		if tools[i].OfTool == nil || tools[i].OfTool.Name != want {
			t.Fatalf("tool %d = %+v, want %s", i, tools[i], want)
		}
	}
}

func TestAnthropicAdaptiveThinkingModels(t *testing.T) {
	for _, model := range []string{"claude-opus-4-8", "claude-opus-4-7", "claude-opus-4-6", "claude-sonnet-5", "claude-sonnet-4-6"} {
		if !requiresExplicitAnthropicAdaptiveThinking(model) {
			t.Fatalf("%s should use adaptive thinking", model)
		}
	}
	if requiresExplicitAnthropicAdaptiveThinking("claude-fable-5") {
		t.Fatal("Fable adaptive thinking is always on and should not send a thinking override")
	}
}
