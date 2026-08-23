package notify

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sort"

	"github.com/noriyo_tcp/gh-automagist/pkg/state"
)

// Fetcher is the subset of gist.Client that Detect needs. The interface lets
// tests substitute a mock without hitting the network; production code passes
// *gist.Client.
type Fetcher interface {
	FetchGistMeta(gistID string) (updatedAt int64, err error)
}

// FileStatus is the per-tracked-file notify status. The two axes are
// independent: RemoteNewer answers "did the Gist move since we last observed
// it", LocalDirty answers "does the file on disk still match what we last
// synced". Both can be true at once, and neither implies the other.
type FileStatus struct {
	Path            string
	GistID          string
	RemoteNewer     bool
	RemoteUpdatedAt int64 // Gist's most recent commit timestamp, unix epoch; 0 on Err
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
		remoteUpdatedAt, err := client.FetchGistMeta(gistID)
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
				RemoteNewer:     remoteUpdatedAt > sm.Files[path].RemoteUpdatedAt,
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
