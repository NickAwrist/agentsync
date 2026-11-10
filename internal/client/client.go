// Package client mirrors a server's config directory into a local one.
package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/NickAwrist/agentsync/internal/manifest"
)

// Client syncs a local mirror directory against one server.
type Client struct {
	base *url.URL
	dir  string
	http *http.Client
}

func New(server, dir string) (*Client, error) {
	base, err := url.Parse(server)
	if err != nil {
		return nil, err
	}
	if base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("server %q is not a URL like http://host:port", server)
	}
	// The timeout has to outlast the server's long-poll hold. It also
	// frees a request left hanging when a laptop sleeps mid-poll.
	return &Client{base: base, dir: dir, http: &http.Client{Timeout: 2 * time.Minute}}, nil
}

// Sync brings the mirror in line with the server and reports whether it
// changed anything. With wait set, a mirror that already matches blocks until
// the server has something new or its hold time runs out.
func (c *Client) Sync(ctx context.Context, wait bool) (bool, error) {
	local, err := manifest.Scan(c.dir)
	if err != nil {
		return false, err
	}

	known := ""
	if wait {
		known = local.Hash
	}
	remote, err := c.fetchManifest(ctx, known)
	if err != nil {
		return false, err
	}
	if remote == nil || remote.Hash == local.Hash {
		return false, nil
	}

	if err := c.apply(ctx, local, *remote); err != nil {
		return false, err
	}

	// The server can change between the manifest and the downloads. Each
	// file is verified on its own, and this catches anything left over.
	after, err := manifest.Scan(c.dir)
	if err != nil {
		return false, err
	}
	if after.Hash != remote.Hash {
		return false, fmt.Errorf("mirror hash %.12s does not match server hash %.12s after sync", after.Hash, remote.Hash)
	}
	return true, nil
}

// fetchManifest returns nil when the server answers that known is current.
func (c *Client) fetchManifest(ctx context.Context, known string) (*manifest.Manifest, error) {
	u := c.url("manifest")
	if known != "" {
		u += "?known=" + url.QueryEscape(known)
	}
	resp, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", u, resp.Status)
	}
	var m manifest.Manifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	return &m, nil
}

func (c *Client) apply(ctx context.Context, local, remote manifest.Manifest) error {
	want := make(map[string]manifest.File, len(remote.Files))
	for _, f := range remote.Files {
		if !manifest.ValidPath(f.Path) {
			return fmt.Errorf("server sent a path outside the synced set: %q", f.Path)
		}
		want[f.Path] = f
	}

	// Delete first so a path that switched between file and directory is
	// clear before its replacement is written.
	have := make(map[string]manifest.File, len(local.Files))
	for _, f := range local.Files {
		have[f.Path] = f
		if _, ok := want[f.Path]; !ok {
			if err := c.remove(f.Path); err != nil {
				return err
			}
		}
	}

	for _, f := range remote.Files {
		if have[f.Path] == f {
			continue
		}
		if err := c.download(ctx, f); err != nil {
			return err
		}
	}
	return nil
}

// remove deletes a mirrored file and any directories it leaves empty.
func (c *Client) remove(rel string) error {
	target := filepath.Join(c.dir, filepath.FromSlash(rel))
	if err := os.Remove(target); err != nil {
		return err
	}
	for dir := filepath.Dir(target); dir != c.dir; dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil {
			break
		}
	}
	return nil
}

// download writes one file through a temp file in the same directory, checks
// its hash against the manifest, and renames it into place.
func (c *Client) download(ctx context.Context, f manifest.File) error {
	u := c.url("files", f.Path)
	resp, err := c.get(ctx, u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", u, resp.Status)
	}

	target := filepath.Join(c.dir, filepath.FromSlash(f.Path))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".agentsync-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), resp.Body); err != nil {
		return fmt.Errorf("download %s: %w", f.Path, err)
	}
	if sum := hex.EncodeToString(h.Sum(nil)); sum != f.SHA256 {
		return fmt.Errorf("download %s: hash %.12s does not match manifest %.12s", f.Path, sum, f.SHA256)
	}

	mode := os.FileMode(0o644)
	if f.Exec {
		mode = 0o755
	}
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}

func (c *Client) get(ctx context.Context, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	return c.http.Do(req)
}

func (c *Client) url(elem ...string) string {
	u := *c.base
	u.Path = path.Join(append([]string{"/", u.Path}, elem...)...)
	return u.String()
}
