# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [3.16.0] - 2026-09-14

### Changed

- **`awst exec` and `awst run` now present one command surface.** Both take
  the command body from exactly one of `--command/-c` (inline), `--file/-f`
  (a path), or a positional saved-command name, both read the same
  `# key: value` header + verbatim body file format, both layer their
  commands directories the same way, and both accept `--dir/-d`,
  `--list/-l`, `--profile/-p`, and `--region/-r`. With no body given, a
  terminal shows a picker of saved commands and a pipe/CI prints the list
  and exits non-zero. What remains different is only what has to be:
  `exec` targets instances with `--instances/-i`, `run` fans out across
  profiles with its positional filter and runs executable scripts directly.

Breaking, all on `awst run`:

- `--query/-q` is gone; the inline command flag is `--command/-c`, matching
  `exec` and the rest of the CLI.
- Bare `awst run` no longer lists commands — it now picks one interactively
  (or lists and exits non-zero in a pipe/CI). Use `awst run --list/-l` for
  the old listing.
- Command files are no longer stripped of comments and blank lines. The
  leading `# key: value` header is parsed and everything after it is passed
  to `sh -c` byte-for-byte, so heredocs and inline comments work.
- The `#ENV` and `#REGION` placeholders are no longer substituted. Use
  `$AWS_PROFILE` and `$AWS_REGION`, which have been exported into the child
  environment since 2.x.
- A profile that fails to authenticate or whose command exits non-zero now
  makes `awst run` itself exit non-zero, listing the failed profiles.
  Previously it always exited 0.
- `AWST_EXEC_CMD_DIR` is replaced by the layered `AWST_EXEC_CMD_BASE` /
  `AWST_EXEC_CMD_USER` pair, mirroring `AWST_RUN_CMD_BASE` /
  `AWST_RUN_CMD_USER`. (`AWST_EXEC_CMD_DIR` never shipped in a release.)

### Added

- `awst exec` accepts saved command files via `--file/-f <path>` or a
  positional name resolved from the commands directory
  (`~/.config/aws-tools/commands/ssm`). Saved command files are plain
  scripts with an optional `# key: value` header block
  (description/profile/region/instances) — the body is sent verbatim, so
  heredocs, embedded comments, and blank lines survive untouched. Explicit
  flags always override header values. `-i` is now optional; omitting it
  prompts interactively (or errors in a pipe/CI).
- `awst exec` triggers the same SSO device-flow login as `connect`,
  `console`, and `run` when a profile's cached token is missing or expired,
  instead of failing with an auth error.
- `awst run` accepts `--file/-f`, `--list/-l`, `--targets/-t`,
  `--profile/-p`, and `--region/-r`. `-t` takes the same
  `"profile"` / `"profile:region"` filter as the trailing positional, but
  leaves the positional slot free — so `awst run -t "dev prod"` names the
  targets and still picks the command interactively, mirroring
  `awst exec -i web`. Passing both `-t` and the positional filter is an
  error. `-p`/`-r` (or a command file's `# profile:` / `# region:`
  header) select a single target without the positional filter; passing
  both a filter and `-p` is an error.

## [3.15.0] - 2026-09-02

### Changed

- `--profile` / `-p` (and the positional `[profile]` on `login`/`logout`) now does case-insensitive substring matching against profiles in `~/.aws/config`, on every command that takes a profile (`login`, `logout`, `console`, `connect`, `exec`, `run`). An exact match is used as-is. A single substring match is auto-selected (with a note on stderr). Multiple matches show an interactive picker (or error in a pipe/CI with the list of candidates). No matches pass the value through unchanged (the SDK produces the error).

## [3.14.0] - 2026-08-13

### Removed

- Windows support.

### Added

- fish shell integration.

## [3.13.0] - 2026-07-17

