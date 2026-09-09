package notify

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"

	"github.com/noriyo_tcp/gh-automagist/pkg/state"
)

// Fetcher is the subset of gist.Client that Detect needs. The interface lets
// tests substitute a mock without hitting the network; production code passes
// *gist.Client.
type Fetcher interface {
	FetchGistMeta(gistID string) (updatedAt int64, contentSHAs map[string]string, err error)
}

// FileStatus is the per-tracked-file notify status. The two axes are
// independent: RemoteNewer answers "does the Gist hold different bytes than we
// last synced", LocalDirty answers "does the file on disk still match what we
// last synced". Both can be true at once, and neither implies the other.
type FileStatus struct {
	Path   string
	GistID string
	// RemoteNewer is true when the Gist's copy of this file differs from the
	// digest recorded at the last sync. It is per-file on purpose: the Gist
	// timestamp is shared by every file in the Gist, so pushing one file used
	// to make all its siblings look remotely changed.
	RemoteNewer     bool
	RemoteUpdatedAt int64 // Gist's updated_at, unix epoch; 0 on Err
	// LocalDirty is true when the file on disk differs from ContentSHA, the
	// digest recorded at the last successful sync. It stays false when no
	// digest has been recorded yet — there is nothing to compare against, so
	// no claim is made either way.
	LocalDirty bool
	// Err is set when Detect could not fetch metadata for this file's Gist.
	// All files sharing that Gist carry the same error; other Gists are
	// unaffected.
	Err error
	// LocalErr is set when the local file could not be read. It is per-file
	// and independent of Err.
	LocalErr error
}

// Detect returns one FileStatus per tracked file. API calls are deduped by
// Gist ID: files sharing a Gist cost one FetchGistMeta call between them.
// A fetch error for one Gist marks every file in that Gist with the same
// Err but does not affect files in other Gists.
//
// Results are sorted by Path for stable CLI output.
func Detect(sm *state.Manager, client Fetcher) []FileStatus {
	gistToPaths := make(map[string][]string)
	for absPath, fs := range sm.Files {
		gistToPaths[fs.GistID] = append(gistToPaths[fs.GistID], absPath)
	}

	result := make([]FileStatus, 0, len(sm.Files))
	for gistID, paths := range gistToPaths {
		remoteUpdatedAt, remoteSHAs, err := client.FetchGistMeta(gistID)
		for _, path := range paths {
			localDirty, localErr := localDirtyState(path, sm.Files[path].ContentSHA)
			if err != nil {
				result = append(result, FileStatus{
					Path:       path,
					GistID:     gistID,
					LocalDirty: localDirty,
					Err:        err,
					LocalErr:   localErr,
				})
				continue
			}
			result = append(result, FileStatus{
				Path:            path,
				GistID:          gistID,
				RemoteNewer:     remoteNewerState(sm.Files[path], remoteSHAs, remoteUpdatedAt, path),
				RemoteUpdatedAt: remoteUpdatedAt,
				LocalDirty:      localDirty,
				LocalErr:        localErr,
			})
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Path < result[j].Path
	})
	return result
}

// remoteNewerState decides whether the Gist holds something we have not seen,
// comparing content rather than the Gist's timestamp. The timestamp is a
// property of the whole Gist, so a push to one file moves it for every sibling;
// the digest is per file and stays put.
//
// Three cases, in order:
//   - No ContentSHA recorded: nothing has been synced yet, or the entry predates
//     the digest. Fall back to the timestamp comparison, which is all the
//     information we have — a fresh entry with RemoteUpdatedAt 0 still gets
//     flagged so the user pulls a baseline.
//   - The file is absent from the Gist: there is nothing to pull, so no claim of
//     newer content. `pull` would fail on the missing entry, and reporting it
//     here would send the user straight at that failure.
//   - Otherwise: newer exactly when the remote bytes differ from the ones we
//     last synced.
func remoteNewerState(fs state.FileState, remoteSHAs map[string]string, remoteUpdatedAt int64, path string) bool {
	if fs.ContentSHA == "" {
		return remoteUpdatedAt > fs.RemoteUpdatedAt
	}
	remoteSHA, ok := remoteSHAs[filepath.Base(path)]
	if !ok {
		return false
	}
	return remoteSHA != fs.ContentSHA
}

// localDirtyState compares the file on disk against the digest recorded at the
// last successful sync. An empty contentSHA means nothing has been synced yet,
// so the file is not read at all and no dirtiness is claimed.
func localDirtyState(path, contentSHA string) (dirty bool, err error) {
	if contentSHA == "" {
		return false, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]) != contentSHA, nil
}
