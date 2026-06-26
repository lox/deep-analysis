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
	"github.com/lox/deep-analysis/internal/client"
	"github.com/lox/deep-analysis/internal/fileops"
	"gopkg.in/yaml.v3"
)

var (
	version = "dev"
)

type CLI struct {
	Analyze AnalyzeCmd `cmd:"" default:"withargs" help:"Analyze a markdown document"`
	Setup   SetupCmd   `cmd:"" help:"Create XDG config and save the OpenAI API key"`
	Doctor  DoctorCmd  `cmd:"" help:"Check install and credential configuration"`
}

type AnalyzeCmd struct {
	Input    string `arg:"" help:"Path to input markdown document (relative to --cwd if set)"`
	Output   string `help:"Path to output markdown document (defaults to input file)"`
	Debug    bool   `help:"Enable debug logging"`
	Continue string `help:"Session id to continue a previous conversation" name:"continue"`
	Reset    bool   `help:"Ignore stored session state and start a fresh conversation"`
	// renovate: depName=openai/gpt-latest-pro
	ResearcherModel string `help:"Model to use for researcher" default:"gpt-5.5-pro"`
	// renovate: depName=openai/gpt-latest
	ScoutModel      string `help:"Model to use for scout dispatcher" default:"gpt-5.5"`
	ReasoningEffort string `help:"Reasoning effort for researcher: low, medium, high, xhigh (default: xhigh)" default:"xhigh" enum:"low,medium,high,xhigh"`
	Cwd             string `help:"Working directory for file operations (default: current directory)"`
}

type SetupCmd struct{}

type DoctorCmd struct{}

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

	apiKey, err := openAIAPIKey()
	if err != nil {
		return err
	}

	// Read input document
	log.Info("Reading input document", "path", c.Input)
	inputContent, err := os.ReadFile(c.Input)
	if err != nil {
		return fmt.Errorf("failed to read input file: %w", err)
	}

	// Initialize client with scout dispatcher
	f := fileops.New()
	cl := client.New(apiKey, f, c.ResearcherModel, c.ScoutModel)

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
	if previousResponseID != "" {
		// Add continuation note for the researcher
		document += "\n\n---\n\n**[Continuing from previous session. Look for any new questions or sections added after your last \"## Analysis\" output. Focus on answering those rather than repeating prior analysis.]**"
	}

	// Run analysis
	ctx := context.Background()
	log.Info("Running deep analysis",
		"bytes", len(document),
		"researcher_model", c.ResearcherModel,
		"scout_model", c.ScoutModel,
		"reasoning_effort", c.ReasoningEffort)
	result, err := cl.Analyze(ctx, document, client.AnalysisOptions{
		PreviousResponseID: previousResponseID,
		ReasoningEffort:    c.ReasoningEffort,
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

func (c *SetupCmd) Run() error {
	apiKey, err := promptOpenAIAPIKey()
	if err != nil {
		return err
	}

	path, err := saveOpenAIConfig(apiKey)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Saved config to %s\n", path)
	return nil
}

func (c *DoctorCmd) Run() error {
	return runDoctor(os.Stdout)
}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("deep-analysis"),
		kong.Description(fmt.Sprintf("Deep analysis tool powered by %s with file operation capabilities", client.DefaultResearcherModel)),
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
	if apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); apiKey != "" {
		return apiKey, "OPENAI_API_KEY", nil
	}

	paths, err := configPaths()
	if err != nil {
		return "", "", err
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", "", fmt.Errorf("read OpenAI API key from %s: %w", path, err)
		}

		var cfg appConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return "", "", fmt.Errorf("parse config %s: %w", path, err)
		}

		apiKey := strings.TrimSpace(cfg.OpenAIAPIKey)
		if apiKey == "" {
			apiKey = strings.TrimSpace(cfg.APIKey)
		}
		if apiKey == "" {
			return "", "", fmt.Errorf("OpenAI API key is missing in %s", path)
		}
		return apiKey, path, nil
	}

	return "", "", fmt.Errorf("OPENAI_API_KEY environment variable is required or put the key in %s", strings.Join(paths, " or "))
}

type appConfig struct {
	OpenAIAPIKey string `yaml:"openai_api_key,omitempty"`
	APIKey       string `yaml:"api_key,omitempty"`
}

func configPaths() ([]string, error) {
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
		filepath.Join(configDir, "openai", "config.yaml"),
	}, nil
}

func promptOpenAIAPIKey() (string, error) {
	fmt.Fprint(os.Stderr, "OpenAI API key: ")

	var data []byte
	var err error
	if term.IsTerminal(os.Stdin.Fd()) {
		data, err = term.ReadPassword(os.Stdin.Fd())
		fmt.Fprintln(os.Stderr)
	} else {
		data, err = readLine(os.Stdin)
	}
	if err != nil {
		return "", fmt.Errorf("read OpenAI API key: %w", err)
	}

	apiKey := strings.TrimSpace(string(data))
	if apiKey == "" {
		return "", fmt.Errorf("OpenAI API key is required")
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
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return "", fmt.Errorf("OpenAI API key is required")
	}

	paths, err := configPaths()
	if err != nil {
		return "", err
	}
	path := paths[0]

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}

	data, err := yaml.Marshal(appConfig{OpenAIAPIKey: apiKey})
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

	_, source, err := loadOpenAIAPIKey()
	if err != nil {
		fmt.Fprintf(w, "credentials: missing (%v)\n", err)
		return nil
	}
	fmt.Fprintf(w, "credentials: ok (%s)\n", source)
	return nil
}
