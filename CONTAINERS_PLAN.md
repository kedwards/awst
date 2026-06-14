# Containerization Plan: `aws-tools` (`awst`)

Status: **Plan only** — no implementation in this commit. Branch: `containers`.

## Decisions

| Question | Decision |
|---|---|
| Scope | **Containers as the default.** Host-native install kept as a documented fallback. |
| Auth (Granted) | **Mount host creds only.** Container reads `~/.aws` + `~/.granted` from host. No in-container `assume`. |
| Base image | **`debian:stable-slim`** for both dev and runtime. |
| Delivery | Plan only on `containers` branch. |

## Goals

1. Devs can run `task test`, `task lint`, `task ci` without installing `bats`, `shellcheck`, or `task` on the host.
2. End users can run `awst <subcommand>` via a thin wrapper that execs a container — no host Bash/AWS-CLI/Granted requirement *for the awst tool itself* (Granted/assume is still expected on the host for SSO).
3. Image works under both Docker and Podman without source changes.
4. Interactive flows (`fzf` menus, `ssm start-session`) keep working.
5. No regressions for the existing host-native install — it still works, it's just no longer the default.

## Non-Goals

- In-container SSO browser auth (deferred; explicitly out of scope this iteration).
- Multi-arch publishing pipeline (single linux/amd64 to start; arm64 can come later).
- Replacing the BATS test framework or Task runner.

---

## File Additions

```
containers/
├── Dockerfile.dev              # dev/CI image (bats, shellcheck, task, shfmt + runtime deps)
├── Dockerfile.runtime          # slim runtime image (aws-cli, fzf, session-manager-plugin, bash)
├── compose.yaml                # convenience for `dev` and `awst` services
├── entrypoint.sh               # runtime entrypoint: drops privs to UID/GID from env, execs awst
└── README.md                   # how to build/run; engine-detection notes

bin/
└── awst-host                   # NEW: host-side wrapper that detects docker/podman and execs container

scripts/
└── install-container.sh        # NEW: pulls/builds image, drops `bin/awst-host` symlinked as `awst`

Taskfile.yml                    # add docker:* tasks (see below)
install.sh / update.sh          # rewrite to container-first; keep host-install branch behind a flag
.dockerignore                   # NEW
```

No changes required to `lib/`, `bin/awst`, or `examples/` — the existing entrypoint stays intact and is what runs *inside* the runtime container.

---

## Image Designs

### `Dockerfile.runtime` (sketch)

```dockerfile
FROM debian:stable-slim AS runtime

ARG AWS_CLI_VERSION=2.17.0
ARG TARGETARCH=amd64

RUN apt-get update && apt-get install -y --no-install-recommends \
      bash ca-certificates curl unzip jq rsync fzf less \
    && rm -rf /var/lib/apt/lists/*

# AWS CLI v2
RUN curl -sSL "https://awscli.amazonaws.com/awscli-exe-linux-x86_64-${AWS_CLI_VERSION}.zip" -o /tmp/awscli.zip \
    && unzip -q /tmp/awscli.zip -d /tmp && /tmp/aws/install \
    && rm -rf /tmp/aws /tmp/awscli.zip

# SSM session-manager-plugin
RUN curl -sSL "https://s3.amazonaws.com/session-manager-downloads/plugin/latest/ubuntu_64bit/session-manager-plugin.deb" \
      -o /tmp/smp.deb && dpkg -i /tmp/smp.deb && rm /tmp/smp.deb

# Copy tool
COPY . /opt/aws-tools
RUN ln -s /opt/aws-tools/bin/awst /usr/local/bin/awst

# Non-root user; entrypoint will re-exec under host UID/GID
RUN useradd -m -u 1000 -s /bin/bash awst
USER awst
WORKDIR /home/awst

ENV PATH=/usr/local/bin:/opt/aws-tools/bin:$PATH
ENTRYPOINT ["/opt/aws-tools/containers/entrypoint.sh"]
CMD ["awst", "--help"]
```

Notes:
- Granted (`assume`) is **not** installed in the runtime image (per "mount host creds only").
- `session-manager-plugin` package is the Ubuntu .deb — works on Debian.
- `entrypoint.sh` reads `HOST_UID`/`HOST_GID` and `exec`s as that user so files written to mounted dirs stay owned correctly. Skip when running as root for `task ci` use case.

