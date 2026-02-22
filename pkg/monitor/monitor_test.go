package monitor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatcher_DetectsFileChange(t *testing.T) {
	// 1. Setup mock environment
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir) // Hijack home so state manager writes cleanly

	sm, err := state.NewManager()
	require.NoError(t, err)

	// Create a mock file to monitor
	targetFile := filepath.Join(tempDir, "test_config.txt")
	err = os.WriteFile(targetFile, []byte("initial data"), 0644)
	require.NoError(t, err)

	// Register it in our state manager
	sm.AddTrackedFile(targetFile, "github_gist_123", time.Now().Unix())
	sm.Save() // persist so watch loop is aware

	// 2. Initialize the Watcher
	w, err := NewWatcher(sm)
	require.NoError(t, err)

	// 3. Setup the callback channel to intercept the change event asynchronously
	changeDetected := make(chan string)
	w.OnChange = func(absPath string, gistID string) {
		assert.Equal(t, targetFile, absPath)
		assert.Equal(t, "github_gist_123", gistID)
		changeDetected <- absPath
	}

	// 4. Start watcher in a goroutine
	go func() {
		_ = w.Start()
	}()
	defer w.Stop()

	// Give the watcher a fraction of a second to spin up and hook into the OS kernel
	time.Sleep(100 * time.Millisecond)

	// 5. Trigger the event (Simulate user modifying the file)
	err = os.WriteFile(targetFile, []byte("updated data"), 0644)
	require.NoError(t, err)

	// 6. Assert that the callback fired within a reasonable timeframe (timeout protection)
	select {
	case changedPath := <-changeDetected:
		assert.Equal(t, targetFile, changedPath)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout: Watcher failed to detect file modification within 2 seconds")
	}
}
