#!/bin/sh
set -e

REPO="jinto/kittypaw"
BINARY="kittypaw"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

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
if command -v sha256sum >/dev/null 2>&1; then
  grep -F "$TARBALL" checksums.txt | sha256sum -c --quiet
elif command -v shasum >/dev/null 2>&1; then
  grep -F "$TARBALL" checksums.txt | shasum -a 256 -c --quiet
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

echo "Installed ${BINARY} v${VERSION} to ${INSTALL_DIR}/${BINARY}"
echo "Run '${BINARY} init' to get started."
