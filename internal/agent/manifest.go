package agent

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// BuildManifest walks the tree up to maxDepth and returns a newline-delimited list of paths.
func BuildManifest(ctx context.Context, root string, maxEntries int, maxDepth int) (string, error) {
	if root == "" {
		root = "."
	}
	if maxEntries <= 0 {
		maxEntries = 400
	}
	if maxDepth <= 0 {
		maxDepth = 2
	}

	var entries []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable
		}

		// respect context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if path == root {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}

		depth := strings.Count(rel, string(filepath.Separator)) + 1
		if depth > maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		entries = append(entries, rel)
		if len(entries) >= maxEntries {
			return fmt.Errorf("manifest limit reached")
		}
		return nil
	})

	if err != nil && err != context.Canceled && err.Error() != "manifest limit reached" {
		return "", err
	}

	return strings.Join(entries, "\n"), nil
}
