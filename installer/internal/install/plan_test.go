package install

import (
	"strings"
	"testing"

	"github.com/ggstudios/andersxn-os/installer/internal/sys"
)

func validPlan() Plan {
	p := NewPlan()
	p.Disk = sys.Disk{Path: "/dev/sda", SizeBytes: 256 << 30}
	p.Username = "operator"
	p.Password = "correct horse battery staple"
	p.Firmware = sys.FirmwareUEFI
	return p
}

func TestValidateAcceptsAGoodPlan(t *testing.T) {
	if err := validPlan().Validate(); err != nil {
		t.Fatalf("expected a valid plan, got %v", err)
	}
}

func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	p := NewPlan()
	p.Hostname = "-bad-"
	p.Username = "1nvalid"

	err := p.Validate()
	if err == nil {
		t.Fatal("expected validation to fail")
	}
	for _, want := range []string{"no target disk", "hostname", "username"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestValidateRejectsShortLUKSPassphrase(t *testing.T) {
	p := validPlan()
	p.Encrypt = true
	p.LUKSPass = "short"
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "passphrase") {
		t.Fatalf("expected a passphrase complaint, got %v", err)
	}
}

func TestValidateRequiresAWayToLogIn(t *testing.T) {
	p := validPlan()
	p.Password = ""
	p.SSHKey = ""
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "cannot log in") {
		t.Fatalf("expected a login complaint, got %v", err)
	}
}

func TestValidateRejectsReservedUsernames(t *testing.T) {
	p := validPlan()
	p.Username = "root"
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("expected root to be reserved, got %v", err)
	}
}

// Partition numbering shifts on BIOS to make room for the 1 MiB Limine stage.
func TestPartitionNumbersFollowFirmware(t *testing.T) {
	p := validPlan()

	p.Firmware = sys.FirmwareUEFI
	if got := p.BootPartition(); got != "/dev/sda1" {
		t.Errorf("UEFI boot partition = %s, want /dev/sda1", got)
	}
	if got := p.RawRootPartition(); got != "/dev/sda2" {
		t.Errorf("UEFI root partition = %s, want /dev/sda2", got)
	}
	if p.BIOSBootPartition() != "" {
		t.Error("UEFI should have no BIOS boot partition")
	}

	p.Firmware = sys.FirmwareBIOS
	if got := p.BIOSBootPartition(); got != "/dev/sda1" {
		t.Errorf("BIOS stage partition = %s, want /dev/sda1", got)
	}
	if got := p.BootPartition(); got != "/dev/sda2" {
		t.Errorf("BIOS boot partition = %s, want /dev/sda2", got)
	}
	if got := p.RawRootPartition(); got != "/dev/sda3" {
		t.Errorf("BIOS root partition = %s, want /dev/sda3", got)
	}
}

// NVMe and MMC devices need a "p" between the device and the partition index.
func TestPartitionPathHandlesNVMe(t *testing.T) {
	p := validPlan()
	p.Disk = sys.Disk{Path: "/dev/nvme0n1"}
	if got := p.RawRootPartition(); got != "/dev/nvme0n1p2" {
		t.Errorf("nvme root partition = %s, want /dev/nvme0n1p2", got)
	}
}

func TestMountOptionsMarkNoCOWSubvolumes(t *testing.T) {
	p := validPlan()
	var docker Subvolume
	for _, sv := range BtrfsSubvolumes {
		if sv.Name == "@docker" {
			docker = sv
		}
	}
	opts := p.MountOptions(docker)
	if !strings.Contains(opts, "nodatacow") {
		t.Errorf("@docker options %q should contain nodatacow", opts)
	}
	if strings.Contains(opts, "compress") {
		t.Errorf("@docker options %q should not compress", opts)
	}
}
