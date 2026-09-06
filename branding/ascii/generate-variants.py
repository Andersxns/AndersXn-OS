#!/usr/bin/env python3
"""
AndersXn OS - ASCII logo variant generator.

The master mark (axos-logo-master.txt) is a 100-column render exported from
AXnobg.png. It is far too wide for an 80x24 TTY, a getty /etc/issue banner or
the AX-Installer header, so this script derives smaller variants from it.

The master is treated as a 1-bit raster (non-space == ink). Each NxM block of
source cells is collapsed to a single output cell, which is inked when the
block contains any ink at all. A density ramp was tried and rejected: the mark
is sparse outline-and-fill line art, so proportional shading breaks thin
strokes into dotted noise. A solid two-tone fill in '$' -- the glyph the master
itself is drawn in -- keeps the silhouette readable down to 25 columns.

Run this whenever the master art is re-exported from the PNG:

    python3 branding/ascii/generate-variants.py

Outputs (all written next to the master, LF line endings, no trailing blanks):
    axos-logo.txt          74 cols - full mark: fastfetch on wide terminals
    axos-logo-compact.txt  37 cols - AX-Installer header, motd
    axos-logo-mini.txt     25 cols - /etc/issue, narrow and serial consoles
"""

import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
MASTER = os.path.join(HERE, "axos-logo-master.txt")

# The master art is drawn predominantly in '$'; reusing it keeps every derived
# size visually part of the same family.
INK = "$"


def load_master(path):
    """Read the master art and crop it to its ink bounding box."""
    with open(path, "r", encoding="utf-8", newline="") as fh:
        lines = [ln.rstrip("\r\n") for ln in fh]

    ink_rows = [i for i, ln in enumerate(lines) if ln.strip()]
    if not ink_rows:
        sys.exit("error: master art contains no ink")

    rows = lines[ink_rows[0]:ink_rows[-1] + 1]
    left = min(len(ln) - len(ln.lstrip()) for ln in rows if ln.strip())
    right = max(len(ln.rstrip()) for ln in rows)
    return [ln[left:right].ljust(right - left) for ln in rows]


def downsample(rows, fx, fy):
    """Collapse each fx-by-fy block of source cells into one output cell."""
    height = len(rows)
    width = max(len(r) for r in rows)
    grid = [r.ljust(width) for r in rows]

    out = []
    for oy in range(0, height, fy):
        line = []
        for ox in range(0, width, fx):
            inked = any(
                grid[y][x] != " "
                for y in range(oy, min(oy + fy, height))
                for x in range(ox, min(ox + fx, width))
            )
            line.append(INK if inked else " ")
        out.append("".join(line).rstrip())

    while out and not out[0].strip():
        out.pop(0)
    while out and not out[-1].strip():
        out.pop()
    return out


def write(name, rows):
    path = os.path.join(HERE, name)
    with open(path, "w", encoding="utf-8", newline="\n") as fh:
        fh.write("\n".join(rows) + "\n")
    width = max((len(r) for r in rows), default=0)
    print("wrote %-24s %3d cols x %2d rows" % (name, width, len(rows)))


def main():
    master = load_master(MASTER)
    write("axos-logo.txt", master)
    write("axos-logo-compact.txt", downsample(master, 2, 2))
    write("axos-logo-mini.txt", downsample(master, 3, 3))


if __name__ == "__main__":
    main()
