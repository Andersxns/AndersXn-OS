#!/usr/bin/env bash
#
# Stage 30 - apply the AndersXn OS visual identity.
#
# Every visual touchpoint is set here so the branding can be iterated on
# without re-bootstrapping:  os-release, /etc/issue, motd, Plymouth splash,
# Fastfetch, the shell skeleton, and the raster assets the bootloader draws.
#
# Safe to re-run:  build.sh --only 30

set -euo pipefail
AXOS_ROOT="${AXOS_ROOT:-$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)}"
# shellcheck source=../lib/common.sh
source "$AXOS_ROOT/build/lib/common.sh"
# shellcheck source=../config/build.conf
source "$AXOS_ROOT/build/config/build.conf"

require_root
[[ -d "$AXOS_ROOTFS" ]] || die "no rootfs at $AXOS_ROOTFS - run stage 10 first"
trap 'axos_umount_all' EXIT INT TERM

BRAND="$AXOS_ROOT/branding"
ASCII_DIR="$BRAND/ascii"

# Refuse to ship a file that still contains an unexpanded @TOKEN@.
#
# This has bitten twice: a template token and the stage that feeds it drift
# apart, the render silently no-ops, and the placeholder ends up on screen
# at the login prompt. Cheap to check, embarrassing to miss.
assert_rendered() {
    local f="$1" leftover
    leftover="$(grep -o '@[A-Z_]\{2,\}@' "$f" | sort -u | tr '\n' ' ')" || true
    [[ -z "$leftover" ]] || die "unexpanded tokens in ${f#$AXOS_ROOTFS}: $leftover
    The template and the stage that renders it disagree about the token name."
}

# ---------------------------------------------------------------------------
# ASCII marks - the single source of truth, consumed by installer, motd,
# /etc/issue and fastfetch alike.
# ---------------------------------------------------------------------------
log "installing ASCII marks"
install -dm0755 "$AXOS_ROOTFS/usr/share/andersxn/ascii"
for v in axos-logo.txt axos-logo-compact.txt axos-logo-mini.txt axos-logo-master.txt; do
    axos_install_file "$ASCII_DIR/$v" "$AXOS_ROOTFS/usr/share/andersxn/ascii/$v"
done

# ---------------------------------------------------------------------------
# System identity
# ---------------------------------------------------------------------------
log "rendering system identity"
axos_render "$BRAND/etc/os-release.in"  "$AXOS_ROOTFS/usr/lib/os-release"
assert_rendered "$AXOS_ROOTFS/usr/lib/os-release"
ln -sf ../usr/lib/os-release "$AXOS_ROOTFS/etc/os-release"
axos_render "$BRAND/etc/lsb-release.in" "$AXOS_ROOTFS/etc/lsb-release"
axos_render "$BRAND/etc/issue.net.in"   "$AXOS_ROOTFS/etc/issue.net"

# /etc/issue and /etc/motd embed the art itself, so the @ASCII_*@ token is
# expanded with the file contents before the ordinary @TOKEN@ pass. sed's "r"
# command cannot be used here because it appends after the line rather than
# replacing it, which would leave the placeholder visible.
render_with_ascii() {
    local src="$1" dst="$2" token="$3" art="$4" tmp
    tmp="$(mktemp)"
    awk -v token="$token" -v artfile="$art" '
        $0 == token {
            while ((getline line < artfile) > 0) print line
            close(artfile)
            next
        }
        { print }
    ' "$src" > "$tmp"
    axos_render "$tmp" "$dst"
    rm -f "$tmp"
}

# Compact, not mini: at 25 columns the downsample destroys the silhouette.
# 37x16 still fits an 80x25 console with the login prompt below it.
render_with_ascii "$BRAND/etc/issue.in" "$AXOS_ROOTFS/etc/issue" \
    "@ASCII_COMPACT@" "$ASCII_DIR/axos-logo-compact.txt"