### `Dockerfile.dev` (sketch)

```dockerfile
FROM debian:stable-slim AS dev

RUN apt-get update && apt-get install -y --no-install-recommends \
      bash ca-certificates curl unzip jq rsync fzf less git make \
      bats shellcheck shfmt \
    && rm -rf /var/lib/apt/lists/*

# go-task (no Debian package; install binary)
ARG TASK_VERSION=3.39.2
RUN curl -sSL "https://github.com/go-task/task/releases/download/v${TASK_VERSION}/task_linux_amd64.tar.gz" \
      | tar -xz -C /usr/local/bin task

# AWS CLI v2 and session-manager-plugin (same as runtime; lets devs run awst in dev container too)
# ... copy/share via a base stage if image size matters ...

WORKDIR /work
ENTRYPOINT ["/bin/bash"]
```

Optionally extract a `base` stage with the OS deps and have both `dev` and `runtime` `FROM base` to avoid duplication.

---

## `compose.yaml` (sketch)

```yaml
services:
  awst:
    build:
      context: ..
      dockerfile: containers/Dockerfile.runtime
    image: aws-tools:runtime
    stdin_open: true
    tty: true
    environment:
      - AWS_PROFILE
      - AWS_REGION
      - AWS_DEFAULT_REGION
      - AWS_LOG_LEVEL
      - AWS_LOG_COLOR
      - AWS_LOG_TIMESTAMP
      - HOST_UID=${UID:-1000}
      - HOST_GID=${GID:-1000}
    volumes:
      - ${HOME}/.aws:/home/awst/.aws
      - ${HOME}/.granted:/home/awst/.granted
      - ${HOME}/.config/aws-tools:/home/awst/.config/aws-tools
      - ${HOME}/.cache/aws-tools:/home/awst/.cache/aws-tools

  dev:
    build:
      context: ..
      dockerfile: containers/Dockerfile.dev
    image: aws-tools:dev
    stdin_open: true
    tty: true
    working_dir: /work
    volumes:
      - ..:/work
      - ${HOME}/.aws:/root/.aws
      - ${HOME}/.granted:/root/.granted
```

---

## Host Wrapper: `bin/awst-host`

Installed as `~/.local/bin/awst` (symlink) after `install.sh` runs. Replaces the current direct symlink to `bin/awst`.

Behavior:
1. Detect engine: prefer `$AWST_CONTAINER_ENGINE`, else `docker`, else `podman`. Fail loudly if neither.
2. Build run args:
   - `-it` if stdout is a TTY, else `-i` only (CI-friendly).
   - Bind mounts: `~/.aws`, `~/.granted`, `~/.config/aws-tools`, `~/.cache/aws-tools`.
   - Pass through env: `AWS_PROFILE`, `AWS_REGION`, `AWS_DEFAULT_REGION`, `AWS_LOG_*`, `MENU_*`, `AWST_*`.
   - `HOST_UID=$(id -u)`, `HOST_GID=$(id -g)`.
   - Image: `${AWST_IMAGE:-ghcr.io/kedwards/aws-tools:latest}`.
3. Engine quirks:
   - Podman + SELinux: append `:z` to bind mounts. Detect via `command -v selinuxenabled && selinuxenabled`.
   - Rootless podman: `--userns=keep-id` instead of HOST_UID juggling.
4. `exec` the engine — wrapper itself does no work post-fork, so signal forwarding (Ctrl-C during SSM session) works naturally.

Special-case forwarded subcommands that need extra mounts (e.g., `awst connect` opening local ports): document `--network=host` as the default, with a flag to switch to `-p` published ports.

---

## Taskfile Additions

```yaml
docker:build:
  desc: Build both dev and runtime images
  cmds:
    - docker build -f containers/Dockerfile.dev      -t aws-tools:dev .
    - docker build -f containers/Dockerfile.runtime  -t aws-tools:runtime .

docker:test:
  desc: Run unit tests inside dev image
  cmds:
    - docker run --rm -v "$PWD":/work aws-tools:dev task test

docker:lint:
  desc: Run shellcheck inside dev image
  cmds:
    - docker run --rm -v "$PWD":/work aws-tools:dev task lint

docker:ci:
  desc: Lint + test inside dev image
  cmds:
    - docker run --rm -v "$PWD":/work aws-tools:dev task ci

docker:shell:
  desc: Interactive shell in dev image
  cmds:
    - docker run --rm -it -v "$PWD":/work aws-tools:dev
```

