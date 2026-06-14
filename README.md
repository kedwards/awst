# AWS Tools

A Bash-based CLI toolkit for AWS operations — SSM sessions, multi-instance command execution, profile-based scripting, and more.

## Features

- 🔐 **AWS Authentication** - Integration with [Granted](https://granted.dev) for AWS SSO login
- 🖥️ **Interactive Menus** - fzf-powered selection with fallback to bash `select`
- 🚀 **Shell Sessions** - Quick SSM session connections to EC2 instances
- ⚡ **Command Execution** - Run commands on multiple instances simultaneously
- 🔄 **Profile Iteration** - Run commands/scripts across multiple AWS profiles
- 🔑 **Credential Management** - Store and re-apply AWS credentials
- 📋 **Session Management** - List and terminate active SSM sessions
- 🔌 **Port Forwarding** - Config-based port forwarding to instances
- 💾 **Saved Commands** - Reusable command library with snippet placeholders
- ✅ **250+ Tests** - Comprehensive test coverage with BATS

## Installation

### Latest Release (Recommended)

```bash
curl -sSL https://raw.githubusercontent.com/kedwards/aws-tools/main/install.sh | bash
```

### Specific Version

```bash
# Install specific version
curl -sSL https://raw.githubusercontent.com/kedwards/aws-tools/main/install.sh | bash -s v0.1.0
```

### From Source (Development)

```bash
git clone https://github.com/kedwards/aws-tools
cd aws-tools
./install.sh
```

This installs to `~/.local/share/aws-tools` with symlinks in `~/.local/bin`.

### Check Version

```bash
awst --version
```

## Prerequisites

**Required:**
- `bash` (4.0+)
- `aws` CLI
- [`assume` (Granted)](https://granted.dev) - for AWS SSO authentication
- `session-manager-plugin` - for SSM connections
- `rsync` - used by `install.sh` and `update.sh` to sync run-commands

**Optional:**
- `fzf` - for enhanced interactive menus (falls back to bash `select`)

## Quick Start

### 1. Connect to an Instance

All commands authenticate automatically — no pre-login required. If no credentials are found, the tool will prompt for profile/region and log in via Granted.

```bash
# Interactive profile/region selection + instance selection
awst connect

# Direct connection with specific profile
awst connect -p prod -r us-east-1

# Config-based port forwarding
awst connect --config
```

### 2. Execute Commands

```bash
# Interactive: select command and instances
awst exec

# Explicit command on multiple instances
awst exec -c "uptime" -i "web-server;db-server"

# Use saved command with specific profile
awst exec -c disk-usage -i prod-app -p prod -r us-east-1
```

### 4. Run Across Profiles

```bash
# List available commands
awst run

# Run a saved snippet across profiles
awst run vpc-cidrs "dev prod"

# Run inline query
awst run -q "aws s3 ls" "staging:us-west-2"

# Run executable script
awst run instances
```

### 5. Credential Management

```bash
# Store credentials for an environment
eval "$(awst creds store myenv)"

# Re-apply stored credentials
eval "$(awst creds use)"
```

### 6. Manage Sessions

```bash
# List active sessions
awst list

# Terminate sessions
awst kill
```

## Commands

### `awst connect`
Start an SSM shell session or port forwarding to an EC2 instance.

**Options:**
- `-p, --profile` - AWS profile
- `-r, --region` - AWS region
- `-c, --config` - Use config-based port forwarding
- `-f, --file` - Config file path (default: `~/.ssmf.cfg`)
- `-n, --dry-run` - Show commands without executing

**Examples:**
```bash
# Interactive instance selection
awst connect -p prod

# Config-based port forwarding
awst connect --config -f ~/.ports.cfg
```

### `awst exec`
Execute a command on one or more EC2 instances via SSM.

**Options:**
- `-c <command>` - Command to execute
- `-p, --profile` - AWS profile
- `-r, --region` - AWS region
- `-i <instances>` - Semicolon-separated instance names/IDs
- `-n, --dry-run` - Show commands without executing
- `-y, --yes` - Non-interactive mode

**Examples:**
```bash
# Interactive command and instance selection
awst exec

# Explicit command on multiple instances
awst exec -c "df -h" -i "web1;web2;web3"

# Use saved command
awst exec -c system-uptime -p prod
```

### `awst run`
Run a command or script against one or more AWS profiles.

**Options:**
- `-q <command>` - Run an inline AWS command
- `-d <path>` - Use only this commands directory (overrides defaults)

**Command Directories (checked in priority order):**
1. `~/.local/share/aws-tools/run-commands/` — default scripts shipped with the tool
2. `~/.config/aws-tools/run-commands/` — your custom scripts (never overwritten by updates)

User scripts with the same name as a default script override the default. Use `-d` or `AWST_CMD_DIR` for an exclusive single-directory override.

**Filters:**
Space-separated profile names or `profile:region` pairs. When no filter is given, saved commands iterate all profiles. Default region is `us-east-1`.

**Snippet Placeholders:**
- `#ENV` - Replaced with the current profile name
- `#REGION` - Replaced with the current region

**Examples:**
```bash
# List available commands
awst run

# Run snippet across profiles
awst run vpc-cidrs "dev prod"

# Run with profile:region pairs
awst run cfn-stacks "prod:us-east-1 staging:us-west-2"

# Inline query
awst run -q "aws s3 ls" "prod staging"

# Run executable script directly
awst run instances

# Run executable per profile
awst run instances "dev:us-west-2"

# Custom commands directory (exclusive override)
awst run -d /path/to/commands my-script
```

### `awst creds`
Manage AWS credentials for the current shell.

**Subcommands:**
- `store <env>` - Export AWS credentials for `<env>` into the current shell
- `use` - Re-apply stored credentials (AK/SK/ST) as AWS_ env vars

**Examples:**
```bash
# Store credentials
eval "$(awst creds store myenv)"

# Re-apply stored credentials
eval "$(awst creds use)"
```

### `awst list`
List active SSM sessions on the current host.

**Example:**
```bash
awst list
```

### `awst kill`
Terminate active SSM sessions.

**Examples:**
```bash
# Interactive selection
awst kill

# Kill all sessions (with confirmation)
awst kill --all
```

### `awst update`
Update aws-tools to a specific version or the latest release.

**Examples:**
```bash
# Update to latest release
awst update

# Update to specific version
awst update v1.3.1

# Update to development branch
awst update main
```

## Configuration

### Saved Commands (`awst exec`)

Default commands are installed to `~/.local/share/aws-tools/commands.config` from `examples/commands.config`.

You can override or add commands in these locations (checked in order):
1. `~/.local/share/aws-tools/commands.config` (default commands, updated on install/update)
2. `~/.config/aws-tools/commands.user.config` (your custom commands, never overwritten)
3. Custom path via `$AWST_SSM_CMD_FILE` environment variable

**Format:**
```
# Command format: NAME|Description|Command to execute
disk-usage|Check disk usage|df -h
memory-info|Display memory information|free -h
docker-status|Check Docker containers|docker ps -a
```

**Adding Custom Commands:**
```bash
# Create user commands file (will never be overwritten by updates)
mkdir -p ~/.config/aws-tools
cat > ~/.config/aws-tools/commands.user.config <<'EOF'
# My custom commands
my-check|Custom health check|curl http://localhost:8080/health
restart-app|Restart application|systemctl restart myapp
EOF
```

### Run Commands (`awst run`)

Default run-commands are installed to `~/.local/share/aws-tools/run-commands/` from `examples/run-commands/`.

Bundled scripts:

| Command | Description | Type |
|---|---|---|
| `cfn-stacks` | CloudFormation stacks with status | snippet |
| `ecs-services` | ECS clusters and service status | script |
| `engine-ami-sync` | Sync engine AMI parameter store values | script |
| `engine-amis` | Engine AMI report with parameter store comparison | script |
| `iam-users` | IAM users with creation date and last password use | snippet |
| `instances` | Running instances with AMI name and creation date | script |
| `lambda-functions` | Lambda functions with runtime and memory | snippet |
| `rds-instances` | RDS instances with engine versions | snippet |
| `s3-buckets` | S3 buckets with region and creation date | snippet |
| `security-groups` | Security groups with VPC and description | snippet |
| `vpc-cidrs` | VPC CIDRs, names and account IDs | snippet |

**Adding Custom Run Commands:**
```bash
# Create user run-commands directory (never overwritten by updates)
mkdir -p ~/.config/aws-tools/run-commands

# Add a snippet (non-executable)
cat > ~/.config/aws-tools/run-commands/my-report <<'EOF'
# aws-tools command
# My custom AWS report
aws ec2 describe-instances --output table
EOF

# Add an executable script
cat > ~/.config/aws-tools/run-commands/my-script <<'EOF'
#!/usr/bin/env bash
# My custom script
echo "Running as profile: $AWS_PROFILE"
EOF
chmod +x ~/.config/aws-tools/run-commands/my-script
```

User scripts with the same name as a bundled script override the bundled version (shown with `+` in `awst run` listing).

### Port Forwarding Config

Create `~/.ssmf.cfg` with INI-style sections:

```ini
[postgres-prod]
profile = production
region = us-east-1
name = postgres-primary
host = localhost
port = 5432
local_port = 5432

[redis-staging]
profile = staging
region = us-west-2
name = redis-cache
host = localhost
port = 6379
local_port = 6379
```

Then use:
```bash
awst connect --config
```

## Environment Variables

### Logging
- `AWS_LOG_LEVEL` - DEBUG|INFO|WARN|ERROR (default: INFO)
- `AWS_LOG_COLOR` - 1=enabled, 0=disabled (default: 1)
- `AWS_LOG_TIMESTAMP` - 1=show, 0=hide (default: 1)
- `AWS_LOG_FILE` - Log file path (default: none)

### Behavior
- `MENU_NON_INTERACTIVE` - Disable interactive prompts
- `MENU_NO_FZF` - Force bash `select` instead of fzf
- `AWST_SSM_CMD_FILE` - Custom commands file path
- `AWST_CMD_DIR` - Exclusive single-directory override for `awst run` (bypasses default + user dir merging)
- `AWST_AUTH_DISABLE_ASSUME` - Set to `1` to skip assume calls (for testing)

## Updating

Update to the latest release:

```bash
awst update
```

Update to a specific version:

```bash
awst update v1.3.1
```

Update to development version (main branch):

```bash
awst update main
```

## PATH Configuration

Ensure `~/.local/bin` is in your PATH:
```bash
export PATH="$HOME/.local/bin:$PATH"
```

## Development

### Setup

When cloning the repository, initialize the test dependencies (BATS helper libraries):

```bash
git clone https://github.com/kedwards/aws-tools
cd aws-tools
git submodule update --init --recursive
```

### Running Tests

**Prerequisites:**
- BATS (Bash Automated Testing System)
- Test helpers (installed via git submodules)

```bash
# All unit tests
task test

# Or use bats directly
bats test/unit/

# Run specific test file
bats test/unit/awst_exec.bats

# Run specific test
bats test/unit/awst_exec.bats -f "polls for command completion"
```

### Linting

```bash
task lint

# Or check specific file
shellcheck lib/core/logging.sh
```

### CI

```bash
# Run all checks (lint + unit tests)
task ci
```

### Container-Based Development

If Docker (or Podman) is available but you don't want to install `bats`, `task`,
or `shellcheck` on the host, run the suite inside the dev image:

```bash
task docker:build   # build aws-tools:dev (debian:stable-slim + tooling)
task docker:test    # bats test/unit
task docker:lint    # shellcheck
task docker:ci      # both
task docker:shell   # interactive shell with the repo at /work
```

The repo is bind-mounted at `/work` and commands run under your host UID/GID,
so no root-owned files end up in the tree. The dev image is defined in
`containers/Dockerfile.dev`.

### Running `awst` in a Container (Preview)

A runtime image and host wrapper let you run the full toolkit without installing
`aws-cli` or `session-manager-plugin` on the host. Currently opt-in — the
default `install.sh` flow is unchanged.

```bash
task docker:build:runtime   # build aws-tools:runtime (~520 MB)

# Run any awst command via the wrapper:
containers/awst-host --version
containers/awst-host list
containers/awst-host connect -p prod -r us-east-1
```

The wrapper:
- auto-detects `docker` vs `podman` (override with `AWST_CONTAINER_ENGINE=podman`)
- bind-mounts `~/.aws`, `~/.granted`, `~/.config/aws-tools`, `~/.cache/aws-tools`
- runs as your host UID/GID, forwards `AWS_PROFILE`/`AWS_REGION`/SSO tokens
- uses host networking + host PID namespace so port forwarding and
  `awst list`/`kill` keep working against sessions started outside the container

**Auth model:** run `assume` (Granted) on the *host* first; the container reads
the cached SSO credentials from the mounted `~/.aws/sso/cache/`. Granted itself
is not installed inside the runtime image.

**Convenient alias:**
```bash
alias awst="$PWD/containers/awst-host"   # or use the absolute install path
```

See `CONTAINERS_PLAN.md` for the rollout phases.

### Releases

For maintainers creating releases:

```bash
# Show current version
task version

# Create a new release interactively
task release

# Or create specific release types
task release:patch   # 0.1.0 -> 0.1.1 (bug fixes)
task release:minor   # 0.1.0 -> 0.2.0 (new features)
task release:major   # 0.1.0 -> 1.0.0 (breaking changes)
```

See [RELEASE.md](RELEASE.md) for detailed release management documentation.

## Troubleshooting

### "No AWS credentials found"

All commands auto-login when a profile is provided. If you see this error, pass a profile:
```bash
awst connect -p your-profile -r us-east-1
```

Or authenticate with Granted directly:
```bash
assume your-profile -r us-east-1
```

### "session-manager-plugin not found"

Install the Session Manager plugin:
https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html

### fzf not working

Install fzf for better menus, or the tool will fall back to bash `select`:
```bash
# macOS
brew install fzf

# Ubuntu/Debian
apt install fzf

# Arch
pacman -S fzf
```

## License

MIT License - See LICENSE file for details

## Contributing

Contributions welcome! Please:
1. Run tests: `task test`
2. Run linter: `task lint`
3. Follow existing code style
4. Add tests for new features

## Credits

Built with:
- [Granted](https://granted.dev) - AWS SSO authentication
- [BATS](https://github.com/bats-core/bats-core) - Bash testing framework
- [fzf](https://github.com/junegunn/fzf) - Command-line fuzzy finder
