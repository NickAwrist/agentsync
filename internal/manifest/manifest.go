// Package manifest describes the synced config files under a root directory
// and the hash that identifies their combined state.
package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The synced set is the instructions file plus everything under the skills
// directory. Nothing else in the root is served or mirrored.
const (
	InstructionsFile = "AGENTS.md"
	SkillsDir        = "skills"
)

// File is one synced file. Path is slash-separated and relative to the root.
type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Exec   bool   `json:"exec"`
}

// Manifest lists every synced file, sorted by path. Hash covers all of them,
// so two roots with equal hashes hold identical config.
type Manifest struct {
	Hash  string `json:"hash"`
	Files []File `json:"files"`
}

// Scan builds the manifest for root. A missing instructions file or skills
// directory contributes no files, so an empty root scans cleanly.
func Scan(root string) (Manifest, error) {
	var files []File

	add := func(path string, info fs.FileInfo) error {
		sum, err := hashFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, File{
			Path:   filepath.ToSlash(rel),
			SHA256: sum,
			Exec:   info.Mode()&0o111 != 0,
		})
		return nil
	}

	instructions := filepath.Join(root, InstructionsFile)
	if info, err := os.Lstat(instructions); err == nil && info.Mode().IsRegular() {
		if err := add(instructions, info); err != nil {
			return Manifest{}, err
		}
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Manifest{}, err
	}

	err := filepath.WalkDir(filepath.Join(root, SkillsDir), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return add(path, info)
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Manifest{}, err
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	h := sha256.New()
	for _, f := range files {
		fmt.Fprintf(h, "%s\x00%s\x00%t\n", f.Path, f.SHA256, f.Exec)
	}
	return Manifest{Hash: hex.EncodeToString(h.Sum(nil)), Files: files}, nil
}

// ValidPath reports whether path names a file inside the synced set. Clients
// check it before writing, so a manifest cannot place files outside the mirror.
func ValidPath(path string) bool {
	if !filepath.IsLocal(filepath.FromSlash(path)) {
		return false
	}
	return path == InstructionsFile || strings.HasPrefix(path, SkillsDir+"/")
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
