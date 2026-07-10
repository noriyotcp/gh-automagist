package monitor

import (
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/noriyo_tcp/gh-automagist/pkg/state"
)

// DefaultDebounceInterval is the quiet-window after the last write before OnChange
// fires, so rapid successive writes to the same file collapse into a single sync.
const DefaultDebounceInterval = 1 * time.Second

// Watcher watches the parent directories of tracked files and calls OnChange on write.
type Watcher struct {
	watcher      *fsnotify.Watcher
	stateManager *state.Manager
	OnChange     func(absPath string, gistID string) // Callback when a watched file changes
	done         chan bool

	// DebounceInterval overrides DefaultDebounceInterval; must be set before Start().
	// A zero or negative value disables debouncing.
	DebounceInterval time.Duration

	timersMu sync.Mutex
	timers   map[string]*debounceEntry
}

type debounceEntry struct {
	timer  *time.Timer
	gistID string
}

func NewWatcher(sm *state.Manager) (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	return &Watcher{
		watcher:          w,
		stateManager:     sm,
		done:             make(chan bool),
		DebounceInterval: DefaultDebounceInterval,
		timers:           make(map[string]*debounceEntry),
	}, nil
}

// Start runs the event loop; blocks until Stop().
func (w *Watcher) Start() error {
	// 1. Add all directories containing tracked files to the watcher
	// fsnotify works best by watching the parent directory to catch vim/editor "save by replace" events.
	dirsToWatch := make(map[string]bool)
	for absPath := range w.stateManager.Files {
		dir := filepath.Dir(absPath)
		dirsToWatch[dir] = true
	}

	for dir := range dirsToWatch {
		// When using fsnotify.Add(), macOS FSEvents might attempt to scan the directory.
		// If the directory contains broken symlinks (e.g., dangling dotfiles), it can throw an error like:
		// "no such file or directory". We should catch this but not let it crash the whole monitor.
		// With go's fsnotify, if we add a path ending in `/...`, it watches recursively, but we are just adding `dir`.
		err := w.watcher.Add(dir)
		if err != nil {
			log.Printf("Warning: failed to watch directory cleanly %s: %v", dir, err)
			log.Printf("  -> This is often caused by broken symlinks in the directory. Continuing anyway.")
			// We intentionally do not 'continue' or 'return' here, because fsnotify often still succeeds
			// in watching the valid files in the directory despite throwing an error on the broken symlink.
		} else {
			log.Printf("[gh-automagist] Watching directory: %s", dir)
		}
	}

	// 2. Start the event loop
	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return nil
			}

			// We are only interested in Write or Create events (editors sometimes Create/Rename instead of Write)
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				if fileState, isTracked := w.stateManager.Files[event.Name]; isTracked {
					log.Printf("[Sync] Change detected in %s", filepath.Base(event.Name))

					fileState.UpdatedAt = time.Now().Unix()
					w.stateManager.Files[event.Name] = fileState
					w.stateManager.Save() // Persist immediately

					w.scheduleSync(event.Name, fileState.GistID)
				}
			}

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return nil
			}
			log.Printf("fsnotify error: %v", err)

		case <-w.done:
			log.Println("[gh-automagist] Stopping file monitor...")
			return nil
		}
	}
}

// scheduleSync arms (or resets) the per-file debounce timer. gistID is captured
// in the timer's closure so the AfterFunc callback never touches stateManager.Files
// concurrently with the Start() event loop.
func (w *Watcher) scheduleSync(absPath, gistID string) {
	if w.DebounceInterval <= 0 {
		if w.OnChange != nil {
			w.OnChange(absPath, gistID)
		}
		return
	}

	w.timersMu.Lock()
	defer w.timersMu.Unlock()

	if entry, ok := w.timers[absPath]; ok {
		entry.timer.Stop()
	}
	w.timers[absPath] = &debounceEntry{
		gistID: gistID,
		timer: time.AfterFunc(w.DebounceInterval, func() {
			w.timersMu.Lock()
			delete(w.timers, absPath)
			w.timersMu.Unlock()

			if w.OnChange != nil {
				w.OnChange(absPath, gistID)
			}
		}),
	}
}

// Stop gracefully shuts down the file watcher. Pending debounced syncs are
// flushed synchronously so the final edit is not lost on shutdown.
func (w *Watcher) Stop() {
	close(w.done)
	w.watcher.Close()
	w.flushPendingSyncs()
}

// flushPendingSyncs cancels armed debounce timers and fires OnChange for each.
// Timers whose AfterFunc is already running or enqueued are left alone — the
// callback will invoke OnChange itself.
func (w *Watcher) flushPendingSyncs() {
	w.timersMu.Lock()
	pending := make(map[string]string, len(w.timers))
	for absPath, entry := range w.timers {
		if !entry.timer.Stop() {
			continue // already fired or firing; the AfterFunc callback handles it
		}
		pending[absPath] = entry.gistID
	}
	w.timers = make(map[string]*debounceEntry)
	w.timersMu.Unlock()

	for absPath, gistID := range pending {
		if w.OnChange != nil {
			w.OnChange(absPath, gistID)
		}
	}
}
