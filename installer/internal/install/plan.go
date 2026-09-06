// Package install turns an operator's choices into an installed AndersXn OS.
//
// The TUI (and the GUI wrapper) build a Plan; Execute then carries it out as an
// ordered list of named Steps. Nothing in this package touches the terminal, so
// the same engine backs both front ends and an unattended mode.
package install

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ggstudios/andersxn-os/installer/internal/sys"
)

// FSType is the root filesystem.
type FSType string

const (
	FSBtrfs FSType = "btrfs"
	FSExt4  FSType = "ext4"
	FSXFS   FSType = "xfs"
)

// Scheme is how the target disk is laid out.
type Scheme string

const (
	// SchemeWipe destroys the existing partition table and takes the whole disk.
	SchemeWipe Scheme = "wipe"
	// SchemeFreeSpace installs into contiguous unallocated space, leaving the
	// existing partitions untouched.
	SchemeFreeSpace Scheme = "freespace"
	// SchemeManual uses partitions the operator prepared beforehand.
	SchemeManual Scheme = "manual"
)

// Plan is the complete description of an install. It is deliberately a plain
// value: it can be written to /etc/andersxn/install.json for support, and
// replayed for an unattended install.
type Plan struct {
	// Target
	Disk     sys.Disk
	Scheme   Scheme
	FS       FSType
	Encrypt  bool
	LUKSPass string `json:"-"` // never serialised

	// Manual scheme only: pre-existing partitions to use as-is.
	ManualRoot string
	ManualESP  string

	// Identity
	Hostname string
	Timezone string
	Locale   string
	Keymap   string

	// Operator account
	Username  string
	FullName  string
	Password  string `json:"-"`
	SSHKey    string
	Sudo      bool
	DisablePw bool // disable SSH password auth; auto-set when SSHKey is given

	// Software
	Modules []string

	// Platform: detected, not chosen
	Firmware sys.Firmware
	Arch     string

	// Behaviour
	DryRun bool
}

// NewPlan returns a Plan with the AndersXn defaults applied.
func NewPlan() Plan {
	return Plan{
		Scheme:   SchemeWipe,
		FS:       FSBtrfs,
		Hostname: "andersxn",
		Timezone: "UTC",
		Locale:   "en_US.UTF-8",
		Keymap:   "us",
		Sudo:     true,
		Firmware: sys.DetectFirmware(),
		Arch:     sys.DebArch(),
	}
}

// BootSizeMiB is the size of the FAT32 boot partition.
//
// 1 GiB rather than the conventional 512 MiB: AndersXn keeps every installed
// kernel and its zstd-compressed initramfs here, and three kernels on a machine
// carrying a lot of firmware blobs will overrun 512 MiB. Disk space is cheaper
// than a failed kernel upgrade.
const BootSizeMiB = 1024

// BootMountPoint is where the boot partition is mounted.
//
// The boot partition is FAT32 and mounted at /boot rather than the more usual
// /boot/efi, on BOTH firmware paths. The reason is Limine: it reads FAT, ext2/3/4
// and ISO9660, but NOT btrfs - and btrfs is the AndersXn default root. Mounting
// the FAT partition at /boot puts vmlinuz-* and initrd.img-* exactly where
// Debian's kernel packages already write them, on a filesystem the bootloader
// can read. No copy hooks, and no chance of the bootloader and the package
// manager disagreeing about which kernel is installed.
const BootMountPoint = "/boot"

// biosBootSizeMiB is the unformatted BIOS boot partition holding Limine's
// second stage on legacy-BIOS machines.
const biosBootSizeMiB = 1

// Partition numbers depend on firmware: legacy BIOS needs an extra 1 MiB
// bios_grub partition ahead of everything else, which shifts the rest along.
//
//	UEFI:  1 = FAT32 ESP (/boot)          2 = root
//	BIOS:  1 = BIOS boot   2 = FAT32 (/boot)   3 = root
func (p Plan) partNumBIOSBoot() int { return 1 }

func (p Plan) partNumBoot() int {
	if p.Firmware == sys.FirmwareUEFI {
		return 1
	}
	return 2
}

func (p Plan) partNumRoot() int {
	if p.Firmware == sys.FirmwareUEFI {
		return 2
	}
	return 3
}

// LUKSName is the device-mapper name for an encrypted root.
const LUKSName = "axos-root"

// liveUser is the unprivileged account the live session runs as. It must match
// AXOS_LIVE_USER in build/config/build.conf; the installer deletes this account
// from the target so an installed system never carries a passwordless,
// NOPASSWD-sudo login.
const liveUser = "axos"

// Subvolume is one entry in the btrfs layout.
type Subvolume struct {
	Name     string
	MountAt  string // empty means it is not mounted directly
	NoCOW    bool
	Compress bool
}

