#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
# SPDX-FileContributor: Тимур Шакирбеков
# SPDX-License-Identifier: Apache-2.0
"""Offline tests of diag-check.py on journals written as evo writes them."""
import hashlib
import importlib.util
import json
import os
import sys

spec = importlib.util.spec_from_file_location("diag_check", os.path.join(os.path.dirname(__file__), "diag-check.py"))
dc = importlib.util.module_from_spec(spec)
spec.loader.exec_module(dc)

BASE = {"time": "2026-10-27T10:00:00Z", "evo": "1.4.0", "session": "0123456789ab", "surface": "mcp-stdio", "os": "linux/amd64"}


def rec(**kw):
    r = dict(BASE)
    r.update(kw)
    return json.dumps(r, ensure_ascii=False)


def journal(*lines):
    return ("\n".join(lines) + "\n").encode()


fail = 0


def case(name, data, want_ok, public=False, sha=None, want=None):
    global fail
    summary, bad = dc.check(data, public, sha)
    ok = not bad
    if ok != want_ok or (want and not any(want in b for b in bad)):
        print(f"FAIL {name}: ok={ok}, failures {bad}")
        fail = 1
    else:
        print(f"ok   {name}")
    return summary


good = journal(
    rec(tool="vm_list", ms=12, bytes=340),
    rec(tool="pg_delete", args=["cluster_id"], preview=True, ms=30, bytes=210),
    rec(tool="pg_delete", args=["cluster_id"], confirmed=True, error=True, reason="API_ERROR", ms=900, bytes=180),
    rec(tool="stack_apply", args=["job_id"], depth=1, ms=5, bytes=90, session="ba9876543210", surface="cli"),
    rec(event="journal_full", ms=0, bytes=0),
)
s = case("journal as evo writes it", good, True, public=True)
if (s["records"], s["sessions"], s["full"], s["tools"]) != (4, 2, True, 3):
    print(f"FAIL summary: {s}")
    fail = 1
case("sha256 the issue gives", good, True, sha=hashlib.sha256(good).hexdigest())
case("another sha256", good, False, sha="0" * 64, want="sha256 is")
case("a field outside the record", journal(rec(tool="vm_list", ms=1, bytes=1, value="x")), False, want="no field")
case("a value in args", journal(rec(tool="vm_list", args=["10.0.0.5"], ms=1, bytes=1)), False, want="not an argument's name")
case("a reason that is text", journal(rec(tool="vm_list", error=True, reason="dial tcp: refused", ms=1, bytes=1)), False, want="not a code")
case("no session", journal(json.dumps({"time": BASE["time"], "evo": "1.4.0", "surface": "cli", "os": "linux/amd64", "tool": "vm_list", "ms": 1, "bytes": 1})), False, want="no session")
case("journal_full not last", journal(rec(event="journal_full", ms=0, bytes=0), rec(tool="vm_list", ms=1, bytes=1)), False, want="not the last")
case("not JSON", b"{\n", False, want="not JSON")
case("a boolean for a number", journal(rec(tool="vm_list", ms=True, bytes=1)), False, want="ms is bool")
# --public: anything that names the cloud or a person, wherever it is.
uuid = "11110000-0000-4000-8000-000000000001"
case("UUID, public", journal(rec(tool="vm_list", ms=1, bytes=1, session=uuid)), False, public=True, want="UUID")
case("IPv4, public", journal(rec(tool="vm_list", ms=1, bytes=1, os="203.0.113.7")), False, public=True, want="IPv4")
case("host, public", journal(rec(tool="vm_list", ms=1, bytes=1, evo="api.example.com")), False, public=True, want="host name")
case("e-mail, public", journal(rec(tool="vm_list", ms=1, bytes=1, evo="a@b.example")), False, public=True, want="e-mail")
case("version is no IP", journal(rec(tool="vm_list", ms=1, bytes=1, evo="1.4.0")), True, public=True)

sys.exit(fail)