### Changed
- `awst run` no longer fans out across every profile in `~/.aws/config` when no `profile:region` filter is given. In a terminal it now shows an interactive picker: multi-select the profiles, then choose a region for each. With no filter and no terminal (pipe/CI) it errors instead of running against all accounts.
- `awst run` now triggers the same SSO device-flow login as `connect` and `console` (via `sso.EnsureToken`) when a profile's cached token is missing or expired, instead of silently skipping the profile.
- The `run` filter argument accepts comma- and/or space-separated tokens, so `"dev:us-east-1,prod:us-west-2"` and `"dev prod"` both work.

## [3.9.1] - 2026-06-28

### Changed
- Tidied branding in `--help` and `awst config` output: dropped "Go port" / bash-comparison phrasing, added the repo URL to `awst config`, and minor wording fixes. No behavior change.

## [3.9.0] - 2026-06-28

### Added
- `awst connect --project-name NAME` connects to a running CodeBuild build that has a debug session enabled, opening an SSM shell against the build's debug session target. It auto-selects a lone debug build, prompts to pick when there are several, or lists candidates and errors (asking for `--build-id`) when non-interactive. Pass `--build-id` to connect to a specific build directly.

### Changed
- `awst connect --forward` (and saved connections) now detaches the port-forward into the background by default, freeing the shell immediately and printing the PID plus the `ps`/`kill` commands to manage it. The forward runs in its own session so it survives the shell closing, with output redirected to a per-port log under the data dir. Pass `--foreground`/`-F` (or `AWST_CONNECT_FOREGROUND=1`) to keep the previous blocking, Ctrl+C-to-stop behavior. Shell sessions are unaffected.

## [3.8.0] - 2026-06-25

