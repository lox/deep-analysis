package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/alecthomas/kong"
	"github.com/charmbracelet/log"
	"github.com/lox/deep-analysis-mcp/internal/client"
	"github.com/lox/deep-analysis-mcp/internal/fileops"
)

var (
	version = "dev"
)

type CLI struct {
	Input      string `arg:"" help:"Path to input markdown document (relative to --cwd if set)"`
	Output     string `help:"Path to output markdown document (defaults to input file)"`
	Debug      bool   `help:"Enable debug logging"`
	Continue   string `help:"Session id to continue a previous conversation" name:"continue"`
	Reset      bool   `help:"Ignore stored session state and start a fresh conversation"`
	ScoutModel string `help:"Model to use for scout dispatcher (default: gpt-5.1)" default:"gpt-5.1"`
	Cwd        string `help:"Working directory for file operations (default: current directory)"`
}

func (c *CLI) Run() error {
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

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("OPENAI_API_KEY environment variable is required")
	}

	// Read input document
	log.Info("Reading input document", "path", c.Input)
	inputContent, err := os.ReadFile(c.Input)
	if err != nil {
		return fmt.Errorf("failed to read input file: %w", err)
	}

	// Initialize client with scout dispatcher
	f := fileops.New()
	cl := client.New(apiKey, f, c.ScoutModel)

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
	log.Info("Running deep analysis", "bytes", len(document), "scout_model", c.ScoutModel)
	result, err := cl.Analyze(ctx, document, client.AnalysisOptions{
		PreviousResponseID: previousResponseID,
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

func main() {
	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("deep-analysis"),
		kong.Description("Deep analysis tool powered by GPT-5-Pro with file operation capabilities"),
		kong.UsageOnError(),
		kong.Vars{
			"version": version,
		},
	)

	if err := cli.Run(); err != nil {
		log.Fatal(err)
	}
	ctx.Exit(0)
}
