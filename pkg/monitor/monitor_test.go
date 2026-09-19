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

// A file registered by `add` while the daemon is up lives in a directory that
// was not in the watch set at startup, so nothing about that file would ever
// reach the event loop. The daemon hears about it through state.json instead.
func TestWatcher_PicksUpFileAddedWhileRunning(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	existingDir := filepath.Join(tempDir, "existing")
	require.NoError(t, os.MkdirAll(existingDir, 0755))
	existingFile := filepath.Join(existingDir, "old.txt")
	require.NoError(t, os.WriteFile(existingFile, []byte("initial"), 0644))

	sm, err := state.NewManager()
	require.NoError(t, err)
	sm.AddTrackedFile(existingFile, "gist_old", time.Now().Unix())
	require.NoError(t, sm.Save())

	w, err := NewWatcher(sm)
	require.NoError(t, err)
	w.DebounceInterval = 50 * time.Millisecond

	fired := make(chan string, 4)
	w.OnChange = func(absPath, gistID string) { fired <- absPath }

	go func() { _ = w.Start() }()
	defer w.Stop()
	time.Sleep(100 * time.Millisecond)

	// Stand in for `gh automagist add`: a separate Manager over the same HOME
	// writes the new entry, exactly as another process would.
	newDir := filepath.Join(tempDir, "added-later")
	require.NoError(t, os.MkdirAll(newDir, 0755))
	newFile := filepath.Join(newDir, "new.txt")
	require.NoError(t, os.WriteFile(newFile, []byte("initial"), 0644))

	adder, err := state.NewManager()
	require.NoError(t, err)
	require.NoError(t, adder.Load())
	adder.AddTrackedFile(newFile, "gist_new", time.Now().Unix())
	require.NoError(t, adder.Save())

	time.Sleep(200 * time.Millisecond) // let the state.json event land and the watch register

	require.NoError(t, os.WriteFile(newFile, []byte("edited after add"), 0644))

	select {
	case changed := <-fired:
		assert.Equal(t, newFile, changed)
	case <-time.After(2 * time.Second):
		t.Fatal("Watcher did not pick up a file added while it was running")
	}

	// A second add, because every Save() replaces state.json by rename and the
	// daemon saves after each successful PATCH: the registry watch has to
	// survive being re-created, not just the first one.
	secondDir := filepath.Join(tempDir, "added-even-later")
	require.NoError(t, os.MkdirAll(secondDir, 0755))
	secondFile := filepath.Join(secondDir, "second.txt")
	require.NoError(t, os.WriteFile(secondFile, []byte("initial"), 0644))

	adder.AddTrackedFile(secondFile, "gist_second", time.Now().Unix())
	require.NoError(t, adder.Save())

	time.Sleep(200 * time.Millisecond)
	require.NoError(t, os.WriteFile(secondFile, []byte("edited after second add"), 0644))

	select {
	case changed := <-fired:
		assert.Equal(t, secondFile, changed)
	case <-time.After(2 * time.Second):
		t.Fatal("Watcher stopped following state.json after it was replaced once")
	}
}

// The mirror case: `remove` while the daemon is up must stop the syncing, which
// depends on Load() dropping entries that are gone from state.json.
func TestWatcher_DropsFileRemovedWhileRunning(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	keptDir := filepath.Join(tempDir, "kept")
	droppedDir := filepath.Join(tempDir, "dropped")
	require.NoError(t, os.MkdirAll(keptDir, 0755))
	require.NoError(t, os.MkdirAll(droppedDir, 0755))
	keptFile := filepath.Join(keptDir, "kept.txt")
	droppedFile := filepath.Join(droppedDir, "dropped.txt")
	require.NoError(t, os.WriteFile(keptFile, []byte("initial"), 0644))
	require.NoError(t, os.WriteFile(droppedFile, []byte("initial"), 0644))

	sm, err := state.NewManager()
	require.NoError(t, err)
	sm.AddTrackedFile(keptFile, "gist_kept", time.Now().Unix())
	sm.AddTrackedFile(droppedFile, "gist_dropped", time.Now().Unix())
	require.NoError(t, sm.Save())

	w, err := NewWatcher(sm)
	require.NoError(t, err)
	w.DebounceInterval = 50 * time.Millisecond

	fired := make(chan string, 4)
	w.OnChange = func(absPath, gistID string) { fired <- absPath }

	go func() { _ = w.Start() }()
	defer w.Stop()
	time.Sleep(100 * time.Millisecond)

	remover, err := state.NewManager()
	require.NoError(t, err)
	require.NoError(t, remover.Load())
	remover.RemoveTrackedFile(droppedFile)
	require.NoError(t, remover.Save())

	time.Sleep(200 * time.Millisecond)

	require.NoError(t, os.WriteFile(droppedFile, []byte("edited after remove"), 0644))
	select {
	case changed := <-fired:
		t.Fatalf("OnChange fired for an untracked file: %s", changed)
	case <-time.After(500 * time.Millisecond):
	}

	// Positive control: the same write on a still-tracked file does fire, so
	// the silence above is untracking rather than a dead watcher.
	require.NoError(t, os.WriteFile(keptFile, []byte("edited"), 0644))
	select {
	case changed := <-fired:
		assert.Equal(t, keptFile, changed)
	case <-time.After(2 * time.Second):
		t.Fatal("Watcher stopped reporting the file that is still tracked")
	}
}