Existing `task test` / `task lint` / `task ci` keep working — they remain the inner commands the docker tasks call. Users with host-native tooling get the same UX they have today.

---

## `install.sh` / `update.sh` Rewrite

New default flow (container-first):

1. Detect `docker` or `podman`. If neither, print actionable error and exit non-zero.
2. `docker pull ghcr.io/kedwards/aws-tools:${VERSION:-latest}` (with build-from-source fallback if pull fails and the repo dir is local).
3. Write `bin/awst-host` to `~/.local/share/aws-tools/bin/awst-host`.
4. Symlink `~/.local/bin/awst -> ~/.local/share/aws-tools/bin/awst-host`.
5. Deploy `examples/commands/*` to `~/.config/aws-tools/commands/*` (unchanged from today).

Flag `--host` opts into the existing host-native install path. This preserves migration for users who don't want containers.

`update.sh`: same logic; just re-pull the image and re-write the wrapper.

---

## Test Strategy

- **Unit tests** (`task test`): runs inside `aws-tools:dev` against bind-mounted repo. No AWS contact. CI calls `task docker:ci`.
- **Integration tests** (`task test:integration`): kept host-side for now since they expect real AWS creds. Optional follow-up to make them container-runnable.
- **Wrapper smoke test**: a new `test/unit/awst_host.bats` that asserts engine detection, arg construction (mocking `docker`/`podman` binaries on `$PATH`).
- **Engine parity check**: a CI matrix job runs `task docker:ci` once under docker and once under podman.

---

## Migration / Rollout

1. **Phase 1 (this branch)**: add Dockerfile.dev + `task docker:*`. No changes to user install. Devs can opt in to containerized testing immediately.
2. **Phase 2**: add Dockerfile.runtime + `awst-host` wrapper. Document it in README but don't switch the default installer yet. Dogfood.
3. **Phase 3**: cut a new minor release; `install.sh` defaults to container path; `--host` flag preserves the old behavior.
4. **Phase 4**: publish image to GHCR via release workflow; add multi-arch (amd64 + arm64) when arm64 host is available for testing.

Each phase is independently shippable; no flag day.

---

## Risks & Open Questions

1. **Interactive Ctrl-C during SSM sessions** — needs verification that signal forwarding works through `docker exec`/`podman exec` for nested `aws ssm start-session`. Likely fine with `-it`, but worth a manual smoke test before Phase 3.
2. **`fzf` height/colors** — fzf inside a container reads `TERM` from the env; wrapper must pass `TERM`, `COLORTERM`, `LANG` through.
3. **EC2 cache permissions** — host UID mapping must match, else cache files become root-owned. The `entrypoint.sh` UID/GID drop solves this on docker; podman rootless uses `--userns=keep-id`.
4. **Image distribution** — GHCR vs. just-build-locally. Recommend GHCR for end users; falls back to local build during install if pull fails. Need to set up the publish workflow as a separate PR.
5. **`bin/awst` auto-deploy of examples** — currently writes to `~/.config/aws-tools` on first run. Inside the container with `~/.config/aws-tools` mounted, this is fine *as long as* the UID matches host. Validated by the entrypoint UID-drop.
6. **Granted state location** — `~/.granted/` covers most cases; confirm there isn't a per-platform path (e.g., `~/.config/granted/`) the user relies on.
7. **`session-manager-plugin` arm64** — the Amazon-provided .deb has arm64 variants but the URL template differs; multi-arch build needs a conditional `TARGETARCH` step.

---

## Acceptance Criteria (for the eventual implementation PR)

- [ ] `task docker:ci` passes on a clean host with only `docker` installed.
- [ ] `task docker:ci` passes under `podman` with `AWST_CONTAINER_ENGINE=podman`.
- [ ] `awst --version` via container wrapper prints same version as host install.
- [ ] `awst connect` via container wrapper opens an SSM shell session and Ctrl-C cleanly exits.
- [ ] `awst exec` runs against >=2 instances and streams output back through the container.
- [ ] `awst run -q` works with `assume`-cached host credentials.
- [ ] `install.sh` succeeds on a host with no bash dependencies beyond docker.
- [ ] `install.sh --host` reproduces today's behavior exactly.
- [ ] README documents the container install as primary; host install as fallback.
