#!/usr/bin/env bash

# global defaults
SHOW_HELP=false
CONFIG_MODE=false
ASSUME_YES=false
CONFIG_FILE=""
PROFILE=""
REGION=""
COMMAND_ARG=""
INSTANCES_ARG=""
CODEBUILD_MODE=false
CODEBUILD_PROJECT=""
CODEBUILD_BUILD_ID=""

POSITIONAL=()

parse_common_flags() {
  POSITIONAL=()

  while [[ $# -gt 0 ]]; do
    case "$1" in
      -c|--command)
        COMMAND_ARG="$2"
        shift 2
        ;;
      --config)
        CONFIG_MODE=true
        shift
        ;;
      --codebuild)
        CODEBUILD_MODE=true
        shift
        ;;
      --project-name)
        CODEBUILD_PROJECT="$2"
        shift 2
        ;;
      --build-id)
        CODEBUILD_BUILD_ID="$2"
        shift 2
        ;;
      -i|--instances)
        INSTANCES_ARG="$2"
        shift 2
        ;;
      -f|--file)
        CONFIG_FILE="$2"
        shift 2
        ;;
      -p|--profile)
        PROFILE="$2"
        shift 2
        ;;
      -r|--region)
        REGION="$2"
        shift 2
        ;;
      -h|--help)
        SHOW_HELP=true
        shift
        ;;
      -y|--yes|--assume-yes)
        ASSUME_YES=1
        MENU_NON_INTERACTIVE=1
        MENU_ASSUME_FIRST=1
        shift
        ;;
      --)
        shift
        POSITIONAL+=("$@")
        break
        ;;
      -*)
        # unknown flags are passed through
        POSITIONAL+=("$1")
        shift
        ;;
      *)
        POSITIONAL+=("$1")
        shift
        ;;
    esac
  done

  return 0
}
