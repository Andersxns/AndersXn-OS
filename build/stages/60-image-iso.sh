#!/usr/bin/env bash
#
# Stage 60 - build the bootable live installer ISO.
#
# Produces a hybrid image: UEFI via an embedded EFI system partition, legacy
# BIOS via Limine's El Torito stage. The same file boots from a USB stick
# written with dd and from a virtual optical drive.

set -euo pipefail
AXOS_ROOT="${AXOS_ROOT:-$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)}"
# shellcheck source=../lib/common.sh
source "$AXOS_ROOT/build/lib/common.sh"
# shellcheck source=../config/build.conf
source "$AXOS_ROOT/build/config/build.conf"

require_root
[[ -d "$AXOS_ROOTFS" ]] || die "no rootfs at $AXOS_ROOTFS - run stage 10 first"
trap 'axos_umount_all' EXIT INT TERM

need_cmd mksquashfs xorriso

limine_dir="$AXOS_WORKDIR/limine"
[[ -d "$limine_dir" ]] || die "Limine not staged - run stage 40 first"

iso_root="$AXOS_WORKDIR/iso"
rm -rf -- "$iso_root"
install -dm0755 "$iso_root"/{live,andersxn,EFI/BOOT}

# ---------------------------------------------------------------------------
# Kernel and initramfs for the live boot
# ---------------------------------------------------------------------------
kernel_ver="$(axos_debian_kernel "$AXOS_ROOTFS")"
[[ -n "$kernel_ver" ]] || die "no kernel in the rootfs"

cp "$AXOS_ROOTFS/boot/vmlinuz-$kernel_ver"    "$iso_root/live/vmlinuz"
cp "$AXOS_ROOTFS/boot/initrd.img-$kernel_ver" "$iso_root/live/initrd.img"
ok "live kernel $kernel_ver staged"

# ---------------------------------------------------------------------------
# squashfs
#
# zstd at level 19 rather than xz: it decompresses several times faster, which
# is what the operator actually feels while the installer copies the image, and
# the size difference on a system this small is a few tens of MiB.
# ---------------------------------------------------------------------------
# Put the installed-system resolv.conf symlink back before sealing the image:
# the build-time copy points at whatever resolved this build host, which is
# meaningless on the machine that boots the ISO.
axos_finalize_resolv "$AXOS_ROOTFS"

log "compressing the root filesystem (this takes a while)"
mksquashfs "$AXOS_ROOTFS" "$iso_root/live/filesystem.squashfs" \
    -comp zstd -Xcompression-level 19 \
    -b 1M -noappend -no-progress \
    -e boot/vmlinuz-* boot/initrd.img-* \
    -wildcards \
    -e 'var/cache/apt/archives/*.deb' 'var/lib/apt/lists/*' 'tmp/*' ||
    die "mksquashfs failed"

squash_mib=$(du -m "$iso_root/live/filesystem.squashfs" | cut -f1)
ok "squashfs built (${squash_mib}MiB)"

# live-boot needs this to find the image without a filesystem label.
printf '%s\n' "filesystem.squashfs" > "$iso_root/live/filesystem.module"

# ---------------------------------------------------------------------------
# Branding assets on the medium
# ---------------------------------------------------------------------------
if [[ -f "$AXOS_WORKDIR/splash.png" ]]; then
    cp "$AXOS_WORKDIR/splash.png" "$iso_root/andersxn/splash.png"
fi
cp "$AXOS_ROOT/branding/ascii/axos-logo.txt" "$iso_root/andersxn/logo.txt"

# ---------------------------------------------------------------------------
# Limine
# ---------------------------------------------------------------------------
log "installing Limine onto the ISO tree"
axos_render "$AXOS_ROOT/branding/limine/limine-live.conf.in" "$iso_root/limine.conf"

efi_stub="$(axos_efi_stub "$AXOS_ARCH")"
[[ -f "$limine_dir/$efi_stub" ]] || die "Limine EFI binary $efi_stub not found"
cp "$limine_dir/$efi_stub" "$iso_root/EFI/BOOT/$efi_stub"

# Force the boot files to the FRONT of the image.
#
# Without this, xorriso lays each directory out alphabetically, so
# /live/filesystem.squashfs ("f") is written before initrd.img ("i") and
# vmlinuz ("v"). Once the squashfs passed a gigabyte that pushed the kernel
# past 1.4GB into the ISO, where Limine's BIOS CD reads could no longer reach
# it - the read returned garbage and Limine rejected the header with
# "Invalid kernel signature". It booted fine at 678MB purely by luck.
#
# Higher weight means closer to the start of the image.
xorriso_args=(
    -as mkisofs
    -volid "AXOS_${AXOS_VERSION//./}"
    -full-iso9660-filenames
    -joliet -rational-rock
    # The source tree must be added BEFORE the weights: --sort-weight addresses
    # paths inside the image being composed, not paths on disk.
    "$iso_root"
    --sort-weight 100 /live/vmlinuz
    --sort-weight 100 /live/initrd.img
    --sort-weight 90  /limine.conf
    --sort-weight 90  /andersxn
    --sort-weight 80  /EFI
    --sort-weight -10 /live/filesystem.squashfs
)

