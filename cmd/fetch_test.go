package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTruncateGistID_LongIDs(t *testing.T) {
	assert.Equal(t, "5d44a74e...", truncateGistID("5d44a74eb1dbb6fc02e5b556baf16e0a"))
}

func TestTruncateGistID_ShortIDsUnchanged(t *testing.T) {
	// Anything ≤ 8 chars is returned as-is (no ellipsis, no panic).
	assert.Equal(t, "abc", truncateGistID("abc"))
	assert.Equal(t, "12345678", truncateGistID("12345678"))
}
