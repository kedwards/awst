#!/usr/bin/env bash
# aws-tools updater. Detects whether the existing install is container-mode
# or host-mode (via ${INSTALL_DIR}/.mode) and updates in place.
#
# Usage:
#   ./update.sh                # update to latest release
#   ./update.sh v2.4.0         # update to specific tag
#   ./update.sh main           # track main branch (host mode only)

set -euo pipefail

REPO_NAME="aws-tools"
REPO="kedwards/${REPO_NAME}"
REPO_URL="https://github.com/${REPO}"
IMAGE_REPO="ghcr.io/${REPO}"

resolve_user_home() {
  local target_user home
  target_user="${SUDO_USER:-$(id -un)}"
  home=""

  if command -v getent >/dev/null 2>&1; then
    home="$(getent passwd "$target_user" | cut -d: -f6 || true)"
  fi

  if [[ -z "$home" && -r /etc/passwd ]]; then
    home="$(awk -F: -v user="$target_user" '$1 == user { print $6; exit }' /etc/passwd)"
  fi

  if [[ -z "$home" && "$target_user" == "$(id -un)" ]]; then
    home="${HOME:-}"
  fi

  if [[ -z "$home" ]]; then
    echo "[ERROR] Unable to determine home directory for user: ${target_user}" >&2
    exit 1
  fi

  printf '%s\n' "$home"
}

USER_HOME="$(resolve_user_home)"
INSTALL_DIR="${USER_HOME}/.local/share/${REPO_NAME}"
ETC_DIR="${INSTALL_DIR}/etc"
ENV_FILE="${ETC_DIR}/awst.env"
MODE_FILE="${INSTALL_DIR}/.mode"

if [[ ! -d "${INSTALL_DIR}" ]]; then
  cat >&2 <<EOF
[ERROR] ${REPO_NAME} is not installed in ${INSTALL_DIR}
Install it first:
  curl -sSL https://raw.githubusercontent.com/${REPO}/main/install.sh | bash
EOF
  exit 1
fi

VERSION="${1:-latest}"

resolve_version() {
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

# Detect install mode (default to host for back-compat with pre-Phase-3 installs)
INSTALL_MODE="host"
if [[ -f "$MODE_FILE" ]]; then
  INSTALL_MODE="$(cat "$MODE_FILE")"
fi

CURRENT_VERSION="$(cat "${INSTALL_DIR}/VERSION" 2>/dev/null || echo 'unknown')"
echo "[INFO] Current install: mode=${INSTALL_MODE}, version=${CURRENT_VERSION}"

update_container() {
  local engine=""
  if command -v docker >/dev/null 2>&1; then
    engine=docker
  elif command -v podman >/dev/null 2>&1; then
    engine=podman
  else
    echo "[ERROR] Container update requires docker or podman on PATH." >&2
    exit 1
  fi

  resolve_version
  local image_tag="${IMAGE_REPO}:${VERSION}"
  local wrapper_url="https://raw.githubusercontent.com/${REPO}/${VERSION}/containers/awst-host"

  echo "[INFO] Pulling ${image_tag}..."
  if ! "${engine}" pull "${image_tag}"; then
    echo "[ERROR] Failed to pull ${image_tag}" >&2
    exit 1
  fi

  echo "[INFO] Refreshing wrapper script (containers/awst-host @ ${VERSION})..."
  if ! curl -fsSL "${wrapper_url}" -o "${INSTALL_DIR}/bin/awst"; then
    echo "[ERROR] Failed to download wrapper from ${wrapper_url}" >&2
    exit 1
  fi
  chmod +x "${INSTALL_DIR}/bin/awst"

  mkdir -p "${ETC_DIR}"
  cat >"${ENV_FILE}" <<EOF
# Written by install.sh / update.sh. Override by exporting AWST_IMAGE
# or by editing this file.
AWST_IMAGE="${image_tag}"
EOF

  printf '%s\n' "$VERSION" >"${INSTALL_DIR}/VERSION"

  if [[ "$CURRENT_VERSION" != "$VERSION" ]]; then
    echo "[SUCCESS] ${REPO_NAME} updated: ${CURRENT_VERSION} -> ${VERSION} (container)"
  else
    echo "[SUCCESS] ${REPO_NAME} ${VERSION} reinstalled (container)"
  fi
}

update_host() {
  echo "[INFO] Updating ${REPO_NAME} in ${INSTALL_DIR} (host mode)"

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

  echo "[INFO] Syncing files..."
  rsync -a --delete "${extracted_dir}/" "${INSTALL_DIR}/"

  # Ship default commands into the install dir (base, not user config).
  if [[ -d "${INSTALL_DIR}/examples/commands" ]]; then
    mkdir -p "${INSTALL_DIR}/commands/aws" "${INSTALL_DIR}/commands/ssm"
    rsync -a --ignore-existing "${INSTALL_DIR}/examples/commands/aws/" "${INSTALL_DIR}/commands/aws/"
    rsync -a --ignore-existing "${INSTALL_DIR}/examples/commands/ssm/" "${INSTALL_DIR}/commands/ssm/"
  fi

  if [[ -f "${INSTALL_DIR}/examples/connections.config" ]]; then
    cp "${INSTALL_DIR}/examples/connections.config" "${INSTALL_DIR}/connections.config"
  fi

  printf '%s\n' "host" >"${INSTALL_DIR}/.mode"

  local new_version
  new_version="$(cat "${INSTALL_DIR}/VERSION" 2>/dev/null || echo 'unknown')"

  if [[ "$CURRENT_VERSION" != "$new_version" ]]; then
    echo "[SUCCESS] ${REPO_NAME} updated: v${CURRENT_VERSION} -> v${new_version} (host)"
  else
    echo "[SUCCESS] ${REPO_NAME} v${new_version} reinstalled (host)"
  fi
}

case "$INSTALL_MODE" in
  container) update_container ;;
  host)      update_host ;;
  *)
    echo "[ERROR] Unknown install mode '${INSTALL_MODE}' in ${MODE_FILE}" >&2
    exit 1
    ;;
esac
