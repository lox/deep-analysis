package client

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionStoreRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	store, err := NewSessionStore("deep-analysis-test")
	if err != nil {
		t.Fatalf("NewSessionStore: %v", err)
	}

	id, err := store.GenerateID()
	if err != nil {
		t.Fatalf("GenerateID: %v", err)
	}

	sess := &Session{ID: id, PreviousResponseID: "resp_123"}
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.ID != id || loaded.PreviousResponseID != "resp_123" {
		t.Fatalf("loaded session mismatch: %+v", loaded)
	}

	if loaded.CreatedAt.IsZero() || loaded.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not set")
	}

	if loaded.UpdatedAt.Before(loaded.CreatedAt) {
		t.Fatalf("updated before created: created=%s updated=%s", loaded.CreatedAt, loaded.UpdatedAt)
	}

	// Ensure file is written where we expect.
	expectedPath := filepath.Join(os.Getenv("XDG_STATE_HOME"), "deep-analysis-test", "sessions", id+".json")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("session file not found: %v", err)
	}

	// Subsequent save keeps CreatedAt and updates data.
	time.Sleep(20 * time.Millisecond)
	loaded.PreviousResponseID = "resp_456"
	if err := store.Save(loaded); err != nil {
		t.Fatalf("Save second time: %v", err)
	}
	loaded2, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load second time: %v", err)
	}
	if loaded2.CreatedAt != loaded.CreatedAt {
		t.Fatalf("CreatedAt changed across saves")
	}
	if loaded2.UpdatedAt.Before(loaded2.CreatedAt) {
		t.Fatalf("UpdatedAt before CreatedAt")
	}
	if loaded2.PreviousResponseID != "resp_456" {
		t.Fatalf("PreviousResponseID not updated: %s", loaded2.PreviousResponseID)
	}
}
