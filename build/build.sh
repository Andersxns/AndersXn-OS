#!/usr/bin/env bash
#
# AndersXn OS - build orchestrator.
#
#   sudo ./build/build.sh                       # amd64 live ISO
#   sudo ./build/build.sh --arch arm64          # arm64 raw appliance image
#   sudo ./build/build.sh --from 30             # resume from the branding stage
#   sudo ./build/build.sh --only 60             # run a single stage
#   sudo ./build/build.sh --clean               # discard work/ for this arch
#
# Stages live in build/stages and are plain scripts, each sourcing the same
# config and library. Any of them can be run standalone for debugging.

set -euo pipefail

AXOS_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
export AXOS_ROOT

# shellcheck source=lib/common.sh
source "$AXOS_ROOT/build/lib/common.sh"

FROM_STAGE=0
ONLY_STAGE=""
DO_CLEAN=0

usage() {
    sed -n '2,14p' "${BASH_SOURCE[0]}" | sed 's/^#\{0,1\} \{0,1\}//'
    cat <<'USAGE'

Options:
  --arch {amd64|arm64}   Target architecture             (default: amd64)
  --bootloader {limine|systemd-boot}
                         Bootloader to install           (default: limine)
  --suite NAME           Debian suite to bootstrap       (default: trixie)
  --from N               Resume at stage number N
  --only N               Run only stage number N
  --clean                Remove work/<arch> before building
  --no-compress          Leave release artifacts uncompressed
  -h, --help             Show this help
USAGE
}

while (( $# )); do
    case "$1" in
        --arch)        AXOS_ARCH="$2"; shift 2 ;;
        --bootloader)  AXOS_BOOTLOADER="$2"; shift 2 ;;
        --suite)       AXOS_SUITE="$2"; shift 2 ;;
        --from)        FROM_STAGE="$2"; shift 2 ;;
        --only)        ONLY_STAGE="$2"; shift 2 ;;
        --clean)       DO_CLEAN=1; shift ;;
        --no-compress) AXOS_COMPRESS=0; shift ;;
        -h|--help)     usage; exit 0 ;;
        *) die "unknown option: $1 (try --help)" ;;
    esac
done

export AXOS_ARCH AXOS_BOOTLOADER AXOS_SUITE AXOS_COMPRESS

# shellcheck source=config/build.conf
source "$AXOS_ROOT/build/config/build.conf"
export AXOS_NAME AXOS_ID AXOS_VERSION AXOS_CODENAME AXOS_HOME_URL \
       AXOS_SUPPORT_URL AXOS_BUG_URL AXOS_VENDOR AXOS_MIRROR \
       AXOS_SECURITY_MIRROR AXOS_COMPONENTS AXOS_KERNEL_FLAVOUR \
       AXOS_LIMINE_VERSION AXOS_LIMINE_REPO AXOS_DEFAULT_FS AXOS_ESP_SIZE_MIB \
       AXOS_BTRFS_OPTS AXOS_LOCALE AXOS_TIMEZONE AXOS_KEYMAP AXOS_HOSTNAME \
       AXOS_PKGS_BASE AXOS_PKGS_SYSTEM AXOS_PKGS_LIVE AXOS_WORKDIR \
       AXOS_PKGS_DESKTOP AXOS_PKGS_INSTALLER_GUI AXOS_LIVE_USER AXOS_KEYRING AXOS_PKGS_ARM_FIRMWARE AXOS_PI_MODELS AXOS_PKGS_VIDEO_AMD64 AXOS_PKGS_VIDEO_ARM64 AXOS_RPI_REPO AXOS_RPI_SUITE AXOS_RPI_KEY_URL AXOS_PKGS_PI_KERNEL \
       AXOS_ROOTFS AXOS_DISTDIR AXOS_IMAGE_BASENAME AXOS_RAW_IMAGE_MIB

case "$AXOS_ARCH" in amd64|arm64) ;; *) die "unsupported --arch: $AXOS_ARCH" ;; esac
case "$AXOS_BOOTLOADER" in
    limine|systemd-boot) ;;
    grub*) die "GRUB is not supported by AndersXn OS. Use limine or systemd-boot." ;;
    *) die "unsupported --bootloader: $AXOS_BOOTLOADER" ;;
esac

trap 'axos_umount_all' EXIT INT TERM

axos_print_logo compact
printf '\n  %s%s%s %s  %s(%s)%s\n' \
    "$C_BOLD$C_SNOW" "$AXOS_NAME" "$C_RESET" "$AXOS_VERSION" \
    "$C_MUTED" "$AXOS_CODENAME" "$C_RESET" >&2
printf '  %sarch%s %-8s %sbase%s %-10s %sboot%s %s\n\n' \
    "$C_MUTED" "$C_RESET" "$AXOS_ARCH" \
    "$C_MUTED" "$C_RESET" "debian/$AXOS_SUITE" \
    "$C_MUTED" "$C_RESET" "$AXOS_BOOTLOADER" >&2

if (( DO_CLEAN )); then
    require_root
    log "removing $AXOS_WORKDIR"
    axos_umount_all
    rm -rf -- "$AXOS_WORKDIR"
fi

mkdir -p "$AXOS_WORKDIR" "$AXOS_DISTDIR"

mapfile -t STAGES < <(find "$AXOS_ROOT/build/stages" -maxdepth 1 -name '[0-9][0-9]-*.sh' | sort)
(( ${#STAGES[@]} )) || die "no stage scripts found in build/stages"

start_ts=$SECONDS
for stage in "${STAGES[@]}"; do
    name="$(basename "$stage")"
    num="${name%%-*}"
    num="${num#0}"; num="${num:-0}"

    if [[ -n "$ONLY_STAGE" ]]; then
        (( num == ONLY_STAGE )) || continue
    elif (( num < FROM_STAGE )); then
        info "skip  $name"
        continue
    fi

    stage_banner "$name"
    bash "$stage" || die "stage failed: $name"
    axos_umount_all
done

ok "build finished in $(( SECONDS - start_ts ))s"
[[ -d "$AXOS_DISTDIR" ]] && ls -lh "$AXOS_DISTDIR" >&2 || true
