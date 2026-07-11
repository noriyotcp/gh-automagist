package cmd

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLineDiffSummary_Identical(t *testing.T) {
	a := []byte("foo\nbar\nbaz\n")
	added, removed := lineDiffSummary(a, a)
	assert.Equal(t, 0, added)
	assert.Equal(t, 0, removed)
}

func TestLineDiffSummary_PureAdditions(t *testing.T) {
	a := []byte("foo\nbar\n")
	b := []byte("foo\nbar\nbaz\nquux\n")
	added, removed := lineDiffSummary(a, b)
	assert.Equal(t, 2, added)
	assert.Equal(t, 0, removed)
}

func TestLineDiffSummary_PureDeletions(t *testing.T) {
	a := []byte("foo\nbar\nbaz\n")
	b := []byte("foo\n")
	added, removed := lineDiffSummary(a, b)
	assert.Equal(t, 0, added)
	assert.Equal(t, 2, removed)
}

func TestLineDiffSummary_EditsCountAsAddPlusRemove(t *testing.T) {
	a := []byte("foo\nbar\nbaz\n")
	b := []byte("foo\nBAR\nbaz\n")
	added, removed := lineDiffSummary(a, b)
	assert.Equal(t, 1, added)
	assert.Equal(t, 1, removed)
}

func TestLineDiffSummary_DuplicateLinesHandledAsMultiset(t *testing.T) {
	a := []byte("x\nx\ny\n")
	b := []byte("x\ny\ny\n")
	added, removed := lineDiffSummary(a, b)
	// x: 2 -> 1 (removed 1); y: 1 -> 2 (added 1)
	assert.Equal(t, 1, added)
	assert.Equal(t, 1, removed)
}

func TestDisplayPath_ReplacesHomeWithTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir available")
	}
	assert.Equal(t, "~/some/where", displayPath(home+"/some/where"))
}

func TestDisplayPath_KeepsPathsOutsideHomeUnchanged(t *testing.T) {
	assert.Equal(t, "/etc/hosts", displayPath("/etc/hosts"))
}

func TestSHA256Hex_Deterministic(t *testing.T) {
	a := sha256Hex([]byte("hello"))
	b := sha256Hex([]byte("hello"))
	assert.Equal(t, a, b)
	assert.Len(t, a, 64, "SHA256 hex should be 64 chars")
}

func TestSHA256Hex_DifferentInputsDiffer(t *testing.T) {
	assert.NotEqual(t, sha256Hex([]byte("hello")), sha256Hex([]byte("world")))
}
