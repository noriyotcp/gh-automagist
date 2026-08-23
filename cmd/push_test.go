package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPushGate(t *testing.T) {
	tests := []struct {
		name       string
		remoteSHA  string
		localSHA   string
		contentSHA string
		force      bool
		want       pushDecision
	}{
		{"identical content needs no push", "a", "a", "a", false, pushDecisionSkip},
		{"identical content wins over force", "a", "a", "", true, pushDecisionSkip},
		{"remote still holds our last sync", "base", "local", "base", false, pushDecisionPush},
		{"remote moved since our last sync", "theirs", "local", "base", false, pushDecisionBlock},
		{"no baseline means no proof", "remote", "local", "", false, pushDecisionBlock},
		{"force overrides a moved remote", "theirs", "local", "base", true, pushDecisionPush},
		{"force overrides a missing baseline", "remote", "local", "", true, pushDecisionPush},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := pushGate(tt.remoteSHA, tt.localSHA, tt.contentSHA, tt.force)
			assert.Equal(t, tt.want, got)
			assert.NotEmpty(t, reason, "every decision explains itself")
		})
	}
}

// fakePusher scripts Gist content and PATCH results, and records what was sent
// so tests can assert that blocked and identical files cost no API write.
type fakePusher struct {
	files        map[string]map[string][]byte
	updatedAt    map[string]int64
	patchedAt    map[string]int64
	fetchErr     map[string]error
	updateErr    error
	updatedNames []string
}

func (f *fakePusher) FetchAllFiles(gistID string) (map[string][]byte, int64, error) {
	if err, ok := f.fetchErr[gistID]; ok {
		return nil, 0, err
	}
	return f.files[gistID], f.updatedAt[gistID], nil
}

func (f *fakePusher) UpdateFile(gistID string, localFilePath string, content []byte) (int64, error) {
	if f.updateErr != nil {
		return 0, f.updateErr
	}
	name := filepath.Base(localFilePath)
	f.updatedNames = append(f.updatedNames, name)
	f.files[gistID][name] = content
	return f.patchedAt[gistID], nil
}

// pushEnv builds a tracked file on disk plus the state entry pointing at it.
type pushEnv struct {
	sm    *state.Manager
	paths map[string]string
}

func newPushEnv(t *testing.T) *pushEnv {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	sm, err := state.NewManager()
	require.NoError(t, err)
	return &pushEnv{sm: sm, paths: make(map[string]string)}
}

func (e *pushEnv) track(t *testing.T, name, localContent, gistID string, fs state.FileState) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(localContent), 0o644))
	fs.GistID = gistID
	e.sm.Files[path] = fs
	e.paths[name] = path
	return path
}

func silentOpts(force, dryRun bool) pushOptions {
	return pushOptions{force: force, dryRun: dryRun, logf: func(string, ...any) {}}
}

func TestPushTargets_PushesWhenRemoteMatchesBaseline(t *testing.T) {
	env := newPushEnv(t)
	path := env.track(t, "a.txt", "local v2", "g1", state.FileState{
		ContentSHA:      sha256Hex([]byte("base")),
		RemoteUpdatedAt: 100,
		UpdatedAt:       50,
	})
	client := &fakePusher{
		files:     map[string]map[string][]byte{"g1": {"a.txt": []byte("base")}},
		updatedAt: map[string]int64{"g1": 100},
		patchedAt: map[string]int64{"g1": 900},
	}

	summary := pushTargets(env.sm, client, []string{path}, silentOpts(false, false))

	assert.Equal(t, 1, summary.pushed)
	assert.Equal(t, []string{"a.txt"}, client.updatedNames)
	got := env.sm.Files[path]
	assert.Equal(t, sha256Hex([]byte("local v2")), got.ContentSHA, "digest of the bytes we sent")
	assert.Equal(t, int64(900), got.RemoteUpdatedAt, "our own PATCH is the newest observation")
	assert.Greater(t, got.UpdatedAt, int64(50), "sync time advances only on a real transfer")
}

