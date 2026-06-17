#!/usr/bin/env bash

awst_update_usage() {
  cat <<EOF
Usage: awst update [VERSION]

Update aws-tools to a specific version or the latest release.

Arguments:
  VERSION    Version to install (default: latest)
             - "latest" for the most recent release
             - "main" or "dev" for the development branch
             - "vX.Y.Z" for a specific version tag

Examples:
  awst update           # Update to latest release
  awst update main      # Update to development branch
  awst update v0.1.0    # Update to specific version

Options:
  -h, --help    Show this help message
EOF
}
awst_resolve_user_home() {
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
    return 1
  fi

  printf '%s\n' "$home"
}

awst_update() {
  if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    awst_update_usage
    return 0
  fi

  local REPO_NAME="aws-tools"
  local REPO="kedwards/${REPO_NAME}"
  local USER_HOME
  USER_HOME="$(awst_resolve_user_home)" || return 1
  local INSTALL_DIR="${USER_HOME}/.local/share/${REPO_NAME}"
  local REPO_URL="https://github.com/${REPO}"
  local VERSION="${1:-latest}"

  # Check if installed
  if [[ ! -d "${INSTALL_DIR}" ]]; then
    log_error "${REPO_NAME} is not installed in ${INSTALL_DIR}"
    echo ""
    echo "Install it with:"
    echo "  curl -sSL https://raw.githubusercontent.com/${REPO}/main/install.sh | bash"
    return 1
  fi

  # Show current version
  local CURRENT_VERSION
  CURRENT_VERSION="$(cat "${INSTALL_DIR}/VERSION" 2>/dev/null || echo 'unknown')"
  log_info "Current version: ${CURRENT_VERSION}"

  log_info "Updating ${REPO_NAME} in ${INSTALL_DIR}"

  # Create temporary directory
  local tmpdir
  tmpdir="$(mktemp -d)"

  # Determine download URL based on version
  local DOWNLOAD_URL EXTRACTED_DIR
  
  if [[ "$VERSION" == "latest" ]]; then
    # Try to get latest release tag, fallback to main
    log_info "Fetching latest release..."
    local LATEST_TAG
    LATEST_TAG=$(curl -sSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' || echo "")
    
    if [[ -n "$LATEST_TAG" ]]; then
      log_info "Downloading ${REPO_NAME} ${LATEST_TAG}..."
      DOWNLOAD_URL="${REPO_URL}/archive/refs/tags/${LATEST_TAG}.tar.gz"
      EXTRACTED_DIR="${tmpdir}/${REPO_NAME}-${LATEST_TAG#v}"
    else
      log_info "No releases found, downloading from main branch..."
      DOWNLOAD_URL="${REPO_URL}/archive/refs/heads/main.tar.gz"
      EXTRACTED_DIR="${tmpdir}/${REPO_NAME}-main"
    fi
  elif [[ "$VERSION" == "main" ]] || [[ "$VERSION" == "dev" ]]; then
    log_info "Downloading ${REPO_NAME} from main branch..."
    DOWNLOAD_URL="${REPO_URL}/archive/refs/heads/main.tar.gz"
    EXTRACTED_DIR="${tmpdir}/${REPO_NAME}-main"
  else
    # Update to specific version (tag)
    log_info "Downloading ${REPO_NAME} ${VERSION}..."
    DOWNLOAD_URL="${REPO_URL}/archive/refs/tags/${VERSION}.tar.gz"
    EXTRACTED_DIR="${tmpdir}/${REPO_NAME}-${VERSION#v}"
  fi

  # Download and extract
  if ! curl -sSL "$DOWNLOAD_URL" | tar xz -C "$tmpdir"; then
    log_error "Failed to download or extract ${REPO_NAME} from ${DOWNLOAD_URL}"
    rm -rf "$tmpdir"
    return 1
  fi

  # Verify extraction
  if [[ ! -d "$EXTRACTED_DIR" ]]; then
    log_error "Extraction failed: expected directory ${EXTRACTED_DIR} not found"
    rm -rf "$tmpdir"
    return 1
  fi

  # Sync files
  log_info "Syncing files..."
  if ! rsync -a --delete "${EXTRACTED_DIR}/" "${INSTALL_DIR}/"; then
    log_error "Failed to sync files to ${INSTALL_DIR}"
    rm -rf "$tmpdir"
    return 1
  fi

  # Ship default commands into the install dir (base, not user config).
  if [[ -d "${INSTALL_DIR}/examples/commands" ]]; then
    mkdir -p "${INSTALL_DIR}/commands/aws" "${INSTALL_DIR}/commands/ssm"
    rsync -a --ignore-existing "${INSTALL_DIR}/examples/commands/aws/" "${INSTALL_DIR}/commands/aws/"
    rsync -a --ignore-existing "${INSTALL_DIR}/examples/commands/ssm/" "${INSTALL_DIR}/commands/ssm/"
  else
    log_warn "examples/commands not found, default commands may be outdated"
  fi

  # Update default connections from examples/connections.config
  if [[ -f "${INSTALL_DIR}/examples/connections.config" ]]; then
    log_info "Updating default connections..."
    cp "${INSTALL_DIR}/examples/connections.config" "${INSTALL_DIR}/connections.config"
  else
    log_warn "examples/connections.config not found, default connections may be outdated"
  fi

  # Show new version
  local NEW_VERSION
  NEW_VERSION="$(cat "${INSTALL_DIR}/VERSION" 2>/dev/null || echo 'unknown')"

  # Clean up temporary directory before showing success message
  rm -rf "$tmpdir"
  
  echo ""
  if [[ "$CURRENT_VERSION" != "$NEW_VERSION" ]]; then
    log_success "${REPO_NAME} updated from v${CURRENT_VERSION} to v${NEW_VERSION}!"
  else
    log_success "${REPO_NAME} v${NEW_VERSION} reinstalled!"
  fi
  echo ""
  echo "To update to a specific version, run:"
  echo "  awst update v0.1.0"
  echo ""
}
