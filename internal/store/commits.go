package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Commit is an immutable reference to a schema snapshot and its migration.
type Commit struct {
	Hash        string    `json:"hash"`
	Parents     []string  `json:"parents,omitempty"`
	SchemaHash  string    `json:"schema_hash"`
	MigrationID string    `json:"migration_id,omitempty"`
	Author      string    `json:"author"`
	Timestamp   time.Time `json:"timestamp"`
	Message     string    `json:"message"`
	RiskLevel   string    `json:"risk_level,omitempty"`
}

type commitPayload struct {
	Parents     []string  `json:"parents,omitempty"`
	SchemaHash  string    `json:"schema_hash"`
	MigrationID string    `json:"migration_id,omitempty"`
	Author      string    `json:"author"`
	Timestamp   time.Time `json:"timestamp"`
	Message     string    `json:"message"`
	RiskLevel   string    `json:"risk_level,omitempty"`
}

// WriteCommit stores a content-addressed commit and assigns its hash to c.
func (s *Store) WriteCommit(c *Commit) (string, error) {
	if c == nil {
		return "", fmt.Errorf("commit is nil")
	}
	if !validHash(c.SchemaHash) {
		return "", fmt.Errorf("invalid schema hash %q", c.SchemaHash)
	}
	for _, parent := range c.Parents {
		if !validHash(parent) {
			return "", fmt.Errorf("invalid parent commit hash %q", parent)
		}
	}
	c.Timestamp = c.Timestamp.UTC()
	payload := commitPayload{
		Parents: c.Parents, SchemaHash: c.SchemaHash, MigrationID: c.MigrationID,
		Author: c.Author, Timestamp: c.Timestamp, Message: c.Message, RiskLevel: c.RiskLevel,
	}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode commit payload: %w", err)
	}
	digest := sha256.Sum256(canonical)
	c.Hash = hex.EncodeToString(digest[:])
	data, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("encode commit: %w", err)
	}
	if err := atomicWrite(s.commitPath(c.Hash), data); err != nil {
		return "", fmt.Errorf("write commit: %w", err)
	}
	return c.Hash, nil
}

// ReadCommit loads and verifies a content-addressed commit.
func (s *Store) ReadCommit(sha string) (*Commit, error) {
	if !validHash(sha) {
		return nil, fmt.Errorf("invalid commit hash %q", sha)
	}
	data, err := os.ReadFile(s.commitPath(sha))
	if err != nil {
		return nil, fmt.Errorf("read commit: %w", err)
	}
	var c Commit
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("decode commit: %w", err)
	}
	if c.Hash != sha {
		return nil, fmt.Errorf("commit hash mismatch: expected %s, got %s", sha, c.Hash)
	}
	payload, err := json.Marshal(commitPayload{
		Parents: c.Parents, SchemaHash: c.SchemaHash, MigrationID: c.MigrationID,
		Author: c.Author, Timestamp: c.Timestamp, Message: c.Message, RiskLevel: c.RiskLevel,
	})
	if err != nil {
		return nil, fmt.Errorf("encode commit payload: %w", err)
	}
	digest := sha256.Sum256(payload)
	if actual := hex.EncodeToString(digest[:]); actual != sha {
		return nil, fmt.Errorf("commit content hash mismatch: expected %s, got %s", sha, actual)
	}
	return &c, nil
}

func (s *Store) commitPath(sha string) string {
	return filepath.Join(s.path("commits", sha[:2]), sha[2:]+".json")
}
