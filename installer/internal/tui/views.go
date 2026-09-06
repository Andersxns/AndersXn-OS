package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	"github.com/ggstudios/andersxn-os/installer/internal/install"
	"github.com/ggstudios/andersxn-os/installer/internal/provision"
	"github.com/ggstudios/andersxn-os/installer/internal/sys"
)

// --- selectable option tables ----------------------------------------------

type schemeOption struct {
	scheme install.Scheme
	name   string
	desc   string
}

func layoutSchemes() []schemeOption {
	return []schemeOption{
		{install.SchemeWipe, "Erase disk",
			"Wipe the whole disk and take all of it. Everything on it is lost."},
		{install.SchemeManual, "Use existing partitions",
			"You have already made a root partition and a FAT boot partition."},
		{install.SchemeFreeSpace, "Use free space",
			"Install alongside what is there. Not implemented yet."},
	}
}

type fsOption struct {
	fs   install.FSType
	name string
	desc string
}

func layoutFilesystems() []fsOption {
	return []fsOption{
		{install.FSBtrfs, "btrfs (recommended)",
			"Subvolumes, snapshots and zstd compression. The AndersXn default."},
		{install.FSExt4, "ext4",
			"Conservative and universally understood. No snapshots."},
		{install.FSXFS, "xfs",
			"Strong with large files and parallel I/O. No shrink, no snapshots."},
	}
}

// --- top-level view ---------------------------------------------------------

// View renders the current screen. The branded header is drawn first on every
// step without exception, which is what the AX-Installer specification calls
// for; the body is then given whatever vertical space is left.
func (m Model) View() string {
	crumb := int(m.step)
	if crumb >= len(stepNames) {
		crumb = len(stepNames) - 1
	}
	header := m.theme.Header(m.width, m.height, stepNames, crumb)

	var body, footer string
	switch m.step {
	case stepWelcome:
		body, footer = m.viewWelcome()
	case stepDisk:
		body, footer = m.viewDisk()
	case stepLayout:
		body, footer = m.viewLayout()
	case stepSystem:
		body, footer = m.viewSystem()
	case stepAccount:
		body, footer = m.viewAccount()
	case stepModules:
		body, footer = m.viewModules()
	case stepReview:
		body, footer = m.viewReview()
	case stepInstall:
		body, footer = m.viewInstall()
	case stepDone:
		body, footer = m.viewDone()
	}

	if m.err != nil && m.step != stepDone {
		body += "\n\n" + m.indent(m.theme.Err.Render("! "+m.err.Error()))
	}

	return header + "\n" + body + "\n\n" + footer
}

// indent applies the standard left margin used by every body block.
func (m Model) indent(s string) string {
	pad := "  "
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}

// bullet renders a selection marker.
func (m Model) bullet(active bool) string {
	if active {
		return m.theme.Cursor.Render("> ")
	}
	return "  "
}

// checkbox renders a multi-select marker.
func (m Model) checkbox(on bool) string {
	if on {
		return m.theme.Accent.Render("[x] ")
	}
	return m.theme.Muted.Render("[ ] ")
}

// --- screens ----------------------------------------------------------------

func (m Model) viewWelcome() (string, string) {
	t := m.theme

	lines := []string{
		t.Title.Render("Welcome to the AndersXn OS installer."),
		"",
		t.Body.Render("This will install AndersXn OS on this machine."),
		"",
		t.Muted.Render("firmware  ") + t.Body.Render(m.plan.Firmware.String()),
		t.Muted.Render("arch      ") + t.Body.Render(m.plan.Arch),
	}
	if m.dryRun {
		lines = append(lines, "",
			t.Warn.Render("DRY RUN - commands are logged, nothing is written to disk."))
	}
	if !sys.IsRoot() && !m.dryRun {
		lines = append(lines, "",
			t.Err.Render("Not running as root. The install will fail at the first write."))
	}

	return m.indent(strings.Join(lines, "\n")),
		t.Footer(m.width, "enter", "begin", "q", "quit")
}