func TestPushTargets_BackfillsStateForIdenticalContent(t *testing.T) {
	// The pre-v1.9.0 entries: content matches the Gist, but state carries no
	// digest and a zero watermark, which is what made status cry wolf.
	env := newPushEnv(t)
	path := env.track(t, "b.txt", "same", "g1", state.FileState{UpdatedAt: 50})
	client := &fakePusher{
		files:     map[string]map[string][]byte{"g1": {"b.txt": []byte("same")}},
		updatedAt: map[string]int64{"g1": 700},
	}

	summary := pushTargets(env.sm, client, []string{path}, silentOpts(false, false))

	assert.Equal(t, 1, summary.backfilled)
	assert.Equal(t, 0, summary.pushed)
	assert.Empty(t, client.updatedNames, "backfill is a state write, not an API write")
	got := env.sm.Files[path]
	assert.Equal(t, sha256Hex([]byte("same")), got.ContentSHA)
	assert.Equal(t, int64(700), got.RemoteUpdatedAt)
	assert.Equal(t, int64(50), got.UpdatedAt, "no bytes moved, so last-sync stands")
}

func TestPushTargets_AlreadyRecordedFileCountsAsSkipped(t *testing.T) {
	env := newPushEnv(t)
	path := env.track(t, "c.txt", "same", "g1", state.FileState{
		ContentSHA:      sha256Hex([]byte("same")),
		RemoteUpdatedAt: 700,
	})
	client := &fakePusher{
		files:     map[string]map[string][]byte{"g1": {"c.txt": []byte("same")}},
		updatedAt: map[string]int64{"g1": 700},
	}

	summary := pushTargets(env.sm, client, []string{path}, silentOpts(false, false))

	assert.Equal(t, 1, summary.skipped)
	assert.Equal(t, 0, summary.backfilled)
}

func TestPushTargets_BlocksWhenRemoteMovedAndWhenBaselineMissing(t *testing.T) {
	env := newPushEnv(t)
	moved := env.track(t, "moved.txt", "local", "g1", state.FileState{
		ContentSHA: sha256Hex([]byte("base")),
	})
	unproven := env.track(t, "unproven.txt", "local", "g2", state.FileState{})
	client := &fakePusher{
		files: map[string]map[string][]byte{
			"g1": {"moved.txt": []byte("theirs")},
			"g2": {"unproven.txt": []byte("remote")},
		},
		updatedAt: map[string]int64{"g1": 100, "g2": 100},
	}

	summary := pushTargets(env.sm, client, []string{moved, unproven}, silentOpts(false, false))

	assert.Equal(t, 2, summary.blocked)
	assert.Empty(t, client.updatedNames)
	assert.Equal(t, state.FileState{GistID: "g1", ContentSHA: sha256Hex([]byte("base"))}, env.sm.Files[moved],
		"a blocked file's state is left exactly as it was")
	assert.Equal(t, int64(0), env.sm.Files[unproven].RemoteUpdatedAt)
}

func TestPushTargets_SiblingPushCarriesTheGistTimestampToInSyncFiles(t *testing.T) {
	// Both files live in one Gist. Pushing one bumps the Gist's updated_at, so
	// the untouched sibling has to record that same timestamp — otherwise the
	// next status reports our own push as a remote change.
	env := newPushEnv(t)
	dirty := env.track(t, "dirty.txt", "local v2", "g1", state.FileState{
		ContentSHA: sha256Hex([]byte("base")),
	})
	quiet := env.track(t, "quiet.txt", "same", "g1", state.FileState{})
	client := &fakePusher{
		files: map[string]map[string][]byte{
			"g1": {"dirty.txt": []byte("base"), "quiet.txt": []byte("same")},
		},
		updatedAt: map[string]int64{"g1": 100},
		patchedAt: map[string]int64{"g1": 900},
	}

	summary := pushTargets(env.sm, client, []string{dirty, quiet}, silentOpts(false, false))

	assert.Equal(t, 1, summary.pushed)
	assert.Equal(t, 1, summary.backfilled)
	assert.Equal(t, int64(900), env.sm.Files[quiet].RemoteUpdatedAt)
	assert.Equal(t, int64(900), env.sm.Files[dirty].RemoteUpdatedAt)
}

