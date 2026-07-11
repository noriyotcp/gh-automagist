package state

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestEnv hijacks the HOME directory so tests write to a temporary location
// instead of the user's actual ~/.config/gh-automagist/state.json.
func setupTestEnv(t *testing.T) string {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	return tempHome
}

func TestManager_NewManager(t *testing.T) {
	tempHome := setupTestEnv(t)
	m, err := NewManager()
	require.NoError(t, err)

	expectedConfigDir := filepath.Join(tempHome, ".config", "gh-automagist")
	assert.Equal(t, expectedConfigDir, m.configDir)
	assert.Equal(t, filepath.Join(expectedConfigDir, "state.json"), m.statePath)
	assert.NotNil(t, m.Files)
}

func TestManager_LoadAndSaveParity(t *testing.T) {
	_ = setupTestEnv(t)
	m, err := NewManager()
	require.NoError(t, err)

	// Step 1: File doesn't exist, Load should not error
	err = m.Load()
	require.NoError(t, err)
	assert.Empty(t, m.Files)

	// Step 2: Add mock data mimicking Ruby structure
	currentTime := time.Now().Unix()
	mockPath := "/Users/test/workspace/file.txt"
	m.AddTrackedFile(mockPath, "abcdef123456", currentTime)

	// Step 3: Save to disk
	err = m.Save()
	require.NoError(t, err)

	// Ensure the physical file exists
	_, statErr := os.Stat(m.statePath)
	require.NoError(t, statErr)

	// Step 4: Create a completely new manager to simulate a fresh CLI run
	newManager, _ := NewManager()
	err = newManager.Load()
	require.NoError(t, err)

	require.Contains(t, newManager.Files, mockPath)
	loadedState := newManager.Files[mockPath]

	assert.Equal(t, "abcdef123456", loadedState.GistID)
	assert.Equal(t, currentTime, loadedState.UpdatedAt)
	assert.Equal(t, "active", loadedState.Status)
}

func TestKillMonitor_StalePID(t *testing.T) {
	_ = setupTestEnv(t)
	m, err := NewManager()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(m.configDir, 0755))

	// PID that is virtually guaranteed not to exist.
	stalePID := 99999999
	require.NoError(t, os.WriteFile(m.pidPath, []byte(fmt.Sprintf("%d", stalePID)), 0644))

	killed, err := m.KillMonitor(stalePID)
	assert.NoError(t, err)
	assert.False(t, killed, "stale PID should not be reported as killed")
	assert.Equal(t, 0, m.GetPID(), "PID file should be cleaned up after stale detection")
}

func TestKillMonitor_LiveProcess(t *testing.T) {
	_ = setupTestEnv(t)
	m, err := NewManager()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(m.configDir, 0755))

	// Spawn a real subprocess we can safely kill.
	cmd := exec.Command("sleep", "30")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Wait() }) // reap zombie

	pid := cmd.Process.Pid
	require.NoError(t, os.WriteFile(m.pidPath, []byte(fmt.Sprintf("%d", pid)), 0644))

	killed, err := m.KillMonitor(pid)
	assert.NoError(t, err)
	assert.True(t, killed, "live process should be reported as killed")
	assert.Equal(t, 0, m.GetPID(), "PID file should be cleaned up after successful kill")
}

func TestManager_RemoveTrackedFile(t *testing.T) {
	_ = setupTestEnv(t)
	m, _ := NewManager()

	m.AddTrackedFile("/fake/path", "gist1", 100)
	assert.Contains(t, m.Files, "/fake/path")

	m.RemoveTrackedFile("/fake/path")
	assert.NotContains(t, m.Files, "/fake/path")
}

func TestManager_Save_NoTempLeaks(t *testing.T) {
	_ = setupTestEnv(t)
	m, err := NewManager()
	require.NoError(t, err)

	m.AddTrackedFile("/some/path", "gist1", 100)
	require.NoError(t, m.Save())

	tmpPath := m.statePath + ".tmp"
	_, err = os.Stat(tmpPath)
	assert.True(t, os.IsNotExist(err), "tmp file should not remain after successful Save")
}

func TestManager_Save_LeavesOriginalIntactOnError(t *testing.T) {
	_ = setupTestEnv(t)
	m, err := NewManager()
	require.NoError(t, err)

	m.AddTrackedFile("/a", "gist_a", 100)
	require.NoError(t, m.Save())
	originalBytes, err := os.ReadFile(m.statePath)
	require.NoError(t, err)

	// Read-only the config directory so the tmp write fails.
	require.NoError(t, os.Chmod(m.configDir, 0555))
	t.Cleanup(func() { _ = os.Chmod(m.configDir, 0755) })

	m.AddTrackedFile("/b", "gist_b", 200)
	saveErr := m.Save()
	assert.Error(t, saveErr, "Save should fail while the config directory is read-only")

	require.NoError(t, os.Chmod(m.configDir, 0755))

	afterBytes, err := os.ReadFile(m.statePath)
	require.NoError(t, err)
	assert.Equal(t, string(originalBytes), string(afterBytes),
		"state.json must be untouched when Save fails partway")
}

func TestFileState_JSONRoundTrip_NewFields(t *testing.T) {
	orig := FileState{
		GistID:          "abc",
		UpdatedAt:       100,
		Status:          "active",
		RemoteUpdatedAt: 200,
		ContentSHA:      "a1b2c3d4",
	}
	data, err := json.Marshal(orig)
	require.NoError(t, err)

	var back FileState
	require.NoError(t, json.Unmarshal(data, &back))
	assert.Equal(t, orig, back)
}

func TestFileState_JSONBackwardCompat(t *testing.T) {
	// Existing state.json without the new fields must still unmarshal cleanly.
	oldJSON := `{"gist_id":"abc","updated_at":100,"status":"active"}`
	var s FileState
	require.NoError(t, json.Unmarshal([]byte(oldJSON), &s))
	assert.Equal(t, "abc", s.GistID)
	assert.Equal(t, int64(100), s.UpdatedAt)
	assert.Equal(t, "active", s.Status)
	assert.Equal(t, int64(0), s.RemoteUpdatedAt)
	assert.Equal(t, "", s.ContentSHA)
}
