#!/usr/bin/env bats
# Smoke tests for containers/awst-host. Mocks docker/podman on PATH and
# inspects the command line the wrapper would execute.

bats_require_minimum_version 1.5.0   # for `run -N` exit-code form

load '../helpers/bats-support/load'
load '../helpers/bats-assert/load'

WRAPPER="$BATS_TEST_DIRNAME/../../containers/awst-host"

setup() {
  TMPDIR_BIN="$(mktemp -d)"
  TMPDIR_HOME="$(mktemp -d)"
  mkdir -p "$TMPDIR_HOME/.aws" "$TMPDIR_HOME/.granted"

  # Fake docker/podman: write argv to a known file, exit 0.
  cat >"$TMPDIR_BIN/docker" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$@" > "$TMPDIR_BIN/argv.docker"
exit 0
EOF
  cat >"$TMPDIR_BIN/podman" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$@" > "$TMPDIR_BIN/argv.podman"
exit 0
EOF
  chmod +x "$TMPDIR_BIN/docker" "$TMPDIR_BIN/podman"

  export PATH="$TMPDIR_BIN:$PATH"
  export HOME="$TMPDIR_HOME"
}

teardown() {
  rm -rf "$TMPDIR_BIN" "$TMPDIR_HOME"
}

@test "auto-detects docker when both engines available" {
  run "$WRAPPER" --version
  assert_success
  [ -f "$TMPDIR_BIN/argv.docker" ]
  [ ! -f "$TMPDIR_BIN/argv.podman" ]
}

@test "AWST_CONTAINER_ENGINE=podman overrides auto-detect" {
  AWST_CONTAINER_ENGINE=podman run "$WRAPPER" --version
  assert_success
  [ -f "$TMPDIR_BIN/argv.podman" ]
  [ ! -f "$TMPDIR_BIN/argv.docker" ]
}

@test "passes --user with host UID:GID" {
  run "$WRAPPER" list
  assert_success
  run grep -F -- "--user" "$TMPDIR_BIN/argv.docker"
  assert_success
  run grep -E "^$(id -u):$(id -g)$" "$TMPDIR_BIN/argv.docker"
  assert_success
}

@test "mounts ~/.aws, ~/.config/aws-tools, ~/.cache/aws-tools" {
  run "$WRAPPER" list
  assert_success
  run grep -F "$TMPDIR_HOME/.aws:/home/awst/.aws" "$TMPDIR_BIN/argv.docker"
  assert_success
  run grep -F "$TMPDIR_HOME/.config/aws-tools:/home/awst/.config/aws-tools" "$TMPDIR_BIN/argv.docker"
  assert_success
  run grep -F "$TMPDIR_HOME/.cache/aws-tools:/home/awst/.cache/aws-tools" "$TMPDIR_BIN/argv.docker"
  assert_success
}

@test "mounts ~/.granted only when it exists" {
  run "$WRAPPER" list
  assert_success
  run grep -F "$TMPDIR_HOME/.granted:/home/awst/.granted" "$TMPDIR_BIN/argv.docker"
  assert_success

  rm -rf "$TMPDIR_HOME/.granted"
  rm -f "$TMPDIR_BIN/argv.docker"

  run "$WRAPPER" list
  assert_success
  run grep -F "/.granted:/home/awst/.granted" "$TMPDIR_BIN/argv.docker"
  assert_failure
}

@test "forwards AWS_PROFILE and AWS_REGION env vars" {
  AWS_PROFILE=prod AWS_REGION=us-east-1 run "$WRAPPER" list
  assert_success
  run grep -F "AWS_PROFILE=prod" "$TMPDIR_BIN/argv.docker"
  assert_success
  run grep -F "AWS_REGION=us-east-1" "$TMPDIR_BIN/argv.docker"
  assert_success
}

@test "does not forward unset AWS_* vars" {
  unset AWS_PROFILE AWS_REGION AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN
  run "$WRAPPER" list
  assert_success
  run grep -F "AWS_PROFILE=" "$TMPDIR_BIN/argv.docker"
  assert_failure
}

@test "uses AWST_IMAGE override" {
  AWST_IMAGE=ghcr.io/example/aws-tools:v1.2.3 run "$WRAPPER" list
  assert_success
  run grep -F "ghcr.io/example/aws-tools:v1.2.3" "$TMPDIR_BIN/argv.docker"
  assert_success
}

@test "defaults --network host" {
  run "$WRAPPER" list
  assert_success
  run grep -F -- "--network" "$TMPDIR_BIN/argv.docker"
  assert_success
}

@test "AWST_NETWORK= omits --network entirely" {
  AWST_NETWORK= run "$WRAPPER" list
  assert_success
  run grep -F -- "--network" "$TMPDIR_BIN/argv.docker"
  assert_failure
}

@test "defaults --pid host (so list/kill see host ssm sessions)" {
  run "$WRAPPER" list
  assert_success
  run grep -F -- "--pid" "$TMPDIR_BIN/argv.docker"
  assert_success
}

@test "AWST_PID_MODE= omits --pid entirely" {
  AWST_PID_MODE= run "$WRAPPER" list
  assert_success
  run grep -F -- "--pid" "$TMPDIR_BIN/argv.docker"
  assert_failure
}

@test "exits 127 when no engine on PATH" {
  rm -f "$TMPDIR_BIN/docker" "$TMPDIR_BIN/podman"
  # Keep /usr/bin /bin so the wrapper's `#!/usr/bin/env bash` shebang resolves,
  # but drop the dirs that hold our fake docker/podman.
  PATH="/usr/bin:/bin" run -127 "$WRAPPER" --version
  assert_output --partial "neither docker nor podman"
}

@test "appends user args after the image" {
  run "$WRAPPER" exec -c uptime -i web-1
  assert_success
  # The argv file has one token per line; image must be followed by user args.
  local img_line cmd_line
  img_line=$(grep -n "^aws-tools:runtime$" "$TMPDIR_BIN/argv.docker" | head -1 | cut -d: -f1)
  [ -n "$img_line" ]
  cmd_line=$(awk -v ln="$img_line" 'NR>ln' "$TMPDIR_BIN/argv.docker")
  [[ "$cmd_line" == *"exec"* ]]
  [[ "$cmd_line" == *"uptime"* ]]
  [[ "$cmd_line" == *"web-1"* ]]
}
