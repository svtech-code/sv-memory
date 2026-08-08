#!/bin/sh
# Installer for sv-memory (macOS / Linux)
# Downloads a prebuilt binary from GitHub Releases and installs it to
# $HOME/.local/bin (no sudo required).
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/svtech-code/sv-memory/main/install.sh | bash
#
# Options (environment variables):
#   SV_MEMORY_VERSION       pin a release tag instead of "latest" (e.g. v0.1.0)
#   SV_MEMORY_INSTALL_DIR   override the install directory (default $HOME/.local/bin)
set -e

REPO="svtech-code/sv-memory"
BINARY="sv-memory"
VERSION="${SV_MEMORY_VERSION:-latest}"
INSTALL_DIR="${SV_MEMORY_INSTALL_DIR:-$HOME/.local/bin}"

# 1. Detect OS
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
    darwin) OS="darwin" ;;
    linux) OS="linux" ;;
    *)
        case "$OS" in
            mingw*|msys*|cygwin*|windows*)
                echo "❌ Windows detected. Use the PowerShell installer instead:"
                echo "   iwr -useb https://raw.githubusercontent.com/$REPO/main/install.ps1 | iex"
                ;;
            *)
                echo "❌ Unsupported operating system: $OS" ;;
        esac
        exit 1
        ;;
esac

# 2. Detect architecture
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *)
        echo "❌ Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

# 3. Build the download URL (latest release or pinned tag)
TARBALL="sv-memory_${OS}_${ARCH}.tar.gz"
if [ "$VERSION" = "latest" ]; then
    URL="https://github.com/$REPO/releases/latest/download/$TARBALL"
else
    URL="https://github.com/$REPO/releases/download/${VERSION}/$TARBALL"
fi

VERSION_LABEL=""
if [ "$VERSION" != "latest" ]; then
    VERSION_LABEL=" version $VERSION"
fi
echo "📥 Downloading sv-memory ($OS/$ARCH)$VERSION_LABEL from GitHub Releases..."
TEMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TEMP_DIR"' EXIT

if ! curl -fsSL "$URL" -o "$TEMP_DIR/$TARBALL"; then
    echo "❌ Failed to download $URL"
    echo "   Make sure the release exists, or install from source instead:"
    echo "   go install github.com/svtech-code/sv-memory/cmd/sv-memory@latest"
    exit 1
fi

# 4. Verify the SHA-256 checksum against checksums.txt from the release.
#    Best-effort: releases without a checksums.txt (or platforms without a
#    hashing tool) warn instead of failing, but a mismatched hash aborts.
if [ "$VERSION" = "latest" ]; then
    CHECKSUM_URL="https://github.com/$REPO/releases/latest/download/checksums.txt"
else
    CHECKSUM_URL="https://github.com/$REPO/releases/download/${VERSION}/checksums.txt"
fi

if CHECKSUM_FILE="$(curl -fsSL "$CHECKSUM_URL" 2>/dev/null)"; then
    EXPECTED="$(printf '%s\n' "$CHECKSUM_FILE" | awk -v f="$TARBALL" '$2 == f { print $1; exit }')"
    if [ -z "$EXPECTED" ]; then
        echo "⚠️  checksums.txt found but no entry for $TARBALL — skipping checksum verification"
    else
        if command -v shasum >/dev/null 2>&1; then
            ACTUAL="$(shasum -a 256 "$TEMP_DIR/$TARBALL" | awk '{print $1}')"
        elif command -v sha256sum >/dev/null 2>&1; then
            ACTUAL="$(sha256sum "$TEMP_DIR/$TARBALL" | awk '{print $1}')"
        else
            ACTUAL=""
            echo "⚠️  No shasum/sha256sum available — skipping checksum verification"
        fi
        if [ -n "$ACTUAL" ]; then
            if [ "$ACTUAL" = "$EXPECTED" ]; then
                echo "🔒 Checksum verified (SHA-256): OK"
            else
                echo "❌ Checksum verification FAILED for $TARBALL"
                echo "   expected: $EXPECTED"
                echo "   actual:   $ACTUAL"
                echo "   The download may be corrupt or tampered with. Aborting."
                exit 1
            fi
        fi
    fi
else
    echo "⚠️  Could not fetch $CHECKSUM_URL — skipping checksum verification"
fi

# 5. Extract into the install directory (tarball contains a single binary)
mkdir -p "$INSTALL_DIR"
tar -xzf "$TEMP_DIR/$TARBALL" -C "$INSTALL_DIR" "$BINARY"
chmod +x "$INSTALL_DIR/$BINARY"

# 6. Warn if the install directory is not on PATH
case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *)
        echo ""
        echo "⚠️  $INSTALL_DIR is not in your PATH. Add it to your shell config:"
        case "$(basename "${SHELL:-sh}")" in
            fish)
                echo "   fish_add_path $INSTALL_DIR" ;;
            *)
                echo "   echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.$(basename "${SHELL:-sh}")rc"
                echo "   source ~/.$(basename "${SHELL:-sh}")rc" ;;
        esac
        ;;
esac

echo ""
echo "✅ sv-memory installed to $INSTALL_DIR"
echo "   Run 'sv-memory --help' to get started."
