package agent

import "context"

// FileOps is the shared filesystem/search interface used by agents.
type FileOps interface {
	ReadFile(ctx context.Context, path string) (string, error)
	GrepFiles(ctx context.Context, pattern, path string, ignoreCase bool) (string, error)
	GlobFiles(ctx context.Context, pattern string) (string, error)
	ListFiles(ctx context.Context, path string) (string, error)
	SemanticSearch(ctx context.Context, query string, limit int) (string, error)
	IsCKAvailable() bool
}
