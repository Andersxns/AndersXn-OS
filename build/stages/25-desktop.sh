#!/usr/bin/env bash
#
# Stage 25 - KDE Plasma, and the live installer session.
#
# Runs before stage 40 on purpose: live-boot must be installed BEFORE the
# initramfs is regenerated, or the resulting image has no hook to find and
# mount the squashfs, and the ISO cannot boot at all.
#
# The live session is built here rather than left to live-config. live-config
# creates its own user and rewrites the tty1 autologin at boot; when that does
# not complete you get agetty autologging into an account that does not exist -
# an "Authentication failure" respawn loop. Everything below happens at BUILD
# time, so nothing has to succeed at boot for the installer to appear.

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

# Video drivers are architecture-specific; see AXOS_PKGS_VIDEO_* in build.conf.
case "$AXOS_ARCH" in
    amd64) video_pkgs="$AXOS_PKGS_VIDEO_AMD64" ;;
    arm64) video_pkgs="$AXOS_PKGS_VIDEO_ARM64" ;;
    *)     die "no video package set defined for $AXOS_ARCH" ;;
esac

# ---------------------------------------------------------------------------
# Fail fast on packages that do not exist for this architecture.
#
# Without this, a name that is x86-only (xserver-xorg-video-vmware, say) is not
# discovered until apt has already spent several minutes installing everything
# before it. Ten seconds here saves that every time.
# ---------------------------------------------------------------------------
log "checking package availability for $AXOS_ARCH"
axos_chroot_sh "$AXOS_ROOTFS" <<'CHROOT' >/dev/null
apt-get update -qq
CHROOT

missing="$(axos_chroot "$AXOS_ROOTFS" /bin/sh -c '
for p in '"$AXOS_PKGS_DESKTOP $video_pkgs $AXOS_PKGS_INSTALLER_GUI"'; do
    c=$(apt-cache policy "$p" 2>/dev/null | awk "/Candidate:/ {print \$2}")
    if [ -z "$c" ] || [ "$c" = "(none)" ]; then printf "%s " "$p"; fi
done')"

if [[ -n "${missing// /}" ]]; then
    die "no installation candidate for $AXOS_ARCH: ${missing% }
    Either the package is x86-only, or the name changed in this Debian
    release. Check AXOS_PKGS_DESKTOP and AXOS_PKGS_VIDEO_* in
    build/config/build.conf."
fi
ok "every desktop package resolves for $AXOS_ARCH"

# ---------------------------------------------------------------------------
# Packages
#
# The desktop is installed WITH recommends. The base system keeps
# Install-Recommends "false" for leanness, but Plasma's recommended packages
# are what make it a working desktop rather than a shell with pieces missing.
#
# The live set is NOT installed here - stage 20 does it, because live-boot has
# to be present before stage 40 regenerates the initramfs.
# ---------------------------------------------------------------------------
log "installing KDE Plasma"
info "on a cross build this is the slowest step - every maintainer script runs under qemu"

axos_chroot_sh "$AXOS_ROOTFS" <<CHROOT
apt-get install -y --install-recommends $AXOS_PKGS_DESKTOP $video_pkgs
apt-get install -y --no-install-recommends $AXOS_PKGS_INSTALLER_GUI
apt-get clean
CHROOT
ok "Plasma installed"

# ---------------------------------------------------------------------------
# Raspberry Pi kernel and firmware (arm64 only)
#
# The Pi has no UEFI: its bootloader reads config.txt from the FAT partition
# and loads the kernel and DTB itself. It also needs a kernel Debian does not
# provide - see the AXOS_PKGS_PI_KERNEL comment in build.conf. Stage 70 lays
# all of it out on the boot partition beside the EFI path that generic arm64
# machines use.
# ---------------------------------------------------------------------------
if [[ "$AXOS_ARCH" == "arm64" ]]; then
    log "adding the Raspberry Pi archive"

    # raspi-firmware's kernel hook expects this to exist; without it the
    # postinst fails and takes the whole apt transaction with it.
    install -dm0755 "$AXOS_ROOTFS/boot/firmware"

    axos_chroot_sh "$AXOS_ROOTFS" <<CHROOT
