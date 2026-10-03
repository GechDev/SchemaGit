package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SetRef atomically points a fully qualified head or tag ref at a commit.
func (s *Store) SetRef(ref, sha string) error {
	if !validRef(ref) {
		return fmt.Errorf("invalid ref %q", ref)
	}
	if !validHash(sha) {
		return fmt.Errorf("invalid commit hash %q", sha)
	}
	if _, err := s.ReadCommit(sha); err != nil {
		return fmt.Errorf("ref target is not a valid commit: %w", err)
	}
	return atomicWrite(s.path(filepath.FromSlash(ref)), []byte(sha+"\n"))
}

// GetRef reads a fully qualified head or tag ref.
func (s *Store) GetRef(ref string) (string, error) {
	if !validRef(ref) {
		return "", fmt.Errorf("invalid ref %q", ref)
	}
	data, err := os.ReadFile(s.path(filepath.FromSlash(ref)))
	if err != nil {
		return "", fmt.Errorf("read ref %q: %w", ref, err)
	}
	sha := strings.TrimSpace(string(data))
	if !validHash(sha) {
		return "", fmt.Errorf("ref %q contains an invalid commit hash", ref)
	}
	return sha, nil
}

// SetHEAD updates HEAD to a symbolic ref or detached commit hash.
func (s *Store) SetHEAD(ref, sha string) error {
	if ref != "" {
		if !validRef(ref) {
			return fmt.Errorf("invalid HEAD ref %q", ref)
		}
		return atomicWrite(s.path("HEAD"), []byte("ref: "+ref+"\n"))
	}
	if !validHash(sha) {
		return fmt.Errorf("invalid detached HEAD hash %q", sha)
	}
	if _, err := s.ReadCommit(sha); err != nil {
		return fmt.Errorf("detached HEAD target is not a valid commit: %w", err)
	}
	return atomicWrite(s.path("HEAD"), []byte(sha+"\n"))
}

// HEAD resolves the current symbolic ref and its commit hash, if present.
func (s *Store) HEAD() (ref string, sha string, err error) {
	data, err := os.ReadFile(s.path("HEAD"))
	if err != nil {
		return "", "", fmt.Errorf("read HEAD: %w", err)
	}
	value := strings.TrimSpace(string(data))
	if strings.HasPrefix(value, "ref: ") {
		ref = strings.TrimPrefix(value, "ref: ")
		if !validRef(ref) {
			return "", "", fmt.Errorf("HEAD contains an invalid ref %q", ref)
		}
		sha, err = s.GetRef(ref)
		if os.IsNotExist(err) {
			return ref, "", nil
		}
		return ref, sha, err
	}
	if !validHash(value) {
		return "", "", fmt.Errorf("HEAD contains an invalid commit hash")
	}
	return "", value, nil
}

func validRef(ref string) bool {
	if !strings.HasPrefix(ref, "refs/heads/") && !strings.HasPrefix(ref, "refs/tags/") {
		return false
	}
	if strings.Contains(ref, "\\") || strings.Contains(ref, "..") || strings.Contains(ref, "//") {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(ref)))
	return clean == ref && !strings.HasSuffix(ref, "/")
}

func validHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
