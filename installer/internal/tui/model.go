// Package tui implements the AX-Installer terminal interface.
//
// It is a Bubble Tea program: a single Model holds the whole wizard, Update
// advances it, and View renders it. Every screen is drawn through the same
// Theme.Header, which is what puts the AndersXn mark above every step.
package tui

import (
	"context"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ggstudios/andersxn-os/installer/internal/install"
	"github.com/ggstudios/andersxn-os/installer/internal/provision"
	"github.com/ggstudios/andersxn-os/installer/internal/sys"
)

// step identifies a wizard screen.
type step int

const (
	stepWelcome step = iota
	stepDisk
	stepLayout
	stepSystem
	stepAccount
	stepModules
	stepReview
	stepInstall
	stepDone
)

// stepNames drives the breadcrumb in the header. Install and Done are not
// shown: once the install starts, going back is no longer meaningful.
var stepNames = []string{
	"Welcome", "Disk", "Layout", "System", "Account", "Software", "Review",
}

// field indexes for the multi-input screens.
const (
	sysHostname = iota
	sysTimezone
	sysLocale
	sysKeymap
	sysFieldCount
)

const (
	accUsername = iota
	accFullName
	accPassword
	accConfirm
	accSSHKey
	accFieldCount
)

// Model is the whole installer state.
type Model struct {
	theme  Theme
	width  int
	height int

	step step
	plan install.Plan
	err  error

	// Disk selection
	disks      []sys.Disk
	diskCursor int
	loadingDsk bool

	// Layout selection
	layoutCursor int
	fsCursor     int
	layoutFocus  int // 0 = scheme list, 1 = filesystem list, 2 = encryption

	// Text screens
	sysInputs []textinput.Model
	sysFocus  int
	accInputs []textinput.Model
	accFocus  int

	// Module selection
	modules      []provision.Module
	moduleCursor int
	selected     map[string]bool

	// Review
	reviewConfirmed bool

	// Install run
	runner     *sys.ExecRunner
	events     chan tea.Msg
	logLines   []string
	logOffset  int
	progress   install.Progress
	installing bool
	cancel     context.CancelFunc

	// Options
	dryRun bool
}

// New builds the initial model.
func New(dryRun bool) Model {
	plan := install.NewPlan()
	plan.DryRun = dryRun

	m := Model{
		theme:      NewTheme(),
		step:       stepWelcome,
		plan:       plan,
		loadingDsk: true,
		selected:   map[string]bool{},
		modules:    provision.ForArch(plan.Arch),
		events:     make(chan tea.Msg, 256),
		dryRun:     dryRun,
		width:      80,
		height:     24,
	}

	for _, id := range provision.Defaults(plan.Arch) {
		m.selected[id] = true
	}

	m.sysInputs = buildInputs([]inputSpec{
		{label: "Hostname", value: plan.Hostname, placeholder: "andersxn"},
		{label: "Timezone", value: plan.Timezone, placeholder: "Europe/Oslo"},
		{label: "Locale", value: plan.Locale, placeholder: "en_US.UTF-8"},
		{label: "Keymap", value: plan.Keymap, placeholder: "us"},
	})
	m.accInputs = buildInputs([]inputSpec{
		{label: "Username", placeholder: "operator"},
		{label: "Full name", placeholder: "optional"},
		{label: "Password", mask: true},
		{label: "Confirm password", mask: true},
		{label: "SSH public key", placeholder: "ssh-ed25519 AAAA... (optional)"},
	})
	m.sysInputs[sysHostname].Focus()

	return m
}

type inputSpec struct {
	label       string
	value       string
	placeholder string
	mask        bool
}

func buildInputs(specs []inputSpec) []textinput.Model {
	out := make([]textinput.Model, len(specs))
	for i, s := range specs {
		ti := textinput.New()
		ti.Prompt = ""
		ti.SetValue(s.value)
		ti.Placeholder = s.placeholder
		ti.CharLimit = 512
		ti.Width = 40
		if s.mask {
			ti.EchoMode = textinput.EchoPassword
			ti.EchoCharacter = '*'
		}
		out[i] = ti
	}
	return out
}

// Init loads the disk list before the operator reaches the disk screen.
func (m Model) Init() tea.Cmd {
	return tea.Batch(loadDisks(), textinput.Blink)
}