# Stored armoured, exactly as served. apt accepts an ASCII-armoured key in
# Signed-By directly, and that avoids gpg --dearmor - gpg wants a
# controlling tty, which does not exist inside this chroot.
curl -fsSL --retry 3 "$AXOS_RPI_KEY_URL" -o /usr/share/keyrings/raspberrypi.asc
chmod 0644 /usr/share/keyrings/raspberrypi.asc
grep -q 'BEGIN PGP PUBLIC KEY BLOCK' /usr/share/keyrings/raspberrypi.asc

cat > /etc/apt/sources.list.d/raspberrypi.sources <<SOURCES
Types: deb
URIs: $AXOS_RPI_REPO
Suites: $AXOS_RPI_SUITE
Components: main
Architectures: arm64
Signed-By: /usr/share/keyrings/raspberrypi.asc
SOURCES
CHROOT

    # Pinning is not optional here. Left at its default priority the Raspberry
    # Pi archive shadows large parts of Debian with Raspberry Pi OS rebuilds -
    # a different distribution wearing Debian's package names. Everything from
    # it loses to Debian except the handful of things we actually want.
    install -Dm0644 /dev/stdin "$AXOS_ROOTFS/etc/apt/preferences.d/raspberrypi" <<'PIN'
# Raspberry Pi archive: kernels and firmware only.
#
# firmware-brcm80211 matters as much as the kernel. Debian's build omits
# the Pi board NVRAM files the CYW43455 needs, and brcmfmac then fails its
# firmware handshake with -52 (EBADE) and brings up no wireless at all.
# The +rpt rebuild carries them.
Package: *
Pin: origin archive.raspberrypi.com
Pin-Priority: 100

Package: linux-image-rpi-* linux-image-*-rpi-* linux-headers-rpi-* raspi-firmware raspi-utils firmware-brcm80211 bluez-firmware
Pin: origin archive.raspberrypi.com
Pin-Priority: 1000
PIN

    # -----------------------------------------------------------------------
    # The Raspberry Pi archive key is from 2012 and its self-signature uses
    # SHA1. Debian trixie verifies apt signatures with Sequoia (sqv), whose
    # default policy has rejected SHA1 since 2026-02-01, so the repository is
    # refused outright. There is no newer key: the packaged
    # raspberrypi-archive-keyring carries the same one.
    #
    # This re-permits SHA1 for SECOND-PREIMAGE resistance only, which is the
    # exact property the error names, and leaves collision resistance rejected
    # - collision resistance is what an attacker would need to forge a
    # signature. Verification stays on for every repository, Debian's included;
    # only this one narrow property is relaxed.
    # -----------------------------------------------------------------------
    install -Dm0644 /dev/stdin \
        "$AXOS_ROOTFS/etc/crypto-policies/back-ends/sequoia.config" <<'POLICY'
# AndersXn OS - see build/stages/25-desktop.sh for why this exists.
[hash_algorithms]
sha1.collision_resistance = "never"
sha1.second_preimage_resistance = "always"
POLICY
    log "installing the Raspberry Pi kernel and firmware"
    axos_chroot_sh "$AXOS_ROOTFS" <<CHROOT
