package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddGate(t *testing.T) {
	tests := []struct {
		name        string
		remoteSHA   string
		localSHA    string
		force       bool
		adoptRemote bool
		want        addDecision
	}{
		{"absent from the gist is a plain link", "", "local", false, false, addDecisionPush},
		{"absent stays a push even under force", "", "local", true, false, addDecisionPush},
		{"absent leaves nothing to adopt", "", "local", false, true, addDecisionBlock},
		{"identical content needs no transfer", "same", "same", false, false, addDecisionLink},
		{"identical content wins over force", "same", "same", true, false, addDecisionLink},
		{"identical content wins over adopt", "same", "same", false, true, addDecisionLink},
		{"divergence blocks by default", "theirs", "ours", false, false, addDecisionBlock},
		{"force sends local up", "theirs", "ours", true, false, addDecisionPush},
		{"adopt brings remote down", "theirs", "ours", false, true, addDecisionAdopt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := addGate(tt.remoteSHA, tt.localSHA, tt.force, tt.adoptRemote)
			assert.Equal(t, tt.want, got)
			assert.NotEmpty(t, reason, "every decision explains itself")
		})
	}
}

// newAddEnv builds an as-yet-untracked local file plus a state manager rooted
// in a throwaway HOME, so Save() cannot touch the developer's real state.json.
func newAddEnv(t *testing.T, name, content string) (*state.Manager, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	sm, err := state.NewManager()
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return sm, path
}

func silentAddOpts(force, adoptRemote bool) addOptions {
	return addOptions{force: force, adoptRemote: adoptRemote, logf: func(string, ...any) {}}
}

// This is the cross-machine case that motivated the gate: machine B's copy is
// stale, and linking it must not push that staleness over machine A's work.
func TestLinkExistingGist_BlocksOnDivergence(t *testing.T) {
	sm, path := newAddEnv(t, "notes.md", "stale local copy\n")
	client := &fakePusher{
		files:     map[string]map[string][]byte{"g1": {"notes.md": []byte("newer remote copy\n")}},
		updatedAt: map[string]int64{"g1": 700},
	}

	err := linkExistingGist(sm, client, path, "g1", []byte("stale local copy\n"), silentAddOpts(false, false))

	require.Error(t, err)
	assert.Empty(t, client.updatedNames, "a blocked link costs no API write")
	assert.NotContains(t, sm.Files, path, "a blocked link tracks nothing")
	local, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, "stale local copy\n", string(local), "a blocked link leaves the local file alone")
}

func TestLinkExistingGist_IdenticalContentSkipsThePatch(t *testing.T) {
	sm, path := newAddEnv(t, "notes.md", "same bytes\n")
	client := &fakePusher{
		files:     map[string]map[string][]byte{"g1": {"notes.md": []byte("same bytes\n")}},
		updatedAt: map[string]int64{"g1": 700},
	}

	require.NoError(t, linkExistingGist(sm, client, path, "g1", []byte("same bytes\n"), silentAddOpts(false, false)))

	assert.Empty(t, client.updatedNames, "both sides already agree")
	got := sm.Files[path]
	assert.Equal(t, "g1", got.GistID)
	assert.Equal(t, sha256Hex([]byte("same bytes\n")), got.ContentSHA)
	assert.Equal(t, int64(700), got.RemoteUpdatedAt, "the observed gist timestamp, not one we produced")
}

func TestLinkExistingGist_PushesWhenTheGistLacksTheFile(t *testing.T) {
	sm, path := newAddEnv(t, "notes.md", "brand new\n")
	client := &fakePusher{
		files:     map[string]map[string][]byte{"g1": {"other.md": []byte("unrelated\n")}},
		updatedAt: map[string]int64{"g1": 700},
		patchedAt: map[string]int64{"g1": 900},
	}

	require.NoError(t, linkExistingGist(sm, client, path, "g1", []byte("brand new\n"), silentAddOpts(false, false)))

	assert.Equal(t, []string{"notes.md"}, client.updatedNames)
	got := sm.Files[path]
	assert.Equal(t, sha256Hex([]byte("brand new\n")), got.ContentSHA)
	assert.Equal(t, int64(900), got.RemoteUpdatedAt, "our own PATCH is the newest observation")
}

func TestLinkExistingGist_ForceOverwritesTheGist(t *testing.T) {
	sm, path := newAddEnv(t, "notes.md", "local wins\n")
	client := &fakePusher{
		files:     map[string]map[string][]byte{"g1": {"notes.md": []byte("remote loses\n")}},
		updatedAt: map[string]int64{"g1": 700},
		patchedAt: map[string]int64{"g1": 900},
	}

	require.NoError(t, linkExistingGist(sm, client, path, "g1", []byte("local wins\n"), silentAddOpts(true, false)))

	assert.Equal(t, []string{"notes.md"}, client.updatedNames)
	assert.Equal(t, []byte("local wins\n"), client.files["g1"]["notes.md"])
	got := sm.Files[path]
	assert.Equal(t, sha256Hex([]byte("local wins\n")), got.ContentSHA)
	assert.Equal(t, int64(900), got.RemoteUpdatedAt)
}

func TestLinkExistingGist_AdoptRemoteRewritesTheLocalFile(t *testing.T) {
	sm, path := newAddEnv(t, "notes.md", "stale local copy\n")
	client := &fakePusher{
		files:     map[string]map[string][]byte{"g1": {"notes.md": []byte("newer remote copy\n")}},
		updatedAt: map[string]int64{"g1": 700},
	}

	require.NoError(t, linkExistingGist(sm, client, path, "g1", []byte("stale local copy\n"), silentAddOpts(false, true)))

	assert.Empty(t, client.updatedNames, "adopting pulls down; it never pushes")
	local, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "newer remote copy\n", string(local))

	backups, err := filepath.Glob(path + ".bak.*")
	require.NoError(t, err)
	require.Len(t, backups, 1, "the overwritten local copy is kept")
	saved, err := os.ReadFile(backups[0])
	require.NoError(t, err)
	assert.Equal(t, "stale local copy\n", string(saved))

	got := sm.Files[path]
	assert.Equal(t, sha256Hex([]byte("newer remote copy\n")), got.ContentSHA)
	assert.Equal(t, int64(700), got.RemoteUpdatedAt)
	assert.Greater(t, got.PullSuppressUntil, time.Now().Unix(),
		"a running daemon must not bounce the adopted bytes back up")
}

func TestLinkExistingGist_AdoptRemoteNeedsARemoteFile(t *testing.T) {
	sm, path := newAddEnv(t, "notes.md", "only local\n")
	client := &fakePusher{
		files:     map[string]map[string][]byte{"g1": {}},
		updatedAt: map[string]int64{"g1": 700},
	}

	err := linkExistingGist(sm, client, path, "g1", []byte("only local\n"), silentAddOpts(false, true))

	require.Error(t, err)
	assert.Empty(t, client.updatedNames)
	assert.NotContains(t, sm.Files, path)
}

func TestLinkExistingGist_FetchFailureTracksNothing(t *testing.T) {
	sm, path := newAddEnv(t, "notes.md", "local\n")
	client := &fakePusher{fetchErr: map[string]error{"g1": errors.New("404 Not Found")}}

	err := linkExistingGist(sm, client, path, "g1", []byte("local\n"), silentAddOpts(false, false))

	require.Error(t, err)
	assert.Empty(t, client.updatedNames, "an unreadable gist is never written to")
	assert.NotContains(t, sm.Files, path)
}
