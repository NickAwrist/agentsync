// Package server serves a config directory to agentsync clients.
package server

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/NickAwrist/agent_config/internal/manifest"
)

//go:embed install.sh
var installScript string

// hostPattern limits the Host header value that gets written into the install
// script, since the script is piped to a shell.
var hostPattern = regexp.MustCompile(`^[A-Za-z0-9.-]+(:[0-9]+)?$`)

// Server holds the current manifest of dir and wakes waiting clients when it
// changes.
type Server struct {
	dir  string
	dist string
	hold time.Duration

	mu      sync.Mutex
	current manifest.Manifest
	changed chan struct{}
}

// New scans dir and returns a server for it. dist is the directory holding
// the client binaries offered at /bin/.
func New(dir, dist string) (*Server, error) {
	s := &Server{
		dir:     dir,
		dist:    dist,
		hold:    50 * time.Second,
		changed: make(chan struct{}),
	}
	if err := s.Rescan(); err != nil {
		return nil, err
	}
	return s, nil
}

// Rescan rebuilds the manifest and, if the hash moved, releases every client
// waiting on the old one.
func (s *Server) Rescan() error {
	m, err := manifest.Scan(s.dir)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if m.Hash != s.current.Hash {
		s.current = m
		close(s.changed)
		s.changed = make(chan struct{})
	}
	return nil
}

// Watch rescans every interval until ctx is done. It catches edits that
// nobody announced with a notify.
func (s *Server) Watch(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Rescan(); err != nil {
				log.Printf("rescan: %v", err)
			}
		}
	}
}

// Hash returns the current manifest hash.
func (s *Server) Hash() string {
	m, _ := s.snapshot()
	return m.Hash
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /manifest", s.handleManifest)
	mux.HandleFunc("GET /files/{path...}", s.handleFile)
	mux.HandleFunc("POST /notify", s.handleNotify)
	mux.HandleFunc("GET /install.sh", s.handleInstall)
	mux.Handle("GET /bin/", http.StripPrefix("/bin/", http.FileServer(http.Dir(s.dist))))
	return mux
}

func (s *Server) snapshot() (manifest.Manifest, <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current, s.changed
}

// handleManifest returns the manifest. A client that passes its own hash as
// ?known= and already matches is held until the manifest changes, and gets
// 304 if nothing changes within the hold time.
func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	m, changed := s.snapshot()
	if known := r.URL.Query().Get("known"); known != "" && known == m.Hash {
		timer := time.NewTimer(s.hold)
		defer timer.Stop()
		select {
		case <-changed:
			m, _ = s.snapshot()
		case <-timer.C:
			w.WriteHeader(http.StatusNotModified)
			return
		case <-r.Context().Done():
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(m); err != nil {
		log.Printf("manifest: %v", err)
	}
}

// handleFile serves one file, and only if the current manifest lists it.
func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	path := r.PathValue("path")
	m, _ := s.snapshot()
	listed := false
	for _, f := range m.Files {
		if f.Path == path {
			listed = true
			break
		}
	}
	if !listed {
		http.NotFound(w, r)
		return
	}

	f, err := os.Open(filepath.Join(s.dir, filepath.FromSlash(path)))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	if _, err := io.Copy(w, f); err != nil {
		log.Printf("file %s: %v", path, err)
	}
}

func (s *Server) handleNotify(w http.ResponseWriter, r *http.Request) {
	if err := s.Rescan(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Fprintln(w, s.Hash())
}

// handleInstall serves the client install script with this server's address
// filled in, taken from the address the caller used to reach it.
func (s *Server) handleInstall(w http.ResponseWriter, r *http.Request) {
	if !hostPattern.MatchString(r.Host) {
		http.Error(w, "unsupported Host header", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript")
	io.WriteString(w, strings.ReplaceAll(installScript, "__SERVER__", "http://"+r.Host))
}
