package agent

import "testing"

func TestNewAnthropicScoutUsesDefaultModel(t *testing.T) {
	scout := NewAnthropicScout("test-key", "", nil)
	if scout.model != DefaultAnthropicScoutModel {
		t.Fatalf("model = %q, want %q", scout.model, DefaultAnthropicScoutModel)
	}
}
