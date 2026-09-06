#!/usr/bin/env bash
#
# Stage 20 - turn the bootstrapped tree into a usable AndersXn system.
#
# Installs the system package set, configures apt/locale/time, enables the
# services AndersXn expects, and sets the security defaults. Branding is
# deliberately NOT done here - that is stage 30, so it can be re-run alone
# while iterating on the visual identity.

set -euo pipefail
AXOS_ROOT="${AXOS_ROOT:-$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)}"
# shellcheck source=../lib/common.sh
source "$AXOS_ROOT/build/lib/common.sh"
# shellcheck source=../config/build.conf
source "$AXOS_ROOT/build/config/build.conf"

require_root
[[ -d "$AXOS_ROOTFS" ]] || die "no rootfs at $AXOS_ROOTFS - run stage 10 first"
trap 'axos_umount_all' EXIT INT TERM

axos_mount_pseudo "$AXOS_ROOTFS"

# ---------------------------------------------------------------------------
# apt sources (deb822 format - Debian trixie onward)
# ---------------------------------------------------------------------------
log "writing apt sources"
install -dm0755 "$AXOS_ROOTFS/etc/apt/sources.list.d"
rm -f "$AXOS_ROOTFS/etc/apt/sources.list"

cat > "$AXOS_ROOTFS/etc/apt/sources.list.d/debian.sources" <<SOURCES
Types: deb
URIs: $AXOS_MIRROR
Suites: $AXOS_SUITE ${AXOS_SUITE}-updates
Components: $AXOS_COMPONENTS
Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg

Types: deb
URIs: $AXOS_SECURITY_MIRROR
Suites: ${AXOS_SUITE}-security
Components: $AXOS_COMPONENTS
Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg
SOURCES

# ---------------------------------------------------------------------------
# System package set
# ---------------------------------------------------------------------------
# The package list carries a literal ARCH token so one list serves both targets.
sys_pkgs="${AXOS_PKGS_SYSTEM//linux-image-ARCH/linux-image-$AXOS_ARCH}"

log "installing system package set"
axos_chroot_sh "$AXOS_ROOTFS" <<CHROOT
apt-get update -qq
apt-get install -y --no-install-recommends $sys_pkgs
apt-get clean
CHROOT
ok "system packages installed"

# Live/installer-only packages. live-boot MUST be present before stage 40
# rebuilds the initramfs, otherwise the initramfs has no hooks to locate and
# mount the squashfs on the live medium — and the ISO kernel-panics.
# AX-Installer removes the live-specific pieces when installing to disk.
log "installing live package set"
axos_chroot_sh "$AXOS_ROOTFS" <<CHROOT
apt-get install -y --no-install-recommends $AXOS_PKGS_LIVE
apt-get clean
CHROOT
ok "live packages installed"

# ---------------------------------------------------------------------------
# Locale, timezone, keymap
# ---------------------------------------------------------------------------
log "configuring locale and time"
echo "$AXOS_LOCALE UTF-8" > "$AXOS_ROOTFS/etc/locale.gen"
echo "LANG=$AXOS_LOCALE" > "$AXOS_ROOTFS/etc/locale.conf"
echo "KEYMAP=$AXOS_KEYMAP" > "$AXOS_ROOTFS/etc/vconsole.conf"
echo "$AXOS_HOSTNAME" > "$AXOS_ROOTFS/etc/hostname"

cat > "$AXOS_ROOTFS/etc/hosts" <<HOSTS
127.0.0.1   localhost
127.0.1.1   $AXOS_HOSTNAME
::1         localhost ip6-localhost ip6-loopback
ff02::1     ip6-allnodes
ff02::2     ip6-allrouters
HOSTS

axos_chroot_sh "$AXOS_ROOTFS" <<CHROOT
locale-gen >/dev/null
ln -sf /usr/share/zoneinfo/$AXOS_TIMEZONE /etc/localtime
CHROOT

# ---------------------------------------------------------------------------
# Services
# ---------------------------------------------------------------------------
log "enabling base services"
axos_chroot_sh "$AXOS_ROOTFS" <<'CHROOT'
systemctl enable systemd-resolved.service   >/dev/null 2>&1 || true
systemctl enable systemd-timesyncd.service  >/dev/null 2>&1 || true
systemctl enable ssh.service                >/dev/null 2>&1 || true
systemctl enable nftables.service           >/dev/null 2>&1 || true

