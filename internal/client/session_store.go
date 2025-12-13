package client

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Session holds persisted conversation state.
type Session struct {
	ID                 string    `json:"id"`
	PreviousResponseID string    `json:"previous_response_id"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// SessionStore persists session metadata under the XDG state directory.
type SessionStore struct {
	dir string
}

// NewSessionStore creates a store using XDG_STATE_HOME or ~/.local/state.
func NewSessionStore(appName string) (*SessionStore, error) {
	stateDir := os.Getenv("XDG_STATE_HOME")
	if stateDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir: %w", err)
		}
		stateDir = filepath.Join(home, ".local", "state")
	}

	dir := filepath.Join(stateDir, appName, "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create session dir: %w", err)
	}

	return &SessionStore{dir: dir}, nil
}

// GenerateID returns a random hex session id.
func (s *SessionStore) GenerateID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// Load retrieves a session; returns os.ErrNotExist if missing.
func (s *SessionStore) Load(id string) (*Session, error) {
	path := s.pathFor(id)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("decode session: %w", err)
	}
	return &sess, nil
}

// Save writes the session to disk.
func (s *SessionStore) Save(sess *Session) error {
	now := time.Now().UTC()
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = now
	}
	sess.UpdatedAt = now

	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session: %w", err)
	}

	path := s.pathFor(sess.ID)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write session: %w", err)
	}
	return nil
}

func (s *SessionStore) pathFor(id string) string {
	return filepath.Join(s.dir, fmt.Sprintf("%s.json", id))
}
