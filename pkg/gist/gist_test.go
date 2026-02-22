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

	// 3. Replicate the payload generation logic to verify the JSON structure matches GitHub's API perfectly
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
