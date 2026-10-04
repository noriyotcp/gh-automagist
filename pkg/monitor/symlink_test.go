package monitor

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/stretchr/testify/require"
)

// A dotfile linked into a dotfiles repository is the shape this has to work
// for: ~/.zshrc is a symlink and the bytes live in ~/dotfiles/zshrc. Only the
// link's own parent is watched (syncWatches takes filepath.Dir of the tracked
// path), so whether an edit is seen at all depends on what the platform's
// fsnotify backend does with the link.
//
// The two backends differ. kqueue emulates a directory watch by also opening
// each entry in it, and open(2) follows a symlink, so the descriptor lands on
// the target's inode and a write to the target reports under the link's path.
// inotify adds one watch on the directory inode and nothing else: a write
// inside another directory never touches this one, so there is no event to
// report. macOS was observed to work in #33; Linux was never probed until
// this test.
func TestWatcher_DetectsAnEditThroughASymlink(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)

	// linkDir gets watched because the tracked path lives there. storeDir does
	// not, which is the whole point — a dotfiles repository is somewhere else.
	linkDir := filepath.Join(tempDir, "home")
	storeDir := filepath.Join(tempDir, "dotfiles")
	require.NoError(t, os.MkdirAll(linkDir, 0755))
	require.NoError(t, os.MkdirAll(storeDir, 0755))

	target := filepath.Join(storeDir, "zshrc")
	link := filepath.Join(linkDir, ".zshrc")
	require.NoError(t, os.WriteFile(target, []byte("initial data"), 0644))
	require.NoError(t, os.Symlink(target, link))

	sm, err := state.NewManager()
	require.NoError(t, err)
	sm.AddTrackedFile(link, "gist_symlink", time.Now().Unix())
	require.NoError(t, sm.Save())

	w, err := NewWatcher(sm)
	require.NoError(t, err)
	w.DebounceInterval = 50 * time.Millisecond

	changed := make(chan string, 1)
	w.OnChange = func(absPath, gistID string) {
		changed <- absPath
	}

	go func() { _ = w.Start() }()
	defer w.Stop()
	time.Sleep(100 * time.Millisecond)

	// Writing to the link follows it, so the bytes land on storeDir's inode.
	// This is what an editor saving ~/.zshrc does, and what pull does after
	// resolveForWrite.
	require.NoError(t, os.WriteFile(link, []byte("updated data"), 0644))

	select {
	case got := <-changed:
		require.Equal(t, link, got, "the event should carry the tracked path, not the target")
	case <-time.After(2 * time.Second):
		t.Fatalf("no change reported for %s after writing through the symlink (GOOS=%s)",
			link, runtime.GOOS)
	}
}
