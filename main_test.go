package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alecthomas/kong"
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

func TestCLIParsesSetupAndDefaultAnalyze(t *testing.T) {
	var setupCLI CLI
	setupParser, err := kong.New(&setupCLI)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}

	if _, err := setupParser.Parse([]string{"setup"}); err != nil {
		t.Fatalf("Parse setup: %v", err)
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
}
