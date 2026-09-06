#!/usr/bin/env bash
#
# AndersXn OS - post-install provisioning entry point.
#
# Installed to /usr/lib/andersxn/postinstall.sh and executed INSIDE the target
# chroot by AX-Installer (internal/install/provision.go). Also runnable on a
# live system through ax-provision(8) to add a stack after the fact.
#
# Contract:
#   AXOS_MODULES         space-separated module IDs, already dependency-resolved
#   AXOS_USER            the operator account, added to service groups
#   AXOS_NONINTERACTIVE  set to 1 by the installer
#
# Each module is a script in /usr/lib/andersxn/modules named NN-<id>.sh. The
# numeric prefix fixes execution order (Docker must exist before Portainer);
# the id after it is what the catalog and the selector refer to.

set -euo pipefail

AXOS_LIB="${AXOS_LIB:-/usr/lib/andersxn}"
AXOS_MODULE_DIR="${AXOS_MODULE_DIR:-$AXOS_LIB/modules}"
AXOS_LOG="${AXOS_LOG:-/var/log/andersxn-provision.log}"
AXOS_STATE_DIR="${AXOS_STATE_DIR:-/etc/andersxn/provision.d}"

export DEBIAN_FRONTEND=noninteractive
export DEBCONF_NONINTERACTIVE_SEEN=true

# shellcheck source=modules/_lib.sh
source "$AXOS_MODULE_DIR/_lib.sh"

mkdir -p "$AXOS_STATE_DIR" "$(dirname "$AXOS_LOG")"

modules="${AXOS_MODULES:-}"
if [[ -z "$modules" && -r "$AXOS_STATE_DIR/selected" ]]; then
    modules="$(tr '\n' ' ' < "$AXOS_STATE_DIR/selected")"
fi

if [[ -z "${modules// /}" ]]; then
    ax_info "no modules selected - nothing to provision"
    exit 0
fi

ax_info "provisioning: $modules"

# --- refresh apt once, not once per module ---------------------------------
ax_step "refreshing package lists"
apt-get update -qq || ax_die "apt-get update failed - check network and DNS"

failed=()

for id in $modules; do
    # Match NN-<id>.sh so ordering lives in the filename and the catalog only
    # ever deals in bare IDs.
    script="$(find "$AXOS_MODULE_DIR" -maxdepth 1 -name "[0-9][0-9]-${id}.sh" -print -quit)"

    if [[ -z "$script" ]]; then
        ax_warn "no module script for '$id' - skipping"
        failed+=("$id")
        continue
    fi

    if [[ -e "$AXOS_STATE_DIR/$id.done" ]]; then
        ax_info "$id already provisioned - skipping"
        continue
    fi

    ax_step "module: $id"
    # A failing optional module must not abort the whole install: the base
    # system is already on disk and bootable by this point, and losing Jellyfin
    # is not a reason to leave the operator without a machine.
    if bash "$script"; then
        date -u +%Y-%m-%dT%H:%M:%SZ > "$AXOS_STATE_DIR/$id.done"
        ax_ok "$id provisioned"
    else
        ax_warn "module '$id' failed - continuing with the rest"
        failed+=("$id")
    fi
done

# --- firewall ---------------------------------------------------------------
if [[ -d /etc/nftables.d ]] && command -v nft >/dev/null 2>&1; then
    ax_step "reloading firewall rules"
    # Include generated per-service rules from the base ruleset.
    if ! grep -q 'include "/etc/nftables.d/\*.nft"' /etc/nftables.conf 2>/dev/null; then
        printf '\ninclude "/etc/nftables.d/*.nft"\n' >> /etc/nftables.conf
    fi
    nft -c -f /etc/nftables.conf 2>/dev/null ||
        ax_warn "nftables ruleset did not validate; leaving the base rules in place"
fi

apt-get clean

if (( ${#failed[@]} )); then
    ax_warn "finished with failures: ${failed[*]}"
    ax_warn "re-run a single module with: ax-provision --only <id>"
    exit 0
fi

ax_ok "provisioning complete"
