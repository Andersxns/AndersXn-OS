package sys

import (
	"os"
	"runtime"
	"strings"
)

// Firmware is the platform boot mode the installer must target.
type Firmware int

const (
	FirmwareBIOS Firmware = iota
	FirmwareUEFI
)

func (f Firmware) String() string {
	if f == FirmwareUEFI {
		return "UEFI"
	}
	return "BIOS"
}

// DetectFirmware reports whether the machine booted via UEFI.
//
// The presence of /sys/firmware/efi is the authoritative signal: the kernel
// only creates it when it was handed an EFI system table. Note this reflects
// how the LIVE system booted, which is what matters - an installed system must
// use the same mode, because the firmware will not switch on its behalf.
func DetectFirmware() Firmware {
	if _, err := os.Stat("/sys/firmware/efi"); err == nil {
		return FirmwareUEFI
	}
	return FirmwareBIOS
}

// EFIVarsWritable reports whether efivarfs is mounted read-write, which decides
// whether the installer can register a boot entry with efibootmgr. Plenty of
// systems boot UEFI with efivarfs read-only (or absent in a container), and in
// that case the installer must fall back to the removable-media path
// \EFI\BOOT\BOOTX64.EFI instead of failing the install.
func EFIVarsWritable() bool {
	const path = "/sys/firmware/efi/efivars"
	st, err := os.Stat(path)
	if err != nil || !st.IsDir() {
		return false
	}
	mounts, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(mounts), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[1] != path {
			continue
		}
		for _, opt := range strings.Split(fields[3], ",") {
			if opt == "rw" {
				return true
			}
		}
	}
	return false
}

// DebArch maps the Go architecture to the Debian architecture name used
// throughout the build system and package lists.
func DebArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	default:
		return runtime.GOARCH
	}
}

// EFIStub is the removable-media fallback filename for this architecture, the
// path firmware looks for when no NVRAM boot entry exists.
func EFIStub() string {
	switch DebArch() {
	case "arm64":
		return "BOOTAA64.EFI"
	default:
		return "BOOTX64.EFI"
	}
}

// IsRoot reports whether the process can actually partition disks.
func IsRoot() bool { return os.Geteuid() == 0 }

// IsLiveISO reports whether we are running from AndersXn live media, which the
// installer uses to decide whether it is safe to offer the booted device as an
// install target.
func IsLiveISO() bool {
	if _, err := os.Stat("/run/live/medium"); err == nil {
		return true
	}
	cmdline, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return false
	}
	return strings.Contains(string(cmdline), "boot=live")
}

// HasVirtualisation reports whether the CPU exposes hardware virtualisation,
// which the provisioning step uses to decide whether K3s and hardware
// transcoding are worth offering prominently.
func HasVirtualisation() bool {
	info, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return false
	}
	s := string(info)
	return strings.Contains(s, "vmx") || strings.Contains(s, "svm")
}
