package gist

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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

func TestFetchGistResponse_MultipleFiles(t *testing.T) {
	body := `{
		"updated_at": "2026-07-12T01:26:31Z",
		"files": {
			"a.txt": { "filename": "a.txt", "content": "aaa" },
			"b.txt": { "filename": "b.txt", "content": "bbb" }
		}
	}`
	var resp gistFetchResponse
	require.NoError(t, json.Unmarshal([]byte(body), &resp))

	require.Len(t, resp.Files, 2)
	assert.Equal(t, "aaa", resp.Files["a.txt"].Content)
	assert.Equal(t, "bbb", resp.Files["b.txt"].Content)
	assert.Equal(t, "2026-07-12T01:26:31Z", resp.UpdatedAt)
}

func TestGistResponse_ExtractsUpdatedAtFromWriteResponse(t *testing.T) {
	// Trimmed response shape from PATCH /gists/:id and POST /gists — both
	// return the full Gist object, which is where the new timestamp comes from.
	body := `{
		"id": "2decf6c462d9b4418f2",
		"updated_at": "2026-07-10T22:03:26Z",
		"files": { "README.md": { "filename": "README.md", "content": "hi" } }
	}`
	var resp GistResponse
	require.NoError(t, json.Unmarshal([]byte(body), &resp))
	assert.Equal(t, "2decf6c462d9b4418f2", resp.ID)

	updatedAt, err := resp.updatedAtUnix()
	require.NoError(t, err)
	assert.Equal(t, int64(1783721006), updatedAt)
}

func TestGistResponse_UnparseableUpdatedAtIsAnError(t *testing.T) {
	// A response we cannot read a timestamp out of must not silently become
	// epoch 0 — that would look like "never observed" and re-flag the file.
	resp := GistResponse{ID: "abc", UpdatedAt: "not a timestamp"}

	updatedAt, err := resp.updatedAtUnix()
	require.Error(t, err)
	assert.Zero(t, updatedAt)
}

func TestFileSHAs_DigestsInlineContent(t *testing.T) {
	files := map[string]gistFetchFile{
		"a.txt": {Content: "hello world"},
		"b.txt": {Content: ""},
	}

	shas, err := fileSHAs(files, func(string) ([]byte, error) {
		t.Fatal("raw_url must not be fetched for untruncated files")
		return nil, nil
	})
	require.NoError(t, err)

	assert.Equal(t, sha256Hex("hello world"), shas["a.txt"])
	assert.Equal(t, sha256Hex(""), shas["b.txt"])
}

func TestFileSHAs_ResolvesTruncatedContentFromRawURL(t *testing.T) {
	// GitHub cuts `content` off for large files. Digesting the stub would put a
	// digest of bytes that never existed on disk into the comparison.
	full := "the whole file, all of it"
	files := map[string]gistFetchFile{
		"big.txt": {
			Content:   "the whole file, al",
			Truncated: true,
			RawURL:    "https://gist.githubusercontent.com/u/g/raw/big.txt",
		},
	}

	var requested []string
	shas, err := fileSHAs(files, func(url string) ([]byte, error) {
		requested = append(requested, url)
		return []byte(full), nil
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"https://gist.githubusercontent.com/u/g/raw/big.txt"}, requested)
	assert.Equal(t, sha256Hex(full), shas["big.txt"])
}

func TestFileSHAs_RawFetchFailureIsAnError(t *testing.T) {
	// Dropping the key instead would read as "not in the Gist" downstream, which
	// reports the file as in sync.
	files := map[string]gistFetchFile{
		"big.txt": {Content: "stub", Truncated: true, RawURL: "https://example.invalid/raw"},
	}

	shas, err := fileSHAs(files, func(string) ([]byte, error) {
		return nil, errors.New("simulated 404")
	})

	require.Error(t, err)
	assert.Nil(t, shas)
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
