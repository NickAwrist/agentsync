package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/NickAwrist/agentsync/internal/manifest"
	"github.com/NickAwrist/agentsync/internal/server"
)

func write(t *testing.T, root, rel, content string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func sync(t *testing.T, c *Client) bool {
	t.Helper()
	changed, err := c.Sync(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	return changed
}

func TestSyncMirrorsServer(t *testing.T) {
	source, mirror := t.TempDir(), t.TempDir()
	write(t, source, "AGENTS.md", "v1", 0o644)
	write(t, source, "skills/a/SKILL.md", "a", 0o644)
	write(t, source, "skills/a/scripts/run.sh", "#!/bin/sh", 0o755)
	write(t, source, "skills/b/SKILL.md", "b", 0o644)
	write(t, source, "README.md", "not synced", 0o644)

	srv, err := server.New(source, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	c, err := New(ts.URL, mirror)
	if err != nil {
		t.Fatal(err)
	}

	if !sync(t, c) {
		t.Fatal("first sync reported no change")
	}
	if _, err := os.Stat(filepath.Join(mirror, "README.md")); err == nil {
		t.Error("README.md was mirrored, but it is outside the synced set")
	}
	info, err := os.Stat(filepath.Join(mirror, "skills/a/scripts/run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Error("run.sh lost its executable bit")
	}
	if sync(t, c) {
		t.Error("second sync reported a change with nothing new on the server")
	}

	// Edit one file, delete a skill, and turn a file into a directory.
	write(t, source, "AGENTS.md", "v2", 0o644)
	if err := os.RemoveAll(filepath.Join(source, "skills/b")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(source, "skills/a/SKILL.md")); err != nil {
		t.Fatal(err)
	}
	write(t, source, "skills/a/SKILL.md/nested.md", "nested", 0o644)
	if err := srv.Rescan(); err != nil {
		t.Fatal(err)
	}

	if !sync(t, c) {
		t.Fatal("sync after server edits reported no change")
	}
	got, err := os.ReadFile(filepath.Join(mirror, "AGENTS.md"))
	if err != nil || string(got) != "v2" {
		t.Errorf("AGENTS.md = %q, %v; want v2", got, err)
	}
	if _, err := os.Stat(filepath.Join(mirror, "skills/b")); err == nil {
		t.Error("deleted skill directory is still in the mirror")
	}
	want, err := manifest.Scan(source)
	if err != nil {
		t.Fatal(err)
	}
	have, err := manifest.Scan(mirror)
	if err != nil {
		t.Fatal(err)
	}
	if have.Hash != want.Hash {
		t.Errorf("mirror hash %s != source hash %s", have.Hash, want.Hash)
	}
}

func TestSyncRejectsPathsOutsideSyncedSet(t *testing.T) {
	for _, path := range []string{"../escape", "skills/../../escape", "/etc/passwd", ".bashrc"} {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(manifest.Manifest{
				Hash:  "remote",
				Files: []manifest.File{{Path: path, SHA256: "x"}},
			})
		}))
		parent := t.TempDir()
		mirror := filepath.Join(parent, "mirror")
		if err := os.Mkdir(mirror, 0o755); err != nil {
			t.Fatal(err)
		}
		c, err := New(ts.URL, mirror)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := c.Sync(context.Background(), false); err == nil {
			t.Errorf("path %q: sync succeeded, want an error", path)
		}
		entries, _ := os.ReadDir(parent)
		if len(entries) != 1 {
			t.Errorf("path %q: sync wrote outside the mirror: %v", path, entries)
		}
		ts.Close()
	}
}

func TestSyncRejectsContentThatDoesNotMatchManifest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest" {
			json.NewEncoder(w).Encode(manifest.Manifest{
				Hash:  "remote",
				Files: []manifest.File{{Path: "AGENTS.md", SHA256: "not-the-real-hash"}},
			})
			return
		}
		w.Write([]byte("tampered"))
	}))
	defer ts.Close()
	mirror := t.TempDir()
	c, err := New(ts.URL, mirror)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.Sync(context.Background(), false); err == nil {
		t.Fatal("sync succeeded, want a hash mismatch error")
	}
	if entries, _ := os.ReadDir(mirror); len(entries) != 0 {
		t.Errorf("mismatched download left files behind: %v", entries)
	}
}
