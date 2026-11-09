#!/usr/bin/env sh
#
# Installs the vyzorix CLI from a GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/VinnsEdesigner/vyzorix-cli/main/install.sh | sh
#
# The download is verified against the release's SHA256SUMS before it is moved
# onto PATH, so a corrupted or swapped asset is refused rather than installed.
#
# Environment:
#   VYZORIX_VERSION  a tag, e.g. v0.1.0 (default: the latest release)
#   PREFIX           install directory (default: ~/.local/bin)
set -eu

REPO=${VYZORIX_REPO:-VinnsEdesigner/vyzorix-cli}
PREFIX=${PREFIX:-"$HOME/.local/bin"}
VERSION=${VYZORIX_VERSION:-}

die() {
  echo "install: $*" >&2
  exit 1
}

# --- platform ---------------------------------------------------------------

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux|darwin) ;;
  *) die "unsupported OS '$os'; build from source: go install github.com/$REPO@latest" ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported architecture '$arch'; build from source: go install github.com/$REPO@latest" ;;
esac

asset="vyzorix-$os-$arch"
base="https://github.com/$REPO/releases"
if [ -n "$VERSION" ]; then
  url="$base/download/$VERSION"
else
  url="$base/latest/download"
fi

# --- download ---------------------------------------------------------------

command -v curl >/dev/null 2>&1 || die "curl is required"

workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

curl -fsSL "$url/$asset" -o "$workdir/$asset" \
  || die "could not download $asset from $url"
curl -fsSL "$url/SHA256SUMS" -o "$workdir/SHA256SUMS" \
  || die "could not download SHA256SUMS from $url"

# --- verify -----------------------------------------------------------------

(
  cd "$workdir"
  # sha256sum on Linux, shasum on macOS.
  if command -v sha256sum >/dev/null 2>&1; then
    grep " $asset\$" SHA256SUMS | sha256sum -c -
  else
    grep " $asset\$" SHA256SUMS | shasum -a 256 -c -
  fi
) || die "checksum verification failed; the download was refused"

# --- install ----------------------------------------------------------------

mkdir -p "$PREFIX"
install -m 0755 "$workdir/$asset" "$PREFIX/vyzorix"

echo "Installed vyzorix to $PREFIX/vyzorix"
"$PREFIX/vyzorix" version || true

case ":$PATH:" in
  *":$PREFIX:"*) ;;
  *)
    echo ""
    echo "Warning: $PREFIX is not on PATH. Add it with:"
    echo "  export PATH=\"$PREFIX:\$PATH\""
    ;;
esac
