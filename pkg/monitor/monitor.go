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

	// linkTargets maps a symlinked tracked file's resolved path back to the
	// tracked path, so an event arriving for the target can be attributed to
	// the entry that names the link. Rebuilt by syncWatches and read by the
	// event loop, both under the same access rules as stateManager.Files.
	linkTargets map[string]string
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
		linkTargets:      make(map[string]string),
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
				// An event for a symlinked file arrives under the target's
				// path on Linux, so resolve it to the key state.json uses
				// before anything looks the entry up.
				name := w.trackedName(event.Name)
				_, isTracked := w.stateManager.Files[name]
				if isTracked {
					log.Printf("[Sync] Change detected in %s", filepath.Base(name))
					if err := w.stateManager.Load(); err != nil {
						log.Printf("Warning: failed to reload state.json: %v", err)
					}
				}
				fileState, stillTracked := w.stateManager.Files[name]
				w.StateMu.Unlock()

				// Scheduling happens outside the lock: with debouncing disabled
				// it calls OnChange inline, and OnChange takes the same lock.
				if isTracked && stillTracked {
					w.scheduleSync(name, fileState.GistID)
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

// symlinkTarget is where a tracked symlink points, if it is one. Reporting
// false covers two cases that share no mechanism but mean the same thing here:
// a plain file has no second end to watch, and a link with nothing behind it
// has no bytes to sync. Neither is a failure the caller has to tell apart.
func symlinkTarget(absPath string) (string, bool) {
	fi, err := os.Lstat(absPath)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return "", false
	}
	target, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", false
	}
	return target, true
}

// trackedName maps an event path onto the registry key it belongs to. An event
// for a symlink's target carries the target's path, which is not what
// state.json is keyed by; everything else passes through unchanged.
//
// Callers hold StateMu, which is also what guards linkTargets.
func (w *Watcher) trackedName(eventPath string) string {
	if _, tracked := w.stateManager.Files[eventPath]; tracked {
		return eventPath
	}
	if linked, ok := w.linkTargets[eventPath]; ok {
		return linked
	}
	return eventPath
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
	links := make(map[string]string, len(w.linkTargets))
	for absPath := range w.stateManager.Files {
		desired[filepath.Dir(absPath)] = true

		// A tracked path can be a symlink into a dotfiles repository, where
		// the bytes live in a directory nothing here would otherwise watch.
		// inotify reports only what happens inside the directories it was
		// given, so without the target's directory an edit to ~/.zshrc is
		// invisible on Linux — kqueue happens to catch it because it opens
		// the directory's entries too, and open(2) follows the link.
		//
		// Only real symlinks are resolved: EvalSymlinks on a plain file still
		// rewrites any symlinked parent (/var to /private/var on macOS), which
		// would register a second watch on the same directory.
		target, ok := symlinkTarget(absPath)
		if !ok {
			continue
		}
		desired[filepath.Dir(target)] = true
		links[target] = absPath
	}
	w.linkTargets = links

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
