package fileops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobRecursive(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	
	// Create nested directories
	dirs := []string{
		"dir1",
		"dir1/subdir",
		"dir2",
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(tmpDir, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	
	// Create test files
	files := []string{
		"file1.txt",
		"dir1/file2.txt",
		"dir1/subdir/file3.txt",
		"dir2/file4.txt",
	}
	for _, file := range files {
		path := filepath.Join(tmpDir, file)
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	
	h := New()
	ctx := context.Background()
	
	// Test recursive glob with **
	pattern := filepath.Join(tmpDir, "**/*.txt")
	result, err := h.GlobFiles(ctx, pattern)
	if err != nil {
		t.Fatalf("GlobFiles failed: %v", err)
	}
	
	// Should find all 4 .txt files
	lines := strings.Split(result, "\n")
	if len(lines) != 4 {
		t.Errorf("Expected 4 matches, got %d: %v", len(lines), lines)
	}
	
	// Verify all expected files are found
	for _, file := range files {
		expectedPath := filepath.Join(tmpDir, file)
		if !strings.Contains(result, expectedPath) {
			t.Errorf("Expected to find %s in results", expectedPath)
		}
	}
}

func TestGlobDirSuffix(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create a directory and a file
	subdir := filepath.Join(tmpDir, "testdir")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	
	testfile := filepath.Join(tmpDir, "testfile.txt")
	if err := os.WriteFile(testfile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	
	h := New()
	ctx := context.Background()
	
	pattern := filepath.Join(tmpDir, "*")
	result, err := h.GlobFiles(ctx, pattern)
	if err != nil {
		t.Fatalf("GlobFiles failed: %v", err)
	}
	
	// Directory should have trailing /
	if !strings.Contains(result, subdir+"/") {
		t.Errorf("Expected directory to have trailing slash: %s", result)
	}
	
	// File should not have trailing /
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		if strings.Contains(line, "testfile.txt") && strings.HasSuffix(line, "/") {
			t.Error("File should not have trailing slash")
		}
	}
}

func TestReadFileTooLarge(t *testing.T) {
	tmpDir := t.TempDir()
	largefile := filepath.Join(tmpDir, "large.txt")
	
	// Create a file larger than maxFileSize (5MB)
	f, err := os.Create(largefile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	
	// Write 6MB of data
	data := make([]byte, 6*1024*1024)
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	f.Close()
	
	h := New()
	ctx := context.Background()
	
	_, err = h.ReadFile(ctx, largefile)
	if err == nil {
		t.Error("Expected error for too large file")
	}
	
	if !strings.Contains(err.Error(), "file too large") {
		t.Errorf("Expected 'file too large' error, got: %v", err)
	}
}

func TestGrepCaseInsensitive(t *testing.T) {
	tmpDir := t.TempDir()
	testfile := filepath.Join(tmpDir, "test.txt")
	
	content := `Hello World
HELLO WORLD
hello world
HeLLo WoRLd`
	
	if err := os.WriteFile(testfile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	
	h := New()
	ctx := context.Background()
	
	// Case insensitive search
	result, err := h.GrepFiles(ctx, "hello", testfile, true)
	if err != nil {
		t.Fatalf("GrepFiles failed: %v", err)
	}
	
	// Should match all 4 lines (count lines that have line numbers)
	lines := strings.Split(result, "\n")
	matchCount := 0
	for _, line := range lines {
		// Count lines with format "linenum:content" (not the file header)
		if strings.Contains(line, ":") && len(line) > 0 && line[0] >= '0' && line[0] <= '9' {
			matchCount++
		}
	}
	
	if matchCount != 4 {
		t.Errorf("Expected 4 matches, got %d: %s", matchCount, result)
	}
	
	// Case sensitive search
	result, err = h.GrepFiles(ctx, "hello", testfile, false)
	if err != nil {
		t.Fatalf("GrepFiles failed: %v", err)
	}
	
	// Should match only 1 line (lowercase hello)
	matchCount = 0
	for _, line := range strings.Split(result, "\n") {
		// Count lines with format "linenum:content" (not the file header)
		if strings.Contains(line, ":") && len(line) > 0 && line[0] >= '0' && line[0] <= '9' {
			matchCount++
		}
	}
	
	if matchCount != 1 {
		t.Errorf("Expected 1 match, got %d: %s", matchCount, result)
	}
}

func TestGrepRecursive(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create nested structure
	dirs := []string{
		"dir1",
		"dir1/subdir",
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(tmpDir, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	
	// Create files with pattern
	files := map[string]string{
		"file1.txt":              "contains target word",
		"dir1/file2.txt":         "contains target word",
		"dir1/subdir/file3.txt":  "contains target word",
		"file4.txt":              "no match here",
	}
	
	for file, content := range files {
		path := filepath.Join(tmpDir, file)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	
	h := New()
	ctx := context.Background()
	
	// Search recursively
	pattern := filepath.Join(tmpDir, "**/*.txt")
	result, err := h.GrepFiles(ctx, "target", pattern, false)
	if err != nil {
		t.Fatalf("GrepFiles failed: %v", err)
	}
	
	// Should find matches in 3 files
	if !strings.Contains(result, "file1.txt") {
		t.Error("Expected to find match in file1.txt")
	}
	if !strings.Contains(result, "file2.txt") {
		t.Error("Expected to find match in file2.txt")
	}
	if !strings.Contains(result, "file3.txt") {
		t.Error("Expected to find match in file3.txt")
	}
	if strings.Contains(result, "file4.txt") {
		t.Error("Should not find match in file4.txt")
	}
}
