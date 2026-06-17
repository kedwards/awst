#!/usr/bin/env bats
# shellcheck disable=SC2329,SC2030,SC2031
# Tests for awst run command directory resolution and overrides.
#
# The directory rules under test:
#   1. Default dir  (AWST_RUN_CMD_BASE / AWST_RUN_CMD_USER) is the standard location
#   2. -d <path>    is an exclusive override — default dir is ignored
#   3. AWST_CMD_DIR is an exclusive override — default dir is ignored

load '../helpers/bats-support/load'
load '../helpers/bats-assert/load'

# Stub logging
log_debug()   { :; }
log_info()    { :; }
log_warn()    { echo "[WARN] $*"; }
log_error()   { echo "[ERROR] $*" >&2; }
log_success() { :; }

setup() {
  TEST_TMPDIR="$(mktemp -d)"

  # Isolate HOME so _AWST_RUN_CMD_DIR resolves to test paths
  export HOME="$TEST_TMPDIR/home"
  mkdir -p "$HOME"

  # Ensure env-var override does not interfere
  unset AWST_CMD_DIR

  # Stubs
  aws_auth_login() { echo "LOGIN: $1 $2"; return 0; }
  aws_list_profiles() { printf '%s\n' "dev" "prod"; }

  # Pre-set the fallback variables so awst_run.sh picks them up
  export _AWST_RUN_CMD_DIR="$HOME/.config/aws-tools/commands/aws"
  export AWST_RUN_CMD_BASE="$_AWST_RUN_CMD_DIR"
  export AWST_RUN_CMD_USER="$_AWST_RUN_CMD_DIR"

  source ./lib/commands/awst_run.sh
}

teardown() {
  rm -rf "$TEST_TMPDIR"
}

# ── Helpers ──────────────────────────────────────────────────────────────────

# Create a non-executable snippet file
make_snippet() {
  local dir="$1" name="$2" desc="${3:-A description}"
  mkdir -p "$dir"
  printf '# aws-tools command\n# %s\necho "OUTPUT:%s"\n' "$desc" "$name" > "$dir/$name"
}

# Create an executable script
make_script() {
  local dir="$1" name="$2" desc="${3:-A description}"
  mkdir -p "$dir"
  printf '#!/usr/bin/env bash\n# %s\necho "OUTPUT:%s"\n' "$desc" "$name" > "$dir/$name"
  chmod +x "$dir/$name"
}

# ── Listing: default directory ────────────────────────────────────────────────

@test "list: shows commands from default dir" {
  make_snippet "$_AWST_RUN_CMD_DIR" "vpc-cidrs" "VPC CIDRs"

  run awst_run

  assert_success
  assert_output --partial "vpc-cidrs"
  assert_output --partial "VPC CIDRs"
}

@test "list: shows multiple commands from default dir" {
  make_snippet "$_AWST_RUN_CMD_DIR" "cmd-a" "Command A"
  make_snippet "$_AWST_RUN_CMD_DIR" "cmd-b" "Command B"

  run awst_run

  assert_success
  assert_output --partial "cmd-a"
  assert_output --partial "cmd-b"
}

@test "list: commands are sorted alphabetically" {
  make_snippet "$_AWST_RUN_CMD_DIR" "z-last"  "Last command"
  make_snippet "$_AWST_RUN_CMD_DIR" "a-first" "First command"

  run awst_run

  assert_success
  # a-first should appear before z-last in output
  local pos_first pos_last
  pos_first=$(echo "$output" | grep -n "a-first" | cut -d: -f1)
  pos_last=$(echo  "$output" | grep -n "z-last"  | cut -d: -f1)
  [ "$pos_first" -lt "$pos_last" ]
}

# ── Listing: markers ─────────────────────────────────────────────────────────

@test "list: snippet commands have no * marker" {
  make_snippet "$_AWST_RUN_CMD_DIR" "my-cmd" "My command"

  run awst_run

  assert_success
  assert_output --partial "my-cmd"
  refute_output --partial "*"
}

@test "list: executable script shows * marker" {
  make_script "$_AWST_RUN_CMD_DIR" "my-script" "My script"

  run awst_run

  assert_success
  assert_output --partial "my-script"
  assert_output --partial "*"
  assert_output --partial "* = executable script"
}

# ── Execution ─────────────────────────────────────────────────────────────────

@test "execute: snippet runs with profile iteration" {
  make_snippet "$_AWST_RUN_CMD_DIR" "my-cmd" "My command"

  run awst_run my-cmd "dev"

  assert_success
  assert_output --partial "OUTPUT:my-cmd"
}

@test "execute: executable script runs directly without profile iteration" {
  make_script "$_AWST_RUN_CMD_DIR" "my-script" "My script"

  run awst_run my-script

  assert_success
  assert_output --partial "OUTPUT:my-script"
  # Executable ran directly — no profile login
  refute_output --partial "LOGIN:"
}

# ── Exclusive override: -d flag ───────────────────────────────────────────────

@test "-d uses only the specified dir and ignores default dir" {
  make_snippet "$_AWST_RUN_CMD_DIR" "default-cmd" "Default command"
  local alt="$TEST_TMPDIR/alt"
  make_snippet "$alt" "alt-cmd" "Alt command"

  run awst_run -d "$alt"

  assert_success
  assert_output --partial "alt-cmd"
  refute_output --partial "default-cmd"
}

@test "-d fails with error when specified dir does not exist" {
  run awst_run -d "$TEST_TMPDIR/nonexistent"

  assert_failure
  assert_output --partial "Commands directory not found"
}

# ── Exclusive override: AWST_CMD_DIR ─────────────────────────────────────

@test "AWST_CMD_DIR uses only that dir and ignores default dir" {
  make_snippet "$_AWST_RUN_CMD_DIR" "default-cmd" "Default command"
  local override="$TEST_TMPDIR/override"
  make_snippet "$override" "override-cmd" "Override command"

  export AWST_CMD_DIR="$override"
  run awst_run

  assert_success
  assert_output --partial "override-cmd"
  refute_output --partial "default-cmd"
}

@test "AWST_CMD_DIR fails with error when set to nonexistent dir" {
  export AWST_CMD_DIR="$TEST_TMPDIR/nonexistent"

  run awst_run

  assert_failure
  assert_output --partial "Commands directory not found"
}

# ── Error cases ───────────────────────────────────────────────────────────────

@test "fails with error when default dir does not exist" {
  # HOME is isolated with no dirs created — default path is missing
  run awst_run

  assert_failure
  assert_output --partial "No commands directories found"
}

@test "list: shows 'No commands found' when dir exists but is empty" {
  mkdir -p "$_AWST_RUN_CMD_DIR"

  run awst_run

  assert_success
  assert_output --partial "No commands found"
}
