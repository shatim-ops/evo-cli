#!/usr/bin/env python3
"""Download the official Cloud.ru API specifications evo talks to.

The specs are Cloud.ru's published documentation (https://cloud.ru/docs):
the API reference page of each service links its OpenAPI/Swagger file. They
are not kept in this repository; this script fetches the current ones into a
directory (default .cache/cloudru-specs, git-ignored) with a manifest
(service, URL, sha256, OpenAPI version, servers, number of paths), so a
contract check can compare evo's requests with what Cloud.ru publishes and
a changed spec shows up as a changed hash.

A service whose page or file did not load, or that lacks a spec the
contract check needs, is fetched again on its own, up to ROUNDS times; the
others are kept. Services are fetched WORKERS at a time.

Usage: python3 scripts/cloudru-specs.py [OUT_DIR]
Needs: curl, PyYAML (pip install pyyaml).
Tests: python3 scripts/cloudru-specs-test.py
"""
import hashlib
import json
import os
import re
import subprocess
import sys
import time
from concurrent.futures import ThreadPoolExecutor

BASE = "https://cloud.ru"

# API reference pages of the services evo calls (one per *.api.cloud.ru
# host in internal/), from the registry of Cloud.ru APIs:
# https://cloud.ru/docs/console_api/ug/topics/overview__reestr_api
START_PAGES = {
    "compute": "/docs/virtual-machines/ug/topics/api-ref",
    "vpc": "/docs/evolution-vpc/ug/topics/api-ref",
    "dns": "/docs/evolution-dns/ug/topics/api-ref",
    "nlb": "/docs/nlb/ug/topics/api-ref",
    "mk8s": "/docs/kubernetes-evolution/ug/topics/api-ref",
    "postgresql": "/docs/paas-postgresql/ug/topics/api-ref",
    "kafka": "/docs/paas-kafka/ug/topics/api-ref",
    "redis": "/docs/redis/ug/topics/api-ref",
    "ar": "/docs/artifact-registry-evolution/ug/topics/api-ref",
    "containers": "/docs/container-apps-evolution/ug/topics/api-ref",
    "secretmanager": "/docs/scsm/ug/topics/api-ref",
    "certificatemanager": "/docs/certificate-manager/ug/topics/api-ref",
    "monitoring": "/docs/service-monitoring/ug/topics/api-ref",
    "audit": "/docs/audit-logging/ug/topics/api",
    "billing": "/docs/billing/ug/topics/api-ref",
    "organization+iam": "/docs/administration/ug/topics/guides__directory-api",
    # Data Platform (1.3): one host, dataplatform.api.cloud.ru, a spec per
    # product. BI has no API page (404 on 22.10.2026).
    "dataplatform": "/docs/dataplatform/ug/topics/api",
    "spark": "/docs/spark/ug/topics/api",
    "trino": "/docs/trino/ug/topics/api",
    "managed-airflow": "/docs/managed-airflow/ug/topics/api",
    "metastore": "/docs/metastore/ug/topics/api",
    "managed-flink": "/docs/managed-flink/ug/topics/api",
}
SPEC_RE = re.compile(r"((?:https://cloud\.ru)?/docs/api/specs/[^\"?\s'\\]+\.(?:ya?ml|json))")
MAX_PAGES = 30  # per service: the reference and its api sub-pages
# A full pass over the docs takes minutes and some one of its ~30 files
# fails now and then, a different one each time (#245): only the services
# that came back incomplete are fetched again.
ROUNDS = 3
# Moderate on purpose: the docs site drops requests when hit harder.
WORKERS = 4


def get(url, attempts=3):
    # The docs site drops a request now and then: retry before deciding a
    # page has no spec link.
    code, body = "000", ""
    for i in range(attempts):
        r = subprocess.run(["curl", "-sSL", "--max-time", "60", "-w", "\n%{http_code}", url],
                           capture_output=True, text=True)
        body, _, code = r.stdout.rpartition("\n")
        if code == "200" and body:
            return code, body
        if code == "404":
            break
        time.sleep(2 * (i + 1))
    return code, body


