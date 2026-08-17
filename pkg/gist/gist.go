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

// UpdateFile PATCHes the Gist with the file's content and returns the Gist's
// new updated_at as a unix epoch. The PATCH response body is the full Gist
// object, so the caller records the new remote timestamp without a second
// round-trip. An unparseable timestamp is returned as an error even though the
// PATCH itself succeeded: the caller's state write would otherwise carry a
// value we do not understand, and skipping it only makes the next sync repeat
// the work.
func (c *Client) UpdateFile(gistID string, localFilePath string, content []byte) (updatedAt int64, err error) {
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
		return 0, fmt.Errorf("failed to marshal gist update payload: %w", err)
	}

	apiEndpoint := fmt.Sprintf("gists/%s", gistID)

	restClient, err := api.DefaultRESTClient()
	if err != nil {
		return 0, fmt.Errorf("failed to initialize github rest client: %w", err)
	}

	var resp GistResponse
	if err := restClient.Patch(apiEndpoint, bytes.NewReader(payloadBytes), &resp); err != nil {
		return 0, fmt.Errorf("failed to execute gist patch request: %w", err)
	}

	return resp.updatedAtUnix()
}

type gistCreateRequest struct {
	Description string              `json:"description"`
	Public      bool                `json:"public"`
	Files       map[string]gistFile `json:"files"`
}

// GistResponse is the subset of the Gist object we read from the create (POST)
// and update (PATCH) endpoints. Both return the full object; we only need the
// identity and the timestamp our own write just produced.
type GistResponse struct {
	ID        string `json:"id"`
	UpdatedAt string `json:"updated_at"`
}

// updatedAtUnix parses the RFC3339 updated_at into a unix epoch, matching the
// unit stored in state.json's remote_updated_at.
func (r GistResponse) updatedAtUnix() (int64, error) {
	t, err := time.Parse(time.RFC3339, r.UpdatedAt)
	if err != nil {
		return 0, fmt.Errorf("failed to parse gist updated_at %q: %w", r.UpdatedAt, err)
	}
	return t.Unix(), nil
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

// CreateGist creates a Gist from the local file and returns its ID along with
// the Gist's updated_at as a unix epoch, so the caller can record a remote
// watermark from the moment the file is first tracked.
func (c *Client) CreateGist(localFilePath, description string, public bool) (id string, updatedAt int64, err error) {
	filename := filepath.Base(localFilePath)

	content, err := os.ReadFile(localFilePath)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read file %s: %w", localFilePath, err)
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
		return "", 0, fmt.Errorf("failed to marshal create gist payload: %w", err)
	}

	restClient, err := api.DefaultRESTClient()
	if err != nil {
		return "", 0, fmt.Errorf("failed to initialize github rest client: %w", err)
	}

	var response GistResponse
	err = restClient.Post("gists", bytes.NewReader(payloadBytes), &response)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create gist via API: %w", err)
	}

	// The Gist exists at this point, so an unparseable timestamp must not fail
	// the call — that would orphan a created Gist the caller never records.
	// updatedAt 0 degrades to the pre-watermark behaviour: the file looks
	// "never observed" until its first successful push writes a real value.
	createdAt, _ := response.updatedAtUnix()
	return response.ID, createdAt, nil
}
