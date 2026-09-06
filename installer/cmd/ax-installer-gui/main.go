// Command ax-installer-gui is the graphical entry point for AX-Installer.
//
// It is a launcher, not a second installer. When a Wayland (or X11) session is
// running it opens ax-installer full-screen in a terminal themed with the
// AndersXn palette; otherwise it hands straight over to the TUI.
//
// Why a wrapper rather than a native Fyne/GTK front end:
//
// The wizard is nine screens of branching state. Implementing it twice would
// double the surface area and guarantee the two copies drift - the graphical
// one would quietly fall behind on exactly the screens that matter (disk
// selection, encryption, the destructive confirmation). The install engine in
// internal/install is deliberately UI-agnostic and has no terminal code in it,
// so a native front end can be built against the same Engine later without
// touching any install logic. Until there is a reason to pay for that second
// front end, wrapping the TUI is the honest option: one implementation, one
// place for bugs, identical behaviour in both modes.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/ggstudios/andersxn-os/installer/internal/branding"
)

// terminalCandidates are tried in order. foot is the AndersXn default and the
// only one guaranteed present on a desktop-provisioned image; the rest cover
// operators running the installer from their own session.
var terminalCandidates = []struct {
	bin  string
	args func(inner []string) []string
}{
	{"foot", func(inner []string) []string {
		return append([]string{
			"--fullscreen",
			"--title=" + branding.Name + " installer",
			"--font=monospace:size=12",
			"--override=colors.background=" + hex(branding.Ink),
			"--override=colors.foreground=" + hex(branding.Snow),
			"--override=colors.regular6=" + hex(branding.Accent),
			"-e",
		}, inner...)
	}},
	{"alacritty", func(inner []string) []string {
		return append([]string{"--title", branding.Name + " installer", "-e"}, inner...)
	}},
	{"kitty", func(inner []string) []string {
		return append([]string{"--title", branding.Name + " installer"}, inner...)
	}},
	{"xterm", func(inner []string) []string {
		return append([]string{"-fa", "Monospace", "-fs", "12",
			"-bg", "#" + hex(branding.Ink), "-fg", "#" + hex(branding.Snow), "-e"}, inner...)
	}},
}

func hex(c string) string {
	if len(c) > 0 && c[0] == '#' {
		return c[1:]
	}
	return c
}

func main() {
	tuiPath, err := exec.LookPath("ax-installer")
	if err != nil {
		// Same directory as this binary is the layout on the live ISO.
		tuiPath = "/usr/bin/ax-installer"
		if _, statErr := os.Stat(tuiPath); statErr != nil {
			fatal(errors.New("ax-installer not found in PATH or /usr/bin"))
		}
	}

	inner := append([]string{tuiPath}, os.Args[1:]...)

	// Prefer the real GTK installer when a display is available. It is the
	// primary graphical front end; this launcher exists only to pick between
	// it and the text installer, and to survive its absence.
	if graphicalSessionRunning() {
		if gtkPath, gtkErr := exec.LookPath("ax-installer-gtk"); gtkErr == nil {
			argv := append([]string{gtkPath}, os.Args[1:]...)
			if execErr := syscall.Exec(gtkPath, argv, os.Environ()); execErr != nil {
				fmt.Fprintln(os.Stderr,
					"ax-installer-gtk failed to start:", execErr,
					"- falling back to the text installer")
			}
		}
	}

	if !graphicalSessionRunning() {
		// No display server: become the TUI rather than spawning a child, so
		// signals and the exit status pass through unchanged.
		if err := syscall.Exec(tuiPath, inner, os.Environ()); err != nil {
			fatal(fmt.Errorf("starting %s: %w", tuiPath, err))
		}
		return
	}

	for _, term := range terminalCandidates {
		bin, err := exec.LookPath(term.bin)
		if err != nil {
			continue
		}
		cmd := exec.Command(bin, term.args(inner)...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				os.Exit(exitErr.ExitCode())
			}
			fatal(fmt.Errorf("running %s: %w", term.bin, err))
		}
		return
	}

	// A graphical session with no terminal emulator we recognise. Falling back
	// to the TUI on the current tty is better than refusing to install.
	fmt.Fprintln(os.Stderr,
		"no supported terminal emulator found; falling back to the text installer")
	if err := syscall.Exec(tuiPath, inner, os.Environ()); err != nil {
		fatal(err)
	}
}

// graphicalSessionRunning reports whether a Wayland or X11 session is available
// to open a window on.
func graphicalSessionRunning() bool {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return true
	}
	if os.Getenv("DISPLAY") != "" {
		return true
	}
	return false
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "ax-installer-gui: %v\n", err)
	os.Exit(1)
}