assert_rendered "$AXOS_ROOTFS/etc/issue"
# The full official mark. 74 columns fits an 80-column terminal, and motd
# prints on its own with nothing alongside it.
render_with_ascii "$BRAND/etc/motd.in" "$AXOS_ROOTFS/etc/motd" \
    "@ASCII_FULL@" "$ASCII_DIR/axos-logo.txt"
assert_rendered "$AXOS_ROOTFS/etc/motd"

ok "identity: $(grep PRETTY_NAME "$AXOS_ROOTFS/usr/lib/os-release")"

# ---------------------------------------------------------------------------
# Raster assets derived from the master PNG
#
# One 2000x2000 source produces every raster the system needs. ImageMagick 7
# ships "magick"; 6 ships "convert". Both are handled, and the build degrades
# to a flat-colour splash rather than failing if neither is present.
# ---------------------------------------------------------------------------
log "generating raster assets from AXnobg.png"
install -dm0755 "$AXOS_ROOTFS/usr/share/andersxn/assets"
axos_install_file "$BRAND/assets/AXnobg.png" \
    "$AXOS_ROOTFS/usr/share/andersxn/assets/AXnobg.png"

IM=""
if command -v magick >/dev/null 2>&1; then IM="magick"
elif command -v convert >/dev/null 2>&1; then IM="convert"
fi

theme_dir="$AXOS_ROOTFS/usr/share/plymouth/themes/andersxn"
install -dm0755 "$theme_dir"

if [[ -n "$IM" ]]; then
    src="$BRAND/assets/AXnobg.png"

    # Plymouth mark: 512px, alpha preserved.
    "$IM" "$src" -resize 512x512 -strip PNG32:"$theme_dir/logo.png"

    # Bootloader splash: the mark composited onto the ink backdrop. Limine
    # draws this as a wallpaper and does not composite alpha itself.
    "$IM" -size 1920x1080 "xc:#$AXOS_HEX_INK" \
        \( "$src" -resize 560x560 \) -gravity center -composite \
        -strip "$AXOS_WORKDIR/splash.png"

    # Desktop wallpaper, same treatment at native panel sizes.
    install -dm0755 "$AXOS_ROOTFS/usr/share/backgrounds/andersxn"
    for res in 1920x1080 2560x1440 3840x2160; do
        "$IM" -size "$res" "xc:#$AXOS_HEX_INK" \
            \( "$src" -resize "$(( ${res%x*} / 4 ))x" \) -gravity center -composite \
            -strip "$AXOS_ROOTFS/usr/share/backgrounds/andersxn/axos-${res}.png"
    done
    ln -sf axos-1920x1080.png \
        "$AXOS_ROOTFS/usr/share/backgrounds/andersxn/default.png"

    # Plymouth progress bar: 1x3 slivers that the script scales horizontally.
    "$IM" -size 1x3 "xc:#$AXOS_HEX_SURFACE" -strip PNG32:"$theme_dir/bar-bg.png"
    "$IM" -size 1x3 "xc:#$AXOS_HEX_ACCENT"  -strip PNG32:"$theme_dir/bar-fg.png"

    ok "raster assets generated with $IM"
else
    warn "ImageMagick not found - installing the master PNG unscaled"
    warn "install imagemagick for correctly sized splash and wallpapers"
    cp "$BRAND/assets/AXnobg.png" "$theme_dir/logo.png"
    cp "$BRAND/assets/AXnobg.png" "$AXOS_WORKDIR/splash.png"
    # A 1x3 PNG cannot be synthesised without a raster tool; Plymouth treats a
    # missing bar image as a no-op, so the splash still renders.
fi

# ---------------------------------------------------------------------------
# Plymouth
# ---------------------------------------------------------------------------
log "installing Plymouth theme"
axos_install_file "$BRAND/plymouth/andersxn/andersxn.plymouth" \
    "$theme_dir/andersxn.plymouth"
axos_install_file "$BRAND/plymouth/andersxn/andersxn.script" \
    "$theme_dir/andersxn.script"

axos_chroot_sh "$AXOS_ROOTFS" <<'CHROOT'
if command -v plymouth-set-default-theme >/dev/null 2>&1; then
    plymouth-set-default-theme andersxn >/dev/null 2>&1 || true
