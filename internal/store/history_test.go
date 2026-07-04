package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/schemagit/schemagit/internal/schema"
	"github.com/stretchr/testify/require"
)

// helper builds a repository with one schema object and a chain of commits.
type fixture struct {
	t       *testing.T
	store   *Store
	schema  string
	commits []string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, Init(root))
	repository, err := Open(root)
	require.NoError(t, err)
	value := &schema.Schema{Namespaces: []schema.Namespace{{ID: "ns:public", Name: "public"}}}
	objectHash, err := repository.WriteObject(value)
	require.NoError(t, err)
	return &fixture{t: t, store: repository, schema: objectHash}
}

func (f *fixture) commit(message string, parents []string) string {
	f.t.Helper()
	sha, err := f.store.WriteCommit(&Commit{
		SchemaHash: f.schema,
		Parents:    parents,
		Author:     "tester",
		Timestamp:  time.Date(2026, time.July, 2, 9, 0, 0, 0, time.UTC),
		Message:    message,
		RiskLevel:  "low",
	})
	require.NoError(f.t, err)
	f.commits = append(f.commits, sha)
	return sha
}

func TestConfigRoundTrip(t *testing.T) {
	built := newFixture(t)
	config, err := built.store.ReadConfig()
	require.NoError(t, err)
	require.Equal(t, 1, config.Version)
	require.Equal(t, "main", config.DefaultBranch)
	require.NotEmpty(t, built.store.MetadataDir())
}

func TestReadConfigRejectsCorruption(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Init(root))
	repository, err := Open(root)
	require.NoError(t, err)
	require.NoError(t, writeFile(t, repository.path("config.json"), `{"version": 9, "default_branch": "main"}`))
	_, err = repository.ReadConfig()
	require.Error(t, err)

	require.NoError(t, writeFile(t, repository.path("config.json"), `{"version": 1, "default_branch": "main", "extra": true}`))
	_, err = repository.ReadConfig()
	require.Error(t, err)

	require.NoError(t, writeFile(t, repository.path("config.json"), `{"version": 1}`))
	_, err = repository.ReadConfig()
	require.Error(t, err)
}

func TestMigrationRoundTrip(t *testing.T) {
	built := newFixture(t)
	id := built.schema
	written, err := built.store.WriteMigration(id, "CREATE TABLE t (a int);\n", "DROP TABLE t;\n")
	require.NoError(t, err)
	require.NotEmpty(t, written.Checksum)

	loaded, err := built.store.ReadMigration(id)
	require.NoError(t, err)
	require.Equal(t, written, loaded)

	ids, err := built.store.ListMigrations()
	require.NoError(t, err)
	require.Equal(t, []string{id}, ids)

	_, err = built.store.ReadMigration("not-a-migration")
	require.Error(t, err)
	_, err = built.store.WriteMigration("short", "a", "b")
	require.Error(t, err)
}

func TestBranchesTagsAndResolveRef(t *testing.T) {
	built := newFixture(t)
	first := built.commit("first", nil)
	require.NoError(t, built.store.SetRef("refs/heads/main", first))
	require.NoError(t, built.store.SetRef("refs/heads/feature/x", first))
	require.NoError(t, built.store.SetRef("refs/tags/v1", first))

	branches, err := built.store.Branches()
	require.NoError(t, err)
	require.Len(t, branches, 2)
	require.Equal(t, "feature/x", branches[0].Name)
	require.Equal(t, "main", branches[1].Name)

	tags, err := built.store.Tags()
	require.NoError(t, err)
	require.Len(t, tags, 1)
	require.Equal(t, "v1", tags[0].Name)

	for _, revision := range []string{"HEAD", "main", "refs/heads/main", "heads/main", "v1", "tags/v1", "refs/tags/v1", first, first[:10]} {
		resolved, err := built.store.ResolveRef(revision)
		require.NoError(t, err, revision)
		require.Equal(t, first, resolved, revision)
	}

	for _, revision := range []string{"", "nope", "abc", "not-a-hash-but-long-enough"} {
		_, err := built.store.ResolveRef(revision)
		require.Error(t, err, revision)
	}
}

