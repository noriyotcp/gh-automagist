package cmd

import (
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestMonitorLogPath_OneFilePerStartMatchingRubyLayout(t *testing.T) {
	start := time.Date(2026, 8, 23, 15, 1, 19, 0, time.UTC)

	got := monitorLogPath("/cfg/log", start)

	assert.Equal(t, "/cfg/log/monitor_20260823_150119.log", got)
}

func TestOpenMonitorLog_CreatesDirectoryAndCapturesLogOutput(t *testing.T) {
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	path := filepath.Join(t.TempDir(), "log", "monitor_20260823_150119.log")

	f, err := openMonitorLog(path)
	require.NoError(t, err)
	log.Printf("reconcile: 1 pushed")
	require.NoError(t, f.Close())

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(content), "reconcile: 1 pushed")
}

func TestOpenMonitorLog_AppendsInsteadOfTruncating(t *testing.T) {
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	path := filepath.Join(t.TempDir(), "monitor.log")
	require.NoError(t, os.WriteFile(path, []byte("earlier run\n"), 0o644))

	f, err := openMonitorLog(path)
	require.NoError(t, err)
	log.Printf("later run")
	require.NoError(t, f.Close())

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(content), "earlier run")
	assert.Contains(t, string(content), "later run")
}
