package cmd

import (
	"testing"

	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/stretchr/testify/assert"
)

func TestRecordSyncSuccess_StampsAllThreeSyncFacts(t *testing.T) {
	fs := state.FileState{
		GistID:          "g1",
		Status:          "active",
		UpdatedAt:       100,
		RemoteUpdatedAt: 200,
		ContentSHA:      "old",
	}

	got := recordSyncSuccess(fs, "new", 900, 800)

	assert.Equal(t, int64(800), got.UpdatedAt, "UpdatedAt is the moment the sync succeeded")
	assert.Equal(t, int64(900), got.RemoteUpdatedAt, "our own PATCH is the newest remote observation")
	assert.Equal(t, "new", got.ContentSHA, "digest of the bytes we pushed")
	assert.Equal(t, "g1", got.GistID, "unrelated fields survive")
	assert.Equal(t, "active", got.Status)
}

func TestRecordSyncSuccess_LeavesPullSuppressionUntouched(t *testing.T) {
	// A push that lands inside a pull's suppression window must not clear the
	// marker; only ShouldSuppress's own path owns that field.
	fs := state.FileState{GistID: "g1", PullSuppressUntil: 4242}

	got := recordSyncSuccess(fs, "sha", 900, 800)

	assert.Equal(t, int64(4242), got.PullSuppressUntil)
}
