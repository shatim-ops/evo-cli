#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
# SPDX-FileContributor: Тимур Шакирбеков
# SPDX-License-Identifier: Apache-2.0
"""Checks a session journal of evo (evo diag export, D46) before it is read.

The journal is the participant's file, not ours: it is only parsed as JSON
and matched against fixed rules, never executed. It holds metadata only
(docs/journal.md); a field or a value outside that is a defect of evo or a
file that is not a journal, and fails the check.

Usage: diag-check.py [--public] [--sha256 HEX] FILE

  --public    also fail on anything that names the cloud or a person —
              a UUID, an IP address, a host name, an e-mail — anywhere in
              the file: the file may go where the repository's rules hold
              (a public issue must never carry it; the triage reads this).
  --sha256    the sha256 the issue gives for the file; another one fails.

Prints a summary (JSON) on stdout and the failures on stderr. Exit 0 — the
journal passes, 1 — it does not, 2 — the call is wrong.
"""
import hashlib
import json
import re
import sys

FIELDS = {
    "time": str, "evo": str, "session": str, "surface": str, "os": str,
    "event": str, "tool": str, "args": list, "unknown_args": int, "depth": int,
    "confirmed": bool, "preview": bool, "error": bool, "reason": str, "ms": int, "bytes": int,
}
SESSION_FIELDS = ("time", "evo", "session", "surface", "os")
SURFACES = {"cli", "mcp-stdio", "mcp-http"}
EVENTS = {"journal_full"}
NAME = re.compile(r"^[a-z][a-z0-9_]*$")
REASON = re.compile(r"^[A-Z][A-Z_]*$")
TIME = re.compile(r"^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$")
SESSION = re.compile(r"^[0-9a-f]{12}$")
OS = re.compile(r"^[a-z0-9]+/[a-z0-9]+$")
PUBLIC = [
    ("UUID", re.compile(r"(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b")),
    ("IPv4", re.compile(r"\b(?:\d{1,3}\.){3}\d{1,3}\b")),
    ("IPv6", re.compile(r"(?i)\b(?:[0-9a-f]{1,4}:){3,7}[0-9a-f]{1,4}\b")),
    ("e-mail", re.compile(r"[\w.+-]+@[\w-]+\.[\w.-]+")),
    ("host name", re.compile(r"(?i)\b[a-z0-9-]+(?:\.[a-z0-9-]+)*\.(?:ru|com|net|org|io|local|internal|cloud)\b")),
]


def check_record(n, r, last):
    """The failures of record r, line n (1-based); last: the last line."""
    bad = []
    if not isinstance(r, dict):
        return [f"line {n}: not a JSON object"]
    for k, v in r.items():
        if k not in FIELDS:
            bad.append(f"line {n}: field {k!r} is no field of a journal record")
        elif not isinstance(v, FIELDS[k]) or (FIELDS[k] is int and isinstance(v, bool)):
            bad.append(f"line {n}: {k} is {type(v).__name__}, not {FIELDS[k].__name__}")
    for k in SESSION_FIELDS:
        if not r.get(k):
            bad.append(f"line {n}: no {k} (every record names its time, version, session, surface and OS)")
    if isinstance(r.get("time"), str) and r["time"] and not TIME.match(r["time"]):
        bad.append(f"line {n}: time is not UTC to the second")
    if isinstance(r.get("session"), str) and r["session"] and not SESSION.match(r["session"]):
        bad.append(f"line {n}: session is not a session mark")
    if r.get("surface") and r.get("surface") not in SURFACES:
        bad.append(f"line {n}: surface is none of {sorted(SURFACES)}")
    if isinstance(r.get("os"), str) and r["os"] and not OS.match(r["os"]):
        bad.append(f"line {n}: os is not os/arch")
    if "event" in r:
        if r["event"] not in EVENTS:
            bad.append(f"line {n}: event is none of {sorted(EVENTS)}")
        elif not last:
            bad.append(f"line {n}: journal_full is not the last record")
        if "tool" in r:
            bad.append(f"line {n}: an event record names a tool")
        return bad
    if not isinstance(r.get("tool"), str) or not NAME.match(r["tool"]):
        bad.append(f"line {n}: tool is not a tool's name")
    for a in r.get("args", []) if isinstance(r.get("args"), list) else []:
        if not isinstance(a, str) or not NAME.match(a):
            bad.append(f"line {n}: args holds something that is not an argument's name")
            break
    if isinstance(r.get("reason"), str) and not REASON.match(r["reason"]):
        bad.append(f"line {n}: reason is not a code")
    if r.get("reason") and not r.get("error"):
        bad.append(f"line {n}: a reason without an error")
    return bad


def check(data, public=False, sha=None):
    """(summary, failures) of the journal's bytes."""
    digest = hashlib.sha256(data).hexdigest()
    bad = []
    if sha is not None and sha.lower() != digest:
        bad.append(f"sha256 is {digest}, not the {sha} the issue gives")
    try:
        text = data.decode("utf-8")
    except UnicodeDecodeError:
        return {"sha256": digest}, bad + ["not UTF-8"]
    lines = [l for l in text.split("\n") if l.strip()]
    records, sessions, versions, tools, full = 0, set(), set(), set(), False
    first = last = ""
    for i, line in enumerate(lines, 1):
        try:
            r = json.loads(line)
        except json.JSONDecodeError:
            bad.append(f"line {i}: not JSON")
            continue
        bad += check_record(i, r, i == len(lines))
        if not isinstance(r, dict):
            continue
        if r.get("event") == "journal_full":
            full = True
            continue
        records += 1
        sessions.add(r.get("session"))
        versions.add(r.get("evo"))
        tools.add(r.get("tool"))
        first = first or r.get("time", "")
        last = r.get("time", last)
    if public:
        # Every line of each kind, so that one pass finds all of them.
        for what, pat in PUBLIC:
            hit = [str(i) for i, line in enumerate(lines, 1) if pat.search(line)]
            if hit:
                bad.append(f"line {', '.join(hit)}: {what} in the journal — it must not go public")
    summary = {
        "sha256": digest, "bytes": len(data), "records": records, "sessions": len(sessions),
        "versions": sorted(v for v in versions if isinstance(v, str)), "tools": len(tools),
        "first": first, "last": last, "full": full,
    }
    return summary, bad


def main(argv):
    public, sha, path = False, None, None
    i = 0
    while i < len(argv):
        a = argv[i]
        if a == "--public":
            public = True
        elif a == "--sha256" and i + 1 < len(argv):
            i += 1
            sha = argv[i]
        elif a.startswith("-") or path is not None:
            print(__doc__, file=sys.stderr)
            return 2
        else:
            path = a
        i += 1
    if path is None:
        print(__doc__, file=sys.stderr)
        return 2
    try:
        with open(path, "rb") as f:
            data = f.read()
    except OSError as e:
        print(f"diag-check: {e.strerror}: {path}", file=sys.stderr)
        return 2
    summary, bad = check(data, public, sha)
    print(json.dumps(summary, ensure_ascii=False, indent=2))
    for b in bad:
        print("diag-check: " + b, file=sys.stderr)
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
