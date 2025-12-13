package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/responses"
)

const (
	DefaultScoutModel = "gpt-5.2"
)

// ScoutUsage tracks token usage for scout operations.
type ScoutUsage struct {
	InputTokens  int64
	OutputTokens int64
	Calls        int
}

// Scout dispatches high-level tool requests to low-level file operations.
// It translates natural language queries into specific glob/grep/read operations.
type Scout struct {
	client  *openai.Client
	model   string
	fileOps FileOps
	usage   ScoutUsage
}

// NewScout creates a scout dispatcher with the given API key and model.
func NewScout(apiKey string, model string, fileOps FileOps) *Scout {
	if model == "" {
		model = DefaultScoutModel
	}
	client := openai.NewClient(option.WithAPIKey(apiKey))
	return &Scout{
		client:  &client,
		model:   model,
		fileOps: fileOps,
	}
}

// Usage returns the accumulated token usage for this scout.
func (s *Scout) Usage() ScoutUsage {
	return s.usage
}

// ResetUsage clears the accumulated usage counters.
func (s *Scout) ResetUsage() {
	s.usage = ScoutUsage{}
}

// FindFilesResult contains the results of a find_files operation.
type FindFilesResult struct {
	Files      []FileMatch `json:"files"`
	TotalBytes int64       `json:"total_bytes"`
	Notes      string      `json:"notes,omitempty"`
}

// FileMatch represents a single file match with context.
type FileMatch struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`              // file size in bytes
	Context string `json:"context,omitempty"` // matching lines, relevance note
}

// FindFiles discovers files matching a natural language query.
func (s *Scout) FindFiles(ctx context.Context, query string, paths []string) (*FindFilesResult, error) {
	if query == "" {
		return nil, fmt.Errorf("query must not be empty")
	}
	if len(paths) == 0 {
		paths = []string{"."}
	}

	log.Info("Scout find_files", "query", query, "paths", paths)

	// Build context for the scout: list available files
	manifest, err := BuildManifest(ctx, ".", 500, 3)
	if err != nil {
		log.Warn("Failed to build manifest for scout", "error", err)
		manifest = "(manifest unavailable)"
	}

	prompt := fmt.Sprintf(`Find files matching this query: %q

Search paths: %v

Available files in project:
%s

Return a JSON object with:
- "operations": array of operations to run, each with:
  - "type": "glob" or "grep"  
  - "pattern": the pattern to use
  - "path": path/glob to search in (for grep)
- "reasoning": brief explanation of your approach`, query, paths, manifest)

	params := responses.ResponseNewParams{
		Model:        s.model,
		Instructions: openai.Opt(s.findFilesSystemPrompt()),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: responses.ResponseInputParam{
				responses.ResponseInputItemParamOfMessage(prompt, responses.EasyInputMessageRoleUser),
			},
		},
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigParamOfJSONSchema("find_operations", s.findOperationsSchema()),
		},
	}

	response, err := s.client.Responses.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("scout find_files API call failed: %w", err)
	}

	s.usage.InputTokens += response.Usage.InputTokens
	s.usage.OutputTokens += response.Usage.OutputTokens
	s.usage.Calls++

	log.Debug("Scout find_files response",
		"input_tokens", response.Usage.InputTokens,
		"output_tokens", response.Usage.OutputTokens)

	text := extractScoutText(response)
	if text == "" {
		return nil, fmt.Errorf("no response from scout")
	}

	var ops struct {
		Operations []struct {
			Type    string `json:"type"`
			Pattern string `json:"pattern"`
			Path    string `json:"path"`
		} `json:"operations"`
		Reasoning string `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(text), &ops); err != nil {
		return nil, fmt.Errorf("failed to parse scout response: %w", err)
	}

	log.Debug("Scout planned operations", "count", len(ops.Operations), "reasoning", ops.Reasoning)

	// Execute the operations
	seen := make(map[string]struct{})
	var matches []FileMatch

	for _, op := range ops.Operations {
		switch op.Type {
		case "glob":
			result, err := s.fileOps.GlobFiles(ctx, op.Pattern)
			if err != nil {
				log.Debug("Glob operation failed", "pattern", op.Pattern, "error", err)
				continue
			}
			for _, line := range strings.Split(result, "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasSuffix(line, "/") {
					continue
				}
				if _, ok := seen[line]; !ok {
					seen[line] = struct{}{}
					matches = append(matches, FileMatch{Path: line})
				}
			}

		case "grep":
			path := op.Path
			if path == "" {
				path = "."
			}
			result, err := s.fileOps.GrepFiles(ctx, op.Pattern, path, true)
			if err != nil {
				log.Debug("Grep operation failed", "pattern", op.Pattern, "error", err)
				continue
			}
			// Parse grep results: "path:line:content"
			for _, line := range strings.Split(result, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				parts := strings.SplitN(line, ":", 3)
				if len(parts) >= 1 {
					filePath := parts[0]
					if _, ok := seen[filePath]; !ok {
						seen[filePath] = struct{}{}
						context := ""
						if len(parts) >= 3 {
							context = strings.TrimSpace(parts[2])
							if len(context) > 100 {
								context = context[:100] + "..."
							}
						}
						matches = append(matches, FileMatch{Path: filePath, Context: context})
					}
				}
			}
		}
	}

	// Limit results
	if len(matches) > 50 {
		matches = matches[:50]
	}

	// Get file sizes
	var totalBytes int64
	for i := range matches {
		if info, err := os.Stat(matches[i].Path); err == nil {
			matches[i].Size = info.Size()
			totalBytes += info.Size()
		}
	}

	log.Info("Scout find_files complete", "matches", len(matches), "total_bytes", totalBytes)

	return &FindFilesResult{
		Files:      matches,
		TotalBytes: totalBytes,
		Notes:      ops.Reasoning,
	}, nil
}

