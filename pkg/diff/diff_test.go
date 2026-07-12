package diff

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func TestUnified_Identical(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "hello\nworld\n")
	b := writeTemp(t, dir, "b.txt", "hello\nworld\n")

	out, err := Unified("", a, b, ColorNever)
	require.NoError(t, err)
	assert.Empty(t, out, "identical files should produce empty diff")
}

func TestUnified_Different(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "hello\nworld\n")
	b := writeTemp(t, dir, "b.txt", "hello\nworld!\n")

	out, err := Unified("", a, b, ColorNever)
	require.NoError(t, err)
	require.NotEmpty(t, out, "different files should produce a diff")
	assert.Contains(t, string(out), "-world", "diff should show the removed line")
	assert.Contains(t, string(out), "+world!", "diff should show the added line")
}

func TestUnified_NonexistentPath_SurfacesError(t *testing.T) {
	// `git diff --no-index` exits 1 for both "files differ" and "cannot open"
	// — Unified disambiguates via empty-stdout + non-empty-stderr.
	dir := t.TempDir()
	a := filepath.Join(dir, "does-not-exist.txt")
	b := writeTemp(t, dir, "b.txt", "hello world\n")

	_, err := Unified("", a, b, ColorNever)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Could not access")
}

func TestUnified_ColorAlways_EmitsANSI(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "hello\nworld\n")
	b := writeTemp(t, dir, "b.txt", "hello\nworld!\n")

	out, err := Unified("", a, b, ColorAlways)
	require.NoError(t, err)
	assert.Contains(t, string(out), "\x1b[", "ColorAlways should emit ANSI escape sequences")
}