func (m Model) viewDisk() (string, string) {
	t := m.theme

	if m.loadingDsk {
		return m.indent(t.Muted.Render("Scanning block devices...")),
			t.Footer(m.width, "ctrl+c", "quit")
	}
	if len(m.disks) == 0 {
		return m.indent(t.Err.Render("No installable disks found.") + "\n\n" +
				t.Muted.Render("AndersXn needs a disk of at least 2 GiB.")),
			t.Footer(m.width, "r", "rescan", "esc", "back", "ctrl+c", "quit")
	}

	var b strings.Builder
	b.WriteString(t.Title.Render("Select the target disk") + "\n")
	b.WriteString(t.Muted.Render("Everything on the chosen disk will be erased.") + "\n\n")

	for i, d := range m.disks {
		style := t.Unselect
		if i == m.diskCursor {
			style = t.Selected
		}
		line := fmt.Sprintf("%-14s %s", d.Name, d.Describe())
		b.WriteString(m.bullet(i == m.diskCursor) + style.Render(line) + "\n")

		if i == m.diskCursor && len(d.Partitions) > 0 {
			for _, p := range d.Partitions {
				detail := fmt.Sprintf("    %-16s %-8s %s",
					p.Path, sys.HumanSize(p.SizeBytes), p.FSType)
				if p.MountPoint != "" {
					detail += t.Warn.Render("  mounted at " + p.MountPoint)
				}
				b.WriteString(t.Muted.Render(detail) + "\n")
			}
		}
	}

	if m.disks[m.diskCursor].Mounted {
		b.WriteString("\n" + t.Warn.Render(
			"This disk has mounted partitions - it may be the medium you booted from."))
	}

	return m.indent(b.String()),
		t.Footer(m.width, "up/down", "select", "enter", "continue", "r", "rescan", "esc", "back")
}

func (m Model) viewLayout() (string, string) {
	t := m.theme
	schemes := layoutSchemes()
	filesystems := layoutFilesystems()

	var b strings.Builder
	b.WriteString(t.Title.Render("Disk layout") + "\n")
	b.WriteString(t.Muted.Render("Target: ") + t.Body.Render(m.plan.Disk.Path) + "\n\n")

	// --- scheme ---
	b.WriteString(m.sectionTitle("Partitioning", m.layoutFocus == 0) + "\n")
	for i, s := range schemes {
		style := t.Unselect
		marker := t.Muted.Render("( ) ")
		if i == m.layoutCursor {
			marker = t.Accent.Render("(*) ")
		}
		if m.layoutFocus == 0 && i == m.layoutCursor {
			style = t.Selected
		}
		b.WriteString("  " + marker + style.Render(s.name) + "\n")
		if i == m.layoutCursor {
			b.WriteString("      " + t.Muted.Render(s.desc) + "\n")
		}
	}

	// --- filesystem ---
	b.WriteString("\n" + m.sectionTitle("Root filesystem", m.layoutFocus == 1) + "\n")
	for i, f := range filesystems {
		style := t.Unselect
		marker := t.Muted.Render("( ) ")
		if i == m.fsCursor {
			marker = t.Accent.Render("(*) ")
		}
		if m.layoutFocus == 1 && i == m.fsCursor {
			style = t.Selected
		}
		b.WriteString("  " + marker + style.Render(f.name) + "\n")
		if i == m.fsCursor {
			b.WriteString("      " + t.Muted.Render(f.desc) + "\n")
		}
	}

	// --- encryption ---
	b.WriteString("\n" + m.sectionTitle("Encryption", m.layoutFocus == 2) + "\n")
	encStyle := t.Unselect
	if m.layoutFocus == 2 {
		encStyle = t.Selected
	}
	b.WriteString("  " + m.checkbox(m.plan.Encrypt) +
		encStyle.Render("Encrypt the root filesystem with LUKS2") + "\n")
	if m.plan.Encrypt {
		b.WriteString("      " + t.Muted.Render(
			"You will be asked for the passphrase on the review screen.") + "\n")
	}

	return m.indent(b.String()),
		t.Footer(m.width, "tab", "section", "up/down", "choose", "space", "toggle",
			"enter", "continue", "esc", "back")
}

// sectionTitle renders a form section heading, highlighted when focused.
func (m Model) sectionTitle(label string, focused bool) string {
	if focused {
		return m.theme.Accent.Render("- " + label)
	}
	return m.theme.Muted.Render("- " + label)
}

// renderForm draws a labelled input list.
func (m Model) renderForm(labels []string, inputs []textinput.Model, focus int) string {
	var b strings.Builder
	width := 0
	for _, l := range labels {
		if len(l) > width {
			width = len(l)
		}
	}
	for i, l := range labels {
		label := m.theme.Muted.Render(fmt.Sprintf("%-*s  ", width, l))
		if i == focus {
			label = m.theme.Accent.Render(fmt.Sprintf("%-*s  ", width, l))
		}
		b.WriteString(m.bullet(i == focus) + label + inputs[i].View() + "\n")
	}
	return b.String()
}

