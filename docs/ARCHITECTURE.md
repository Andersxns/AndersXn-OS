# AndersXn OS architecture

## The shape of the thing

```
  branding/  ---------------+
   (one visual identity)    |
                            v
  build/stages/  -->  work/<arch>/rootfs  -->  dist/*.iso
       ^                    |                  dist/*.img.xz
       |                    v
  build/config/     installer/ + provisioning/
   (every knob)      (baked into the image)
```

A build is nine ordered stages against one rootfs directory. Nothing in the
pipeline is a black box: each stage is a script that sources the same config and
library, and any of them can be run on its own.

## Layering

**`build/lib/common.sh`** is the floor: palette, logging, architecture mapping,
chroot helpers, mount bookkeeping and chroot DNS.

Two of those matter more than they look. Every bind mount is recorded so a
failed build unwinds in reverse from an EXIT trap - without it, a build that
dies midway leaves `/proc`, `/sys` and `/dev` bound inside the rootfs, and a
later `rm -rf` on that directory becomes genuinely destructive. And
`axos_mount_pseudo` writes a real `/etc/resolv.conf` into the chroot, because
the installed system's resolv.conf is a symlink into `/run`, which the tmpfs
mounted over `/run` hides - leaving every later stage without name resolution.

**`build/config/build.conf`** holds every tunable, each reading from the
environment first, so CI and one-off local builds never need to edit a file.
Package sets are architecture-keyed where they have to be: `xserver-xorg-video-vmware`
is the VirtualBox/VMware adapter driver and exists only on x86.

**`build/stages/`** are the pipeline. Stage 10 is skipped when a rootfs already
exists, so iterating on branding or the installer costs seconds rather than a
full bootstrap.

Stage ordering carries one hard constraint: **stage 25 must precede stage 40**.
`live-boot` has to be installed before the initramfs is regenerated, or the
image has no hook to find and mount its own squashfs and cannot boot at all.

## Base and bootloader

Debian `trixie`, bootstrapped with `mmdebstrap` rather than plain `debootstrap`:
one pass, native foreign-architecture support, and the AndersXn apt policy
injected *during* the bootstrap, so the bloat is never installed rather than
deleted afterwards.

Limine, never GRUB. One binary covers UEFI and legacy BIOS on both
architectures, the config is a flat readable file instead of a generated shell
script, and it draws the brand wallpaper without a theme engine.

## The /boot decision

This one constraint shapes the partition layout everywhere - the installer, the
raw image builder and the ISO all follow it.

Limine reads FAT, ext2/3/4 and ISO9660. It does **not** read btrfs, the AndersXn
default root. So the kernel cannot live on the root filesystem.

The layout puts a 1 GiB FAT32 partition at `/boot` on every firmware path:

```
UEFI:   1 = FAT32 ESP -> /boot             2 = btrfs root
BIOS:   1 = BIOS boot   2 = FAT32 -> /boot   3 = btrfs root
```

Debian's kernel packages write `vmlinuz-*` and `initrd.img-*` straight into
`/boot`, which is now a filesystem the bootloader can read. No copy hooks to
keep in sync, and no way for the bootloader and the package manager to disagree
about which kernel is installed. 1 GiB rather than the conventional 512 MiB
because three kernels with zstd initramfs images on a machine carrying a lot of
firmware will overrun the smaller size.

GPT is used on both paths; legacy BIOS gets a 1 MiB BIOS boot partition for
Limine's second stage, which is cheap and avoids carrying an MBR code path
through the rest of the installer.

## One arm64 image, two boot paths

A Raspberry Pi has no UEFI. Its bootloader reads `config.txt` from the root of
the FAT partition and loads the kernel and device tree itself. Generic arm64
machines boot `EFI/BOOT/BOOTAA64.EFI` instead.

Because both live on the same FAT partition and each firmware ignores the
other's files, one image serves both:

| Path | Reads | Ignores |
|---|---|---|
| Raspberry Pi | `config.txt`, `bcm*.dtb`, `start*.elf` | `EFI/` |
| arm64 UEFI | `EFI/BOOT/BOOTAA64.EFI`, `limine.conf` | `config.txt` |

### Two kernels, not one

The Pi path does not run Debian's kernel. Debian's mainline arm64 build cannot
adequately drive a Pi 5 - BCM2712 and especially the RP1 southbridge, which
carries USB, Ethernet and most I/O, were only partly supported at 6.12, and a
Pi 5 booting it lands in an initramfs shell with no root device. Raspberry Pi OS
ships the Foundation's kernel tree for exactly this reason.

