# aws-tools (Go port)

CLI for AWS shell + session work. A Go rewrite of the original Bash
toolkit (see branch `main`); the port lives on branch `go-port` while
commands are migrated one vertical slice at a time.

**Status:** slices 1–6 — `awst creds` + `awst login` + `awst connect`
+ `awst list`/`kill` + `awst exec` + `awst run`. Other commands
(`config`, `update`) still live in the bash toolkit on `main`.

## Why a Go port

The bash version depended on shell sourcing for AWS credentials
(`source assume <profile>`, `eval "$(awst creds …)"`). That contract
doesn't cross container boundaries cleanly: `assume` must run on the
host, and the container only sees whatever env vars the host wrapper
forwards. A single Go binary using the AWS SDK Go v2 default credential
chain reads the SSO cache and shared config files directly — no
sourcing, no `assume` shell-out.

## Install

```sh
go install github.com/kedwards/aws-tools@latest
```

Or build from source:

```sh
git clone -b go-port https://github.com/kedwards/aws-tools.git
cd aws-tools
task build              # → dist/awst
```

Requires Go 1.26+.

## Usage

### `awst creds`

Manage AWS credentials per profile. The store / use commands print
shell `export` statements designed for `eval`:

```sh
# Resolve credentials for a profile via the SDK chain and persist them
eval "$(awst creds store dev)"

# Re-export previously stored credentials into a new shell
eval "$(awst creds use dev)"

# List stored profiles + their age
awst creds list

# Remove stored credentials
awst creds clear dev      # one profile
awst creds clear          # all profiles
```

Stored credentials live in `$AWST_CREDS_DIR` (default
`~/.local/share/aws-tools/creds`), one `<profile>.env` per profile,
mode `0600`.

#### Authentication

