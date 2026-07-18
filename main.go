package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/charmbracelet/log"
	"github.com/charmbracelet/x/term"
	"github.com/lox/deep-analysis/internal/agent"
	"github.com/lox/deep-analysis/internal/client"
	"github.com/lox/deep-analysis/internal/fileops"
	"gopkg.in/yaml.v3"
)

var (
	version = "dev"
)

type CLI struct {
	VersionFlag kong.VersionFlag `name:"version" help:"Print version and exit"`
	Analyze     AnalyzeCmd       `cmd:"" default:"withargs" help:"Analyze a markdown document"`
	Setup       SetupCmd         `cmd:"" help:"Create XDG config and save a provider API key"`
	Doctor      DoctorCmd        `cmd:"" help:"Check install, effective models, and credential configuration"`
	Version     VersionCmd       `cmd:"" help:"Print version and exit"`
}

type AnalyzeCmd struct {
	Input           string `arg:"" help:"Path to input markdown document (relative to --cwd if set)"`
	Output          string `help:"Path to output markdown document (defaults to input file)"`
	Debug           bool   `help:"Enable debug logging"`
	Continue        string `help:"Session id to continue a previous conversation" name:"continue"`
	Reset           bool   `help:"Ignore stored session state and start a fresh conversation"`
	Researcher      string `help:"Researcher as provider.model@effort (overrides global config; default: openai.gpt-5.6-sol@xhigh in Pro mode)"`
	Scout           string `help:"Scout as provider.model@effort (overrides global config; default: openai.gpt-5.5@low)"`
	ReasoningEffort string `help:"Deprecated researcher effort override: low, medium, high, xhigh"`
	Cwd             string `help:"Working directory for file operations (default: current directory)"`
}

type modelSelection struct {
	Provider string
	Model    string
	Effort   string
}

func (s modelSelection) String() string {
	value := s.Provider + "." + s.Model
	if s.Effort != "" {
		value += "@" + s.Effort
	}
	return value
}

type SetupCmd struct {
	Provider string `help:"Provider to configure: openai or anthropic" default:"openai" enum:"openai,anthropic"`
}

type DoctorCmd struct{}

type VersionCmd struct{}

