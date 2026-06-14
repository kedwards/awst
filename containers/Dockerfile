# syntax=docker/dockerfile:1
#
# Multi-stage Dockerfile for aws-tools.
#
# Targets:
#   --target dev      developer/CI image (bats, shellcheck, shfmt, task,
#                     make, git) for local development and CI gating.
#   --target runtime  slim image used by containers/awst-host, mounted
#                     against the host's SSO/Granted caches. Default.
#
# Both targets inherit a shared `base` stage with debian-slim + AWS CLI v2
# + session-manager-plugin so the dependency installs happen exactly once
# (and only drift when intentionally bumped).

ARG TASK_VERSION=3.39.2

# ---------- base ----------------------------------------------------------
FROM debian:stable-slim AS base

# TARGETARCH is auto-populated by BuildKit (single-arch) and buildx
# (multi-arch). Do NOT default it - a default overrides the auto-value
# and silently builds the host arch for every requested target platform.
ARG TARGETARCH

ENV DEBIAN_FRONTEND=noninteractive \
    LANG=C.UTF-8 \
    LC_ALL=C.UTF-8

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      bash \
      ca-certificates \
      curl \
      jq \
      rsync \
      fzf \
 && rm -rf /var/lib/apt/lists/*

# AWS CLI v2 - install unzip only for the duration of this layer so it never
# lands in any committed image (saves ~700 KB across base and downstream stages).
RUN set -eux; \
    case "${TARGETARCH}" in \
      amd64) AWS_ARCH=x86_64 ;; \
      arm64) AWS_ARCH=aarch64 ;; \
      *) echo "unsupported arch: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    apt-get update; \
    apt-get install -y --no-install-recommends unzip; \
    curl -fsSL "https://awscli.amazonaws.com/awscli-exe-linux-${AWS_ARCH}.zip" -o /tmp/awscli.zip; \
    unzip -q /tmp/awscli.zip -d /tmp; \
    /tmp/aws/install; \
    apt-get purge -y --auto-remove unzip; \
    rm -rf /tmp/aws /tmp/awscli.zip /var/lib/apt/lists/*; \
    aws --version

# SSM session-manager-plugin
RUN set -eux; \
    case "${TARGETARCH}" in \
      amd64) SMP_ARCH=ubuntu_64bit ;; \
      arm64) SMP_ARCH=ubuntu_arm64 ;; \
      *) echo "unsupported arch: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL "https://s3.amazonaws.com/session-manager-downloads/plugin/latest/${SMP_ARCH}/session-manager-plugin.deb" \
      -o /tmp/smp.deb; \
    dpkg -i /tmp/smp.deb; \
    rm /tmp/smp.deb

# ---------- dev -----------------------------------------------------------
# Repo is bind-mounted at runtime via `task docker:*`; nothing is COPYed in.
FROM base AS dev

ARG TASK_VERSION
ARG TARGETARCH

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      git \
      less \
      make \
      bats \
      shellcheck \
      shfmt \
 && rm -rf /var/lib/apt/lists/*

# go-task: no Debian package, install pinned release binary
RUN set -eux; \
    case "${TARGETARCH}" in \
      amd64) TASK_ARCH=amd64 ;; \
      arm64) TASK_ARCH=arm64 ;; \
      *) echo "unsupported arch: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL "https://github.com/go-task/task/releases/download/v${TASK_VERSION}/task_linux_${TASK_ARCH}.tar.gz" \
      | tar -xz -C /usr/local/bin task; \
    chmod +x /usr/local/bin/task; \
    task --version

WORKDIR /work
CMD ["bash"]

# ---------- runtime -------------------------------------------------------
# Invoked by containers/awst-host with host ~/.aws / ~/.granted /
# ~/.config/aws-tools / ~/.cache/aws-tools bind-mounted so SSO credentials
# carry into the container. This is the default target.
FROM base AS runtime

# AWS_PAGER="" disables the AWS CLI's default `less`-based pager. `less` is not
# installed in this stage; without this, an interactive `aws ...` invocation
# would try to spawn the missing pager. procps stays - awst list / awst kill
# call `ps eww aux` to find session-manager-plugin processes.
ENV PATH=/opt/aws-tools/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin \
    AWS_PAGER=""

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      procps \
 && rm -rf /var/lib/apt/lists/*

COPY bin/awst                 /opt/aws-tools/bin/awst
COPY lib                      /opt/aws-tools/lib
COPY examples/commands        /opt/aws-tools/examples/commands
COPY VERSION                  /opt/aws-tools/VERSION
COPY containers/entrypoint.sh /usr/local/bin/awst-entrypoint
RUN chmod +x /opt/aws-tools/bin/awst /usr/local/bin/awst-entrypoint \
 && mkdir -p /home/awst /home/awst/.aws /home/awst/.config /home/awst/.cache \
 && chmod 0777 /home/awst /home/awst/.config /home/awst/.cache

ENTRYPOINT ["/usr/local/bin/awst-entrypoint"]
CMD ["--help"]
