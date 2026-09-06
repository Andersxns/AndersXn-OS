#!/usr/bin/env bash
# AndersXn OS module: Tailscale.
#
# Installs and enables tailscaled but deliberately does NOT run "tailscale up".
# Joining a tailnet needs either an interactive login or an auth key, and baking
# a key into an install image hands anyone who obtains that image access to the
# network. The operator connects the node once, after first boot.
set -euo pipefail
source "${AXOS_MODULE_DIR:-/usr/lib/andersxn/modules}/_lib.sh"

ax_add_repo tailscale \
    "https://pkgs.tailscale.com/stable/debian" \
    "https://pkgs.tailscale.com/stable/debian/bookworm.noarmor.gpg" \
    main

ax_apt tailscale

# Forwarding is already on from /etc/sysctl.d/90-andersxn.conf; this makes the
# subnet-router and exit-node paths work without a second reboot.
install -Dm0644 /dev/stdin /etc/sysctl.d/95-tailscale.conf <<'SYSCTL'
# Required for Tailscale subnet routing and exit-node operation.
net.ipv4.ip_forward = 1
net.ipv6.conf.all.forwarding = 1
SYSCTL

ax_enable tailscaled.service

install -Dm0644 /dev/stdin /etc/andersxn/provision.d/tailscale.README <<'README'
Tailscale is installed but this node is NOT connected to a tailnet.

Connect it:

    sudo tailscale up

As a subnet router (advertise the LAN behind this node):

    sudo tailscale up --advertise-routes=192.168.1.0/24

As an exit node:

    sudo tailscale up --advertise-exit-node

No auth key was baked into this image on purpose: an image carrying one grants
tailnet access to anyone who gets hold of it.
README

ax_ok "Tailscale installed - run 'sudo tailscale up' to connect this node"
