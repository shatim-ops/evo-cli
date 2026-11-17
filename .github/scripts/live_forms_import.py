#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
# SPDX-FileContributor: Тимур Шакирбеков
# SPDX-License-Identifier: Apache-2.0
"""Carry the live forms of a run into the catalogue of live answers (plan
1.9, «миры по спецификации, а не по живой форме»; cloud-e2e 226: the
catalogue had grown from api-probe alone, and a world of backups answered
as the specification has it).

The input is the artifact answers-<job> of a live run: lines that
answers_catalogue.py already let out — a host of the inventory, a path of
a template, field paths of the contract, kinds and no values. This script
adds a line for a request the catalogue has no line for, and only its
form: id, request, status, source, recorded and paths whose kind is one of
KINDS; any other field of the input is dropped. A request the catalogue
has keeps its line: a form seen again changes nothing, a form that differs
is printed for the person to read, not merged. The public trace of the
repository (internal/repolayout) reads the file as any other, and
TestPlatformAnswers_FormsOnly holds that it holds forms alone.

usage: live_forms_import.py CATALOGUE ANSWERS.json…
"""
import json
import re
import sys

KINDS = {"uuid", "name", "number", "bool", "null", "object", "array", "?"}
SEGMENT = re.compile(r"^(\[\]|[A-Za-z_@][A-Za-z0-9_@-]*)$")
REQUEST = re.compile(r"^(GET|POST|PUT|PATCH|DELETE) https://[a-z0-9-]+\.api\.cloud\.ru/[A-Za-z0-9_./{}?=&-]*$")


def form(line):
    paths = {}
    for p, kind in sorted((line.get("paths") or {}).items()):
        if kind in KINDS and all(SEGMENT.match(s) for s in p.split(".")):
            paths[p] = kind
    return paths


def main(argv):
    if len(argv) < 3:
        print(__doc__.strip().splitlines()[-1], file=sys.stderr)
        return 2
    catalogue = argv[1]
    with open(catalogue, encoding="utf-8") as f:
        lines = json.load(f)
    by_request = {(x["request"], x["status"]): x for x in lines}
    ids = {x["id"] for x in lines}
    added = differ = 0
    for path in argv[2:]:
        with open(path, encoding="utf-8") as f:
            for a in json.load(f):
                req, status = a.get("request", ""), a.get("status")
                if not REQUEST.match(req) or not isinstance(status, int):
                    continue
                paths = form(a)
                have = by_request.get((req, status))
                if have is not None:
                    if set(paths) - set(have["paths"]) - set(have.get("open") or []):
                        differ += 1
                        print(f"differs from the catalogue: {have['id']}: {len(set(paths) - set(have['paths']))} path(s) not in its line")
                    continue
                if not paths or a.get("id") in ids:
                    continue
                line = {"id": a["id"], "request": req, "status": status,
                        "source": a.get("source", ""), "recorded": a.get("recorded", ""), "paths": paths}
                lines.append(line)
                by_request[(req, status)] = line
                ids.add(a["id"])
                added += 1
    with open(catalogue, "w", encoding="utf-8") as f:
        json.dump(lines, f, ensure_ascii=False, indent=2)
        f.write("\n")
    print(f"added: {added}, differ: {differ}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