fi
CHROOT

# update-alternatives is how Debian actually selects the theme; the helper above
# only rebuilds the initramfs hook.
install -Dm0644 /dev/stdin \
    "$AXOS_ROOTFS/etc/plymouth/plymouthd.conf" <<'PLY'
[Daemon]
Theme=andersxn
ShowDelay=0
DeviceTimeout=8
PLY

ok "Plymouth theme 'andersxn' set as default"

# ---------------------------------------------------------------------------
# Fastfetch
# ---------------------------------------------------------------------------
log "installing Fastfetch configuration"
axos_install_file "$BRAND/fastfetch/config.jsonc" \
    "$AXOS_ROOTFS/etc/fastfetch/config.jsonc"

# ---------------------------------------------------------------------------
# Shell skeleton
#
# Everything under branding/skel becomes /etc/skel, so every account the
# installer creates inherits the AndersXn environment. Root gets a copy too -
# a recovery shell should not look and behave differently from a normal login.
# ---------------------------------------------------------------------------
log "installing shell skeleton"
install -dm0755 "$AXOS_ROOTFS/etc/skel"
cp -a "$BRAND/skel/." "$AXOS_ROOTFS/etc/skel/"

install -dm0755 "$AXOS_ROOTFS/etc/skel/.config/fastfetch"
ln -sf /etc/fastfetch/config.jsonc \
    "$AXOS_ROOTFS/etc/skel/.config/fastfetch/config.jsonc"

cp -a "$BRAND/skel/.zshrc" "$BRAND/skel/.zprofile" "$AXOS_ROOTFS/root/"
install -dm0755 "$AXOS_ROOTFS/root/.config/fastfetch"
ln -sf /etc/fastfetch/config.jsonc \
    "$AXOS_ROOTFS/root/.config/fastfetch/config.jsonc"

# zsh becomes the default for accounts created from here on. Existing system
# accounts keep /bin/sh deliberately - daemons must not depend on an
# interactive shell being present or configured.
axos_chroot_sh "$AXOS_ROOTFS" <<'CHROOT'
if grep -q '^/usr/bin/zsh$' /etc/shells 2>/dev/null; then :; else
    echo /usr/bin/zsh >> /etc/shells
fi
sed -i 's|^SHELL=.*|SHELL=/usr/bin/zsh|' /etc/default/useradd 2>/dev/null || true
chsh -s /usr/bin/zsh root >/dev/null 2>&1 || true
CHROOT

# ---------------------------------------------------------------------------
# Dynamic motd
# ---------------------------------------------------------------------------
log "installing dynamic motd"
install -dm0755 "$AXOS_ROOTFS/etc/update-motd.d"
axos_install_file "$BRAND/etc/update-motd.d/20-andersxn-status" \
    "$AXOS_ROOTFS/etc/update-motd.d/20-andersxn-status" 0755

# pam_motd reads /run/motd.dynamic; this unit regenerates it at boot and hourly
# so the login banner is never stale about disk pressure or container counts.
install -Dm0644 /dev/stdin \
    "$AXOS_ROOTFS/etc/systemd/system/axos-motd.service" <<'UNIT'
[Unit]
Description=Generate the AndersXn dynamic message of the day
After=local-fs.target
Documentation=man:update-motd(5)

[Service]
Type=oneshot
ExecStart=/usr/lib/andersxn/axos-motd
UNIT

install -Dm0644 /dev/stdin \
    "$AXOS_ROOTFS/etc/systemd/system/axos-motd.timer" <<'UNIT'
[Unit]
Description=Refresh the AndersXn message of the day hourly

[Timer]
OnBootSec=30s
OnUnitActiveSec=1h
AccuracySec=1m

[Install]
WantedBy=timers.target
UNIT

