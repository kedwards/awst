#!/usr/bin/env bash
# Single source of truth for aws-tools base and user config paths.
#
# Naming convention:
#   Base paths    = installed defaults (shipped in the repo tarball / container)
#   User paths    = per-user customizations (never overwritten by updates)
#
# Install dir is resolved dynamically to handle sudo/odd HOME setups.

_AWST_RESOLVE_USER_HOME() {
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

AWST_USER_HOME="${AWST_USER_HOME:-$(_AWST_RESOLVE_USER_HOME)}"
AWST_INSTALL_DIR="${AWST_INSTALL_DIR:-${AWST_USER_HOME}/.local/share/aws-tools}"
AWST_CONFIG_DIR="${AWST_USER_HOME}/.config/aws-tools"
AWST_CACHE_DIR="${AWST_USER_HOME}/.cache/aws-tools"
AWST_BIN_DIR="${AWST_USER_HOME}/.local/bin"

# ── SSM exec commands ────────────────────────────────────────────────────────
# Base: shipped defaults (in install dir)
# User: per-user additions/overrides
# Env override: AWST_SSM_CMD_DIR (exclusive, replaces both base + user)
AWST_SSM_CMD_BASE="${AWST_INSTALL_DIR}/commands/ssm"
AWST_SSM_CMD_USER="${AWST_CONFIG_DIR}/commands/ssm"

# ── AWS run commands ─────────────────────────────────────────────────────────
# Base: shipped defaults (in install dir)
# User: per-user additions/overrides
# Env override: AWST_CMD_DIR (exclusive, replaces both base + user)
AWST_RUN_CMD_BASE="${AWST_INSTALL_DIR}/commands/aws"
AWST_RUN_CMD_USER="${AWST_CONFIG_DIR}/commands/aws"

# ── Connection configs (port-forwarding tunnels) ──────────────────────────────
# Base: shipped defaults (in install dir)
# User: per-user additions/overrides
# CLI override: --file flag for one-off configs
AWST_CONN_BASE="${AWST_INSTALL_DIR}/connections.config"
AWST_CONN_USER="${AWST_CONFIG_DIR}/connections.user.config"
