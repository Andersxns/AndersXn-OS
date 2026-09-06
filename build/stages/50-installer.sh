#!/usr/bin/env bash
#
# Stage 50 - build AX-Installer and install the provisioning tree.
#
# Cross-compiles the Go installer for the target architecture (CGO is off, so
# no cross toolchain is needed), then places it, the provisioning modules and
# the live autostart unit into the rootfs.

set -euo pipefail
AXOS_ROOT="${AXOS_ROOT:-$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)}"
# shellcheck source=../lib/common.sh
source "$AXOS_ROOT/build/lib/common.sh"
# shellcheck source=../config/build.conf
source "$AXOS_ROOT/build/config/build.conf"

require_root
[[ -d "$AXOS_ROOTFS" ]] || die "no rootfs at $AXOS_ROOTFS - run stage 10 first"

need_cmd go

SRC="$AXOS_ROOT/installer"

# ---------------------------------------------------------------------------
# Keep the embedded ASCII marks in step with branding/ascii before building.
# go:embed reads from the package directory, so these must be real files.
# ---------------------------------------------------------------------------
log "syncing embedded branding assets"
for v in axos-logo.txt axos-logo-compact.txt axos-logo-mini.txt; do
    axos_install_file "$AXOS_ROOT/branding/ascii/$v" \
        "$SRC/internal/branding/assets/$v"
done

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------
log "building ax-installer for $AXOS_ARCH"

pkg="github.com/ggstudios/andersxn-os/installer/internal/branding"
# Each -X value is single-quoted. Go splits the -ldflags string itself using
# shell-like quoting rules, and two of these values contain spaces
# ("AndersXn OS", "GG Studios") - unquoted, the linker sees "OS" as a stray
# flag and dumps its usage instead of building.
ldflags="-s -w"
ldflags+=" -X '${pkg}.Version=${AXOS_VERSION}'"
ldflags+=" -X '${pkg}.Codename=${AXOS_CODENAME}'"
ldflags+=" -X '${pkg}.Name=${AXOS_NAME}'"
ldflags+=" -X '${pkg}.Vendor=${AXOS_VENDOR}'"

outdir="$AXOS_WORKDIR/installer-bin"
mkdir -p "$outdir"

# GOFLAGS=-mod=mod lets the first build populate go.sum; CI should commit it.
( cd "$SRC" && GOFLAGS=-mod=mod go mod download ) || die "go mod download failed"

for cmd in ax-installer ax-installer-gui; do
    ( cd "$SRC" && \
        CGO_ENABLED=0 GOOS=linux GOARCH="$AXOS_ARCH" \
        go build -trimpath -ldflags "$ldflags" -o "$outdir/$cmd" "./cmd/$cmd" ) ||
        die "building $cmd failed"
    ok "built $cmd ($(du -h "$outdir/$cmd" | cut -f1))"
done

install -Dm0755 "$outdir/ax-installer"     "$AXOS_ROOTFS/usr/bin/ax-installer"
install -Dm0755 "$outdir/ax-installer-gui" "$AXOS_ROOTFS/usr/bin/ax-installer-gui"

# ---------------------------------------------------------------------------
# Provisioning tree
# ---------------------------------------------------------------------------
log "installing provisioning modules"
install -Dm0755 "$AXOS_ROOT/provisioning/postinstall.sh" \
    "$AXOS_ROOTFS/usr/lib/andersxn/postinstall.sh"

install -dm0755 "$AXOS_ROOTFS/usr/lib/andersxn/modules"
install -Dm0644 "$AXOS_ROOT/provisioning/modules/_lib.sh" \
    "$AXOS_ROOTFS/usr/lib/andersxn/modules/_lib.sh"
for m in "$AXOS_ROOT"/provisioning/modules/[0-9][0-9]-*.sh; do
    install -Dm0755 "$m" "$AXOS_ROOTFS/usr/lib/andersxn/modules/$(basename "$m")"
done

for tool in ax-provision ax-release; do
    install -Dm0755 "$AXOS_ROOT/provisioning/bin/$tool" "$AXOS_ROOTFS/usr/bin/$tool"
done

install -dm0755 "$AXOS_ROOTFS/etc/nftables.d"

# ---------------------------------------------------------------------------
# Verify every catalog module has a script
#
# The Go catalog and the shell modules are two halves of one contract. Catching
# a mismatch here beats discovering it on a machine mid-install.
# ---------------------------------------------------------------------------
log "checking catalog/module parity"
missing=0
while read -r id; do
    [[ -n "$id" ]] || continue
    if ! compgen -G "$AXOS_ROOT/provisioning/modules/[0-9][0-9]-${id}.sh" >/dev/null; then
        warn "catalog module '$id' has no provisioning script"
        missing=1
    fi
done < <(grep -oP '^\s*ID:\s*"\K[a-z0-9-]+' "$SRC/internal/provision/catalog.go")
(( missing == 0 )) || die "catalog and provisioning modules disagree"
ok "catalog and modules agree"

# ---------------------------------------------------------------------------
# The graphical installer
#
# Python/PyGObject, so nothing here is compiled and nothing has to be
# cross-built. It drives the Go engine over the JSON protocol in
# installer/internal/api rather than touching disks itself.
# ---------------------------------------------------------------------------
log "installing the graphical installer"
install -Dm0755 "$AXOS_ROOT/installer/gui/ax-installer-gtk" \
    "$AXOS_ROOTFS/usr/bin/ax-installer-gtk"

install -Dm0644 /dev/stdin \
    "$AXOS_ROOTFS/usr/share/applications/andersxn-installer.desktop" <<DESKTOP
[Desktop Entry]
Type=Application
Version=1.0
Name=Install $AXOS_NAME
GenericName=System Installer
Comment=Install $AXOS_NAME $AXOS_VERSION to this machine
Exec=/usr/bin/ax-installer-gtk
Icon=/usr/share/andersxn/assets/AXnobg.png
Terminal=false
Categories=System;Settings;
Keywords=install;installer;system;
StartupNotify=true
DESKTOP

# ---------------------------------------------------------------------------
# NOTE: there is deliberately no XDG autostart entry.
#
# The live medium boots straight into the dedicated installer session created
# by stage 25 (/usr/share/xsessions/andersxn-installer.desktop), which runs a
# window manager and the installer and nothing else. An autostart entry on top
# of that would launch a second copy of the installer.
#
# The applications-menu entry above is kept, so the installer is still
# reachable by hand from a full desktop session.
# ---------------------------------------------------------------------------

# ---------------------------------------------------------------------------
# Recovery console
#
# tty2 gets a root shell on live media. If the desktop fails to start there
# would otherwise be no way in: the live user has no password, and Debian's
# PAM stack refuses passwordless console logins.
# ---------------------------------------------------------------------------
install -Dm0644 /dev/stdin \
    "$AXOS_ROOTFS/etc/systemd/system/getty@tty2.service.d/axos-recovery.conf" <<'UNIT'
# LIVE MEDIA ONLY - removed by AX-Installer during "Configure system".
[Service]
ExecStart=
ExecStart=-/sbin/agetty --autologin root --noclear %I $TERM
UNIT

# The old tty1 override is gone; make sure a rebuild over an existing rootfs
# does not leave it behind to fight LightDM.
rm -f "$AXOS_ROOTFS/etc/systemd/system/getty@tty1.service.d/ax-installer.conf"
rm -f "$AXOS_ROOTFS/usr/lib/andersxn/live-installer-shell"
rmdir "$AXOS_ROOTFS/etc/systemd/system/getty@tty1.service.d" 2>/dev/null || true

ok "stage 50 complete"