func (m Model) viewSystem() (string, string) {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.Title.Render("System settings") + "\n")
	b.WriteString(t.Muted.Render("Identity and localisation for the installed machine.") + "\n\n")
	b.WriteString(m.renderForm(
		[]string{"Hostname", "Timezone", "Locale", "Keymap"},
		m.sysInputs, m.sysFocus))

	return m.indent(b.String()),
		t.Footer(m.width, "tab", "next field", "enter", "continue", "esc", "back")
}

func (m Model) viewAccount() (string, string) {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.Title.Render("Operator account") + "\n")
	b.WriteString(t.Muted.Render(
		"Root login is disabled. This account gets sudo.") + "\n\n")
	b.WriteString(m.renderForm(
		[]string{"Username", "Full name", "Password", "Confirm", "SSH key"},
		m.accInputs, m.accFocus))

	if strings.TrimSpace(m.accInputs[accSSHKey].Value()) != "" {
		b.WriteString("\n" + t.Muted.Render(
			"An SSH key was given, so SSH password authentication will be disabled."))
	}

	return m.indent(b.String()),
		t.Footer(m.width, "tab", "next field", "enter", "continue", "esc", "back")
}

func (m Model) viewModules() (string, string) {
	t := m.theme
	var b strings.Builder

	b.WriteString(t.Title.Render("Software provisioning") + "\n")
	b.WriteString(t.Muted.Render(
		"Selected stacks are deployed automatically after the base install.") + "\n\n")

	var lastCat string
	for i, mod := range m.modules {
		if string(mod.Category) != lastCat {
			if lastCat != "" {
				b.WriteString("\n")
			}
			b.WriteString(t.Accent.Render("- "+string(mod.Category)) + "\n")
			lastCat = string(mod.Category)
		}

		style := t.Unselect
		if i == m.moduleCursor {
			style = t.Selected
		}
		line := fmt.Sprintf("%-22s %s", mod.Name, t.Muted.Render(mod.Summary))
		b.WriteString(m.bullet(i == m.moduleCursor) + m.checkbox(m.selected[mod.ID]) +
			style.Render(line) + "\n")
	}

	// Detail pane for the highlighted module.
	if len(m.modules) > 0 {
		mod := m.modules[m.moduleCursor]
		wrapWidth := m.width - 8
		if wrapWidth < 30 {
			wrapWidth = 30
		}
		b.WriteString("\n" + t.Muted.Render(wrap(mod.Detail, wrapWidth)) + "\n")
	}

	ids := m.selectedIDs()
	if len(ids) > 0 {
		b.WriteString("\n" + t.Body.Render(fmt.Sprintf(
			"%d selected, roughly %d GiB", len(ids), provision.DiskEstimateGiB(ids))))
	}
	for _, w := range provision.Conflicts(ids) {
		b.WriteString("\n" + t.Warn.Render("! "+wrap(w, m.width-8)))
	}

	return m.indent(b.String()),
		t.Footer(m.width, "up/down", "move", "space", "toggle", "a", "all", "n", "none",
			"enter", "continue", "esc", "back")
}

func (m Model) viewReview() (string, string) {
	t := m.theme
	p := m.plan
	var b strings.Builder

	b.WriteString(t.Title.Render("Review") + "\n\n")

	row := func(k, v string) {
		b.WriteString(t.Muted.Render(fmt.Sprintf("%-12s", k)) + t.Body.Render(v) + "\n")
	}
	row("Disk", p.Disk.Path+"  "+p.Disk.Describe())
	row("Scheme", string(p.Scheme))
	row("Filesystem", string(p.FS))
	row("Encryption", map[bool]string{true: "LUKS2", false: "none"}[p.Encrypt])
	row("Firmware", p.Firmware.String())
	row("Bootloader", "Limine")
	row("Hostname", p.Hostname)
	row("Timezone", p.Timezone+"   locale "+p.Locale+"   keymap "+p.Keymap)
	row("Account", p.Username)
	if len(p.Modules) > 0 {
		row("Software", strings.Join(p.Modules, ", "))
	} else {
		row("Software", t.Muted.Render("none"))
	}

	b.WriteString("\n")
	if p.Scheme == install.SchemeWipe {
		b.WriteString(t.Err.Render(fmt.Sprintf(
			"Everything on %s (%s) will be destroyed.",
			p.Disk.Path, sys.HumanSize(p.Disk.SizeBytes))) + "\n")
	}
	if m.dryRun {
		b.WriteString(t.Warn.Render("DRY RUN - nothing will actually be written.") + "\n")
	}

	footer := t.Footer(m.width, "enter", "confirm", "esc", "back")
	if m.reviewConfirmed {
		b.WriteString("\n" + t.Warn.Render("Press enter once more to begin. This cannot be undone."))
		footer = t.Footer(m.width, "enter", "START INSTALL", "esc", "back")
	}

	return m.indent(b.String()), footer
}

