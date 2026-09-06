#!/usr/bin/env bash
# AndersXn OS module: developer toolchain.
#
# The base image already carries zsh, neovim, git, btop and fastfetch. This
# module adds the configuration and the shell plugins that make them the
# AndersXn environment rather than stock packages.
set -euo pipefail
source "${AXOS_MODULE_DIR:-/usr/lib/andersxn/modules}/_lib.sh"

ax_apt zsh-autosuggestions zsh-syntax-highlighting fzf ripgrep fd-find tree

# --- git ---------------------------------------------------------------------
install -Dm0644 /dev/stdin /etc/gitconfig <<'GIT'
# AndersXn OS system-wide git defaults. Per-user settings in ~/.gitconfig win.
[init]
    defaultBranch = main
[pull]
    rebase = true
[fetch]
    prune = true
[diff]
    algorithm = histogram
    colorMoved = default
[core]
    editor = nvim
[color]
    ui = auto
GIT

# --- neovim ------------------------------------------------------------------
# A deliberately small config: sane defaults and the AndersXn palette, with LSP
# scaffolding left commented. A homelab node is not the place to auto-install a
# plugin manager that phones home on first launch.
install -Dm0644 /dev/stdin /etc/xdg/nvim/init.lua <<'NVIM'
-- AndersXn OS default Neovim configuration.
-- Users override by creating ~/.config/nvim/init.lua.

vim.opt.number         = true
vim.opt.relativenumber = true
vim.opt.expandtab      = true
vim.opt.shiftwidth     = 4
vim.opt.tabstop        = 4
vim.opt.smartindent    = true
vim.opt.ignorecase     = true
vim.opt.smartcase      = true
vim.opt.termguicolors  = true
vim.opt.undofile       = true
vim.opt.signcolumn     = "yes"
vim.opt.updatetime     = 250
vim.opt.scrolloff      = 6
vim.opt.clipboard      = "unnamedplus"

vim.g.mapleader = " "

-- AndersXn palette
vim.cmd([[
  highlight Normal       guibg=#0B0E14 guifg=#F2F5FA
  highlight LineNr       guifg=#6B7A90
  highlight CursorLineNr guifg=#4FC3F7 gui=bold
  highlight Comment      guifg=#6B7A90 gui=italic
  highlight Visual       guibg=#1B6CA8
  highlight StatusLine   guibg=#131722 guifg=#4FC3F7
]])

vim.keymap.set("n", "<leader>w", "<cmd>write<cr>",  { desc = "write" })
vim.keymap.set("n", "<leader>q", "<cmd>quit<cr>",   { desc = "quit" })
vim.keymap.set("n", "<esc>",     "<cmd>nohlsearch<cr>")

-- LSP: uncomment once you have installed the servers you want.
-- vim.lsp.enable({ "gopls", "pyright", "bashls" })
NVIM

# --- btop --------------------------------------------------------------------
install -d /usr/share/btop/themes
install -Dm0644 /dev/stdin /usr/share/btop/themes/andersxn.theme <<'BTOP'
# AndersXn OS btop theme.
theme[main_bg]="#0B0E14"
theme[main_fg]="#F2F5FA"
theme[title]="#F2F5FA"
theme[hi_fg]="#4FC3F7"
theme[selected_bg]="#131722"
theme[selected_fg]="#4FC3F7"
theme[inactive_fg]="#6B7A90"
theme[graph_text]="#6B7A90"
theme[proc_misc]="#4FC3F7"
theme[cpu_box]="#1B6CA8"
theme[mem_box]="#1B6CA8"
theme[net_box]="#1B6CA8"
theme[proc_box]="#1B6CA8"
theme[div_line]="#131722"
theme[temp_start]="#63D68A"
theme[temp_mid]="#E8B84B"
theme[temp_end]="#E5484D"
theme[cpu_start]="#63D68A"
theme[cpu_mid]="#4FC3F7"
theme[cpu_end]="#E5484D"
theme[free_start]="#1B6CA8"
theme[free_mid]="#4FC3F7"
theme[free_end]="#63D68A"
theme[available_start]="#4FC3F7"
theme[available_mid]="#4FC3F7"
theme[available_end]="#F2F5FA"
theme[used_start]="#63D68A"
theme[used_mid]="#E8B84B"
theme[used_end]="#E5484D"
BTOP

install -Dm0644 /dev/stdin /etc/xdg/btop/btop.conf <<'BTOPCONF'
color_theme = "andersxn"
theme_background = False
vim_keys = True
update_ms = 1500
proc_sorting = "cpu lazy"
proc_tree = True
BTOPCONF

ax_ok "developer toolchain configured"
