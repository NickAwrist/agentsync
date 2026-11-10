// Package link points each configured harness's global instructions file and
// personal skills directory at a config directory, using symlinks.
package link

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NickAwrist/agent_config/internal/config"
	"github.com/NickAwrist/agent_config/internal/manifest"
)

// Linker links each harness's paths to the config in Source. Source must be
// an absolute path. Out receives one line per change or status entry.
type Linker struct {
	Source    string
	Harnesses []config.Harness
	Out       io.Writer
}

// target is one harness path: an instructions file or a skills directory.
type target struct {
	harness string
	path    string
}

func (l Linker) targets() (instructions, skillDirs []target) {
	for _, h := range l.Harnesses {
		if h.Instructions != "" {
			instructions = append(instructions, target{h.Name, h.Instructions})
		}
		if h.Skills != "" {
			skillDirs = append(skillDirs, target{h.Name, h.Skills})
		}
	}
	return instructions, skillDirs
}

func (l Linker) instructionsSource() string {
	return filepath.Join(l.Source, manifest.InstructionsFile)
}

func (l Linker) skillsSource() string {
	return filepath.Join(l.Source, manifest.SkillsDir)
}

// skills returns the names of the directories in Source that hold a SKILL.md.
func (l Linker) skills() ([]string, error) {
	entries, err := os.ReadDir(l.skillsSource())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(l.skillsSource(), e.Name(), "SKILL.md")); err == nil {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// ownsSkillLink reports whether path is a symlink into Source's skills.
func (l Linker) ownsSkillLink(path string) bool {
	dest, err := os.Readlink(path)
	return err == nil && strings.HasPrefix(dest, l.skillsSource()+string(filepath.Separator))
}

// Apply creates or repairs every link and removes links to skills that no
// longer exist. Anything else found at a target path is moved to
// <path>.bak.<timestamp> first.
func (l Linker) Apply() error {
	if _, err := os.Stat(l.instructionsSource()); err != nil {
		return err
	}
	skills, err := l.skills()
	if err != nil {
		return err
	}
	stamp := time.Now().Format("20060102-150405")
	instructions, skillDirs := l.targets()

	for _, t := range instructions {
		if err := l.ensure(t.harness, t.path, l.instructionsSource(), stamp); err != nil {
			return err
		}
	}

	for _, t := range skillDirs {
		current := make(map[string]bool, len(skills))
		for _, name := range skills {
			current[name] = true
			if err := l.ensure(t.harness, filepath.Join(t.path, name), filepath.Join(l.skillsSource(), name), stamp); err != nil {
				return err
			}
		}

		entries, err := os.ReadDir(t.path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		for _, e := range entries {
			path := filepath.Join(t.path, e.Name())
			if current[e.Name()] || !l.ownsSkillLink(path) {
				continue
			}
			if err := os.Remove(path); err != nil {
				return err
			}
			fmt.Fprintf(l.Out, "%-15s removed link to deleted skill %s\n", t.harness, e.Name())
		}
	}
	return nil
}

// ensure makes path a symlink to dest.
func (l Linker) ensure(harness, path, dest, stamp string) error {
	if current, err := os.Readlink(path); err == nil && current == dest {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		backup := path + ".bak." + stamp
		if err := os.Rename(path, backup); err != nil {
			return err
		}
		fmt.Fprintf(l.Out, "%-15s backed up %s -> %s\n", harness, path, backup)
	}
	if err := os.Symlink(dest, path); err != nil {
		return err
	}
	fmt.Fprintf(l.Out, "%-15s linked %s\n", harness, path)
	return nil
}

// Status prints what every target path currently holds and changes nothing.
func (l Linker) Status() error {
	skills, err := l.skills()
	if err != nil {
		return err
	}
	instructions, skillDirs := l.targets()

	fmt.Fprintf(l.Out, "%-15s %s\n", "source", l.Source)
	for _, t := range instructions {
		fmt.Fprintf(l.Out, "%-15s %s\n", t.harness, describe(t.path, l.instructionsSource()))
	}
	for _, t := range skillDirs {
		for _, name := range skills {
			state := describe(filepath.Join(t.path, name), filepath.Join(l.skillsSource(), name))
			fmt.Fprintf(l.Out, "%-15s skill %-24s %s\n", t.harness, name, state)
		}
	}
	return nil
}

func describe(path, dest string) string {
	current, err := os.Readlink(path)
	switch {
	case err == nil && current == dest:
		return "linked"
	case err == nil:
		return "symlink to something else -> " + current
	}
	if _, err := os.Lstat(path); err == nil {
		return "exists, not linked"
	}
	return "missing"
}

// Remove deletes the links that point into Source and leaves everything else,
// including backups, in place.
func (l Linker) Remove() error {
	instructions, skillDirs := l.targets()

	for _, t := range instructions {
		if current, err := os.Readlink(t.path); err != nil || current != l.instructionsSource() {
			continue
		}
		if err := os.Remove(t.path); err != nil {
			return err
		}
		fmt.Fprintf(l.Out, "%-15s unlinked %s\n", t.harness, t.path)
	}

	for _, t := range skillDirs {
		entries, err := os.ReadDir(t.path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		for _, e := range entries {
			path := filepath.Join(t.path, e.Name())
			if !l.ownsSkillLink(path) {
				continue
			}
			if err := os.Remove(path); err != nil {
				return err
			}
			fmt.Fprintf(l.Out, "%-15s unlinked %s\n", t.harness, path)
		}
	}
	return nil
}