func (c *AnalyzeCmd) Run() error {
	// Configure logging
	if c.Debug {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}

	// Change working directory if specified
	if c.Cwd != "" {
		if err := os.Chdir(c.Cwd); err != nil {
			return fmt.Errorf("failed to change directory to %s: %w", c.Cwd, err)
		}
	}

	// Log working directory
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "unknown"
	}
	log.Info("Starting deep-analysis", "cwd", cwd, "debug", c.Debug)

	// Validate input file exists (after cwd change so relative paths work)
	if _, err := os.Stat(c.Input); err != nil {
		return fmt.Errorf("input file not found: %w", err)
	}

	// Default output to input if not specified
	outputPath := c.Output
	if outputPath == "" {
		outputPath = c.Input
	}

	config, _, err := loadAppConfig()
	if err != nil {
		return err
	}
	researcher, scout, err := c.modelSelections(config)
	if err != nil {
		return err
	}

	researcherAPIKey, err := providerAPIKey(researcher.Provider)
	if err != nil {
		return err
	}
	scoutAPIKey := researcherAPIKey
	if scout.Provider != researcher.Provider {
		scoutAPIKey, err = providerAPIKey(scout.Provider)
		if err != nil {
			return err
		}
	}

	// Read input document
	log.Info("Reading input document", "path", c.Input)
	inputContent, err := os.ReadFile(c.Input)
	if err != nil {
		return fmt.Errorf("failed to read input file: %w", err)
	}

	// Initialize client with scout dispatcher
	f := fileops.New()
	cl, err := client.NewForProviders(
		researcher.Provider, researcherAPIKey,
		scout.Provider, scoutAPIKey,
		f, researcher.Model, scout.Model, scout.Effort,
	)
	if err != nil {
		return err
	}

	// Prepare session state
	store, err := client.NewSessionStore("deep-analysis")
	if err != nil {
		return fmt.Errorf("init session store: %w", err)
	}

	continueID := c.Continue
	if continueID == "" {
		continueID, err = store.GenerateID()
		if err != nil {
			return fmt.Errorf("generate session id: %w", err)
		}
		log.Info("Generated session id", "session", continueID)
	}

	var previousResponseID string
	var existingSession *client.Session
	if !c.Reset {
		if sess, err := store.Load(continueID); err == nil {
			sessionResearcherProvider, sessionScoutProvider := providersForSession(sess)
			if sessionResearcherProvider != researcher.Provider || sessionScoutProvider != scout.Provider {
				return fmt.Errorf(
					"session %s uses researcher=%s scout=%s; use --reset to restart it with researcher=%s scout=%s",
					continueID, sessionResearcherProvider, sessionScoutProvider, researcher.Provider, scout.Provider,
				)
			}
			existingSession = sess
			previousResponseID = sess.PreviousResponseID
			log.Info("Continuing session", "session", continueID, "previous_response_id", previousResponseID)
		} else if !os.IsNotExist(err) {
			log.Warn("Failed to load session", "session", continueID, "error", err)
		}
	} else {
		log.Info("Resetting session", "session", continueID)
	}

	// Prepare document content
	document := string(inputContent)
	if existingSession != nil {
		// Add continuation note for the researcher
		document += "\n\n---\n\n**[Continuing from previous session. Look for any new questions or sections added after your last \"## Analysis\" output. Focus on answering those rather than repeating prior analysis.]**"
	}

	// Run analysis
	ctx := context.Background()
	log.Info("Running deep analysis",
		"bytes", len(document),
		"researcher_provider", researcher.Provider,
		"scout_provider", scout.Provider,
		"researcher_model", researcher.Model,
		"scout_model", scout.Model,
		"researcher_effort", researcher.Effort,
		"scout_effort", scout.Effort)
	result, err := cl.Analyze(ctx, document, client.AnalysisOptions{
		PreviousResponseID: previousResponseID,
		ReasoningEffort:    researcher.Effort,
	})
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}

	// Append result to document
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	updatedContent := fmt.Sprintf("%s\n\n---\n\n## Analysis %s\n\n%s\n", string(inputContent), timestamp, result.Text)

	// Write output document
	log.Info("Writing output document", "path", outputPath)
	if err := os.WriteFile(outputPath, []byte(updatedContent), 0644); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	// Persist session state for follow-ups
	nextSession := &client.Session{
		ID:                 continueID,
		Provider:           sharedProvider(researcher.Provider, scout.Provider),
		ResearcherProvider: researcher.Provider,
		ScoutProvider:      scout.Provider,
		PreviousResponseID: result.ResponseID,
	}
	if existingSession != nil {
		nextSession.CreatedAt = existingSession.CreatedAt
	}
	if err := store.Save(nextSession); err != nil {
		log.Warn("Failed to save session state", "session", continueID, "error", err)
	} else {
		log.Info("Saved session", "session", continueID, "response_id", result.ResponseID)
	}

	log.Info("Analysis complete", "output", outputPath)
	return nil
}

func (c *AnalyzeCmd) modelSelections(config appConfig) (researcher, scout modelSelection, err error) {
	researcherValue := strings.TrimSpace(c.Researcher)
	researcherFromCLI := researcherValue != ""
	if researcherValue == "" {
		researcherValue = strings.TrimSpace(config.Researcher)
	}
	if researcherValue == "" {
		researcherValue = client.OpenAIProvider + "." + client.DefaultResearcherModel + "@xhigh"
	}
	scoutValue := strings.TrimSpace(c.Scout)
	if scoutValue == "" {
		scoutValue = strings.TrimSpace(config.Scout)
	}
	if scoutValue == "" {
		scoutValue = client.OpenAIProvider + "." + agent.DefaultScoutModel + "@low"
	}

	researcher, err = parseModelSelection("researcher", researcherValue)
	if err != nil {
		return modelSelection{}, modelSelection{}, err
	}
	scout, err = parseModelSelection("scout", scoutValue)
	if err != nil {
		return modelSelection{}, modelSelection{}, err
	}
	if c.ReasoningEffort != "" {
		if !validReasoningEffort(c.ReasoningEffort) {
			return modelSelection{}, modelSelection{}, fmt.Errorf("--reasoning-effort must be low, medium, high, or xhigh, got %q", c.ReasoningEffort)
		}
		if researcher.Effort != "" && researcherFromCLI {
			return modelSelection{}, modelSelection{}, fmt.Errorf("researcher effort is set in both --researcher and --reasoning-effort")
		}
		researcher.Effort = c.ReasoningEffort
	}
	return researcher, scout, nil
}

func parseModelSelection(role, value string) (modelSelection, error) {
	original := value
	effort := ""
	if index := strings.LastIndex(value, "@"); index >= 0 {
		effort = value[index+1:]
		value = value[:index]
		if !validReasoningEffort(effort) {
			return modelSelection{}, fmt.Errorf("%s effort must be low, medium, high, or xhigh, got %q", role, effort)
		}
	}
	provider, model, ok := strings.Cut(value, ".")
	if !ok || provider == "" || model == "" {
		return modelSelection{}, fmt.Errorf("%s must be provider.model[@effort], got %q", role, original)
	}
	if provider != client.OpenAIProvider && provider != client.AnthropicProvider {
		return modelSelection{}, fmt.Errorf("unsupported %s provider %q", role, provider)
	}
	return modelSelection{Provider: provider, Model: model, Effort: effort}, nil
}

