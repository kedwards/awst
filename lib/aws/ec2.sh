#!/usr/bin/env bash

source "$(dirname "${BASH_SOURCE[0]}")/../core/test_guard.sh"

guard_function_override aws_expand_instances || aws_expand_instances() {
  local name="$1"

  # If already an instance ID, return it directly
  if [[ "$name" == i-* ]]; then
    echo "$name"
    return 0
  fi

  log_debug "Expanding instance name '$name' via EC2 describe-instances"

  # Query AWS for instances with matching Name tag
  aws ec2 describe-instances \
    --filters "Name=instance-state-name,Values=running" "Name=tag:Name,Values=$name" \
    --query 'Reservations[].Instances[].InstanceId' \
    --output text 2>/dev/null | tr '\t' '\n'
}

guard_function_override aws_ec2_select_instance || aws_ec2_select_instance() {
  local prompt="$1"
  local target="$2"
  local subheader="${3:-}"

  local instance_id instance_name

  if [[ -z "$target" ]]; then
    log_debug "Target is empty, proceeding to interactive menu"
    aws_get_all_running_instances || return 1

    (( ${#INSTANCE_LIST[@]} == 0 )) && {
      log_error "No running EC2 instances found"
      return 1
    }

    # auto-select first (used by --yes)
    if [[ "${MENU_ASSUME_FIRST:-0}" == "1" ]]; then
      local chosen="${INSTANCE_LIST[0]}"
      instance_name="${chosen% *}"
      instance_id="${chosen##* }"
      printf '%s %s\n' "$instance_name" "$instance_id"
      return 0
    fi

    # interactive selection
    if [[ "${MENU_NON_INTERACTIVE:-0}" == "1" ]]; then
      log_error "Instance selection requires interaction"
      return 1
    fi

    local chosen
    menu_select_one "$prompt" "$subheader" chosen "${INSTANCE_LIST[@]}" || return 130

    instance_name="${chosen% *}"
    instance_id="${chosen##* }"

  elif [[ "$target" == i-* ]]; then
    instance_id="$target"
    instance_name="$target"

  else
    instance_id="$(aws_expand_instances "$target" | head -n1)"
    instance_name="$target"
    [[ -z "$instance_id" ]] && return 1
  fi

  printf '%s %s\n' "$instance_name" "$instance_id"
}

guard_function_override aws_get_all_running_instances || aws_get_all_running_instances() {
  local profile="${PROFILE:-default}"
  local region="${AWS_REGION:-${AWS_DEFAULT_REGION:-}}"


  [[ -z "$region" ]] && {
    log_error "AWS region not set"
    return 1
  }

  local cache_dir="$HOME/.cache/aws-tools"
  local cache_file="$cache_dir/instances_${profile}_${region}.cache"
  local ttl="${AWST_CACHE_TTL:-30}"

  mkdir -p "$cache_dir"

  INSTANCE_LIST=()  # nounset-safe

  # use cache if fresh
  if [[ -f "$cache_file" ]]; then
    local now mtime
    now="$(date +%s)"
    mtime="$(stat -c %Y "$cache_file" 2>/dev/null || echo 0)"

    if (( now - mtime < ttl )); then
      mapfile -t INSTANCE_LIST < "$cache_file"
      (( ${#INSTANCE_LIST[@]} > 0 )) || return 1
      return 0
    fi
  fi

  # fetch from AWS
  local output
  if ! output="$(
    aws ec2 describe-instances \
      --filters Name=instance-state-name,Values=running \
      --query 'Reservations[].Instances[].[
        InstanceId,
        Tags[?Key==`Name`].Value | [0]
      ]' \
      --output text 2>/dev/null
  )"; then
    log_error "Failed to query EC2 instances (are you logged in?)"
    return 1
  fi

  while read -r instance_id name; do
    # `<<<""` yields one empty line; skip it so we don't synthesize a phantom
    # "unnamed " entry when the account/region has no running instances.
    [[ -z "$instance_id" ]] && continue
    name="${name:-unnamed}"
    INSTANCE_LIST+=( "$name $instance_id" )
  done <<<"$output"

  (( ${#INSTANCE_LIST[@]} > 0 )) || return 1

  # sort list before caching
  mapfile -t INSTANCE_LIST < <(printf '%s\n' "${INSTANCE_LIST[@]}" | sort)

  # write cache
  printf '%s\n' "${INSTANCE_LIST[@]}" >"$cache_file"

  return 0
}
