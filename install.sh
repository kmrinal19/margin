#!/bin/sh
# margin installer — downloads the right prebuilt binary from GitHub Releases,
# verifies its checksum, and installs it. Pure POSIX sh; no dependencies beyond
# curl (or wget) and tar. Installs WITHOUT setting a quarantine flag, so macOS
# Gatekeeper / Windows SmartScreen never fire (unlike a browser download).
#
#   curl -fsSL https://raw.githubusercontent.com/kmrinal19/margin/main/install.sh | sh
#
# Override the target dir with MARGIN_INSTALL_DIR, or the version with MARGIN_VERSION.
set -eu

REPO="kmrinal19/margin"
BIN="margin"

say()  { printf '  %s\n' "$*"; }
err()  { printf 'margin install: %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

# ── fetch helper (curl or wget) ───────────────────────────────────────────────
if have curl; then
  dl() { curl -fsSL "$1"; }
  dlo() { curl -fsSL -o "$2" "$1"; }
elif have wget; then
  dl() { wget -qO- "$1"; }
  dlo() { wget -qO "$2" "$1"; }
else
  err "need curl or wget"
fi
have tar || err "need tar"

# ── detect OS/arch ────────────────────────────────────────────────────────────
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux) os=linux ;;
  darwin) os=darwin ;;
  *) err "unsupported OS: $os (Windows: use 'scoop install' or download from the Releases page)" ;;
esac
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) err "unsupported architecture: $arch" ;;
esac

# ── resolve version ───────────────────────────────────────────────────────────
ver="${MARGIN_VERSION:-}"
if [ -z "$ver" ]; then
  ver=$(dl "https://api.github.com/repos/$REPO/releases/latest" \
        | grep -m1 '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
  [ -n "$ver" ] || err "could not determine the latest version (set MARGIN_VERSION=vX.Y.Z)"
fi
num=${ver#v}

base="https://github.com/$REPO/releases/download/$ver"
archive="${BIN}_${num}_${os}_${arch}.tar.gz"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

say "downloading $archive ($ver)…"
dlo "$base/$archive" "$tmp/$archive" || err "download failed: $base/$archive"

# ── verify checksum (best-effort: skip if checksums.txt is unavailable) ───────
if dlo "$base/checksums.txt" "$tmp/checksums.txt" 2>/dev/null; then
  want=$(grep " $archive\$" "$tmp/checksums.txt" | awk '{print $1}')
  if [ -n "$want" ]; then
    if have sha256sum; then got=$(sha256sum "$tmp/$archive" | awk '{print $1}');
    elif have shasum; then got=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}');
    else got=""; fi
    if [ -n "$got" ] && [ "$got" != "$want" ]; then
      err "checksum mismatch for $archive (expected $want, got $got)"
    fi
    [ -n "$got" ] && say "checksum verified"
  fi
fi

tar -xzf "$tmp/$archive" -C "$tmp" || err "extract failed"
[ -f "$tmp/$BIN" ] || err "archive did not contain '$BIN'"

# ── choose an install dir on PATH ─────────────────────────────────────────────
dir="${MARGIN_INSTALL_DIR:-}"
if [ -z "$dir" ]; then
  if [ -w /usr/local/bin ] 2>/dev/null; then dir=/usr/local/bin
  else dir="$HOME/.local/bin"; fi
fi
mkdir -p "$dir"
install -m 0755 "$tmp/$BIN" "$dir/$BIN" 2>/dev/null || { cp "$tmp/$BIN" "$dir/$BIN" && chmod 0755 "$dir/$BIN"; }

say "installed $BIN $ver → $dir/$BIN"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) say "NOTE: $dir is not on your PATH — add it:  export PATH=\"$dir:\$PATH\"" ;;
esac
printf '\n'
say "next:  margin serve            # review docs in your browser"
say "       margin agent-setup      # let your AI agent address comments"
