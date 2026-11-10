# agent_config

`agentsync` is a small Go program that keeps your global agent instructions and
personal skills identical on every machine.

`AGENTS.md` is the instructions file. Skills live in `skills/`. One machine, the
server, holds both and serves them. Every other machine runs a client that
mirrors them and symlinks the mirror into each agent harness it has: Claude
Code, Codex CLI, Gemini CLI, and Antigravity.

This repo ships the program only. Your `AGENTS.md` and `skills/` sit in your
checkout and `.gitignore` keeps them out of git.

## Setup

### 1. Server

The server is the machine where you edit your instructions and skills. It
needs Linux with systemd, Go 1.22 or newer, and `make`.

```sh
git clone https://github.com/NickAwrist/agent_config.git
cd agent_config
```

Put your instructions and skills in the checkout:

```
agent_config/
  AGENTS.md              your global instructions
  skills/
    my-skill/SKILL.md    one directory per skill
```

Then run:

```sh
make server
```

That one command:

- builds `agentsync` into `~/.local/bin`
- cross-compiles the client binaries the server hands out
- starts the server on port 8377 as a systemd user service
- detects the harnesses on this machine and links this checkout into them

It ends by printing the install command for your clients. Anything already at a
harness path, such as an existing `~/.claude/CLAUDE.md`, is moved to
`<path>.bak.<timestamp>` and not deleted.

A systemd user service stops when you log out unless lingering is on. Turn it
on once with `loginctl enable-linger`.

No systemd? Run `make install dist`, then start `agentsync serve` however you
like and run `agentsync init && agentsync link`.

### 2. Clients

A client is any other Linux or macOS machine that can reach the server on port
8377. It needs only `curl`.

```sh
curl -fsSL http://<server>:8377/install.sh | sh
```

`<server>` is the server's hostname or IP address. The script:

- downloads the binary for this OS and CPU into `~/.local/bin`
- detects the harnesses on this machine and prints what it found
- starts the client as a systemd user service on Linux or a launchd agent on
  macOS

The first sync runs right away. From then on the machine follows the server.
Re-run the same command to upgrade the client after a `make dist` on the
server. A re-run keeps your config.

### 3. Check it

```sh
agentsync link --status --dir ~/.agentsync/mirror   # on a client
agentsync link --status                             # on the server, in the repo
```

Every line should end in `linked`. Client logs are in `journalctl --user -u
agentsync -f` on Linux and `~/Library/Logs/agentsync.log` on macOS.

## Day to day

Edit `AGENTS.md` or anything under `skills/` on the server. Clients pick the
change up within 30 seconds. To push it right away, run this on the server:

```sh
agentsync notify
```

Sync is one-way. A client overwrites local edits to its mirror on the next
pass, so make changes on the server.

## Harness paths

`agentsync init` looks for each harness at its default location and writes the
ones it finds to `~/.agentsync/config.json`. The defaults are the same on Linux
and macOS.

| Harness | Found when this exists | Instructions | Skills |
| --- | --- | --- | --- |
| Claude Code | `~/.claude` | `~/.claude/CLAUDE.md` | `~/.claude/skills` |
| Codex CLI | `~/.codex` | `~/.codex/AGENTS.md` | `~/.codex/skills` |
| Gemini CLI | `~/.gemini/settings.json` | `~/.gemini/GEMINI.md` | `~/.gemini/skills` |
| Antigravity | `~/.gemini/config` | `~/.gemini/GEMINI.md` | `~/.gemini/config/skills` |
| Antigravity in T3 Code | `~/.t3/userdata/providers/antigravity/<profile>` | `<profile>/GEMINI.md` | `<profile>/config/skills` |

`$CLAUDE_CONFIG_DIR` and `$CODEX_HOME` replace `~/.claude` and `~/.codex` when
set. A harness that is not installed gets no links and no directories.

If `init` misses a harness, or you install one later, add it to the config by
hand. Paths must be absolute, and either path can be left out.

```json
{
  "harnesses": [
    {
      "name": "claude",
      "instructions": "/Users/me/.claude/CLAUDE.md",
      "skills": "/Users/me/.claude/skills"
    }
  ]
}
```

The client reads the config when it starts. After an edit, re-run the install
command on a client, or `agentsync link` in the repo on the server. `init`
never overwrites an existing config. Delete the file and run `agentsync init`
to detect from scratch.

## How sync works

The server hashes `AGENTS.md` and every file under `skills/` into one manifest
hash. A client sends its own hash to `GET /manifest?known=<hash>`. If the hashes
differ, the server returns the manifest, and the client downloads the files
that changed, deletes the ones that are gone, and relinks. If the hashes match,
the server holds the request open for up to 50 seconds and answers the moment
its hash changes.

The server learns about a change in two ways. `agentsync notify` makes it
rescan now. It also rescans on its own every 30 seconds, so an edit you never
announce still reaches every client.

## Commands

```sh
agentsync init
agentsync serve  [--dir .] [--addr 0.0.0.0:8377] [--interval 30s]
agentsync notify [--server http://localhost:8377]
agentsync sync   --server http://<server>:8377 [--once]
agentsync link   [--dir .] [--status | --uninstall]
```

`link` removes links to skills that no longer exist. Do not run `agentsync
sync` on the server machine. It would repoint the harness links from the repo
to a mirror of it.

`make check` runs gofmt, vet, and the tests.

## Uninstall

On a Linux client:

```sh
systemctl --user disable --now agentsync
agentsync link --uninstall --dir ~/.agentsync/mirror
rm -r ~/.agentsync ~/.local/bin/agentsync ~/.config/systemd/user/agentsync.service
```

On a macOS client, replace the first and last lines:

```sh
launchctl unload ~/Library/LaunchAgents/com.agentsync.client.plist
agentsync link --uninstall --dir ~/.agentsync/mirror
rm -r ~/.agentsync ~/.local/bin/agentsync ~/Library/LaunchAgents/com.agentsync.client.plist
```

On the server, run `systemctl --user disable --now agentsync-server` and
`agentsync link --uninstall` in the repo.

`--uninstall` removes only the links agentsync made. The `.bak.<timestamp>`
files from the first link are still there if you want your old files back.

## Trust

The server has no authentication and speaks plain HTTP. Anyone who can reach
port 8377 can read the instructions and skills, and a client runs its agents on
whatever the server sends. Keep both ends on a private network such as a
tailnet.

## Scope

These are global instructions and apply to every project. Repo-local
`CLAUDE.md` and `AGENTS.md` files still load on top of them and take precedence
for anything project-specific.
