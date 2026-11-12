#!/usr/bin/env python3
"""The requests of the live probes of a version, as Cloud.ru's specs give them.

A probe of the platform (input probes of cloud-e2e.yml) sends requests evo
has no code for yet, so scripts/api-contract.json — the inventory of evo's
own requests — does not hold them. This script takes the list of requests
of each probe (scripts/probe-requests.json) and writes, from the specs
that scripts/cloudru-specs.py downloads, what each request is: query
parameters, body fields (a dot path, [] — an array, * — required, enum
values) and the declared response codes. A probe is then written from the
spec, not from a guess, and a difference between the platform and the spec
is a finding, not an error of the probe (plan 1.9, probe Г-04).

A request the specs do not have is written as such and listed at the end;
the exit code is then 1 with --strict.

Usage: python3 scripts/probe-requests.py [--strict] [SPECS_DIR] > docs/internal/probes/requests-1.9.md
       (SPECS_DIR — .cache/cloudru-specs by default)
Needs: PyYAML. Tests: python3 scripts/probe-requests-test.py
"""
import json
import os
import sys

MAX_DEPTH = 4
COMBINERS = ("allOf", "anyOf", "oneOf")


def _deref(doc, node, seen):
    """node with its $ref chain followed; None for a ref already on the path."""
    while isinstance(node, dict) and "$ref" in node:
        ref = node["$ref"]
        if ref in seen:
            return None, seen
        seen = seen + (ref,)
        target = doc
        for part in ref.lstrip("#/").split("/"):
            target = target[part]
        node = target
    return node, seen


def _shape(doc, node, seen):
    """(types, enum) of a property: every type of its anyOf, its enum values."""
    types, enum = [], None
    node, seen = _deref(doc, node, seen)
    if not isinstance(node, dict):
        return types, enum
    if node.get("type"):
        types.append(node["type"])
    if node.get("enum") and len(node["enum"]) > 1:
        enum = node["enum"]
    for key in COMBINERS:
        for sub in node.get(key, []):
            t, e = _shape(doc, sub, seen)
            types += [x for x in t if x not in types]
            enum = enum or e
    return types, enum


def _fields(doc, schema, prefix, seen, depth, out):
    schema, seen = _deref(doc, schema, seen)
    if not isinstance(schema, dict) or depth > MAX_DEPTH:
        return
    for key in COMBINERS:
        for sub in schema.get(key, []):
            _fields(doc, sub, prefix, seen, depth, out)
    if "items" in schema:
        _fields(doc, schema["items"], prefix + "[]", seen, depth, out)
        return
    required = set(schema.get("required", []))
    for name, prop in (schema.get("properties") or {}).items():
        path = f"{prefix}.{name}" if prefix else name
        if any(f["name"] == path for f in out):
            continue
        types, enum = _shape(doc, prop, seen)
        field = {"name": path, "type": " | ".join(types) or "?", "required": name in required}
        if enum:
            field["enum"] = enum
        out.append(field)
        _fields(doc, prop, path, seen, depth + 1, out)


def describe(doc, method, path):
    """The request as the spec gives it; {"absent": True} when it does not."""
    op = (doc.get("paths", {}).get(path) or {}).get(method.lower())
    if not op:
        return {"absent": True}
    params = []
    for p in (doc["paths"][path].get("parameters") or []) + (op.get("parameters") or []):
        p, _ = _deref(doc, p, ())
        if p:
            params.append(p)
    body = None
    if "requestBody" in op:
        rb, _ = _deref(doc, op["requestBody"], ())
        content = (rb or {}).get("content") or {}
        body = next(iter(content.values()), {}).get("schema")
    else:  # Swagger 2
        body = next((p.get("schema") for p in params if p.get("in") == "body"), None)
    fields = []
    if body is not None:
        _fields(doc, body, "", (), 0, fields)
    return {
        "summary": op.get("summary", ""),
        "query": [p["name"] + ("*" if p.get("required") else "") for p in params if p.get("in") == "query"],
        "body": fields,
        "responses": [str(c) for c in (op.get("responses") or {})],
    }


def _load(path):
    with open(path, encoding="utf-8") as f:
        if path.endswith(".json"):
            return json.load(f)
        import yaml
        return yaml.safe_load(f)


def render(plan, specs_dir):
    """(markdown, absent) — absent: the requests the specs do not have."""
    docs, lines, absent = {}, [], []
    for probe in plan["probes"]:
        lines += [f"### {probe['id']}. {probe['title']}", ""]
        for req in probe["requests"]:
            spec = req["spec"]
            if spec not in docs:
                docs[spec] = _load(os.path.join(specs_dir, spec))
            r = describe(docs[spec], req["method"], req["path"])
            head = f"`{req['method']} {req['path']}` ({spec})"
            note = f" — {req['note']}" if req.get("note") else ""
            if r.get("absent"):
                lines += [f"- {head} — **нет в спецификации**{note}"]
                absent.append(f"{probe['id']}: {req['method']} {req['path']} ({spec})")
                continue
            summary = f" — {r['summary']}" if r["summary"] else ""
            lines += [f"- {head}{summary}{note}"]
            if r["query"]:
                lines += ["  - query: " + ", ".join(f"`{q}`" for q in r["query"])]
            for f in r["body"]:
                enum = ": " + ", ".join(f"`{v}`" for v in f["enum"]) if f.get("enum") else ""
                lines += [f"  - `{f['name']}`{'*' if f['required'] else ''} — {f['type']}{enum}"]
            lines += ["  - ответы: " + ", ".join(r["responses"])]
        lines += [""]
    return "\n".join(lines), absent


def main(argv):
    args = [a for a in argv if not a.startswith("-")]
    if "-h" in argv or "--help" in argv:
        print(__doc__)
        return 0
    specs_dir = args[0] if args else os.path.join(".cache", "cloudru-specs")
    here = os.path.dirname(os.path.abspath(__file__))
    with open(os.path.join(here, "probe-requests.json"), encoding="utf-8") as f:
        plan = json.load(f)
    text, absent = render(plan, specs_dir)
    print("\n".join(plan["header"]))
    print()
    print(text)
    if absent:
        print("## Запросы, которых нет в спецификациях\n")
        for a in absent:
            print(f"- {a}")
    return 1 if absent and "--strict" in argv else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