if [[ "$AXOS_ARCH" == "amd64" ]]; then
    # BIOS + UEFI hybrid. amd64 is the only target where legacy BIOS is worth
    # carrying; arm64 machines are UEFI or device-tree, never El Torito BIOS.
    cp "$limine_dir/limine-bios.sys"    "$iso_root/limine-bios.sys"
    cp "$limine_dir/limine-bios-cd.bin" "$iso_root/limine-bios-cd.bin"
    cp "$limine_dir/limine-uefi-cd.bin" "$iso_root/limine-uefi-cd.bin"

    xorriso_args+=(
        -b limine-bios-cd.bin
        -no-emul-boot -boot-load-size 4 -boot-info-table
        --efi-boot limine-uefi-cd.bin
        -efi-boot-part --efi-boot-image
        --protective-msdos-label
    )
else
    cp "$limine_dir/limine-uefi-cd.bin" "$iso_root/limine-uefi-cd.bin"
    xorriso_args+=(
        --efi-boot limine-uefi-cd.bin
        -efi-boot-part --efi-boot-image
        --protective-msdos-label
    )
fi

iso_path="$AXOS_DISTDIR/${AXOS_IMAGE_BASENAME}.iso"
mkdir -p "$AXOS_DISTDIR"
rm -f "$iso_path"

log "writing $iso_path"
# Note: $iso_root is already inside xorriso_args, ahead of the sort weights.
xorriso "${xorriso_args[@]}" -o "$iso_path" ||
    die "xorriso failed"

# Embed the BIOS stage into the finished image so a dd-written USB stick boots
# on legacy firmware as well as from optical media.
if [[ "$AXOS_ARCH" == "amd64" ]]; then
    # The HOST-architecture utility: this runs here, on the build machine.
    # (make puts it at the repo root; older layouts used bin/.)
    limine_host=""
    for cand in "$limine_dir/limine" "$limine_dir/bin/limine"; do
        [[ -x "$cand" ]] && { limine_host="$cand"; break; }
    done

    if [[ -n "$limine_host" ]]; then
        "$limine_host" bios-install "$iso_path" &&
            ok "Limine BIOS stage embedded in the ISO" ||
            warn "limine bios-install failed; El Torito CD boot still works"
    else
        warn "limine host utility not found - skipping the isohybrid MBR stage."
        warn "The ISO still boots as a CD (El Torito) under BIOS and UEFI, but"
        warn "writing it to a USB stick with dd may not boot on legacy BIOS."
    fi
fi

# ---------------------------------------------------------------------------
# Verify the boot files landed early
#
# This is the failure that produced "Invalid kernel signature", and it is
# silent: the ISO builds fine and only fails on real hardware. Checking the
# extent here turns it into a build error instead.
# ---------------------------------------------------------------------------
kernel_lba="$(xorriso -indev "$iso_path" -find /live/vmlinuz -exec report_lba -- 2>/dev/null |
    awk -F'[ ,]+' '/vmlinuz/ {print $5}')"
if [[ -n "$kernel_lba" ]]; then
    kernel_mib=$(( kernel_lba * 2048 / 1024 / 1024 ))
    info "kernel sits at LBA $kernel_lba (${kernel_mib}MiB into the image)"
    if (( kernel_mib > 700 )); then
        die "the kernel is ${kernel_mib}MiB into the ISO - too far in for a BIOS
    CD read, which is what causes Limine's \"Invalid kernel signature\".
    The --sort-weight arguments in this stage are not taking effect."
    fi
    ok "boot files are near the start of the image"
else
    warn "could not determine the kernel's extent; skipping the placement check"
fi

# ---------------------------------------------------------------------------
# Checksums and compression
# ---------------------------------------------------------------------------
( cd "$AXOS_DISTDIR" && sha256sum "$(basename "$iso_path")" > "$(basename "$iso_path").sha256" )

if [[ "${AXOS_COMPRESS:-1}" == "1" ]] && command -v zstd >/dev/null 2>&1; then
    log "compressing the release artifact"
    zstd -19 -T0 -q -f "$iso_path" -o "${iso_path}.zst"
fi

ok "ISO ready: $iso_path ($(du -h "$iso_path" | cut -f1))"
