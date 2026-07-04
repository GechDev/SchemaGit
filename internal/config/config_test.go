package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func write(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return path
}

const minimal = `version: 1
default_branch: main
environments:
  dev:
    url_env: SCHEMAGIT_DEV_URL
  prod:
    url_env: SCHEMAGIT_PROD_URL
    read_only: true
safety:
  block_destructive: true
linked_git:
  enabled: false
`

func TestLoadFullDocument(t *testing.T) {
	path := write(t, t.TempDir(), FileName, minimal)
	config, err := Load(path)
	require.NoError(t, err)
	require.True(t, config.Explicit)
	require.Equal(t, 1, config.Version)
	require.Equal(t, "main", config.DefaultBranch)
	require.True(t, config.BlockDestructive())
	require.False(t, config.LinkedGit.Enabled)
	require.Equal(t, []string{"dev", "prod"}, config.EnvironmentNames())

	prod, err := config.Environment("prod")
	require.NoError(t, err)
	require.True(t, prod.ReadOnly)
	require.Equal(t, "SCHEMAGIT_PROD_URL", prod.URLEnv)
}

func TestURLResolvesFromEnvironmentOnly(t *testing.T) {
	path := write(t, t.TempDir(), FileName, minimal)
	config, err := Load(path)
	require.NoError(t, err)

	t.Setenv("SCHEMAGIT_DEV_URL", "postgres://localhost/schemagit_test")
	environment, url, err := config.URL("dev")
	require.NoError(t, err)
	require.False(t, environment.ReadOnly)
	require.Equal(t, "postgres://localhost/schemagit_test", url)

	_, _, err = config.URL("prod")
	require.Error(t, err)

	_, _, err = config.URL("staging")
	require.Error(t, err)
}

func TestLoadRejectsInlineURLs(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, FileName, "version: 1\ndefault_branch: main\nenvironments:\n  dev:\n    url: postgres://user:pw@host/db\n")
	_, err := Load(path)
	require.Error(t, err)
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, FileName, "version: 1\ndefault_branch: main\nsurprise: true\n")
	_, err := Load(path)
	require.Error(t, err)
}

func TestLoadRejectsBadVersionAndBranch(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(write(t, dir, "a.yaml", "version: 2\ndefault_branch: main\n"))
	require.Error(t, err)

	_, err = Load(write(t, dir, "b.yaml", "version: 1\ndefault_branch: 'bad branch'\n"))
	require.Error(t, err)

	_, err = Load(write(t, dir, "c.yaml", "version: 1\n"))
	require.Error(t, err)

	_, err = Load(write(t, dir, "d.yaml", "version: 1\ndefault_branch: main\nenvironments:\n  dev:\n    url_env: ''\n"))
	require.Error(t, err)

	_, err = Load(write(t, dir, "e.yaml", "version: 1\ndefault_branch: main\nenvironments:\n  dev:\n    url_env: 'lower case'\n"))
	require.Error(t, err)

	_, err = Load(write(t, dir, "f.yaml", "version: 1\ndefault_branch: main\nenvironments:\n  'bad name':\n    url_env: OK\n"))
	require.Error(t, err)

	_, err = Load(write(t, dir, "g.yaml", "version: 1\ndefault_branch: main\nlinked_git:\n  enabled: true\n"))
	require.Error(t, err)
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	require.Error(t, err)
}

func TestFindWalksUpAndFallsBack(t *testing.T) {
	root := t.TempDir()
	write(t, root, FileName, minimal)
	nested := filepath.Join(root, "a", "b")
	require.NoError(t, os.MkdirAll(nested, 0o755))

	config, err := Find(nested)
	require.NoError(t, err)
	require.True(t, config.Explicit)
	require.Equal(t, root, filepath.Dir(config.Path))

	bare := t.TempDir()
	bareNested := filepath.Join(bare, "x")
	require.NoError(t, os.MkdirAll(bareNested, 0o755))
	fallback, err := Find(bareNested)
	require.NoError(t, err)
	require.False(t, fallback.Explicit)
	require.True(t, fallback.BlockDestructive())
	require.Equal(t, "main", fallback.DefaultBranch)
	require.Empty(t, fallback.EnvironmentNames())
}

func TestDefaultHelpers(t *testing.T) {
	config := Default()
	require.True(t, config.BlockDestructive())
	require.False(t, Environment{}.Default())
	_, err := Environment{}.URL()
	require.Error(t, err)
	_, err = Environment{}.URL()
	require.Error(t, err)
	_, err = config.Environment("")
	require.Error(t, err)
	require.True(t, (*Config)(nil).BlockDestructive())
}
