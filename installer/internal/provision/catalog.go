// Package provision describes the optional software stacks AX-Installer can
// deploy into a freshly installed system.
//
// The catalog here is the user-facing half: what the selector shows and what
// the summary screen promises. The other half lives in provisioning/modules/,
// one shell script per module ID, executed inside the target chroot by
// provisioning/postinstall.sh. Keeping the two in step is enforced by
// "make check-modules", which fails when an ID here has no matching script.
package provision

import "sort"

// Category groups modules in the selector.
type Category string

const (
	CatContainers Category = "Containers & Orchestration"
	CatHomelab    Category = "Homelab Services"
	CatDeveloper  Category = "Developer Tools"
)

// categoryOrder fixes the display order; map iteration would shuffle it.
var categoryOrder = []Category{CatContainers, CatHomelab, CatDeveloper}

// Module is one selectable stack.
type Module struct {
	ID        string
	Name      string
	Summary   string // one line, shown next to the checkbox
	Detail    string // shown in the side pane when highlighted
	Category  Category
	Requires  []string // module IDs that must also be selected
	Ports     []int    // TCP ports opened in the nftables services chain
	DefaultOn bool
	Arches    []string // empty means every architecture
	DiskGiB   int      // rough installed footprint, for the summary
}

// Catalog is every module the installer offers.
var Catalog = []Module{
	{
		ID:      "docker",
		Name:    "Docker Engine",
		Summary: "Container runtime + Compose v2",
		Detail: "Installs Docker CE, containerd and the Compose v2 plugin from " +
			"Docker's official repository, with the data root on a dedicated " +
			"btrfs subvolume (@docker, nodatacow) so container layers never " +
			"fragment the root filesystem or get caught in a root snapshot.",
		Category:  CatContainers,
		DefaultOn: true,
		DiskGiB:   2,
	},
	{
		ID:      "portainer",
		Name:    "Portainer CE",
		Summary: "Web UI for Docker, on :9443",
		Detail: "Runs Portainer CE as a container with a persistent volume, " +
			"reachable on https://<host>:9443. Requires Docker. The initial " +
			"admin password must be set within 5 minutes of first boot or " +
			"Portainer locks itself and the container needs a restart.",
		Category:  CatContainers,
		Requires:  []string{"docker"},
		Ports:     []int{9443},
		DefaultOn: true,
		DiskGiB:   1,
	},
	{
		ID:      "k3s",
		Name:    "K3s",
		Summary: "Lightweight Kubernetes, single-node server",
		Detail: "Installs K3s as a single-node server with Traefik and the " +
			"local-path provisioner. Flannel uses the interface holding the " +
			"default route. Not selected alongside Docker by default: both " +
			"want to manage packet forwarding rules, and on a small node " +
			"running one of the two is almost always the better call.",
		Category: CatContainers,
		Ports:    []int{6443, 10250},
		DiskGiB:  3,
	},
	{
		ID:      "jellyfin",
		Name:    "Jellyfin",
		Summary: "Media server with hardware transcoding",
		Detail: "Installs Jellyfin from the official repository and configures " +
			"hardware transcoding: VA-API on Intel/AMD, NVENC when the NVIDIA " +
			"driver is present, V4L2 on supported arm64 boards. The service " +
			"account joins the render and video groups and the /dev/dri nodes " +
			"are exposed to it. Media lives under /srv/andersxn/data/media.",
		Category: CatHomelab,
		Ports:    []int{8096, 8920},
		DiskGiB:  1,
	},
	{
		ID:      "casaos",
		Name:    "CasaOS",
		Summary: "Home cloud dashboard and app store, on :80",
		Detail: "A personal-cloud dashboard over Docker: a web UI for browsing " +
			"and one-click installing self-hosted apps, plus file management " +
			"and storage pooling. Requires Docker.\n\n" +
			"Installed on FIRST BOOT, not during the install: the CasaOS " +
			"installer inspects a running systemd and a live Docker daemon, " +
			"neither of which exists inside the installer chroot. It needs " +
			"working networking the first time the machine boots.\n\n" +
			"Serves on port 80, so it collides with anything else serving " +
			"port 80 on this node.",
		Category: CatHomelab,
		Requires: []string{"docker"},
		Ports:    []int{80},
		DiskGiB:  2,
	},
	{
		ID:      "tailscale",
		Name:    "Tailscale",
		Summary: "Mesh VPN; node stays offline until you authenticate",
		Detail: "Installs tailscaled and enables it, but does NOT bring the " +
			"node up - joining a tailnet needs an interactive login or an auth " +
			"key, and baking either into an install image is a bad idea. Run " +
			"'tailscale up' after first boot. IP forwarding is pre-enabled so " +
			"the node can serve as a subnet router or exit node.",
		Category:  CatHomelab,
		DefaultOn: true,
		DiskGiB:   1,
	},
	{
		ID:      "devtools",
		Name:    "Developer toolchain",
		Summary: "Zsh theme, Neovim config, git, btop, fastfetch",
		Detail: "The AndersXn shell environment: the framework-free zsh theme, " +
			"a Neovim configuration with LSP scaffolding, git defaults, btop " +
			"with the AndersXn palette, and fastfetch. The base packages are " +
			"already in the image; this module adds the configuration and the " +
			"zsh completion plugins.",
		Category:  CatDeveloper,
		DefaultOn: true,
		DiskGiB:   1,
	},
	{
		ID:      "buildtools",
		Name:    "Build toolchain",
		Summary: "gcc, make, Go, Python headers",
		Detail: "build-essential, pkg-config, the Go toolchain and Python " +
			"development headers - enough to build most things that turn up in " +
			"a homelab without pulling in a full desktop SDK.",
		Category: CatDeveloper,
		DiskGiB:  2,
	},
}

