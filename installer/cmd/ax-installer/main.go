// Command ax-installer is the AndersXn OS system installer.
//
//	ax-installer              # interactive TUI
//	ax-installer --dry-run    # walk the wizard, log commands, write nothing
//	ax-installer --version
//
// It must run as root to do anything destructive; --dry-run works unprivileged
// and is the right way to demo or test the flow.
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/ggstudios/andersxn-os/installer/internal/branding"
	"github.com/ggstudios/andersxn-os/installer/internal/tui"
)

func main() {
	var (
		dryRun      = flag.Bool("dry-run", false, "log every command without executing it")
		showVersion = flag.Bool("version", false, "print the release and exit")
		showLogo    = flag.Bool("logo", false, "print the AndersXn mark and exit")
		probe       = flag.Bool("probe", false, "print machine and catalog JSON, then exit (for the GTK front end)")
		runPlanFile = flag.String("run-plan", "", "execute a plan JSON file, streaming progress as JSON")
	)
	flag.Usage = usage
	flag.Parse()

	switch {
	case *showVersion:
		fmt.Printf("%s %s (%s)\n", branding.Name, branding.Version, branding.Codename)
		return
	case *showLogo:
		fmt.Println(branding.Full())
		return
	case *probe:
		// Headless: needs neither a terminal nor root, so the GTK front end
		// can draw a populated window before it escalates privileges.
		os.Exit(runProbe())
	case *runPlanFile != "":
		os.Exit(runPlan(*runPlanFile))
	}

	// Bubble Tea needs a real terminal. Without this check the failure mode is
	// an unreadable escape-sequence dump, which is a poor thing to hand someone
	// who piped the installer somewhere by mistake.
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintln(os.Stderr,
			"ax-installer needs an interactive terminal (stdout is not a tty)")
		os.Exit(1)
	}

	if os.Geteuid() != 0 && !*dryRun {
		fmt.Fprintln(os.Stderr,
			"ax-installer must run as root. Re-run with sudo, or use --dry-run to explore.")
		os.Exit(1)
	}

	p := tea.NewProgram(
		tui.New(*dryRun),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "ax-installer: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "%s\n\n", branding.Logo(80))
	fmt.Fprintf(os.Stderr, "%s installer %s (%s)\n\n",
		branding.Name, branding.Version, branding.Codename)
	fmt.Fprintln(os.Stderr, "Usage: ax-installer [flags]")
	flag.PrintDefaults()
}
