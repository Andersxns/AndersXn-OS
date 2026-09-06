#!/usr/bin/env bash
# shellcheck shell=bash
#
# AndersXn OS - shared build library.
#
# Sourced by build.sh and every stage script. Provides the brand palette,
# logging, root/dependency checks, architecture mapping, and the mount
# bookkeeping that keeps a failed build from leaving /proc bind-mounted inside
# a rootfs directory.

# ---------------------------------------------------------------------------
# Brand palette
#
# Derived from the AndersXn mark: a snow-white alpine glyph over deep space.
# Only the mark itself was supplied as an asset, so the accent tones below are
# the single place to retune the whole system colour identity - bootloader,
# Plymouth, installer TUI and shell prompt all read from these values.
# ---------------------------------------------------------------------------
AXOS_HEX_INK="0B0E14"         # base background, near-black navy
AXOS_HEX_SURFACE="131722"     # raised surfaces, panels
AXOS_HEX_SNOW="F2F5FA"        # the logo white, primary foreground
AXOS_HEX_ACCENT="4FC3F7"      # glacier cyan, primary accent
AXOS_HEX_ACCENT_DEEP="1B6CA8" # pressed / unfocused accent
AXOS_HEX_MUTED="6B7A90"       # secondary text, hints
AXOS_HEX_OK="63D68A"
AXOS_HEX_WARN="E8B84B"
AXOS_HEX_ERR="E5484D"
export AXOS_HEX_INK AXOS_HEX_SURFACE AXOS_HEX_SNOW AXOS_HEX_ACCENT \
       AXOS_HEX_ACCENT_DEEP AXOS_HEX_MUTED AXOS_HEX_OK AXOS_HEX_WARN AXOS_HEX_ERR

if [[ -t 2 ]] && [[ -z "${NO_COLOR:-}" ]]; then
    C_RESET=$'\033[0m'
    C_ACCENT=$'\033[38;2;79;195;247m'
    C_SNOW=$'\033[38;2;242;245;250m'
    C_MUTED=$'\033[38;2;107;122;144m'
    C_OK=$'\033[38;2;99;214;138m'
    C_WARN=$'\033[38;2;232;184;75m'
    C_ERR=$'\033[38;2;229;72;77m'
    C_BOLD=$'\033[1m'
else
    C_RESET='' C_ACCENT='' C_SNOW='' C_MUTED='' C_OK='' C_WARN='' C_ERR='' C_BOLD=''
fi

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------
_axos_stamp() { date '+%H:%M:%S'; }

log()  { printf '%s %s::%s %s\n'  "${C_MUTED}$(_axos_stamp)${C_RESET}" "$C_ACCENT" "$C_RESET" "$*" >&2; }
info() { printf '%s %s->%s %s\n'  "${C_MUTED}$(_axos_stamp)${C_RESET}" "$C_SNOW"   "$C_RESET" "$*" >&2; }
ok()   { printf '%s %sok%s %s\n'  "${C_MUTED}$(_axos_stamp)${C_RESET}" "$C_OK"     "$C_RESET" "$*" >&2; }
warn() { printf '%s %s!!%s %s\n'  "${C_MUTED}$(_axos_stamp)${C_RESET}" "$C_WARN"   "$C_RESET" "$*" >&2; }
die()  { printf '%s %sXX%s %s\n'  "${C_MUTED}$(_axos_stamp)${C_RESET}" "$C_ERR"    "$C_RESET" "$*" >&2; exit 1; }

stage_banner() {
    local title="$*"
    local rule
    rule="$(printf -- '-%.0s' $(seq 1 $(( 62 - ${#title} > 3 ? 62 - ${#title} : 3 ))))"
    printf '\n%s%s+- %s %s%s\n' "$C_ACCENT" "$C_BOLD" "$title" "$rule" "$C_RESET" >&2
}

# Print the brand mark. Variant: full | compact | mini.
axos_print_logo() {
    local variant="${1:-compact}"
    local file="$AXOS_ROOT/branding/ascii/axos-logo-${variant}.txt"
    [[ "$variant" == "full" ]] && file="$AXOS_ROOT/branding/ascii/axos-logo.txt"
    [[ -r "$file" ]] || return 0
    printf '%s' "$C_ACCENT"
    cat "$file"
    printf '%s' "$C_RESET"
}

# ---------------------------------------------------------------------------
# Guards
# ---------------------------------------------------------------------------
require_root() {
    [[ ${EUID:-$(id -u)} -eq 0 ]] || die "this stage must run as root (use: sudo $0)"
}

need_cmd() {
    local missing=() c
    for c in "$@"; do
        command -v "$c" >/dev/null 2>&1 || missing+=("$c")
    done
    if (( ${#missing[@]} )); then
        die "missing required commands: ${missing[*]} (see build/stages/00-check-deps.sh)"
    fi
}

# ---------------------------------------------------------------------------
# Architecture mapping
# ---------------------------------------------------------------------------
# Debian arch -> kernel/uname arch
axos_uname_arch() {
    case "$1" in
        amd64) echo "x86_64"  ;;
        arm64) echo "aarch64" ;;
        *) die "unsupported architecture: $1 (expected amd64 or arm64)" ;;
    esac
}

