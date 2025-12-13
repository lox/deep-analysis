package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// FileSearcher dispatches high-level search intent to filesystem tools.
type FileSearcher struct {
	fileOps FileOps
}

func NewFileSearcher(fileOps FileOps) *FileSearcher {
	return &FileSearcher{fileOps: fileOps}
}

// Search performs a simple path-oriented search based on intent text and optional hint paths.
// It favors globbing for filename matches and falls back to a broad glob if nothing is hinted.
func (s *FileSearcher) Search(ctx context.Context, query string, hintPaths []string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("query must not be empty")
	}

	// Decide bases to search.
	bases := hintPaths
	if len(bases) == 0 {
		bases = []string{"."}
	}

	var results []string
	seen := make(map[string]struct{})

	var lastErr error
	for _, base := range bases {
		base = strings.TrimSpace(base)
		if base == "" {
			base = "."
		}
		pattern := filepath.Join(base, "**", "*"+sanitizeForGlob(query)+"*")
		out, err := s.fileOps.GlobFiles(ctx, pattern)
		if err != nil {
			lastErr = err
			continue
		}
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if _, ok := seen[line]; ok {
				continue
			}
			seen[line] = struct{}{}
			results = append(results, line)
		}
	}

	if len(results) == 0 {
		if lastErr != nil {
			return "", fmt.Errorf("file_search failed: %w", lastErr)
		}
		return "No matches found for file_search query.", nil
	}

	// Keep it concise.
	if len(results) > 100 {
		results = results[:100]
		results = append(results, fmt.Sprintf("... (%d more results truncated)", len(seen)-100))
	}

	return strings.Join(results, "\n"), nil
}

// sanitizeForGlob strips path separators from query fragments.
func sanitizeForGlob(q string) string {
	q = strings.ReplaceAll(q, string(filepath.Separator), " ")
	return q
}
