package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// squashfsCandidates are where live-boot may have placed the system image.
var squashfsCandidates = []string{
	"/run/live/medium/live/filesystem.squashfs",
	"/run/live/medium/live/filesystem.squashfs.new",
	"/lib/live/mount/medium/live/filesystem.squashfs",
	"/cdrom/live/filesystem.squashfs",
}

// rsyncExcludes are paths that must never be copied into the target: kernel
// pseudo-filesystems, the live overlay's own scaffolding, and the mount point
// of the target itself (copying e.Root into e.Root recurses forever).
var rsyncExcludes = []string{
	"/dev/*", "/proc/*", "/sys/*", "/run/*", "/tmp/*",
	"/mnt/*", "/media/*", "/lost+found",
	"/var/lib/live/*", "/var/log/live/*",
	"/etc/fstab", "/etc/crypttab",
	"/swapfile",
}

// stepCopySystem populates the target with the AndersXn root filesystem.
//
// Two sources are possible. On live media the squashfs is present and
// unsquashfs is both faster and exact. Falling back to rsync from "/" covers
// running the installer from an already-installed system (which is how the
// image-build stages use it), at the cost of also copying whatever the live
// session has since written.
func (e *Engine) stepCopySystem(ctx context.Context) error {
	if src := findSquashfs(); src != "" {
		e.logf("extracting %s", src)
		// -f: the target already contains the mounted subvolume directories.
		if err := e.R.Run(ctx, "unsquashfs", "-f", "-d", e.Root, src); err != nil {
			return fmt.Errorf("extracting system image: %w", err)
		}
		e.logf("system image extracted")
		return nil
	}

	e.logf("no squashfs found; copying the running system with rsync")
	args := []string{"-aHAXx", "--info=progress2", "--numeric-ids"}
	for _, ex := range rsyncExcludes {
		args = append(args, "--exclude="+ex)
	}
	args = append(args, "--exclude="+e.Root+"/*", "/", e.Root+"/")

	if err := e.R.Run(ctx, "rsync", args...); err != nil {
		return fmt.Errorf("copying system: %w", err)
	}
	return e.copyBoot(ctx)
}

// copyBoot makes a second pass over /boot.
//
// The main copy runs rsync with -x (--one-file-system), which stops dead at a
// mount boundary. That is correct for /dev and friends, but /boot is its own
// filesystem on every Raspberry Pi image - a FAT32 firmware partition beside
// the btrfs root - and the kernels live on it. Without this pass the target
// receives an empty /boot and the install dies later in stepBootloader with
// "no kernel found in /boot", having already partitioned the disk.
//
// When /boot is merely a directory on the root filesystem the main rsync has
// already copied it and this pass finds everything present and identical, so
// it runs unconditionally rather than behind a mount-point test.
//
// The flags are deliberately not -aHAX: the target /boot is always FAT32
// (mkfs.vfat in stepFormat), which cannot store ownership, Unix permissions,
// xattrs or symlinks. -rtL copies recursively, keeps timestamps, and resolves
// symlinks into real files rather than failing on them.
func (e *Engine) copyBoot(ctx context.Context) error {
	if _, err := os.Stat(BootMountPoint); err != nil {
		return nil // no /boot on the source; nothing to carry over
	}
	e.logf("copying %s (a separate filesystem is skipped by the main pass)", BootMountPoint)

	dst := filepath.Join(e.Root, BootMountPoint) + "/"
	if err := e.R.Run(ctx, "rsync", "-rtL", "--info=progress2", BootMountPoint+"/", dst); err != nil {
		return fmt.Errorf("copying %s: %w", BootMountPoint, err)
	}
	return nil
}

