package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ggstudios/andersxn-os/installer/internal/install"
	"github.com/ggstudios/andersxn-os/installer/internal/provision"
	"github.com/ggstudios/andersxn-os/installer/internal/sys"
)

// --- messages ---------------------------------------------------------------

type disksMsg struct {
	disks []sys.Disk
	err   error
}

type logMsg string

type progressMsg install.Progress

type doneMsg struct{ err error }

// --- commands ---------------------------------------------------------------

// loadDisks enumerates block devices off the UI goroutine.
func loadDisks() tea.Cmd {
	return func() tea.Msg {
		r := sys.NewExecRunner(nil)
		disks, err := sys.ListDisks(context.Background(), r)
		return disksMsg{disks: disks, err: err}
	}
}

// waitForEvent blocks on the install event channel and turns the next item into
// a Bubble Tea message. Re-issued after every event, which is the standard way
// to stream from a goroutine into the update loop.
func waitForEvent(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

// startInstall launches the engine in the background, feeding m.events.
func (m *Model) startInstall() tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.installing = true

	ch := m.events
	plan := m.plan

	runner := sys.NewExecRunner(func(line string) {
		// Non-blocking: a stalled UI must never deadlock the install.
		select {
		case ch <- logMsg(line):
		default:
		}
	})
	runner.DryRun = plan.DryRun
	m.runner = runner

	go func() {
		defer cancel()
		engine := install.NewEngine(plan, runner, func(line string) {
			select {
			case ch <- logMsg(line):
			default:
			}
		})
		engine.OnProgress = func(p install.Progress) {
			ch <- progressMsg(p)
		}
		err := engine.Execute(ctx)
		ch <- doneMsg{err: err}
	}()

	return waitForEvent(ch)
}

// --- update -----------------------------------------------------------------

// Update advances the wizard.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case disksMsg:
		m.loadingDsk = false
		m.disks, m.err = msg.disks, msg.err
		if len(m.disks) > 0 {
			m.plan.Disk = m.disks[0]
		}
		return m, nil

	case logMsg:
		m.logLines = append(m.logLines, string(msg))
		// The log pane only ever scrolls forward; keeping the whole history in
		// memory is fine (an install produces a few thousand lines) but the
		// view only renders the tail.
		return m, waitForEvent(m.events)

	case progressMsg:
		m.progress = install.Progress(msg)
		return m, waitForEvent(m.events)

	case doneMsg:
		m.installing = false
		m.err = msg.err
		m.step = stepDone
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// handleKey routes a keypress to the active screen.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Ctrl+C always works. During an install it cancels the run rather than
	// killing the process outright, so the engine can unwind its mounts.
	if msg.Type == tea.KeyCtrlC {
		if m.installing && m.cancel != nil {
			m.cancel()
			return m, nil
		}
		return m, tea.Quit
	}

	switch m.step {
	case stepWelcome:
		return m.keyWelcome(msg)
	case stepDisk:
		return m.keyDisk(msg)
	case stepLayout:
		return m.keyLayout(msg)
	case stepSystem:
		return m.keySystem(msg)
	case stepAccount:
		return m.keyAccount(msg)
	case stepModules:
		return m.keyModules(msg)
	case stepReview:
		return m.keyReview(msg)
	case stepInstall:
		return m.keyInstall(msg)
	case stepDone:
		return m.keyDone(msg)
	}
	return m, nil
}

func (m Model) keyWelcome(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "right", "l":
		m.step = stepDisk
	case "q", "esc":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) keyDisk(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.diskCursor > 0 {
			m.diskCursor--
		}
	case "down", "j":
		if m.diskCursor < len(m.disks)-1 {
			m.diskCursor++
		}
	case "r":
		m.loadingDsk = true
		return m, loadDisks()
	case "enter", "right", "l":
		if len(m.disks) == 0 {
			return m, nil
		}
		m.plan.Disk = m.disks[m.diskCursor]
		m.step = stepLayout
	case "left", "h", "esc":
		m.step = stepWelcome
	}
	return m, nil
}

func (m Model) keyLayout(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	schemes := layoutSchemes()
	filesystems := layoutFilesystems()

	switch msg.String() {
	case "tab":
		m.layoutFocus = (m.layoutFocus + 1) % 3
	case "shift+tab":
		m.layoutFocus = (m.layoutFocus + 2) % 3
	case "up", "k":
		switch m.layoutFocus {
		case 0:
			if m.layoutCursor > 0 {
				m.layoutCursor--
			}
		case 1:
			if m.fsCursor > 0 {
				m.fsCursor--
			}
		}
	case "down", "j":
		switch m.layoutFocus {
		case 0:
			if m.layoutCursor < len(schemes)-1 {
				m.layoutCursor++
			}
		case 1:
			if m.fsCursor < len(filesystems)-1 {
				m.fsCursor++
			}
		}
	case " ":
		if m.layoutFocus == 2 {
			m.plan.Encrypt = !m.plan.Encrypt
		}
	case "enter", "right", "l":
		m.plan.Scheme = schemes[m.layoutCursor].scheme
		m.plan.FS = filesystems[m.fsCursor].fs
		m.step = stepSystem
		m.sysInputs[m.sysFocus].Focus()
	case "left", "h", "esc":
		m.step = stepDisk
	}
	return m, nil
}

