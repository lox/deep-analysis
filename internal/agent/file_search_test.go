package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type fakeFileOps struct {
	globCalls []string
	out       string
	err       error
}

func (f *fakeFileOps) ReadFile(ctx context.Context, path string) (string, error) { return "", nil }
func (f *fakeFileOps) GrepFiles(ctx context.Context, pattern, path string, ignoreCase bool) (string, error) {
	return "", nil
}
func (f *fakeFileOps) GlobFiles(ctx context.Context, pattern string) (string, error) {
	f.globCalls = append(f.globCalls, pattern)
	return f.out, f.err
}
func (f *fakeFileOps) ListFiles(ctx context.Context, path string) (string, error) { return "", nil }
func (f *fakeFileOps) SemanticSearch(ctx context.Context, query string, limit int) (string, error) {
	return "", nil
}
func (f *fakeFileOps) IsCKAvailable() bool { return false }

func TestFileSearchUsesHintPaths(t *testing.T) {
	f := &fakeFileOps{out: "src/main.go\nsrc/util.go"}
	fs := NewFileSearcher(f)
	_, err := fs.Search(context.Background(), "main", []string{"src"})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(f.globCalls) != 1 || f.globCalls[0] != "src/**/*.main*" && f.globCalls[0] != "src/**/*main*" {
		t.Fatalf("unexpected glob call: %v", f.globCalls)
	}
}

func TestFileSearchDedupesAndTruncates(t *testing.T) {
	lines := ""
	for i := 0; i < 120; i++ {
		lines += "file" + fmt.Sprintf("%03d", i) + "\n"
	}
	f := &fakeFileOps{out: lines}
	fs := NewFileSearcher(f)
	out, err := fs.Search(context.Background(), "file", nil)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	count := len(splitNonEmpty(out))
	if count != 101 { // 100 results + truncation line
		t.Fatalf("expected 101 lines, got %d", count)
	}
}

func TestFileSearchEmptyQuery(t *testing.T) {
	f := &fakeFileOps{}
	fs := NewFileSearcher(f)
	_, err := fs.Search(context.Background(), "  ", nil)
	if err == nil {
		t.Fatalf("expected error for empty query")
	}
}

func TestFileSearchPropagatesErrors(t *testing.T) {
	f := &fakeFileOps{err: errors.New("boom")}
	fs := NewFileSearcher(f)
	_, err := fs.Search(context.Background(), "x", nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}
