# AndersXn OS - default interactive zsh configuration.
#
# Deliberately framework-free. Oh-My-Zsh and friends add ~200ms to every shell
# start and a moving upstream dependency to every node in a fleet; everything
# below is stock zsh and loads in single-digit milliseconds.
#
# Installed to /etc/skel/.zshrc. Personal overrides go in ~/.zshrc.local,
# which is sourced last and never touched by system updates.

[[ -o interactive ]] || return

# ---------------------------------------------------------------------------
# Brand palette (tracks build/lib/common.sh)
# ---------------------------------------------------------------------------
typeset -g AXOS_ACCENT='#4FC3F7'
typeset -g AXOS_SNOW='#F2F5FA'
typeset -g AXOS_MUTED='#6B7A90'
typeset -g AXOS_OK='#63D68A'
typeset -g AXOS_ERR='#E5484D'
typeset -g AXOS_WARN='#E8B84B'

# ---------------------------------------------------------------------------
# History
# ---------------------------------------------------------------------------
HISTFILE="${HOME}/.zsh_history"
HISTSIZE=50000
SAVEHIST=50000

setopt SHARE_HISTORY HIST_IGNORE_DUPS HIST_IGNORE_ALL_DUPS HIST_IGNORE_SPACE
setopt HIST_REDUCE_BLANKS HIST_VERIFY EXTENDED_HISTORY INC_APPEND_HISTORY
setopt AUTO_CD AUTO_PUSHD PUSHD_IGNORE_DUPS INTERACTIVE_COMMENTS
setopt NO_BEEP PROMPT_SUBST

