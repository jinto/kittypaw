#!/bin/sh
set -e

REPO="kittypaw-app/kittypaw"
BINARY="kittypaw"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

restart_standalone_daemon() {
  BIN_PATH="$1"

  if [ ! -x "$BIN_PATH" ]; then
    return
  fi

  if "$BIN_PATH" daemon status 2>/dev/null | grep -q "Daemon is running"; then
    echo "  Restarting running kittypaw daemon..."
    if "$BIN_PATH" daemon stop >/dev/null 2>&1 && "$BIN_PATH" daemon start >/dev/null 2>&1; then
      echo "  ✓ kittypaw daemon restarted"
    else
      echo "  ! Installed, but failed to restart the daemon."
      echo "    Run: kittypaw daemon stop && kittypaw daemon start"
    fi
  fi
}

restart_after_install() {
  BIN_PATH="$1"

  case "$OS" in
    darwin)
      if command -v launchctl >/dev/null 2>&1; then
        TARGET="gui/$(id -u)/dev.kittypaw.daemon"
        if launchctl print "$TARGET" >/dev/null 2>&1; then
          echo "  Restarting running kittypaw service..."
          if launchctl kickstart -k "$TARGET" >/dev/null 2>&1; then
            echo "  ✓ kittypaw service restarted"
          else
            echo "  ! Installed, but failed to restart the service."
            echo "    Run: launchctl kickstart -k $TARGET"
          fi
          return
        fi
      fi
      ;;
    linux)
      if command -v systemctl >/dev/null 2>&1; then
        if systemctl --user is-active --quiet kittypaw.service >/dev/null 2>&1; then
          echo "  Restarting running kittypaw service..."
          if systemctl --user restart kittypaw.service >/dev/null 2>&1; then
            echo "  ✓ kittypaw service restarted"
          else
            echo "  ! Installed, but failed to restart the service."
            echo "    Run: systemctl --user restart kittypaw.service"
          fi
          return
        fi
      fi
      ;;
  esac

  restart_standalone_daemon "$BIN_PATH"
}

# ----- detect platform -----

OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Linux*)  OS="linux"  ;;
  Darwin*) OS="darwin" ;;
  *)       echo "Unsupported OS: $OS"; exit 1 ;;
esac

case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  arm64)   ARCH="arm64" ;;
  *)       echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# ----- resolve version -----

if [ -z "$VERSION" ]; then
  VERSION="$(curl -fsSL --proto '=https' --tlsv1.2 \
    "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' | head -1 | sed 's/.*"v\(.*\)".*/\1/')"
fi

VERSION="${VERSION#v}"

if [ -z "$VERSION" ]; then
  echo "Failed to determine latest version" >&2; exit 1
fi

echo "Installing ${BINARY} v${VERSION} (${OS}/${ARCH})..."

# ----- download & verify -----

TARBALL="${BINARY}_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/${REPO}/releases/download/v${VERSION}"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

curl -fsSL --proto '=https' --tlsv1.2 "${BASE_URL}/${TARBALL}" -o "${TMPDIR}/${TARBALL}"
curl -fsSL --proto '=https' --tlsv1.2 "${BASE_URL}/checksums.txt" -o "${TMPDIR}/checksums.txt"

# verify checksum
cd "$TMPDIR"
if command -v shasum >/dev/null 2>&1; then
  grep -F "$TARBALL" checksums.txt | shasum -a 256 -c --quiet
elif command -v sha256sum >/dev/null 2>&1; then
  grep -F "$TARBALL" checksums.txt | sha256sum -c --quiet
else
  echo "Error: sha256 verification requires sha256sum or shasum." >&2
  exit 1
fi

# ----- install -----

tar xzf "$TARBALL"

if [ -w "$INSTALL_DIR" ]; then
  mv "$BINARY" "$INSTALL_DIR/"
else
  echo "Need sudo to install to ${INSTALL_DIR}"
  sudo mv "$BINARY" "$INSTALL_DIR/"
fi

case "$INSTALL_DIR" in
  */) INSTALLED_BIN="${INSTALL_DIR}${BINARY}" ;;
  *)  INSTALLED_BIN="${INSTALL_DIR}/${BINARY}" ;;
esac

echo ""
echo "  ✓ ${BINARY} v${VERSION} installed"
restart_after_install "$INSTALLED_BIN"
echo ""
echo "  Get started:"
echo "    kittypaw setup"
echo ""
