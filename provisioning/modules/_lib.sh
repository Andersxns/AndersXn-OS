# shellcheck shell=bash
#
# AndersXn OS - shared helpers for provisioning modules.
#
# Sourced by postinstall.sh and available to every module. Modules should use
# these rather than calling apt-get or systemctl directly, so that logging,
# retries and the non-interactive contract stay consistent.

AXOS_LOG="${AXOS_LOG:-/var/log/andersxn-provision.log}"

_ax_c_accent=$'\033[38;2;79;195;247m'
_ax_c_muted=$'\033[38;2;107;122;144m'
_ax_c_ok=$'\033[38;2;99;214;138m'
_ax_c_warn=$'\033[38;2;232;184;75m'
_ax_c_err=$'\033[38;2;229;72;77m'
_ax_c_reset=$'\033[0m'

_ax_log() {
    # Everything goes to the log file plain, and to stderr coloured. The
    # installer captures stderr into its log pane.
    printf '%s %s\n' "$(date -u +%H:%M:%S)" "$2" >> "$AXOS_LOG" 2>/dev/null || true
    printf '%s%s%s %s\n' "$1" "$3" "$_ax_c_reset" "$2" >&2
}

ax_info() { _ax_log "$_ax_c_muted"  "$*" "  "; }
ax_step() { _ax_log "$_ax_c_accent" "$*" "::"; }
ax_ok()   { _ax_log "$_ax_c_ok"     "$*" "ok"; }
ax_warn() { _ax_log "$_ax_c_warn"   "$*" "!!"; }
ax_die()  { _ax_log "$_ax_c_err"    "$*" "XX"; exit 1; }

# ax_apt installs packages without recommends and without prompting.
ax_apt() {
    ax_info "installing: $*"
    apt-get install -y --no-install-recommends "$@" ||
        ax_die "failed to install: $*"
}

# ax_enable enables a unit, tolerating one that is not present yet (a module
# may install a service whose unit only appears after a later step).
ax_enable() {
    for unit in "$@"; do
        systemctl enable "$unit" >/dev/null 2>&1 ||
            ax_warn "could not enable $unit"
    done
}

# ax_add_repo registers a third-party apt repository with a keyring, using the
# deb822 format Debian trixie prefers.
#
#   ax_add_repo docker https://download.docker.com/linux/debian \
#       https://download.docker.com/linux/debian/gpg stable
ax_add_repo() {
    local name="$1" uri="$2" keyurl="$3" component="${4:-main}"
    local keyring="/usr/share/keyrings/${name}.gpg"
    local suite
    suite="$(. /etc/os-release && echo "${VERSION_CODENAME:-trixie}")"

    # AndersXn's own VERSION_CODENAME is "Summit", not a Debian suite. Fall back
    # to the base suite recorded at build time.
    if [[ -r /etc/andersxn/build-manifest ]]; then
        local base
        base="$(sed -n 's|^AXOS_BASE=debian/||p' /etc/andersxn/build-manifest)"
        [[ -n "$base" ]] && suite="$base"
    fi

    ax_info "adding repository $name ($suite)"
    curl -fsSL --retry 3 --retry-delay 2 "$keyurl" | gpg --dearmor -o "$keyring" ||
        ax_die "could not fetch the signing key for $name"
    chmod 0644 "$keyring"

    cat > "/etc/apt/sources.list.d/${name}.sources" <<REPO
Types: deb
URIs: $uri
Suites: $suite
Components: $component
Architectures: $(dpkg --print-architecture)
Signed-By: $keyring
REPO

    apt-get update -qq -o Dir::Etc::sourcelist="sources.list.d/${name}.sources" \
        -o Dir::Etc::sourceparts="-" -o APT::Get::List-Cleanup="0" ||
        ax_warn "could not refresh the $name repository"
}

# ax_add_user_to_groups adds the operator account to groups that exist.
ax_add_user_to_groups() {
    local user="${AXOS_USER:-}"
    [[ -n "$user" ]] || return 0
    id "$user" >/dev/null 2>&1 || return 0
    for g in "$@"; do
        getent group "$g" >/dev/null 2>&1 || continue
        usermod -aG "$g" "$user" && ax_info "added $user to $g"
    done
}

# ax_compose_dir prepares a per-stack compose directory under /srv/andersxn.
ax_compose_dir() {
    local stack="$1"
    local dir="/srv/andersxn/compose/$stack"
    mkdir -p "$dir" "/srv/andersxn/data/$stack"
    printf '%s' "$dir"
}

# ax_arch prints the Debian architecture.
ax_arch() { dpkg --print-architecture; }

# ax_has_gpu reports whether a render node exists, i.e. whether hardware
# transcoding has any chance of working.
ax_has_gpu() { compgen -G "/dev/dri/renderD*" >/dev/null 2>&1; }
