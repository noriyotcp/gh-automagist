package notify

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeFetcher lets tests script FetchGistMeta responses without touching the
// network and counts calls so we can verify Gist-level dedup. shaByGist maps a
// Gist ID to its filename -> content digest map; leaving it nil models a Gist
// whose files we have never digested.
type fakeFetcher struct {
	metaByGist map[string]int64
	shaByGist  map[string]map[string]string
	errByGist  map[string]error
	calls      map[string]int
}

func (f *fakeFetcher) FetchGistMeta(gistID string) (int64, map[string]string, error) {
	if f.calls == nil {
		f.calls = make(map[string]int)
	}
	f.calls[gistID]++
	if err, ok := f.errByGist[gistID]; ok {
		return 0, nil, err
	}
	return f.metaByGist[gistID], f.shaByGist[gistID], nil
}

func newManager(t *testing.T, files map[string]state.FileState) *state.Manager {
	t.Helper()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	sm, err := state.NewManager()
	require.NoError(t, err)
	sm.Files = files
	return sm
}

func TestDetect_AllInSync(t *testing.T) {
	sm := newManager(t, map[string]state.FileState{
		"/a.txt": {GistID: "g1", RemoteUpdatedAt: 100},
		"/b.txt": {GistID: "g1", RemoteUpdatedAt: 100},
	})
	f := &fakeFetcher{metaByGist: map[string]int64{"g1": 100}}

	result := Detect(sm, f)

	require.Len(t, result, 2)
	for _, s := range result {
		assert.False(t, s.RemoteNewer, "%s should be in sync", s.Path)
		assert.NoError(t, s.Err)
	}
}

func TestDetect_PartialNewer(t *testing.T) {
	sm := newManager(t, map[string]state.FileState{
		"/a.txt": {GistID: "g1", RemoteUpdatedAt: 100},
		"/b.txt": {GistID: "g2", RemoteUpdatedAt: 200},
	})
	f := &fakeFetcher{metaByGist: map[string]int64{
		"g1": 150, // newer
		"g2": 200, // same
	}}

	result := Detect(sm, f)

	byPath := indexByPath(result)
	assert.True(t, byPath["/a.txt"].RemoteNewer)
	assert.False(t, byPath["/b.txt"].RemoteNewer)
}

func TestDetect_NeverPulledAlwaysNewer(t *testing.T) {
	// RemoteUpdatedAt == 0 means "never observed"; any positive remote value
	// counts as newer, which matches the CLI's "flag it for follow-up pull"
	// intent.
	sm := newManager(t, map[string]state.FileState{
		"/fresh.txt": {GistID: "g1", RemoteUpdatedAt: 0},
	})
	f := &fakeFetcher{metaByGist: map[string]int64{"g1": 1}}

	result := Detect(sm, f)

	require.Len(t, result, 1)
	assert.True(t, result[0].RemoteNewer)
}

func TestDetect_DedupsByGistID(t *testing.T) {
	sm := newManager(t, map[string]state.FileState{
		"/a.txt": {GistID: "shared", RemoteUpdatedAt: 100},
		"/b.txt": {GistID: "shared", RemoteUpdatedAt: 100},
		"/c.txt": {GistID: "shared", RemoteUpdatedAt: 100},
	})
	f := &fakeFetcher{metaByGist: map[string]int64{"shared": 150}}

	result := Detect(sm, f)

	assert.Equal(t, 1, f.calls["shared"], "3 files sharing a Gist must cost 1 fetch")
	require.Len(t, result, 3)
	for _, s := range result {
		assert.True(t, s.RemoteNewer, "%s should be marked newer", s.Path)
	}
}

