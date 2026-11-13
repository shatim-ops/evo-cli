#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
# SPDX-FileContributor: Тимур Шакирбеков
# SPDX-License-Identifier: Apache-2.0
"""Catalogue lines from the file of EVO_RECORD_ANSWERS (plan 1.9, #1716).

The raw file holds the host and the segments of the path that are not ids
(a bucket, a key of S3, a DNS zone): it is never committed or uploaded
(«Риск», conclusion 206, condition У2). What leaves the job is this
output: a line passes only when its host is a service of the inventory
and its path matches a template of scripts/api-contract.json or
scripts/probe-requests.json — the request out is the template, not the
record; a path of the body passes only when scripts/api-contract-fields.json
lists it for that request (so the keys of labels and tags do not). What
is dropped is counted on stderr by reason.

usage: answers_catalogue.py --root REPO --source TEXT --date YYYY-MM-DD RAW
"""
import argparse
import json
import os
import re
import sys
from urllib.parse import urlsplit

API_HOST = re.compile(r"^([a-z0-9-]+)\.api\.cloud\.ru$")


def templates(root):
    """(service, method) -> [path templates] from both inventories."""
    out = {}
    with open(os.path.join(root, "scripts", "api-contract.json"), encoding="utf-8") as f:
        for s in json.load(f)["services"]:
            for q in s["requests"]:
                out.setdefault((s["name"], q["method"]), set()).add(q["path"])
    with open(os.path.join(root, "scripts", "probe-requests.json"), encoding="utf-8") as f:
        for p in json.load(f)["probes"]:
            for q in p["requests"]:
                svc = q["spec"].split("/", 1)[0]
                out.setdefault((svc, q["method"]), set()).add(q["path"])
    return out


def match(tmpls, path):
    """The template path matches, the most literal one; None when none."""
    segs = path.strip("/").split("/")
    best, best_params = None, None
    for t in tmpls:
        ts = t.strip("/").split("/")
        if len(ts) != len(segs):
            continue
        params = 0
        for a, b in zip(ts, segs):
            if a.startswith("{") and a.endswith("}"):
                params += 1
            elif a != b:
                break
        else:
            if best is None or params < best_params:
                best, best_params = t, params
    return best


def slug(text):
    return re.sub(r"[^a-z0-9]+", "-", text.lower()).strip("-")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", required=True)
    ap.add_argument("--source", required=True)
    ap.add_argument("--date", required=True)
    ap.add_argument("raw")
    a = ap.parse_args()

    tmpl = templates(a.root)
    with open(os.path.join(a.root, "scripts", "api-contract-fields.json"), encoding="utf-8") as f:
        fields = json.load(f)["requests"]
    services = {svc for svc, _ in tmpl}

    count = {"строк": 0, "принято": 0, "не JSON": 0, "хост вне описи": 0, "путь вне описи": 0, "полей вне описи": 0}
    entries = {}
    lines = []
    if os.path.exists(a.raw):
        with open(a.raw, encoding="utf-8", errors="replace") as f:
            lines = f.read().splitlines()
    for raw in lines:
        if not raw.strip():
            continue
        count["строк"] += 1
        try:
            rec = json.loads(raw)
            method, url = rec["request"].split(" ", 1)
            status = int(rec["status"])
            paths = rec.get("paths") or {}
        except (ValueError, KeyError, TypeError, AttributeError):
            count["не JSON"] += 1
            continue
        u = urlsplit(url)
        m = API_HOST.match(u.hostname or "")
        svc = m.group(1) if m else None
        if svc not in services:
            count["хост вне описи"] += 1
            continue
        t = match(tmpl.get((svc, method), ()), u.path)
        if t is None:
            count["путь вне описи"] += 1
            continue
        allowed = set(fields.get(f"{svc} {method} {t}", ()))
        kept = {}
        for p, kind in paths.items():
            if "*" in p.split(".") or p.replace(".[]", "[]") not in allowed:
                count["полей вне описи"] += 1
                continue
            kept[p] = kind
        count["принято"] += 1
        request = f"{method} https://{u.hostname}{t}"
        key = (request, status)
        e = entries.setdefault(key, {
            "id": slug(f"{svc}-{method}-{t}-{status}"),
            "request": request,
            "status": status,
            "source": a.source,
            "recorded": a.date,
            "paths": {},
        })
        for p, kind in kept.items():
            prev = e["paths"].get(p)
            # Two records that disagree on a kind leave it unknown.
            e["paths"][p] = kind if prev in (None, kind) else "?"

    print(json.dumps(sorted(entries.values(), key=lambda e: e["id"]), ensure_ascii=False, indent=2, sort_keys=True))
    print("answers-catalogue: " + ", ".join(f"{k}: {v}" for k, v in count.items()), file=sys.stderr)


if __name__ == "__main__":
    main()
