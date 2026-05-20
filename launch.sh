#!/bin/bash
# Launch the Hermes Agent Dashboard (Go binary, no Chromium kiosk).
#
# The binary is the GUI: no separate backend, no HTTP server, no browser.
# Build it with `go build -o dashboard ./cmd/dashboard` from the repo root.

set -e

DIR="$(cd "$(dirname "$0")" && pwd)"
BIN="$DIR/dashboard"

if [ ! -x "$BIN" ]; then
    echo "dashboard binary not found at $BIN" >&2
    echo "build with: go build -o $BIN ./cmd/dashboard" >&2
    exit 1
fi

# Ensure XWayland session vars are present when launched from a non-graphical
# context (e.g. systemd). Inside an interactive shell these are already set.
: "${DISPLAY:=:0}"
: "${XAUTHORITY:=$HOME/.Xauthority}"
export DISPLAY XAUTHORITY

exec "$BIN" "$@"
