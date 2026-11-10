package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(parts...)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDetectFindsOnlyInstalledHarnesses(t *testing.T) {
	home := t.TempDir()
	mkdir(t, home, ".claude")
	mkdir(t, home, ".gemini", "config")
	profile := mkdir(t, home, ".t3", "userdata", "providers", "antigravity", "abc123")
	codexHome := mkdir(t, t.TempDir(), "custom-codex")
	env := map[string]string{"CODEX_HOME": codexHome}

	cfg, missing := Detect(home, func(key string) string { return env[key] })

	want := []Harness{
		{"claude", filepath.Join(home, ".claude", "CLAUDE.md"), filepath.Join(home, ".claude", "skills")},
		{"codex", filepath.Join(codexHome, "AGENTS.md"), filepath.Join(codexHome, "skills")},
		{"antigravity", filepath.Join(home, ".gemini", "GEMINI.md"), filepath.Join(home, ".gemini", "config", "skills")},
		{"t3-antigravity", filepath.Join(profile, "GEMINI.md"), filepath.Join(profile, "config", "skills")},
	}
	if !reflect.DeepEqual(cfg.Harnesses, want) {
		t.Errorf("harnesses = %+v\nwant %+v", cfg.Harnesses, want)
	}
	if !reflect.DeepEqual(missing, []string{"gemini"}) {
		t.Errorf("missing = %v, want [gemini]", missing)
	}
}

func TestLoadRejectsBadConfig(t *testing.T) {
	cases := map[string]string{
		"relative path": `{"harnesses": [{"name": "claude", "skills": "~/.claude/skills"}]}`,
		"unknown field": `{"harnesses": [{"name": "claude", "instruction": "/a/CLAUDE.md"}]}`,
		"missing name":  `{"harnesses": [{"skills": "/a/skills"}]}`,
	}
	for name, body := range cases {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Errorf("%s: Load succeeded, want an error", name)
		} else if !strings.Contains(err.Error(), path) {
			t.Errorf("%s: error %q does not name the config file", name, err)
		}
	}
}
