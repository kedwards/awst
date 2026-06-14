#!/usr/bin/env bash
# aws-tools installer.
#
# Default (Phase 3): container-first.
#   Pulls the runtime image from GHCR, drops the host wrapper at
#   ~/.local/share/aws-tools/bin/awst, symlinks ~/.local/bin/awst.
#   Requires docker or podman on PATH.
#
# Fallback: --host
#   Original tarball-based host install. Requires aws-cli, session-manager-plugin,
#   Granted, etc. on PATH; everything runs natively.
#
# Usage:
#   ./install.sh [--host] [VERSION]
#   curl -sSL https://raw.githubusercontent.com/kedwards/aws-tools/main/install.sh | bash
#   curl -sSL https://raw.githubusercontent.com/kedwards/aws-tools/main/install.sh | bash -s v2.4.0
#   curl -sSL https://raw.githubusercontent.com/kedwards/aws-tools/main/install.sh | bash -s -- --host

set -euo pipefail

REPO_NAME="aws-tools"
REPO="kedwards/${REPO_NAME}"
REPO_URL="https://github.com/${REPO}"
IMAGE_REPO="ghcr.io/${REPO}"
INSTALL_DIR="${HOME}/.local/share/${REPO_NAME}"
BIN_DIR="${HOME}/.local/bin"
ETC_DIR="${INSTALL_DIR}/etc"
ENV_FILE="${ETC_DIR}/awst.env"

MODE="container"
VERSION=""

# Parse args (single optional --host flag and an optional version)
while (( $# > 0 )); do
  case "$1" in
    --host)   MODE="host" ;;
    --help|-h)
      sed -n '2,18p' "$0"
      exit 0
      ;;
    -*)       echo "[ERROR] Unknown flag: $1" >&2; exit 2 ;;
    *)        VERSION="$1" ;;
  esac
  shift
done

VERSION="${VERSION:-latest}"

resolve_version() {
  # Resolves "latest" to a vX.Y.Z tag via the GitHub releases API.
  # Other values pass through (allows passing "main", "v2.4.0", etc.).
  if [[ "$VERSION" == "latest" ]]; then
    local tag
    tag=$(curl -sSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null \
            | grep '"tag_name":' \
            | sed -E 's/.*"([^"]+)".*/\1/' \
            || true)
    if [[ -n "$tag" ]]; then
      VERSION="$tag"
    else
      echo "[WARN] No releases found; falling back to 'main'"
      VERSION="main"
    fi
  fi
}

