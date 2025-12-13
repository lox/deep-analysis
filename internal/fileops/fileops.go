package fileops

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/charmbracelet/log"
)

// Handler provides file operation capabilities
type Handler struct {
	ckAvailable bool
}

// New creates a new file operations handler
func New() *Handler {
	h := &Handler{}
	h.ckAvailable = h.checkCKAvailable()
	if h.ckAvailable {
		log.Info("ck tool detected", "semantic_search", "enabled")
	} else {
		log.Debug("ck tool not found", "semantic_search", "disabled")
	}
	return h
}

// checkCKAvailable checks if the ck command is available
func (h *Handler) checkCKAvailable() bool {
	_, err := exec.LookPath("ck")
	return err == nil
}

// IsCKAvailable returns whether ck is available
func (h *Handler) IsCKAvailable() bool {
	return h.ckAvailable
}

const (
	maxFileSize       = 5 * 1024 * 1024  // 5MB
	maxGrepResultSize = 10 * 1024 * 1024 // 10MB total grep results
	maxMatchesPerFile = 1000             // Max matches per file
)

// Common directories to exclude from grep
var excludeDirs = []string{
	".git",
	"node_modules",
	"vendor",
	".venv",
	"venv",
	"dist",
	"build",
	"target",
	".next",
	".nuxt",
	"__pycache__",
	".cache",
}

// ReadFile reads a file and returns its contents
func (h *Handler) ReadFile(ctx context.Context, path string) (string, error) {
	log.Debug("ReadFile called", "path", path)

	// Check context before starting
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Expand ~ to home directory (only ~/path, not ~user/path)
	if strings.HasPrefix(path, "~") {
		if len(path) > 1 && path[1] != '/' && path[1] != filepath.Separator {
			return "", fmt.Errorf("unsupported path format: only ~/ is supported, not ~username")
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		path = filepath.Join(home, path[1:])
	}

	// Check file size before reading
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}

	if info.Size() > maxFileSize {
		return "", fmt.Errorf("file too large (%d bytes, max %d bytes): consider using grep_files instead", info.Size(), maxFileSize)
	}

	// Check context again before reading
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Read the file
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	return string(content), nil
}

// GrepFiles searches for a pattern in files
func (h *Handler) GrepFiles(ctx context.Context, pattern, pathPattern string, ignoreCase bool) (string, error) {
	log.Debug("GrepFiles called", "pattern", pattern, "path", pathPattern, "ignore_case", ignoreCase)

	// Check context before starting
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Compile regex
	flags := ""
	if ignoreCase {
		flags = "(?i)"
	}
	re, err := regexp.Compile(flags + pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex pattern: %w", err)
	}

	// Expand ~ to home directory (only ~/path, not ~user/path)
	if strings.HasPrefix(pathPattern, "~") {
		if len(pathPattern) > 1 && pathPattern[1] != '/' && pathPattern[1] != filepath.Separator {
			return "", fmt.Errorf("unsupported path format: only ~/ is supported, not ~username")
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		pathPattern = filepath.Join(home, pathPattern[1:])
	}

	// Find matching files using doublestar for ** support
	matches, err := doublestar.FilepathGlob(pathPattern)
	if err != nil {
		return "", fmt.Errorf("invalid path pattern: %w", err)
	}

	if len(matches) == 0 {
		return "No files matched the pattern", nil
	}

	log.Debug("GrepFiles matched files", "count", len(matches))

	var results []string
	filesProcessed := 0
	filesSkipped := 0

	// Search each file
	for _, path := range matches {
		// Check context periodically
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		// Skip excluded directories
		shouldSkip := false
		for _, excludeDir := range excludeDirs {
			if strings.Contains(path, string(filepath.Separator)+excludeDir+string(filepath.Separator)) ||
				strings.HasPrefix(path, excludeDir+string(filepath.Separator)) {
				shouldSkip = true
				break
			}
		}
		if shouldSkip {
			filesSkipped++
			continue
		}

		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			filesSkipped++
			continue
		}

		// Skip large files
		if info.Size() > maxFileSize {
			log.Debug("Skipping large file", "path", path, "size_mb", info.Size()/1024/1024)
			filesSkipped++
			continue
		}

		filesProcessed++

		file, err := os.Open(path)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(file)
		// Increase buffer size to handle long lines (1MB max token)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		lineNum := 0
		var fileResults []string
		matchCount := 0

		for scanner.Scan() {
			// Check context periodically
			select {
			case <-ctx.Done():
				_ = file.Close()
				return "", ctx.Err()
			default:
			}

			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				matchCount++
				fileResults = append(fileResults, fmt.Sprintf("%d:%s", lineNum, line))

				// Stop if we hit the per-file match limit
				if matchCount >= maxMatchesPerFile {
					fileResults = append(fileResults, fmt.Sprintf("... (truncated, %d+ matches)", maxMatchesPerFile))
					break
				}
			}
		}

		// Check for scanner errors - skip binary files
		if err := scanner.Err(); err != nil {
			_ = file.Close()
			// Don't fail on binary files, just skip them
			continue
		}

		_ = file.Close()

		if len(fileResults) > 0 {
			results = append(results, fmt.Sprintf("\n%s:", path))
			results = append(results, fileResults...)

			// Check total result size
			currentResult := strings.Join(results, "\n")
			if len(currentResult) > maxGrepResultSize {
				results = append(results, fmt.Sprintf("\n\n[TRUNCATED: Results exceeded %d bytes limit. %d files processed so far.]", maxGrepResultSize, len(matches)))
				return strings.Join(results, "\n"), nil
			}
		}
	}

	if len(results) == 0 {
		log.Debug("GrepFiles complete", "files_processed", filesProcessed, "files_skipped", filesSkipped, "matches", 0)
		return "No matches found", nil
	}

	log.Debug("GrepFiles complete", "files_processed", filesProcessed, "files_skipped", filesSkipped, "result_bytes", len(strings.Join(results, "\n")))
	return strings.Join(results, "\n"), nil
}

