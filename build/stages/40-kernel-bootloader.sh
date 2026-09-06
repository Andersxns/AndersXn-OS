#!/usr/bin/env bash
#
# Stage 40 - kernel, initramfs and bootloader acquisition.
#
# Configures initramfs-tools for the AndersXn boot path (Plymouth splash, btrfs
# root, LUKS) and stages the bootloader binaries that stages 60 and 70 write
# onto the ESP. Nothing here touches a real disk.

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
# initramfs
# ---------------------------------------------------------------------------
log "configuring initramfs"

# MODULES=dep keeps the initramfs small on a machine that will only ever boot
# its own hardware; the live ISO needs "most" because it boots anything.
install -Dm0644 /dev/stdin "$AXOS_ROOTFS/etc/initramfs-tools/conf.d/andersxn.conf" <<'CONF'
# AndersXn OS initramfs policy.
# "most" is required for portable/live media. AX-Installer rewrites this to
# "dep" on an installed system, which typically halves the initramfs.
MODULES=most
COMPRESS=zstd
COMPRESSLEVEL=9
RESUME=none
CONF

# Filesystem and crypto modules the installer may need to mount a root device.
cat > "$AXOS_ROOTFS/etc/initramfs-tools/modules" <<'MODS'
btrfs
crc32c
xxhash_generic
zstd_compress
dm_mod
dm_crypt
MODS

log "rebuilding initramfs for all installed kernels"
axos_chroot_sh "$AXOS_ROOTFS" <<'CHROOT'
if command -v update-initramfs >/dev/null 2>&1; then
    update-initramfs -u -k all
fi
CHROOT

# Debian's kernel specifically. Limine boots this one, and on arm64 the
# Raspberry Pi kernels installed alongside it carry a higher version.
kernel_ver="$(axos_debian_kernel "$AXOS_ROOTFS")"
[[ -n "$kernel_ver" ]] || die "no kernel found in the rootfs - stage 20 may have failed"
ok "kernel $kernel_ver"

# Record it for the ISO/image stages so they do not have to re-derive it.
echo "AXOS_KERNEL_VERSION_BUILT=$kernel_ver" >> "$AXOS_ROOTFS/etc/andersxn/build-manifest"

# ---------------------------------------------------------------------------
# Bootloader
# ---------------------------------------------------------------------------
limine_dir="$AXOS_WORKDIR/limine"

