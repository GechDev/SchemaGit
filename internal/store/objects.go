package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/schemagit/schemagit/internal/schema"
)

// WriteObject stores a validated schema by its canonical content hash.
func (s *Store) WriteObject(value *schema.Schema) (string, error) {
	if err := schema.Validate(value); err != nil {
		return "", err
	}
	sha := schema.Hash(value)
	data, err := schema.CanonicalJSON(value)
	if err != nil {
		return "", fmt.Errorf("encode schema object: %w", err)
	}
	path := s.objectPath(sha)
	if existing, err := os.ReadFile(path); err == nil {
		if string(existing) != string(data) {
			return "", fmt.Errorf("object hash collision at %s", path)
		}
		return sha, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read existing schema object: %w", err)
	}
	if err := atomicWrite(path, data); err != nil {
		return "", fmt.Errorf("write schema object: %w", err)
	}
	return sha, nil
}

// ReadObject loads and verifies a schema object by its SHA-256 hash.
func (s *Store) ReadObject(sha string) (*schema.Schema, error) {
	if !validHash(sha) {
		return nil, fmt.Errorf("invalid object hash %q", sha)
	}
	data, err := os.ReadFile(s.objectPath(sha))
	if err != nil {
		return nil, fmt.Errorf("read schema object: %w", err)
	}
	var value schema.Schema
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("decode schema object: %w", err)
	}
	if err := schema.Validate(&value); err != nil {
		return nil, fmt.Errorf("validate schema object: %w", err)
	}
	if actual := schema.Hash(&value); actual != sha {
		return nil, fmt.Errorf("schema object hash mismatch: expected %s, got %s", sha, actual)
	}
	return &value, nil
}

func (s *Store) objectPath(sha string) string {
	return filepath.Join(s.path("objects", sha[:2]), sha[2:]+".json")
}
