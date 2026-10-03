package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const metadataDir = ".schemagit"

// Store reads and writes a SchemaGit repository under a project root.
type Store struct {
	root string
}

// Init creates an empty repository with main as its initial branch.
func Init(root string) error {
	metadataPath := filepath.Join(root, metadataDir)
	if _, err := os.Stat(metadataPath); err == nil {
		return fmt.Errorf("SchemaGit repository already exists at %s", metadataPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect repository path: %w", err)
	}
	for _, directory := range []string{
		"refs/heads", "refs/tags", "objects", "commits", "migrations", "conflicts",
	} {
		if err := os.MkdirAll(filepath.Join(metadataPath, directory), 0o700); err != nil {
			return fmt.Errorf("create repository directory %s: %w", directory, err)
		}
	}
	config, err := json.Marshal(struct {
		Version       int    `json:"version"`
		DefaultBranch string `json:"default_branch"`
	}{Version: 1, DefaultBranch: "main"})
	if err != nil {
		return fmt.Errorf("encode repository config: %w", err)
	}
	if err := atomicWrite(filepath.Join(metadataPath, "config.json"), config); err != nil {
		return fmt.Errorf("write repository config: %w", err)
	}
	if err := atomicWrite(filepath.Join(metadataPath, "HEAD"), []byte("ref: refs/heads/main\n")); err != nil {
		return fmt.Errorf("write repository HEAD: %w", err)
	}
	return nil
}

// Open opens the repository metadata rooted in root.
func Open(root string) (*Store, error) {
	metadataPath := filepath.Join(root, metadataDir)
	info, err := os.Stat(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("open SchemaGit repository: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("SchemaGit metadata path is not a directory: %s", metadataPath)
	}
	return &Store{root: metadataPath}, nil
}

func (s *Store) path(parts ...string) string {
	return filepath.Join(append([]string{s.root}, parts...)...)
}

func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".schemagit-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