func TestRenameAndDeleteRef(t *testing.T) {
	built := newFixture(t)
	first := built.commit("first", nil)
	require.NoError(t, built.store.SetRef("refs/heads/main", first))
	require.NoError(t, built.store.SetRef("refs/heads/old", first))

	require.NoError(t, built.store.RenameRef("refs/heads/old", "refs/heads/new"))
	_, err := built.store.GetRef("refs/heads/old")
	require.Error(t, err)
	renamed, err := built.store.GetRef("refs/heads/new")
	require.NoError(t, err)
	require.Equal(t, first, renamed)

	require.Error(t, built.store.RenameRef("refs/heads/main", "refs/heads/new"))
	require.Error(t, built.store.RenameRef("refs/heads/main", "refs/tags/new"))
	require.Error(t, built.store.RenameRef("refs/heads/missing", "refs/heads/other"))
	require.Error(t, built.store.RenameRef("bogus", "refs/heads/other"))

	require.NoError(t, built.store.DeleteRef("refs/heads/main"))
	_, err = built.store.GetRef("refs/heads/main")
	require.Error(t, err)
	require.Error(t, built.store.DeleteRef("refs/heads/main"))
}

func TestRenameRefMovesHEAD(t *testing.T) {
	built := newFixture(t)
	first := built.commit("first", nil)
	require.NoError(t, built.store.SetRef("refs/heads/main", first))
	require.NoError(t, built.store.SetHEAD("refs/heads/main", ""))
	require.NoError(t, built.store.RenameRef("refs/heads/main", "refs/heads/trunk"))
	moved, err := built.store.GetRef("refs/heads/trunk")
	require.NoError(t, err)
	require.Equal(t, first, moved)
	ref, _, err := built.store.HEAD()
	require.NoError(t, err)
	require.Equal(t, "refs/heads/trunk", ref)
}

func TestLogMergeBaseAndAncestry(t *testing.T) {
	built := newFixture(t)
	first := built.commit("first", nil)
	require.NoError(t, built.store.SetRef("refs/heads/main", first))
	second := built.commit("second", []string{first})
	require.NoError(t, built.store.SetRef("refs/heads/main", second))
	require.NoError(t, built.store.SetRef("refs/heads/side", first))
	merge := built.commit("merge", []string{second, first})
	require.NoError(t, built.store.SetRef("refs/heads/merged", merge))

	entries, err := built.store.Log(second, 0)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, second, entries[0].SHA)
	require.Equal(t, "refs/heads/main", entries[0].Ref)

	limited, err := built.store.Log(merge, 1)
	require.NoError(t, err)
	require.Len(t, limited, 1)

	empty, err := built.store.Log("", 0)
	require.NoError(t, err)
	require.Empty(t, empty)

	base, err := built.store.MergeBase(second, first)
	require.NoError(t, err)
	require.Equal(t, first, base)

	_, err = built.store.MergeBase(first, second)
	require.NoError(t, err)

	ancestor, err := built.store.IsAncestor(first, merge)
	require.NoError(t, err)
	require.True(t, ancestor)

	ancestor, err = built.store.IsAncestor(merge, first)
	require.NoError(t, err)
	require.False(t, ancestor)

	names, err := built.store.AncestorNames(merge)
	require.NoError(t, err)
	require.Len(t, names, 3)

	_, err = built.store.Log("f", 0)
	require.Error(t, err)
}

func TestMergeBaseRequiresSharedHistory(t *testing.T) {
	built := newFixture(t)
	first := built.commit("first", nil)
	require.NoError(t, built.store.SetRef("refs/heads/main", first))
	orphan := built.commit("orphan", nil)
	_, err := built.store.MergeBase(first, orphan)
	require.Error(t, err)
}

