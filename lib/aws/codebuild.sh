#!/usr/bin/env bash

source "$(dirname "${BASH_SOURCE[0]}")/../core/test_guard.sh"

guard_function_override aws_codebuild_list_builds || aws_codebuild_list_builds() {
  local project_name="$1"

  [[ -z "$project_name" ]] && {
    log_error "Project name required"
    return 1
  }

  log_debug "Listing active builds for CodeBuild project: $project_name"

  aws codebuild list-builds-for-project \
    --project-name "$project_name" \
    --no-paginate \
    --sort-order DESCENDING \
    --query 'ids' \
    --output json 2>/dev/null || {
    log_error "Failed to list builds for project: $project_name"
    return 1
  }
}

guard_function_override aws_codebuild_batch_get_builds || aws_codebuild_batch_get_builds() {
  local -a build_ids=("$@")

  [[ ${#build_ids[@]} -eq 0 ]] && {
    log_error "At least one build ID required"
    return 1
  }

  log_debug "Fetching build details for ${#build_ids[@]} builds"

  aws codebuild batch-get-builds \
    --ids "${build_ids[@]}" \
    --output json 2>/dev/null || {
    log_error "Failed to get build details"
    return 1
  }
}

guard_function_override aws_codebuild_get_debug_session_target || aws_codebuild_get_debug_session_target() {
  local build_id="$1"

  [[ -z "$build_id" ]] && {
    log_error "Build ID required"
    return 1
  }

  log_debug "Extracting debug session target from build: $build_id"

  local builds_json target
  builds_json=$(aws_codebuild_batch_get_builds "$build_id") || return 1
  target=$(echo "$builds_json" | jq -r '.builds[0].debugSession.sessionTarget // empty')

  if [[ -z "$target" || "$target" == "null" || "$target" == "None" ]]; then
    log_error "Build $build_id does not have a debug session enabled"
    return 1
  fi

  echo "$target"
}

guard_function_override aws_codebuild_select_build || aws_codebuild_select_build() {
  local project_name="$1"
  local explicit_build_id="$2"
  local subheader="${3:-}"

  [[ -z "$project_name" ]] && {
    log_error "Project name required"
    return 1
  }

  # If explicit build ID provided, use it directly
  if [[ -n "$explicit_build_id" ]]; then
    log_debug "Using explicit build ID: $explicit_build_id"
    echo "$explicit_build_id"
    return 0
  fi

  log_debug "Fetching builds for project: $project_name"

  local builds_json
  builds_json=$(aws_codebuild_list_builds "$project_name") || return 1

  local -a build_ids
  mapfile -t build_ids < <(echo "$builds_json" | jq -r '.[]')

  if (( ${#build_ids[@]} == 0 )); then
    log_error "No builds found for project: $project_name"
    return 1
  fi

  # Fetch first 10 builds to find ones with active debug sessions
  local max_check=10
  local -a check_ids=("${build_ids[@]:0:max_check}")

  local builds_detail_json
  builds_detail_json=$(aws_codebuild_batch_get_builds "${check_ids[@]}") || return 1

  # Extract builds with debug sessions enabled
  local -a debug_builds
  while IFS= read -r build_json; do
    local build_id status phase
    build_id=$(echo "$build_json" | jq -r '.id')
    status=$(echo "$build_json" | jq -r '.buildStatus // "UNKNOWN"')
    phase=$(echo "$build_json" | jq -r '.currentPhase // "UNKNOWN"')

    local has_debug
    has_debug=$(echo "$build_json" | jq -r '.debugSession.sessionTarget // empty')

    if [[ -n "$has_debug" ]]; then
      debug_builds+=("$build_id ($status - $phase)")
    fi
  done < <(echo "$builds_detail_json" | jq -c '.builds[]')

  if (( ${#debug_builds[@]} == 0 )); then
    log_error "No builds with debug sessions found for project: $project_name"
    return 1
  fi

  # Auto-select first if only one
  if (( ${#debug_builds[@]} == 1 )); then
    local chosen="${debug_builds[0]}"
    local build_id="${chosen%% *}"
    echo "$build_id"
    return 0
  fi

  # Auto-select first with --yes flag
  if [[ "${MENU_ASSUME_FIRST:-0}" == "1" ]]; then
    local chosen="${debug_builds[0]}"
    local build_id="${chosen%% *}"
    echo "$build_id"
    return 0
  fi

  # Interactive selection
  if [[ "${MENU_NON_INTERACTIVE:-0}" == "1" ]]; then
    log_error "Build selection requires interaction"
    return 1
  fi

  local chosen
  menu_select_one "Select build to connect to" "$subheader" chosen "${debug_builds[@]}" || return 130

  local build_id="${chosen%% *}"
  echo "$build_id"
}
