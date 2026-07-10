package gist

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cli/go-gh/v2/pkg/api"
)

type Client struct{}

func NewClient() *Client {
	return &Client{}
}

type gistUpdateRequest struct {
	Files map[string]gistFile `json:"files"`
}

type gistFile struct {
	Content string `json:"content"`
}

// UpdateFile PATCHes the Gist with the file's content.
func (c *Client) UpdateFile(gistID string, localFilePath string, content []byte) error {
	filename := filepath.Base(localFilePath)

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

	err = restClient.Patch(apiEndpoint, bytes.NewReader(payloadBytes), nil)
	if err != nil {
		return fmt.Errorf("failed to execute gist patch request: %w", err)
	}

	return nil
}

type gistCreateRequest struct {
	Description string              `json:"description"`
	Public      bool                `json:"public"`
	Files       map[string]gistFile `json:"files"`
}

type GistResponse struct {
	ID string `json:"id"`
}

func (c *Client) CreateGist(localFilePath, description string, public bool) (string, error) {
	filename := filepath.Base(localFilePath)

	content, err := os.ReadFile(localFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", localFilePath, err)
	}

	payload := gistCreateRequest{
		Description: description,
		Public:      public,
		Files: map[string]gistFile{
			filename: {
				Content: string(content),
			},
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal create gist payload: %w", err)
	}

	restClient, err := api.DefaultRESTClient()
	if err != nil {
		return "", fmt.Errorf("failed to initialize github rest client: %w", err)
	}

	var response GistResponse
	err = restClient.Post("gists", bytes.NewReader(payloadBytes), &response)
	if err != nil {
		return "", fmt.Errorf("failed to create gist via API: %w", err)
	}

	return response.ID, nil
}