func (m Model) viewInstall() (string, string) {
	t := m.theme
	var b strings.Builder

	b.WriteString(t.Title.Render("Installing") + "\n\n")

	name := m.progress.Name
	if name == "" {
		name = "starting"
	}
	b.WriteString(t.Body.Render(fmt.Sprintf("%d/%d  %s",
		m.progress.Step, m.progress.Total, name)) + "\n")
	b.WriteString(m.progressBar(m.width-6) + "\n\n")
	b.WriteString(m.logPane())

	return m.indent(b.String()),
		t.Footer(m.width, "up/down", "scroll log", "ctrl+c", "cancel")
}

func (m Model) viewDone() (string, string) {
	t := m.theme
	var b strings.Builder

	if m.err != nil {
		b.WriteString(t.Err.Render("Installation failed") + "\n\n")
		b.WriteString(t.Body.Render(wrap(m.err.Error(), m.width-6)) + "\n\n")
		b.WriteString(t.Muted.Render(
			"The target has been unmounted. Fix the problem and run ax-installer again.") + "\n\n")
		b.WriteString(m.logPane())
		return m.indent(b.String()), t.Footer(m.width, "up/down", "scroll log", "q", "quit")
	}

	b.WriteString(t.OK.Render("AndersXn OS is installed.") + "\n\n")
	b.WriteString(t.Body.Render("Remove the installation medium and reboot.") + "\n\n")
	b.WriteString(t.Muted.Render("log in as ") + t.Body.Render(m.plan.Username) + "\n")
	if len(m.plan.Modules) > 0 {
		b.WriteString(t.Muted.Render("provisioned ") +
			t.Body.Render(strings.Join(m.plan.Modules, ", ")) + "\n")
	}
	if contains(m.plan.Modules, "tailscale") {
		b.WriteString("\n" + t.Muted.Render(
			"Tailscale is installed but not connected - run 'sudo tailscale up' after boot."))
	}
	if contains(m.plan.Modules, "portainer") {
		b.WriteString("\n" + t.Muted.Render(
			"Portainer is on https://"+m.plan.Hostname+":9443 - set the admin password "+
				"within 5 minutes of first boot."))
	}

	return m.indent(b.String()), t.Footer(m.width, "r", "reboot now", "q", "quit to shell")
}

// progressBar renders the install progress as a solid accent bar.
func (m Model) progressBar(width int) string {
	if width < 10 {
		width = 10
	}
	frac := m.progress.Fraction()
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	filled := int(float64(width) * frac)
	return m.theme.BarFill.Render(strings.Repeat("=", filled)) +
		m.theme.BarTrack.Render(strings.Repeat("-", width-filled)) +
		m.theme.Muted.Render(fmt.Sprintf("  %3.0f%%", frac*100))
}

// logPane renders the tail of the install log, honouring the scroll offset.
func (m Model) logPane() string {
	// Rows left after the header, progress block and footer.
	rows := m.height - 20
	if rows < 5 {
		rows = 5
	}

	end := len(m.logLines) - m.logOffset
	if end < 0 {
		end = 0
	}
	start := end - rows
	if start < 0 {
		start = 0
	}

	var b strings.Builder
	for _, line := range m.logLines[start:end] {
		// Display width, not byte length: log lines carry UTF-8 from package
		// managers and would otherwise be cut mid-rune.
		if m.width > 10 && lipgloss.Width(line) > m.width-6 {
			line = truncateRunes(line, m.width-9) + "..."
		}
		style := m.theme.Muted
		switch {
		case strings.HasPrefix(line, "== "):
			style = m.theme.Accent
		case strings.HasPrefix(line, "$ "):
			style = m.theme.Body
		}
		b.WriteString(style.Render(line) + "\n")
	}
	if m.logOffset > 0 {
		b.WriteString(m.theme.Warn.Render(
			fmt.Sprintf("-- scrolled back %d lines, press G for the tail --", m.logOffset)))
	}
	return b.String()
}

// --- small helpers ----------------------------------------------------------

// wrap is a minimal greedy word wrapper. lipgloss can do this, but pulling its
// reflow dependency in for two call sites is not worth it.
func wrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			switch {
			case line == "":
				line = word
			case len(line)+1+len(word) <= width:
				line += " " + word
			default:
				out = append(out, line)
				line = word
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// truncateRunes cuts a string to at most n runes, never splitting one.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