func TestDetect_ErrorIsolatedPerGist(t *testing.T) {
	sm := newManager(t, map[string]state.FileState{
		"/broken1.txt":  {GistID: "bad", RemoteUpdatedAt: 100},
		"/broken2.txt":  {GistID: "bad", RemoteUpdatedAt: 100},
		"/healthy.txt":  {GistID: "good", RemoteUpdatedAt: 100},
		"/uptodate.txt": {GistID: "quiet", RemoteUpdatedAt: 100},
	})
	fetchErr := errors.New("simulated 500")
	f := &fakeFetcher{
		metaByGist: map[string]int64{
			"good":  150,
			"quiet": 100,
		},
		errByGist: map[string]error{"bad": fetchErr},
	}

	result := Detect(sm, f)

	byPath := indexByPath(result)
	// Broken Gist: both files carry Err, RemoteNewer left zero-value.
	assert.ErrorIs(t, byPath["/broken1.txt"].Err, fetchErr)
	assert.ErrorIs(t, byPath["/broken2.txt"].Err, fetchErr)
	assert.False(t, byPath["/broken1.txt"].RemoteNewer)
	// Other Gists unaffected.
	assert.NoError(t, byPath["/healthy.txt"].Err)
	assert.True(t, byPath["/healthy.txt"].RemoteNewer)
	assert.NoError(t, byPath["/uptodate.txt"].Err)
	assert.False(t, byPath["/uptodate.txt"].RemoteNewer)
}

func TestDetect_SortsResultByPath(t *testing.T) {
	sm := newManager(t, map[string]state.FileState{
		"/c.txt": {GistID: "g1"},
		"/a.txt": {GistID: "g1"},
		"/b.txt": {GistID: "g1"},
	})
	f := &fakeFetcher{metaByGist: map[string]int64{"g1": 0}}

	result := Detect(sm, f)

	require.Len(t, result, 3)
	assert.Equal(t, "/a.txt", result[0].Path)
	assert.Equal(t, "/b.txt", result[1].Path)
	assert.Equal(t, "/c.txt", result[2].Path)
}

// writeTracked creates a real file on disk and returns its path plus the
// sha256 of its content, so a test can set ContentSHA to either the matching
// digest (clean) or a different one (dirty).
func writeTracked(t *testing.T, name, content string) (path, contentSHA string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	sum := sha256.Sum256([]byte(content))
	return path, hex.EncodeToString(sum[:])
}

func TestDetect_LocalDirtyWhenFileDivergesFromLastSync(t *testing.T) {
	path, _ := writeTracked(t, "tracked.txt", "edited after the last push")
	otherSHA := "0000000000000000000000000000000000000000000000000000000000000000"
	sm := newManager(t, map[string]state.FileState{
		path: {GistID: "g1", RemoteUpdatedAt: 100, ContentSHA: otherSHA},
	})
	f := &fakeFetcher{metaByGist: map[string]int64{"g1": 100}}

	result := Detect(sm, f)

	require.Len(t, result, 1)
	assert.True(t, result[0].LocalDirty, "local content differs from the recorded digest")
	assert.False(t, result[0].RemoteNewer, "the Gist itself has not moved")
	assert.NoError(t, result[0].LocalErr)
}

func TestDetect_LocalCleanWhenFileMatchesLastSync(t *testing.T) {
	path, contentSHA := writeTracked(t, "tracked.txt", "same bytes as the last push")
	sm := newManager(t, map[string]state.FileState{
		path: {GistID: "g1", RemoteUpdatedAt: 100, ContentSHA: contentSHA},
	})
	f := &fakeFetcher{metaByGist: map[string]int64{"g1": 100}}

	result := Detect(sm, f)

	require.Len(t, result, 1)
	assert.False(t, result[0].LocalDirty)
	assert.NoError(t, result[0].LocalErr)
}

func TestDetect_NoContentSHAMakesNoLocalClaim(t *testing.T) {
	// Nothing has been synced yet, so there is no digest to compare against.
	// The file is not even read — a missing path must not surface as an error.
	sm := newManager(t, map[string]state.FileState{
		"/does/not/exist.txt": {GistID: "g1", RemoteUpdatedAt: 100},
	})
	f := &fakeFetcher{metaByGist: map[string]int64{"g1": 100}}

	result := Detect(sm, f)

	require.Len(t, result, 1)
	assert.False(t, result[0].LocalDirty)
	assert.NoError(t, result[0].LocalErr)
}

func TestDetect_UnreadableLocalFileReportsLocalErr(t *testing.T) {
	// A recorded digest with no file to compare it against is reported, not
	// silently rounded down to "clean".
	sm := newManager(t, map[string]state.FileState{
		"/does/not/exist.txt": {GistID: "g1", RemoteUpdatedAt: 100, ContentSHA: "abc"},
	})
	f := &fakeFetcher{metaByGist: map[string]int64{"g1": 100}}

	result := Detect(sm, f)

	require.Len(t, result, 1)
	assert.Error(t, result[0].LocalErr)
	assert.False(t, result[0].LocalDirty)
	assert.NoError(t, result[0].Err, "a local read failure is not a Gist fetch failure")
}

