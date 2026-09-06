package install

import (
	"context"
	"fmt"

	"github.com/ggstudios/andersxn-os/installer/internal/sys"
)

// GPT partition type GUIDs.
const (
	typeESP      = "ef00" // EFI system partition
	typeBIOSBoot = "ef02" // BIOS boot partition, for Limine's second stage
	typeLinuxFS  = "8300"
	typeMSBasic  = "0700" // plain FAT data partition, used for /boot on BIOS
)

// stepPartition lays out the target disk.
//
// GPT is used on both firmware paths. On legacy BIOS that requires a 1 MiB
// BIOS boot partition for Limine's second stage, which is cheap and avoids
// carrying an MBR code path through the rest of the installer.
//
// Layout:
//
//	UEFI:  1 = FAT32 ESP -> /boot        2 = root
//	BIOS:  1 = BIOS boot  2 = FAT32 -> /boot   3 = root
func (e *Engine) stepPartition(ctx context.Context) error {
	p := e.Plan

	switch p.Scheme {
	case SchemeManual:
		e.logf("manual scheme: root=%s boot=%s", p.ManualRoot, p.ManualESP)
		return nil
	case SchemeFreeSpace:
		return fmt.Errorf("free-space installs are not implemented yet; " +
			"pre-create the partitions and choose the manual scheme")
	}

	disk := p.Disk.Path
	e.logf("erasing partition table on %s", disk)

	// Clear stale filesystem and RAID signatures first. sgdisk --zap-all leaves
	// them behind, and a leftover superblock makes udev present phantom devices
	// that the later mkfs then refuses to overwrite.
	if err := e.R.Run(ctx, "wipefs", "--all", "--force", disk); err != nil {
		return fmt.Errorf("wiping signatures on %s: %w", disk, err)
	}
	if err := e.R.Run(ctx, "sgdisk", "--zap-all", disk); err != nil {
		return fmt.Errorf("clearing partition table on %s: %w", disk, err)
	}

	var args []string
	if p.Firmware == sys.FirmwareBIOS {
		n := p.partNumBIOSBoot()
		args = append(args,
			fmt.Sprintf("--new=%d:0:+%dM", n, biosBootSizeMiB),
			fmt.Sprintf("--typecode=%d:%s", n, typeBIOSBoot),
			fmt.Sprintf("--change-name=%d:AXOS-BIOS", n),
		)
	}

	bootType, bootName := typeESP, "AXOS-ESP"
	if p.Firmware == sys.FirmwareBIOS {
		bootType, bootName = typeMSBasic, "AXOS-BOOT"
	}
	nb := p.partNumBoot()
	args = append(args,
		fmt.Sprintf("--new=%d:0:+%dM", nb, BootSizeMiB),
		fmt.Sprintf("--typecode=%d:%s", nb, bootType),
		fmt.Sprintf("--change-name=%d:%s", nb, bootName),
	)

	nr := p.partNumRoot()
	args = append(args,
		fmt.Sprintf("--new=%d:0:0", nr),
		fmt.Sprintf("--typecode=%d:%s", nr, typeLinuxFS),
		fmt.Sprintf("--change-name=%d:AXOS-ROOT", nr),
		disk,
	)

	if err := e.R.Run(ctx, "sgdisk", args...); err != nil {
		return fmt.Errorf("partitioning %s: %w", disk, err)
	}

	// The kernel re-reads the table asynchronously; without settling, the mkfs
	// that follows can race the creation of the device nodes.
	if err := e.R.Run(ctx, "partprobe", disk); err != nil {
		e.logf("   partprobe: %v (continuing - udev settle follows)", err)
	}
	for _, dev := range []string{p.BootPartition(), p.RawRootPartition()} {
		if err := sys.WaitForDevice(ctx, e.R, dev); err != nil {
			return err
		}
	}

	e.logf("created %s (boot, %d MiB) and %s (root)",
		p.BootPartition(), BootSizeMiB, p.RawRootPartition())
	return nil
}

// stepLUKS creates and opens the encrypted container holding root.
//
// LUKS2 with Argon2id. The passphrase goes in on stdin rather than argv, so it
// never appears in /proc/<pid>/cmdline while cryptsetup runs.
func (e *Engine) stepLUKS(ctx context.Context) error {
	dev := e.Plan.RawRootPartition()
	e.logf("creating LUKS2 container on %s", dev)

	err := e.R.RunInput(ctx, e.Plan.LUKSPass,
		"cryptsetup", "luksFormat",
		"--type", "luks2",
		"--pbkdf", "argon2id",
		"--cipher", "aes-xts-plain64",
		"--key-size", "512",
		"--hash", "sha256",
		"--label", "AXOS-CRYPT",
		"--batch-mode",
		dev,
	)
	if err != nil {
		return fmt.Errorf("formatting LUKS container: %w", err)
	}

	if err := e.R.RunInput(ctx, e.Plan.LUKSPass, "cryptsetup", "open", dev, LUKSName); err != nil {
		return fmt.Errorf("opening LUKS container: %w", err)
	}

	uuid, err := sys.BlkidValue(ctx, e.R, dev, "UUID")
	if err != nil {
		return err
	}
	e.logf("   container %s opened as /dev/mapper/%s", uuid, LUKSName)
	return nil
}