// keySystem drives the hostname/timezone/locale/keymap form.
func (m Model) keySystem(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.step = stepLayout
		return m, nil
	case "tab", "down":
		m.sysFocus = (m.sysFocus + 1) % sysFieldCount
		return m, m.focusOnly(m.sysInputs, m.sysFocus)
	case "shift+tab", "up":
		m.sysFocus = (m.sysFocus + sysFieldCount - 1) % sysFieldCount
		return m, m.focusOnly(m.sysInputs, m.sysFocus)
	case "enter":
		m.plan.Hostname = strings.TrimSpace(m.sysInputs[sysHostname].Value())
		m.plan.Timezone = strings.TrimSpace(m.sysInputs[sysTimezone].Value())
		m.plan.Locale = strings.TrimSpace(m.sysInputs[sysLocale].Value())
		m.plan.Keymap = strings.TrimSpace(m.sysInputs[sysKeymap].Value())
		m.step = stepAccount
		return m, m.focusOnly(m.accInputs, m.accFocus)
	}

	var cmd tea.Cmd
	m.sysInputs[m.sysFocus], cmd = m.sysInputs[m.sysFocus].Update(msg)
	return m, cmd
}

func (m Model) keyAccount(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.step = stepSystem
		return m, m.focusOnly(m.sysInputs, m.sysFocus)
	case "tab", "down":
		m.accFocus = (m.accFocus + 1) % accFieldCount
		return m, m.focusOnly(m.accInputs, m.accFocus)
	case "shift+tab", "up":
		m.accFocus = (m.accFocus + accFieldCount - 1) % accFieldCount
		return m, m.focusOnly(m.accInputs, m.accFocus)
	case "enter":
		m.plan.Username = strings.TrimSpace(m.accInputs[accUsername].Value())
		m.plan.FullName = strings.TrimSpace(m.accInputs[accFullName].Value())
		m.plan.Password = m.accInputs[accPassword].Value()
		m.plan.SSHKey = strings.TrimSpace(m.accInputs[accSSHKey].Value())
		// A key on file makes password auth dead weight and a standing
		// brute-force target, so turn it off by default when one is supplied.
		m.plan.DisablePw = m.plan.SSHKey != ""

		if m.plan.Password != m.accInputs[accConfirm].Value() {
			m.err = errPasswordMismatch
			return m, nil
		}
		m.err = nil
		m.step = stepModules
		return m, nil
	}

	var cmd tea.Cmd
	m.accInputs[m.accFocus], cmd = m.accInputs[m.accFocus].Update(msg)
	return m, cmd
}

// focusOnly gives the keyboard to exactly one field in a form.
func (m Model) focusOnly(inputs []textinput.Model, idx int) tea.Cmd {
	var cmd tea.Cmd
	for i := range inputs {
		if i == idx {
			cmd = inputs[i].Focus()
		} else {
			inputs[i].Blur()
		}
	}
	return cmd
}

func (m Model) keyModules(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.moduleCursor > 0 {
			m.moduleCursor--
		}
	case "down", "j":
		if m.moduleCursor < len(m.modules)-1 {
			m.moduleCursor++
		}
	case " ", "x":
		if len(m.modules) > 0 {
			id := m.modules[m.moduleCursor].ID
			m.selected[id] = !m.selected[id]
		}
	case "a":
		for _, mod := range m.modules {
			m.selected[mod.ID] = true
		}
	case "n":
		m.selected = map[string]bool{}
	case "enter", "right", "l":
		m.plan.Modules = m.selectedIDs()
		m.step = stepReview
	case "left", "h", "esc":
		m.step = stepAccount
		return m, m.focusOnly(m.accInputs, m.accFocus)
	}
	return m, nil
}

// selectedIDs returns the ticked module IDs in catalog order.
func (m Model) selectedIDs() []string {
	var ids []string
	for _, mod := range m.modules {
		if m.selected[mod.ID] {
			ids = append(ids, mod.ID)
		}
	}
	full, _ := provision.ResolveDependencies(ids)
	return full
}

func (m Model) keyReview(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "left", "h", "esc":
		m.step = stepModules
		m.reviewConfirmed = false
	case "enter":
		if err := m.plan.Validate(); err != nil {
			m.err = err
			return m, nil
		}
		m.err = nil
		// Two deliberate keypresses before anything is erased. The first arms
		// the confirmation, the second starts the install.
		if !m.reviewConfirmed {
			m.reviewConfirmed = true
			return m, nil
		}
		m.step = stepInstall
		return m, m.startInstall()
	}
	return m, nil
}

func (m Model) keyInstall(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.logOffset < len(m.logLines) {
			m.logOffset++
		}
	case "down", "j":
		if m.logOffset > 0 {
			m.logOffset--
		}
	case "end", "G":
		m.logOffset = 0
	}
	return m, nil
}

func (m Model) keyDone(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "r":
		if m.err == nil {
			return m, tea.Sequence(tea.Quit, rebootCmd())
		}
	case "q", "enter", "esc":
		return m, tea.Quit
	case "up", "k":
		if m.logOffset < len(m.logLines) {
			m.logOffset++
		}
	case "down", "j":
		if m.logOffset > 0 {
			m.logOffset--
		}
	}
	return m, nil
}

// rebootCmd reboots after the program has released the terminal.
func rebootCmd() tea.Cmd {
	return func() tea.Msg {
		r := sys.NewExecRunner(nil)
		_ = r.Run(context.Background(), "systemctl", "reboot")
		return nil
	}
}
