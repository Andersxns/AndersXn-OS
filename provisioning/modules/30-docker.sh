#!/usr/bin/env bash
# AndersXn OS module: Docker Engine + Compose v2.
set -euo pipefail
source "${AXOS_MODULE_DIR:-/usr/lib/andersxn/modules}/_lib.sh"

ax_add_repo docker \
    "https://download.docker.com/linux/debian" \
    "https://download.docker.com/linux/debian/gpg" \
    stable

ax_apt docker-ce docker-ce-cli containerd.io \
       docker-buildx-plugin docker-compose-plugin

# The installer already created @docker as a nodatacow subvolume mounted at
# /var/lib/docker. Pick the storage driver to match: overlay2 on btrfs works and
# is better tested than the native btrfs driver, which upstream has deprecated.
install -Dm0644 /dev/stdin /etc/docker/daemon.json <<'DAEMON'
{
  "storage-driver": "overlay2",
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "32m",
    "max-file": "3"
  },
  "default-address-pools": [
    { "base": "172.20.0.0/16", "size": 24 }
  ],
  "live-restore": true,
  "features": { "buildkit": true }
}
DAEMON

# live-restore and the default bridge do not coexist with an nftables ruleset
# that drops forwarding; the base ruleset accepts established flows and leaves
# the container chains to Docker.
ax_enable docker.service containerd.service
ax_add_user_to_groups docker

mkdir -p /srv/andersxn/compose /srv/andersxn/data
install -Dm0644 /dev/stdin /srv/andersxn/compose/README <<'README'
AndersXn OS - compose stacks.

One directory per stack, each with its own compose.yaml. Bind-mount persistent
data from /srv/andersxn/data/<stack> so it stays on the @srv subvolume and is
not swept up in a root snapshot rollback.

    cd /srv/andersxn/compose/<stack> && docker compose up -d
README

ax_ok "Docker installed - log out and back in for group membership to apply"
