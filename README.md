# agentsync

Keep your AI agent instructions and skills identical on every machine.

You keep one `AGENTS.md` and one `skills/` directory on a machine you pick as
the server. Every other machine runs a client that mirrors them and symlinks
them into the agent harnesses it finds: Claude Code, Codex CLI, Gemini CLI, and
Antigravity. Edit on the server and every machine has the change within 30
seconds, or within a second if you run `agentsync notify`.

`agentsync` is a single Go binary with no dependencies. This repo ships the
program only. Your instructions and skills sit in your checkout, and
`.gitignore` keeps them out of git.

Tested with a Linux server and with Linux and macOS clients. There is no
Windows support.

## Setup

### 1. Server

The server is the machine where you edit your instructions and skills. It
needs Linux with systemd, Go 1.22 or newer, and `make`.

```sh
git clone https://github.com/NickAwrist/agentsync.git
cd agentsync
```

Put your instructions and skills in the checkout:

```
agentsync/
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
- detects the harnesses on this machine and links your checkout into them

It ends by printing the install command for your clients. Anything already at a
harness path, such as an existing `~/.claude/CLAUDE.md`, is moved to
`<path>.bak.<timestamp>` and not deleted.

A systemd user service stops when you log out unless lingering is on. Turn it
on once with `loginctl enable-linger`.

Without systemd, run `make install dist`, start `agentsync serve` however you
like, and run `agentsync init && agentsync link`.

### 2. Clients

A client is any other Linux or macOS machine that can reach the server on port
8377. It needs only `curl`.

```sh
curl -fsSL http://<server>:8377/install.sh | sh
```

`<server>` is the server's hostname or IP address. The server writes that same
address into the script it sends back, so there is nothing to configure. The
script:

- downloads the binary for this OS and CPU into `~/.local/bin`
- detects the harnesses on this machine and prints what it found
- starts the client as a systemd user service on Linux or a launchd agent on
  macOS

The first sync runs right away. From then on the machine follows the server.

Re-run the same command to upgrade the client after a `make dist` on the
server, or to point the client at a new server address. A re-run keeps your
config.

### 3. Check it

```sh
agentsync link --status --dir ~/.agentsync/mirror   # on a client
agentsync link --status                             # on the server, in the repo
```

Every line should end in `linked`. If the shell cannot find `agentsync`, add
`~/.local/bin` to your `PATH` or call `~/.local/bin/agentsync`.

Client logs are in `journalctl --user -u agentsync -f` on Linux and
`~/Library/Logs/agentsync.log` on macOS.

## Daily use

Edit `AGENTS.md` or anything under `skills/` on the server. Clients pick the
change up within 30 seconds. To push it right away, run this on the server:

```sh
agentsync notify
```

After you add or remove a whole skill, also run `agentsync link` in the repo.
Clients relink themselves, but the server's own harnesses only see a new skill
after that command.

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

These are each harness's global paths. A project's own `CLAUDE.md` or
`AGENTS.md` still loads on top and takes precedence.

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

## Commands

```sh
agentsync init
agentsync serve  [--dir .] [--addr 0.0.0.0:8377] [--interval 30s]
agentsync notify [--server http://localhost:8377]
agentsync sync   --server http://<server>:8377 [--once]
agentsync link   [--dir .] [--status | --uninstall]
```

| Command | What it does |
| --- | --- |
| `init` | Detects this machine's harnesses and writes `~/.agentsync/config.json`. |
| `serve` | Serves `AGENTS.md` and `skills/` from a directory, plus the client installer. |
| `notify` | Tells the server to rescan now, so clients pull right away. |
| `sync` | Mirrors the server into `~/.agentsync/mirror` and links it. Runs until stopped. |
| `link` | Links a local directory into the harnesses. This is what the server machine uses. |

Do not run `agentsync sync` on the server machine. It would repoint the harness
links from the repo to a mirror of it.

## How it works

The server hashes `AGENTS.md` and every file under `skills/` into one manifest
hash. A client sends its own hash to `GET /manifest?known=<hash>`. If the hashes
differ, the server returns the manifest, and the client downloads the files
that changed, deletes the ones that are gone, and relinks. If the hashes match,
the server holds the request open for up to 50 seconds and answers the moment
its hash changes.

The server learns about a change in two ways. `agentsync notify` makes it
rescan now. It also rescans on its own every 30 seconds, so an edit you never
announce still reaches every client.

The client checks each download against the hash in the manifest and writes it
through a temp file, so a harness never reads a half-written file.

## Uninstall

On a Linux client:

```sh
systemctl --user disable --now agentsync
agentsync link --uninstall --dir ~/.agentsync/mirror
rm -r ~/.agentsync ~/.local/bin/agentsync ~/.config/systemd/user/agentsync.service
```

On a macOS client:

```sh
launchctl unload ~/Library/LaunchAgents/com.agentsync.client.plist
agentsync link --uninstall --dir ~/.agentsync/mirror
rm -r ~/.agentsync ~/.local/bin/agentsync ~/Library/LaunchAgents/com.agentsync.client.plist
```

On the server, run `systemctl --user disable --now agentsync-server` and
`agentsync link --uninstall` in the repo.

`--uninstall` removes only the links agentsync made. The `.bak.<timestamp>`
files from the first link are still there if you want your old files back.

## Security

The server has no authentication and speaks plain HTTP. Anyone who can reach
port 8377 can read your instructions and skills, and a client runs its agents
on whatever the server sends. Keep both ends on a private network such as a
tailnet, and do not expose the port to the internet.

## Development

`make check` runs gofmt, vet, and the tests.

## License

[0BSD](LICENSE). Use it however you like, with no conditions.
