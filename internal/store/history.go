package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Ref is a resolved pointer to a commit.
type Ref struct {
	Name   string `json:"name"`
	Full   string `json:"full"`
	SHA    string `json:"sha"`
	Commit string `json:"commit"`
}

// Branches lists every branch in ascending name order.
func (s *Store) Branches() ([]Ref, error) {
	return s.listRefs("refs/heads")
}

// Tags lists every tag in ascending name order.
func (s *Store) Tags() ([]Ref, error) {
	return s.listRefs("refs/tags")
}

// DeleteRef removes a branch or tag.
func (s *Store) DeleteRef(ref string) error {
	if !validRef(ref) {
		return fmt.Errorf("invalid ref %q", ref)
	}
	if err := os.Remove(s.path(filepath.FromSlash(ref))); err != nil {
		return fmt.Errorf("delete ref %q: %w", ref, err)
	}
	return nil
}

// RenameRef moves a branch, keeping its commit. Tags are not renamed.
func (s *Store) RenameRef(from, to string) error {
	if !validRef(from) || !validRef(to) {
		return fmt.Errorf("invalid ref rename %q -> %q", from, to)
	}
	if !strings.HasPrefix(to, "refs/heads/") {
		return fmt.Errorf("only branches can be renamed")
	}
	sha, err := s.GetRef(from)
	if err != nil {
		return err
	}
	if _, err := s.GetRef(to); err == nil {
		return fmt.Errorf("branch %q already exists", strings.TrimPrefix(to, "refs/heads/"))
	}
	if err := s.SetRef(to, sha); err != nil {
		return err
	}
	if ref, _, err := s.HEAD(); err == nil && ref == from {
		if err := s.SetHEAD(to, ""); err != nil {
			return err
		}
	}
	return s.DeleteRef(from)
}

func (s *Store) listRefs(prefix string) ([]Ref, error) {
	root := s.path(filepath.FromSlash(prefix))
	refs := []Ref{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		full := filepath.ToSlash(relative)
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sha := strings.TrimSpace(string(contents))
		if !validHash(sha) {
			return fmt.Errorf("ref %s contains an invalid commit hash", full)
		}
		refs = append(refs, Ref{
			Name:   strings.TrimPrefix(full, prefix+"/"),
			Full:   full,
			SHA:    sha,
			Commit: sha,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", prefix, err)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	return refs, nil
}

// ResolveRef maps a user supplied revision to a commit hash. Accepted forms
// are HEAD, a branch name, a tag name, a fully qualified ref, or a full or
// abbreviated commit hash.
func (s *Store) ResolveRef(revision string) (string, error) {
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return "", fmt.Errorf("empty revision")
	}
	if revision == "HEAD" {
		_, sha, err := s.HEAD()
		if err != nil {
			return "", err
		}
		if sha == "" {
			return "", fmt.Errorf("HEAD does not point at a commit yet")
		}
		return sha, nil
	}
	if strings.HasPrefix(revision, "refs/") {
		sha, err := s.GetRef(revision)
		if err != nil {
			return "", err
		}
		return sha, nil
	}
	if strings.HasPrefix(revision, "heads/") {
		return s.ResolveRef("refs/" + revision)
	}
	if strings.HasPrefix(revision, "tags/") {
		return s.ResolveRef("refs/" + revision)
	}
	candidates := []string{"refs/heads/" + revision, "refs/tags/" + revision}
	for _, candidate := range candidates {
		if sha, err := s.GetRef(candidate); err == nil {
			return sha, nil
		}
	}
	if len(revision) < 7 {
		return "", fmt.Errorf("ambiguous or unknown revision %q", revision)
	}
	if sha, err := s.ReadCommit(revision); err == nil {
		return sha.Hash, nil
	}
	prefixed, err := s.resolveAbbreviated(revision)
	if err != nil {
		return "", fmt.Errorf("unknown revision %q", revision)
	}
	return prefixed, nil
}

func (s *Store) resolveAbbreviated(prefix string) (string, error) {
	root := filepath.Join(s.root, "commits")
	matches := map[string]bool{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		name := strings.TrimSuffix(info.Name(), ".json")
		sha := filepath.Base(filepath.Dir(path)) + name
		if strings.HasPrefix(sha, prefix) {
			matches[sha] = true
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("abbreviated revision %q is ambiguous", prefix)
	}
	for sha := range matches {
		return sha, nil
	}
	return "", fmt.Errorf("abbreviated revision %q is unknown", prefix)
}