func validReasoningEffort(effort string) bool {
	return effort == "low" || effort == "medium" || effort == "high" || effort == "xhigh"
}

func providersForSession(session *client.Session) (researcherProvider, scoutProvider string) {
	if session.ResearcherProvider != "" && session.ScoutProvider != "" {
		return session.ResearcherProvider, session.ScoutProvider
	}
	provider := session.Provider
	if provider == "" {
		provider = client.OpenAIProvider
	}
	return provider, provider
}

func sharedProvider(researcherProvider, scoutProvider string) string {
	if researcherProvider == scoutProvider {
		return researcherProvider
	}
	return ""
}

func (c *SetupCmd) Run() error {
	apiKey, err := promptProviderAPIKey(c.Provider)
	if err != nil {
		return err
	}

	path, err := saveProviderConfig(c.Provider, apiKey)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Saved config to %s\n", path)
	return nil
}

func (c *DoctorCmd) Run() error {
	return runDoctor(os.Stdout)
}

func (c *VersionCmd) Run() error {
	return writeVersion(os.Stdout, version)
}

func writeVersion(w io.Writer, value string) error {
	_, err := fmt.Fprintln(w, value)
	return err
}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("deep-analysis"),
		kong.Description("Deep analysis tool with OpenAI and Anthropic model support and file operation capabilities"),
		kong.UsageOnError(),
		kong.Vars{
			"version": version,
		},
	)

	if err := ctx.Run(); err != nil {
		log.Fatal(err)
	}
	ctx.Exit(0)
}

func openAIAPIKey() (string, error) {
	apiKey, _, err := loadOpenAIAPIKey()
	return apiKey, err
}

func loadOpenAIAPIKey() (string, string, error) {
	return loadProviderAPIKey(client.OpenAIProvider)
}

func providerAPIKey(provider string) (string, error) {
	apiKey, _, err := loadProviderAPIKey(provider)
	return apiKey, err
}

func loadProviderAPIKey(provider string) (string, string, error) {
	envName, err := providerAPIKeyEnv(provider)
	if err != nil {
		return "", "", err
	}
	if apiKey := strings.TrimSpace(os.Getenv(envName)); apiKey != "" {
		return apiKey, envName, nil
	}

	paths, err := providerConfigPaths(provider)
	if err != nil {
		return "", "", err
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", "", fmt.Errorf("read %s API key from %s: %w", provider, path, err)
		}

		var cfg appConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return "", "", fmt.Errorf("parse config %s: %w", path, err)
		}

		var apiKey string
		switch provider {
		case client.OpenAIProvider:
			apiKey = strings.TrimSpace(cfg.OpenAIAPIKey)
		case client.AnthropicProvider:
			apiKey = strings.TrimSpace(cfg.AnthropicAPIKey)
		}
		if apiKey == "" && (path != paths[0] || provider == client.OpenAIProvider) {
			apiKey = strings.TrimSpace(cfg.APIKey)
		}
		if apiKey == "" {
			continue
		}
		return apiKey, path, nil
	}

	return "", "", fmt.Errorf("%s environment variable is required or put the key in %s", envName, strings.Join(paths, " or "))
}

type appConfig struct {
	OpenAIAPIKey    string `yaml:"openai_api_key,omitempty"`
	AnthropicAPIKey string `yaml:"anthropic_api_key,omitempty"`
	APIKey          string `yaml:"api_key,omitempty"`
	Researcher      string `yaml:"researcher,omitempty"`
	Scout           string `yaml:"scout,omitempty"`
}

func loadAppConfig() (appConfig, string, error) {
	paths, err := configPaths()
	if err != nil {
		return appConfig{}, "", err
	}
	path := paths[0]
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return appConfig{}, path, nil
		}
		return appConfig{}, path, fmt.Errorf("read config %s: %w", path, err)
	}

	var config appConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return appConfig{}, path, fmt.Errorf("parse config %s: %w", path, err)
	}
	return config, path, nil
}

func configPaths() ([]string, error) {
	return providerConfigPaths(client.OpenAIProvider)
}