### Changed
- `awst console` now auto-detects the Granted Containers Firefox extension (by scanning the Firefox profiles' `extensions.json`) and opens a per-profile container tab by default when present, falling back to a regular Firefox tab when not. `--container` still forces a container (skipping detection); the new `--no-container` forces a plain tab.

## [3.7.0] - 2026-06-25

### Added
- Interactive pickers when a profile and/or region is missing: `connect`, `console`, `exec`, and `creds store` now prompt (profile first, then region) instead of guessing or erroring. The region prompt is skipped when the region is already resolvable (flag, env, or the profile's `region=`). Non-interactive runs (pipes/CI) are unchanged.
- `awst config regions` (`add`/`remove`/list) to configure the region list the picker offers; until configured it falls back to a built-in default list. Stored at `~/.config/aws-tools/regions.config` (override with `AWST_REGIONS_FILE`).

## [3.6.0] - 2026-06-25

### Added
- `login`, `console`, and `logout` now accept `--profile`/`-p` as an equivalent to the positional `[profile]` (error if both are given), matching `connect`/`exec` and the AWS CLI's global `--profile`.

## [2.5.0] - 2026-06-17

### Changed
- Split shipped defaults from user customizations for saved commands, run commands, and connection configs.
- Load configuration as base then user layers, while keeping explicit environment-variable overrides exclusive.
- Simplified `awst config` output and added a `--verbose` flag for fuller config inspection.
- Removed default-config copying from install/update flows and fixed installer/updater home-directory resolution.

## [1.6.0] - 2026-03-13

### Added
- **`awst run`** - Run commands/scripts across multiple AWS profiles (integrated from aws-tools)
  - Snippet files with `#ENV`/`#REGION` placeholder substitution
  - Executable scripts (run directly or iterated per-profile)
  - Inline queries via `-q` flag
  - Custom commands directory via `-d` flag or `AWST_CMD_DIR` env var
  - Profile iteration with `source assume` per entry
  - Filter by profile name or `profile:region` pairs
- **`awst creds`** - AWS credential management
  - `store <env>` - Capture credentials via Granted into shell env vars
  - `use` - Re-apply stored AK/SK/ST as AWS\_ env vars
- **Auth layer upgrade** - `aws_auth_assume()` now supports auto-login via `AWS_AUTH_AUTO_LOGIN=1`
- **55 new tests** - Total test count increased from 204 to 259

## [1.5.0] - 2026-01-20

### Added
- **Config-based Port Forwarding**: Profile field is now optional in connection configs
  - When profile is omitted, uses current `AWS_PROFILE` or prompts for selection
  - When profile is specified, validates and uses that profile as before
  - Improves workflow when working within a single AWS profile

## [1.0.0] - 2025-12-20

### Added
- **Core Commands**
  - `awst connect` - Start SSM shell sessions or port forwarding to EC2 instances
  - `awst exec` - Execute commands on multiple instances simultaneously with real-time polling
  - `awst list` - List active SSM sessions on the current host
  - `awst kill` - Terminate active SSM sessions

- **Authentication**
  - Integration with [Granted](https://granted.dev) for AWS SSO authentication
  - Support for AWS profiles and regions via CLI flags
  - Automatic credential validation

- **Interactive Menus**
  - fzf-powered interactive selection with fallback to bash `select`
  - Single and multi-instance selection
  - Cancel support with consistent error code (130)
  - Non-interactive mode support for automation

- **Saved Commands**
  - Command library system with user and system commands
  - Default commands installed from `examples/commands.config`
  - User custom commands in `~/.config/aws-tools/commands.user.config`
  - Environment variable override support (`AWST_SSM_CMD_FILE`)

- **Port Forwarding**
  - Config-based port forwarding with INI-style configuration
  - Interactive config section selection
  - Custom config file support

- **Instance Management**
  - EC2 instance listing with Name tag resolution
  - Instance caching (30s TTL) to reduce API calls
  - Support for both instance IDs and instance names
  - Semicolon-separated multi-instance targeting

- **Command Execution Features**
  - Real-time command status polling
  - Automatic command completion detection
  - Stdout/stderr output display from all instances
  - Failed command output highlighting
  - Configurable polling intervals

- **Installation & Updates**
  - One-line curl installer
  - Version pinning support (install specific versions)
  - Update script with version comparison
  - Automatic default commands installation

- **Version Management**
  - Semantic versioning (SemVer)
  - `--version` flag to display current version
  - Automated release script with GitHub integration
  - Task commands for releases: `task release`, `task release:patch`, etc.

- **Logging System**
  - Structured logging with multiple levels (DEBUG, INFO, WARN, ERROR)
  - Colored output with timestamps
  - File logging support
  - Environment variable configuration

- **Common Flags**
  - `--dry-run` - Show commands without executing
  - `--profile` - AWS profile selection
  - `--region` - AWS region selection
  - `--yes` - Non-interactive mode
  - `--help` - Command help

- **Testing**
  - 155 comprehensive unit tests using BATS
  - Test guard pattern for stubbing external dependencies
  - Menu system tests with fzf mocking
  - Command execution tests with SSM mocking
  - CI integration with `task ci`

- **Documentation**
  - Comprehensive README with examples
  - RELEASE.md with full release process documentation
  - QUICKSTART-RELEASES.md for quick reference
  - WARP.md for AI assistant guidance
  - Inline help for all commands

### Technical Details
- **Architecture**: Layered architecture with core, AWS, menu, and command layers
- **Dependencies**: bash 4.0+, AWS CLI, Granted (assume), session-manager-plugin
- **Optional**: fzf for enhanced menus, shellcheck for linting
- **Install Location**: `~/.local/share/aws-tools` with symlinks in `~/.local/bin`

### Compatibility
- Tested on Linux (EndeavourOS, Ubuntu, Amazon Linux)
- macOS support expected but not extensively tested
- Requires bash 4.0 or later

[Unreleased]: https://github.com/kedwards/awst/compare/v2.5.0...HEAD
[2.5.0]: https://github.com/kedwards/awst/compare/v2.4.1...v2.5.0
[1.6.0]: https://github.com/kedwards/awst/compare/v1.5.0...v1.6.0
[1.5.0]: https://github.com/kedwards/awst/compare/v1.0.0...v1.5.0
[1.0.0]: https://github.com/kedwards/awst/releases/tag/v1.0.0
