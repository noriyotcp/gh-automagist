package monitor

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/noriyo_tcp/gh-automagist/pkg/state"
)

// DefaultDebounceInterval is the quiet-window after the last write before OnChange
// fires, so rapid successive writes to the same file collapse into a single sync.
// 5s matches the observed cadence of AI-agent Edit tools (Claude Code and
// similar), which typically emit edits 2–10s apart — Phase 1's original 1s
// undershot that pattern and let bursts leak through as one PATCH each.
// Callers can override via Watcher.DebounceInterval or the --debounce flag
// / GH_AUTOMAGIST_DEBOUNCE_INTERVAL env var wired up in cmd/monitor.go.
const DefaultDebounceInterval = 5 * time.Second

// Watcher watches the parent directories of tracked files and calls OnChange on write.
type Watcher struct {
	watcher      *fsnotify.Watcher
	stateManager *state.Manager
	OnChange     func(absPath string, gistID string) // Callback when a watched file changes
	done         chan bool

	// DebounceInterval overrides DefaultDebounceInterval; must be set before Start().
	// A zero or negative value disables debouncing.
	DebounceInterval time.Duration

	// StateMu guards stateManager. The event loop reloads state.json on its own
	// goroutine while debounce timers run OnChange on theirs, and both read and
	// write the Files map — an OnChange that touches the manager must hold this.
	StateMu sync.Mutex

	timersMu sync.Mutex
	timers   map[string]*debounceEntry

	// watched is the set of directories currently handed to fsnotify, so
	// syncWatches can tell an addition from a re-registration. addFailed holds
	// the directories whose Add errored, to keep the retries from re-logging.
	watched   map[string]bool
	addFailed map[string]bool
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
		watched:          make(map[string]bool),
		addFailed:        make(map[string]bool),
	}, nil
}

// Start runs the event loop; blocks until Stop().
func (w *Watcher) Start() error {
	statePath := w.stateManager.StatePath()

	// 1. Watch the parent directory of every tracked file, plus state.json's
	// own directory (see syncWatches).
	w.syncWatches()

	// 2. Start the event loop
	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return nil
			}

			// The registry itself changed: `add`, `remove` and `pull` all
			// rewrite state.json while the daemon is up. Re-reading it here is
			// what lets a file added just now be watched without a restart.
			// Every event kind counts, not just Write/Create: Save() replaces
			// the file by rename, and which kind a rename-into-a-watched-
			// directory produces differs between inotify and kqueue. The
			// replacement is atomic, so reacting to the wrong kind costs one
			// redundant reload rather than a wrong answer. The sibling
			// state.json.tmp, monitor.pid and monitor.json are ignored by the
			// exact path match.
			if event.Name == statePath {
				// A Remove/Rename can also mean the file is gone for good, and
				// Load treats a missing state.json as an empty registry —
				// which would silently unwatch everything.
				if _, err := os.Stat(statePath); err != nil {
					continue
				}
				w.StateMu.Lock()
				if err := w.stateManager.Load(); err != nil {
					log.Printf("Warning: failed to reload state.json: %v", err)
				} else {
					w.syncWatches()
					w.cancelUntrackedSyncs()
				}
				w.StateMu.Unlock()
				continue
			}

			// We are only interested in Write or Create events (editors sometimes Create/Rename instead of Write)
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				// Reload so `pull`, `add` and `remove` writes made while the
				// daemon was up are visible without a restart.
				//
				// No state is written here on purpose: state.json records
				// what was synced, not what was typed. The timestamps move
				// only after the PATCH succeeds, in the OnChange callback.
				w.StateMu.Lock()
				_, isTracked := w.stateManager.Files[event.Name]
				if isTracked {
					log.Printf("[Sync] Change detected in %s", filepath.Base(event.Name))
					if err := w.stateManager.Load(); err != nil {
						log.Printf("Warning: failed to reload state.json: %v", err)
					}
				}
				fileState, stillTracked := w.stateManager.Files[event.Name]
				w.StateMu.Unlock()

				// Scheduling happens outside the lock: with debouncing disabled
				// it calls OnChange inline, and OnChange takes the same lock.
				if isTracked && stillTracked {
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

// syncWatches brings fsnotify's directory set in line with the registry.
// Directories are watched rather than files because editors save by replacing
// the file, which drops a watch on the inode. state.json's directory is always
// in the set: it is how the event loop hears about `add`, `remove` and `pull`,
// and it must stay watched even when no tracked file lives there.
//
// Called from Start() and from the event loop, both on the same goroutine.
func (w *Watcher) syncWatches() {
	desired := map[string]bool{
		filepath.Dir(w.stateManager.StatePath()): true,
	}
	for absPath := range w.stateManager.Files {
		desired[filepath.Dir(absPath)] = true
	}

	for dir := range desired {
		if w.watched[dir] {
			continue
		}
		// When using fsnotify.Add(), macOS FSEvents might attempt to scan the directory.
		// If the directory contains broken symlinks (e.g., dangling dotfiles), it can throw an error like:
		// "no such file or directory". We should catch this but not let it crash the whole monitor.
		// With go's fsnotify, if we add a path ending in `/...`, it watches recursively, but we are just adding `dir`.
		//
		// A failed Add is left out of `watched`, so the next registry change
		// retries it — fsnotify often watches the valid files in the directory
		// anyway, and a transient failure (EMFILE, a directory not created yet)
		// would otherwise leave the file unwatched for the daemon's whole life,
		// right after `add` reported that no restart was needed. The warning is
		// printed once per directory so the retries stay quiet.
		if err := w.watcher.Add(dir); err != nil {
			if !w.addFailed[dir] {
				w.addFailed[dir] = true
				log.Printf("Warning: failed to watch directory cleanly %s: %v", dir, err)
				log.Printf("  -> This is often caused by broken symlinks in the directory. Continuing anyway.")
			}
			continue
		}
		log.Printf("[gh-automagist] Watching directory: %s", dir)
		delete(w.addFailed, dir)
		w.watched[dir] = true
	}

	for dir := range w.watched {
		if desired[dir] {
			continue
		}
		if err := w.watcher.Remove(dir); err != nil {
			log.Printf("Warning: failed to stop watching %s: %v", dir, err)
		} else {
			log.Printf("[gh-automagist] Stopped watching directory: %s", dir)
		}
		delete(w.watched, dir)
	}
}

// cancelUntrackedSyncs drops armed debounce timers for files the registry no
// longer lists. Without it, a `remove` inside the quiet window still uploads:
// the timer holds the gist ID in its closure, so it would fire and PATCH a file
// the user just stopped tracking. Callers hold StateMu.
func (w *Watcher) cancelUntrackedSyncs() {
	w.timersMu.Lock()
	defer w.timersMu.Unlock()

	for absPath, entry := range w.timers {
		if _, tracked := w.stateManager.Files[absPath]; tracked {
			continue
		}
		if entry.timer.Stop() {
			log.Printf("[Sync] %s is no longer tracked; dropping its pending sync", filepath.Base(absPath))
		}
		delete(w.timers, absPath)
	}
}

// scheduleSync arms (or resets) the per-file debounce timer. gistID is captured
// in the timer's closure so scheduling itself needs no state lookup. The
// OnChange callback does read and write stateManager.Files, from the timer
// goroutine — callers are responsible for serialising that against their own
// use of the manager (cmd/monitor.go holds a mutex for the whole callback).
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
