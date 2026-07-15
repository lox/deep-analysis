package client

import (
	"testing"

	"github.com/lox/deep-analysis/internal/agent"
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
			got := estimateCost(tc.model, tc.inputTokens, 0, tc.outputTokens)
			if got != tc.want {
				t.Fatalf("estimateCost(%q) = %v, want %v", tc.model, got, tc.want)
			}
		})
	}
}

func TestEstimateCostUsesCachedInputPricingWhenAvailable(t *testing.T) {
	got := estimateCost("gpt-5.5", 1_000_000, 400_000, 1_000_000)

	const want = 33.2 // 600k uncached @ $5 + 400k cached @ $0.50 + 1M output @ $30
	if got != want {
		t.Fatalf("estimateCost(gpt-5.5) with cached input = %v, want %v", got, want)
	}
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
