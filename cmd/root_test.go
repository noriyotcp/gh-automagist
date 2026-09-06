package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A subcommand registered without a GroupID still shows up in the help, but
// under a stray "Additional Commands:" heading below the four real groups
// instead of inside the one it belongs to.
func TestEveryCommandBelongsToAGroup(t *testing.T) {
	for _, c := range rootCmd.Commands() {
		assert.NotEmpty(t, c.GroupID, "%s has no GroupID", c.Name())
	}
}
