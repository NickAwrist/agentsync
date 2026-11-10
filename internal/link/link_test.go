package link

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/NickAwrist/agentsync/internal/config"
)

func setup(t *testing.T) (Linker, string) {
	t.Helper()
	source, home := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "AGENTS.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha", "beta"} {
		dir := filepath.Join(source, "skills", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var harnesses []config.Harness
	for _, h := range [][3]string{
		{"claude", ".claude/CLAUDE.md", ".claude/skills"},
		{"codex", ".codex/AGENTS.md", ".codex/skills"},
		{"gemini", ".gemini/GEMINI.md", ".gemini/skills"},
	} {
		harnesses = append(harnesses, config.Harness{
			Name:         h[0],
			Instructions: filepath.Join(home, filepath.FromSlash(h[1])),
			Skills:       filepath.Join(home, filepath.FromSlash(h[2])),
		})
	}
	return Linker{Source: source, Harnesses: harnesses, Out: io.Discard}, home
}

func TestApplyLinksAndBacksUpExistingFiles(t *testing.T) {
	l, home := setup(t)
	existing := filepath.Join(home, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("old rules"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := l.Apply(); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(existing)
	if err != nil || string(got) != "rules" {
		t.Errorf("CLAUDE.md reads %q, %v; want the source content", got, err)
	}
	backups, _ := filepath.Glob(existing + ".bak.*")
	if len(backups) != 1 {
		t.Fatalf("want one backup of the old CLAUDE.md, got %v", backups)
	}
	if old, _ := os.ReadFile(backups[0]); string(old) != "old rules" {
		t.Errorf("backup holds %q, want the old content", old)
	}
	skill, err := os.ReadFile(filepath.Join(home, ".codex", "skills", "alpha", "SKILL.md"))
	if err != nil || string(skill) != "alpha" {
		t.Errorf("codex alpha skill reads %q, %v", skill, err)
	}

	// A second pass must not back up the links it made.
	if err := l.Apply(); err != nil {
		t.Fatal(err)
	}
	if backups, _ := filepath.Glob(filepath.Join(home, ".claude", "*.bak.*")); len(backups) != 1 {
		t.Errorf("second pass made more backups: %v", backups)
	}
}

func TestApplyRemovesLinksToDeletedSkills(t *testing.T) {
	l, home := setup(t)
	skillsDir := filepath.Join(home, ".claude", "skills")
	foreign := filepath.Join(skillsDir, "foreign")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := l.Apply(); err != nil {
		t.Fatal(err)
	}

	if err := os.RemoveAll(filepath.Join(l.Source, "skills", "beta")); err != nil {
		t.Fatal(err)
	}
	if err := l.Apply(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Lstat(filepath.Join(skillsDir, "beta")); err == nil {
		t.Error("link to the deleted beta skill is still there")
	}
	if _, err := os.Lstat(filepath.Join(skillsDir, "alpha")); err != nil {
		t.Error("link to the alpha skill was removed")
	}
	if _, err := os.Lstat(foreign); err != nil {
		t.Error("a skill directory that agentsync does not own was removed")
	}
}

func TestRemoveLeavesForeignPathsAlone(t *testing.T) {
	l, home := setup(t)
	if err := l.Apply(); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(home, ".claude", "skills", "foreign")
	if err := os.Mkdir(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	gemini := filepath.Join(home, ".gemini", "GEMINI.md")
	if err := os.Remove(gemini); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gemini, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := l.Remove(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Lstat(filepath.Join(home, ".claude", "CLAUDE.md")); err == nil {
		t.Error("CLAUDE.md link was not removed")
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude", "skills", "alpha")); err == nil {
		t.Error("alpha skill link was not removed")
	}
	if _, err := os.Lstat(foreign); err != nil {
		t.Error("foreign skill directory was removed")
	}
	if _, err := os.Lstat(gemini); err != nil {
		t.Error("a regular GEMINI.md that is not our link was removed")
	}
}
