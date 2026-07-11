package gist

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_PayloadGeneration(t *testing.T) {
	// 1. Create a temporary file to mock local file modification
	tempDir := t.TempDir()
	tempFilePath := filepath.Join(tempDir, "test_config.lua")

	err := os.WriteFile(tempFilePath, []byte("print('hello auto-magist')"), 0644)
	require.NoError(t, err)

	// 2. Read the file
	content, err := os.ReadFile(tempFilePath)
	require.NoError(t, err)

	// 3. Re-derive the payload here to lock down the JSON shape against the GitHub API.
	filename := filepath.Base(tempFilePath)
	payload := gistUpdateRequest{
		Files: map[string]gistFile{
			filename: {
				Content: string(content),
			},
		},
	}

	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	// 4. Verify JSON structure
	expectedJSON := `{"files":{"test_config.lua":{"content":"print('hello auto-magist')"}}}`
	assert.JSONEq(t, expectedJSON, string(payloadBytes))
}

// Note: We avoid an actual integration test calling restClient.Patch() here to prevent
// mutating the user's real GitHub account or exceeding API rate limits during standard local tests.

func TestFetchGistResponse_ExtractsFileContent(t *testing.T) {
	body := `{
		"updated_at": "2026-07-10T22:03:26Z",
		"files": {
			"test.txt": {
				"filename": "test.txt",
				"content": "hello world"
			}
		}
	}`
	var resp gistFetchResponse
	require.NoError(t, json.Unmarshal([]byte(body), &resp))

	f, ok := resp.Files["test.txt"]
	require.True(t, ok, "test.txt should be present in parsed response")
	assert.Equal(t, "hello world", f.Content)
	assert.Equal(t, "2026-07-10T22:03:26Z", resp.UpdatedAt)
}

func TestGistCommitEntry_ExtractsCommittedAt(t *testing.T) {
	// Response shape from GET /gists/:id/commits?per_page=1
	body := `[
		{
			"version": "58215998cc8162cbf2f2a45b0bc3775d107fb4a0",
			"committed_at": "2026-07-10T22:03:26Z"
		}
	]`
	var commits []gistCommitEntry
	require.NoError(t, json.Unmarshal([]byte(body), &commits))

	require.Len(t, commits, 1)
	assert.Equal(t, "2026-07-10T22:03:26Z", commits[0].CommittedAt)
}
