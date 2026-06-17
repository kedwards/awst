#!/usr/bin/env bats

export MENU_NON_INTERACTIVE=1
export AWST_EC2_DISABLE_LIVE_CALLS=1
export AWST_AUTH_DISABLE_ASSUME=1

load '../helpers/bats-support/load'
load '../helpers/bats-assert/load'

# Stub logging functions
log_debug()   { :; }
log_info()    { echo "[INFO] $*"; }
log_warn()    { echo "[WARN] $*"; }
log_error()   { echo "[ERROR] $*" >&2; }
log_success() { echo "[SUCCESS] $*"; }

setup() {
  # Create a temporary directory for fake installation
  export FAKE_HOME="$(mktemp -d)"
  export HOME="$FAKE_HOME"
  export INSTALL_DIR="${HOME}/.local/share/aws-tools"

  # Stub the user-home resolver so it doesn't bypass our fake HOME
  awst_resolve_user_home() { printf '%s\n' "$FAKE_HOME"; }

  # Source the update command
  source ./lib/commands/awst_update.sh
}

teardown() {
  # Clean up fake home
  rm -rf "$FAKE_HOME"
}

@test "awst_update shows help with --help" {
  run awst_update --help
  assert_success
  assert_output --partial "Usage: awst update"
}

@test "awst_update shows help with -h" {
  run awst_update -h
  assert_success
  assert_output --partial "Usage: awst update"
}

@test "awst_update fails when not installed" {
  run awst_update
  assert_failure
  assert_output --partial "not installed"
}

@test "awst_update shows current version when installed" {
  # Create fake installation
  mkdir -p "$INSTALL_DIR"
  echo "1.3.1" > "$INSTALL_DIR/VERSION"
  
  # Stub curl to fail so it doesn't proceed
  curl() { return 1; }
  
  run awst_update
  
  assert_failure
  assert_output --partial "Current version: 1.3.1"
}

@test "awst_update usage mentions specific version example" {
  run awst_update --help
  assert_success
  assert_output --partial "vX.Y.Z"
}

@test "awst_update usage mentions main branch option" {
  run awst_update --help
  assert_success
  assert_output --partial "main"
}

@test "awst_update usage mentions dev branch option" {
  run awst_update --help
  assert_success
  assert_output --partial "dev"
}
