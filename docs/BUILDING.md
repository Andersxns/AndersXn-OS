# Building AndersXn OS

## Host requirements

The build runs on any Debian or Ubuntu host (a container works, with
`--privileged` for the loop-device stages).

Core tooling, needed for every build:

```bash
sudo apt-get install --no-install-recommends \
    mmdebstrap squashfs-tools xorriso \
    gdisk dosfstools btrfs-progs \
    git curl gnupg rsync zstd xz-utils \
    build-essential imagemagick \
    golang-go python3
```

On any host that is **not** Debian - Ubuntu included - you also need Debian's
archive keys, or every repository fails verification with `NO_PUBKEY`:

```bash
sudo apt-get install debian-archive-keyring
```

For **cross-architecture** builds (amd64 on an arm64 host, or the reverse):

```bash
# Emulator + binfmt registration. On Debian and older Ubuntu:
sudo apt-get install qemu-user-static binfmt-support arch-test
# On Ubuntu 26.04+, the same binaries live in a differently named package:
sudo apt-get install qemu-user qemu-user-binfmt arch-test
```

And a cross toolchain for the target - **both** the compiler and its libc
headers, or Limine's deploy utility cannot be built:

```bash
sudo apt-get install gcc-x86-64-linux-gnu libc6-dev-amd64-cross   # target amd64
sudo apt-get install gcc-aarch64-linux-gnu libc6-dev-arm64-cross  # target arm64
```

Roughly 20 GiB free is needed under `work/` for a desktop image.

## Building

```bash
make iso                    # amd64 live installer ISO
make iso ARCH=arm64         # arm64 live installer ISO
make image ARCH=arm64       # arm64 flashable raw image (Pi + UEFI)
make iso BOOTLOADER=systemd-boot
```

Artifacts appear in `dist/` with `.sha256` files beside them. ISOs are
additionally compressed with `zstd`; raw images with `xz`, because Raspberry Pi
Imager and balenaEtcher read `.xz` natively but not `.zst`.

## Stages

The pipeline is nine independent scripts. Each sources the same config and can
be run on its own, which is the fast path when iterating.

| # | Stage | What it does |
|---|---|---|
| 00 | `check-deps` | Host tooling, keyring, disk space, binfmt for cross builds |
| 10 | `bootstrap` | `mmdebstrap` a minimal Debian root, apt tuned for lean |
| 20 | `configure-base` | System + live packages, locale, services, firewall, sysctl |
| 25 | `desktop` | KDE Plasma, SDDM, the live user and installer session, Pi firmware |
| 30 | `branding` | ASCII marks, os-release, issue, motd, Plymouth, Fastfetch, Plasma theme |
| 40 | `kernel-bootloader` | initramfs policy, fetch and build Limine |
| 50 | `installer` | Build AX-Installer, install the provisioning tree |
| 60 | `image-iso` | squashfs + hybrid BIOS/UEFI ISO |
| 70 | `image-raw` | GPT raw image, Pi boot path, first-boot growfs |

Stage 25 runs **before** stage 40 deliberately: `live-boot` must be installed
before the initramfs is regenerated, or the resulting image has no hook to find
and mount the squashfs and the ISO cannot boot at all.

Re-run one stage:

```bash
make stage N=30           # just redo the branding
sudo ./build/build.sh --from 40
sudo ./build/build.sh --only 60
```

Stage 10 is skipped automatically when a bootstrapped rootfs already exists, so
`make iso` after a code change only redoes the cheap stages. Force a clean
bootstrap with `make clean`.

## Cross-architecture builds

A cross build needs three things, and stage 00 checks all three before any
expensive work starts:

1. A **statically linked** qemu user emulator. A dynamically linked one cannot
   resolve its own libraries inside a foreign-architecture chroot, whatever the
   binfmt flags say. The package name moved: `qemu-user-static` on Debian and
   older Ubuntu, `qemu-user` on Ubuntu 26.04+ where the plain binaries are
   static-pie linked. Stage 00 accepts either and verifies static linkage.
2. A `binfmt_misc` registration carrying the **`F` (fix-binary) flag**. Without
   it the kernel resolves the interpreter path relative to the chroot, where it
   does not exist.
