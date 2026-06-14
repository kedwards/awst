#!/usr/bin/env bats
# Exercises the real aws_get_all_running_instances (most other tests stub it).

load '../helpers/bats-support/load'
load '../helpers/bats-assert/load'

setup() {
  TMP_HOME="$(mktemp -d)"
  export HOME="$TMP_HOME"
  export PROFILE=testprofile
  export AWS_REGION=us-west-2
  export AWST_CACHE_TTL=0   # disable cache reuse between tests

  log_debug() { :; }
  log_info()  { :; }
  log_warn()  { :; }
  log_error() { echo "$@" >&2; }

  # Source under test
  source ./lib/core/test_guard.sh
  source ./lib/aws/ec2.sh
}

teardown() {
  rm -rf "$TMP_HOME"
  unset INSTANCE_LIST
}

@test "aws_get_all_running_instances returns failure on empty AWS output" {
  aws() {
    [[ "$1" == "ec2" && "$2" == "describe-instances" ]] || return 1
    # Empty output: account/region has no running instances
    printf ''
  }

  INSTANCE_LIST=()
  run aws_get_all_running_instances
  assert_failure
}

@test "aws_get_all_running_instances does not synthesize a phantom entry on empty output" {
  aws() {
    [[ "$1" == "ec2" && "$2" == "describe-instances" ]] || return 1
    printf ''
  }

  INSTANCE_LIST=()
  aws_get_all_running_instances || true
  [ "${#INSTANCE_LIST[@]}" -eq 0 ]
}

@test "aws_get_all_running_instances populates list from describe-instances output" {
  aws() {
    [[ "$1" == "ec2" && "$2" == "describe-instances" ]] || return 1
    printf 'i-aaa\tweb\ni-bbb\tdb\n'
  }

  INSTANCE_LIST=()
  run aws_get_all_running_instances
  assert_success

  # Re-run to populate INSTANCE_LIST in this shell (run uses a subshell)
  aws_get_all_running_instances
  [ "${#INSTANCE_LIST[@]}" -eq 2 ]
  [[ "${INSTANCE_LIST[*]}" == *"web i-aaa"* ]]
  [[ "${INSTANCE_LIST[*]}" == *"db i-bbb"* ]]
}

@test "aws_get_all_running_instances tags untagged instances 'unnamed'" {
  aws() {
    [[ "$1" == "ec2" && "$2" == "describe-instances" ]] || return 1
    printf 'i-aaa\t\n'
  }

  INSTANCE_LIST=()
  aws_get_all_running_instances
  [ "${#INSTANCE_LIST[@]}" -eq 1 ]
  [[ "${INSTANCE_LIST[0]}" == "unnamed i-aaa" ]]
}
