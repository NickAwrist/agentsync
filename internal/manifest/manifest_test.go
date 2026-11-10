package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanSkipsSymlinks(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("private"), 0o644); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(root, SkillsDir, "a")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(skill, "leak")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, SkillsDir, "b")); err != nil {
		t.Fatal(err)
	}

	m, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Files) != 1 || m.Files[0].Path != "skills/a/SKILL.md" {
		t.Errorf("files = %+v, want only skills/a/SKILL.md", m.Files)
	}
}

// A chmod alone has to move the hash, or clients would never pick it up.
func TestScanHashTracksExecBit(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, SkillsDir, "a", "run.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	after, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if before.Hash == after.Hash {
		t.Error("hash did not change when run.sh became executable")
	}
}
