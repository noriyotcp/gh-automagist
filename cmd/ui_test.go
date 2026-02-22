package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHomeDir(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	assert.Equal(t, tempHome, homeDir())
}

func TestIsMonitorRunning(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	sm, err := state.NewManager()
	require.NoError(t, err)

	// Ensure config dir exists for manual PID writing
	configDir := filepath.Join(tempHome, ".config", "gh-automagist")
	err = os.MkdirAll(configDir, 0755)
	require.NoError(t, err)

	// Case 1: No PID file
	assert.False(t, isMonitorRunning())

	// Case 2: PID file exists but process is dead (using a high PID likely not in use)
	pidPath := filepath.Join(configDir, "monitor.pid")
	err = os.WriteFile(pidPath, []byte("999999"), 0644)
	require.NoError(t, err)
	assert.False(t, isMonitorRunning())

	// Case 3: PID file exists and process is alive (using current process PID)
	err = sm.WritePID() // Uses current PID
	require.NoError(t, err)
	assert.True(t, isMonitorRunning())
}