# Debian arch -> EFI removable-media fallback filename
axos_efi_stub() {
    case "$1" in
        amd64) echo "BOOTX64.EFI"  ;;
        arm64) echo "BOOTAA64.EFI" ;;
        *) die "unsupported architecture: $1" ;;
    esac
}

# Debian arch -> the machine string file(1) prints for that ELF.
# Used to refuse shipping a binary the target architecture cannot execute.
axos_elf_machine_name() {
    case "$1" in
        amd64) echo "x86-64"  ;;
        arm64) echo "aarch64" ;;
        *) die "unsupported architecture: $1" ;;
    esac
}

# Debian arch -> the qemu-user binary needed for foreign-arch chroots.
#
# Naming differs by distribution, and both must be statically linked to work
# inside a chroot at all:
#   Debian / older Ubuntu : qemu-x86_64-static   (package qemu-user-static)
#   Ubuntu 26.04+         : qemu-x86_64          (package qemu-user, static-pie)
#
# Prints the resolved absolute path, or nothing when neither is installed.
axos_qemu_binary() {
    local uarch cand
    uarch="$(axos_uname_arch "$1")"
    for cand in "qemu-${uarch}-static" "qemu-${uarch}"; do
        if command -v "$cand" >/dev/null 2>&1; then
            command -v "$cand"
            return 0
        fi
    done
    return 1
}

# True when the given binary is statically linked. A dynamically linked qemu
# cannot run inside a foreign-architecture chroot: the kernel holds the
# interpreter open via the binfmt F flag, but its shared libraries would still
# have to be resolved from inside the chroot, where they do not exist.
axos_is_static() {
    local bin="$1"
    [[ -x "$bin" ]] || return 1
    if command -v file >/dev/null 2>&1; then
        file -L "$bin" | grep -qE 'static-pie linked|statically linked' && return 0
    fi
    # No file(1): ldd prints this exact phrase for a static binary.
    ldd "$bin" 2>&1 | grep -q 'not a dynamic executable\|statically linked'
}

# True when the target architecture cannot run natively on this host.
axos_is_cross() {
    local want
    want="$(axos_uname_arch "$1")"
    [[ "$(uname -m)" != "$want" ]]
}

# ---------------------------------------------------------------------------
# Mount bookkeeping
#
# Every bind mount goes through axos_mount so axos_umount_all can tear the
# stack down in reverse from an EXIT trap. Without this, a build that dies
# midway leaves /proc, /sys and /dev bound inside the rootfs - and a later
# "rm -rf" on that directory becomes genuinely destructive.
# ---------------------------------------------------------------------------
declare -a _AXOS_MOUNTS=()

axos_mount() {
    local src="$1" dst="$2"
    shift 2
    mkdir -p "$dst"
    mount "$@" "$src" "$dst" || die "mount failed: $src -> $dst"
    _AXOS_MOUNTS+=("$dst")
}

