#!/usr/bin/env bats
# shellcheck disable=SC2329,SC2030,SC2031

export MENU_NON_INTERACTIVE=1
export AWST_EC2_DISABLE_LIVE_CALLS=1
export AWST_AUTH_DISABLE_ASSUME=1

load '../helpers/bats-support/load'
load '../helpers/bats-assert/load'

setup() {
  # enforce non-interactive test mode
  export MENU_NON_INTERACTIVE=1
  export MENU_ASSUME_FIRST=1

  # AWS auth stubs
  aws_auth_assume() { return 0; }

  # logging stubs
  log_debug(){ :; }
  log_info(){ :; }
  log_warn(){ :; }
  log_error(){ :; }

  # hard fail if menu is called
  menu_select_one() {
    echo "ERROR: menu_select_one should not be called in --yes mode" >&2
    return 99
  }

  # core stubs
  ensure_aws_cli(){ :; }

  choose_profile_and_region(){
    PROFILE=default
    REGION=us-west-2
  }

  awst_ssm_start_shell() {
    echo "SSM_SHELL $1"
  }

  # load code
  source ./lib/core/flags.sh
  source ./lib/aws/ec2.sh
  source ./lib/commands/awst_connect.sh

  # instance list stub (no live AWS call — safe to populate unconditionally)
  aws_get_all_running_instances() {
    INSTANCE_LIST=(
      "first-instance i-1111111111"
      "second-instance i-2222222222"
    )
  }
}

@test "ssm connect --yes auto-selects first instance" {
  run awst_connect --yes
  assert_success
  assert_output --partial "SSM_SHELL i-1111111111"
}

