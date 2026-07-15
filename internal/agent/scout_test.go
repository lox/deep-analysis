package agent

import "testing"

func TestScoutConstructorsSetDefaultsAndEffort(t *testing.T) {
	scout := NewAnthropicScout("test-key", "", "low", nil)
	if scout.model != DefaultAnthropicScoutModel {
		t.Fatalf("model = %q, want %q", scout.model, DefaultAnthropicScoutModel)
	}
	if generator := scout.generator.(*anthropicStructuredOutputGenerator); generator.effort != "low" {
		t.Fatalf("effort = %q, want low", generator.effort)
	}

	openAI := NewScout("test-key", "", "medium", nil)
	if generator := openAI.generator.(*openAIStructuredOutputGenerator); generator.effort != "medium" {
		t.Fatalf("effort = %q, want medium", generator.effort)
	}
}