// BtrfsSubvolumes is the layout created on a btrfs root.
//
// Separating @ from @home means a root snapshot rollback never reverts the
// operator's data. @docker and @containers are nodatacow: copy-on-write over
// container overlay layers fragments badly and costs real throughput. @log and
// @snapshots stay outside root snapshots for the same reason.
var BtrfsSubvolumes = []Subvolume{
	{Name: "@", MountAt: "/", Compress: true},
	{Name: "@home", MountAt: "/home", Compress: true},
	{Name: "@log", MountAt: "/var/log", Compress: true},
	{Name: "@cache", MountAt: "/var/cache", Compress: true},
	{Name: "@snapshots", MountAt: "/.snapshots"},
	{Name: "@docker", MountAt: "/var/lib/docker", NoCOW: true},
	{Name: "@containers", MountAt: "/var/lib/containers", NoCOW: true},
	{Name: "@srv", MountAt: "/srv", Compress: true},
}

// MountOptions returns the fstab options for one subvolume.
func (p Plan) MountOptions(sv Subvolume) string {
	opts := []string{"noatime", "subvol=" + sv.Name}
	if sv.Compress {
		opts = append(opts, "compress=zstd:3")
	}
	if sv.NoCOW {
		opts = append(opts, "nodatacow")
	}
	if !p.Disk.Rotational {
		opts = append(opts, "ssd")
	}
	return strings.Join(append(opts, "space_cache=v2"), ",")
}

// RootDevice is the device holding the root filesystem, accounting for LUKS.
func (p Plan) RootDevice() string {
	if p.Encrypt {
		return "/dev/mapper/" + LUKSName
	}
	return p.RawRootPartition()
}

// RawRootPartition is the partition backing root, before any LUKS mapping.
func (p Plan) RawRootPartition() string {
	if p.Scheme == SchemeManual {
		return p.ManualRoot
	}
	return p.Disk.PartitionPath(p.partNumRoot())
}

// BootPartition is the FAT32 partition mounted at /boot. On UEFI it is also the
// EFI system partition.
func (p Plan) BootPartition() string {
	if p.Scheme == SchemeManual {
		return p.ManualESP
	}
	return p.Disk.PartitionPath(p.partNumBoot())
}

// BIOSBootPartition is the 1 MiB unformatted partition Limine's BIOS stage is
// embedded into. Empty on UEFI, where it is not created.
func (p Plan) BIOSBootPartition() string {
	if p.Firmware == sys.FirmwareUEFI {
		return ""
	}
	return p.Disk.PartitionPath(p.partNumBIOSBoot())
}

// Validate reports every problem with the plan at once, so the operator is not
// walked back through the wizard one error at a time.
func (p Plan) Validate() error {
	var problems []string

	if p.Scheme != SchemeManual {
		if p.Disk.Path == "" {
			problems = append(problems, "no target disk selected")
		}
		if p.Disk.ReadOnly {
			problems = append(problems, "target disk is read-only")
		}
	} else {
		if p.ManualRoot == "" {
			problems = append(problems, "manual scheme needs a root partition")
		}
		if p.Firmware == sys.FirmwareUEFI && p.ManualESP == "" {
			problems = append(problems, "manual scheme on UEFI needs a boot/ESP partition")
		}
	}

	if p.Encrypt && len(p.LUKSPass) < 8 {
		problems = append(problems, "encryption passphrase must be at least 8 characters")
	}
	if err := validateHostname(p.Hostname); err != nil {
		problems = append(problems, err.Error())
	}
	if err := validateUsername(p.Username); err != nil {
		problems = append(problems, err.Error())
	}
	if p.Password == "" && p.SSHKey == "" {
		problems = append(problems,
			"set a password or an SSH key, otherwise the account cannot log in")
	}
	if p.SSHKey != "" && !looksLikeSSHKey(p.SSHKey) {
		problems = append(problems,
			"SSH key should start with ssh-ed25519, ssh-rsa or ecdsa-sha2-")
	}

	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "; "))
}

func validateHostname(h string) error {
	if h == "" {
		return errors.New("hostname is empty")
	}
	if len(h) > 63 {
		return errors.New("hostname is longer than 63 characters")
	}
	for i, r := range h {
		alnum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if alnum || (r == '-' && i != 0 && i != len(h)-1) {
			continue
		}
		return fmt.Errorf("hostname %q may only contain letters, digits and interior hyphens", h)
	}
	return nil
}

func validateUsername(u string) error {
	if u == "" {
		return errors.New("username is empty")
	}
	if len(u) > 32 {
		return errors.New("username is longer than 32 characters")
	}
	if u[0] >= '0' && u[0] <= '9' {
		return errors.New("username may not start with a digit")
	}
	// Colliding with a system account would hand the operator a login that
	// daemons also use.
	for _, reserved := range []string{"root", "daemon", "bin", "sys", "nobody", "systemd-network"} {
		if u == reserved {
			return fmt.Errorf("username %q is reserved by the system", u)
		}
	}
	for _, r := range u {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return fmt.Errorf("username %q may only contain lowercase letters, digits, - and _", u)
	}
	return nil
}

func looksLikeSSHKey(k string) bool {
	k = strings.TrimSpace(k)
	prefixes := []string{
		"ssh-ed25519 ", "ssh-rsa ", "ecdsa-sha2-",
		"sk-ssh-ed25519", "sk-ecdsa-sha2-",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}
