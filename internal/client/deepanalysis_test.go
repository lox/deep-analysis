package client

import (
	"testing"

	"github.com/lox/deep-analysis/internal/agent"
)

func TestEstimateCostSupportsGPT54Models(t *testing.T) {
	testCases := []struct {
		name         string
		model        string
		inputTokens  int64
		outputTokens int64
		want         float64
	}{
		{
			name:         "gpt-5.4",
			model:        "gpt-5.4",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			want:         17.5,
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
	got := estimateCost("gpt-5.4", 1_000_000, 400_000, 1_000_000)

	const want = 16.6 // 600k uncached @ $2.50 + 400k cached @ $0.25 + 1M output @ $15
	if got != want {
		t.Fatalf("estimateCost(gpt-5.4) with cached input = %v, want %v", got, want)
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