// GlobFiles returns a list of files matching the glob pattern
func (h *Handler) GlobFiles(ctx context.Context, pattern string) (string, error) {
	log.Debug("GlobFiles called", "pattern", pattern)

	// Check context before starting
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Expand ~ to home directory (only ~/path, not ~user/path)
	if strings.HasPrefix(pattern, "~") {
		if len(pattern) > 1 && pattern[1] != '/' && pattern[1] != filepath.Separator {
			return "", fmt.Errorf("unsupported path format: only ~/ is supported, not ~username")
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		pattern = filepath.Join(home, pattern[1:])
	}

	// Find matching files using doublestar for ** support
	matches, err := doublestar.FilepathGlob(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid glob pattern: %w", err)
	}

	if len(matches) == 0 {
		return "No files matched the pattern", nil
	}

	log.Debug("GlobFiles matched", "count", len(matches))

	var results []string
	for _, path := range matches {
		// Check context periodically
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		// Mark directories with trailing /
		if info.IsDir() {
			results = append(results, path+"/")
		} else {
			results = append(results, path)
		}
	}

	log.Debug("GlobFiles complete", "results", len(results))
	return strings.Join(results, "\n"), nil
}

// ListFiles lists files and directories in a given path
func (h *Handler) ListFiles(ctx context.Context, path string) (string, error) {
	log.Debug("ListFiles called", "path", path)

	// Check context before starting
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Expand ~ to home directory
	if strings.HasPrefix(path, "~") {
		if len(path) > 1 && path[1] != '/' && path[1] != filepath.Separator {
			return "", fmt.Errorf("unsupported path format: only ~/ is supported, not ~username")
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		path = filepath.Join(home, path[1:])
	}

	// Default to current directory if empty
	if path == "" {
		path = "."
	}

	// Read directory
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("failed to read directory: %w", err)
	}

	var results []string
	for _, entry := range entries {
		name := entry.Name()

		// Mark directories with trailing /
		if entry.IsDir() {
			results = append(results, name+"/")
		} else {
			// Get file size
			info, err := entry.Info()
			if err == nil {
				size := info.Size()
				sizeStr := formatSize(size)
				results = append(results, fmt.Sprintf("%s (%s)", name, sizeStr))
			} else {
				results = append(results, name)
			}
		}
	}

	log.Debug("ListFiles complete", "count", len(results))
	return strings.Join(results, "\n"), nil
}

// formatSize formats a file size in human-readable format
func formatSize(size int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case size >= GB:
		return fmt.Sprintf("%.1fGB", float64(size)/GB)
	case size >= MB:
		return fmt.Sprintf("%.1fMB", float64(size)/MB)
	case size >= KB:
		return fmt.Sprintf("%.1fKB", float64(size)/KB)
	default:
		return fmt.Sprintf("%dB", size)
	}
}

// CKResult represents a result from ck semantic search
type CKResult struct {
	Path    string  `json:"path"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet"`
}

// SemanticSearch performs semantic code search using ck
func (h *Handler) SemanticSearch(ctx context.Context, query string, limit int) (string, error) {
	if !h.ckAvailable {
		return "", fmt.Errorf("ck tool not available - install from https://github.com/BeaconBay/ck")
	}

	log.Debug("SemanticSearch called", "query", query, "limit", limit)

	// Check context before starting
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Default limit if not specified
	if limit <= 0 {
		limit = 10
	}

	// Run ck with semantic search and JSON output
	cmd := exec.CommandContext(ctx, "ck", "--sem", query, "--json")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("ck search failed: %s", string(exitErr.Stderr))
		}
		return "", fmt.Errorf("ck search failed: %w", err)
	}

	// Parse JSONL output (one JSON object per line)
	var results []CKResult
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var result CKResult
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			log.Debug("Failed to parse ck result", "line", line, "error", err)
			continue
		}
		results = append(results, result)
	}

	if len(results) == 0 {
		return "No semantic matches found", nil
	}

	log.Debug("SemanticSearch complete", "results", len(results))

	// Format results for GPT-5-Pro
	var formatted []string
	for i, r := range results {
		if i >= limit {
			break
		}

		// Format: path (score) followed by snippet
		formatted = append(formatted,
			fmt.Sprintf("%s (relevance: %.2f)\n%s", r.Path, r.Score, r.Snippet))
	}

	summary := fmt.Sprintf("Found %d semantic matches (showing top %d):\n\n%s",
		len(results), min(limit, len(results)), strings.Join(formatted, "\n\n---\n\n"))

	log.Debug("SemanticSearch formatted", "output_bytes", len(summary))
	return summary, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
