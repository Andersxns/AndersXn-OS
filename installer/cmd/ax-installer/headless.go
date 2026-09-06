package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ggstudios/andersxn-os/installer/internal/api"
	"github.com/ggstudios/andersxn-os/installer/internal/branding"
	"github.com/ggstudios/andersxn-os/installer/internal/install"
	"github.com/ggstudios/andersxn-os/installer/internal/provision"
	"github.com/ggstudios/andersxn-os/installer/internal/sys"
)

// runProbe writes one api.Probe object to stdout and exits.
//
// Deliberately usable without root: the GTK front end calls this before it
// escalates, so it can draw a populated window and explain what is wrong
// rather than showing an empty one.
func runProbe() int {
	r := sys.NewExecRunner(nil)
	arch := sys.DebArch()

	p := api.Probe{
		Name:     branding.Name,
		Version:  branding.Version,
		Codename: branding.Codename,
		Vendor:   branding.Vendor,
		Arch:     arch,
		Firmware: sys.DetectFirmware().String(),
		IsRoot:   sys.IsRoot(),
		IsLive:   sys.IsLiveISO(),
		Defaults: api.Defaults{
			Hostname: "andersxn",
			Timezone: "UTC",
			Locale:   "en_US.UTF-8",
			Keymap:   "us",
			FS:       string(install.FSBtrfs),
		},
		Disks:   []api.Disk{},
		Modules: []api.Module{},
	}

	for _, m := range provision.ForArch(arch) {
		p.Modules = append(p.Modules, api.ModuleFrom(m))
	}

	disks, err := sys.ListDisks(context.Background(), r)
	if err != nil {
		p.Warning = "could not enumerate disks: " + err.Error()
	}
	for _, d := range disks {
		wd := api.Disk{
			Path: d.Path, Name: d.Name,
			SizeBytes: d.SizeBytes, SizeHuman: sys.HumanSize(d.SizeBytes),
			Model: d.Model, Transport: d.Transport,
			Rotational: d.Rotational, Removable: d.Removable,
			ReadOnly: d.ReadOnly, Mounted: d.Mounted,
			Describe:   d.Describe(),
			Partitions: []api.Partition{},
		}
		for _, part := range d.Partitions {
			wd.Partitions = append(wd.Partitions, api.Partition{
				Path: part.Path, SizeBytes: part.SizeBytes,
				SizeHuman: sys.HumanSize(part.SizeBytes),
				FSType:    part.FSType, Label: part.Label,
				MountPoint: part.MountPoint,
			})
		}
		p.Disks = append(p.Disks, wd)
	}

	if !p.IsRoot && p.Warning == "" {
		p.Warning = "not running as root - the install will fail at the first write"
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(p); err != nil {
		fmt.Fprintf(os.Stderr, "encoding probe: %v\n", err)
		return 1
	}
	return 0
}

// runPlan executes a plan file, streaming newline-delimited api.Event to
// stdout. Secrets are read from stdin, never from the plan file.
func runPlan(path string) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		return emitFatal("reading plan: %v", err)
	}

	var req api.PlanRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return emitFatal("parsing plan: %v", err)
	}

	var secrets api.Secrets
	// stdin is optional: a passwordless, key-only install is legitimate.
	if st, err := os.Stdin.Stat(); err == nil && (st.Mode()&os.ModeCharDevice) == 0 {
		if err := json.NewDecoder(bufio.NewReader(os.Stdin)).Decode(&secrets); err != nil {
			return emitFatal("parsing secrets from stdin: %v", err)
		}
	}

	plan, err := planFromRequest(req, secrets)
	if err != nil {
		return emitFatal("%v", err)
	}
	if err := plan.Validate(); err != nil {
		return emitFatal("plan is not valid: %v", err)
	}

	out := json.NewEncoder(os.Stdout)
	emit := func(e api.Event) {
		_ = out.Encode(e)
		// The front end reads line by line and must see progress as it
		// happens, not when the pipe buffer happens to flush.
		os.Stdout.Sync()
	}

	runner := sys.NewExecRunner(func(line string) {
		emit(api.Event{Type: "log", Line: line})
	})
	runner.DryRun = plan.DryRun

	engine := install.NewEngine(plan, runner, func(line string) {
		emit(api.Event{Type: "log", Line: line})
	})
	engine.OnProgress = func(pr install.Progress) {
		if pr.Err != nil {
			return // the error is reported once, by Execute's return value
		}
		emit(api.Event{
			Type: "progress", Step: pr.Step, Total: pr.Total,
			Name: pr.Name, Fraction: pr.Fraction(),
		})
	}

	if err := engine.Execute(context.Background()); err != nil {
		emit(api.Event{Type: "error", Message: err.Error()})
		return 1
	}
	emit(api.Event{Type: "done", OK: true})
	return 0
}

// planFromRequest maps the wire format onto the internal Plan, resolving the
// disk path against what is actually present.
func planFromRequest(req api.PlanRequest, secrets api.Secrets) (install.Plan, error) {
	plan := install.NewPlan()

	if req.Disk != "" {
		disks, err := sys.ListDisks(context.Background(), sys.NewExecRunner(nil))
		if err != nil {
			return plan, fmt.Errorf("enumerating disks: %w", err)
		}
		found := false
		for _, d := range disks {
			if d.Path == req.Disk {
				plan.Disk = d
				found = true
				break
			}
		}
		if !found {
			return plan, fmt.Errorf("target disk %q is no longer present", req.Disk)
		}
	}

	if req.Scheme != "" {
		plan.Scheme = install.Scheme(req.Scheme)
	}
	if req.FS != "" {
		plan.FS = install.FSType(req.FS)
	}
	plan.Encrypt = req.Encrypt

	if req.Hostname != "" {
		plan.Hostname = req.Hostname
	}
	if req.Timezone != "" {
		plan.Timezone = req.Timezone
	}
	if req.Locale != "" {
		plan.Locale = req.Locale
	}
	if req.Keymap != "" {
		plan.Keymap = req.Keymap
	}

	plan.Username = req.Username
	plan.FullName = req.FullName
	plan.SSHKey = req.SSHKey
	plan.Password = secrets.Password
	plan.LUKSPass = secrets.LUKSPass
	plan.DisablePw = req.SSHKey != ""
	plan.Modules = req.Modules
	plan.DryRun = req.DryRun

	return plan, nil
}

func emitFatal(format string, a ...any) int {
	e := api.Event{Type: "error", Message: fmt.Sprintf(format, a...)}
	_ = json.NewEncoder(os.Stdout).Encode(e)
	return 1
}
