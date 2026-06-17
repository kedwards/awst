#!/usr/bin/env bash
# Runtime entrypoint. The host wrapper (containers/awst-host) invokes the
# container with `--user $(id -u):$(id -g)` and `-e HOME=/home/awst`, so the
# container process already runs as the host user — no privilege drop here.
set -eu

export HOME="${HOME:-/home/awst}"
export PATH="/opt/aws-tools/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin"

# Marker so the toolkit can tailor error messages (e.g. point users back to the
# host shell for Granted/SSO auth instead of telling them to install Granted).
export AWST_IN_CONTAINER=1

# Ensure the container points at the baked-in install dir for base commands
export AWST_INSTALL_DIR="/opt/aws-tools"

# First-run: copy shipped examples into the user's bind-mounted config so
# awst run / awst exec have default commands available on a fresh install.
if [[ -d /opt/aws-tools/examples/commands ]]; then
  mkdir -p "${HOME}/.config/aws-tools/commands/aws" \
           "${HOME}/.config/aws-tools/commands/ssm"
  rsync -a --ignore-existing /opt/aws-tools/examples/commands/aws/ \
    "${HOME}/.config/aws-tools/commands/aws/"
  rsync -a --ignore-existing /opt/aws-tools/examples/commands/ssm/ \
    "${HOME}/.config/aws-tools/commands/ssm/"
fi
if [[ -f /opt/aws-tools/examples/connections.config ]]; then
  mkdir -p "${HOME}/.config/aws-tools"
  [[ -f "${HOME}/.config/aws-tools/connections.user.config" ]] || \
    cp /opt/aws-tools/examples/connections.config \
       "${HOME}/.config/aws-tools/connections.user.config"
fi

# Tools that probe /etc/passwd (less, fzf interactive line editing) get cranky
# when the running UID has no entry. Silence that.
if [ -w /etc/passwd ] && ! getent passwd "$(id -u)" >/dev/null 2>&1; then
  echo "awst:x:$(id -u):$(id -g):aws-tools runtime:${HOME}:/bin/bash" >> /etc/passwd
fi

exec /opt/aws-tools/bin/awst "$@"