apt-get update -qq
apt-get install -y --no-install-recommends $AXOS_PKGS_PI_KERNEL $AXOS_PKGS_ARM_FIRMWARE
apt-get clean
CHROOT

    # Confirm both kernels landed. A Pi 5 needs rpi-2712 specifically; without
    # it the board boots Debian's mainline kernel and cannot find its own SD
    # card, which is the failure this whole block exists to prevent.
    for flavour in 2712 v8; do
        if compgen -G "$AXOS_ROOTFS/boot/vmlinuz-*-rpi-$flavour" >/dev/null; then
            ok "Pi kernel present: $(basename "$(ls -1 "$AXOS_ROOTFS"/boot/vmlinuz-*-rpi-$flavour | sort -V | tail -1)")"
        else
            die "no rpi-$flavour kernel in the rootfs. Without it a Raspberry Pi
    drops to an initramfs shell. Check that $AXOS_RPI_REPO carries
    $AXOS_RPI_SUITE for arm64."
        fi
    done


    # -----------------------------------------------------------------------
    # Tell Xorg which GPU drives the display.
    #
    # A Pi exposes two DRM devices: vc4 (the display pipeline, with outputs)
    # and v3d (render-only, no outputs). Xorg's autoconfiguration picks one,
    # and if it lands on the render node it finds no screens and exits - SDDM
    # then starts, fails, and the board sits at a text console with a perfectly
    # healthy /dev/dri.
    #
    # MatchDriver only fires on vc4 devices, so this is inert on every other
    # arm64 machine. Raspberry Pi OS ships the same rule via
    # raspberrypi-sys-mods; that package is not used here because it carries a
    # great deal of unrelated Raspberry Pi OS system configuration.
    # -----------------------------------------------------------------------
    install -Dm0644 /dev/stdin \
        "$AXOS_ROOTFS/usr/share/X11/xorg.conf.d/99-axos-vc4.conf" <<'XORG'
Section "OutputClass"
    Identifier  "vc4"
    MatchDriver "vc4"
    Driver      "modesetting"
    Option      "PrimaryGPU" "true"
EndSection
XORG
    ok "Xorg vc4 primary-GPU rule installed"

    # smartmontools polls for SMART data that an SD card cannot provide, so it
    # fails on every boot and puts a red line on an otherwise clean startup.
    axos_chroot_sh "$AXOS_ROOTFS" <<'CHROOT'
systemctl disable smartmontools.service >/dev/null 2>&1 || true
systemctl mask smartmontools.service    >/dev/null 2>&1 || true
CHROOT
    [[ -d "$AXOS_ROOTFS/usr/lib/raspi-firmware" ]] ||
        warn "raspi-firmware installed but /usr/lib/raspi-firmware is missing"
fi

# live-config must never creep back in as a dependency of something else: it
# would resume fighting this stage's live session at boot.
if axos_chroot "$AXOS_ROOTFS" dpkg -l live-config 2>/dev/null | grep -q '^ii'; then
    warn "live-config got pulled in as a dependency - removing it"
    axos_chroot_sh "$AXOS_ROOTFS" <<'CHROOT'
apt-get purge -y live-config >/dev/null 2>&1 || true
CHROOT
fi

# ---------------------------------------------------------------------------
# The live user
#
# Created at build time with no password: SDDM logs it in without asking, and
# the installer escalates through sudo. Both the account and its sudo rule are
# removed from the installed system by AX-Installer.
# ---------------------------------------------------------------------------
log "creating the live user '$AXOS_LIVE_USER'"
axos_chroot_sh "$AXOS_ROOTFS" <<CHROOT
if ! id -u "$AXOS_LIVE_USER" >/dev/null 2>&1; then
    useradd --create-home --shell /usr/bin/zsh \
        --comment "AndersXn live session" \
        --groups sudo,video,audio,netdev,plugdev "$AXOS_LIVE_USER"
fi
passwd -d "$AXOS_LIVE_USER" >/dev/null 2>&1 || true
CHROOT

install -Dm0440 /dev/stdin "$AXOS_ROOTFS/etc/sudoers.d/99-axos-live" <<SUDO
# LIVE MEDIA ONLY - removed by AX-Installer during "Configure system".
# The graphical installer needs root and the live session has no password.
$AXOS_LIVE_USER ALL=(ALL) NOPASSWD: ALL
SUDO
ok "live user ready"

