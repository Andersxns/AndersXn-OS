#!/usr/bin/env bash
#
# Stage 00 - host dependency and capability check.
#
# Verifies the build host can produce an image for AXOS_ARCH before any
# expensive work starts, and registers the qemu-user-static binfmt handler when
# the target architecture is foreign (e.g. building arm64 on an amd64 host).

set -euo pipefail
AXOS_ROOT="${AXOS_ROOT:-$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)}"
# shellcheck source=../lib/common.sh
source "$AXOS_ROOT/build/lib/common.sh"
# shellcheck source=../config/build.conf
source "$AXOS_ROOT/build/config/build.conf"

require_root

log "checking host tooling"
need_cmd mount umount chroot install sed find sort awk
need_cmd mmdebstrap
need_cmd mksquashfs xorriso
need_cmd sgdisk mkfs.vfat mkfs.btrfs
need_cmd git curl gpg
need_cmd losetup
ok "core tooling present"

# --- Bootloader tooling -----------------------------------------------------
case "$AXOS_BOOTLOADER" in
    limine)
        # Limine is fetched and built in stage 40; it only needs a C toolchain.
        need_cmd cc make
        ;;
    systemd-boot)
        need_cmd bootctl
        ;;
esac

# --- Cross-architecture support --------------------------------------------
if axos_is_cross "$AXOS_ARCH"; then
    warn "cross-building $AXOS_ARCH on $(uname -m) - qemu user emulation required"

    qemu_bin="$(axos_qemu_binary "$AXOS_ARCH")" || qemu_bin=""
    [[ -n "$qemu_bin" ]] ||
        die "no qemu user emulator for $AXOS_ARCH found. Install one with:
    Debian/older Ubuntu : apt-get install qemu-user-static binfmt-support
    Ubuntu 26.04+       : apt-get install qemu-user qemu-user-binfmt"

    # A dynamically linked emulator cannot work inside the chroot, whatever the
    # binfmt flags say - its shared libraries are not in there.
    axos_is_static "$qemu_bin" ||
        die "$qemu_bin is dynamically linked and cannot run inside a chroot.
    Install a static build (qemu-user-static, or qemu-user on Ubuntu 26.04+)."

    # binfmt_misc must be able to hand foreign binaries to qemu inside a chroot.
    # The F (fix-binary) flag matters: without it the handler resolves the
    # interpreter path relative to the chroot, which will not exist.
    if [[ ! -d /proc/sys/fs/binfmt_misc ]]; then
        modprobe binfmt_misc 2>/dev/null ||
            die "binfmt_misc unavailable; cannot cross-build $AXOS_ARCH"
    fi
    if ! mountpoint -q /proc/sys/fs/binfmt_misc; then
        mount -t binfmt_misc none /proc/sys/fs/binfmt_misc ||
            die "could not mount binfmt_misc"
    fi

    handler="qemu-$(axos_uname_arch "$AXOS_ARCH")"
    if [[ ! -e "/proc/sys/fs/binfmt_misc/$handler" ]]; then
        if command -v update-binfmts >/dev/null 2>&1; then
            update-binfmts --enable "$handler" 2>/dev/null || true
        fi
    fi
    [[ -e "/proc/sys/fs/binfmt_misc/$handler" ]] ||
        die "binfmt handler '$handler' is not registered.
    Enable it with: update-binfmts --enable $handler
    or run: docker run --rm --privileged multiarch/qemu-user-static --reset -p yes"

    grep -q '^flags:.*F' "/proc/sys/fs/binfmt_misc/$handler" 2>/dev/null ||
        warn "binfmt handler '$handler' lacks the F flag; chroot execution may fail"

    # mmdebstrap refuses to bootstrap a foreign architecture unless arch-test
    # can confirm the emulator actually runs binaries for it. Without this the
    # failure surfaces as a bare "E: install arch-test for foreign architecture
    # support" several minutes into the bootstrap.
    need_cmd arch-test
    arch-test "$AXOS_ARCH" >/dev/null 2>&1 ||
        die "arch-test reports this host cannot execute $AXOS_ARCH binaries.
    The binfmt handler is registered but not working. Check:
        arch-test $AXOS_ARCH
        cat /proc/sys/fs/binfmt_misc/$handler"

    ok "cross-build support ready ($qemu_bin, arch-test $AXOS_ARCH ok)"
else
    ok "native build for $AXOS_ARCH"
fi

# --- Disk space -------------------------------------------------------------
avail_mib=$(df -Pm "$AXOS_WORKDIR" 2>/dev/null | awk 'NR==2 {print $4}')
avail_mib="${avail_mib:-0}"
if (( avail_mib < 12000 )); then
    warn "only ${avail_mib}MiB free at $AXOS_WORKDIR; 12GiB+ recommended"
fi

# --- Branding assets --------------------------------------------------------
for asset in \
    "$AXOS_ROOT/branding/ascii/axos-logo.txt" \
    "$AXOS_ROOT/branding/ascii/axos-logo-compact.txt" \
    "$AXOS_ROOT/branding/ascii/axos-logo-mini.txt" \
    "$AXOS_ROOT/branding/assets/AXnobg.png"
do
    [[ -r "$asset" ]] || die "missing branding asset: $asset
    regenerate ASCII variants with: python3 branding/ascii/generate-variants.py"
done
ok "branding assets present"

# --- base archive keyring ---------------------------------------------------
# Checked here so a non-Debian build host fails in seconds rather than several
# minutes into the bootstrap with an opaque NO_PUBKEY error.
[[ -r "$AXOS_KEYRING" ]] || die "archive keyring not found: $AXOS_KEYRING
    The build host is $( . /etc/os-release 2>/dev/null && echo "$NAME" || echo "not Debian" ),
    so the Debian archive keys are not installed by default. Fix with:
        apt-get install debian-archive-keyring"
ok "archive keyring present ($AXOS_KEYRING)"

ok "stage 00 complete"
