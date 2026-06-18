# aws-tools (Go port)

CLI for AWS shell + session work. A Go rewrite of the original Bash
toolkit (see branch `main`); the port lives on branch `go-port` while
commands are migrated one vertical slice at a time.

**Status:** slices 1–2 — `awst creds` + `awst login`. Other commands
(`connect`, `exec`, `run`, `list`, `kill`, `config`, `update`) still
live in the bash toolkit on `main`.

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
cmd/                cobra commands (root, creds, login)
internal/paths/     XDG / AWST_CREDS_DIR + SSO cache dir resolution
internal/creds/     store (file I/O), exporter (eval output), resolver (SDK)
internal/sso/       config (sso_session lookup), cache (token write),
                    login (device-flow orchestration)
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
- [ ] `awst connect` — EC2 + SSM start-session
- [ ] `awst exec` — run command across one/many instances
- [ ] `awst run` — execute snippets across AWS profiles
- [ ] `awst list` / `kill` — local SSM session inspection
- [ ] `awst config` — print resolved configuration
- [ ] Distribution: GoReleaser, signed binaries
- [ ] CI workflow (replaces deleted `.github/workflows/`)

## License

Same as the bash branch (see `main`).
