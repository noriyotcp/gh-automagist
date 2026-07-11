package notify

import (
	"sort"

	"github.com/noriyo_tcp/gh-automagist/pkg/state"
)

// Fetcher is the subset of gist.Client that Detect needs. The interface lets
// tests substitute a mock without hitting the network; production code passes
// *gist.Client.
type Fetcher interface {
	FetchGistMeta(gistID string) (updatedAt int64, err error)
}

// FileStatus is the per-tracked-file notify status.
type FileStatus struct {
	Path            string
	GistID          string
	RemoteNewer     bool
	RemoteUpdatedAt int64 // Gist's most recent commit timestamp, unix epoch; 0 on Err
	// Err is set when Detect could not fetch metadata for this file's Gist.
	// All files sharing that Gist carry the same error; other Gists are
	// unaffected.
	Err error
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
			if err != nil {
				result = append(result, FileStatus{
					Path:   path,
					GistID: gistID,
					Err:    err,
				})
				continue
			}
			result = append(result, FileStatus{
				Path:            path,
				GistID:          gistID,
				RemoteNewer:     remoteUpdatedAt > sm.Files[path].RemoteUpdatedAt,
				RemoteUpdatedAt: remoteUpdatedAt,
			})
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Path < result[j].Path
	})
	return result
}
