package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPickerOptions(t *testing.T) {
	// os.ReadDir hands entries over already sorted by name, which is why
	// .config sits before Documents here.
	entries := []pickerEntry{
		{name: ".config", isDir: true},
		{name: ".zshrc"},
		{name: "Documents", isDir: true},
		{name: "notes.md"},
	}

	options := pickerOptions(entries)

	var keys, labels []string
	for _, o := range options {
		keys = append(keys, o.Value)
		labels = append(labels, o.Key)
	}

	assert.Equal(t, []string{"Documents", "notes.md", ".config", ".zshrc"}, keys,
		"visible entries first, dot-entries after, each group alphabetical")
	assert.Equal(t, []string{"Documents/", "notes.md", ".config/", ".zshrc"}, labels,
		"directories carry a trailing slash; the returned value stays the bare name")
}

func TestPickerOptions_KeepsDotfilesReachable(t *testing.T) {
	// The whole point: hiding these made ~/.zshrc unselectable from the
	// dashboard, and ~/.config unenterable.
	options := pickerOptions([]pickerEntry{
		{name: ".config", isDir: true},
		{name: ".zshrc"},
	})

	require.Len(t, options, 2)
	assert.Equal(t, ".config", options[0].Value)
	assert.Equal(t, ".zshrc", options[1].Value)
}

func TestPickerOptions_EmptyDirectory(t *testing.T) {
	assert.Empty(t, pickerOptions(nil))
}
