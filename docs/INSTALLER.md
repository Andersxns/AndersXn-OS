# AX-Installer

Two front ends over one deliberately UI-agnostic install engine.

```
gui/ax-installer-gtk   the GTK4 front end (Python/PyGObject) - the primary UI
cmd/ax-installer       the engine binary; also carries the Bubble Tea TUI
cmd/ax-installer-gui   launcher: picks the GTK front end, falls back to the TUI
internal/api           the JSON protocol between the two
internal/tui           the text wizard, for headless and serial installs
internal/install       the engine: plan and steps, no UI code at all
internal/sys           block devices, firmware, command execution
internal/provision     the module catalog
internal/branding      embedded marks and colour tokens
```

## The two halves, and why they are separate processes

`internal/install` takes a `Plan` and a `sys.Runner` and nothing else. It has no
terminal code and no toolkit code in it. Front ends do not import it - they
drive it over a newline-delimited JSON protocol:

```bash
ax-installer --probe                 # one Probe object: disks, firmware, catalog
ax-installer --run-plan plan.json    # a stream of Event objects: progress, log, done
```

Secrets never appear in the plan file or in argv: `--run-plan` reads a `Secrets`
object from stdin.

**The GTK front end is Python, and that is the point.** AndersXn images are
routinely cross-built. A compiled toolkit binding - gotk4, Fyne - would drag a
full cross-compiled GTK stack into every build for no benefit. PyGObject needs
no compilation at all, so the graphical installer costs the build nothing and
stays readable, and patchable, on the live medium itself.

It also means the GUI can run unprivileged. It escalates only the install
subprocess, through `sudo` (the live session has a NOPASSWD rule) or `pkexec`,
so a crash in the UI cannot touch a disk.

## The wizard

Eight screens:

`Welcome -> Disk -> Layout -> System -> Account -> Software -> Review -> Install`

Timezone, locale and keyboard layout are **picked from lists**, not typed.
Typing them is a trap: `Europe/London` is easy, but a free-text field silently
accepts `GMT+1` or `en_GB.utf8` and the mistake surfaces on the installed
machine. The lists are read from the live system - `timedatectl`, then
`zone1970.tab`; `/usr/share/i18n/SUPPORTED`; and the XKB layout list from
`xkb-data` - each with a fallback so a missing data file degrades to a short
usable list rather than an empty dropdown.

The keyboard choice is written to **both** `/etc/vconsole.conf` and
`/etc/default/keyboard`. Writing only the first sets the text console and leaves
the desktop on US - a confusing, half-applied setting.

## The engine

`Engine.Steps()` returns only the steps that apply to a given plan - the LUKS
step is simply absent on an unencrypted install - so the progress count the
operator watches is always honest.

Every mount goes through `Engine.mount`, and `Execute` unwinds them in reverse
from a deferred `Cleanup` whether it succeeded or failed. A failed install
leaves the target unmounted and retryable without rebooting the live medium.

Command output is read with a splitter that breaks on `\r` as well as `\n`.
Tools that redraw a progress bar in place - `unsquashfs` writing ~4GB across
114,000 files - emit no newline until they finish, so a line-based reader shows
nothing at all and the longest step of the install looks frozen.

## Safety

- **Tools are checked before anything is written.** `stepVerify` resolves every
  binary the plan needs - chosen per-plan, so an unencrypted install is not made
  to produce `cryptsetup` - and reports all missing ones at once. This exists
  because a missing `unsquashfs` was once discovered *after* the disk had been
  partitioned and formatted.
- **A mounted target disk is refused.** On live media the USB stick you booted
  from is a plausible-looking install target, and wiping it mid-install destroys
  the running system.
- **Two keypresses before anything is erased.** The first confirmation on Review
  arms it; the second starts the install.
- **Secrets never reach argv.** The LUKS passphrase goes to `cryptsetup` on
  stdin, the account password to `chpasswd` on stdin, and both reach the engine
  over stdin rather than the plan file. Neither appears in `/proc/<pid>/cmdline`
  or in the installer log.
- **`Validate()` reports every problem at once**, so the operator is not walked
  back through the wizard one error at a time.
- **An SSH key disables SSH password authentication** by default. With a key on
  file, password auth is dead weight and a standing brute-force target.

## Live media behaviour

Booting the ISO autologins into a **dedicated installer session**: a window
manager (`kwin_x11`, already present with Plasma, so it costs no extra package)
and the installer fullscreen. No panel, no menu, no desktop.

That is deliberate. A complete-looking desktop sitting behind a disk-erasing
wizard implies the machine is already installed, which is exactly the wrong
thing to suggest while someone is choosing which disk to destroy.

A root console is on **tty2** for troubleshooting, and the Limine menu offers a
*Rescue console* entry that boots to `multi-user.target` instead.

## What the installer removes from the target

The live session's conveniences must not survive onto an installed machine.
`stepConfigure` deletes them explicitly - leaving the autologin behind would
ship a system that logs anyone straight into a passwordless sudo account:

- the SDDM autologin drop-in and the installer session
- the live user and its NOPASSWD sudo rule
- the tty2 root recovery console
- `live-boot`, whose initramfs hooks would otherwise keep looking for a squashfs

## Provisioning

The selector shows the catalog in `internal/provision/catalog.go`. A selection
is dependency-resolved to a fixed point (Portainer and CasaOS both pull in
Docker) and the UI names what it pulled in, rather than silently changing what
was ticked. Conflicts - Docker and K3s both wanting to manage packet forwarding
- warn but do not block.

The work itself is shell modules in `/usr/lib/andersxn/modules`, executed inside
the target chroot. They stay scripts rather than Go code so an operator can
read, fix or re-run any of them on a live system with `ax-provision(8)`, with no
rebuild. A failing optional module does not abort the install: the base system
is already on disk and bootable by that point, and losing Jellyfin is not a
reason to leave someone without a machine.

Two modules install on **first boot** rather than during the install, because
their installers inspect a running systemd and a live Docker daemon - neither of
which exists inside the installer chroot:

| Module | Why | Needs network on first boot |
|---|---|---|
| CasaOS | Upstream installer probes the running system | yes |
| Portainer | Compose stack needs a live Docker daemon | yes (image pull) |

K3s is installed during the install but with `INSTALL_K3S_SKIP_START`, for the
same reason.

```bash
ax-provision --list             # what is available and what is deployed
ax-provision casaos             # add a stack after the fact
ax-provision --only docker      # re-run one module
ax-release                      # build + install manifest, for bug reports
```