// SummarizeFilesResult contains summaries of requested files.
type SummarizeFilesResult struct {
	Summaries []FileSummary `json:"summaries"`
}

// FileSummary is a scout-generated summary of a file.
type FileSummary struct {
	Path    string `json:"path"`
	Summary string `json:"summary"`
	Error   string `json:"error,omitempty"`
}

// SummarizeFiles generates summaries of the given files.
func (s *Scout) SummarizeFiles(ctx context.Context, paths []string, focus string) (*SummarizeFilesResult, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("paths must not be empty")
	}

	log.Info("Scout summarize_files", "paths", len(paths), "focus", focus)

	// Read all files first
	var fileContents []string
	for _, path := range paths {
		content, err := s.fileOps.ReadFile(ctx, path)
		if err != nil {
			log.Debug("Failed to read file for summary", "path", path, "error", err)
			continue
		}
		// Truncate very large files
		if len(content) > 50000 {
			content = content[:50000] + "\n\n[... truncated ...]"
		}
		fileContents = append(fileContents, fmt.Sprintf("=== %s ===\n%s", path, content))
	}

	if len(fileContents) == 0 {
		return &SummarizeFilesResult{}, nil
	}

	focusInstruction := ""
	if focus != "" {
		focusInstruction = fmt.Sprintf("\n\nFocus especially on: %s", focus)
	}

	prompt := fmt.Sprintf(`Summarize each of these files concisely. For each file, provide a 2-4 sentence summary of its purpose and key contents.%s

%s

Return JSON with "summaries" array, each with "path" and "summary".`, focusInstruction, strings.Join(fileContents, "\n\n"))

	params := responses.ResponseNewParams{
		Model:        s.model,
		Instructions: openai.Opt("You are a code summarization assistant. Provide clear, concise summaries of source code files. Focus on purpose, key functions/types, and notable patterns."),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: responses.ResponseInputParam{
				responses.ResponseInputItemParamOfMessage(prompt, responses.EasyInputMessageRoleUser),
			},
		},
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigParamOfJSONSchema("file_summaries", s.summariesSchema()),
		},
	}

	response, err := s.client.Responses.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("scout summarize_files API call failed: %w", err)
	}

	s.usage.InputTokens += response.Usage.InputTokens
	s.usage.OutputTokens += response.Usage.OutputTokens
	s.usage.Calls++

	log.Debug("Scout summarize_files response",
		"input_tokens", response.Usage.InputTokens,
		"output_tokens", response.Usage.OutputTokens)

	text := extractScoutText(response)
	if text == "" {
		return nil, fmt.Errorf("no response from scout")
	}

	var result SummarizeFilesResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, fmt.Errorf("failed to parse scout response: %w", err)
	}

	log.Info("Scout summarize_files complete", "summaries", len(result.Summaries))

	return &result, nil
}

