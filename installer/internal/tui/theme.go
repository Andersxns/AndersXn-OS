package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ggstudios/andersxn-os/installer/internal/branding"
)

// Theme holds every style the installer draws with. It is a struct rather than
// a set of package-level vars so the GUI wrapper and the tests can instantiate
// a variant (e.g. a no-colour theme for a serial console) without mutating
// global state.
type Theme struct {
	Logo      lipgloss.Style
	Title     lipgloss.Style
	Subtitle  lipgloss.Style
	Body      lipgloss.Style
	Muted     lipgloss.Style
	Accent    lipgloss.Style
	Selected  lipgloss.Style
	Unselect  lipgloss.Style
	Cursor    lipgloss.Style
	Panel     lipgloss.Style
	Hint      lipgloss.Style
	Key       lipgloss.Style
	OK        lipgloss.Style
	Warn      lipgloss.Style
	Err       lipgloss.Style
	Crumb     lipgloss.Style
	CrumbNow  lipgloss.Style
	CrumbDone lipgloss.Style
	Field     lipgloss.Style
	FieldOn   lipgloss.Style
	BarFill   lipgloss.Style
	BarTrack  lipgloss.Style
}

// NewTheme builds the standard AndersXn theme.
func NewTheme() Theme {
	accent := lipgloss.Color(branding.Accent)
	snow := lipgloss.Color(branding.Snow)
	muted := lipgloss.Color(branding.Muted)
	surface := lipgloss.Color(branding.Surface)

	return Theme{
		Logo:     lipgloss.NewStyle().Foreground(accent),
		Title:    lipgloss.NewStyle().Foreground(snow).Bold(true),
		Subtitle: lipgloss.NewStyle().Foreground(muted),
		Body:     lipgloss.NewStyle().Foreground(snow),
		Muted:    lipgloss.NewStyle().Foreground(muted),
		Accent:   lipgloss.NewStyle().Foreground(accent),

		Selected: lipgloss.NewStyle().Foreground(accent).Bold(true),
		Unselect: lipgloss.NewStyle().Foreground(snow),
		Cursor:   lipgloss.NewStyle().Foreground(accent).Bold(true),

		Panel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(branding.AccentDeep)).
			Padding(1, 2),

		Hint: lipgloss.NewStyle().Foreground(muted).Padding(0, 1),
		Key: lipgloss.NewStyle().
			Foreground(lipgloss.Color(branding.Ink)).
			Background(accent).
			Padding(0, 1),

		OK:   lipgloss.NewStyle().Foreground(lipgloss.Color(branding.OK)),
		Warn: lipgloss.NewStyle().Foreground(lipgloss.Color(branding.Warn)),
		Err:  lipgloss.NewStyle().Foreground(lipgloss.Color(branding.Err)),

		Crumb:     lipgloss.NewStyle().Foreground(muted),
		CrumbNow:  lipgloss.NewStyle().Foreground(accent).Bold(true),
		CrumbDone: lipgloss.NewStyle().Foreground(lipgloss.Color(branding.OK)),

		Field: lipgloss.NewStyle().
			Foreground(snow).Background(surface).Padding(0, 1),
		FieldOn: lipgloss.NewStyle().
			Foreground(snow).Background(surface).Padding(0, 1).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(accent),

		BarFill:  lipgloss.NewStyle().Foreground(accent),
		BarTrack: lipgloss.NewStyle().Foreground(lipgloss.Color(branding.AccentDeep)),
	}
}

// Header renders the branded installer header: the largest ASCII mark that fits
// the terminal, the release line, and the step breadcrumb.
//
// The requirement is that the mark appears above EVERY installation step, so
// this is called unconditionally from Model.View. On a terminal too short to
// spare the rows (below ~28 lines, common on a 80x25 server KVM) the mark is
// dropped in favour of a single-line wordmark - a header that eats the whole
// screen is worse branding than no header at all.
func (t Theme) Header(width, height int, steps []string, current int) string {
	var b strings.Builder

	if height >= 28 {
		if art := branding.Logo(width); art != "" {
			b.WriteString(t.Logo.Render(centerBlock(art, width)))
			b.WriteString("\n")
		}
	}

	b.WriteString(lipgloss.PlaceHorizontal(width, lipgloss.Center,
		t.Title.Render(branding.Name)+" "+t.Subtitle.Render(branding.Version+" ("+branding.Codename+")")))
	b.WriteString("\n")
	b.WriteString(lipgloss.PlaceHorizontal(width, lipgloss.Center, t.crumbs(steps, current, width)))
	b.WriteString("\n")

	return b.String()
}

// crumbs renders the step breadcrumb, collapsing to "step N/M - Name" when the
// full trail will not fit.
func (t Theme) crumbs(steps []string, current, width int) string {
	if len(steps) == 0 {
		return ""
	}

	parts := make([]string, 0, len(steps))
	for i, s := range steps {
		switch {
		case i < current:
			parts = append(parts, t.CrumbDone.Render(s))
		case i == current:
			parts = append(parts, t.CrumbNow.Render(s))
		default:
			parts = append(parts, t.Crumb.Render(s))
		}
	}
	full := strings.Join(parts, t.Crumb.Render(" > "))

	if lipgloss.Width(full) <= width {
		return full
	}
	return t.Crumb.Render("step ") +
		t.CrumbNow.Render(itoa(current+1)+"/"+itoa(len(steps))) +
		t.Crumb.Render(" - ") + t.CrumbNow.Render(steps[current])
}

// Footer renders the key hint bar from alternating key/description pairs.
func (t Theme) Footer(width int, pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, t.Key.Render(pairs[i])+t.Hint.Render(" "+pairs[i+1]))
	}
	return lipgloss.PlaceHorizontal(width, lipgloss.Center,
		strings.Join(parts, t.Muted.Render("  ")))
}

// centerBlock centres a multi-line block horizontally within width, preserving
// the block's internal alignment (each line is padded by the same amount).
func centerBlock(block string, width int) string {
	lines := strings.Split(block, "\n")
	widest := 0
	for _, l := range lines {
		if n := lipgloss.Width(l); n > widest {
			widest = n
		}
	}
	pad := (width - widest) / 2
	if pad <= 0 {
		return block
	}
	prefix := strings.Repeat(" ", pad)
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// itoa avoids pulling strconv into every call site in the view layer.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}
