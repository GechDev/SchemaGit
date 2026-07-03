package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LogEntry is a commit as reported by the log command.
type LogEntry struct {
	SHA         string    `json:"sha"`
	Parents     []string  `json:"parents,omitempty"`
	SchemaHash  string    `json:"schema_hash"`
	MigrationID string    `json:"migration_id,omitempty"`
	Author      string    `json:"author"`
	Timestamp   time.Time `json:"timestamp"`
	Message     string    `json:"message"`
	RiskLevel   string    `json:"risk_level,omitempty"`
	Ref         string    `json:"ref,omitempty"`
}

// Log walks the history reachable from start, newest commit first.
func (s *Store) Log(start string, limit int) ([]LogEntry, error) {
	if start == "" {
		return nil, nil
	}
	names := map[string]string{}
	branches, err := s.Branches()
	if err != nil {
		return nil, err
	}
	for _, branch := range branches {
		names[branch.SHA] = "refs/heads/" + branch.Name
	}
	tags, err := s.Tags()
	if err != nil {
		return nil, err
	}
	for _, tag := range tags {
		if _, exists := names[tag.SHA]; !exists {
			names[tag.SHA] = "refs/tags/" + tag.Name
		}
	}
	entries := []LogEntry{}
	seen := map[string]bool{}
	queue := []string{start}
	for len(queue) > 0 && (limit <= 0 || len(entries) < limit) {
		sha := queue[0]
		queue = queue[1:]
		if seen[sha] {
			continue
		}
		seen[sha] = true
		commit, err := s.ReadCommit(sha)
		if err != nil {
			return nil, err
		}
		entries = append(entries, LogEntry{
			SHA: sha, Parents: commit.Parents, SchemaHash: commit.SchemaHash,
			MigrationID: commit.MigrationID, Author: commit.Author,
			Timestamp: commit.Timestamp, Message: commit.Message,
			RiskLevel: commit.RiskLevel, Ref: names[sha],
		})
		queue = append(queue, commit.Parents...)
	}
	return entries, nil
}

// IsAncestor reports whether candidate is reachable from start.
func (s *Store) IsAncestor(candidate, start string) (bool, error) {
	if candidate == start {
		return true, nil
	}
	entries, err := s.Log(start, 0)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.SHA == candidate {
			return true, nil
		}
	}
	return false, nil
}

// MergeBase returns the common ancestor of two commits. Both histories are
// walked newest first and the first commit reachable from both is returned,
// which is the merge base for the linear and single-merge histories SchemaGit
// produces.
func (s *Store) MergeBase(left, right string) (string, error) {
	leftEntries, err := s.Log(left, 0)
	if err != nil {
		return "", err
	}
	rightEntries, err := s.Log(right, 0)
	if err != nil {
		return "", err
	}
	rightHashes := map[string]bool{}
	for _, entry := range rightEntries {
		rightHashes[entry.SHA] = true
	}
	for _, entry := range leftEntries {
		if rightHashes[entry.SHA] {
			return entry.SHA, nil
		}
	}
	return "", fmt.Errorf("no common ancestor between %s and %s", left, right)
}

// AncestorNames lists every commit reachable from start.
func (s *Store) AncestorNames(start string) (map[string]bool, error) {
	entries, err := s.Log(start, 0)
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(entries))
	for _, entry := range entries {
		names[entry.SHA] = true
	}
	return names, nil
}

// MergeState records an in-progress merge so it can be continued or aborted.
type MergeState struct {
	TargetRef  string    `json:"target_ref"`
	TargetSHA  string    `json:"target_sha"`
	BaseSHA    string    `json:"base_sha,omitempty"`
	HeadSHA    string    `json:"head_sha"`
	Strategy   string    `json:"strategy,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	Conflicts  []string  `json:"conflicts,omitempty"`
	Resolution string    `json:"resolution,omitempty"`
}

const mergeStateFile = "MERGE_HEAD"

// BeginMerge records merge state after the pre-merge HEAD has been captured.
func (s *Store) BeginMerge(state *MergeState) error {
	if state == nil || state.TargetSHA == "" {
		return fmt.Errorf("merge state is incomplete")
	}
	previous, err := s.MergeState()
	if err != nil {
		return err
	}
	if previous != nil {
		return fmt.Errorf("a merge is already in progress; run `schemagit merge --abort` first")
	}
	encoded, err := marshalIndent(state)
	if err != nil {
		return err
	}
	return atomicWrite(s.path(mergeStateFile), encoded)
}

// UpdateMergeState replaces the recorded merge state.
func (s *Store) UpdateMergeState(state *MergeState) error {
	encoded, err := marshalIndent(state)
	if err != nil {
		return err
	}
	return atomicWrite(s.path(mergeStateFile), encoded)
}

// MergeState returns the recorded merge state, or nil when no merge is pending.
func (s *Store) MergeState() (*MergeState, error) {
	data, err := os.ReadFile(s.path(mergeStateFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read merge state: %w", err)
	}
	state := &MergeState{}
	if err := unmarshalStrict(data, state); err != nil {
		return nil, fmt.Errorf("decode merge state: %w", err)
	}
	return state, nil
}

// ClearMerge removes any recorded merge state.
func (s *Store) ClearMerge() error {
	if err := os.Remove(s.path(mergeStateFile)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear merge state: %w", err)
	}
	return nil
}

// Conflict is a persisted merge conflict for a single object.
type Conflict struct {
	ObjectID string   `json:"object_id"`
	Kind     string   `json:"kind"`
	Base     string   `json:"base,omitempty"`
	Ours     string   `json:"ours,omitempty"`
	Theirs   string   `json:"theirs,omitempty"`
	Fields   []string `json:"fields,omitempty"`
	Message  string   `json:"message"`
}

// WriteConflicts stores one JSON document per conflicting object.
func (s *Store) WriteConflicts(conflicts []Conflict) error {
	if err := s.ClearConflicts(); err != nil {
		return err
	}
	for _, conflict := range conflicts {
		name := sanitizeObjectID(conflict.ObjectID)
		encoded, err := marshalIndent(&conflict)
		if err != nil {
			return err
		}
		if err := atomicWrite(s.path("conflicts", name+".json"), encoded); err != nil {
			return fmt.Errorf("write conflict %s: %w", conflict.ObjectID, err)
		}
	}
	return nil
}

// ReadConflicts returns every stored conflict, ordered by object id.
func (s *Store) ReadConflicts() ([]Conflict, error) {
	root := filepath.Join(s.root, "conflicts")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list conflicts: %w", err)
	}
	conflicts := make([]Conflict, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read conflict: %w", err)
		}
		conflict := Conflict{}
		if err := unmarshalStrict(data, &conflict); err != nil {
			return nil, fmt.Errorf("decode conflict: %w", err)
		}
		conflicts = append(conflicts, conflict)
	}
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].ObjectID < conflicts[j].ObjectID })
	return conflicts, nil
}

// ClearConflicts removes every stored conflict document.
func (s *Store) ClearConflicts() error {
	root := filepath.Join(s.root, "conflicts")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("list conflicts: %w", err)
	}
	for _, entry := range entries {
		if err := os.Remove(filepath.Join(root, entry.Name())); err != nil {
			return fmt.Errorf("clear conflict %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func sanitizeObjectID(objectID string) string {
	replacer := strings.NewReplacer(":", "_", "/", "_", "\\", "_", "..", "_", " ", "_")
	cleaned := replacer.Replace(objectID)
	if cleaned == "" {
		return "unknown"
	}
	return cleaned
}
