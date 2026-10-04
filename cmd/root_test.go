package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A subcommand registered without a GroupID still shows up in the help, but
// under a stray "Additional Commands:" heading below the four real groups
// instead of inside the one it belongs to.
func TestEveryCommandBelongsToAGroup(t *testing.T) {
	for _, c := range rootCmd.Commands() {
		assert.NotEmpty(t, c.GroupID, "%s has no GroupID", c.Name())
	}
}

// Cobra prints a failing command's error before Execute returns, so anything
// run adds on top of that reaches the user twice. Both streams go to one
// buffer because cobra picks between them by accessor, not by severity.
func TestAFailureIsReportedOnce(t *testing.T) {
	var sink bytes.Buffer

	// `remove` takes exactly one argument, so this fails in argument parsing
	// without touching state.json or the network.
	code := run([]string{"gh-automagist", "remove"}, &sink, &sink)

	require.Equal(t, 1, code)
	out := sink.String()
	require.Contains(t, out, "accepts 1 arg(s)")
	assert.Equal(t, 1, strings.Count(out, "accepts 1 arg(s), received 0"),
		"the error reached the user more than once:\n%s", out)
}

func TestGhArgsNamesTheExtension(t *testing.T) {
	assert.Equal(t, []string{"automagist"}, ghArgs([]string{"gh-automagist"}))
	assert.Equal(t, []string{"automagist", "status"}, ghArgs([]string{"gh-automagist", "status"}))
	assert.Equal(t, []string{"automagist"}, ghArgs(nil))
}
