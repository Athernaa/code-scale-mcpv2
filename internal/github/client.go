package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
)

// TreeEntry represents a file in the GitHub tree API response.
type TreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"` // "blob" or "tree"
	Size int64  `json:"size"`
	SHA  string `json:"sha"`
}

// Client is a GitHub API client.
type Client struct {
	token      string
	httpClient *http.Client
}

// NewClient creates a new GitHub API client.
func NewClient() *Client {
	return &Client{
		token:      os.Getenv("GITHUB_TOKEN"),
		httpClient: http.DefaultClient,
	}
}

// ParseRepoURL parses a GitHub URL or owner/repo string into owner and repo.
func ParseRepoURL(url string) (owner, repo string, err error) {
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(url, ".git")
	url = strings.TrimSuffix(url, "/")

	// Handle full URLs
	for _, prefix := range []string{"https://github.com/", "http://github.com/", "github.com/"} {
		if strings.HasPrefix(url, prefix) {
			url = strings.TrimPrefix(url, prefix)
			break
		}
	}

	parts := strings.SplitN(url, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repository: %q (expected owner/repo)", url)
	}
	return parts[0], parts[1], nil
}

// FetchRepoTree fetches the full file tree for a repository.
func (c *Client) FetchRepoTree(owner, repo string) ([]TreeEntry, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/HEAD?recursive=1", owner, repo)
	body, err := c.get(url)
	if err != nil {
		return nil, err
	}

	var result struct {
		Tree      []TreeEntry `json:"tree"`
		Truncated bool        `json:"truncated"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse tree response: %w", err)
	}

	// Filter to blobs only
	var blobs []TreeEntry
	for _, e := range result.Tree {
		if e.Type == "blob" {
			blobs = append(blobs, e)
		}
	}
	return blobs, nil
}

// FetchFileContent fetches the raw content of a file.
func (c *Client) FetchFileContent(owner, repo, path string) ([]byte, error) {
	url := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/HEAD/%s", owner, repo, path)
	return c.get(url)
}

// FetchGitignore fetches the .gitignore file for a repository.
func (c *Client) FetchGitignore(owner, repo string) (string, error) {
	content, err := c.FetchFileContent(owner, repo, ".gitignore")
	if err != nil {
		return "", nil // Not an error if missing
	}
	return string(content), nil
}

// FetchFilesConcurrently fetches multiple files in parallel.
func (c *Client) FetchFilesConcurrently(owner, repo string, paths []string, concurrency int) map[string][]byte {
	if concurrency <= 0 {
		concurrency = 20
	}

	results := make(map[string][]byte)
	var mu sync.Mutex
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, path := range paths {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			content, err := c.FetchFileContent(owner, repo, p)
			if err != nil {
				return
			}
			mu.Lock()
			results[p] = content
			mu.Unlock()
		}(path)
	}

	wg.Wait()
	return results
}

// get performs an authenticated GET request.
func (c *Client) get(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode {
	case 200:
		return body, nil
	case 404:
		return nil, fmt.Errorf("not found: %s", url)
	case 403:
		return nil, fmt.Errorf("rate limited or forbidden (set GITHUB_TOKEN for higher limits)")
	default:
		return nil, fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, string(body[:min(200, len(body))]))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
