package gist

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

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

type gistFetchFile struct {
	Content string `json:"content"`
}

type gistFetchResponse struct {
	UpdatedAt string                   `json:"updated_at"`
	Files     map[string]gistFetchFile `json:"files"`
}

// FetchFile returns the content of `filename` inside the given Gist along with
// the Gist's updated_at as a unix epoch. GitHub does not expose a per-file
// endpoint, so we fetch the whole Gist and pick out the entry.
func (c *Client) FetchFile(gistID, filename string) (content []byte, gistUpdatedAt int64, err error) {
	restClient, err := api.DefaultRESTClient()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to initialize github rest client: %w", err)
	}

	var resp gistFetchResponse
	if err := restClient.Get(fmt.Sprintf("gists/%s", gistID), &resp); err != nil {
		return nil, 0, fmt.Errorf("failed to fetch gist %s: %w", gistID, err)
	}

	f, ok := resp.Files[filename]
	if !ok {
		return nil, 0, fmt.Errorf("file %q not found in gist %s", filename, gistID)
	}

	t, err := time.Parse(time.RFC3339, resp.UpdatedAt)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse gist updated_at %q: %w", resp.UpdatedAt, err)
	}

	return []byte(f.Content), t.Unix(), nil
}

// FetchAllFiles returns every file in the Gist keyed by filename, along with
// the Gist's updated_at as a unix epoch. Single API call — call this instead
// of looping FetchFile when you need multiple files from the same Gist.
func (c *Client) FetchAllFiles(gistID string) (files map[string][]byte, updatedAt int64, err error) {
	restClient, err := api.DefaultRESTClient()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to initialize github rest client: %w", err)
	}

	var resp gistFetchResponse
	if err := restClient.Get(fmt.Sprintf("gists/%s", gistID), &resp); err != nil {
		return nil, 0, fmt.Errorf("failed to fetch gist %s: %w", gistID, err)
	}

	t, err := time.Parse(time.RFC3339, resp.UpdatedAt)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse gist updated_at %q: %w", resp.UpdatedAt, err)
	}

	out := make(map[string][]byte, len(resp.Files))
	for name, f := range resp.Files {
		out[name] = []byte(f.Content)
	}
	return out, t.Unix(), nil
}

type gistCommitEntry struct {
	CommittedAt string `json:"committed_at"`
}

// FetchGistMeta returns the timestamp of the Gist's most recent commit as a
// unix epoch. Uses the commits endpoint rather than the full Gist so the
// payload does not include any file content — useful for periodic polling
// where only "did anything change" matters.
func (c *Client) FetchGistMeta(gistID string) (updatedAt int64, err error) {
	restClient, err := api.DefaultRESTClient()
	if err != nil {
		return 0, fmt.Errorf("failed to initialize github rest client: %w", err)
	}

	var commits []gistCommitEntry
	if err := restClient.Get(fmt.Sprintf("gists/%s/commits?per_page=1", gistID), &commits); err != nil {
		return 0, fmt.Errorf("failed to fetch gist %s commits: %w", gistID, err)
	}
	if len(commits) == 0 {
		return 0, fmt.Errorf("gist %s has no commits", gistID)
	}

	t, err := time.Parse(time.RFC3339, commits[0].CommittedAt)
	if err != nil {
		return 0, fmt.Errorf("failed to parse gist committed_at %q: %w", commits[0].CommittedAt, err)
	}
	return t.Unix(), nil
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
