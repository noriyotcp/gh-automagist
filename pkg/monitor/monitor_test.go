package monitor

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
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

	targetFile := filepath.Join(tempDir, "test_config.txt")
	err = os.WriteFile(targetFile, []byte("initial data"), 0644)
	require.NoError(t, err)

	sm.AddTrackedFile(targetFile, "github_gist_123", time.Now().Unix())
	sm.Save() // persist so watch loop is aware

	// 2. Initialize the Watcher
	w, err := NewWatcher(sm)
	require.NoError(t, err)
	// Shorten the debounce so the test doesn't wait a full second for the sync.
	w.DebounceInterval = 50 * time.Millisecond

	// 3. Setup the callback channel to intercept the change event asynchronously
	changeDetected := make(chan string, 1)
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

func TestWatcher_ScheduleSync_DebouncesRapidCalls(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	sm, err := state.NewManager()
	require.NoError(t, err)

	w, err := NewWatcher(sm)
	require.NoError(t, err)
	w.DebounceInterval = 100 * time.Millisecond

	var count atomic.Int32
	fired := make(chan string, 4)
	w.OnChange = func(absPath, gistID string) {
		count.Add(1)
		fired <- gistID
	}

	const absPath = "/fake/rapid.txt"
	for i := 1; i <= 3; i++ {
		w.scheduleSync(absPath, fmt.Sprintf("gist_v%d", i))
		time.Sleep(20 * time.Millisecond)
	}

	select {
	case gotGistID := <-fired:
		assert.Equal(t, "gist_v3", gotGistID)
	case <-time.After(1 * time.Second):
		t.Fatal("OnChange did not fire within 1s of the final scheduleSync")
	}

	// Give a generous window for any spurious extra fires to appear.
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, int32(1), count.Load(), "expected exactly one OnChange for 3 rapid scheduleSync calls")
}

func TestWatcher_StopFlushesPendingSyncs(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	sm, err := state.NewManager()
	require.NoError(t, err)

	w, err := NewWatcher(sm)
	require.NoError(t, err)
	// Long enough that the timer will NOT fire naturally within the test.
	w.DebounceInterval = 10 * time.Second

	fired := make(chan struct {
		absPath string
		gistID  string
	}, 1)
	w.OnChange = func(absPath, gistID string) {
		fired <- struct {
			absPath string
			gistID  string
		}{absPath, gistID}
	}

	// Start the event loop so Stop() can close(w.done) cleanly.
	go func() { _ = w.Start() }()
	time.Sleep(50 * time.Millisecond)

	w.scheduleSync("/fake/flush.txt", "gist_flush")

	w.Stop()

	select {
	case got := <-fired:
		assert.Equal(t, "/fake/flush.txt", got.absPath)
		assert.Equal(t, "gist_flush", got.gistID)
	case <-time.After(1 * time.Second):
		t.Fatal("Stop() did not flush the pending debounced sync")
	}
}
