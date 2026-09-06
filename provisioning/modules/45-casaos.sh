#!/usr/bin/env bash
# AndersXn OS module: CasaOS.
#
# CasaOS is installed on FIRST BOOT, not here. Its installer inspects the
# running system - systemd units, a live Docker daemon, network interfaces -
# none of which exist inside the installer chroot. Running it here produces a
# half-configured install that looks fine and then does not start.
#
# So this module does two things: cache the installer script now, while the
# chroot definitely has network, and enable a one-shot unit that runs it once
# the real system is up with Docker running.
set -euo pipefail
source "${AXOS_MODULE_DIR:-/usr/lib/andersxn/modules}/_lib.sh"

command -v docker >/dev/null 2>&1 || ax_die "CasaOS needs Docker; module 'docker' did not run"

installer=/var/cache/andersxn/casaos-install.sh
mkdir -p "$(dirname "$installer")"

ax_step "caching the CasaOS installer"
if curl -fsSL --retry 3 --retry-delay 2 https://get.casaos.io -o "$installer"; then
    # Cheap sanity check: refuse to keep something that is not the script we
    # expect. It runs as root on first boot, so this is worth the two lines.
    if grep -qi 'casaos' "$installer"; then
        chmod 0755 "$installer"
        ax_ok "installer cached ($(wc -c < "$installer") bytes)"
    else
        rm -f "$installer"
        ax_warn "the downloaded CasaOS installer did not look right - discarding it"
    fi
else
    ax_warn "could not download the CasaOS installer; first boot will fetch it instead"
fi

# CasaOS pulls its app images at install time, so the first boot needs network
# regardless of whether the script itself was cached.
install -Dm0755 /dev/stdin /usr/lib/andersxn/casaos-firstboot <<'FIRSTBOOT'
#!/bin/sh
# Install CasaOS on first boot, once Docker is actually running.
set -eu

installer=/var/cache/andersxn/casaos-install.sh
log() { echo "axos-casaos: $*"; }

# Docker may still be settling; CasaOS aborts if it cannot talk to the daemon.
i=0
while [ "$i" -lt 60 ]; do
    docker info >/dev/null 2>&1 && break
    i=$((i + 1))
    sleep 2
done
if ! docker info >/dev/null 2>&1; then
    log "Docker did not become ready; leaving CasaOS uninstalled"
    exit 1
fi

if [ ! -x "$installer" ]; then
    log "no cached installer, fetching it now"
    curl -fsSL --retry 3 https://get.casaos.io -o "$installer" || {
        log "download failed - check networking, then: sudo ax-provision --only casaos"
        exit 1
    }
    chmod 0755 "$installer"
fi

log "running the CasaOS installer"
# The upstream script is interactive by default; -y accepts its prompts.
"$installer" -y
FIRSTBOOT

install -Dm0644 /dev/stdin /etc/systemd/system/axos-casaos-firstboot.service <<'UNIT'
[Unit]
Description=Install CasaOS on first boot
Requires=docker.service
After=docker.service network-online.target
Wants=network-online.target
ConditionPathExists=!/var/lib/andersxn/.casaos-installed
ConditionPathExists=!/usr/bin/casaos

[Service]
Type=oneshot
RemainAfterExit=yes
TimeoutStartSec=1800
ExecStart=/usr/lib/andersxn/casaos-firstboot
ExecStartPost=/bin/sh -c 'mkdir -p /var/lib/andersxn && touch /var/lib/andersxn/.casaos-installed'

[Install]
WantedBy=multi-user.target
UNIT

ax_enable axos-casaos-firstboot.service
ax_add_user_to_groups docker

install -Dm0644 /dev/stdin /etc/andersxn/provision.d/casaos.README <<'README'
CasaOS is installed on the FIRST BOOT of this system, not during installation.

Its installer needs a running systemd and a live Docker daemon, so it cannot
run inside the installer chroot. A one-shot unit does it instead:

    systemctl status axos-casaos-firstboot

That first boot needs working networking - CasaOS pulls its container images
as it installs. If the machine is offline, the unit fails harmlessly and you
can re-run it later with:

    sudo ax-provision --only casaos

Once up, the dashboard is on http://<host>/ (port 80). That will collide with
anything else serving port 80 on this node.
README

ax_warn "CasaOS installs on first boot and needs network then; it serves on port 80"
ax_ok "CasaOS staged"
