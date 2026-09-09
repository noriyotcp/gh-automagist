package gist

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	// Truncated is set by GitHub when Content holds only the first slice of a
	// large file. RawURL then serves the whole thing, so any digest taken over
	// Content alone would be of bytes that never existed on disk.
	Truncated bool   `json:"truncated"`
	RawURL    string `json:"raw_url"`
}

type gistFetchResponse struct {
	UpdatedAt string                   `json:"updated_at"`
	Files     map[string]gistFetchFile `json:"files"`
}

// FetchFile returns the content of `filename` inside the given Gist along with
// the Gist's updated_at as a unix epoch. GitHub does not expose a per-file
// endpoint, so we fetch the whole Gist and pick out the entry. Content the API
// truncates is completed from raw_url — callers hash this against ContentSHA
// and write it to disk, so a partial read would corrupt both.
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

	content, err = resolveContent(f, fetchRawURL)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read file %q in gist %s: %w", filename, gistID, err)
	}
	return content, t.Unix(), nil
}

// FetchAllFiles returns every file in the Gist keyed by filename, along with
// the Gist's updated_at as a unix epoch. One API call for a Gist under
// GitHub's truncation limit — call this instead of looping FetchFile when you
// need multiple files from the same Gist. Truncated files cost one extra
// request each, so their content is whole rather than a prefix.
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

	out, err := fileContents(resp.Files, fetchRawURL)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read gist %s files: %w", gistID, err)
	}
	return out, t.Unix(), nil
}

// FetchGistMeta returns the Gist's updated_at as a unix epoch together with a
// SHA256 hex digest of every file it holds, keyed by Gist filename. Callers
// compare those digests against state.json's ContentSHA, so a push that only
// bumps the Gist's timestamp does not make its untouched sibling files look
// changed. The full-Gist endpoint carries both the timestamp and the content,
// so a Gist under GitHub's truncation limit costs one request.
func (c *Client) FetchGistMeta(gistID string) (updatedAt int64, contentSHAs map[string]string, err error) {
	restClient, err := api.DefaultRESTClient()
	if err != nil {
		return 0, nil, fmt.Errorf("failed to initialize github rest client: %w", err)
	}

	var resp gistFetchResponse
	if err := restClient.Get(fmt.Sprintf("gists/%s", gistID), &resp); err != nil {
		return 0, nil, fmt.Errorf("failed to fetch gist %s: %w", gistID, err)
	}

	t, err := time.Parse(time.RFC3339, resp.UpdatedAt)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to parse gist updated_at %q: %w", resp.UpdatedAt, err)
	}

	shas, err := fileSHAs(resp.Files, fetchRawURL)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to digest gist %s files: %w", gistID, err)
	}
	return t.Unix(), shas, nil
}

// resolveContent returns a file's whole content, completing it from raw_url
// when the API truncated the inline copy. Every read path goes through here:
// a prefix silently substituted for the real bytes would be written to disk by
// pull and compared against ContentSHA everywhere else.
func resolveContent(f gistFetchFile, fetchRaw func(url string) ([]byte, error)) ([]byte, error) {
	if !f.Truncated {
		return []byte(f.Content), nil
	}
	return fetchRaw(f.RawURL)
}

// fileContents resolves every file in the Gist. A raw fetch that fails aborts
// the whole map rather than dropping the file: an absent key reads as "not in
// the Gist", which notify.Detect reports as in sync and push reports as absent
// from the Gist.
func fileContents(files map[string]gistFetchFile, fetchRaw func(url string) ([]byte, error)) (map[string][]byte, error) {
	out := make(map[string][]byte, len(files))
	for name, f := range files {
		content, err := resolveContent(f, fetchRaw)
		if err != nil {
			return nil, fmt.Errorf("truncated file %q: %w", name, err)
		}
		out[name] = content
	}
	return out, nil
}

// fileSHAs digests each file's resolved content. Same failure rule as
// fileContents: an error beats a map with a hole in it.
func fileSHAs(files map[string]gistFetchFile, fetchRaw func(url string) ([]byte, error)) (map[string]string, error) {
	contents, err := fileContents(files, fetchRaw)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(contents))
	for name, content := range contents {
		sum := sha256.Sum256(content)
		out[name] = hex.EncodeToString(sum[:])
	}
	return out, nil
}

// fetchRawURL downloads a Gist file from its raw_url. The host is
// gist.githubusercontent.com rather than the API host, so go-gh attaches no
// Authorization header — raw Gist URLs are served on the strength of the URL
// itself, secret Gists included.
func fetchRawURL(url string) ([]byte, error) {
	httpClient, err := api.DefaultHTTPClient()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize http client: %w", err)
	}

	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned %s", url, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", url, err)
	}
	return body, nil
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