# ---------------------------------------------------------------------------
# Completion
# ---------------------------------------------------------------------------
autoload -Uz compinit
# Rebuild the completion dump at most once a day; a full compinit on every
# shell is the single biggest zsh startup cost on a busy box.
_axos_zcompdump="${HOME}/.zcompdump"
if [[ -n ${_axos_zcompdump}(#qN.mh+24) ]]; then
    compinit -d "${_axos_zcompdump}"
else
    compinit -C -d "${_axos_zcompdump}"
fi
unset _axos_zcompdump

zstyle ':completion:*' menu select
zstyle ':completion:*' matcher-list 'm:{a-zA-Z}={A-Za-z}' 'r:|=*' 'l:|=* r:|=*'
zstyle ':completion:*' list-colors "${(s.:.)LS_COLORS}"
zstyle ':completion:*:descriptions' format "%F{${AXOS_MUTED}}%d%f"
zstyle ':completion:*' group-name ''
zstyle ':completion:*' use-cache on
zstyle ':completion:*' cache-path "${HOME}/.cache/zsh"

# ---------------------------------------------------------------------------
# Keybindings - emacs mode with sane history search
# ---------------------------------------------------------------------------
bindkey -e
autoload -Uz up-line-or-beginning-search down-line-or-beginning-search
zle -N up-line-or-beginning-search
zle -N down-line-or-beginning-search
bindkey '^[[A' up-line-or-beginning-search
bindkey '^[[B' down-line-or-beginning-search
bindkey '^[[1;5C' forward-word
bindkey '^[[1;5D' backward-word
bindkey '^[[3~' delete-char
bindkey '^[[H'  beginning-of-line
bindkey '^[[F'  end-of-line

# ---------------------------------------------------------------------------
# Prompt - the AndersXn theme
#
#   ^ ~/srv/andersxn/compose  main*
#   >                                                            exit 1   1.2s
#
# The mark glyph echoes the apex of the logo. The left prompt stays short so
# long paths never push commands off-screen; timing and exit status sit on the
# right and disappear when there is nothing worth saying.
# ---------------------------------------------------------------------------
autoload -Uz vcs_info
zstyle ':vcs_info:*' enable git
zstyle ':vcs_info:git:*' check-for-changes true
zstyle ':vcs_info:git:*' unstagedstr '*'
zstyle ':vcs_info:git:*' stagedstr '+'
zstyle ':vcs_info:git:*' formats       ' %F{'"${AXOS_MUTED}"'}%b%u%c%f'
zstyle ':vcs_info:git:*' actionformats ' %F{'"${AXOS_WARN}"'}%b|%a%u%c%f'

zmodload -F zsh/datetime p:EPOCHREALTIME
autoload -Uz add-zsh-hook

typeset -g _axos_cmd_start=0
typeset -g _axos_cmd_elapsed=''

_axos_preexec() { _axos_cmd_start=$EPOCHREALTIME }

_axos_precmd() {
    if (( _axos_cmd_start > 0 )); then
        local -F delta=$(( EPOCHREALTIME - _axos_cmd_start ))
        # Only surface durations worth noticing.
        if (( delta >= 60 )); then
            _axos_cmd_elapsed=$(printf '%dm%02ds' $(( delta / 60 )) $(( delta % 60 )))
        elif (( delta >= 2 )); then
            _axos_cmd_elapsed=$(printf '%.1fs' $delta)
        else
            _axos_cmd_elapsed=''
        fi
        _axos_cmd_start=0
    fi
    vcs_info
}

add-zsh-hook preexec _axos_preexec
add-zsh-hook precmd  _axos_precmd

# Root and SSH sessions get a visually distinct mark - it is worth knowing at a
# glance that you are about to run something as root on a remote node.
if (( EUID == 0 )); then
    _axos_mark="%F{${AXOS_ERR}}^%f"
elif [[ -n ${SSH_CONNECTION:-} ]]; then
    _axos_mark="%F{${AXOS_WARN}}^%f"
else
    _axos_mark="%F{${AXOS_ACCENT}}^%f"
fi

PROMPT='${_axos_mark} %F{'"${AXOS_SNOW}"'}%~%f${vcs_info_msg_0_}
%(?.%F{'"${AXOS_ACCENT}"'}.%F{'"${AXOS_ERR}"'})>%f '

RPROMPT='%(?..%F{'"${AXOS_ERR}"'}exit %?%f )%F{'"${AXOS_MUTED}"'}${_axos_cmd_elapsed}%f'

# ---------------------------------------------------------------------------
# Aliases
# ---------------------------------------------------------------------------
alias ls='ls --color=auto --group-directories-first'
alias ll='ls -lh'
alias la='ls -lAh'
alias grep='grep --color=auto'
alias df='df -h'
alias du='du -h'
alias ip='ip -color=auto'
alias vim='nvim'
alias vi='nvim'

# systemd shorthands - the commands actually typed on a homelab box.
alias sc='systemctl'
alias scu='systemctl --user'
alias jc='journalctl'
alias jcf='journalctl -f'
alias jce='journalctl -p err -b'

# Container shorthands. Defined whether or not Docker is provisioned, so the
# muscle memory stays identical across every node in the fleet.
alias dps='docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"'
alias dcu='docker compose up -d'
alias dcd='docker compose down'
alias dcl='docker compose logs -f'
alias dcp='cd /srv/andersxn/compose'

alias axos='fastfetch'

# ---------------------------------------------------------------------------
# Environment
# ---------------------------------------------------------------------------
export EDITOR=nvim
export VISUAL=nvim
export PAGER=less
export LESS='-R -F -X -i'
export SYSTEMD_LESS="$LESS"

# ---------------------------------------------------------------------------
# Optional plugins, if the operator installed them. Never a hard dependency.
# ---------------------------------------------------------------------------
ZSH_AUTOSUGGEST_HIGHLIGHT_STYLE="fg=${AXOS_MUTED}"

for _axos_plugin in \
    /usr/share/zsh-autosuggestions/zsh-autosuggestions.zsh \
    /usr/share/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh
do
    [[ -r $_axos_plugin ]] && source $_axos_plugin
done
unset _axos_plugin

# ---------------------------------------------------------------------------
# Local overrides - survives system updates
# ---------------------------------------------------------------------------
# NOTE the if/fi rather than "[[ ... ]] && source". A trailing && whose
# condition is false leaves $? = 1, and since this is the LAST statement in
# the file, every new shell opened its first prompt already reporting
# "exit 1". An if with a false condition returns 0.
if [[ -r "${HOME}/.zshrc.local" ]]; then
    source "${HOME}/.zshrc.local"
fi

# Belt and braces: whatever ran last above, a freshly started shell should
# never show a non-zero status before the operator has run anything.
true
