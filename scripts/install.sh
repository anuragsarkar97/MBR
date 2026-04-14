#!/usr/bin/env sh
# mbr install script
# Usage: curl -sSfL https://raw.githubusercontent.com/anuragsarkar97/mbr/main/scripts/install.sh | sh
#
# Environment variables:
#   MBR_INSTALL_DIR   Installation directory (default: /usr/local/bin)
#   MBR_VERSION       Specific version to install (default: latest)

set -e

REPO="anuragsarkar97/mbr"
BINARY="mbr"
INSTALL_DIR="${MBR_INSTALL_DIR:-/usr/local/bin}"
VERSION="${MBR_VERSION:-}"

# ── Detect OS and architecture ────────────────────────────────────────────────

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$OS" in
  linux|darwin) ;;
  mingw*|msys*|cygwin*) OS="windows" ;;
  *)
    echo "Unsupported OS: $OS" >&2
    exit 1
    ;;
esac

case "$ARCH" in
  x86_64)        ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac

# ── Resolve version ───────────────────────────────────────────────────────────

if [ -z "$VERSION" ]; then
  echo "Fetching latest release…"
  VERSION=$(curl -sSfL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' \
    | sed -E 's/.*"([^"]+)".*/\1/')
  if [ -z "$VERSION" ]; then
    echo "Could not determine latest version. Set MBR_VERSION to install a specific version." >&2
    exit 1
  fi
fi

echo "Installing ${BINARY} ${VERSION} for ${OS}/${ARCH}…"

# ── Download and extract ──────────────────────────────────────────────────────

EXT="tar.gz"
[ "$OS" = "windows" ] && EXT="zip"

FILENAME="${BINARY}_${OS}_${ARCH}.${EXT}"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${FILENAME}"
TMP_DIR=$(mktemp -d)
TMP_ARCHIVE="${TMP_DIR}/${FILENAME}"

echo "Downloading ${URL}…"
curl -sSfL "$URL" -o "$TMP_ARCHIVE"

echo "Extracting…"
if [ "$EXT" = "zip" ]; then
  unzip -q "$TMP_ARCHIVE" -d "$TMP_DIR"
else
  tar -xzf "$TMP_ARCHIVE" -C "$TMP_DIR"
fi

TMP_BINARY="${TMP_DIR}/${BINARY}"
[ "$OS" = "windows" ] && TMP_BINARY="${TMP_BINARY}.exe"
chmod +x "$TMP_BINARY"

# ── Install binary ────────────────────────────────────────────────────────────

DEST="${INSTALL_DIR}/${BINARY}"
[ "$OS" = "windows" ] && DEST="${DEST}.exe"

echo "Installing to ${DEST}…"
if [ -w "$INSTALL_DIR" ]; then
  mv "$TMP_BINARY" "$DEST"
else
  echo "(needs sudo to write to ${INSTALL_DIR})"
  sudo mv "$TMP_BINARY" "$DEST"
fi

# ── Cleanup ───────────────────────────────────────────────────────────────────

rm -rf "$TMP_DIR"

# ── Verify ────────────────────────────────────────────────────────────────────

echo ""
echo "Installed successfully:"
"$DEST" version

# ── PATH check ────────────────────────────────────────────────────────────────

echo ""
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*)
    echo "✓ ${INSTALL_DIR} is already in your PATH."
    echo "  Run 'mbr' to get started."
    ;;
  *)
    echo "⚠  ${INSTALL_DIR} is not in your PATH."
    echo ""

    # Detect shell and suggest the right rc file.
    SHELL_NAME=$(basename "${SHELL:-}")
    case "$SHELL_NAME" in
      zsh)
        RC_FILE="$HOME/.zshrc"
        ;;
      fish)
        RC_FILE="$HOME/.config/fish/config.fish"
        ;;
      *)
        # Default to .bashrc; covers bash and anything unknown.
        RC_FILE="$HOME/.bashrc"
        if [ "$(uname -s)" = "Darwin" ] && [ ! -f "$HOME/.bashrc" ]; then
          RC_FILE="$HOME/.bash_profile"
        fi
        ;;
    esac

    if [ "$SHELL_NAME" = "fish" ]; then
      ADD_CMD="fish_add_path ${INSTALL_DIR}"
    else
      ADD_CMD="export PATH=\"\$PATH:${INSTALL_DIR}\""
    fi

    echo "  Add ${INSTALL_DIR} to your PATH by running:"
    echo ""
    echo "    echo '${ADD_CMD}' >> ${RC_FILE}"
    echo "    source ${RC_FILE}"
    echo ""
    echo "  Or add this line to ${RC_FILE} manually:"
    echo ""
    echo "    ${ADD_CMD}"
    echo ""
    echo "  Then run 'mbr' to get started."
    ;;
esac
