#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${1:-$ROOT_DIR/dist}"
# Resolved before the cd below, so a relative argument keeps the meaning the
# caller gave it.
mkdir -p "$DIST_DIR"
DIST_DIR="$(cd "$DIST_DIR" && pwd)"
# go build runs in the module, wherever the script was invoked from.
cd "$ROOT_DIR"
DEFAULT_VERSION="$(sed -n 's/^var Value = "\(.*\)"$/\1/p' "$ROOT_DIR/internal/version/version.go" | head -1)"
VERSION="${VERSION:-${GITHUB_REF_NAME:-${DEFAULT_VERSION:-dev}}}"
VERSION="${VERSION#v}"
# The repository the built binaries fetch their own updates from. In CI this is
# the repository the workflow runs in, so a fork of the fork stays self-contained.
UPDATE_REPO="${GITHUB_REPOSITORY:-starbuck100/clother}"

rm -f "$DIST_DIR"/clother_*.tar.gz "$DIST_DIR"/clother_*.zip "$DIST_DIR"/checksums.txt "$DIST_DIR"/latest.json

build_target() {
  local os="$1"
  local arch="$2"
  local work="$DIST_DIR/${os}-${arch}"
  local binary="clother"
  local asset="clother_${os}_${arch}.tar.gz"

  # Windows runs executables by extension, and its releases ship .zip because
  # that is what PowerShell can expand without any extra tooling.
  if [[ "$os" == "windows" ]]; then
    binary="clother.exe"
    asset="clother_${os}_${arch}.zip"
  fi

  rm -rf "$work"
  mkdir -p "$work"
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w -X github.com/jolehuit/clother/internal/version.Value=${VERSION} -X github.com/jolehuit/clother/internal/update.releaseRepo=${UPDATE_REPO}" \
    -o "$work/$binary" \
    ./cmd/clother

  if [[ "$os" == "windows" ]]; then
    # Not `zip`: Git Bash on Windows does not have it, and the Windows CI job
    # packages a release with exactly this script. The Go toolchain is already
    # required here, so the helper costs no new dependency and writes the same
    # archive on every platform.
    go run ./scripts/zipasset -o "$DIST_DIR/$asset" "$work/$binary"
  else
    tar -C "$work" -czf "$DIST_DIR/$asset" "$binary"
  fi
}

build_target darwin amd64
build_target darwin arm64
build_target linux amd64
build_target linux arm64
build_target windows amd64
build_target windows arm64

TAG_VERSION="$VERSION"
[[ "$TAG_VERSION" != v* ]] && TAG_VERSION="v$TAG_VERSION"

if command -v shasum >/dev/null 2>&1; then
  (cd "$DIST_DIR" && shasum -a 256 clother_*.tar.gz clother_*.zip > checksums.txt)
else
  (cd "$DIST_DIR" && sha256sum clother_*.tar.gz clother_*.zip > checksums.txt)
fi

cat > "$DIST_DIR/latest.json" <<EOF
{
  "version": "$TAG_VERSION",
  "url": "https://github.com/${UPDATE_REPO}/releases/tag/$TAG_VERSION"
}
EOF