func TestPushTargets_DryRunSendsNothingAndWritesNoState(t *testing.T) {
	env := newPushEnv(t)
	path := env.track(t, "d.txt", "local v2", "g1", state.FileState{
		ContentSHA: sha256Hex([]byte("base")),
	})
	client := &fakePusher{
		files:     map[string]map[string][]byte{"g1": {"d.txt": []byte("base")}},
		updatedAt: map[string]int64{"g1": 100},
	}

	summary := pushTargets(env.sm, client, []string{path}, silentOpts(false, true))

	assert.Equal(t, 1, summary.pushed, "dry-run still reports what it would send")
	assert.Empty(t, client.updatedNames)
	assert.Equal(t, sha256Hex([]byte("base")), env.sm.Files[path].ContentSHA, "state untouched")
}

func TestPushTargets_DryRunDoesNotClaimAnythingWasRecorded(t *testing.T) {
	env := newPushEnv(t)
	path := env.track(t, "h.txt", "same", "g1", state.FileState{})
	client := &fakePusher{
		files:     map[string]map[string][]byte{"g1": {"h.txt": []byte("same")}},
		updatedAt: map[string]int64{"g1": 700},
	}

	var lines []string
	opts := silentOpts(false, true)
	opts.logf = func(format string, args ...any) {
		lines = append(lines, fmt.Sprintf(format, args...))
	}
	summary := pushTargets(env.sm, client, []string{path}, opts)

	assert.Equal(t, 1, summary.backfilled)
	assert.Equal(t, int64(0), env.sm.Files[path].RemoteUpdatedAt, "state untouched")
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], "would record")
	assert.NotContains(t, lines[0], "recorded digest")
}

func TestPushTargets_FetchErrorMarksEveryFileInThatGistOnly(t *testing.T) {
	env := newPushEnv(t)
	broken := env.track(t, "e.txt", "local", "g1", state.FileState{})
	fine := env.track(t, "f.txt", "same", "g2", state.FileState{})
	client := &fakePusher{
		files:     map[string]map[string][]byte{"g2": {"f.txt": []byte("same")}},
		updatedAt: map[string]int64{"g2": 700},
		fetchErr:  map[string]error{"g1": errors.New("gist 404")},
	}

	summary := pushTargets(env.sm, client, []string{broken, fine}, silentOpts(false, false))

	assert.Equal(t, 1, summary.errored)
	assert.Equal(t, 1, summary.backfilled, "an unrelated Gist keeps being processed")
	assert.Equal(t, int64(700), env.sm.Files[fine].RemoteUpdatedAt)
}

func TestPushTargets_MissingFileInGistIsAnErrorNotAPush(t *testing.T) {
	env := newPushEnv(t)
	path := env.track(t, "gone.txt", "local", "g1", state.FileState{
		ContentSHA: sha256Hex([]byte("base")),
	})
	client := &fakePusher{
		files:     map[string]map[string][]byte{"g1": {"other.txt": []byte("x")}},
		updatedAt: map[string]int64{"g1": 100},
	}

	summary := pushTargets(env.sm, client, []string{path}, silentOpts(false, false))

	assert.Equal(t, 1, summary.errored)
	assert.Empty(t, client.updatedNames)
}

func TestPushTargets_FailedPatchLeavesStateAlone(t *testing.T) {
	env := newPushEnv(t)
	path := env.track(t, "g.txt", "local v2", "g1", state.FileState{
		ContentSHA:      sha256Hex([]byte("base")),
		RemoteUpdatedAt: 100,
		UpdatedAt:       50,
	})
	client := &fakePusher{
		files:     map[string]map[string][]byte{"g1": {"g.txt": []byte("base")}},
		updatedAt: map[string]int64{"g1": 100},
		updateErr: errors.New("network down"),
	}

	summary := pushTargets(env.sm, client, []string{path}, silentOpts(false, false))

	assert.Equal(t, 1, summary.errored)
	assert.Equal(t, 0, summary.pushed)
	got := env.sm.Files[path]
	assert.Equal(t, sha256Hex([]byte("base")), got.ContentSHA, "a failed PATCH must not look like a sync")
	assert.Equal(t, int64(50), got.UpdatedAt)
}