So the image carries both, each on the path that needs it:

| Boot path | Kernel | Selected by |
|---|---|---|
| Raspberry Pi 5 | `linux-image-rpi-2712` | `[pi5]` in `config.txt` |
| Raspberry Pi 4 / 400 / CM4 / 3 / Zero 2 W | `linux-image-rpi-v8` | `[pi4]`, `[pi3]` |
| Generic arm64 UEFI | Debian `linux-image-arm64` | Limine |

The Foundation kernel also brings the Pi overlays Debian has none of, including
`vc4-kms-v3d` - without which the desktop has no accelerated display.

Its archive is **pinned**: everything from `archive.raspberrypi.com` sits below
Debian's default priority except the kernel and firmware packages. Unpinned, it
shadows large parts of Debian with Raspberry Pi OS rebuilds - a different
distribution wearing the same package names.

`config.txt` names kernel files explicitly and the Pi bootloader has no notion
of "newest", so a `postinst.d` hook repoints each `[piN]` section on every
kernel upgrade. Debian's own `raspi-firmware` hook maintains `/boot/firmware`,
which is not where this layout keeps things.

## The ISO layout constraint

xorriso lays each directory out alphabetically. In `/live` that puts
`filesystem.squashfs` before `initrd.img` and `vmlinuz`, so as the squashfs grew
past a gigabyte the boot files were pushed beyond 1.4 GB into the image - past
what Limine's BIOS CD reads can reach. It read garbage and rejected the kernel
header with `Invalid kernel signature`. A smaller ISO booted only by luck.

Stage 60 forces the boot files to the front with `--sort-weight`, then
**measures the result** and fails the build if the kernel lands beyond 700 MiB.
The failure was silent - the ISO built cleanly and only died on real hardware -
so it is now a build error instead.

## btrfs layout

| Subvolume | Mounted at | Notes |
|---|---|---|
| `@` | `/` | zstd:3 |
| `@home` | `/home` | separate, so a root rollback never reverts user data |
| `@log` | `/var/log` | kept out of root snapshots |
| `@cache` | `/var/cache` | |
| `@snapshots` | `/.snapshots` | |
| `@docker` | `/var/lib/docker` | **nodatacow** |
| `@containers` | `/var/lib/containers` | **nodatacow** |
| `@srv` | `/srv` | homelab service data |

`nodatacow` on the container roots is not cosmetic: copy-on-write over overlay
layers fragments badly and costs real throughput.

## The installer seam

`internal/install` knows nothing about terminals or toolkits. It takes a `Plan`
(a plain serialisable value) and a `sys.Runner` (an interface), and reports
progress through a callback.

Front ends do not import it. They drive it as a subprocess over a JSON protocol
(`internal/api`), which is what lets the GTK front end be Python and need no
compilation - and therefore no cross-compiled GTK stack in every cross build.
See [INSTALLER.md](INSTALLER.md).

`internal/provision` and `provisioning/modules/` are two halves of one contract:
the Go catalog is what the operator sees, the shell modules are what runs.
`make check-modules` and build stage 50 both fail on a mismatch, because the
failure mode otherwise is an installer offering a stack it cannot deploy.

## The live session

Booting the ISO does not give a desktop. SDDM autologins into a dedicated
session running a window manager and the installer fullscreen - no panel, no
menu, nothing behind it.

The live user is created at **build time**, not boot time. This is why
`live-config` is deliberately absent: it creates its own user and rewrites the
tty1 autologin during boot, and when that does not complete you get agetty
autologging into an account that does not exist - an authentication failure in
a respawn loop. Nothing in the AndersXn live session has to succeed at boot for
the installer to appear.

## Security posture

- root is locked; the installer creates the operator account
- default-deny inbound nftables ruleset, with a `services` chain the
  provisioning modules append to
- `PermitRootLogin no`, and password auth disabled automatically when an SSH key
  is supplied at install time
- the live user's passwordless account and NOPASSWD sudo rule exist **only** on
  the live medium; the installer deletes both, along with the autologin, so an
  installed machine never carries them
- no auth keys, tokens or credentials are ever baked into an image - Tailscale
  installs and enables but stays disconnected until someone runs `tailscale up`
- the K3s and CasaOS upstream installers are downloaded to a file and
  sanity-checked before they run, rather than piped from curl into a root shell
- the graphical installer runs unprivileged and escalates only the install
  subprocess, so a crash in the UI cannot touch a disk