func providerConfigPaths(provider string) ([]string, error) {
	if provider != client.OpenAIProvider && provider != client.AnthropicProvider {
		return nil, fmt.Errorf("unsupported provider %q", provider)
	}
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir: %w", err)
		}
		configDir = filepath.Join(home, ".config")
	}
	return []string{
		filepath.Join(configDir, "deep-analysis", "config.yaml"),
		filepath.Join(configDir, provider, "config.yaml"),
	}, nil
}

func providerAPIKeyEnv(provider string) (string, error) {
	switch provider {
	case client.OpenAIProvider:
		return "OPENAI_API_KEY", nil
	case client.AnthropicProvider:
		return "ANTHROPIC_API_KEY", nil
	default:
		return "", fmt.Errorf("unsupported provider %q", provider)
	}
}

func promptProviderAPIKey(provider string) (string, error) {
	if _, err := providerAPIKeyEnv(provider); err != nil {
		return "", err
	}
	providerName := map[string]string{
		client.OpenAIProvider:    "OpenAI",
		client.AnthropicProvider: "Anthropic",
	}[provider]
	fmt.Fprintf(os.Stderr, "%s API key: ", providerName)

	var data []byte
	var err error
	if term.IsTerminal(os.Stdin.Fd()) {
		data, err = term.ReadPassword(os.Stdin.Fd())
		fmt.Fprintln(os.Stderr)
	} else {
		data, err = readLine(os.Stdin)
	}
	if err != nil {
		return "", fmt.Errorf("read %s API key: %w", provider, err)
	}

	apiKey := strings.TrimSpace(string(data))
	if apiKey == "" {
		return "", fmt.Errorf("%s API key is required", provider)
	}
	return apiKey, nil
}

func readLine(r io.Reader) ([]byte, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return nil, err
	}
	return []byte(line), nil
}

func saveOpenAIConfig(apiKey string) (string, error) {
	return saveProviderConfig(client.OpenAIProvider, apiKey)
}

func saveProviderConfig(provider, apiKey string) (string, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return "", fmt.Errorf("%s API key is required", provider)
	}

	paths, err := providerConfigPaths(provider)
	if err != nil {
		return "", err
	}
	path := paths[0]

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}

	var cfg appConfig
	if existing, readErr := os.ReadFile(path); readErr == nil {
		if err := yaml.Unmarshal(existing, &cfg); err != nil {
			return "", fmt.Errorf("parse existing config %s: %w", path, err)
		}
	} else if !os.IsNotExist(readErr) {
		return "", fmt.Errorf("read existing config %s: %w", path, readErr)
	}

	switch provider {
	case client.OpenAIProvider:
		cfg.OpenAIAPIKey = apiKey
	case client.AnthropicProvider:
		cfg.AnthropicAPIKey = apiKey
	default:
		return "", fmt.Errorf("unsupported provider %q", provider)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("encode config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write config: %w", err)
	}
	return path, nil
}

func runDoctor(w io.Writer) error {
	fmt.Fprintf(w, "version: %s\n", version)

	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		fmt.Fprintf(w, "binary: %s\n", exe)
	} else {
		fmt.Fprintf(w, "binary: unknown (%v)\n", err)
	}

	paths, err := configPaths()
	if err != nil {
		return err
	}
	anthropicPaths, err := providerConfigPaths(client.AnthropicProvider)
	if err != nil {
		return err
	}
	paths = append(paths, anthropicPaths[1])
	fmt.Fprintln(w, "config:")
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil {
			fmt.Fprintf(w, "  %s: present mode=%#o\n", path, info.Mode().Perm())
		} else if os.IsNotExist(err) {
			fmt.Fprintf(w, "  %s: missing\n", path)
		} else {
			fmt.Fprintf(w, "  %s: error: %v\n", path, err)
		}
	}

	config, configPath, err := loadAppConfig()
	if err != nil {
		return err
	}
	researcher, scout, err := (&AnalyzeCmd{}).modelSelections(config)
	if err != nil {
		return err
	}
	researcherSource := "compiled default"
	if strings.TrimSpace(config.Researcher) != "" {
		researcherSource = configPath
	}
	scoutSource := "compiled default"
	if strings.TrimSpace(config.Scout) != "" {
		scoutSource = configPath
	}
	fmt.Fprintf(w, "models.researcher: %s (%s)\n", researcher, researcherSource)
	fmt.Fprintf(w, "models.scout: %s (%s)\n", scout, scoutSource)

	for _, provider := range []string{client.OpenAIProvider, client.AnthropicProvider} {
		_, source, err := loadProviderAPIKey(provider)
		if err != nil {
			fmt.Fprintf(w, "credentials.%s: missing (%v)\n", provider, err)
			continue
		}
		fmt.Fprintf(w, "credentials.%s: ok (%s)\n", provider, source)
	}
	return nil
}