axos_umount_all() {
    local i m
    for (( i=${#_AXOS_MOUNTS[@]}-1; i>=0; i-- )); do
        m="${_AXOS_MOUNTS[i]}"
        mountpoint -q "$m" 2>/dev/null || continue
        umount -R "$m" 2>/dev/null || umount -lf "$m" 2>/dev/null ||
            warn "could not unmount $m"
    done
    _AXOS_MOUNTS=()
}

# Bind the kernel API filesystems a chroot needs, registering them for cleanup.
axos_mount_pseudo() {
    local root="$1"
    axos_mount /proc     "$root/proc"    -t proc
    axos_mount /sys      "$root/sys"     -t sysfs
    axos_mount /dev      "$root/dev"     --bind
    axos_mount /dev/pts  "$root/dev/pts" --bind
    axos_mount tmpfs     "$root/run"     -t tmpfs

    # The tmpfs above hides the host's /run, which is where the installed
    # system's resolv.conf symlink points. Give the chroot a real one.
    axos_enable_chroot_dns "$root"
}

# ---------------------------------------------------------------------------
# chroot execution
#
# Runs a command inside the target rootfs with a sane, non-interactive
# environment. Foreign-architecture trees work through the qemu-user-static
# binfmt_misc handler verified in stage 00.
# ---------------------------------------------------------------------------
axos_chroot() {
    local root="$1"
    shift
    env -i \
        PATH=/usr/sbin:/usr/bin:/sbin:/bin \
        HOME=/root \
        TERM="${TERM:-xterm-256color}" \
        LC_ALL=C.UTF-8 \
        LANG=C.UTF-8 \
        DEBIAN_FRONTEND=noninteractive \
        DEBCONF_NONINTERACTIVE_SEEN=true \
        AXOS_VERSION="$AXOS_VERSION" \
        AXOS_CODENAME="$AXOS_CODENAME" \
        AXOS_ARCH="$AXOS_ARCH" \
        chroot "$root" "$@"
}

# Pipe a script on stdin into the target rootfs shell.
axos_chroot_sh() {
    axos_chroot "$1" /bin/bash -euo pipefail
}

# ---------------------------------------------------------------------------
# Misc helpers
# ---------------------------------------------------------------------------
axos_retry() {
    local tries="$1"
    shift
    local n=1
    until "$@"; do
        (( n >= tries )) && return 1
        warn "attempt $n/$tries failed - retrying"
        sleep $(( n * 2 ))
        (( n++ ))
    done
}

# Install a file from the branding/overlay tree, creating parents as needed.
axos_install_file() {
    local src="$1" dst="$2" mode="${3:-0644}"
    [[ -r "$src" ]] || die "missing source file: $src"
    install -Dm"$mode" "$src" "$dst"
}

# Substitute @TOKEN@ placeholders from build.conf into a template.
axos_render() {
    local src="$1" dst="$2"
    [[ -r "$src" ]] || die "missing template: $src"
    mkdir -p "$(dirname "$dst")"
    sed -e "s|@NAME@|$AXOS_NAME|g" \
        -e "s|@ID@|$AXOS_ID|g" \
        -e "s|@VERSION@|$AXOS_VERSION|g" \
        -e "s|@CODENAME@|$AXOS_CODENAME|g" \
        -e "s|@ARCH@|$AXOS_ARCH|g" \
        -e "s|@HOME_URL@|$AXOS_HOME_URL|g" \
        -e "s|@SUPPORT_URL@|$AXOS_SUPPORT_URL|g" \
        -e "s|@BUG_URL@|$AXOS_BUG_URL|g" \
        -e "s|@VENDOR@|$AXOS_VENDOR|g" \
        -e "s|@ACCENT@|$AXOS_HEX_ACCENT|g" \
        -e "s|@INK@|$AXOS_HEX_INK|g" \
        -e "s|@SNOW@|$AXOS_HEX_SNOW|g" \
        "$src" > "$dst"
}

# ---------------------------------------------------------------------------
# DNS inside the chroot
#
# Stage 20 points /etc/resolv.conf at ../run/systemd/resolve/stub-resolv.conf,
# which is correct for the INSTALLED system. It is useless during the build:
# axos_mount_pseudo mounts a fresh tmpfs on /run, so that symlink dangles and
# every later chroot stage loses name resolution - apt then fails with
# "Temporary failure resolving deb.debian.org" several hundred packages in.
#
# These two helpers keep a real resolv.conf in place while building and put the
# symlink back before the image is sealed.
# ---------------------------------------------------------------------------

# Upstream nameservers to hand the chroot. The host's own /etc/resolv.conf is
# usually the systemd-resolved stub on 127.0.0.53, which is unreachable from
# inside the chroot, so the real upstream list is preferred.
axos_host_nameservers() {
    local ns=""
    if [[ -r /run/systemd/resolve/resolv.conf ]]; then
        ns="$(awk '/^nameserver/ {print $2}' /run/systemd/resolve/resolv.conf)"
    fi
    if [[ -z "$ns" && -f /etc/resolv.conf ]]; then
        ns="$(awk '/^nameserver/ && $2 !~ /^127\./ {print $2}' /etc/resolv.conf)"
    fi
    [[ -n "$ns" ]] || ns=$'1.1.1.1\n9.9.9.9'
    printf '%s\n' "$ns"
}

axos_enable_chroot_dns() {
    local root="$1"
    local rc="$root/etc/resolv.conf"

    # Remember what was there so it can be restored verbatim.
    if [[ -L "$rc" ]] && [[ ! -e "$root/etc/.axos-resolv-was-symlink" ]]; then
        readlink "$rc" > "$root/etc/.axos-resolv-was-symlink"
    fi

    rm -f "$rc"
    {
        echo "# Temporary, build-time only. Restored by axos_finalize_resolv."
        while read -r server; do
            [[ -n "$server" ]] && echo "nameserver $server"
        done < <(axos_host_nameservers)
    } > "$rc"
    chmod 0644 "$rc"
}

axos_finalize_resolv() {
    local root="$1"
    local rc="$root/etc/resolv.conf"
    local marker="$root/etc/.axos-resolv-was-symlink"

    if [[ -r "$marker" ]]; then
        local target
        target="$(cat "$marker")"
        rm -f "$rc" "$marker"
        ln -sf "$target" "$rc"
        log "restored /etc/resolv.conf -> $target"
    fi
}

# The newest DEBIAN kernel in a rootfs, ignoring Raspberry Pi ones.
#
# On arm64 the image carries both: Debian's linux-image-arm64 for the UEFI and
# BIOS paths, and the Raspberry Pi Foundation kernels for the Pi. The Pi builds
# carry a higher version (6.18 against Debian's 6.12), so a plain
# "sort -V | tail -1" picks a Pi kernel - and Limine would then hand a generic
# arm64 UEFI machine a kernel built for a Pi. Everything Limine boots must come
# from here.
axos_debian_kernel() {
    local root="$1" k
    k="$(ls -1 "$root/lib/modules" 2>/dev/null | grep -v -- '+rpt' | sort -V | tail -1)"
    [[ -n "$k" ]] || k="$(ls -1 "$root/lib/modules" 2>/dev/null | sort -V | tail -1)"
    printf '%s\n' "$k"
}
