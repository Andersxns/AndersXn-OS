package install

import (
	"os"
	"path/filepath"
	"testing"
)

// An arm64 image carries Debian's kernel and the Raspberry Pi ones side by
// side, and the Pi builds have the higher version. Limine must still be handed
// Debian's: a generic arm64 UEFI machine cannot boot a kernel built for a Pi.
func TestFindKernelPrefersDebianOverRaspberryPi(t *testing.T) {
	root := t.TempDir()
	boot := filepath.Join(root, BootMountPoint)
	if err := os.MkdirAll(boot, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"vmlinuz-6.12.107+deb13-arm64", "initrd.img-6.12.107+deb13-arm64",
		"vmlinuz-6.18.39+rpt-rpi-2712", "initrd.img-6.18.39+rpt-rpi-2712",
		"vmlinuz-6.18.39+rpt-rpi-v8", "initrd.img-6.18.39+rpt-rpi-v8",
	} {
		if err := os.WriteFile(filepath.Join(boot, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	e := &Engine{Root: root}
	kernel, initrd, err := e.findKernel()
	if err != nil {
		t.Fatalf("findKernel: %v", err)
	}
	if kernel != "vmlinuz-6.12.107+deb13-arm64" {
		t.Errorf("kernel = %q, want the Debian one despite its lower version", kernel)
	}
	if initrd != "initrd.img-6.12.107+deb13-arm64" {
		t.Errorf("initrd = %q, want the matching Debian initramfs", initrd)
	}
}

// With no Debian kernel present, the newest of whatever exists is still used
// rather than failing outright.
func TestFindKernelFallsBackWhenOnlyPiKernelsExist(t *testing.T) {
	root := t.TempDir()
	boot := filepath.Join(root, BootMountPoint)
	if err := os.MkdirAll(boot, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"vmlinuz-6.18.39+rpt-rpi-v8", "initrd.img-6.18.39+rpt-rpi-v8",
	} {
		if err := os.WriteFile(filepath.Join(boot, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e := &Engine{Root: root}
	kernel, _, err := e.findKernel()
	if err != nil {
		t.Fatalf("findKernel: %v", err)
	}
	if kernel != "vmlinuz-6.18.39+rpt-rpi-v8" {
		t.Errorf("kernel = %q, want the only kernel available", kernel)
	}
}
