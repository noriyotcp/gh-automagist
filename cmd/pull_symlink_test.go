package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The case this exists for: ~/.zshrc is a symlink into a dotfiles repository.
// rename(2) over the link would swap it for a regular file, leaving the
// repository's copy stale and every later edit going nowhere.
func TestWriteAtomic_WritesThroughASymlink(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "dotfiles")
	require.NoError(t, os.Mkdir(repo, 0o755))
	target := filepath.Join(repo, "zshrc")
	require.NoError(t, os.WriteFile(target, []byte("original\n"), 0o644))

	link := filepath.Join(dir, ".zshrc")
	require.NoError(t, os.Symlink(target, link))

	require.NoError(t, writeAtomic(link, []byte("pulled from gist\n"), 0o644))

	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "the link itself must survive the write")

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "pulled from gist\n", string(got), "the write lands on the file the link points at")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 2, "no stray temp file: only .zshrc and dotfiles/")
}

func TestWriteAtomic_RefusesABrokenSymlink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, ".zshrc")
	require.NoError(t, os.Symlink(filepath.Join(dir, "gone"), link))

	require.Error(t, writeAtomic(link, []byte("x\n"), 0o644))

	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "a link we cannot resolve is not ours to replace")
}

func TestWriteAtomic_CreatesAPlainFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.md")

	require.NoError(t, writeAtomic(path, []byte("created\n"), 0o644))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "created\n", string(got))
}
