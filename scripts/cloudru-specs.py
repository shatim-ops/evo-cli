#!/usr/bin/env python3
"""Download the official Cloud.ru API specifications evo talks to.

The specs are Cloud.ru's published documentation (https://cloud.ru/docs):
the API reference page of each service links its OpenAPI/Swagger file. They
are not kept in this repository; this script fetches the current ones into a
directory (default .cache/cloudru-specs, git-ignored) with a manifest
(service, URL, sha256, OpenAPI version, servers, number of paths), so a
contract check can compare evo's requests with what Cloud.ru publishes and
a changed spec shows up as a changed hash.

Usage: python3 scripts/cloudru-specs.py [OUT_DIR]
Needs: curl, PyYAML (pip install pyyaml).
"""
import hashlib
import json
import os
import re
import subprocess
import sys
import time

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
}
SPEC_RE = re.compile(r"((?:https://cloud\.ru)?/docs/api/specs/[^\"?\s'\\]+\.(?:ya?ml|json))")
MAX_PAGES = 30  # per service: the reference and its api sub-pages


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


def main():
    out = sys.argv[1] if len(sys.argv) > 1 else ".cache/cloudru-specs"
    os.makedirs(out, exist_ok=True)
    manifest, missing = [], []
    for service, start in START_PAGES.items():
        urls, failed = spec_urls(start)
        if not urls:
            missing.append(service)
        missing.extend(f"{service}: page {p} did not load" for p in failed)
        for u in urls:
            d = os.path.join(out, service)
            os.makedirs(d, exist_ok=True)
            path = os.path.join(d, u.rsplit("/", 1)[1])
            r = subprocess.run(["curl", "-sSL", "--fail", "--max-time", "120", "-o", path, u])
            if r.returncode != 0:
                missing.append(f"{service}: {u}")
                continue
            data = open(path, "rb").read()
            entry = {"service": service, "file": os.path.relpath(path, out), "url": u,
                     "size": len(data), "sha256": hashlib.sha256(data).hexdigest()}
            entry.update(describe(data))
            manifest.append(entry)
            print(f"{service:20} {entry['file']:50} {entry.get('paths', '?'):>4} paths")
    with open(os.path.join(out, "manifest.json"), "w") as f:
        json.dump({"source": BASE + "/docs", "specs": manifest, "missing": missing},
                  f, indent=1, ensure_ascii=False)
    if missing:
        print("no spec found:", ", ".join(missing), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