# systemd-resolved owns /etc/resolv.conf on an installed system.
ln -sf ../run/systemd/resolve/stub-resolv.conf /etc/resolv.conf

# A homelab box is far more useful reachable than silent: predictable interface
# names on, DHCP on the first wired link by default. The installer can replace
# this with a static profile.
mkdir -p /etc/systemd/network
cat > /etc/systemd/network/20-wired.network <<'NET'
[Match]
Type=ether

[Network]
DHCP=yes
IPv6AcceptRA=yes

[DHCPv4]
UseDomains=yes
NET
systemctl enable systemd-networkd.service >/dev/null 2>&1 || true
CHROOT

# ---------------------------------------------------------------------------
# Security defaults
# ---------------------------------------------------------------------------
log "applying security defaults"
install -dm0755 "$AXOS_ROOTFS/etc/ssh/sshd_config.d"
cat > "$AXOS_ROOTFS/etc/ssh/sshd_config.d/10-andersxn.conf" <<'SSHD'
# AndersXn OS SSH defaults.
# Password login is left enabled so a freshly installed headless node is not
# locked out; AX-Installer disables it automatically when the operator supplies
# an SSH public key during setup.
PermitRootLogin no
X11Forwarding no
MaxAuthTries 4
LoginGraceTime 30
ClientAliveInterval 300
ClientAliveCountMax 2
SSHD

# Default deny-inbound ruleset. Loopback, established flows, ICMP and SSH only;
# the provisioning modules append their own ports to the axos-services chain.
install -Dm0644 /dev/stdin "$AXOS_ROOTFS/etc/nftables.conf" <<'NFT'
#!/usr/sbin/nft -f
# AndersXn OS baseline firewall.
flush ruleset

table inet axos {
    # Provisioning modules add "tcp dport N accept" rules to this chain.
    chain services {
    }

    chain input {
        type filter hook input priority filter; policy drop;

        iif lo accept
        ct state established,related accept
        ct state invalid drop

        meta l4proto icmp accept
        meta l4proto ipv6-icmp accept

        tcp dport 22 accept comment "ssh"
        jump services
    }

    chain forward {
        type filter hook forward priority filter; policy drop;
        # Container runtimes install their own forward rules in the ip/ip6
        # families; this policy only covers what they do not claim.
        ct state established,related accept
    }

    chain output {
        type filter hook output priority filter; policy accept;
    }
}
NFT
chmod 0755 "$AXOS_ROOTFS/etc/nftables.conf"

# sudo for the wheel-equivalent group; AX-Installer adds the operator to it.
install -Dm0440 /dev/stdin "$AXOS_ROOTFS/etc/sudoers.d/10-andersxn" <<'SUDO'
%sudo ALL=(ALL:ALL) ALL
Defaults lecture=never
Defaults passwd_timeout=0
SUDO

# Root is locked; the installer creates the operator account.
axos_chroot "$AXOS_ROOTFS" passwd -l root >/dev/null 2>&1 || true

# ---------------------------------------------------------------------------
# Journald and sysctl tuning for a small server
# ---------------------------------------------------------------------------
install -Dm0644 /dev/stdin "$AXOS_ROOTFS/etc/systemd/journald.conf.d/10-andersxn.conf" <<'JRN'
[Journal]
Storage=persistent
SystemMaxUse=512M
SystemMaxFileSize=64M
MaxRetentionSec=1month
JRN

install -Dm0644 /dev/stdin "$AXOS_ROOTFS/etc/sysctl.d/90-andersxn.conf" <<'SYSCTL'
# AndersXn OS kernel tuning - conservative defaults for container hosts.
vm.swappiness = 10
vm.max_map_count = 262144
fs.inotify.max_user_watches = 524288
fs.inotify.max_user_instances = 8192
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr
net.ipv4.ip_forward = 1
net.ipv6.conf.all.forwarding = 1
kernel.dmesg_restrict = 1
SYSCTL

ok "stage 20 complete"
