package store

import (
	"testing"
	"time"

	"github.com/schemagit/schemagit/internal/schema"
	"github.com/stretchr/testify/require"
)

func TestInitObjectCommitRefRoundTrip(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Init(root))
	repository, err := Open(root)
	require.NoError(t, err)

	value := &schema.Schema{Namespaces: []schema.Namespace{{ID: "ns:public", Name: "public"}}}
	objectHash, err := repository.WriteObject(value)
	require.NoError(t, err)
	loaded, err := repository.ReadObject(objectHash)
	require.NoError(t, err)
	require.Equal(t, schema.Hash(value), schema.Hash(loaded))
	secondHash, err := repository.WriteObject(value)
	require.NoError(t, err)
	require.Equal(t, objectHash, secondHash)

	commit := &Commit{
		SchemaHash: objectHash, Author: "test", Timestamp: time.Date(2026, time.June, 2, 12, 34, 56, 789, time.UTC), Message: "initial",
	}
	commitHash, err := repository.WriteCommit(commit)
	require.NoError(t, err)
	require.Equal(t, commitHash, commit.Hash)
	loadedCommit, err := repository.ReadCommit(commitHash)
	require.NoError(t, err)
	require.Equal(t, commit, loadedCommit)

	require.NoError(t, repository.SetRef("refs/heads/main", commitHash))
	refHash, err := repository.GetRef("refs/heads/main")
	require.NoError(t, err)
	require.Equal(t, commitHash, refHash)
	headRef, headHash, err := repository.HEAD()
	require.NoError(t, err)
	require.Equal(t, "refs/heads/main", headRef)
	require.Equal(t, commitHash, headHash)
	require.NoError(t, repository.SetHEAD(headRef, ""))
	require.NoError(t, repository.SetHEAD("", commitHash))
	_, headHash, err = repository.HEAD()
	require.NoError(t, err)
	require.Equal(t, commitHash, headHash)
}
