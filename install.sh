#!/usr/bin/env bash
set -euo pipefail

# Oochy installer
# Usage: curl -fsSL https://raw.githubusercontent.com/jinto/oochy/main/install.sh | bash

REPO="jinto/oochy"
VERSION="${OOCHY_VERSION:-latest}"
INSTALL_DIR="${OOCHY_INSTALL_DIR:-$HOME/.local/bin}"

main() {
    echo "Installing Oochy..."
    echo ""

    # Detect OS
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    case "$OS" in
        linux)  OS="linux" ;;
        darwin) OS="macos" ;;
        *)
            echo "Error: Unsupported OS: $OS"
            exit 1
            ;;
    esac

    # Detect architecture
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64|amd64)  ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        *)
            echo "Error: Unsupported architecture: $ARCH"
            exit 1
            ;;
    esac

    BINARY="oochy-${OS}-${ARCH}"

    # Get download URL
    if [ "$VERSION" = "latest" ]; then
        DOWNLOAD_URL="https://github.com/${REPO}/releases/latest/download/${BINARY}"
    else
        DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${BINARY}"
    fi

    echo "  OS:      $OS"
    echo "  Arch:    $ARCH"
    echo "  Binary:  $BINARY"
    echo ""

    # Create install directory
    mkdir -p "$INSTALL_DIR"

    # Download
    echo "Downloading from $DOWNLOAD_URL..."
    if command -v curl &> /dev/null; then
        curl -fsSL "$DOWNLOAD_URL" -o "${INSTALL_DIR}/oochy"
    elif command -v wget &> /dev/null; then
        wget -q "$DOWNLOAD_URL" -O "${INSTALL_DIR}/oochy"
    else
        echo "Error: curl or wget required"
        exit 1
    fi

    # Make executable
    chmod +x "${INSTALL_DIR}/oochy"

    echo ""
    echo "Oochy installed to ${INSTALL_DIR}/oochy"
    echo ""

    # Check if install dir is in PATH
    if [[ ":$PATH:" != *":${INSTALL_DIR}:"* ]]; then
        echo "Add to your PATH:"
        echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
        echo ""
    fi

    echo "Get started:"
    echo "  oochy init                              # Set up API key"
    echo "  oochy teach \"send me a daily joke\"      # Teach a skill"
    echo "  oochy serve                              # Start the bot"
    echo ""
    echo "Happy conversational programming!"
}

main "$@"
