package monitor

import (
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/noriyo_tcp/gh-automagist/pkg/state"
)

// Watcher wraps fsnotify to specifically track changes to files defined in the local state.
type Watcher struct {
	watcher      *fsnotify.Watcher
	stateManager *state.Manager
	OnChange     func(absPath string, gistID string) // Callback when a watched file changes
	done         chan bool
}

// NewWatcher initializes the file system watcher based on the provided state manager.
func NewWatcher(sm *state.Manager) (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	return &Watcher{
		watcher:      w,
		stateManager: sm,
		done:         make(chan bool),
	}, nil
}

// Start begins processing watcher events in a blocking manner until Stop() is called.
func (w *Watcher) Start() error {
	// 1. Add all directories containing tracked files to the watcher
	// fsnotify works best by watching the parent directory to catch vim/editor "save by replace" events.
	dirsToWatch := make(map[string]bool)
	for absPath := range w.stateManager.Files {
		dir := filepath.Dir(absPath)
		dirsToWatch[dir] = true
	}

	for dir := range dirsToWatch {
		err := w.watcher.Add(dir)
		if err != nil {
			log.Printf("Warning: failed to watch directory %s: %v", dir, err)
			continue
		}
		log.Printf("[gh-automagist] Watching directory: %s", dir)
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
				// Check if the modified file is actually one of our strictly tracked files
				if fileState, isTracked := w.stateManager.Files[event.Name]; isTracked {
					// To prevent event spamming (compilers/editors writing multiple times super fast),
					// we enforce a basic debounce/throttle. We only trigger if it's been updated.
					// A more robust implementation would use a timer, but this is a simple start.

					log.Printf("[Sync] Change detected in %s", filepath.Base(event.Name))

					// Update state modification time locally
					fileState.UpdatedAt = time.Now().Unix()
					w.stateManager.Files[event.Name] = fileState
					w.stateManager.Save() // Persist immediately

					// Trigger callback for the main app to handle Github Sync
					if w.OnChange != nil {
						w.OnChange(event.Name, fileState.GistID)
					}
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

// Stop gracefully shuts down the file watcher.
func (w *Watcher) Stop() {
	close(w.done)
	w.watcher.Close()
}
