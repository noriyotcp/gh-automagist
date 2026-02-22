package gist

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/cli/go-gh/v2/pkg/api"
)

// Client wraps the GitHub CLI execution context.
type Client struct{}

// NewClient initializes a new GitHub Gist API client.
func NewClient() *Client {
	return &Client{}
}

// gistUpdateRequest represents the JSON payload to update a Gist via GitHub API.
type gistUpdateRequest struct {
	Files map[string]gistFile `json:"files"`
}

type gistFile struct {
	Content string `json:"content"`
}

// UpdateFile uploads the new content of a local file to the specified Gist ID.
func (c *Client) UpdateFile(gistID string, localFilePath string, content []byte) error {
	filename := filepath.Base(localFilePath)

	// Prepare the JSON payload required by the GitHub API
	payload := gistUpdateRequest{
		Files: map[string]gistFile{
			filename: {
				Content: string(content),
			},
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal gist update payload: %w", err)
	}

	apiEndpoint := fmt.Sprintf("gists/%s", gistID)

	restClient, err := api.DefaultRESTClient()
	if err != nil {
		return fmt.Errorf("failed to initialize github rest client: %w", err)
	}

	// We don't necessarily need the response body, so we pass nil
	err = restClient.Patch(apiEndpoint, bytes.NewReader(payloadBytes), nil)
	if err != nil {
		return fmt.Errorf("failed to execute gist patch request: %w", err)
	}

	return nil
}