`awst creds store` uses the [AWS SDK Go v2 default credential
chain](https://aws.github.io/aws-sdk-go-v2/docs/configuring-sdk/#specifying-credentials).
Whatever the SDK resolves for the named profile — SSO cache, instance
role, env vars, static creds — gets persisted. To prime an SSO session
first:

```sh
awst login dev                       # built-in; equivalent to `aws sso login --profile dev`
eval "$(awst creds store dev)"
```

There is no dependency on
[Granted](https://granted.dev) (`assume`). If you already use it on the
host, that's fine — the SDK reads the same SSO cache files Granted
writes to.

### `awst login`

Runs the IAM Identity Center device-authorization flow for the profile's
`sso_session` and caches the resulting token at the SDK-standard path
(`~/.aws/sso/cache/<sha1(session)>.json`). Once the token is cached, any
profile referencing the same `sso_session` can resolve credentials via
the default credential chain — including `awst creds store`.

```sh
awst login dev                # opens browser by default
awst login dev --no-browser   # print the URL only (headless / containers)
```

Only the `sso_session` config form is supported:

```ini
[profile dev]
sso_session = my-sso
sso_account_id = 123456789012
sso_role_name  = Developer
region         = us-east-1

[sso-session my-sso]
sso_start_url = https://my-org.awsapps.com/start
sso_region    = us-east-1
```

Legacy SSO profiles (`sso_start_url` on the profile itself, no
`sso_session`) are rejected — migrate them to the `sso_session` form.

### `awst connect`

Open an SSM shell session on an SSM-managed EC2 instance.

```sh
awst connect i-0123abc          # by instance ID
awst connect web-prod           # case-insensitive substring on Name tag
awst connect                    # list available instances
awst connect -p dev -r us-east-2 web
```

Resolution:
- If the arg starts with `i-`, it's treated as an exact instance ID.
- Otherwise it's matched as a case-insensitive substring on the EC2
  `Name` tag.
- If the arg matches nothing, matches multiple instances, or no arg is
  given, the matching/full list is printed and the command exits
  non-zero. Pipe to `fzf` / `grep` to disambiguate, or pass an `i-…` id.

Requires
[session-manager-plugin](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html)
on `PATH` (override with `AWST_SSM_PLUGIN`). The plugin handles the
WebSocket session itself; awst just calls `ssm:StartSession` and execs
the plugin with the response JSON — the same wiring the AWS CLI uses
internally.

Out of scope for this slice (still in the bash `awst connect` on
`main`):
- `--config` port-forwarding via `connections.conf`
- `--codebuild` debug-session attachment
- Interactive TUI picker (use `fzf`-style external piping for now)

### `awst list` and `awst kill`

Inspect and terminate SSM sessions running on this host. `list` reads
`/proc` for `session-manager-plugin` processes and pulls
region / profile / target from their argv — no AWS calls needed.

```sh
awst list                       # show active sessions
awst kill 12345                 # terminate one session by PID
awst kill 12345 67890           # terminate several
awst kill --all                 # terminate every active SSM session
```

Each kill does `SIGTERM`, waits 250ms, then escalates to `SIGKILL` if
the process is still alive.

Linux-only for now (reads `/proc`). macOS support lands when someone
needs it.

### `awst exec`

Run a shell command on one or more SSM-managed instances via
`ssm:SendCommand`, polling `GetCommandInvocation` until every target
reaches a terminal status. Per-instance output is printed in input
order; exit is non-zero if any target failed.

```sh
awst exec -c 'uptime' -i web-1
awst exec -c 'df -h' -i web,db,i-0123abc
awst exec -c 'systemctl restart nginx' -i web -p prod -r us-east-2
```

`-i` is a comma-separated mix of Name-tag substring patterns and
`i-…` IDs. Each piece is expanded against the live SSM inventory; a
no-match for any piece is a hard error (no silent partial runs).

Output: stdout/stderr come from `GetCommandInvocation`, which caps at
24 KB stdout / 8 KB stderr per instance. Larger output would need S3
configuration, not wired up yet.

The command runs under `AWS-RunShellScript` (default `/bin/sh`).
Include your own shebang or wrap with `bash -c '...'` if you need bash
features. PowerShell targets aren't supported yet.

### `awst run`

Run a saved snippet, an executable script, or an inline command across
one or more AWS profiles. For each profile, awst resolves credentials
via the SDK chain, exports `AWS_PROFILE` / `AWS_REGION` /
`AWS_ACCESS_KEY_ID` / etc. into the child env, and execs the command.
Per-profile auth failures warn and skip — the rest still run.

```sh
awst run                                     # list available commands
awst run vpc-cidrs                           # snippet across every profile in ~/.aws/config
awst run vpc-cidrs "dev prod:us-west-2"      # filtered to two profiles
awst run -q "aws s3 ls" "dev"                # inline command
awst run -d ./snippets my-snippet "dev"      # exclusive override of commands dir
```

Commands live as files under (in increasing priority):
- `$AWST_RUN_CMD_BASE` (default `~/.config/aws-tools/commands/aws`)
- `$AWST_RUN_CMD_USER` (overrides base on collision)
- `-d <path>` / `$AWST_CMD_DIR` (exclusive — replaces both)

Snippet files (non-executable) have comment + blank lines stripped and
are run via `sh -c`. Placeholders `#ENV` (current profile) and
`#REGION` (current region) are substituted for back-compat with the
bash snippet library; new snippets can use `$AWS_PROFILE` / `$AWS_REGION`
directly since those are exported.

Executable files (`+x`) are exec'd directly:
- **with a filter** → iterated per profile, with AWS env vars set
- **without a filter** → run once, no profile loop (the script handles
  its own iteration)

## Development

TDD discipline: each package has tests in the same directory, written
before the implementation. Run the lot with:

```sh
task test               # go test ./...
task acceptance         # builds dist/awst + runs test/acceptance/creds.sh
task ci                 # both of the above
```

Layout:

```
cmd/                cobra commands (root, creds, login, connect, list,
                    kill, exec, run)
internal/paths/     XDG / AWST_CREDS_DIR + SSO cache dir resolution
internal/creds/     store (file I/O), exporter (eval output), resolver (SDK)
internal/sso/       config (sso_session lookup), cache (token write),
                    login (device-flow orchestration)
internal/connect/   describe (EC2/SSM cross-join + Name resolution),
                    session (StartSession + plugin exec)
internal/sessions/  /proc-scan for active session-manager-plugin
                    processes (powers `awst list` / `awst kill`)
internal/ssmexec/   SendCommand + poll loop + pattern expansion
                    (powers `awst exec`)
internal/runner/    dir layering, snippet load, placeholder substitution,
                    filter parsing (powers `awst run`)
test/acceptance/    no-AWS smoke that pins the eval-able output contract
```

### Adding a command

1. Write the failing test first (`cmd/<name>_test.go` and / or an
   `internal/<topic>` package + its test).
2. Add the smallest implementation that makes it pass.
3. Wire into `cmd/root.go` via a `newXxxCmd(deps)` constructor — keep
   commands rebuildable per-test (no global state).
4. Extend `test/acceptance/<name>.sh` if the command has a stable
   text-format contract bash users depend on.

Dependencies are kept thin on purpose: `cobra`, `aws-sdk-go-v2`,
`testify`. No mock framework, no logging library, no fs abstraction.
Extract a shared package only when a second slice forces it.

## Roadmap

- [x] `awst creds {store,use,list,clear}`
- [x] `awst login` — embedded SSO device flow (replaces `aws sso login`)
- [x] `awst connect` — EC2 + SSM shell session (config/port-forward + codebuild still TODO)
- [x] `awst list` / `kill` — local SSM session inspection (Linux /proc only)
- [x] `awst exec` — SendCommand across one/many instances
- [x] `awst run` — execute snippets across AWS profiles
- [ ] `awst config` — print resolved configuration
- [ ] Distribution: GoReleaser, signed binaries
- [ ] CI workflow (replaces deleted `.github/workflows/`)

## License

Same as the bash branch (see `main`).
