#!/bin/sh
# Register the KittyPaw daemon as a per-user systemd service.
#
# Installs the unit into ~/.config/systemd/user/ so no root is required for
# the common path. Resource ceilings (MemoryMax, CPUQuota, TasksMax) rely on
# cgroup v2 delegation; if the host has not enabled DefaultDelegate=yes the
# directives will be silently ignored — we detect and warn.
#
# Usage:
#   sh packaging/linux/register-service.sh                # default port 3000
#   KITTYPAW_BIND_PORT=3001 sh packaging/linux/register-service.sh
#   KITTYPAW_BIN=/custom/path sh packaging/linux/register-service.sh
#
# After install, logs:
#   journalctl --user -u kittypaw -f

set -eu

SRC_UNIT="$(cd "$(dirname "$0")" && pwd)/systemd/user/kittypaw.service"
DEST_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
DEST_UNIT="$DEST_DIR/kittypaw.service"
KITTYPAW_BIN="${KITTYPAW_BIN:-$(command -v kittypaw 2>/dev/null || echo /usr/local/bin/kittypaw)}"
KITTYPAW_BIND_PORT="${KITTYPAW_BIND_PORT:-3000}"

if [ ! -f "$SRC_UNIT" ]; then
  echo "error: unit template not found: $SRC_UNIT" >&2
  exit 1
fi

if [ ! -x "$KITTYPAW_BIN" ]; then
  echo "warning: kittypaw binary not found or not executable at: $KITTYPAW_BIN" >&2
  echo "         set KITTYPAW_BIN=... or install the binary first." >&2
fi

# --- Port conflict detection -------------------------------------------------
# Stop our own service (if already installed) before checking — a repeat
# install shouldn't be blocked by the previous run's listener.
if systemctl --user is-active --quiet kittypaw.service 2>/dev/null; then
  systemctl --user stop kittypaw.service >/dev/null 2>&1 || true
fi

port_in_use() {
  _port="$1"
  if command -v ss >/dev/null 2>&1; then
    ss -lnt "sport = :${_port}" 2>/dev/null | grep -q LISTEN
  elif command -v lsof >/dev/null 2>&1; then
    lsof -nP -iTCP:"${_port}" -sTCP:LISTEN >/dev/null 2>&1
  else
    return 1 # can't detect — proceed and let systemd surface any bind failure
  fi
}

if port_in_use "$KITTYPAW_BIND_PORT"; then
  echo "error: 127.0.0.1:${KITTYPAW_BIND_PORT} is already in use." >&2
  echo "" >&2
  echo "  Another process — likely another OS user's kittypaw daemon — is" >&2
  echo "  bound to this port. Pick a free port and retry:" >&2
  echo "" >&2
  echo "    KITTYPAW_BIND_PORT=3001 sh $(basename "$0")" >&2
  echo "" >&2
  echo "  Then point your client at the same port, e.g.:" >&2
  echo "    kittypaw chat --server http://127.0.0.1:3001" >&2
  exit 1
fi

mkdir -p "$DEST_DIR"

# Rewrite ExecStart to the detected binary path AND substitute the selected
# port so users who installed into ~/.local/bin or who need a non-default
# port get a working unit without hand-editing.
sed \
  -e "s|ExecStart=/usr/local/bin/kittypaw |ExecStart=${KITTYPAW_BIN} |" \
  -e "s|--bind 127.0.0.1:3000|--bind 127.0.0.1:${KITTYPAW_BIND_PORT}|" \
  "$SRC_UNIT" >"$DEST_UNIT"

echo "installed unit: $DEST_UNIT  (bind 127.0.0.1:${KITTYPAW_BIND_PORT})"

systemctl --user daemon-reload
systemctl --user enable --now kittypaw.service

# --- Post-install diagnostics -------------------------------------------------

sleep 1
if ! systemctl --user is-active --quiet kittypaw.service; then
  echo ""
  echo "warning: kittypaw.service is not active. Inspect with:"
  echo "  systemctl --user status kittypaw.service"
  echo "  journalctl --user -u kittypaw -n 50"
fi

# Detect cgroup delegation — resource ceilings won't apply without it.
DELEGATE="$(systemctl --user show -p Delegate --value kittypaw.service 2>/dev/null || echo no)"
if [ "$DELEGATE" != "yes" ]; then
  echo ""
  echo "note: cgroup controller delegation is not enabled for the user manager."
  echo "      MemoryMax / CPUQuota / TasksMax in the unit are ignored until"
  echo "      an administrator runs:"
  echo ""
  echo "        sudo mkdir -p /etc/systemd/system/user-.slice.d"
  echo "        printf '[Slice]\\nDelegate=yes\\n' |"
  echo "          sudo tee /etc/systemd/system/user-.slice.d/10-delegate.conf"
  echo "        sudo systemctl daemon-reload"
  echo ""
  echo "      Then re-login or run: systemctl --user daemon-reexec"
fi

# Linger keeps the user manager alive across logouts so the daemon starts at
# boot and survives SSH disconnect. Requires root.
if ! loginctl show-user "$USER" 2>/dev/null | grep -q '^Linger=yes'; then
  echo ""
  echo "note: linger is not enabled for user '$USER'."
  echo "      Without linger, kittypaw stops when you log out."
  echo "      Enable with:"
  echo ""
  echo "        sudo loginctl enable-linger $USER"
fi

echo ""
echo "done. tail the log with:  journalctl --user -u kittypaw -f"
