#!/usr/bin/env python3
"""Validate a JSONC file by stripping // comments outside strings, then parsing."""
import json
import sys


def strip_comments(text):
    out = []
    in_str = esc = False
    i = 0
    while i < len(text):
        ch = text[i]
        if in_str:
            out.append(ch)
            if esc:
                esc = False
            elif ch == "\\":
                esc = True
            elif ch == '"':
                in_str = False
            i += 1
            continue
        if ch == '"':
            in_str = True
            out.append(ch)
            i += 1
            continue
        if ch == "/" and i + 1 < len(text) and text[i + 1] == "/":
            while i < len(text) and text[i] != "\n":
                i += 1
            continue
        out.append(ch)
        i += 1
    return "".join(out)


for path in sys.argv[1:]:
    with open(path, encoding="utf-8") as fh:
        raw = fh.read()
    try:
        json.loads(strip_comments(raw))
    except json.JSONDecodeError as exc:
        print("INVALID %s: %s" % (path, exc))
        sys.exit(1)
    print("valid JSONC: %s" % path)
