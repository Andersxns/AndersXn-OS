package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ggstudios/andersxn-os/installer/internal/branding"
	"github.com/ggstudios/andersxn-os/installer/internal/sys"
)

// limineShareDir is where the live image keeps the Limine payloads. Populated
// by build stage 40.
const limineShareDir = "/usr/share/limine"

// stepBootloader installs Limine (default) or systemd-boot onto the target.
//
// AndersXn never installs GRUB. Limine covers UEFI and legacy BIOS on both
// amd64 and arm64 from one flat config file, and draws the brand wallpaper
// without a theme engine.
func (e *Engine) stepBootloader(ctx context.Context) error {
	kernel, initrd, err := e.findKernel()
	if err != nil {
		return err
	}
	e.kernelVer = strings.TrimPrefix(kernel, "vmlinuz-")
	e.logf("kernel %s", e.kernelVer)

	if err := e.installLimine(ctx, kernel, initrd); err != nil {
		return err
	}
	return e.writePiCmdline(ctx)
}

// writePiCmdline repoints the Raspberry Pi kernel command line at the new root.
//
// A Pi never consults Limine. Its firmware reads config.txt and cmdline.txt
// straight from the boot partition, and both arrive in the target as verbatim
// copies of the source system's - so cmdline.txt still carries the root= of
// the disk the installer was run from. Left alone, the installed system either
// boots the source medium or, once that is unplugged, waits forever for a root
// device that is not there.
//
// config.txt needs no such treatment: it names kernels by filename, and those
// files are copied across unchanged.
//
// On anything that is not a Pi there is no config.txt and this does nothing.
func (e *Engine) writePiCmdline(ctx context.Context) error {
	if _, err := os.Stat(filepath.Join(e.Root, BootMountPoint, "config.txt")); err != nil {
		return nil
	}
	e.logf("Raspberry Pi boot path detected; rewriting cmdline.txt for the new root")

	// One line only. The firmware passes the file verbatim and silently drops
	// everything after the first newline, so a stray line break costs you every
	// parameter that follows it.
	line := fmt.Sprintf(
		"console=serial0,115200 console=tty1 root=%s rootfstype=%s%s rw fsck.repair=yes rootwait quiet splash",
		e.rootSpec(), e.Plan.FS, e.rootFlags())

	return e.writeFile(ctx, BootMountPoint+"/cmdline.txt", line+"\n", "0644")
}

// findKernel locates the newest kernel and matching initramfs in the target's
// /boot, which the kernel package has already placed on the FAT partition.
func (e *Engine) findKernel() (kernel, initrd string, err error) {
	bootDir := filepath.Join(e.Root, BootMountPoint)

	entries, err := os.ReadDir(bootDir)
	if err != nil {
		return "", "", fmt.Errorf("reading %s: %w", bootDir, err)
	}

	var kernels []string
	for _, ent := range entries {
		if strings.HasPrefix(ent.Name(), "vmlinuz-") {
			kernels = append(kernels, ent.Name())
		}
	}
	if len(kernels) == 0 {
		return "", "", fmt.Errorf(
			"no kernel found in %s - the system copy may have failed", BootMountPoint)
	}
	// Prefer Debian's kernel over a Raspberry Pi one.
	//
	// An arm64 image carries both: Debian's for the UEFI and BIOS paths, and
	// the Foundation's for the Pi, which boots through config.txt and never
	// consults Limine. The Pi builds carry a higher version, so taking the
	// newest outright would point Limine at a kernel built for a Pi - which a
	// generic arm64 UEFI machine cannot boot.
	debian := kernels[:0:0]
	for _, k := range kernels {
		if !strings.Contains(k, "+rpt") {
			debian = append(debian, k)
		}
	}
	if len(debian) > 0 {
		kernels = debian
	}

	// Lexical sort is a good enough proxy for version order within one release
	// series, and the newest is what we want.
	sort.Strings(kernels)
	kernel = kernels[len(kernels)-1]

	version := strings.TrimPrefix(kernel, "vmlinuz-")
	initrd = "initrd.img-" + version
	if _, err := os.Stat(filepath.Join(bootDir, initrd)); err != nil {
		return "", "", fmt.Errorf(
			"kernel %s has no matching %s in %s: %w", kernel, initrd, BootMountPoint, err)
	}
	return kernel, initrd, nil
}

// rootSpec is what the kernel cmdline points root= at.
func (e *Engine) rootSpec() string {
	if e.Plan.Encrypt {
		return "/dev/mapper/" + LUKSName
	}
	return "UUID=" + e.rootUUID
}

// rootFlags returns the rootflags= needed for a btrfs subvolume root.
func (e *Engine) rootFlags() string {
	if e.Plan.FS == FSBtrfs {
		return " rootflags=subvol=" + BtrfsSubvolumes[0].Name
	}
	return ""
}

// installLimine writes limine.conf and deploys the bootloader for the firmware
// the machine actually booted with.
func (e *Engine) installLimine(ctx context.Context, kernel, initrd string) error {
	conf := e.limineConfig(kernel, initrd)
	if err := e.writeFile(ctx, BootMountPoint+"/limine.conf", conf, "0644"); err != nil {
		return err
	}

	// The splash the bootloader draws behind the menu.
	splash := filepath.Join(e.Root, "usr/share/andersxn/assets/splash.png")
	if _, err := os.Stat(splash); err == nil {
		if err := e.R.Run(ctx, "install", "-Dm0644", splash,
			filepath.Join(e.Root, BootMountPoint, "andersxn", "splash.png")); err != nil {
			e.logf("   note: could not install splash: %v", err)
		}
	}

	if e.Plan.Firmware == sys.FirmwareUEFI {
		return e.installLimineUEFI(ctx)
	}
	return e.installLimineBIOS(ctx)
}

