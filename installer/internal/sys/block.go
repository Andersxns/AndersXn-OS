package sys

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Disk is an installable whole-disk block device.
type Disk struct {
	Path       string // /dev/nvme0n1
	Name       string // nvme0n1
	SizeBytes  uint64
	Model      string
	Transport  string // nvme, sata, usb, virtio...
	Rotational bool
	Removable  bool
	ReadOnly   bool

	// Mounted reports whether this disk or any of its partitions is currently
	// mounted. The live ISO's own USB stick shows up here, and offering to
	// repartition the medium you booted from is a mistake worth preventing.
	Mounted bool

	Partitions []Partition
}

// Partition is one child of a Disk.
type Partition struct {
	Path       string
	SizeBytes  uint64
	FSType     string
	Label      string
	UUID       string
	MountPoint string
}

// lsblkNode mirrors the subset of lsblk --json we consume.
type lsblkNode struct {
	Name        string      `json:"name"`
	Path        string      `json:"path"`
	Size        uint64      `json:"size"`
	Type        string      `json:"type"`
	Model       string      `json:"model"`
	Tran        string      `json:"tran"`
	Rota        bool        `json:"rota"`
	RM          bool        `json:"rm"`
	RO          bool        `json:"ro"`
	FSType      string      `json:"fstype"`
	Label       string      `json:"label"`
	UUID        string      `json:"uuid"`
	MountPoints []*string   `json:"mountpoints"`
	Children    []lsblkNode `json:"children"`
}

type lsblkOutput struct {
	BlockDevices []lsblkNode `json:"blockdevices"`
}

// ListDisks enumerates installable disks, newest-and-fastest first.
//
// Loop, ram, rom and zram devices are filtered out, as are devices smaller
// than minDiskBytes - which removes the sundry small virtual devices that would
// otherwise clutter the disk picker.
func ListDisks(ctx context.Context, r Runner) ([]Disk, error) {
	const cols = "NAME,PATH,SIZE,TYPE,MODEL,TRAN,ROTA,RM,RO,FSTYPE,LABEL,UUID,MOUNTPOINTS"

	out, err := r.Output(ctx, "lsblk", "--json", "--bytes", "--paths", "--output", cols)
	if err != nil {
		return nil, fmt.Errorf("enumerating block devices: %w", err)
	}

	var parsed lsblkOutput
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return nil, fmt.Errorf("parsing lsblk output: %w", err)
	}

	const minDiskBytes = 2 << 30 // 2 GiB

	var disks []Disk
	for _, node := range parsed.BlockDevices {
		if node.Type != "disk" || node.Size < minDiskBytes {
			continue
		}
		switch {
		case strings.HasPrefix(node.Name, "/dev/loop"),
			strings.HasPrefix(node.Name, "/dev/ram"),
			strings.HasPrefix(node.Name, "/dev/zram"),
			strings.HasPrefix(node.Name, "/dev/sr"):
			continue
		}

		d := Disk{
			Path:       node.Path,
			Name:       strings.TrimPrefix(node.Path, "/dev/"),
			SizeBytes:  node.Size,
			Model:      strings.TrimSpace(node.Model),
			Transport:  node.Tran,
			Rotational: node.Rota,
			Removable:  node.RM,
			ReadOnly:   node.RO,
			Mounted:    hasMount(node),
		}
		for _, c := range node.Children {
			p := Partition{
				Path:      c.Path,
				SizeBytes: c.Size,
				FSType:    c.FSType,
				Label:     c.Label,
				UUID:      c.UUID,
			}
			for _, mp := range c.MountPoints {
				if mp != nil && *mp != "" {
					p.MountPoint = *mp
					d.Mounted = true
					break
				}
			}
			d.Partitions = append(d.Partitions, p)
		}
		disks = append(disks, d)
	}

	// Non-removable before removable, then SSD before spinning rust, then by
	// size. The disk the operator most likely means ends up at the top.
	sort.SliceStable(disks, func(i, j int) bool {
		a, b := disks[i], disks[j]
		if a.Removable != b.Removable {
			return !a.Removable
		}
		if a.Rotational != b.Rotational {
			return !a.Rotational
		}
		return a.SizeBytes > b.SizeBytes
	})

	return disks, nil
}

func hasMount(n lsblkNode) bool {
	for _, mp := range n.MountPoints {
		if mp != nil && *mp != "" {
			return true
		}
	}
	for _, c := range n.Children {
		if hasMount(c) {
			return true
		}
	}
	return false
}

// HumanSize formats a byte count the way a disk vendor would, which is what the
// operator sees printed on the drive.
func HumanSize(b uint64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMGTP"[exp])
}

// Describe renders a one-line summary for the disk picker.
func (d Disk) Describe() string {
	bits := []string{HumanSize(d.SizeBytes)}
	if d.Model != "" {
		bits = append(bits, d.Model)
	}
	if d.Transport != "" {
		bits = append(bits, d.Transport)
	}
	if d.Rotational {
		bits = append(bits, "hdd")
	} else {
		bits = append(bits, "ssd")
	}
	if d.Removable {
		bits = append(bits, "removable")
	}
	if d.Mounted {
		bits = append(bits, "IN USE")
	}
	return strings.Join(bits, " - ")
}

// PartitionPath returns the device node for partition n of this disk, honouring
// the "p" infix that NVMe, MMC and loop devices use (/dev/nvme0n1p1) but SCSI
// and virtio devices do not (/dev/sda1).
func (d Disk) PartitionPath(n int) string {
	last := d.Path[len(d.Path)-1]
	if last >= '0' && last <= '9' {
		return fmt.Sprintf("%sp%d", d.Path, n)
	}
	return fmt.Sprintf("%s%d", d.Path, n)
}

// BlkidValue reads one udev property of a block device, e.g. "UUID" or
// "PARTUUID". Returns an empty string when the property is not set.
func BlkidValue(ctx context.Context, r Runner, dev, key string) (string, error) {
	out, err := r.Output(ctx, "blkid", "-o", "value", "-s", key, dev)
	if err != nil {
		return "", fmt.Errorf("reading %s of %s: %w", key, dev, err)
	}
	return strings.TrimSpace(out), nil
}

// WaitForDevice blocks until a device node appears, which it may not have done
// immediately after sgdisk returns - the kernel re-reads the partition table
// asynchronously and udev then creates the nodes.
func WaitForDevice(ctx context.Context, r Runner, dev string) error {
	if err := r.Run(ctx, "udevadm", "settle", "--timeout=30"); err != nil {
		return err
	}
	if _, err := os.Stat(dev); err != nil {
		return fmt.Errorf("device %s did not appear after partitioning: %w", dev, err)
	}
	return nil
}