3. **`arch-test`** - how `mmdebstrap` satisfies itself the emulator really works
   before it starts. Without it the bootstrap fails several minutes in with a
   bare `E: install arch-test for foreign architecture support`.

The usual one-liner for the binfmt registration:

```bash
docker run --rm --privileged multiarch/qemu-user-static --reset -p yes
```

Limine's `limine(1)` deploy utility is built **twice** on a cross build, because
it is needed at two different architectures: the host's, so stage 60 can embed
the BIOS stage into the ISO, and the target's, so AX-Installer can run it on the
installed machine. Conflating them ships a binary the target cannot execute, and
the failure surfaces only at install time as `Exec format error`. Without a cross
toolchain the build still produces a working UEFI image and warns; only
legacy-BIOS installs are lost.

**Speed.** Native builds are dramatically faster than emulated ones - the Debian
bootstrap takes about 90 seconds native versus 25 minutes under qemu. Build each
architecture on a host of that architecture where you can.

## Configuration

Every value in `build/config/build.conf` reads from the environment first, so
nothing needs editing for a one-off:

```bash
AXOS_SUITE=sid AXOS_VERSION=1.1 make iso
AXOS_MIRROR=http://ftp.uk.debian.org/debian make iso
AXOS_RAW_IMAGE_MIB=12288 make image ARCH=arm64
AXOS_BUILD_RAW=1 make image           # raw image for amd64 too (VM templates)
```

## Checks

```bash
make check          # everything below
make check-shell    # bash -n, plus shellcheck when installed
make check-go       # gofmt -l, go vet
make check-modules  # the installer catalog and the shell modules agree
make test           # go test ./...
```

`check-modules` is the one worth understanding: the Go catalog in
`internal/provision/catalog.go` and the scripts in `provisioning/modules/` are
two halves of one contract. If they drift, the installer offers the operator a
stack it cannot actually deploy. Stage 50 runs the same check and fails the
build on a mismatch.

## Build-time assertions

Several failures in this pipeline are *silent* - the image builds cleanly and
only fails on real hardware. Those are now checked at build time instead:

| Check | Stage | What it prevents |
|---|---|---|
| Package availability for the target arch | 25 | An x86-only package failing 8 minutes into a desktop install |
| Unexpanded `@TOKEN@` in rendered files | 30 | A literal `@ASCII_COMPACT@` on the login screen |
| Shipped `limine` binary matches target arch | 40 | `Exec format error` during a legacy-BIOS install |
| Catalog and provisioning modules agree | 50 | Offering a stack that has no script |
| Kernel extent within 700 MiB of the ISO start | 60 | Limine's `Invalid kernel signature` on large images |

That last one is the least obvious. xorriso lays each directory out
alphabetically, so `filesystem.squashfs` is written before `initrd.img` and
`vmlinuz`. Once the squashfs passed a gigabyte, that pushed the kernel beyond
what Limine's BIOS CD reads could reach and it read garbage. `--sort-weight`
now forces the boot files to the front, and stage 60 measures the result.

## Testing an image without hardware

```bash
# amd64, legacy BIOS (VirtualBox's default)
qemu-system-x86_64 -machine pc -m 4096 -cdrom dist/andersxn-os-1.0-amd64.iso

# amd64, UEFI
qemu-system-x86_64 -m 4096 -bios /usr/share/OVMF/OVMF_CODE.fd \
    -cdrom dist/andersxn-os-1.0-amd64.iso

# arm64 UEFI, from the raw image
qemu-system-aarch64 -M virt -cpu cortex-a72 -m 4096 \
    -bios /usr/share/AAVMF/AAVMF_CODE.fd \
    -drive file=dist/andersxn-os-1.0-arm64.img,format=raw,if=virtio
```

Add `-display none -qmp unix:/tmp/qmp.sock,server,nowait -daemonize` and drive
it over QMP (`screendump`) to check a boot from a headless build host. Note that
the **Raspberry Pi** boot path cannot be tested this way - QEMU's `virt` machine
is UEFI, and a Pi is not. That path needs real hardware.
