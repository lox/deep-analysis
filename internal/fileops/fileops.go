package fileops

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Handler provides file operation capabilities
type Handler struct{}

// New creates a new file operations handler
func New() *Handler {
	return &Handler{}
}

const (
	maxFileSize = 5 * 1024 * 1024 // 5MB
)

// ReadFile reads a file and returns its contents
func (h *Handler) ReadFile(ctx context.Context, path string) (string, error) {
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

	var results []string

	// Search each file
	for _, path := range matches {
		// Check context periodically
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}

		file, err := os.Open(path)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(file)
		// Increase buffer size to handle long lines (1MB max token)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		lineNum := 0
		var fileResults []string

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
				fileResults = append(fileResults, fmt.Sprintf("%d:%s", lineNum, line))
			}
		}

		// Check for scanner errors
		if err := scanner.Err(); err != nil {
			_ = file.Close()
			return "", fmt.Errorf("error scanning %s: %w", path, err)
		}

		_ = file.Close()

		if len(fileResults) > 0 {
			results = append(results, fmt.Sprintf("\n%s:", path))
			results = append(results, fileResults...)
		}
	}

	if len(results) == 0 {
		return "No matches found", nil
	}

	return strings.Join(results, "\n"), nil
}

// GlobFiles returns a list of files matching the glob pattern
func (h *Handler) GlobFiles(ctx context.Context, pattern string) (string, error) {
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

	return strings.Join(results, "\n"), nil
}
