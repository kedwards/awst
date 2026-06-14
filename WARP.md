# WARP.md

This file provides guidance to WARP (warp.dev) when working with code in this repository.

## Project Overview

This is `aws-tools`, a Bash-based CLI toolkit for AWS operations. The tool provides interactive menus (with fzf support) for SSM sessions, multi-instance command execution, profile-based scripting, credential management, and more.

## Development Commands

### Setup

When first cloning the repository, initialize git submodules for test dependencies:

```bash
git submodule update --init --recursive
```

This installs:
- `test/helpers/bats-support` - BATS support library
- `test/helpers/bats-assert` - BATS assertion library

### Testing
```bash
# Run unit tests (no AWS authentication required)
task test

# Run integration tests (requires AWS authentication)
task test:integration

# Run single test file
bats test/unit/menu_select_one.bats

# Run specific test
bats test/unit/menu_select_one.bats -f "cancel returns error code 130"
```

### Linting
```bash
# Run shellcheck on main menu module
task lint

# Check specific file
shellcheck lib/core/logging.sh
```

### CI
```bash
# Run all checks (lint + unit tests)
task ci
```

### Container-Based Development (Phase 1)

A `debian:stable-slim` dev image bundles `bats`, `shellcheck`, `shfmt`,
`task`, `aws-cli` v2, `session-manager-plugin`, `fzf`, and `jq`. Use it
when the host lacks any of these tools.

```bash
task docker:build   # build aws-tools:dev (skipped if Dockerfile unchanged)
task docker:test    # bats test/unit inside the container
task docker:lint    # shellcheck inside the container
task docker:ci      # lint + tests inside the container
task docker:shell   # interactive shell with the repo bind-mounted at /work
```

The image is defined in `containers/Dockerfile.dev`. The repo is bind-mounted
at `/work`; container processes run under the host UID/GID via
`--user $(id -u):$(id -g)`. `HOME=/tmp` is set so on-first-run config
auto-deploy in `bin/awst` doesn't write into the repo.

`CONTAINERS_PLAN.md` describes the full multi-phase containerization rollout
(Phase 1: dev image + `task docker:*`; Phase 2: runtime image + host wrapper;
Phase 3: container-first installer).

### Installation
```bash
# Install to ~/.local/share/aws-tools with symlinks in ~/.local/bin
./install.sh

# Update existing installation
./update.sh
```

### Releases
```bash
# Show current version
task version

# Create a new release (interactive)
task release

# Or create specific release types
task release:patch   # Bug fixes: 0.1.0 -> 0.1.1
task release:minor   # New features: 0.1.0 -> 0.2.0
task release:major   # Breaking changes: 0.1.0 -> 1.0.0
```

See [RELEASE.md](RELEASE.md) for detailed release documentation.

## Architecture

### Code Organization

The codebase follows a layered architecture with clear separation of concerns:

**Core Layer** (`lib/core/`)
- `logging.sh` - Structured logging with levels, colors, timestamps, and file rotation
- `flags.sh` - Common flag parsing (--dry-run, --profile, --region, --yes, --config)
- `interaction.sh` - Interactive mode guards and browser opening
- `aws_auth.sh` - AWS authentication via Granted (SSO) with validation and active login
- `aws.sh` - AWS CLI utilities
- `tty.sh` - TTY detection
- `test_guard.sh` - Function override protection for tests

**AWS Layer** (`lib/aws/`)
- `ec2.sh` - EC2 instance listing with caching (30s TTL), Name tag resolution
- `ssm.sh` - SSM session start wrappers (shell and port forwarding)

**Menu System** (`lib/menu/`)
- `index.sh` - Entry point that loads all menu components
- `select_one.sh` - Single-item selection with fzf/fallback support
- `select_many.sh` - Multi-item selection with fzf/fallback support
- `_common.sh` - Shared menu utilities
- `backends/auto.sh` - Auto-detect fzf availability
- `backends/fzf.sh` - fzf-specific implementation
- `backends/fallback.sh` - Bash `select` fallback

**Commands** (`lib/commands/`)
- `awst_connect.sh` - Connect to instances (shell or port-forward modes)
- `awst_exec.sh` - Execute commands on multiple instances with polling and output display
- `awst_run.sh` - Run commands/scripts across AWS profiles (ported from aws-tools)
- `awst_creds.sh` - AWS credential store/use management
- `awst_list.sh` - List active SSM sessions
- `awst_kill.sh` - Terminate SSM sessions

**Entry Point** (`bin/awst`)
- Main dispatcher that sources all libraries and routes subcommands

### Key Architectural Patterns

