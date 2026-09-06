#!/usr/bin/env bash
# AndersXn OS module: K3s single-node server.
#
# The upstream installer script is fetched but NOT piped straight into a shell.
# It is downloaded, checked for plausibility and then run from a file, so the
# thing that executes as root is on disk and auditable afterwards.
set -euo pipefail
source "${AXOS_MODULE_DIR:-/usr/lib/andersxn/modules}/_lib.sh"

installer=/var/cache/andersxn/k3s-install.sh
mkdir -p "$(dirname "$installer")"

ax_info "fetching the K3s installer"
curl -fsSL --retry 3 --retry-delay 2 https://get.k3s.io -o "$installer" ||
    ax_die "could not download the K3s installer"

# Cheap sanity check: refuse to run something that is not the script we expect.
grep -q 'k3s' "$installer" || ax_die "the downloaded K3s installer looks wrong; refusing to run it"
chmod 0755 "$installer"

# INSTALL_K3S_SKIP_START matters: inside the installer chroot there is no
# running systemd to start the unit against. It comes up on first boot instead.
export INSTALL_K3S_SKIP_START=true
export INSTALL_K3S_SKIP_SELINUX_RPM=true
export INSTALL_K3S_EXEC="server --write-kubeconfig-mode 0640 --disable-cloud-controller"

ax_step "installing K3s"
"$installer" || ax_die "the K3s installer failed"

# Let the operator use kubectl without sudo, without making the kubeconfig
# world-readable.
if getent group k3s >/dev/null 2>&1 || groupadd --system k3s; then
    ax_add_user_to_groups k3s
fi

install -Dm0644 /dev/stdin /etc/systemd/system/k3s.service.d/10-andersxn.conf <<'UNIT'
[Service]
# K3s writes a lot at startup; on btrfs this avoids a burst of fragmentation.
Environment=K3S_KUBECONFIG_MODE=0640
UNIT

install -Dm0644 /dev/stdin /etc/profile.d/91-axos-k3s.sh <<'KUBE'
# AndersXn OS: point kubectl at the local K3s cluster.
if [ -r /etc/rancher/k3s/k3s.yaml ]; then
    export KUBECONFIG="${KUBECONFIG:-/etc/rancher/k3s/k3s.yaml}"
fi
KUBE

ax_enable k3s.service
ax_ok "K3s installed - it starts on first boot; check with 'kubectl get nodes'"
