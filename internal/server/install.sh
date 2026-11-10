#!/bin/sh
#
# Install the agentsync client from this server and run it as a user service.
# Safe to re-run: it replaces the binary, keeps an existing config, and
# restarts the service.
#
#   curl -fsSL __SERVER__/install.sh | sh

set -eu

SERVER="__SERVER__"

case "$(uname -s)" in
  Linux)  os=linux ;;
  Darwin) os=darwin ;;
  *) echo "agentsync: unsupported OS $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64)  arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "agentsync: unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac

bin="$HOME/.local/bin/agentsync"
mkdir -p "$(dirname "$bin")"
curl -fsSL "$SERVER/bin/agentsync-$os-$arch" -o "$bin.tmp"
chmod +x "$bin.tmp"
mv "$bin.tmp" "$bin"
echo "installed $bin"
case ":$PATH:" in
  *":$HOME/.local/bin:"*) ;;
  *) echo "note: $HOME/.local/bin is not on your PATH" ;;
esac

# Detect the agent harnesses on this machine and record their paths. The
# output says which were found and where to add any it missed.
"$bin" init

if [ "$os" = linux ] && ! systemctl --user show-environment >/dev/null 2>&1; then
  echo "no systemd user session here, so no service was installed. Start the client yourself:"
  echo "  $bin sync --server $SERVER"
elif [ "$os" = linux ]; then
  unit="$HOME/.config/systemd/user/agentsync.service"
  mkdir -p "$(dirname "$unit")"
  cat >"$unit" <<EOF
[Unit]
Description=agentsync client
After=network-online.target

[Service]
ExecStart=$bin sync --server $SERVER
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
EOF
  systemctl --user daemon-reload
  systemctl --user enable agentsync.service
  systemctl --user restart agentsync.service
  echo "started agentsync.service (logs: journalctl --user -u agentsync -f)"
else
  plist="$HOME/Library/LaunchAgents/com.agentsync.client.plist"
  log="$HOME/Library/Logs/agentsync.log"
  mkdir -p "$(dirname "$plist")"
  launchctl unload "$plist" 2>/dev/null || true
  cat >"$plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.agentsync.client</string>
  <key>ProgramArguments</key>
  <array>
    <string>$bin</string>
    <string>sync</string>
    <string>--server</string>
    <string>$SERVER</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>$log</string>
  <key>StandardErrorPath</key>
  <string>$log</string>
</dict>
</plist>
EOF
  launchctl load -w "$plist"
  echo "started com.agentsync.client (logs: tail -f $log)"
fi