# -------- container install --------
install_container() {
  local engine=""
  if command -v docker >/dev/null 2>&1; then
    engine=docker
  elif command -v podman >/dev/null 2>&1; then
    engine=podman
  else
    cat >&2 <<EOF
[ERROR] Container install requires docker or podman on PATH.

Install one of:
  - https://docs.docker.com/get-docker/
  - https://podman.io/getting-started/installation

Or use the host install:
  curl -sSL https://raw.githubusercontent.com/${REPO}/main/install.sh | bash -s -- --host
EOF
    exit 1
  fi

  resolve_version
  local image_tag="${IMAGE_REPO}:${VERSION}"
  local wrapper_url="https://raw.githubusercontent.com/${REPO}/${VERSION}/containers/awst-host"

  echo "[INFO] Installing ${REPO_NAME} (container mode, engine=${engine}, version=${VERSION})"

  mkdir -p "${INSTALL_DIR}/bin" "${ETC_DIR}" "${BIN_DIR}"

  echo "[INFO] Pulling ${image_tag}..."
  if ! "${engine}" pull "${image_tag}"; then
    echo "[ERROR] Failed to pull ${image_tag}" >&2
    echo "[ERROR] Verify the tag exists at https://github.com/${REPO}/pkgs/container/${REPO_NAME}" >&2
    exit 1
  fi

  echo "[INFO] Downloading wrapper script (containers/awst-host @ ${VERSION})..."
  if ! curl -fsSL "${wrapper_url}" -o "${INSTALL_DIR}/bin/awst"; then
    echo "[ERROR] Failed to download wrapper from ${wrapper_url}" >&2
    exit 1
  fi
  chmod +x "${INSTALL_DIR}/bin/awst"

  echo "[INFO] Writing pinned image config to ${ENV_FILE}"
  cat >"${ENV_FILE}" <<EOF
# Written by install.sh / update.sh. Override by exporting AWST_IMAGE
# or by editing this file. Re-run \`awst update\` to bump.
AWST_IMAGE="${image_tag}"
EOF

  # Write a marker so update.sh can detect container mode without re-deriving.
  printf '%s\n' "$VERSION" >"${INSTALL_DIR}/VERSION"
  printf '%s\n' "container" >"${INSTALL_DIR}/.mode"

  ln -sf "${INSTALL_DIR}/bin/awst" "${BIN_DIR}/awst"

  cat <<EOF

[SUCCESS] ${REPO_NAME} ${VERSION} installed (container mode).
  Image:   ${image_tag}
  Wrapper: ${INSTALL_DIR}/bin/awst
  Symlink: ${BIN_DIR}/awst

Ensure ${BIN_DIR} is on your PATH:
  export PATH="${BIN_DIR}:\$PATH"

Then run:
  awst --help

Granted/SSO note: \`assume\` is NOT installed inside the container. On the host,
run \`assume <profile> -r <region>\` first (or \`assume <profile> --exec -- awst <cmd>\`)
so the wrapper can forward cached credentials.
EOF
}

# -------- host install (original tarball flow) --------
install_host() {
  echo "[INFO] Installing ${REPO_NAME} (host mode, version=${VERSION}) to ${INSTALL_DIR}"
  mkdir -p "${INSTALL_DIR}" "${BIN_DIR}"

  # tmpdir is global so the EXIT trap can still see it after this function returns.
  tmpdir="$(mktemp -d)"
  trap 'rm -rf "$tmpdir"' EXIT
  local extracted_dir download_url

  resolve_version

  if [[ "$VERSION" == "main" ]] || [[ "$VERSION" == "dev" ]]; then
    download_url="${REPO_URL}/archive/refs/heads/main.tar.gz"
    extracted_dir="${tmpdir}/${REPO_NAME}-main"
  else
    download_url="${REPO_URL}/archive/refs/tags/${VERSION}.tar.gz"
    extracted_dir="${tmpdir}/${REPO_NAME}-${VERSION#v}"
  fi

  echo "[INFO] Downloading ${download_url}..."
  curl -sSL "$download_url" | tar xz -C "$tmpdir"

  echo "[INFO] Copying files..."
  rsync -a --delete "${extracted_dir}/" "${INSTALL_DIR}/"

  if [[ -f "${INSTALL_DIR}/examples/connections.config" ]]; then
    echo "[INFO] Installing default connections..."
    cp "${INSTALL_DIR}/examples/connections.config" "${INSTALL_DIR}/connections.config"
  fi

  local config_dir="${HOME}/.config/${REPO_NAME}"
  if [[ -d "${INSTALL_DIR}/examples/commands" ]]; then
    echo "[INFO] Installing default commands to ${config_dir}/commands/..."
    mkdir -p "${config_dir}/commands/aws" "${config_dir}/commands/ssm"
    rsync -a "${INSTALL_DIR}/examples/commands/aws/" "${config_dir}/commands/aws/"
    rsync -a "${INSTALL_DIR}/examples/commands/ssm/" "${config_dir}/commands/ssm/"
  fi

  echo "[INFO] Creating symlinks in ${BIN_DIR}"
  for f in "${INSTALL_DIR}/bin/"*; do
    ln -sf "${f}" "${BIN_DIR}/$(basename "$f")"
  done

  printf '%s\n' "host" >"${INSTALL_DIR}/.mode"

  local installed_version
  installed_version="$(cat "${INSTALL_DIR}/VERSION" 2>/dev/null || echo 'unknown')"

  cat <<EOF

[SUCCESS] ${REPO_NAME} v${installed_version} installed (host mode).

Requires on PATH: aws, assume (Granted), session-manager-plugin
Optional: fzf

Ensure ${BIN_DIR} is on your PATH:
  export PATH="${BIN_DIR}:\$PATH"

Then run:
  awst --help
EOF
}

case "$MODE" in
  container) install_container ;;
  host)      install_host ;;
esac
