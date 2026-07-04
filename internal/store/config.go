package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Config is the on-disk repository configuration.
type Config struct {
	Version       int    `json:"version"`
	DefaultBranch string `json:"default_branch"`
}

// marshalIndent encodes v as stable, newline terminated JSON.
func marshalIndent(v any) ([]byte, error) {
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return nil, fmt.Errorf("encode json: %w", err)
	}
	return buffer.Bytes(), nil
}

// unmarshalStrict decodes JSON and rejects unknown fields so a corrupted or
// hand-edited metadata file fails closed instead of being silently ignored.
func unmarshalStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("unexpected trailing JSON content")
	}
	return nil
}

// ReadConfig loads the repository configuration.
func (s *Store) ReadConfig() (*Config, error) {
	data, err := os.ReadFile(s.path("config.json"))
	if err != nil {
		return nil, fmt.Errorf("read repository config: %w", err)
	}
	config := &Config{}
	if err := unmarshalStrict(data, config); err != nil {
		return nil, fmt.Errorf("decode repository config: %w", err)
	}
	if config.Version != 1 {
		return nil, fmt.Errorf("unsupported repository version %d", config.Version)
	}
	if config.DefaultBranch == "" {
		return nil, fmt.Errorf("repository config has no default branch")
	}
	return config, nil
}

// MetadataDir returns the absolute path of the .schemagit directory.
func (s *Store) MetadataDir() string {
	absolute, err := filepath.Abs(s.root)
	if err != nil {
		return s.root
	}
	return absolute
}
