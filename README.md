```
                 $$
               $$$$$
              $$$$$$$$
             $$$$$$
            $$$   $$$$$$
           $   $$$$$$$$$$
            $$$$$$$$$$$$$$
          $$$$$$$$
        $$$$$$       $$$$$$$$
       $$$    $$$$$$$$$$$$$$$$
     $$$  $$$$$$$$$$$$$$$$$$$$$$
       $$$$$$$$$$$$$$$$$$$$$$$$$$
    $$$$$$$$$$$$$$$$$$$$$$$$$$$$$$
  $$$$$$$$$$$$$$$$$$$$$$$$$$$$$$$$$
$$$$$$                         $$$$$$
$                                   $
```

# AndersXn OS

A performance-oriented Linux distribution for developers, self-hosters and
homelab administrators, with a KDE Plasma desktop. Native `amd64` and `arm64`.

Built by GG Studios.

| | |
|---|---|
| **Base** | Debian `trixie`, bootstrapped with `mmdebstrap` |
| **Desktop** | KDE Plasma 6 on SDDM, themed in the AndersXn palette |
| **Bootloader** | Limine on UEFI and legacy BIOS; Raspberry Pi firmware on arm64. **No GRUB, anywhere.** |
| **Kernel** | Debian mainline, plus the Raspberry Pi Foundation kernel on the Pi boot path |
| **Root filesystem** | btrfs with subvolumes, zstd compression, optional LUKS2 |
| **Installer** | AX-Installer - a GTK4 front end over a UI-agnostic Go engine |
| **Shell** | zsh with a framework-free AndersXn theme |
| **Provisioning** | Docker, Portainer, K3s, Jellyfin, CasaOS, Tailscale, dev toolchain |

## Artifacts

| Target | Build | Output |
|---|---|---|
| amd64 PCs and VMs | `make iso` | `dist/*-amd64.iso` - hybrid BIOS/UEFI live installer |
| arm64 UEFI machines | `make iso ARCH=arm64` | `dist/*-arm64.iso` |
| Raspberry Pi and arm64 SBCs | `make image ARCH=arm64` | `dist/*-arm64.img.xz` - flashable |

The arm64 raw image boots **both** a Raspberry Pi and a generic arm64 UEFI
machine from the same FAT partition: the Pi bootloader reads `config.txt`, UEFI
firmware reads `EFI/BOOT/BOOTAA64.EFI`, and each ignores the other's files. It
also carries two kernels - the Foundation kernel for the Pi, Debian's mainline
one for UEFI - because Debian's cannot drive a Pi 5. Flash it with Raspberry Pi
Imager or balenaEtcher; both read `.xz` directly.

## Quick start

```bash
make iso                 # amd64 live installer ISO
make image ARCH=arm64    # Raspberry Pi / arm64 flashable image
make check               # syntax, vet, tests, catalog parity
```

Artifacts land in `dist/` with `.sha256` sidecars.

Explore the installer without touching a disk:

```bash
make installer && ./dist/ax-installer --dry-run
```

## Repository layout

```
branding/        the visual identity - one source of truth
  ascii/         the mark in three sizes, generated from the master art
  assets/        AXnobg.png, the master raster
  etc/           os-release, issue, motd templates
  fastfetch/     system fetch configuration
  limine/        bootloader menu templates (installed + live)
  plymouth/      boot splash theme
  skel/          the zsh environment every account inherits
  systemd-boot/  alternative bootloader templates

build/           the image pipeline
  build.sh       orchestrator
  config/        build.conf - every knob, all overridable from the environment
  lib/common.sh  logging, palette, mount bookkeeping, chroot helpers, DNS
  stages/        00 deps, 10 bootstrap, 20 base, 25 desktop, 30 branding,
                 40 kernel+bootloader, 50 installer, 60 ISO, 70 raw image
  tools/         checkers used by "make check"

installer/       AX-Installer
  gui/           ax-installer-gtk - the GTK4 front end (Python/PyGObject)
  cmd/           ax-installer (engine + TUI), ax-installer-gui (launcher)
  internal/
    api/         the JSON protocol the GTK front end speaks
    branding/    embedded marks and colour tokens
    tui/         the Bubble Tea wizard, for headless and serial installs
    install/     the install engine - no terminal code, deliberately
    sys/         block devices, firmware detection, command execution
    provision/   the module catalog the selector shows

provisioning/    what runs after the base install
  postinstall.sh driver, executed inside the target chroot
  modules/       one script per catalog module
  bin/           ax-provision, ax-release

docs/            architecture, building, branding, installer notes
```

## Design decisions worth knowing

**Debian, not Arch.** The only base with a first-class story on both `amd64` and
`arm64` *and* official upstream repositories for Docker, Tailscale, Jellyfin and
K3s - precisely the software the provisioning selector deploys.

**`/boot` is FAT32, on every firmware path.** Limine reads FAT, ext2/3/4 and
ISO9660 - but not btrfs, the AndersXn default root. Mounting the FAT partition
at `/boot` puts `vmlinuz-*` and `initrd.img-*` exactly where Debian's kernel
packages already write them, on a filesystem the bootloader can read. No copy
hooks, and no way for the bootloader and the package manager to disagree about
which kernel is installed. It is what makes the Pi and UEFI paths share one
partition, too.

**The install engine has no UI in it.** `internal/install` takes a `Plan` and a
`Runner` and nothing else. The GTK front end drives it over a JSON protocol
rather than importing it, which is why the graphical installer needs no
compilation and no cross-compiled GTK stack. See [docs/INSTALLER.md](docs/INSTALLER.md).

**The live session is the installer, not a desktop.** Booting the ISO gives a
dedicated session running a window manager and the installer, with no panel or
menu behind it. A complete-looking desktop sitting behind a disk-erasing wizard
implies the machine is already installed.

**Nothing phones home, and no credentials are baked into images.** Tailscale is
installed and enabled but left disconnected: an image carrying an auth key
grants tailnet access to anyone who obtains that image.

## Documentation

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) - how the pipeline fits together, and why
- [docs/BUILDING.md](docs/BUILDING.md) - host requirements, stages, cross builds
- [docs/INSTALLER.md](docs/INSTALLER.md) - AX-Installer internals and the JSON protocol
- [docs/BRANDING.md](docs/BRANDING.md) - the mark, the palette, every touchpoint