**Test Guard Pattern**
Functions that interact with external systems (AWS, fzf) use `guard_function_override` to allow test stubs to take precedence. Tests define stubs before sourcing the real implementation:
```bash
# In tests
aws_ec2_select_instance() { echo "stub"; }
source ./lib/aws/ec2.sh  # Real function won't override stub
```

**Non-Interactive Mode**
The tool supports both interactive and non-interactive usage. Commands respect:
- `MENU_NON_INTERACTIVE=1` - Explicitly disable interaction
- `MENU_ASSUME_FIRST=1` - Auto-select first item in menus (used with --yes)
- `CI=true` - Detect CI environments

**Instance Caching**
EC2 instances are cached in `~/.cache/aws-tools/instances_${profile}_${region}.cache` with 30s TTL to reduce API calls during interactive selection.

**Error Code Convention**
- `130` - User cancelled/ESC pressed (mirrors fzf convention)
- `1` - General error

**Dry-Run Mode**
All commands support `--dry-run` which:
- Skips AWS authentication entirely
- Prints commands that would be executed
- Never makes external calls

### Testing Strategy

**Unit Tests** (`test/unit/`)
- Use BATS (Bash Automated Testing System)
- Stub all external dependencies (AWS CLI, fzf, logging)
- Set `export AWST_EC2_DISABLE_LIVE_CALLS=1` and `AWST_AUTH_DISABLE_ASSUME=1`
- Test flags, menu selection logic, command dispatch

**Integration Tests** (`test/integration/`)
- Currently empty (placeholder for future AWS integration tests)
- Would require valid AWS credentials

**Test Helpers** (`test/helpers/`)
- `bats-support/` and `bats-assert/` - BATS testing libraries (git submodules)
- `menu_harness.sh` - Common stubs for menu tests
- Fake `fzf` wrapper for testing menu backends

**Note:** Test helper libraries are managed as git submodules. Run `git submodule update --init --recursive` after cloning.

### Flag Handling

All commands inherit common flags via `parse_common_flags`:
- `-n, --dry-run` - Show commands without executing
- `-p, --profile` - AWS profile
- `-r, --region` - AWS region
- `-c, --config` - Enable config-based port forwarding
- `-f, --file` - Config file override (default: `~/.ssmf.cfg`)
- `-y, --yes` - Non-interactive mode (auto-select first option)
- `-h, --help` - Show help

### Logging

Use structured logging functions from `lib/core/logging.sh`:
```bash
log_debug "detailed info"
log_info "informational message"
log_success "operation succeeded"
log_warn "warning message"
log_error "error occurred"
log_fatal "critical error" # exits with code 1
```

Control via environment variables:
- `AWS_LOG_LEVEL` - DEBUG|INFO|WARN|ERROR (default: INFO)
- `AWS_LOG_COLOR` - 1=enabled, 0=disabled (default: 1)
- `AWS_LOG_TIMESTAMP` - 1=show, 0=hide (default: 1)
- `AWS_LOG_FILE` - Log file path (default: none)

### Menu System Usage

The menu system provides consistent interactive selection with automatic fzf detection:

```bash
# Single selection
menu_select_one "Select instance" "Header text" result_var "${array[@]}"
echo "Selected: $result_var"

# Multi-selection
menu_select_many "Select instances" "Header" results "${array[@]}"
# results contains newline-separated selections
```

Menu behavior:
- Automatically uses fzf if available (unless `MENU_NO_FZF=1`)
- Falls back to Bash `select` built-in
- Returns 130 on cancel/ESC
- Respects non-interactive flags

## AWS Configuration

The tool uses **Granted** for AWS authentication.

### Saved Commands

All commands are stored as individual files under a unified `commands/` directory with two subdirectories:
- `commands/aws/` - Commands that run locally against AWS APIs (used by `awst run`)
- `commands/ssm/` - Commands sent to instances via SSM (used by `awst exec`)

**SSM commands** (`commands/ssm/`) are loaded from `lib/core/commands.sh` in the following order:
1. `~/.local/share/aws-tools/commands/ssm/` - Default commands (installed from `examples/commands/ssm/`)
2. `~/.config/aws-tools/commands/ssm/` - User custom commands (never overwritten)
3. `$AWST_SSM_CMD_DIR` - Custom directory via environment variable

Later directories override earlier ones by filename. The installer deploys `examples/commands/ssm/` to the default location during install/update.

**File format** (same for both `aws/` and `ssm/`):
```
# Description (first comment line)
command_body_here
```

Filename = command name. Example (`commands/ssm/disk-usage`):
```
# Check disk usage
df -h
```

### Port Forwarding

Config-based port forwarding uses INI-style config files:

**Single Port:**
```ini
[my-db]
profile = prod
region = us-east-1
name = postgres-primary
host = localhost
port = 5432
local_port = 5432
url = http://localhost:5432
```

