#!/usr/bin/env bash

awst_config_usage() {
  cat <<EOF
Usage: awst config [--verbose]

Display current aws-tools configuration.

Options:
  --verbose    Show all environment variables and internal settings
  -h, --help   Show this help message
EOF
}

awst_config() {
  local verbose=false
  if [[ "${1:-}" == "--verbose" || "${1:-}" == "-v" ]]; then
    verbose=true
  elif [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    awst_config_usage
    return 0
  fi

  local install_dir="${AWST_INSTALL_DIR:-$HOME/.local/share/aws-tools}"
  local config_dir="${AWST_CONFIG_DIR:-$HOME/.config/aws-tools}"
  local bin_dir="${AWST_BIN_DIR:-$HOME/.local/bin}"

  echo "aws-tools v${VERSION:-unknown}"
  echo ""

  # ── Config Domains ──
  echo "SSM Commands (awst exec)"
  _config_path "  Base" "${AWST_SSM_CMD_BASE:-$install_dir/commands/ssm}"
  _config_path "  User" "${AWST_SSM_CMD_USER:-$config_dir/commands/ssm}"
  _config_var  "  Override" "AWST_SSM_CMD_DIR" "(none)"
  echo ""

  echo "AWS Commands (awst run)"
  _config_path "  Base" "${AWST_RUN_CMD_BASE:-$install_dir/commands/aws}"
  _config_path "  User" "${AWST_RUN_CMD_USER:-$config_dir/commands/aws}"
  _config_var  "  Override" "AWST_CMD_DIR" "(none)"
  echo ""

  echo "Connections (awst connect --config)"
  _config_path "  Base" "${AWST_CONN_BASE:-$install_dir/connections.config}"
  _config_path "  User" "${AWST_CONN_USER:-$config_dir/connections.user.config}"
  echo ""

  # ── AWS Auth ──
  echo "AWS"
  _config_path "  Config file" "$HOME/.aws/config"
  _config_show "  Profile" "${AWS_PROFILE:-}" "(not set)"
  _config_show "  Region" "${AWS_REGION:-${AWS_DEFAULT_REGION:-}}" "(not set)"
  echo ""

  # ── Dependencies ──
  echo "Dependencies"
  _config_dep "  aws" "required"
  _config_dep "  assume" "required"
  _config_dep "  rsync" "required"
  _config_dep "  fzf" "optional"

  # ── Verbose mode ──
  if $verbose; then
    echo ""
    echo "─────────────────────────────────────────────────────────"
    echo "Environment Variables (verbose)"
    echo "─────────────────────────────────────────────────────────"
    echo ""
    echo "Installation:"
    _config_var "  Install dir" "AWST_INSTALL_DIR" ""
    _config_var "  Config dir" "AWST_CONFIG_DIR" ""
    _config_var "  Bin dir" "AWST_BIN_DIR" ""
    echo ""
    echo "Logging:"
    _config_var "  AWS_LOG_LEVEL" "AWS_LOG_LEVEL" "INFO"
    _config_var "  AWS_LOG_COLOR" "AWS_LOG_COLOR" "1"
    _config_var "  AWS_LOG_TIMESTAMP" "AWS_LOG_TIMESTAMP" "1"
    _config_var "  AWS_LOG_FILE" "AWS_LOG_FILE" "(disabled)"
    _config_var "  AWS_LOG_FILE_MAX_SIZE" "AWS_LOG_FILE_MAX_SIZE" "1048576"
    _config_var "  AWS_LOG_FILE_ROTATE" "AWS_LOG_FILE_ROTATE" "5"
    echo ""
    echo "Auth:"
    _config_var "  AWS_AUTH_AUTO_LOGIN" "AWS_AUTH_AUTO_LOGIN" "0"
    echo ""
    echo "Menu:"
    _config_var "  MENU_NO_FZF" "MENU_NO_FZF" "0"
    _config_var "  MENU_NON_INTERACTIVE" "MENU_NON_INTERACTIVE" "0"
    _config_var "  MENU_ASSUME_FIRST" "MENU_ASSUME_FIRST" "0"
    echo ""
    echo "Cache:"
    _config_var "  AWST_CACHE_TTL" "AWST_CACHE_TTL" "30"
    _config_var "  AWST_AUTH_DISABLE_ASSUME" "AWST_AUTH_DISABLE_ASSUME" "0"
    echo ""
    echo "Paths:"
    _config_var "  AWST_SSM_CMD_BASE" "AWST_SSM_CMD_BASE" ""
    _config_var "  AWST_SSM_CMD_USER" "AWST_SSM_CMD_USER" ""
    _config_var "  AWST_RUN_CMD_BASE" "AWST_RUN_CMD_BASE" ""
    _config_var "  AWST_RUN_CMD_USER" "AWST_RUN_CMD_USER" ""
    _config_var "  AWST_CONN_BASE" "AWST_CONN_BASE" ""
    _config_var "  AWST_CONN_USER" "AWST_CONN_USER" ""
  fi
}

# ── Helpers ──

# Print a path with exists/missing indicator
_config_path() {
  local label="$1" path="$2"
  if [[ -e "$path" ]]; then
    printf "%-28s %s\n" "$label" "$path"
  else
    printf "%-28s %s (missing)\n" "$label" "$path"
  fi
}

# Print an env var's current value (or default)
_config_var() {
  local label="$1" var_name="$2" default="${3:-}"
  local value="${!var_name:-}"
  if [[ -n "$value" ]]; then
    printf "%-28s %s\n" "$label" "$value"
  else
    printf "%-28s %s\n" "$label" "${default:-(not set)}"
  fi
}

# Print a value with fallback
_config_show() {
  local label="$1" value="$2" fallback="$3"
  printf "%-28s %s\n" "$label" "${value:-$fallback}"
}

# Print dependency status
_config_dep() {
  local label="$1" note="$2"
  local cmd="${label##* }"
  if command -v "$cmd" >/dev/null 2>&1; then
    local ver
    ver=$("$cmd" --version 2>&1 | head -n1) || ver="found"
    printf "%-28s ✓ %s\n" "$label" "$ver"
  else
    printf "%-28s ✗ not found (%s)\n" "$label" "$note"
  fi
}
