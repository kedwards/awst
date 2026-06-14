#!/usr/bin/env bash

LIB_CORE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$LIB_CORE_DIR/interaction.sh"
source "$LIB_CORE_DIR/test_guard.sh"

aws_auth_detected() {
  local profile="${1:-${AWS_PROFILE:-}}"
  if [[ -n "$profile" ]]; then
    aws sts get-caller-identity --profile "$profile" >/dev/null 2>&1
  else
    aws sts get-caller-identity >/dev/null 2>&1
  fi
}

aws_auth_is_valid() {
  [[ -n "${AWS_ACCESS_KEY_ID:-}" &&
     -n "${AWS_SECRET_ACCESS_KEY:-}" &&
     -n "${AWS_SESSION_TOKEN:-}" ]]
}

# Actively authenticate by calling `source assume`.
# This loads credentials into the current shell, unlike aws_auth_assume()
# which only validates existing credentials.
guard_function_override aws_auth_login || aws_auth_login() {
  local profile="${1:-${PROFILE:-}}"
  local region="${2:-${REGION:-}}"

  if [[ -z "$profile" ]]; then
    log_error "Profile required for login"
    return 1
  fi

  if ! command -v assume >/dev/null 2>&1; then
    if [[ "${AWST_IN_CONTAINER:-0}" == "1" ]]; then
      log_error "Cannot authenticate from inside the aws-tools container"
      log_error "Run on your host shell first, then re-run this command:"
      log_error "  source assume $profile${region:+ -r $region}"
    else
      log_error "'assume' (Granted) not found in PATH"
      log_error "Install: https://docs.commonfate.io/granted/getting-started"
    fi
    return 1
  fi

  # Disable assume check for tests
  if [[ "${AWST_AUTH_DISABLE_ASSUME:-0}" == "1" ]]; then
    log_debug "Skipping assume (AWST_AUTH_DISABLE_ASSUME=1)"
    return 0
  fi

  local assume_args=("$profile")
  [[ -n "$region" ]] && assume_args+=("--region" "$region")

  log_debug "Running: source assume ${assume_args[*]}"

  # shellcheck disable=SC1090
  if ! source assume "${assume_args[@]}"; then
    log_error "Failed to assume profile '$profile'"
    return 1
  fi

  log_success "Authenticated as profile '$profile'"
  return 0
}

guard_function_override aws_auth_assume || aws_auth_assume() {
  local profile="${1:-${PROFILE:-}}"
  local region="${2:-${REGION:-}}"

  # Never authenticate during help
  [[ "${SHOW_HELP:-false}" == true ]] && return 0

  # Check if already authenticated
  if ! (aws_auth_is_valid || aws_auth_detected "$profile"); then
    # Auto-login if profile is available
    if [[ -n "$profile" ]]; then
      log_info "No credentials found, attempting login for '$profile'"
      aws_auth_login "$profile" "$region" || return 1
      return 0
    fi

    log_error "No AWS credentials found"
    log_error "Authenticate first with: assume <profile> -r <region>"
    return 1
  fi

  # Validate we're using the expected profile/region if specified
  if [[ -n "$profile" ]]; then
    local current_profile="${AWS_PROFILE:-}"
    if [[ -n "$current_profile" && "$current_profile" != "$profile" ]]; then
      log_error "Currently authenticated with profile '$current_profile'"
      log_error "Requested profile '$profile'"
    log_error "Run 'assume $profile' to switch profiles"
      return 1
    fi
  fi

  if [[ -n "$region" ]]; then
    local current_region="${AWS_REGION:-${AWS_DEFAULT_REGION:-}}"
    if [[ -n "$current_region" && "$current_region" != "$region" ]]; then
      log_error "Currently authenticated with region '$current_region'"
      log_error "Requested region '$region'"
    log_error "Run 'assume <profile> -r $region' to switch regions"
      return 1
    fi
  fi

  return 0
}
