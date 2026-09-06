package install

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ggstudios/andersxn-os/installer/internal/sys"
)

// TargetRoot is where the new system is assembled before it is unmounted.
const TargetRoot = "/mnt/axos"

// Progress is emitted before each step and on notable events within one.
type Progress struct {
	Step    int
	Total   int
	Name    string
	Message string
	Done    bool
	Err     error
}

// Fraction reports overall completion in the range 0..1.
func (p Progress) Fraction() float64 {
	if p.Total == 0 {
		return 0
	}
	return float64(p.Step) / float64(p.Total)
}

// Step is one named unit of the install.
//
// Run is shaped to accept a method expression on *Engine, so the pipeline in
// Steps reads as a list of method names rather than a list of closures.
type Step struct {
	Name string
	Run  func(*Engine, context.Context) error
}

// Engine carries out a Plan.
type Engine struct {
	Plan Plan
	R    sys.Runner
	Root string

	// Log receives human-readable progress lines; OnProgress drives the UI.
	Log        func(string)
	OnProgress func(Progress)

	// Discovered while running; later steps read these.
	rootUUID  string
	bootUUID  string
	kernelVer string

	// mounted tracks what Execute mounted, so Cleanup can unwind in reverse
	// even when a step fails halfway.
	mounted []string
}

// NewEngine wires an Engine with sensible defaults.
func NewEngine(p Plan, r sys.Runner, log func(string)) *Engine {
	if log == nil {
		log = func(string) {}
	}
	return &Engine{Plan: p, R: r, Root: TargetRoot, Log: log}
}

func (e *Engine) logf(format string, a ...any) {
	e.Log(fmt.Sprintf(format, a...))
}

// Steps returns the ordered install pipeline for this plan. Steps that do not
// apply (LUKS on an unencrypted install, EFI work on a BIOS machine) are simply
// not included, so the progress count the operator sees is always accurate.
func (e *Engine) Steps() []Step {
	steps := []Step{
		{Name: "Verify target", Run: (*Engine).stepVerify},
		{Name: "Partition disk", Run: (*Engine).stepPartition},
	}
	if e.Plan.Encrypt {
		steps = append(steps, Step{Name: "Set up encryption", Run: (*Engine).stepLUKS})
	}
	steps = append(steps,
		Step{Name: "Create filesystems", Run: (*Engine).stepFilesystems},
		Step{Name: "Mount target", Run: (*Engine).stepMount},
		Step{Name: "Copy system", Run: (*Engine).stepCopySystem},
		Step{Name: "Write fstab", Run: (*Engine).stepFstab},
		Step{Name: "Configure system", Run: (*Engine).stepConfigure},
		Step{Name: "Create account", Run: (*Engine).stepAccount},
		Step{Name: "Install bootloader", Run: (*Engine).stepBootloader},
	)
	if len(e.Plan.Modules) > 0 {
		steps = append(steps, Step{Name: "Provision software", Run: (*Engine).stepProvision})
	}
	steps = append(steps,
		Step{Name: "Finalise", Run: (*Engine).stepFinalise},
	)
	return steps
}

// Execute runs the pipeline. On failure it unwinds every mount it made before
// returning, so the operator can correct the problem and try again without
// rebooting the live medium.
func (e *Engine) Execute(ctx context.Context) error {
	steps := e.Steps()
	total := len(steps)

	defer func() {
		if err := e.Cleanup(context.WithoutCancel(ctx)); err != nil {
			e.logf("cleanup: %v", err)
		}
	}()

	for i, s := range steps {
		if err := ctx.Err(); err != nil {
			return err
		}

		e.progress(Progress{Step: i, Total: total, Name: s.Name})
		e.logf("== %s", s.Name)

		start := time.Now()
		if err := s.Run(e, ctx); err != nil {
			wrapped := fmt.Errorf("%s: %w", s.Name, err)
			e.progress(Progress{Step: i, Total: total, Name: s.Name, Err: wrapped})
			return wrapped
		}
		e.logf("   done in %s", time.Since(start).Round(time.Millisecond))
	}

	e.progress(Progress{Step: total, Total: total, Name: "Complete", Done: true})
	return nil
}