func findSquashfs() string {
	for _, c := range squashfsCandidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// writeFile writes content to a path inside the target root.
//
// It goes through the Runner rather than os.WriteFile so that a dry run logs
// the write instead of performing it, and so the log shows every file the
// install created.
func (e *Engine) writeFile(ctx context.Context, path, content, mode string) error {
	target := filepath.Join(e.Root, path)
	e.logf("   write %s (%s, %d bytes)", path, mode, len(content))
	return e.R.RunInput(ctx, content, "install", "-Dm"+mode, "/dev/stdin", target)
}

// chrootScript runs a shell script inside the target.
func (e *Engine) chrootScript(ctx context.Context, script string) error {
	return e.R.RunInput(ctx, script, "chroot", e.Root, "/bin/bash", "-euo", "pipefail")
}

// bindPseudoFS mounts the kernel filesystems a chroot needs.
func (e *Engine) bindPseudoFS(ctx context.Context) error {
	binds := []struct{ src, dst, typ string }{
		{"/proc", "proc", "proc"},
		{"/sys", "sys", "sysfs"},
		{"/dev", "dev", ""},
		{"/dev/pts", "dev/pts", ""},
		{"/run", "run", ""},
	}
	for _, b := range binds {
		dst := filepath.Join(e.Root, b.dst)
		if err := e.R.Run(ctx, "mkdir", "-p", dst); err != nil {
			return err
		}
		var err error
		if b.typ != "" {
			err = e.mount(ctx, "-t", b.typ, b.src, dst)
		} else {
			err = e.mount(ctx, "--rbind", b.src, dst)
		}
		if err != nil {
			return fmt.Errorf("binding %s: %w", b.dst, err)
		}
	}
	// An encrypted or DNS-dependent post-install needs working resolution
	// inside the chroot; the live system's resolv.conf is the only one that
	// exists at this point.
	if data, err := os.ReadFile("/etc/resolv.conf"); err == nil {
		_ = e.writeFile(ctx, "/etc/resolv.conf.axos-install", string(data), "0644")
	}
	return nil
}

// stepConfigure applies identity, locale and network settings to the target.
func (e *Engine) stepConfigure(ctx context.Context) error {
	p := e.Plan

	if err := e.bindPseudoFS(ctx); err != nil {
		return err
	}

	if err := e.writeFile(ctx, "/etc/hostname", p.Hostname+"\n", "0644"); err != nil {
		return err
	}

	hosts := fmt.Sprintf(`127.0.0.1   localhost
127.0.1.1   %s
::1         localhost ip6-localhost ip6-loopback
ff02::1     ip6-allnodes
ff02::2     ip6-allrouters
`, p.Hostname)
	if err := e.writeFile(ctx, "/etc/hosts", hosts, "0644"); err != nil {
		return err
	}

	if err := e.writeFile(ctx, "/etc/locale.conf", "LANG="+p.Locale+"\n", "0644"); err != nil {
		return err
	}
	if err := e.writeFile(ctx, "/etc/locale.gen", p.Locale+" UTF-8\n", "0644"); err != nil {
		return err
	}
	if err := e.writeFile(ctx, "/etc/vconsole.conf", "KEYMAP="+p.Keymap+"\n", "0644"); err != nil {
		return err
	}

	// /etc/default/keyboard is what X11 and Plasma actually read. Writing
	// only vconsole.conf sets the text console and leaves the desktop on the
	// default US layout - a confusing, half-applied setting.
	keyboard := `# Generated by AX-Installer.
XKBMODEL="pc105"
XKBLAYOUT="` + p.Keymap + `"
XKBVARIANT=""
XKBOPTIONS=""
BACKSPACE="guess"
`
	if err := e.writeFile(ctx, "/etc/default/keyboard", keyboard, "0644"); err != nil {
		return err
	}

	// crypttab must exist before the initramfs is rebuilt, or the generated
	// image will have no way to unlock root at boot.
	if p.Encrypt {
		luksUUID, err := e.luksUUID(ctx)
		if err != nil {
			return err
		}
		crypttab := fmt.Sprintf("%s UUID=%s none luks,discard\n", LUKSName, luksUUID)
		if err := e.writeFile(ctx, "/etc/crypttab", crypttab, "0644"); err != nil {
			return err
		}
	}

	// Everything the LIVE medium needs and an installed system must not keep.
	// Getting this wrong is not cosmetic: leaving the autologin behind ships a
	// machine that logs anyone straight into a passwordless sudo account.
	liveTeardown := []string{
		"rm -f /etc/xdg/autostart/andersxn-installer.desktop",
		"rm -f /usr/share/xsessions/andersxn-installer.desktop",
		"rm -f /usr/bin/andersxn-installer-session",
		"rm -f /etc/sddm.conf.d/90-axos-live-autologin.conf",
		"rm -f /etc/sudoers.d/99-axos-live",
		"rm -f /etc/systemd/system/getty@tty2.service.d/axos-recovery.conf",
		"rmdir /etc/systemd/system/getty@tty2.service.d 2>/dev/null || true",
		// Legacy paths from earlier images, harmless when already absent.
		"rm -f /etc/systemd/system/getty@tty1.service.d/ax-installer.conf",
		"rm -f /usr/lib/andersxn/live-installer-shell",
		"systemctl disable ax-installer.service 2>/dev/null || true",
		fmt.Sprintf("userdel --remove %s 2>/dev/null || true", shellQuote(liveUser)),
		// Marker the autostart entry keys off, and a signal to ax-release that
		// this is an installed system rather than live media.
		"mkdir -p /etc/andersxn && : > /etc/andersxn/installed",
	}

	script := strings.Join(append([]string{
		"locale-gen >/dev/null",
		fmt.Sprintf("ln -sf /usr/share/zoneinfo/%s /etc/localtime", p.Timezone),
		"hwclock --systohc 2>/dev/null || true",
		// MODULES=dep halves the initramfs on a machine that only ever boots
		// its own hardware. The live image needs "most"; an install does not.
		"sed -i 's/^MODULES=.*/MODULES=dep/' /etc/initramfs-tools/conf.d/andersxn.conf",
		// live-boot's hooks belong only on the ISO. Left installed, the
		// initramfs keeps looking for a squashfs that is not there.
		"apt-get purge -y live-boot live-boot-initramfs-tools >/dev/null 2>&1 || true",
		"update-initramfs -u -k all",
		// NetworkManager owns the desktop's connections. systemd-networkd would
		// fight it for the same interfaces, so only one of the two is enabled.
		"systemctl enable NetworkManager systemd-resolved ssh 2>/dev/null || true",
		"systemctl disable systemd-networkd 2>/dev/null || true",
		"systemctl enable sddm 2>/dev/null || true",
		"systemctl set-default graphical.target 2>/dev/null || true",
	}, liveTeardown...), "\n")

	if err := e.chrootScript(ctx, script); err != nil {
		return fmt.Errorf("configuring target: %w", err)
	}

	// Record what produced this system, for support and for ax-release(1).
	manifest := fmt.Sprintf(`# Written by AX-Installer.
AXOS_INSTALL_DATE=%s
AXOS_INSTALL_DISK=%s
AXOS_INSTALL_FS=%s
AXOS_INSTALL_ENCRYPTED=%t
AXOS_INSTALL_FIRMWARE=%s
AXOS_INSTALL_MODULES=%s
`,
		nowUTC(), p.Disk.Path, p.FS, p.Encrypt, p.Firmware, strings.Join(p.Modules, ","))

	return e.writeFile(ctx, "/etc/andersxn/install-manifest", manifest, "0644")
}

// luksUUID reads the UUID of the raw partition holding the LUKS header, which
// is what crypttab must reference (not the UUID of the filesystem inside).
func (e *Engine) luksUUID(ctx context.Context) (string, error) {
	out, err := e.R.Output(ctx, "blkid", "-o", "value", "-s", "UUID", e.Plan.RawRootPartition())
	if err != nil {
		return "", fmt.Errorf("reading LUKS container UUID: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// stepAccount creates the operator account.
//
// Passwords are set by piping "user:password" into chpasswd rather than by
// passing a hash on the command line, so the secret never appears in argv or in
// the installer log.
func (e *Engine) stepAccount(ctx context.Context) error {
	p := e.Plan

	groups := []string{"sudo", "systemd-journal"}
	// Only add groups that actually exist in the image, or useradd fails and
	// takes the whole install with it.
	script := fmt.Sprintf(`
existing=""
for g in %s; do
    getent group "$g" >/dev/null 2>&1 && existing="${existing:+$existing,}$g"
done

useradd --create-home --shell /usr/bin/zsh \
    %s \
    ${existing:+--groups "$existing"} \
    %s
`,
		strings.Join(groups, " "),
		shellQuoteComment(p.FullName),
		shellQuote(p.Username),
	)

	if err := e.chrootScript(ctx, script); err != nil {
		return fmt.Errorf("creating account %s: %w", p.Username, err)
	}

	if p.Password != "" {
		if err := e.R.RunInput(ctx, p.Username+":"+p.Password+"\n",
			"chroot", e.Root, "chpasswd"); err != nil {
			return fmt.Errorf("setting password: %w", err)
		}
		e.logf("   password set for %s", p.Username)
	}

	if p.SSHKey != "" {
		key := strings.TrimSpace(p.SSHKey) + "\n"
		home := "/home/" + p.Username
		if err := e.writeFile(ctx, home+"/.ssh/authorized_keys", key, "0600"); err != nil {
			return err
		}
		if err := e.chrootScript(ctx, fmt.Sprintf(
			"chown -R %s:%s %s/.ssh && chmod 700 %s/.ssh",
			shellQuote(p.Username), shellQuote(p.Username), home, home)); err != nil {
			return err
		}
		e.logf("   authorized_keys installed")

		// With a key on file, password auth is dead weight and a standing
		// brute-force target. Turned off unless the operator opted out.
		if p.DisablePw {
			sshd := "# Set by AX-Installer: an SSH key was provided at install time.\n" +
				"PasswordAuthentication no\nKbdInteractiveAuthentication no\n"
			if err := e.writeFile(ctx, "/etc/ssh/sshd_config.d/20-axos-keyonly.conf", sshd, "0644"); err != nil {
				return err
			}
			e.logf("   SSH password authentication disabled")
		}
	}

	return nil
}

// stepFinalise flushes writes and reports how to get into the new system.
func (e *Engine) stepFinalise(ctx context.Context) error {
	// The install wrote the operator's resolv.conf copy; the running system
	// should use systemd-resolved's stub once booted.
	_ = e.chrootScript(ctx, "rm -f /etc/resolv.conf.axos-install")

	if err := e.R.Run(ctx, "sync"); err != nil {
		return err
	}
	e.logf("installation complete")
	e.logf("root filesystem: %s (UUID %s)", e.Plan.RootDevice(), e.rootUUID)
	e.logf("log in as %s after reboot", e.Plan.Username)
	return nil
}

// shellQuote wraps a value in single quotes for safe interpolation into the
// scripts this package pipes into the target's bash.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// shellQuoteComment renders the optional --comment flag, omitting it entirely
// when no full name was given.
func shellQuoteComment(name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	return "--comment " + shellQuote(name)
}

// nowUTC is the timestamp format written into the install manifest.
func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}
