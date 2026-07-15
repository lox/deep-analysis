package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/lox/deep-analysis/internal/client"
)

func TestOpenAIAPIKeyPrefersEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "env-key")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	got, err := openAIAPIKey()
	if err != nil {
		t.Fatalf("openAIAPIKey: %v", err)
	}
	if got != "env-key" {
		t.Fatalf("openAIAPIKey = %q, want env-key", got)
	}
}

func TestOpenAIAPIKeyReadsXDGConfig(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	path := filepath.Join(configDir, "deep-analysis", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("openai_api_key: file-key\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := openAIAPIKey()
	if err != nil {
		t.Fatalf("openAIAPIKey: %v", err)
	}
	if got != "file-key" {
		t.Fatalf("openAIAPIKey = %q, want file-key", got)
	}
}

func TestOpenAIAPIKeyReadsSharedOpenAIConfig(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	path := filepath.Join(configDir, "openai", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("api_key: shared-key\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := openAIAPIKey()
	if err != nil {
		t.Fatalf("openAIAPIKey: %v", err)
	}
	if got != "shared-key" {
		t.Fatalf("openAIAPIKey = %q, want shared-key", got)
	}
}

func TestOpenAIAPIKeyReadsLegacyAppConfig(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	path := filepath.Join(configDir, "deep-analysis", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("api_key: legacy-key\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := openAIAPIKey()
	if err != nil {
		t.Fatalf("openAIAPIKey: %v", err)
	}
	if got != "legacy-key" {
		t.Fatalf("openAIAPIKey = %q, want legacy-key", got)
	}
}

func TestAnthropicAPIKeyPrefersEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-env-key")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	got, err := providerAPIKey(client.AnthropicProvider)
	if err != nil {
		t.Fatalf("providerAPIKey: %v", err)
	}
	if got != "anthropic-env-key" {
		t.Fatalf("providerAPIKey = %q, want anthropic-env-key", got)
	}
}

func TestAnthropicAPIKeyReadsXDGConfig(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	path := filepath.Join(configDir, "deep-analysis", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("anthropic_api_key: file-key\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := providerAPIKey(client.AnthropicProvider)
	if err != nil {
		t.Fatalf("providerAPIKey: %v", err)
	}
	if got != "file-key" {
		t.Fatalf("providerAPIKey = %q, want file-key", got)
	}
}

func TestSaveOpenAIConfigWritesYAML(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	path, err := saveOpenAIConfig(" saved-key\n")
	if err != nil {
		t.Fatalf("saveOpenAIConfig: %v", err)
	}

	expectedPath := filepath.Join(configDir, "deep-analysis", "config.yaml")
	if path != expectedPath {
		t.Fatalf("saveOpenAIConfig path = %q, want %q", path, expectedPath)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o, want 600", info.Mode().Perm())
	}

	got, err := openAIAPIKey()
	if err != nil {
		t.Fatalf("openAIAPIKey: %v", err)
	}
	if got != "saved-key" {
		t.Fatalf("openAIAPIKey = %q, want saved-key", got)
	}
}

func TestSaveProviderConfigPreservesOtherProvider(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	configPath := filepath.Join(configDir, "deep-analysis", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("researcher: anthropic.claude-opus-4-8@xhigh\nscout: openai.gpt-5.6-terra@low\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := saveProviderConfig(client.OpenAIProvider, "openai-key"); err != nil {
		t.Fatalf("save openai config: %v", err)
	}
	if _, err := saveProviderConfig(client.AnthropicProvider, "anthropic-key"); err != nil {
		t.Fatalf("save anthropic config: %v", err)
	}

	openAIKey, err := providerAPIKey(client.OpenAIProvider)
	if err != nil {
		t.Fatalf("read openai key: %v", err)
	}
	anthropicKey, err := providerAPIKey(client.AnthropicProvider)
	if err != nil {
		t.Fatalf("read anthropic key: %v", err)
	}
	if openAIKey != "openai-key" || anthropicKey != "anthropic-key" {
		t.Fatalf("keys = %q, %q", openAIKey, anthropicKey)
	}
	config, _, err := loadAppConfig()
	if err != nil {
		t.Fatalf("load app config: %v", err)
	}
	if config.Researcher != "anthropic.claude-opus-4-8@xhigh" || config.Scout != "openai.gpt-5.6-terra@low" {
		t.Fatalf("setup changed model defaults: %+v", config)
	}
}

func TestCLIParsesSetupAndDefaultAnalyze(t *testing.T) {
	var setupCLI CLI
	setupParser, err := kong.New(&setupCLI)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}

	if _, err := setupParser.Parse([]string{"setup"}); err != nil {
		t.Fatalf("Parse setup: %v", err)
	}
	if setupCLI.Setup.Provider != client.OpenAIProvider {
		t.Fatalf("setup provider = %q", setupCLI.Setup.Provider)
	}

	var doctorCLI CLI
	doctorParser, err := kong.New(&doctorCLI)
	if err != nil {
		t.Fatalf("kong.New doctor: %v", err)
	}
	if _, err := doctorParser.Parse([]string{"doctor"}); err != nil {
		t.Fatalf("Parse doctor: %v", err)
	}

	var analyzeCLI CLI
	analyzeParser, err := kong.New(&analyzeCLI)
	if err != nil {
		t.Fatalf("kong.New analyze: %v", err)
	}
	if _, err := analyzeParser.Parse([]string{"--cwd", "/tmp", "notes.md", "--output", "annotated.md"}); err != nil {
		t.Fatalf("Parse default analyze: %v", err)
	}
	if analyzeCLI.Analyze.Input != "notes.md" || analyzeCLI.Analyze.Output != "annotated.md" || analyzeCLI.Analyze.Cwd != "/tmp" {
		t.Fatalf("parsed analyze = %+v", analyzeCLI.Analyze)
	}
	defaultResearcher, defaultScout, err := analyzeCLI.Analyze.modelSelections(appConfig{})
	if err != nil {
		t.Fatalf("default model selections: %v", err)
	}
	if defaultResearcher != (modelSelection{Provider: client.OpenAIProvider, Model: client.DefaultResearcherModel, Effort: "xhigh"}) ||
		defaultScout != (modelSelection{Provider: client.OpenAIProvider, Model: "gpt-5.5", Effort: "low"}) {
		t.Fatalf("default selections = %+v, %+v", defaultResearcher, defaultScout)
	}

	var anthropicCLI CLI
	anthropicParser, err := kong.New(&anthropicCLI)
	if err != nil {
		t.Fatalf("kong.New anthropic: %v", err)
	}
	if _, err := anthropicParser.Parse([]string{"notes.md", "--researcher", "anthropic.claude-opus-4-8"}); err != nil {
		t.Fatalf("Parse anthropic analyze: %v", err)
	}
	researcher, scout, err := anthropicCLI.Analyze.modelSelections(appConfig{})
	if err != nil {
		t.Fatalf("anthropic model selections: %v", err)
	}
	if researcher != (modelSelection{Provider: client.AnthropicProvider, Model: "claude-opus-4-8"}) || scout.Provider != client.OpenAIProvider {
		t.Fatalf("anthropic selections = %+v, %+v", researcher, scout)
	}

	var mixedCLI CLI
	mixedParser, err := kong.New(&mixedCLI)
	if err != nil {
		t.Fatalf("kong.New mixed: %v", err)
	}
	if _, err := mixedParser.Parse([]string{"notes.md", "--researcher", "anthropic.claude-fable-5@xhigh", "--scout", "openai.gpt-5.6-terra@low"}); err != nil {
		t.Fatalf("Parse mixed analyze: %v", err)
	}
	researcher, scout, err = mixedCLI.Analyze.modelSelections(appConfig{})
	if err != nil {
		t.Fatalf("mixed model selections: %v", err)
	}
	if researcher != (modelSelection{Provider: client.AnthropicProvider, Model: "claude-fable-5", Effort: "xhigh"}) ||
		scout != (modelSelection{Provider: client.OpenAIProvider, Model: "gpt-5.6-terra", Effort: "low"}) {
		t.Fatalf("mixed selections = %+v, %+v", researcher, scout)
	}
}

func TestModelSelectionsUseConfigAndCLIOverrides(t *testing.T) {
	config := appConfig{
		Researcher: "anthropic.claude-opus-4-8@xhigh",
		Scout:      "openai.gpt-5.6-terra@low",
	}

	researcher, scout, err := (&AnalyzeCmd{}).modelSelections(config)
	if err != nil {
		t.Fatalf("config model selections: %v", err)
	}
	if researcher != (modelSelection{Provider: client.AnthropicProvider, Model: "claude-opus-4-8", Effort: "xhigh"}) ||
		scout != (modelSelection{Provider: client.OpenAIProvider, Model: "gpt-5.6-terra", Effort: "low"}) {
		t.Fatalf("config selections = %+v, %+v", researcher, scout)
	}

	command := AnalyzeCmd{Researcher: "openai.gpt-5.5-pro@medium"}
	researcher, scout, err = command.modelSelections(config)
	if err != nil {
		t.Fatalf("CLI override model selections: %v", err)
	}
	if researcher != (modelSelection{Provider: client.OpenAIProvider, Model: "gpt-5.5-pro", Effort: "medium"}) ||
		scout != (modelSelection{Provider: client.OpenAIProvider, Model: "gpt-5.6-terra", Effort: "low"}) {
		t.Fatalf("CLI override selections = %+v, %+v", researcher, scout)
	}
}

func TestModelSelectionsRejectInvalidConfig(t *testing.T) {
	_, _, err := (&AnalyzeCmd{}).modelSelections(appConfig{Researcher: "claude-opus-4-8"})
	if err == nil {
		t.Fatal("modelSelections accepted an unqualified configured model")
	}
}

func TestLoadAppConfigReadsGlobalModelDefaults(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	path := filepath.Join(configDir, "deep-analysis", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("researcher: anthropic.claude-opus-4-8@xhigh\nscout: openai.gpt-5.6-terra@low\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	config, gotPath, err := loadAppConfig()
	if err != nil {
		t.Fatalf("loadAppConfig: %v", err)
	}
	if gotPath != path || config.Researcher != "anthropic.claude-opus-4-8@xhigh" || config.Scout != "openai.gpt-5.6-terra@low" {
		t.Fatalf("loaded config path=%q config=%+v", gotPath, config)
	}
}

func TestParseModelSelection(t *testing.T) {
	selection, err := parseModelSelection("researcher", "openai.gpt@preview@xhigh")
	if err != nil {
		t.Fatalf("parseModelSelection: %v", err)
	}
	if selection != (modelSelection{Provider: client.OpenAIProvider, Model: "gpt@preview", Effort: "xhigh"}) {
		t.Fatalf("selection = %+v", selection)
	}
	if selection.String() != "openai.gpt@preview@xhigh" {
		t.Fatalf("selection string = %q", selection)
	}

	for _, value := range []string{"gpt-5.6", ".gpt-5.6", "openai.", "other.model", "openai.gpt@", "openai.gpt@turbo"} {
		if _, err := parseModelSelection("researcher", value); err == nil {
			t.Fatalf("parseModelSelection accepted %q", value)
		}
	}
}

func TestLegacyReasoningEffortOverride(t *testing.T) {
	researcher, _, err := (&AnalyzeCmd{ReasoningEffort: "high"}).modelSelections(appConfig{})
	if err != nil {
		t.Fatalf("legacy effort with compiled default: %v", err)
	}
	if researcher.Effort != "high" {
		t.Fatalf("compiled researcher effort = %q, want high", researcher.Effort)
	}
	researcher, _, err = (&AnalyzeCmd{ReasoningEffort: "medium"}).modelSelections(appConfig{Researcher: "anthropic.claude-opus-4-8@xhigh"})
	if err != nil {
		t.Fatalf("legacy effort overriding config: %v", err)
	}
	if researcher.Effort != "medium" {
		t.Fatalf("configured researcher effort = %q, want medium", researcher.Effort)
	}

	researcher, _, err = (&AnalyzeCmd{Researcher: "anthropic.claude-opus-4-8", ReasoningEffort: "high"}).modelSelections(appConfig{})
	if err != nil {
		t.Fatalf("legacy effort override: %v", err)
	}
	if researcher.Effort != "high" {
		t.Fatalf("researcher effort = %q, want high", researcher.Effort)
	}

	_, _, err = (&AnalyzeCmd{Researcher: "anthropic.claude-opus-4-8@xhigh", ReasoningEffort: "high"}).modelSelections(appConfig{})
	if err == nil {
		t.Fatal("modelSelections accepted effort in both selection and legacy flag")
	}
	_, _, err = (&AnalyzeCmd{Researcher: "anthropic.claude-opus-4-8", ReasoningEffort: "turbo"}).modelSelections(appConfig{})
	if err == nil {
		t.Fatal("modelSelections accepted an invalid legacy effort")
	}
}

func TestProvidersForSessionSupportsLegacyAndMixedSessions(t *testing.T) {
	testCases := []struct {
		name           string
		session        client.Session
		wantResearcher string
		wantScout      string
	}{
		{"legacy openai", client.Session{}, client.OpenAIProvider, client.OpenAIProvider},
		{"legacy anthropic", client.Session{Provider: client.AnthropicProvider}, client.AnthropicProvider, client.AnthropicProvider},
		{"mixed", client.Session{ResearcherProvider: client.AnthropicProvider, ScoutProvider: client.OpenAIProvider}, client.AnthropicProvider, client.OpenAIProvider},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			researcher, scout := providersForSession(&tc.session)
			if researcher != tc.wantResearcher || scout != tc.wantScout {
				t.Fatalf("providers = %q, %q", researcher, scout)
			}
		})
	}
}

func TestDoctorReportsCredentialSourceWithoutSecret(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "secret-key")
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	configPath := filepath.Join(configDir, "deep-analysis", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("researcher: anthropic.claude-opus-4-8@xhigh\nscout: openai.gpt-5.6-terra@low\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var out bytes.Buffer
	if err := runDoctor(&out); err != nil {
		t.Fatalf("runDoctor: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "credentials.openai: ok (OPENAI_API_KEY)") {
		t.Fatalf("doctor output missing credential source:\n%s", got)
	}
	if !strings.Contains(got, "models.researcher: anthropic.claude-opus-4-8@xhigh ("+configPath+")") ||
		!strings.Contains(got, "models.scout: openai.gpt-5.6-terra@low ("+configPath+")") {
		t.Fatalf("doctor output missing effective models:\n%s", got)
	}
	if strings.Contains(got, "secret-key") {
		t.Fatalf("doctor output leaked credential:\n%s", got)
	}
}
