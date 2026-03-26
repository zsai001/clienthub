#!/usr/bin/env bash
# ClientHub installer / updater
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/cltx/clienthub/main/install.sh | bash
#   curl -fsSL ... | bash -s -- --channel dev
#   curl -fsSL ... | bash -s -- --channel stable --install-dir /usr/local/bin
#
# Options:
#   --channel   stable|dev   (default: stable)
#   --install-dir PATH       (default: ~/.local/bin)
#   --component  all|server|client|hubctl  (default: all)
#   --help

set -euo pipefail

REPO="cltx/clienthub"
CHANNEL="stable"
INSTALL_DIR="${HOME}/.local/bin"
COMPONENT="all"

usage() {
  cat <<EOF
ClientHub Installer

Usage:
  install.sh [OPTIONS]

Options:
  --channel   stable|dev   Release channel (default: stable)
  --install-dir PATH       Install directory (default: ~/.local/bin)
  --component COMP         Component to install: all|server|client|hubctl (default: all)
  --help                   Show this help

Examples:
  # Install stable release
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | bash

  # Install dev build
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | bash -s -- --channel dev

  # Install only the client
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | bash -s -- --component client
EOF
  exit 0
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --channel)   CHANNEL="$2"; shift 2 ;;
    --install-dir) INSTALL_DIR="$2"; shift 2 ;;
    --component) COMPONENT="$2"; shift 2 ;;
    --help)      usage ;;
    *) echo "Unknown option: $1"; usage ;;
  esac
done

if [[ "$CHANNEL" != "stable" && "$CHANNEL" != "dev" ]]; then
  echo "Error: --channel must be 'stable' or 'dev'"
  exit 1
fi

detect_os() {
  local os
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    linux*)  echo "linux" ;;
    darwin*) echo "darwin" ;;
    freebsd*) echo "freebsd" ;;
    msys*|mingw*|cygwin*) echo "windows" ;;
    *) echo "Unsupported OS: $os" >&2; exit 1 ;;
  esac
}

detect_arch() {
  local arch
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64)   echo "amd64" ;;
    aarch64|arm64)   echo "arm64" ;;
    armv7l|armhf)    echo "arm" ;;
    *) echo "Unsupported architecture: $arch" >&2; exit 1 ;;
  esac
}

OS="$(detect_os)"
ARCH="$(detect_arch)"

echo "============================================"
echo "  ClientHub Installer"
echo "============================================"
echo ""
echo "  Channel:    ${CHANNEL}"
echo "  OS:         ${OS}"
echo "  Arch:       ${ARCH}"
echo "  Component:  ${COMPONENT}"
echo "  Install to: ${INSTALL_DIR}"
echo ""

# Determine the release tag to download
if [[ "$CHANNEL" == "dev" ]]; then
  TAG="dev-latest"
  echo "==> Fetching latest dev build ..."
else
  echo "==> Fetching latest stable release ..."
  TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
  if [[ -z "$TAG" ]]; then
    echo "Error: could not find latest stable release."
    echo "  Check https://github.com/${REPO}/releases"
    exit 1
  fi
fi

echo "  Release:    ${TAG}"
echo ""

# Build download URL
EXT="tar.gz"
if [[ "$OS" == "windows" ]]; then EXT="zip"; fi

ARCHIVE="clienthub-${OS}-${ARCH}.${EXT}"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${TAG}/${ARCHIVE}"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

echo "==> Downloading ${ARCHIVE} ..."
if ! curl -fSL --progress-bar -o "${TMPDIR}/${ARCHIVE}" "$DOWNLOAD_URL"; then
  echo ""
  echo "Error: download failed."
  echo "  URL: ${DOWNLOAD_URL}"
  echo ""
  echo "Possible reasons:"
  echo "  - No release found for ${OS}/${ARCH} on the '${CHANNEL}' channel"
  echo "  - Network issue"
  echo ""
  echo "Available releases: https://github.com/${REPO}/releases"
  exit 1
fi

echo "==> Extracting ..."
cd "$TMPDIR"
if [[ "$EXT" == "tar.gz" ]]; then
  tar xzf "$ARCHIVE"
elif [[ "$EXT" == "zip" ]]; then
  unzip -q "$ARCHIVE"
fi

EXTRACTED_DIR="clienthub-${OS}-${ARCH}"

echo "==> Installing to ${INSTALL_DIR} ..."
mkdir -p "$INSTALL_DIR"

install_bin() {
  local name="$1"
  local src="${EXTRACTED_DIR}/${name}"
  if [[ ! -f "$src" ]]; then
    echo "  Warning: ${name} not found in archive, skipping"
    return
  fi
  cp "$src" "${INSTALL_DIR}/${name}"
  chmod +x "${INSTALL_DIR}/${name}"
  echo "  Installed: ${INSTALL_DIR}/${name}"
}

case "$COMPONENT" in
  all)
    install_bin "hub-server"
    install_bin "hub-client"
    install_bin "hubctl"
    ;;
  server)  install_bin "hub-server" ;;
  client)  install_bin "hub-client" ;;
  hubctl)  install_bin "hubctl" ;;
  *)
    echo "Error: unknown component '${COMPONENT}' (use: all|server|client|hubctl)"
    exit 1
    ;;
esac

# Copy example configs if installing all
if [[ "$COMPONENT" == "all" && -d "${EXTRACTED_DIR}/examples" ]]; then
  CONFIG_DIR="${HOME}/.config/clienthub"
  if [[ ! -d "$CONFIG_DIR" ]]; then
    mkdir -p "$CONFIG_DIR"
    cp "${EXTRACTED_DIR}/examples/"*.yaml "$CONFIG_DIR/"
    echo "  Examples:  ${CONFIG_DIR}/"
  fi
fi

# Check PATH
if ! echo "$PATH" | tr ':' '\n' | grep -qx "$INSTALL_DIR"; then
  echo ""
  echo "  NOTE: ${INSTALL_DIR} is not in your PATH."
  SHELL_NAME="$(basename "${SHELL:-/bin/bash}")"
  case "$SHELL_NAME" in
    zsh)  RC_FILE="~/.zshrc" ;;
    bash) RC_FILE="~/.bashrc" ;;
    fish) RC_FILE="~/.config/fish/config.fish" ;;
    *)    RC_FILE="~/.profile" ;;
  esac
  echo "  Add it with:"
  echo ""
  if [[ "$SHELL_NAME" == "fish" ]]; then
    echo "    fish_add_path ${INSTALL_DIR}"
  else
    echo "    echo 'export PATH=\"${INSTALL_DIR}:\$PATH\"' >> ${RC_FILE} && source ${RC_FILE}"
  fi
fi

# Print version
echo ""
if command -v "${INSTALL_DIR}/hubctl" &>/dev/null; then
  VER=$("${INSTALL_DIR}/hubctl" version 2>/dev/null || echo "unknown")
  echo "  Version: ${VER}"
fi

echo ""
echo "============================================"
echo "  Installation complete!"
echo "============================================"
echo ""
echo "  To update, run this script again."
echo "  To switch channels, use --channel dev|stable"
echo ""