// byID indexes the catalog for constant-time lookup.
var byID = func() map[string]Module {
	m := make(map[string]Module, len(Catalog))
	for _, mod := range Catalog {
		m[mod.ID] = mod
	}
	return m
}()

// Lookup returns a module by ID.
func Lookup(id string) (Module, bool) {
	m, ok := byID[id]
	return m, ok
}

// SupportsArch reports whether the module runs on the given Debian arch.
func (m Module) SupportsArch(arch string) bool {
	if len(m.Arches) == 0 {
		return true
	}
	for _, a := range m.Arches {
		if a == arch {
			return true
		}
	}
	return false
}

// ForArch returns the catalog filtered to a target architecture, grouped and
// ordered by category.
func ForArch(arch string) []Module {
	var out []Module
	for _, m := range Catalog {
		if m.SupportsArch(arch) {
			out = append(out, m)
		}
	}
	rank := make(map[Category]int, len(categoryOrder))
	for i, c := range categoryOrder {
		rank[c] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		return rank[out[i].Category] < rank[out[j].Category]
	})
	return out
}

// Defaults returns the IDs ticked when the operator first reaches the
// provisioning step.
func Defaults(arch string) []string {
	var ids []string
	for _, m := range ForArch(arch) {
		if m.DefaultOn {
			ids = append(ids, m.ID)
		}
	}
	return ids
}

// ResolveDependencies expands a selection to include everything the chosen
// modules require, and reports which IDs were pulled in, so the UI can say so
// rather than silently changing what the operator ticked.
func ResolveDependencies(selected []string) (full []string, added []string) {
	inSet := make(map[string]bool, len(selected))
	for _, id := range selected {
		inSet[id] = true
	}
	// Iterate to a fixed point: a requirement may have requirements of its own.
	for changed := true; changed; {
		changed = false
		for id := range inSet {
			mod, ok := byID[id]
			if !ok {
				continue
			}
			for _, req := range mod.Requires {
				if !inSet[req] {
					inSet[req] = true
					added = append(added, req)
					changed = true
				}
			}
		}
	}
	// Return in catalog order, for a stable and readable summary.
	for _, m := range Catalog {
		if inSet[m.ID] {
			full = append(full, m.ID)
		}
	}
	sort.Strings(added)
	return full, added
}

// Conflicts reports selections that should not usually be deployed together.
// This is advisory: the installer warns, it does not refuse.
func Conflicts(selected []string) []string {
	set := make(map[string]bool, len(selected))
	for _, id := range selected {
		set[id] = true
	}
	var warnings []string
	if set["docker"] && set["k3s"] {
		warnings = append(warnings,
			"Docker and K3s both manage packet forwarding rules; expect to "+
				"hand-tune nftables if you run both on one node")
	}
	return warnings
}

// Ports returns every TCP port the selection needs opened, deduplicated.
func Ports(selected []string) []int {
	seen := map[int]bool{}
	var ports []int
	for _, id := range selected {
		mod, ok := byID[id]
		if !ok {
			continue
		}
		for _, p := range mod.Ports {
			if !seen[p] {
				seen[p] = true
				ports = append(ports, p)
			}
		}
	}
	sort.Ints(ports)
	return ports
}

// DiskEstimateGiB sums the rough installed footprint of a selection.
func DiskEstimateGiB(selected []string) int {
	total := 0
	for _, id := range selected {
		if m, ok := byID[id]; ok {
			total += m.DiskGiB
		}
	}
	return total
}
