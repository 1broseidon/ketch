#!/bin/sh
# ketch installer
#
#   curl -fsSL https://ketch.run/install | sh
#   curl -fsSL https://ketch.run/install | sh -s -- --version v0.17.0
#   curl -fsSL https://ketch.run/install | sh -s -- --bin-dir /opt/bin
#
# Environment equivalents: KETCH_VERSION, KETCH_INSTALL_DIR.
# Every download is verified against the release's checksums.txt before install.

set -eu

REPO="1broseidon/ketch"
BINARY="ketch"

VERSION="${KETCH_VERSION:-}"
BIN_DIR="${KETCH_INSTALL_DIR:-}"

say() { printf '%s\n' "$*"; }
info() { printf '  %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

usage() {
  cat >&2 <<EOF
ketch installer

Usage: install.sh [options]

Options:
  --version <tag>   Install a specific release (default: latest)
  --bin-dir <dir>   Install into <dir> (default: /usr/local/bin if writable,
                    otherwise \$HOME/.local/bin)
  -h, --help        Show this help

Environment:
  KETCH_VERSION       same as --version
  KETCH_INSTALL_DIR   same as --bin-dir
EOF
}

have() { command -v "$1" >/dev/null 2>&1; }

detect_os() {
  os=$(uname -s)
  case "$os" in
    Linux) echo linux ;;
    Darwin) echo darwin ;;
    MINGW*|MSYS*|CYGWIN*|Windows_NT)
      die "Windows detected. Install with 'winget install ketch', 'scoop install ketch', or grab a zip from https://github.com/$REPO/releases/latest" ;;
    *) die "unsupported operating system: $os" ;;
  esac
}

detect_arch() {
  arch=$(uname -m)
  case "$arch" in
    x86_64|amd64) echo x86_64 ;;
    arm64|aarch64) echo arm64 ;;
    *) die "unsupported architecture: $arch (ketch ships x86_64 and arm64; 'go install github.com/$REPO@latest' will build from source)" ;;
  esac
}

download() {
  # download <url> <dest>
  if have curl; then
    curl -fsSL --retry 3 --retry-delay 1 -o "$2" "$1" || die "download failed: $1"
  elif have wget; then
    wget -q -O "$2" "$1" || die "download failed: $1"
  else
    die "need curl or wget"
  fi
}

latest_version() {
  # Follow the /releases/latest redirect instead of the API: no rate limit, no token.
  url=""
  if have curl; then
    url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" 2>/dev/null || true)
  elif have wget; then
    url=$(wget -q -S --spider --max-redirect=10 "https://github.com/$REPO/releases/latest" 2>&1 |
      awk '/^  *Location:/ { print $2 }' | tail -n 1 || true)
  fi
  case "$url" in
    */releases/tag/*) basename "$url" ;;
    *) die "could not determine the latest version; pass --version <tag>" ;;
  esac
}

sha256_of() {
  if have sha256sum; then sha256sum "$1" | awk '{ print $1 }'
  elif have shasum; then shasum -a 256 "$1" | awk '{ print $1 }'
  elif have openssl; then openssl dgst -sha256 "$1" | awk '{ print $NF }'
  else die "need sha256sum, shasum, or openssl to verify the download"
  fi
}

choose_bin_dir() {
  if [ -n "$BIN_DIR" ]; then
    echo "$BIN_DIR"
  elif [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
    echo /usr/local/bin
  else
    echo "$HOME/.local/bin"
  fi
}

while [ $# -gt 0 ]; do
  case "$1" in
    --version) [ $# -ge 2 ] || die "--version needs a value"; VERSION="$2"; shift 2 ;;
    --version=*) VERSION="${1#*=}"; shift ;;
    --bin-dir) [ $# -ge 2 ] || die "--bin-dir needs a value"; BIN_DIR="$2"; shift 2 ;;
    --bin-dir=*) BIN_DIR="${1#*=}"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) usage; die "unknown option: $1" ;;
  esac
done

OS=$(detect_os)
ARCH=$(detect_arch)

[ -n "$VERSION" ] || VERSION=$(latest_version)
case "$VERSION" in v*) ;; *) VERSION="v$VERSION" ;; esac
NUM_VERSION="${VERSION#v}"

ASSET="${BINARY}_${NUM_VERSION}_${OS}_${ARCH}.tar.gz"
BASE="https://github.com/$REPO/releases/download/$VERSION"

TMP=$(mktemp -d 2>/dev/null || mktemp -d -t ketch)
trap 'rm -rf "$TMP"' EXIT INT TERM

say "installing ketch $VERSION ($OS/$ARCH)"

download "$BASE/$ASSET" "$TMP/$ASSET"
download "$BASE/checksums.txt" "$TMP/checksums.txt"

EXPECTED=$(awk -v f="$ASSET" '$2 == f { print $1 }' "$TMP/checksums.txt")
[ -n "$EXPECTED" ] || die "$ASSET is not listed in checksums.txt for $VERSION"

ACTUAL=$(sha256_of "$TMP/$ASSET")
[ "$EXPECTED" = "$ACTUAL" ] || die "checksum mismatch for $ASSET
  expected $EXPECTED
  actual   $ACTUAL"

tar -xzf "$TMP/$ASSET" -C "$TMP" || die "could not extract $ASSET"
[ -f "$TMP/$BINARY" ] || die "$BINARY not found inside $ASSET"

DEST=$(choose_bin_dir)
mkdir -p "$DEST" || die "could not create $DEST"
[ -w "$DEST" ] || die "$DEST is not writable; re-run with --bin-dir <dir>, or: sudo sh -c \"\$(curl -fsSL https://ketch.run/install)\""

# Write beside the target, then rename, so a running ketch is never half-overwritten.
chmod 755 "$TMP/$BINARY"
mv -f "$TMP/$BINARY" "$DEST/$BINARY.new" || die "could not write to $DEST"
mv -f "$DEST/$BINARY.new" "$DEST/$BINARY" || die "could not install to $DEST/$BINARY"

say "installed $DEST/$BINARY"

case ":$PATH:" in
  *":$DEST:"*) ;;
  *)
    say ""
    say "$DEST is not on your PATH. Add it:"
    say "  export PATH=\"$DEST:\$PATH\""
    ;;
esac

if [ -x "$DEST/$BINARY" ]; then
  say ""
  "$DEST/$BINARY" version 2>/dev/null || true
fi