def spec_urls(start):
    slug = start.split("/")[2]
    link_re = re.compile(r'href="(/docs/' + re.escape(slug) + r'/[^"?#]*/topics/[^"?#]*api[^"?#]*)')
    seen, queue, specs, failed = set(), [start], set(), []
    while queue and len(seen) < MAX_PAGES:
        page = queue.pop(0)
        if page in seen:
            continue
        seen.add(page)
        code, html = get(BASE + page)
        if code == "404":
            continue
        if code != "200":
            # A page that did not load may hold the only link to a spec: the
            # check would then report it missing from the code's point of view.
            failed.append(page)
            continue
        for m in SPEC_RE.findall(html):
            specs.add(m if m.startswith("http") else BASE + m)
        queue.extend(p for p in link_re.findall(html) if p not in seen)
    # The same file is often linked twice: as _specs/NAME and _downloads/HASH/NAME.
    by_name = {}
    for u in sorted(specs, key=lambda u: "/_downloads/" in u):
        by_name.setdefault(u.rsplit("/", 1)[1], u)
    return sorted(by_name.values()), failed


def describe(data):
    try:
        import yaml
    except ImportError:
        return {}
    try:
        y = yaml.safe_load(data)
    except Exception as e:  # noqa: BLE001 — report, do not stop
        return {"parse_error": str(e)[:200]}
    if not isinstance(y, dict):
        return {}
    servers = [s.get("url") for s in y.get("servers") or [] if isinstance(s, dict)]
    if not servers and y.get("host"):
        servers = [y["host"]]
    return {
        "openapi": y.get("openapi") or y.get("swagger"),
        "title": (y.get("info") or {}).get("title"),
        "version": (y.get("info") or {}).get("version"),
        "servers": servers,
        "paths": len(y.get("paths") or {}),
    }


def contract_specs():
    """Spec files (service/file) scripts/api-contract.json checks against."""
    path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "api-contract.json")
    try:
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
    except (OSError, ValueError):
        return []
    found = set()

    def walk(v):
        if isinstance(v, dict):
            spec = v.get("spec")
            if isinstance(spec, str) and "/" in spec:
                found.add(spec)
            for x in v.values():
                walk(x)
        elif isinstance(v, list):
            for x in v:
                walk(x)

    walk(data)
    return sorted(found)


def download(url, path):
    return subprocess.run(["curl", "-sSL", "--fail", "--max-time", "120", "-o", path, url]).returncode == 0


def fetch_service(service, start, out):
    """Spec files of one service: (manifest entries, problems)."""
    urls, failed = spec_urls(start)
    problems = [f"{service}: page {p} did not load" for p in failed]
    if not urls:
        problems.append(service)
    entries = []
    for u in urls:
        d = os.path.join(out, service)
        os.makedirs(d, exist_ok=True)
        path = os.path.join(d, u.rsplit("/", 1)[1])
        if not download(u, path):
            problems.append(f"{service}: {u}")
            continue
        with open(path, "rb") as f:
            data = f.read()
        entry = {"service": service, "file": os.path.relpath(path, out), "url": u,
                 "size": len(data), "sha256": hashlib.sha256(data).hexdigest()}
        entry.update(describe(data))
        entries.append(entry)
    return entries, problems


def unmet(results, needs):
    """Specs the contract check needs that no fetched service has."""
    got = {e["file"] for entries, _ in results.values() for e in entries}
    return [need for need in needs if need not in got]


def main():
    out = sys.argv[1] if len(sys.argv) > 1 else ".cache/cloudru-specs"
    os.makedirs(out, exist_ok=True)
    needs = contract_specs()
    results = {}
    pending = list(START_PAGES)
    for i in range(ROUNDS):
        if i:
            print(f"round {i + 1}: fetching again {', '.join(pending)}", file=sys.stderr)
            time.sleep(20 * i)
        with ThreadPoolExecutor(WORKERS) as pool:
            fetched = pool.map(lambda s: fetch_service(s, START_PAGES[s], out), pending)
            results.update(zip(pending, fetched))
        # A spec page that loads but lists fewer links (seen 19.10.2026: the
        # administration page without api.op_v1.v2.yaml) would leave a spec
        # the contract check needs silently absent: fetch its service again.
        lacking = {need.split("/")[0] for need in unmet(results, needs)}
        pending = [s for s in START_PAGES if results[s][1] or s in lacking]
        if not pending:
            break
    manifest, missing = [], []
    for service in START_PAGES:
        entries, problems = results[service]
        manifest.extend(entries)
        missing.extend(problems)
        for e in entries:
            print(f"{service:20} {e['file']:50} {e.get('paths', '?'):>4} paths")
    missing.extend(f"{need}: needed by scripts/api-contract.json, not found on the API pages"
                   for need in unmet(results, needs))
    with open(os.path.join(out, "manifest.json"), "w") as f:
        json.dump({"source": BASE + "/docs", "specs": manifest, "missing": missing},
                  f, indent=1, ensure_ascii=False)
    if missing:
        print("no spec found:", ", ".join(missing), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
