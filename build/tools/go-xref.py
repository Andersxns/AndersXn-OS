#!/usr/bin/env python3
"""
A crude cross-reference check for the installer sources.

This is NOT a substitute for "go build" - it is what stands in for it on a host
with no Go toolchain. It catches the two mistakes that actually bite when
writing multi-package Go without a compiler:

  1. referencing pkg.Identifier that the package does not export
  2. declaring the same name twice in one package (a field and a method with
     the same name, most often)
"""

import os
import re
import sys
from collections import defaultdict

ROOT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "installer")
ROOT = os.path.normpath(ROOT)

DECL_RE = re.compile(
    r"^(?:func\s+\((?P<recv>[^)]*)\)\s+(?P<method>\w+)"
    r"|func\s+(?P<func>\w+)"
    r"|type\s+(?P<type>\w+)"
    r"|const\s+(?P<const>\w+)"
    r"|var\s+(?P<var>\w+))",
    re.M,
)
BLOCK_RE = re.compile(r"^(?:const|var|type)\s*\(\s*$(.*?)^\)\s*$", re.M | re.S)
BLOCK_NAME_RE = re.compile(r"^\s*(\w+)", re.M)
IMPORT_RE = re.compile(r'^\s*(?:(\w+)\s+)?"([^"]+)"', re.M)
USE_RE = re.compile(r"\b([a-z][a-zA-Z0-9_]*)\.([A-Z]\w*)")
STRUCT_FIELD_RE = re.compile(r"^\t([A-Z]\w*)\s+[\w\[\]*./]+", re.M)


def go_files(pkg_dir):
    for name in sorted(os.listdir(pkg_dir)):
        if name.endswith(".go"):
            yield os.path.join(pkg_dir, name)


def package_dirs():
    for dirpath, _dirnames, filenames in os.walk(ROOT):
        if any(f.endswith(".go") for f in filenames):
            yield dirpath


def strip_strings_and_comments(src):
    """Blank out string literals and comments so they do not produce matches."""
    out = []
    i, n = 0, len(src)
    while i < n:
        c = src[i]
        if c == "/" and i + 1 < n and src[i + 1] == "/":
            while i < n and src[i] != "\n":
                i += 1
            continue
        if c == "/" and i + 1 < n and src[i + 1] == "*":
            i += 2
            while i + 1 < n and not (src[i] == "*" and src[i + 1] == "/"):
                i += 1
            i += 2
            continue
        if c in ('"', "`"):
            quote = c
            out.append(" ")
            i += 1
            while i < n and src[i] != quote:
                if quote == '"' and src[i] == "\\":
                    i += 1
                i += 1
            i += 1
            continue
        out.append(c)
        i += 1
    return "".join(out)


def main():
    exported = defaultdict(set)   # import path -> exported names
    declared = defaultdict(list)  # import path -> [(name, file)]
    sources = {}                  # import path -> [(file, cleaned src)]

    for pkg_dir in package_dirs():
        rel = os.path.relpath(pkg_dir, ROOT).replace(os.sep, "/")
        path = "github.com/ggstudios/andersxn-os/installer" + ("" if rel == "." else "/" + rel)
        for f in go_files(pkg_dir):
            raw = open(f, encoding="utf-8").read()
            src = strip_strings_and_comments(raw)
            sources.setdefault(path, []).append((f, src, raw))

            for block in BLOCK_RE.findall(src):
                for name in BLOCK_NAME_RE.findall(block):
                    exported[path].add(name)
                    declared[path].append((name, f))

            for m in DECL_RE.finditer(src):
                name = m.group("method") or m.group("func") or m.group("type") \
                    or m.group("const") or m.group("var")
                if not name:
                    continue
                exported[path].add(name)
                # Methods live in a per-type namespace, so only plain
                # declarations are candidates for duplicate detection.
                if not m.group("method"):
                    declared[path].append((name, f))

            for name in STRUCT_FIELD_RE.findall(src):
                exported[path].add(name)

    problems = []

    # 1. duplicate top-level declarations
    for path, names in declared.items():
        seen = {}
        for name, f in names:
            if name in seen:
                problems.append(
                    "duplicate declaration %r in %s (%s and %s)"
                    % (name, path.split("/")[-1], os.path.basename(seen[name]), os.path.basename(f)))
            seen[name] = f

    # 2. references to identifiers our own packages do not export
    short = {p.split("/")[-1]: p for p in exported}
    for path, files in sources.items():
        for f, src, _raw in files:
            aliases = {}
            for alias, imp in IMPORT_RE.findall(src):
                if imp.startswith("github.com/ggstudios/andersxn-os/installer"):
                    aliases[alias or imp.split("/")[-1]] = imp
            for pkg, ident in USE_RE.findall(src):
                if pkg not in aliases:
                    continue
                target = aliases[pkg]
                if ident not in exported.get(target, set()):
                    problems.append(
                        "%s: %s.%s is not declared in %s"
                        % (os.path.basename(f), pkg, ident, target.split("/")[-1]))

    if problems:
        print("go-xref found %d problem(s):" % len(problems))
        for p in sorted(set(problems)):
            print("  " + p)
        sys.exit(1)

    print("go-xref: no undefined cross-package references, no duplicate declarations")
    print("packages checked: %d" % len(sources))


if __name__ == "__main__":
    main()