func TestMergeStateLifecycle(t *testing.T) {
	built := newFixture(t)
	first := built.commit("first", nil)
	require.NoError(t, built.store.SetRef("refs/heads/main", first))

	state, err := built.store.MergeState()
	require.NoError(t, err)
	require.Nil(t, state)

	require.Error(t, built.store.BeginMerge(nil))
	require.Error(t, built.store.BeginMerge(&MergeState{}))
	require.NoError(t, built.store.BeginMerge(&MergeState{
		TargetRef: "refs/heads/side", TargetSHA: first, BaseSHA: first, HeadSHA: first,
		StartedAt: time.Date(2026, time.July, 3, 10, 0, 0, 0, time.UTC),
	}))
	require.Error(t, built.store.BeginMerge(&MergeState{TargetSHA: first}))

	state, err = built.store.MergeState()
	require.NoError(t, err)
	require.Equal(t, "refs/heads/side", state.TargetRef)

	state.Strategy = "ours"
	state.Conflicts = []string{"tbl:public.users"}
	require.NoError(t, built.store.UpdateMergeState(state))
	reloaded, err := built.store.MergeState()
	require.NoError(t, err)
	require.Equal(t, "ours", reloaded.Strategy)
	require.Equal(t, []string{"tbl:public.users"}, reloaded.Conflicts)

	require.NoError(t, built.store.ClearMerge())
	state, err = built.store.MergeState()
	require.NoError(t, err)
	require.Nil(t, state)
	require.NoError(t, built.store.ClearMerge())
}

func TestConflictStorage(t *testing.T) {
	built := newFixture(t)
	conflicts, err := built.store.ReadConflicts()
	require.NoError(t, err)
	require.Empty(t, conflicts)

	require.NoError(t, built.store.WriteConflicts([]Conflict{
		{ObjectID: "tbl:public.users", Kind: "modify_delete", Ours: "keep", Message: "conflicting"},
		{ObjectID: "col:public.orders.total", Kind: "field", Message: "field conflict", Fields: []string{"type"}},
	}))
	stored, err := built.store.ReadConflicts()
	require.NoError(t, err)
	require.Len(t, stored, 2)
	require.Equal(t, "col:public.orders.total", stored[0].ObjectID)
	require.Equal(t, "tbl:public.users", stored[1].ObjectID)

	require.NoError(t, built.store.WriteConflicts(nil))
	stored, err = built.store.ReadConflicts()
	require.NoError(t, err)
	require.Empty(t, stored)
	require.Equal(t, "tbl_public_users", sanitizeObjectID("tbl:public/users"))
	require.Equal(t, "unknown", sanitizeObjectID(""))
}

func TestReadConflictsRejectsCorruption(t *testing.T) {
	built := newFixture(t)
	require.NoError(t, writeFile(t, built.store.path("conflicts", "bad.json"), `{"nope": true}`))
	_, err := built.store.ReadConflicts()
	require.Error(t, err)
	require.NoError(t, os.Remove(built.store.path("conflicts", "bad.json")))
	require.NoError(t, writeFile(t, built.store.path("conflicts", "ignored.txt"), "x"))
	_, err = built.store.ReadConflicts()
	require.NoError(t, err)
}

func TestObjectHashMismatchIsDetected(t *testing.T) {
	built := newFixture(t)
	require.NoError(t, writeFile(t, built.store.objectPath(built.schema),
		`{"namespaces":[{"id":"ns:public","name":"other"}]}`))
	_, err := built.store.ReadObject(built.schema)
	require.Error(t, err)
	_, err = built.store.ReadObject("nope")
	require.Error(t, err)
}

func TestCommitValidation(t *testing.T) {
	built := newFixture(t)
	_, err := built.store.WriteCommit(nil)
	require.Error(t, err)
	_, err = built.store.WriteCommit(&Commit{SchemaHash: "short"})
	require.Error(t, err)
	_, err = built.store.WriteCommit(&Commit{SchemaHash: built.schema, Parents: []string{"short"}})
	require.Error(t, err)
	_, err = built.store.ReadCommit("short")
	require.Error(t, err)
}

func writeFile(t *testing.T, path, contents string) error {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(contents), 0o600)
}
