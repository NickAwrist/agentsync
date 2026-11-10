// Command agentsync keeps agent instructions and skills in sync across
// machines. One machine serves its config directory, and the others mirror it
// and link it into each agent harness.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/NickAwrist/agent_config/internal/client"
	"github.com/NickAwrist/agent_config/internal/config"
	"github.com/NickAwrist/agent_config/internal/link"
	"github.com/NickAwrist/agent_config/internal/manifest"
	"github.com/NickAwrist/agent_config/internal/server"
)

const (
	defaultAddr   = "0.0.0.0:8377"
	defaultServer = "http://localhost:8377"
	retryDelay    = 15 * time.Second
)

const usage = `usage: agentsync <command> [flags]

  init     detect this machine's agent harnesses and write the config file
  serve    serve a config directory to clients
  notify   tell the server the config changed, so clients pull right away
  sync     mirror the server's config and link it into each harness
  link     link a local config directory into each harness

Run "agentsync <command> -h" for the flags of a command.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	var err error
	switch args := os.Args[2:]; os.Args[1] {
	case "init":
		err = runInit()
	case "serve":
		err = runServe(args)
	case "notify":
		err = runNotify(args)
	case "sync":
		err = runSync(args)
	case "link":
		err = runLink(args)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentsync:", err)
		os.Exit(1)
	}
}

// runInit writes the harness config from what it can detect. It never
// overwrites an existing config, so hand edits survive a reinstall.
func runInit() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := config.Path(home)

	cfg, err := config.Load(path)
	switch {
	case err == nil:
		fmt.Printf("keeping existing config %s\n", path)
		printHarnesses(cfg)
		return nil
	case !errors.Is(err, os.ErrNotExist):
		return err
	}

	cfg, missing := config.Detect(home, os.Getenv)
	if err := cfg.Save(path); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	printHarnesses(cfg)
	if len(missing) > 0 {
		fmt.Printf("not found: %s\n", strings.Join(missing, ", "))
		fmt.Printf("If one of those is installed somewhere else, add it to %s:\n", path)
		fmt.Println(`  {"name": "NAME", "instructions": "/abs/path/to/FILE.md", "skills": "/abs/path/to/skills"}`)
	}
	return nil
}

func printHarnesses(cfg config.Config) {
	for _, h := range cfg.Harnesses {
		fmt.Printf("  %-15s instructions %s\n", h.Name, h.Instructions)
		fmt.Printf("  %-15s skills       %s\n", "", h.Skills)
	}
}

// loadHarnesses reads the harness list that sync and link act on.
func loadHarnesses(home string) ([]config.Harness, error) {
	path := config.Path(home)
	cfg, err := config.Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("no config at %s, run \"agentsync init\" first", path)
	}
	if err != nil {
		return nil, err
	}
	if len(cfg.Harnesses) == 0 {
		fmt.Fprintf(os.Stderr, "agentsync: no harnesses in %s, so nothing will be linked\n", path)
	}
	return cfg.Harnesses, nil
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dir := fs.String("dir", ".", "config directory to serve")
	addr := fs.String("addr", defaultAddr, "address to listen on")
	dist := fs.String("dist", "", "directory of client binaries served at /bin/ (default <dir>/dist)")
	interval := fs.Duration("interval", 30*time.Second, "how often to rescan the config for changes")
	fs.Parse(args)

	root, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, manifest.InstructionsFile)); err != nil {
		return err
	}
	if *dist == "" {
		*dist = filepath.Join(root, "dist")
	}

	srv, err := server.New(root, *dist)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go srv.Watch(ctx, *interval)

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		httpServer.Close()
	}()

	log.Printf("serving %s on %s (hash %.12s)", root, *addr, srv.Hash())
	if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func runNotify(args []string) error {
	fs := flag.NewFlagSet("notify", flag.ExitOnError)
	serverURL := fs.String("server", defaultServer, "server URL")
	fs.Parse(args)

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Post(strings.TrimRight(*serverURL, "/")+"/notify", "", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("notify: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	fmt.Printf("server hash %.12s\n", body)
	return nil
}

func runSync(args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	serverURL := fs.String("server", "", "server URL, e.g. http://myserver:8377 (required)")
	dir := fs.String("dir", filepath.Join(config.Dir(home), "mirror"), "local mirror directory")
	once := fs.Bool("once", false, "sync one time and exit instead of running until stopped")
	fs.Parse(args)

	if *serverURL == "" {
		return errors.New("sync: --server is required")
	}
	harnesses, err := loadHarnesses(home)
	if err != nil {
		return err
	}
	mirror, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(mirror, 0o755); err != nil {
		return err
	}
	c, err := client.New(*serverURL, mirror)
	if err != nil {
		return err
	}
	linker := link.Linker{Source: mirror, Harnesses: harnesses, Out: os.Stdout}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *once {
		if _, err := c.Sync(ctx, false); err != nil {
			return err
		}
		return linker.Apply()
	}

	// relink stays set until a link pass succeeds. It starts set so a
	// restart repairs links even when the mirror is already current.
	relink := true
	for {
		changed, err := c.Sync(ctx, !relink)
		if changed {
			log.Printf("synced from %s", *serverURL)
			relink = true
		}
		if err == nil && relink {
			if err = linker.Apply(); err == nil {
				relink = false
			}
		}
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			log.Printf("sync: %v (retrying in %s)", err, retryDelay)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(retryDelay):
			}
		}
	}
}

func runLink(args []string) error {
	fs := flag.NewFlagSet("link", flag.ExitOnError)
	dir := fs.String("dir", ".", "config directory to link")
	status := fs.Bool("status", false, "report the current state and change nothing")
	uninstall := fs.Bool("uninstall", false, "remove the links and leave backups in place")
	fs.Parse(args)

	source, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	harnesses, err := loadHarnesses(home)
	if err != nil {
		return err
	}
	linker := link.Linker{Source: source, Harnesses: harnesses, Out: os.Stdout}

	switch {
	case *status:
		return linker.Status()
	case *uninstall:
		return linker.Remove()
	default:
		return linker.Apply()
	}
}
