#!/bin/sh
# multimux installer — downloads the latest release binary for your OS/arch.
#
#   curl -fsSL https://raw.githubusercontent.com/jontuk/multimux/main/install.sh | sh
#
# Environment overrides:
#   MULTIMUX_VERSION       release tag to install (default: latest)
#   MULTIMUX_INSTALL_DIR   install directory (default: /usr/local/bin)
set -eu

REPO="jontuk/multimux"
BINARY="multimux"
VERSION="${MULTIMUX_VERSION:-latest}"
INSTALL_DIR="${MULTIMUX_INSTALL_DIR:-/usr/local/bin}"

err() { echo "multimux install: $*" >&2; exit 1; }

# --- pick a downloader -------------------------------------------------------
if command -v curl >/dev/null 2>&1; then
  dl() { curl -fsSL "$1" -o "$2"; }
  dl_stdout() { curl -fsSL "$1"; }
  head_url() { curl -fsSLI -o /dev/null -w '%{url_effective}' "$1"; }
elif command -v wget >/dev/null 2>&1; then
  dl() { wget -qO "$2" "$1"; }
  dl_stdout() { wget -qO - "$1"; }
  head_url() { wget -q -O /dev/null "$1" 2>&1; err "wget cannot resolve latest tag; set MULTIMUX_VERSION"; }
else
  err "need curl or wget"
fi

# --- detect OS / arch --------------------------------------------------------
os="$(uname -s)"
case "$os" in
  Darwin) os="darwin" ;;
  Linux)  os="linux" ;;
  *) err "unsupported OS: $os (darwin and linux only)" ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64)  arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) err "unsupported architecture: $arch" ;;
esac

# --- resolve version ---------------------------------------------------------
if [ "$VERSION" = "latest" ]; then
  # Follow the /releases/latest redirect to read the tag — no API token needed.
  effective="$(head_url "https://github.com/${REPO}/releases/latest")"
  VERSION="${effective##*/tag/}"
  case "$VERSION" in
    v[0-9]*) ;;
    *) err "could not resolve latest release tag (got '$VERSION')" ;;
  esac
fi

# goreleaser names assets without the leading 'v' (e.g. multimux_0.2.0_...).
ver_no_v="${VERSION#v}"
asset="${BINARY}_${ver_no_v}_${os}_${arch}.tar.gz"
base="https://github.com/${REPO}/releases/download/${VERSION}"

echo "multimux install: ${VERSION} ${os}/${arch}"

# --- download + verify -------------------------------------------------------
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

dl "${base}/${asset}" "${tmp}/${asset}" || err "download failed: ${base}/${asset}"

# Verify the SHA-256 checksum when checksums.txt and a hashing tool are present.
if dl "${base}/checksums.txt" "${tmp}/checksums.txt" 2>/dev/null; then
  if command -v sha256sum >/dev/null 2>&1; then
    hash_cmd="sha256sum"
  elif command -v shasum >/dev/null 2>&1; then
    hash_cmd="shasum -a 256"
  else
    hash_cmd=""
  fi
  if [ -n "$hash_cmd" ]; then
    want="$(grep " ${asset}\$" "${tmp}/checksums.txt" | awk '{print $1}')"
    got="$(cd "$tmp" && $hash_cmd "$asset" | awk '{print $1}')"
    [ -n "$want" ] || err "no checksum listed for ${asset}"
    [ "$want" = "$got" ] || err "checksum mismatch for ${asset}"
    echo "multimux install: checksum verified"
  fi
fi

tar xzf "${tmp}/${asset}" -C "$tmp" "$BINARY" || err "extract failed"
chmod +x "${tmp}/${BINARY}"

# --- install -----------------------------------------------------------------
if [ ! -d "$INSTALL_DIR" ]; then
  mkdir -p "$INSTALL_DIR" 2>/dev/null || sudo mkdir -p "$INSTALL_DIR" || err "cannot create $INSTALL_DIR"
fi

if [ -w "$INSTALL_DIR" ]; then
  mv "${tmp}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
else
  echo "multimux install: ${INSTALL_DIR} not writable, using sudo"
  sudo mv "${tmp}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
fi

echo "multimux install: installed to ${INSTALL_DIR}/${BINARY}"

# --- PATH hint + next steps --------------------------------------------------
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *) echo "multimux install: note ${INSTALL_DIR} is not on your PATH" ;;
esac

echo
echo "Next:  multimux serve --hostname mux.example.com   # initial setup: config host and register passkey"
echo "       multimux ca trust                           # trust the local CA (before opening the setup URL)"
echo "       multimux service install                    # run as a background daemon"
