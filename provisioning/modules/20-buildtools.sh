#!/usr/bin/env bash
# AndersXn OS module: build toolchain.
set -euo pipefail
source "${AXOS_MODULE_DIR:-/usr/lib/andersxn/modules}/_lib.sh"

ax_apt build-essential pkg-config cmake \
       python3-dev python3-venv python3-pip \
       golang-go \
       libssl-dev zlib1g-dev

# Go's default GOPATH lands in the operator's home; make the module cache a
# predictable location so it can be excluded from backups and snapshots.
install -Dm0644 /dev/stdin /etc/profile.d/90-axos-go.sh <<'GOENV'
# AndersXn OS: keep the Go build cache out of the way of backups.
export GOPATH="${GOPATH:-$HOME/.local/share/go}"
export GOMODCACHE="${GOMODCACHE:-$GOPATH/pkg/mod}"
export PATH="$PATH:$GOPATH/bin"
GOENV

ax_ok "build toolchain installed ($(go version 2>/dev/null || echo 'go present'))"
