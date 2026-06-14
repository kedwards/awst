#!/usr/bin/env bats
# shellcheck disable=SC2329,SC2030,SC2031

export MENU_NON_INTERACTIVE=1
export AWST_CODEBUILD_DISABLE_LIVE_CALLS=1
export AWST_AUTH_DISABLE_ASSUME=1

load '../helpers/bats-support/load'
load '../helpers/bats-assert/load'

setup() {
  # Reset global flags
  CONFIG_MODE=false
  CODEBUILD_MODE=false
  CODEBUILD_PROJECT=""
  CODEBUILD_BUILD_ID=""
  SHOW_HELP=false
  PROFILE=""
  REGION=""

  # AWS auth stubs
  aws_auth_assume(){ :; }

  # logging stubs
  log_debug(){ :; }
  log_info(){ :; }
  log_warn(){ :; }
  log_error(){ :; }

  # Core stubs
  ensure_aws_cli(){ :; }
  choose_profile_and_region(){ :; }
  aws_sso_validate_or_login(){ :; }

  # menu dependency (REAL implementation)
  source ./lib/menu/index.sh
  source ./lib/core/flags.sh
  source ./lib/commands/awst_connect.sh
  source ./lib/aws/codebuild.sh

  # aws ssm stub
  awst_ssm_start_shell() {
    echo "SSM_SHELL $1"
  }
}

@test "codebuild connect with project name extracts debug session target" {
  export CODEBUILD_MODE=true
  export CODEBUILD_PROJECT="DatabaseBaseline"
  export MENU_NON_INTERACTIVE=1
  export MENU_ASSUME_FIRST=1

  # Mock CodeBuild functions
  aws_codebuild_list_builds() {
    echo '["build-1", "build-2"]'
  }

  aws_codebuild_batch_get_builds() {
    cat <<'EOF'
{
  "builds": [
    {
      "id": "build-1",
      "buildStatus": "IN_PROGRESS",
      "currentPhase": "PHASE_1",
      "debugSession": {
        "sessionTarget": "arn:aws:ec2:us-east-1:123456789012:instance/i-1234567890"
      }
    }
  ]
}
EOF
  }

  run awst_connect --codebuild --project-name DatabaseBaseline

  assert_success
  assert_output --partial "SSM_SHELL arn:aws:ec2:us-east-1:123456789012:instance/i-1234567890"
}

@test "codebuild connect fails without project name" {
  export CODEBUILD_MODE=true
  export MENU_NON_INTERACTIVE=1

  run awst_connect --codebuild

  assert_failure
  assert_output --partial "CodeBuild project name required"
}

@test "codebuild connect fails if build has no debug session" {
  export CODEBUILD_MODE=true
  export CODEBUILD_PROJECT="DatabaseBaseline"
  export MENU_NON_INTERACTIVE=1
  export MENU_ASSUME_FIRST=1

  # Mock CodeBuild functions
  aws_codebuild_list_builds() {
    echo '["build-1"]'
  }

  aws_codebuild_batch_get_builds() {
    cat <<'EOF'
{
  "builds": [
    {
      "id": "build-1",
      "buildStatus": "FAILED",
      "currentPhase": "UNKNOWN"
    }
  ]
}
EOF
  }

  run awst_connect --codebuild --project-name DatabaseBaseline

  assert_failure
  assert_output --partial "does not have a debug session"
}

@test "codebuild connect with explicit build ID" {
  export CODEBUILD_MODE=true
  export CODEBUILD_PROJECT="DatabaseBaseline"
  export CODEBUILD_BUILD_ID="build-123"

  aws_codebuild_get_debug_session_target() {
    echo "arn:aws:ec2:us-east-1:123456789012:instance/i-9876543210"
  }

  run awst_connect --codebuild --project-name DatabaseBaseline --build-id build-123

  assert_success
  assert_output --partial "SSM_SHELL arn:aws:ec2:us-east-1:123456789012:instance/i-9876543210"
}

@test "codebuild connect includes profile and region in subheader" {
  export CODEBUILD_MODE=true
  export CODEBUILD_PROJECT="DatabaseBaseline"
  export PROFILE="prod"
  export REGION="us-west-2"
  export MENU_NON_INTERACTIVE=1
  export MENU_ASSUME_FIRST=1

  # Mock CodeBuild functions
  aws_codebuild_list_builds() {
    echo '["build-1"]'
  }

  aws_codebuild_batch_get_builds() {
    cat <<'EOF'
{
  "builds": [
    {
      "id": "build-1",
      "buildStatus": "IN_PROGRESS",
      "currentPhase": "PHASE_1",
      "debugSession": {
        "sessionTarget": "arn:aws:ec2:us-west-2:123456789012:instance/i-test"
      }
    }
  ]
}
EOF
  }

  # Stub to capture subheader
  aws_codebuild_select_build() {
    local subheader="$3"
    [[ "$subheader" =~ "prod" ]] && [[ "$subheader" =~ "us-west-2" ]] || return 1
    echo "build-1"
  }

  run awst_connect --codebuild --project-name DatabaseBaseline

  assert_success
  assert_output --partial "Connecting to CodeBuild build: build-1"
}

@test "codebuild list builds disables pagination when sorting descending" {
  aws() {
    local args="$*"
    [[ "$args" == *"codebuild list-builds-for-project"* ]] || return 1
    [[ "$args" == *"--no-paginate"* ]] || return 1
    [[ "$args" == *"--sort-order DESCENDING"* ]] || return 1
    echo '["build-1"]'
  }

  run aws_codebuild_list_builds "DatabaseBaseline"

  assert_success
  assert_output '["build-1"]'
}