func (e *Engine) progress(p Progress) {
	if e.OnProgress != nil {
		e.OnProgress(p)
	}
}

// mount records a mount so Cleanup can reverse it.
func (e *Engine) mount(ctx context.Context, args ...string) error {
	if err := e.R.Run(ctx, "mount", args...); err != nil {
		return err
	}
	e.mounted = append(e.mounted, args[len(args)-1])
	return nil
}

// Cleanup unmounts everything the engine mounted, deepest first, and closes the
// LUKS mapping. Safe to call more than once.
func (e *Engine) Cleanup(ctx context.Context) error {
	var firstErr error
	for i := len(e.mounted) - 1; i >= 0; i-- {
		if err := e.R.Run(ctx, "umount", "-R", e.mounted[i]); err != nil {
			// A lazy unmount still lets the operator retry; a hard failure here
			// would strand the target disk until reboot.
			if err2 := e.R.Run(ctx, "umount", "-lf", e.mounted[i]); err2 != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	e.mounted = nil

	if e.Plan.Encrypt {
		if _, err := os.Stat("/dev/mapper/" + LUKSName); err == nil {
			_ = e.R.Run(ctx, "cryptsetup", "close", LUKSName)
		}
	}
	return firstErr
}

// stepVerify refuses installs that would obviously destroy the wrong thing.
func (e *Engine) stepVerify(ctx context.Context) error {
	if err := e.Plan.Validate(); err != nil {
		return err
	}
	if err := e.checkTools(); err != nil {
		return err
	}
	if !e.Plan.DryRun && !sys.IsRoot() {
		return fmt.Errorf("ax-installer must run as root")
	}

	// Refusing a mounted target is the single most valuable guard here: on live
	// media the booted USB stick is a plausible-looking install target, and
	// wiping it mid-install destroys the running system.
	if e.Plan.Scheme != SchemeManual && e.Plan.Disk.Mounted {
		return fmt.Errorf(
			"%s has mounted partitions - it may be the live medium you booted from; "+
				"unmount it first if you really mean to erase it", e.Plan.Disk.Path)
	}
	e.logf("target %s (%s), %s firmware, arch %s",
		e.Plan.Disk.Path, sys.HumanSize(e.Plan.Disk.SizeBytes),
		e.Plan.Firmware, e.Plan.Arch)
	return nil
}

// requiredTools returns the host binaries this plan actually needs.
//
// The list is plan-specific rather than a fixed roster: there is no point
// demanding cryptsetup for an unencrypted install, and a missing tool should
// name something the operator actually chose.
func (e *Engine) requiredTools() []string {
	tools := []string{
		"wipefs", "sgdisk", "partprobe", "udevadm", "blkid",
		"mount", "umount", "mkfs.vfat", "install", "chroot", "sync",
	}

	switch e.Plan.FS {
	case FSBtrfs:
		tools = append(tools, "mkfs.btrfs", "btrfs", "chattr")
	case FSExt4:
		tools = append(tools, "mkfs.ext4")
	case FSXFS:
		tools = append(tools, "mkfs.xfs")
	}

	if e.Plan.Encrypt {
		tools = append(tools, "cryptsetup")
	}

	// The copy step uses whichever source is actually present, so require the
	// tool that matches. This is the check that matters most: it is the one
	// that previously failed AFTER the disk had been partitioned and formatted.
	if findSquashfs() != "" {
		tools = append(tools, "unsquashfs")
	} else {
		tools = append(tools, "rsync")
	}

	return tools
}

// checkTools reports every missing binary at once.
//
// Called from stepVerify, before anything is written. A missing tool
// discovered halfway through is far worse than a missing tool discovered up
// front: by then the partition table is gone and the filesystems are new.
func (e *Engine) checkTools() error {
	var missing []string
	for _, t := range e.requiredTools() {
		if _, err := exec.LookPath(t); err != nil {
			missing = append(missing, t)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf(
		"this live medium is missing tools the install needs: %s\n"+
			"    Nothing has been written to disk. Install them and run the "+
			"installer again.", strings.Join(missing, ", "))
}
