#!/usr/bin/env bash
# AndersXn OS module: Portainer CE.
#
# Deployed as a compose stack rather than a bare "docker run" so the operator
# can see, edit and version it alongside everything else under /srv/andersxn.
set -euo pipefail
source "${AXOS_MODULE_DIR:-/usr/lib/andersxn/modules}/_lib.sh"

command -v docker >/dev/null 2>&1 || ax_die "Portainer needs Docker; module 'docker' did not run"

dir="$(ax_compose_dir portainer)"

install -Dm0644 /dev/stdin "$dir/compose.yaml" <<'COMPOSE'
# Portainer CE - Docker management UI.
# https://<host>:9443
services:
  portainer:
    image: portainer/portainer-ce:lts
    container_name: portainer
    restart: unless-stopped
    ports:
      - "9443:9443"
    volumes:
      # Full control of the daemon: this container is as privileged as root.
      - /var/run/docker.sock:/var/run/docker.sock
      - /srv/andersxn/data/portainer:/data
    security_opt:
      - no-new-privileges:true
COMPOSE

# Not started here: the installer runs inside a chroot where the Docker daemon
# is not running. A first-boot unit brings it up on the real system instead.
install -Dm0644 /dev/stdin /etc/systemd/system/axos-portainer.service <<'UNIT'
[Unit]
Description=Bring up the AndersXn Portainer stack
Requires=docker.service
After=docker.service network-online.target
ConditionPathExists=/srv/andersxn/compose/portainer/compose.yaml

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/srv/andersxn/compose/portainer
ExecStart=/usr/bin/docker compose up -d
ExecStop=/usr/bin/docker compose down

[Install]
WantedBy=multi-user.target
UNIT

ax_enable axos-portainer.service
ax_warn "Portainer: set the admin password within 5 minutes of first boot, or restart the container"
ax_ok "Portainer stack staged at $dir"