# ---------------------------------------------------------------------------
# The dedicated installer session
#
# Booting the ISO shows the installer and nothing else: no panel, no menu, no
# desktop. A full Plasma session behind the installer reads as an already
# installed machine, which is exactly the wrong thing to imply while the
# operator is deciding which disk to erase.
#
# kwin_x11 is used as the window manager rather than a second lightweight one:
# Plasma already ships it, so the session costs no extra packages, and the
# installer gets normal focus and fullscreen handling.
# ---------------------------------------------------------------------------
log "creating the dedicated installer session"

install -Dm0755 /dev/stdin "$AXOS_ROOTFS/usr/bin/andersxn-installer-session" <<SESSION
#!/bin/sh
# AndersXn OS - live installer session.
#
# A window manager and the installer. Nothing else runs here.
set -u

export XDG_CURRENT_DESKTOP=KDE
export XDG_SESSION_DESKTOP=andersxn-installer
# Tells ax-installer-gtk to open fullscreen rather than as a window.
export AXOS_INSTALLER_KIOSK=1

# Brand backdrop behind the installer, so the edges are ink rather than the
# X server's default grey stipple.
command -v xsetroot >/dev/null 2>&1 && xsetroot -solid "#${AXOS_HEX_INK}"

# The WM must be running before the installer maps its window, or the window
# gets no focus and keyboard input goes nowhere.
kwin_x11 &
sleep 1

# If the installer exits, the session ends and SDDM brings it straight back.
exec /usr/bin/ax-installer-gtk
SESSION

install -Dm0644 /dev/stdin \
    "$AXOS_ROOTFS/usr/share/xsessions/andersxn-installer.desktop" <<DESKTOP
[Desktop Entry]
Type=Application
Name=$AXOS_NAME Installer
Comment=Install $AXOS_NAME $AXOS_VERSION
Exec=/usr/bin/andersxn-installer-session
TryExec=/usr/bin/ax-installer-gtk
DesktopNames=AndersXn
DESKTOP

# ---------------------------------------------------------------------------
# SDDM
#
# Autologin straight into the installer session on live media. The autologin
# drop-in is its own file so AX-Installer deletes exactly that and leaves the
# rest of the SDDM configuration in place for the installed system.
# ---------------------------------------------------------------------------
log "configuring SDDM"
install -dm0755 "$AXOS_ROOTFS/etc/sddm.conf.d"

install -Dm0644 /dev/stdin \
    "$AXOS_ROOTFS/etc/sddm.conf.d/50-andersxn.conf" <<'SDDM'
# AndersXn OS - SDDM defaults (kept on installed systems).
[Theme]
Current=breeze

[General]
# VirtualBox and most VMs have no working DRM cursor; the software cursor
# avoids an invisible pointer on the greeter.
GreeterEnvironment=QT_SCREEN_SCALE_FACTORS=1,QSG_RENDER_LOOP=basic

[Users]
MaximumUid=60000
MinimumUid=1000
SDDM

install -Dm0644 /dev/stdin \
    "$AXOS_ROOTFS/etc/sddm.conf.d/90-axos-live-autologin.conf" <<SDDM
# LIVE MEDIA ONLY - deleted by AX-Installer during "Configure system".
[Autologin]
User=$AXOS_LIVE_USER
Session=andersxn-installer
Relogin=true
SDDM

log "enabling the graphical target"
axos_chroot_sh "$AXOS_ROOTFS" <<'CHROOT'
systemctl enable sddm.service           >/dev/null 2>&1 || true
systemctl set-default graphical.target  >/dev/null 2>&1 || true
# NetworkManager owns the desktop's connections; systemd-networkd from stage 20
# would fight it for the same interfaces.
systemctl enable NetworkManager.service >/dev/null 2>&1 || true
systemctl disable systemd-networkd.service >/dev/null 2>&1 || true
CHROOT

ok "stage 25 complete"