**Multiple Ports:**
```ini
[Monitoring-All]
profile = ps
region = us-west-2
name = Monitoring
host = localhost
ports = 8428,9093
local_ports = 8428,9093
```

Use `ports` and `local_ports` (comma-separated) to forward multiple ports with a single command. Each port opens in a background session.

Config sections can be selected interactively via `awst connect --config`.

## Dependencies

**Required:**
- `bash` (4.0+)
- `aws` CLI
- `assume` (Granted) - for AWS SSO authentication
- `rsync` - used by install.sh/update.sh to sync commands
- BATS - for running tests

**Optional:**
- `fzf` - for enhanced interactive menus (falls back to Bash `select`)
- `shellcheck` - for linting

## Implementation Status

**Completed Commands** (276 tests passing):
- `awst connect` - Shell sessions and config-based port forwarding
- `awst exec` - Multi-instance command execution (54 tests)
  - Saved command support from `commands/ssm/` files
  - Interactive or CLI-driven instance selection
  - Semicolon-separated multi-instance targeting
  - Real-time polling with status updates
  - Stdout/stderr display from all instances
- `awst run` - Run commands/scripts across AWS profiles (integrated from aws-tools)
  - Snippet files with `#ENV`/`#REGION` placeholder substitution
  - Executable scripts (run directly or iterated per-profile)
  - Inline queries via `-q`
  - Multi-source command resolution: installed defaults + user dir (merged, user overrides)
  - Custom commands directory via `-d` or `AWST_CMD_DIR` (exclusive override)
  - 11 bundled commands deployed to `~/.local/share/aws-tools/commands/aws/`
  - User scripts in `~/.config/aws-tools/commands/aws/` (never overwritten)
  - Profile iteration with `source assume` per entry
  - Filter by profile or profile:region pairs
- `awst creds` - AWS credential management
  - `store <env>` - Capture credentials via Granted into shell env vars
  - `use` - Re-apply stored AK/SK/ST as AWS_ env vars
- `awst list` - List active SSM sessions
- `awst kill` - Terminate active sessions

### AWS Auth Layer (Unified)
All commands use the same authentication flow — no pre-login required.

The auth layer (`lib/core/aws_auth.sh`) provides:
- `aws_auth_assume(profile, region)` - Checks for existing credentials; if none found and a profile is available, automatically calls `aws_auth_login` to authenticate. Used by `connect`, `exec`.
- `aws_auth_login(profile, region)` - Actively authenticates by calling `source assume`. Used by `run` (per-profile iteration) and as auto-login fallback in `aws_auth_assume`.
- `choose_profile_and_region()` - Resolves profile/region from flags, env vars, or interactive selection. Called before auth in all commands.

### Profile-Iteration Commands (awst run)
The `awst run` command resolves scripts from multiple directories in priority order:
1. `~/.local/share/aws-tools/commands/aws/` - Default scripts shipped with the tool (from `examples/commands/aws/`)
2. `~/.config/aws-tools/commands/aws/` - User-defined scripts (never overwritten by install/update)

A user script with the same name as a default script overrides it. Both directories are merged when listing commands — user scripts are marked with `+` in the output.

Using `-d <path>` or setting `AWST_CMD_DIR` bypasses merging and uses only that single directory.

**Snippet files** (non-executable) contain AWS CLI commands with optional placeholders:
```
# aws-tools command
# VPC CIDRs, names and account IDs
aws ec2 describe-vpcs --query 'Vpcs[*].[...]' --output table
```

**Executable scripts** (chmod +x) are full bash scripts invoked directly.
When a filter is provided, they run once per profile after `source assume`.

**Bundled AWS commands** (in `examples/commands/aws/`):
- `cfn-stacks` - CloudFormation stacks with status
- `ecs-services` - ECS clusters and service status `*`
- `engine-ami-sync` - Sync engine AMI parameter store values `*`
- `engine-amis` - Engine AMI report with parameter store comparison `*`
- `iam-users` - IAM users with creation date and last password use
- `instances` - Running instances with AMI name and creation date `*`
- `lambda-functions` - Lambda functions with runtime and memory
- `rds-instances` - RDS instances with engine versions
- `s3-buckets` - S3 buckets with region and creation date
- `security-groups` - Security groups with VPC and description
- `vpc-cidrs` - VPC CIDRs, names and account IDs

`*` = executable script (runs directly without profile iteration when no filter given)

**Bundled SSM commands** (in `examples/commands/ssm/`):
- `disk-usage` - Check disk usage
- `memory-info` - Display memory information
- `system-uptime` - Show system uptime
- `docker-status` - Check Docker containers status