func (e *Engine) installLimineUEFI(ctx context.Context) error {
	stub := sys.EFIStub()
	src := filepath.Join(e.Root, limineShareDir, stub)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("Limine EFI binary %s missing from the image: %w", stub, err)
	}

	// The removable-media path. Firmware falls back to this with no NVRAM
	// entry at all, which is what makes the install boot on machines whose
	// efivars are read-only or whose NVRAM gets cleared.
	fallback := filepath.Join(e.Root, BootMountPoint, "EFI", "BOOT", stub)
	if err := e.R.Run(ctx, "install", "-Dm0644", src, fallback); err != nil {
		return fmt.Errorf("installing %s: %w", stub, err)
	}

	// A vendor path as well, so a firmware NVRAM entry has something stable to
	// point at that a Windows install will not overwrite.
	vendor := filepath.Join(e.Root, BootMountPoint, "EFI", "andersxn", stub)
	if err := e.R.Run(ctx, "install", "-Dm0644", src, vendor); err != nil {
		return fmt.Errorf("installing vendor EFI binary: %w", err)
	}
	e.logf("   Limine installed to EFI/BOOT and EFI/andersxn")

	if !sys.EFIVarsWritable() {
		e.logf("   efivars is not writable - relying on the removable-media path")
		return nil
	}

	label := branding.Name + " " + branding.Version
	loader := `\EFI\andersxn\` + stub
	err := e.R.Run(ctx, "efibootmgr",
		"--create", "--disk", e.Plan.Disk.Path,
		"--part", fmt.Sprint(e.Plan.partNumBoot()),
		"--loader", loader,
		"--label", label,
		"--unicode",
	)
	if err != nil {
		// Not fatal: the fallback path above still boots the machine.
		e.logf("   note: could not register an NVRAM boot entry (%v)", err)
		e.logf("   the removable-media path EFI/BOOT/%s will be used instead", stub)
		return nil
	}
	e.logf("   NVRAM boot entry %q created", label)
	return nil
}

func (e *Engine) installLimineBIOS(ctx context.Context) error {
	sysFile := filepath.Join(e.Root, limineShareDir, "limine-bios.sys")
	if _, err := os.Stat(sysFile); err != nil {
		return fmt.Errorf("limine-bios.sys missing from the image: %w", err)
	}
	dst := filepath.Join(e.Root, BootMountPoint, "limine-bios.sys")
	if err := e.R.Run(ctx, "install", "-Dm0644", sysFile, dst); err != nil {
		return err
	}

	// Embeds the first stage in the protective MBR gap and points it at the
	// BIOS boot partition.
	if err := e.R.Run(ctx, "chroot", e.Root,
		"/usr/bin/limine", "bios-install", e.Plan.Disk.Path); err != nil {
		return fmt.Errorf("embedding the Limine BIOS stage on %s: %w", e.Plan.Disk.Path, err)
	}
	e.logf("   Limine BIOS stage embedded on %s", e.Plan.Disk.Path)
	return nil
}

// limineConfig renders the bootloader menu.
//
// Written directly rather than through the @TOKEN@ template in
// /usr/share/andersxn/bootloader: the installer knows the real kernel version,
// root spec and subvolume flags, and generating them here keeps the escaping
// in one place. The template remains the reference for hand-editing.
func (e *Engine) limineConfig(kernel, initrd string) string {
	base := fmt.Sprintf("root=%s rw%s", e.rootSpec(), e.rootFlags())
	title := fmt.Sprintf("%s %s", branding.Name, branding.Version)

	var b strings.Builder
	fmt.Fprintf(&b, "# Generated by AX-Installer on %s. Safe to edit.\n", nowUTC())
	fmt.Fprintf(&b, "# Regenerate with: ax-bootloader --refresh\n\n")

	b.WriteString("timeout: 5\ndefault_entry: 1\n\n")
	fmt.Fprintf(&b, "interface_branding: %s (%s)\n", title, branding.Codename)
	b.WriteString("interface_branding_colour: 6\n\n")

	ink := strings.TrimPrefix(branding.Ink, "#")
	snow := strings.TrimPrefix(branding.Snow, "#")
	fmt.Fprintf(&b, "term_palette: %s;e5484d;63d68a;e8b84b;4fc3f7;1b6ca8;6b7a90;%s\n", ink, snow)
	fmt.Fprintf(&b, "term_background: %s\nterm_foreground: %s\n\n", ink, snow)
	fmt.Fprintf(&b, "wallpaper: boot():/andersxn/splash.png\nwallpaper_style: centered\nbackdrop: %s\n\n", ink)

	entry := func(name, comment, extra string) {
		fmt.Fprintf(&b, "/%s\n", name)
		fmt.Fprintf(&b, "    comment: %s\n", comment)
		b.WriteString("    protocol: linux\n")
		fmt.Fprintf(&b, "    path: boot():/%s\n", kernel)
		fmt.Fprintf(&b, "    cmdline: %s %s\n", base, extra)
		fmt.Fprintf(&b, "    module_path: boot():/%s\n\n", initrd)
	}

	entry(title, "Boot "+branding.Name+" normally",
		"quiet loglevel=3 splash vt.global_cursor_default=0")
	entry(title+" (verbose)", "Boot with kernel messages on the console",
		"loglevel=7")
	entry(title+" (recovery shell)", "Single-user maintenance shell",
		"single loglevel=7 systemd.unit=rescue.target")

	if e.Plan.Firmware == sys.FirmwareUEFI {
		b.WriteString("/+Firmware\n    //UEFI firmware setup\n        protocol: efi\n        image_path: fwsetup://\n")
	}
	return b.String()
}
