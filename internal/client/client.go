// Package client is the CLI's thin HTTP client over the margin REST API. The
// CLI never touches SQLite directly; all writes go through the running server.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kmrinal19/margin/internal/render"
	"github.com/kmrinal19/margin/internal/store"
)

// Client talks to a running `margin serve`.
type Client struct {
	base string
	http *http.Client
}

// New returns a client for the given base URL (e.g. http://127.0.0.1:8848).
func New(base string) *Client {
	return &Client{
		base: strings.TrimRight(base, "/"),
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach margin server at %s (is `margin serve` running?): %w", c.base, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("server returned %s: %s", resp.Status, serverError(data))
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func serverError(data []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &e) == nil && e.Error != "" {
		return e.Error
	}
	if len(data) == 0 {
		return "(no body)"
	}
	return string(data)
}

// Docs lists documents with their open-comment counts.
func (c *Client) Docs(ctx context.Context) ([]render.DocInfo, error) {
	var docs []render.DocInfo
	if err := c.do(ctx, http.MethodGet, "/api/docs", nil, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// Comments lists a doc's threads (anchors freshly re-resolved server-side).
// status is "open", "resolved", or "all".
func (c *Client) Comments(ctx context.Context, slug, status string) ([]store.Thread, error) {
	var out struct {
		Threads []store.Thread `json:"threads"`
	}
	path := "/api/docs/" + url.PathEscape(slug) + "/comments?status=" + url.QueryEscape(status)
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Threads, nil
}

// SetStatus resolves or reopens a thread, optionally posting a note first.
func (c *Client) SetStatus(ctx context.Context, threadID int64, status, by, note string) (store.Thread, error) {
	if status != store.StatusOpen && status != store.StatusResolved {
		return store.Thread{}, errors.New("status must be open or resolved")
	}
	var th store.Thread
	body := map[string]string{"status": status, "by": by, "note": note}
	path := fmt.Sprintf("/api/threads/%d", threadID)
	if err := c.do(ctx, http.MethodPatch, path, body, &th); err != nil {
		return store.Thread{}, err
	}
	return th, nil
}
