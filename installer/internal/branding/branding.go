// Package branding is the single source of the AndersXn OS visual identity as
// the installer sees it: the ASCII mark in three sizes, and the colour tokens
// every other package styles against.
//
// The ASCII files under assets/ are copies of branding/ascii/ in the repository
// root, synchronised by "make sync-assets" (and by build stage 50). They are
// embedded rather than read from disk so ax-installer runs identically from the
// live ISO, from a rescue shell, and from a developer's laptop.
package branding

import (
	_ "embed"
	"strings"
)

//go:embed assets/axos-logo.txt
var logoFull string

//go:embed assets/axos-logo-compact.txt
var logoCompact string

//go:embed assets/axos-logo-mini.txt
var logoMini string

// Release identity. Overridden at link time by build stage 50:
//
//	go build -ldflags "-X ...branding.Version=1.0 -X ...branding.Codename=Summit"
var (
	Name     = "AndersXn OS"
	Version  = "1.0"
	Codename = "Summit"
	Vendor   = "GG Studios"
)

// Colour tokens. These mirror build/lib/common.sh; changing the palette there
// and here keeps the bootloader, splash, shell and installer in agreement.
const (
	Ink        = "#0B0E14" // base background
	Surface    = "#131722" // panels, raised surfaces
	Snow       = "#F2F5FA" // primary foreground, the logo white
	Accent     = "#4FC3F7" // glacier cyan
	AccentDeep = "#1B6CA8" // unfocused accent
	Muted      = "#6B7A90" // secondary text
	OK         = "#63D68A"
	Warn       = "#E8B84B"
	Err        = "#E5484D"
)

// Logo returns the largest mark that fits in the given terminal width, falling
// back through compact to mini and finally to an empty string on a terminal too
// narrow for any of them (a serial console at 40 columns, say).
//
// Width is compared against the art plus a two-column breathing margin on each
// side, because every caller centres it.
func Logo(width int) string {
	for _, art := range []string{logoFull, logoCompact, logoMini} {
		if artWidth(art)+4 <= width {
			return strings.TrimRight(art, "\n")
		}
	}
	return ""
}

// Full, Compact and Mini return a specific size regardless of terminal width.
func Full() string    { return strings.TrimRight(logoFull, "\n") }
func Compact() string { return strings.TrimRight(logoCompact, "\n") }
func Mini() string    { return strings.TrimRight(logoMini, "\n") }

// artWidth reports the width of the widest line in the art.
func artWidth(art string) int {
	widest := 0
	for _, line := range strings.Split(art, "\n") {
		if n := len([]rune(line)); n > widest {
			widest = n
		}
	}
	return widest
}

// Tagline is shown under the mark on the welcome screen.
func Tagline() string {
	return Name + " " + Version + " (" + Codename + ")"
}