// ReadFilesResult contains the contents of requested files.
type ReadFilesResult struct {
	Files []FileContent `json:"files"`
}

// FileContent is the content of a single file.
type FileContent struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ReadFiles reads the contents of the given files.
// This is a thin wrapper that doesn't need scout LLM - just batch reads.
// Enforces limits to prevent excessive token usage.
func (s *Scout) ReadFiles(ctx context.Context, paths []string, ranges map[string][2]int) (*ReadFilesResult, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("paths must not be empty")
	}

	const (
		maxFiles      = 10
		maxTotalBytes = 200000 // 200KB total
		maxFileSize   = 100000 // 100KB per file
	)

	// Check file count limit
	if len(paths) > maxFiles {
		return nil, fmt.Errorf("too many files requested (%d). Maximum is %d files per read_files call. Use summarize_files to triage first, or make multiple smaller read_files calls", len(paths), maxFiles)
	}

	// Check total size before reading
	var totalSize int64
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil {
			totalSize += info.Size()
		}
	}
	if totalSize > maxTotalBytes {
		return nil, fmt.Errorf("total file size too large (%s). Maximum is %s per read_files call. Use summarize_files to triage and identify which files you actually need, then read fewer files", formatBytesScout(totalSize), formatBytesScout(maxTotalBytes))
	}

	log.Info("Scout read_files", "paths", len(paths), "total_size", totalSize)

	var files []FileContent
	for _, path := range paths {
		content, err := s.fileOps.ReadFile(ctx, path)
		if err != nil {
			files = append(files, FileContent{
				Path:  path,
				Error: err.Error(),
			})
			continue
		}

		// Apply line range if specified
		if rng, ok := ranges[path]; ok && len(rng) == 2 {
			lines := strings.Split(content, "\n")
			start, end := rng[0]-1, rng[1] // Convert to 0-indexed
			if start < 0 {
				start = 0
			}
			if end > len(lines) {
				end = len(lines)
			}
			if start < end {
				content = strings.Join(lines[start:end], "\n")
			}
		}

		truncated := false
		if len(content) > maxFileSize {
			content = content[:maxFileSize] + "\n\n[... truncated at 100KB ...]"
			truncated = true
		}

		files = append(files, FileContent{
			Path:      path,
			Content:   content,
			Truncated: truncated,
		})
	}

	log.Info("Scout read_files complete", "files", len(files))

	return &ReadFilesResult{Files: files}, nil
}

func (s *Scout) findFilesSystemPrompt() string {
	return `You are a file discovery assistant. Given a natural language query about what files to find, plan the glob and grep operations needed.

Guidelines:
- Use "glob" for file name patterns: "all go files" → {"type": "glob", "pattern": "**/*.go"}
- Use "grep" for content searches: "error handling" → {"type": "grep", "pattern": "error|Error|ERROR", "path": "**/*.go"}
- Combine operations when helpful
- Be specific with patterns to avoid too many results
- Look at the manifest to understand the project structure`
}

func (s *Scout) findOperationsSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operations": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"type": map[string]any{
							"type": "string",
							"enum": []string{"glob", "grep"},
						},
						"pattern": map[string]any{
							"type": "string",
						},
						"path": map[string]any{
							"type":        "string",
							"description": "Path/glob for grep operations, empty string for glob-only operations",
						},
					},
					"required":             []string{"type", "pattern", "path"},
					"additionalProperties": false,
				},
			},
			"reasoning": map[string]any{
				"type": "string",
			},
		},
		"required":             []string{"operations", "reasoning"},
		"additionalProperties": false,
	}
}

func (s *Scout) summariesSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summaries": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{
							"type": "string",
						},
						"summary": map[string]any{
							"type": "string",
						},
					},
					"required":             []string{"path", "summary"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"summaries"},
		"additionalProperties": false,
	}
}

func extractScoutText(response *responses.Response) string {
	for _, item := range response.Output {
		if item.Type == "message" {
			for _, content := range item.Content {
				if content.Type == "text" || content.Type == "output_text" {
					return content.Text
				}
			}
		}
	}
	return ""
}

func formatBytesScout(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "KMGTPE"[exp])
}