func TestDetect_LocalDirtyIsReportedEvenWhenGistFetchFails(t *testing.T) {
	// The two axes are independent: losing the remote answer must not hide a
	// local edit we can determine offline.
	path, _ := writeTracked(t, "tracked.txt", "edited while GitHub was down")
	otherSHA := "1111111111111111111111111111111111111111111111111111111111111111"
	sm := newManager(t, map[string]state.FileState{
		path: {GistID: "bad", RemoteUpdatedAt: 100, ContentSHA: otherSHA},
	})
	fetchErr := errors.New("simulated 500")
	f := &fakeFetcher{errByGist: map[string]error{"bad": fetchErr}}

	result := Detect(sm, f)

	require.Len(t, result, 1)
	assert.ErrorIs(t, result[0].Err, fetchErr)
	assert.True(t, result[0].LocalDirty)
}

func TestDetect_SiblingsInSharedGistStayInSyncAfterOnePush(t *testing.T) {
	// The regression this change exists for: pushing one file bumps the Gist's
	// updated_at for every file in it, so the sibling's stale RemoteUpdatedAt
	// used to read as "the Gist moved". Content says otherwise, and content wins.
	pushedPath, pushedSHA := writeTracked(t, "pushed.txt", "just pushed")
	siblingPath, siblingSHA := writeTracked(t, "sibling.txt", "untouched for weeks")
	sm := newManager(t, map[string]state.FileState{
		pushedPath:  {GistID: "shared", RemoteUpdatedAt: 200, ContentSHA: pushedSHA},
		siblingPath: {GistID: "shared", RemoteUpdatedAt: 100, ContentSHA: siblingSHA},
	})
	f := &fakeFetcher{
		metaByGist: map[string]int64{"shared": 200},
		shaByGist: map[string]map[string]string{"shared": {
			"pushed.txt":  pushedSHA,
			"sibling.txt": siblingSHA,
		}},
	}

	result := Detect(sm, f)

	byPath := indexByPath(result)
	assert.False(t, byPath[pushedPath].RemoteNewer, "the file we just pushed is not newer than itself")
	assert.False(t, byPath[siblingPath].RemoteNewer, "a sibling nobody touched must not inherit the Gist's new timestamp")
}

func TestDetect_RemoteNewerWhenContentDiffersDespiteEqualTimestamps(t *testing.T) {
	// The inverse of the regression above: equal timestamps must not hide a
	// genuine remote change, which is what proves content drives the verdict.
	path, contentSHA := writeTracked(t, "tracked.txt", "what we last synced")
	sm := newManager(t, map[string]state.FileState{
		path: {GistID: "g1", RemoteUpdatedAt: 100, ContentSHA: contentSHA},
	})
	f := &fakeFetcher{
		metaByGist: map[string]int64{"g1": 100},
		shaByGist: map[string]map[string]string{"g1": {
			"tracked.txt": "2222222222222222222222222222222222222222222222222222222222222222",
		}},
	}

	result := Detect(sm, f)

	require.Len(t, result, 1)
	assert.True(t, result[0].RemoteNewer)
}

func TestDetect_FileMissingFromGistIsNotNewer(t *testing.T) {
	// Nothing to pull, so no claim of newer content — see remoteNewerState.
	path, contentSHA := writeTracked(t, "orphan.txt", "only here")
	sm := newManager(t, map[string]state.FileState{
		path: {GistID: "g1", RemoteUpdatedAt: 100, ContentSHA: contentSHA},
	})
	f := &fakeFetcher{
		metaByGist: map[string]int64{"g1": 500},
		shaByGist:  map[string]map[string]string{"g1": {"someone-else.txt": contentSHA}},
	}

	result := Detect(sm, f)

	require.Len(t, result, 1)
	assert.False(t, result[0].RemoteNewer)
}

func indexByPath(s []FileStatus) map[string]FileStatus {
	m := make(map[string]FileStatus, len(s))
	for _, fs := range s {
		m[fs.Path] = fs
	}
	return m
}