install -Dm0755 /dev/stdin "$AXOS_ROOTFS/usr/lib/andersxn/axos-motd" <<'GEN'
#!/bin/sh
# Concatenate every executable /etc/update-motd.d fragment into the file
# pam_motd displays. Run by axos-motd.service.
set -eu
out=/run/motd.dynamic
tmp="$(mktemp "${out}.XXXXXX")"
for f in /etc/update-motd.d/*; do
    [ -x "$f" ] || continue
    "$f" >> "$tmp" 2>/dev/null || true
done
chmod 0644 "$tmp"
mv "$tmp" "$out"
GEN

axos_chroot "$AXOS_ROOTFS" systemctl enable axos-motd.timer >/dev/null 2>&1 || true

# ---------------------------------------------------------------------------
# Pre-coloured ASCII mark for Fastfetch
#
# Fastfetch's "file" logo type treats $ as a colour-placeholder prefix and eats
# characters - and the AndersXn mark is almost entirely $, so it renders as
# shredded nonsense. "file-raw" prints the file byte for byte, but then applies
# no colour of its own, so the accent colour is baked in here instead.
# ---------------------------------------------------------------------------
log "generating the coloured Fastfetch mark"
ansi_logo="$AXOS_ROOTFS/usr/share/andersxn/ascii/axos-logo-compact.ansi"
{
    while IFS= read -r line; do
        printf '\033[38;2;79;195;247m%s\033[0m\n' "$line"
    done < "$ASCII_DIR/axos-logo-compact.txt"
} > "$ansi_logo"
chmod 0644 "$ansi_logo"
ok "coloured mark written ($(wc -l < "$ansi_logo") lines)"

# ---------------------------------------------------------------------------
# KDE Plasma theming
#
# System-wide defaults so the live session, the greeter and every account the
# installer creates all inherit the AndersXn look with no per-user setup.
# ---------------------------------------------------------------------------
log "theming KDE Plasma"

# Breeze Dark plus the AndersXn accent. Plasma 6 reads AccentColor from here.
install -Dm0644 /dev/stdin "$AXOS_ROOTFS/etc/xdg/kdeglobals" <<'KDE'
[General]
ColorScheme=BreezeDark
AccentColor=79,195,247
accentColorFromWallpaper=false
font=Noto Sans,10,-1,5,50,0,0,0,0,0
fixed=JetBrains Mono,10,-1,5,50,0,0,0,0,0
menuFont=Noto Sans,10,-1,5,50,0,0,0,0,0
smallestReadableFont=Noto Sans,8,-1,5,50,0,0,0,0,0
toolBarFont=Noto Sans,10,-1,5,50,0,0,0,0,0

[Icons]
Theme=breeze-dark

[KDE]
LookAndFeelPackage=org.kde.breezedark.desktop
widgetStyle=Breeze

[WM]
activeFont=Noto Sans,10,-1,5,75,0,0,0,0,0
KDE

# The desktop wallpaper for every account created from /etc/skel, which is
# every account AX-Installer makes.
install -Dm0644 /dev/stdin \
    "$AXOS_ROOTFS/etc/skel/.config/plasma-org.kde.plasma.desktop-appletsrc" <<'PLASMA'
[Containments][1]
ConfigVersion=1
activityId=
formfactor=0
immutability=1
lastScreen=0
location=0
plugin=org.kde.plasma.folder
wallpaperplugin=org.kde.image

[Containments][1][Wallpaper][org.kde.image][General]
Image=/usr/share/backgrounds/andersxn/default.png
SlidePaths=/usr/share/backgrounds/andersxn/
FillMode=2
PLASMA

# The SDDM greeter background. Breeze reads theme.conf.user in preference to
# theme.conf, so the packaged theme stays untouched and survives its updates.
install -dm0755 "$AXOS_ROOTFS/usr/share/sddm/themes/breeze"
install -Dm0644 /dev/stdin \
    "$AXOS_ROOTFS/usr/share/sddm/themes/breeze/theme.conf.user" <<'SDDMT'
[General]
type=image
background=/usr/share/backgrounds/andersxn/default.png
SDDMT

ok "Plasma defaults written"

ok "stage 30 complete"