case "$AXOS_BOOTLOADER" in
limine)
    if [[ -f "$limine_dir/limine.c" ]]; then
        info "Limine already fetched at $limine_dir"
    else
        log "fetching Limine v$AXOS_LIMINE_VERSION"
        rm -rf -- "$limine_dir"

        # The "-binary" branches carry prebuilt EFI images, so no cross
        # toolchain is needed for the bootloader payloads themselves.
        axos_retry 3 git clone --depth 1 \
            --branch "v${AXOS_LIMINE_VERSION}-binary" \
            "$AXOS_LIMINE_REPO" "$limine_dir" ||
            die "could not fetch Limine v$AXOS_LIMINE_VERSION.
    Check AXOS_LIMINE_VERSION in build/config/build.conf against the tags at
    $AXOS_LIMINE_REPO"
    fi

    for blob in BOOTX64.EFI BOOTIA32.EFI BOOTAA64.EFI limine-bios.sys limine-bios-cd.bin limine-uefi-cd.bin; do
        [[ -f "$limine_dir/$blob" ]] || warn "Limine blob missing: $blob"
    done

    # -----------------------------------------------------------------------
    # The limine(1) deploy utility is needed at TWO different architectures,
    # and conflating them ships a binary that cannot run:
    #
    #   host arch   - stage 60 runs it on this machine to embed the BIOS stage
    #                 into the ISO
    #   target arch - installed into the rootfs, where AX-Installer runs it on
    #                 the installed system for a legacy-BIOS install
    #
    # On a native build these are the same file. When cross-building they are
    # not, and a native "make" would put an unrunnable host-arch binary inside
    # the target image.
    # -----------------------------------------------------------------------
    log "building the limine deploy utility (host)"
    make -C "$limine_dir" >/dev/null || die "failed to build the limine host utility"
    [[ -x "$limine_dir/limine" ]] || die "limine host utility missing after make"

    limine_target_bin="$limine_dir/limine"

    if axos_is_cross "$AXOS_ARCH"; then
        cross_cc=""
        for cand in "$(axos_uname_arch "$AXOS_ARCH")-linux-gnu-gcc" \
                    "$(axos_uname_arch "$AXOS_ARCH")-linux-gnu-cc"; do
            command -v "$cand" >/dev/null 2>&1 && { cross_cc="$cand"; break; }
        done

        if [[ -n "$cross_cc" ]]; then
            log "cross-compiling limine for $AXOS_ARCH with $cross_cc"
            # Flags match the upstream Makefile. Failure here is NOT fatal: it
            # costs legacy-BIOS installs only, and aborting an otherwise good
            # image over an optional boot path is the wrong trade.
            if "$cross_cc" -g -O2 -pipe \
                    -o "$limine_dir/limine-target" "$limine_dir/limine.c"; then
                limine_target_bin="$limine_dir/limine-target"
            else
                warn "cross-compiling the limine utility failed."
                warn "The cross gcc needs the target libc headers too:"
                warn "    apt-get install libc6-dev-${AXOS_ARCH}-cross"
                warn "legacy-BIOS installs will not work (UEFI is unaffected)"
                limine_target_bin=""
            fi
        else
            warn "no cross compiler for $AXOS_ARCH found. Install:"
            warn "    gcc-$(axos_uname_arch "$AXOS_ARCH" | tr _ -)-linux-gnu libc6-dev-${AXOS_ARCH}-cross"
            warn "legacy-BIOS installs will not work (UEFI is unaffected)"
            limine_target_bin=""
        fi
    fi

    # AX-Installer reads these from the target at install time (see
    # internal/install/bootloader.go, limineShareDir), so the payloads have to
    # be inside the image, not only in the build tree.
    log "installing Limine payloads into the rootfs"
    install -dm0755 "$AXOS_ROOTFS/usr/share/limine"
    for blob in BOOTX64.EFI BOOTIA32.EFI BOOTAA64.EFI limine-bios.sys; do
        [[ -f "$limine_dir/$blob" ]] || continue
        install -Dm0644 "$limine_dir/$blob" "$AXOS_ROOTFS/usr/share/limine/$blob"
    done

    if [[ -n "$limine_target_bin" ]]; then
        # Never ship a binary the target cannot execute: a wrong-arch limine
        # fails at install time with a bare "Exec format error", long after the
        # point where it could be diagnosed easily.
        want_elf="$(axos_elf_machine_name "$AXOS_ARCH")"
        got_elf="$(file -b "$limine_target_bin")"
        if [[ "$got_elf" == *"$want_elf"* ]]; then
            install -Dm0755 "$limine_target_bin" "$AXOS_ROOTFS/usr/bin/limine"
            ok "limine utility installed into the rootfs ($want_elf)"
        else
            warn "refusing to install a wrong-architecture limine utility"
            warn "  wanted: $want_elf"
            warn "  got:    $got_elf"
            warn "legacy-BIOS installs from this image will not work (UEFI is unaffected)"
        fi
    fi

    # The bootloader splash the installer copies onto the boot partition.
    if [[ -f "$AXOS_WORKDIR/splash.png" ]]; then
        install -Dm0644 "$AXOS_WORKDIR/splash.png" \
            "$AXOS_ROOTFS/usr/share/andersxn/assets/splash.png"
    else
        warn "no splash.png staged by stage 30 - the boot menu will use a flat backdrop"
    fi

    ok "Limine staged at $limine_dir and installed into the rootfs"
    ;;

systemd-boot)
    # systemd-boot ships inside systemd itself; nothing to fetch. Confirm the
    # EFI binary for the target architecture is actually in the rootfs.
    sd_efi="$AXOS_ROOTFS/usr/lib/systemd/boot/efi/systemd-boot$(
        case "$AXOS_ARCH" in amd64) echo x64 ;; arm64) echo aa64 ;; esac).efi"
    [[ -f "$sd_efi" ]] ||
        die "systemd-boot EFI binary not found at ${sd_efi#$AXOS_ROOTFS}
    install the 'systemd-boot-efi' package in AXOS_PKGS_SYSTEM"
    ok "systemd-boot available: ${sd_efi#$AXOS_ROOTFS}"
    ;;
esac

# ---------------------------------------------------------------------------
# Bootloader templates for AX-Installer
#
# The installer regenerates the real config at install time against the actual
# root device, so it needs the templates on the target system rather than only
# in this build tree.
# ---------------------------------------------------------------------------
install -dm0755 "$AXOS_ROOTFS/usr/share/andersxn/bootloader"
axos_install_file "$AXOS_ROOT/branding/limine/limine.conf.in" \
    "$AXOS_ROOTFS/usr/share/andersxn/bootloader/limine.conf.in"
axos_install_file "$AXOS_ROOT/branding/systemd-boot/loader.conf.in" \
    "$AXOS_ROOTFS/usr/share/andersxn/bootloader/loader.conf.in"
for e in andersxn andersxn-verbose andersxn-recovery; do
    axos_install_file "$AXOS_ROOT/branding/systemd-boot/entries/$e.conf.in" \
        "$AXOS_ROOTFS/usr/share/andersxn/bootloader/entries/$e.conf.in"
done

ok "stage 40 complete"
