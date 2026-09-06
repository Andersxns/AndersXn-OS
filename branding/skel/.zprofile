# AndersXn OS - login shell profile.

typeset -U path PATH
path=(
    "$HOME/.local/bin"
    /usr/local/sbin /usr/local/bin
    /usr/sbin /usr/bin /sbin /bin
    $path
)
export PATH

export XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}"
export XDG_DATA_HOME="${XDG_DATA_HOME:-$HOME/.local/share}"
export XDG_CACHE_HOME="${XDG_CACHE_HOME:-$HOME/.cache}"
export XDG_STATE_HOME="${XDG_STATE_HOME:-$HOME/.local/state}"

# Show the system fetch on a real login session. Never on a non-interactive or
# forced-command invocation: writing to stdout there breaks scp and rsync.
if [[ -o interactive ]] && [[ -t 1 ]] && [[ -z "${SSH_ORIGINAL_COMMAND:-}" ]]; then
    if command -v fastfetch >/dev/null 2>&1; then
        fastfetch
    fi
fi

# Never leave a non-zero status behind for the first prompt to report.
true
