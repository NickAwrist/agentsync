// Package config holds this machine's list of agent harnesses: where each one
// reads its global instructions file and its personal skills.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Harness is one agent harness. Paths are absolute. An empty path means the
// harness has no such location and that link is skipped.
type Harness struct {
	Name         string `json:"name"`
	Instructions string `json:"instructions,omitempty"`
	Skills       string `json:"skills,omitempty"`
}

type Config struct {
	Harnesses []Harness `json:"harnesses"`
}

// Dir is where agentsync keeps its config and its mirror.
func Dir(home string) string {
	return filepath.Join(home, ".agentsync")
}

// Path is the config file's location.
func Path(home string) string {
	return filepath.Join(Dir(home), "config.json")
}

// Load reads and validates the config file. A missing file returns an error
// that matches fs.ErrNotExist.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	// Unknown fields are an error, so a typo in a hand-edited key is
	// reported and not silently ignored.
	var cfg Config
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}

	for _, h := range cfg.Harnesses {
		if h.Name == "" {
			return Config{}, fmt.Errorf("%s: a harness has no name", path)
		}
		for _, p := range []string{h.Instructions, h.Skills} {
			if p != "" && !filepath.IsAbs(p) {
				return Config{}, fmt.Errorf("%s: harness %s: %q is not an absolute path", path, h.Name, p)
			}
		}
	}
	return cfg, nil
}

// Save writes the config file, creating its directory if needed.
func (c Config) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Detect finds the harnesses installed for the user whose home directory is
// home, using each harness's default location on Linux and macOS. A harness
// counts as installed when its marker path exists. missing names the known
// harnesses that were not found.
func Detect(home string, getenv func(string) string) (cfg Config, missing []string) {
	envOr := func(key, fallback string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return fallback
	}
	claude := envOr("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	codex := envOr("CODEX_HOME", filepath.Join(home, ".codex"))
	gemini := filepath.Join(home, ".gemini")

	// Gemini CLI and Antigravity share ~/.gemini and its GEMINI.md, but keep
	// skills in different directories. The marker tells them apart.
	known := []struct {
		marker  string
		harness Harness
	}{
		{claude, Harness{"claude", filepath.Join(claude, "CLAUDE.md"), filepath.Join(claude, "skills")}},
		{codex, Harness{"codex", filepath.Join(codex, "AGENTS.md"), filepath.Join(codex, "skills")}},
		{filepath.Join(gemini, "settings.json"), Harness{"gemini", filepath.Join(gemini, "GEMINI.md"), filepath.Join(gemini, "skills")}},
		{filepath.Join(gemini, "config"), Harness{"antigravity", filepath.Join(gemini, "GEMINI.md"), filepath.Join(gemini, "config", "skills")}},
	}
	// An empty list is saved as [] so the file is ready for hand edits.
	cfg.Harnesses = []Harness{}
	for _, k := range known {
		if _, err := os.Stat(k.marker); err == nil {
			cfg.Harnesses = append(cfg.Harnesses, k.harness)
		} else {
			missing = append(missing, k.harness.Name)
		}
	}

	// T3 Code runs Antigravity with one isolated profile directory per
	// instance. Each profile is laid out like ~/.gemini.
	profiles := filepath.Join(home, ".t3", "userdata", "providers", "antigravity")
	entries, _ := os.ReadDir(profiles)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		profile := filepath.Join(profiles, e.Name())
		cfg.Harnesses = append(cfg.Harnesses, Harness{
			Name:         "t3-antigravity",
			Instructions: filepath.Join(profile, "GEMINI.md"),
			Skills:       filepath.Join(profile, "config", "skills"),
		})
	}
	return cfg, missing
}
