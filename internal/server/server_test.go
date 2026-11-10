package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T) (*Server, *httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("private"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, err := New(dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts, dir
}

func status(t *testing.T, method, url string) int {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestManifestHoldsKnownHashUntilNotify(t *testing.T) {
	srv, ts, dir := newTestServer(t)
	url := ts.URL + "/manifest?known=" + srv.Hash()

	srv.hold = 50 * time.Millisecond
	if got := status(t, http.MethodGet, url); got != http.StatusNotModified {
		t.Fatalf("unchanged config: status %d, want 304", got)
	}

	srv.hold = time.Minute
	done := make(chan int)
	go func() { done <- status(t, http.MethodGet, url) }()
	select {
	case got := <-done:
		t.Fatalf("request returned %d before the config changed", got)
	case <-time.After(100 * time.Millisecond):
	}

	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := status(t, http.MethodPost, ts.URL+"/notify"); got != http.StatusOK {
		t.Fatalf("notify: status %d", got)
	}
	select {
	case got := <-done:
		if got != http.StatusOK {
			t.Errorf("after notify: status %d, want 200", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiting client was not released by notify")
	}
}

func TestFilesServesOnlyTheSyncedSet(t *testing.T) {
	_, ts, _ := newTestServer(t)

	if got := status(t, http.MethodGet, ts.URL+"/files/AGENTS.md"); got != http.StatusOK {
		t.Errorf("AGENTS.md: status %d, want 200", got)
	}
	for _, path := range []string{"secret.txt", "..%2Fsecret.txt", "skills/..%2F..%2Fsecret.txt"} {
		if got := status(t, http.MethodGet, ts.URL+"/files/"+path); got != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, got)
		}
	}
}

func TestInstallScriptUsesCallerHost(t *testing.T) {
	_, ts, _ := newTestServer(t)

	get := func(host string) (int, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, ts.URL+"/install.sh", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, string(body)
	}

	code, body := get("myserver:8377")
	if code != http.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}
	if !strings.Contains(body, `SERVER="http://myserver:8377"`) || strings.Contains(body, "__SERVER__") {
		t.Error("install script does not have the server address filled in")
	}

	// The script is piped to a shell, so a Host that could break out of the
	// quoted SERVER value must be refused.
	for _, host := range []string{`x";rm -rf ~;"`, "x$(id)", "x y"} {
		if code, _ := get(host); code != http.StatusBadRequest {
			t.Errorf("Host %q: status %d, want 400", host, code)
		}
	}
}
