// Package api is the wire format between the AX-Installer engine and any front
// end that is not written in Go.
//
// The GTK installer is Python (PyGObject), chosen so the graphical front end
// needs no compilation at all - which matters because AndersXn images are
// routinely cross-built, and a CGO toolkit binding would drag a full
// cross-compiled GTK stack into the build. The two processes talk newline-
// delimited JSON:
//
//	ax-installer --probe              -> one Probe object on stdout
//	ax-installer --run-plan plan.json -> a stream of Event objects on stdout
//
// Secrets never appear in the plan file or in argv: --run-plan reads a Secrets
// object from stdin.
package api

import "github.com/ggstudios/andersxn-os/installer/internal/provision"

// Probe is everything a front end needs to draw the wizard.
type Probe struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Codename string `json:"codename"`
	Vendor   string `json:"vendor"`

	Arch     string `json:"arch"`
	Firmware string `json:"firmware"`
	IsRoot   bool   `json:"is_root"`
	IsLive   bool   `json:"is_live"`

	Defaults Defaults `json:"defaults"`
	Disks    []Disk   `json:"disks"`
	Modules  []Module `json:"modules"`

	// Warning is set when the machine can be inspected but not installed to,
	// e.g. the process is not root. Front ends surface it and keep going.
	Warning string `json:"warning,omitempty"`
}

// Defaults seed the form fields.
type Defaults struct {
	Hostname string `json:"hostname"`
	Timezone string `json:"timezone"`
	Locale   string `json:"locale"`
	Keymap   string `json:"keymap"`
	FS       string `json:"fs"`
}

// Disk is one installable target.
type Disk struct {
	Path       string      `json:"path"`
	Name       string      `json:"name"`
	SizeBytes  uint64      `json:"size_bytes"`
	SizeHuman  string      `json:"size_human"`
	Model      string      `json:"model"`
	Transport  string      `json:"transport"`
	Rotational bool        `json:"rotational"`
	Removable  bool        `json:"removable"`
	ReadOnly   bool        `json:"read_only"`
	Mounted    bool        `json:"mounted"`
	Describe   string      `json:"describe"`
	Partitions []Partition `json:"partitions"`
}

// Partition is an existing partition on a Disk, shown so the operator can see
// what they are about to destroy.
type Partition struct {
	Path       string `json:"path"`
	SizeBytes  uint64 `json:"size_bytes"`
	SizeHuman  string `json:"size_human"`
	FSType     string `json:"fstype"`
	Label      string `json:"label"`
	MountPoint string `json:"mountpoint"`
}

// Module mirrors provision.Module in a shape a front end can render directly.
type Module struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Summary   string   `json:"summary"`
	Detail    string   `json:"detail"`
	Category  string   `json:"category"`
	Requires  []string `json:"requires"`
	Ports     []int    `json:"ports"`
	DefaultOn bool     `json:"default_on"`
	DiskGiB   int      `json:"disk_gib"`
}

// ModuleFrom converts a catalog entry to its wire form.
func ModuleFrom(m provision.Module) Module {
	requires := m.Requires
	if requires == nil {
		requires = []string{}
	}
	ports := m.Ports
	if ports == nil {
		ports = []int{}
	}
	return Module{
		ID: m.ID, Name: m.Name, Summary: m.Summary, Detail: m.Detail,
		Category: string(m.Category), Requires: requires, Ports: ports,
		DefaultOn: m.DefaultOn, DiskGiB: m.DiskGiB,
	}
}

// PlanRequest is what a front end writes to the plan file.
//
// Deliberately flat and stringly-typed: it is a wire format, and keeping it
// decoupled from the internal Plan struct means the engine can be refactored
// without breaking a front end that is not rebuilt at the same time.
type PlanRequest struct {
	Disk    string `json:"disk"`
	Scheme  string `json:"scheme"`
	FS      string `json:"fs"`
	Encrypt bool   `json:"encrypt"`

	Hostname string `json:"hostname"`
	Timezone string `json:"timezone"`
	Locale   string `json:"locale"`
	Keymap   string `json:"keymap"`

	Username string `json:"username"`
	FullName string `json:"full_name"`
	SSHKey   string `json:"ssh_key"`

	Modules []string `json:"modules"`
	DryRun  bool     `json:"dry_run"`
}

// Secrets arrive on stdin so they are never written to disk or visible in
// /proc/<pid>/cmdline.
type Secrets struct {
	Password string `json:"password"`
	LUKSPass string `json:"luks_passphrase"`
}

// Event is one line of the progress stream.
type Event struct {
	Type     string  `json:"type"` // progress | log | done | error
	Step     int     `json:"step,omitempty"`
	Total    int     `json:"total,omitempty"`
	Name     string  `json:"name,omitempty"`
	Fraction float64 `json:"fraction,omitempty"`
	Line     string  `json:"line,omitempty"`
	Message  string  `json:"message,omitempty"`
	OK       bool    `json:"ok,omitempty"`
}
