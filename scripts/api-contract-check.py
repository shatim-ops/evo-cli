#!/usr/bin/env python3
"""Check evo's HTTP requests against the official Cloud.ru API specifications.

Prototype of the contract check described in docs/internal/api-contract.md.
It needs the specs that scripts/cloudru-specs.py downloads and the contract
file scripts/api-contract.json, which lists every request evo makes: method,
path, query parameters, body fields, response fields the code reads, enum
values and HTTP statuses the code relies on. For each request it reports:

  path/method   the path and method exist in the spec
  query         every required query parameter is sent; every sent one is
                declared; numeric values respect `maximum`
  body          every required body field is sent; every sent field is
                declared (an error when the schema forbids extra fields)
  reads         every response field the code reads exists in the 2xx schema
  enum          every enum value the code compares against is in the spec
  status        every HTTP status the code handles specially is documented

It also reads the Go code (internal/**/*.go, tests excluded) for the API
paths it requests — string literals starting with /api/, /v1/, /v2/, /v3/,
/events/ or an https URL on *.cloud.ru, with the `+` chain that follows —
and reports:

  code-uncovered   a path (and method, when the call site names it) the
                   code requests that no contract entry has: `error`
  contract-unused  a contract entry no code requests: `warn`
  code-ref         the entry's "code" file:line is not the call site: `info`

Literals that are no request, and templates whose segment is picked at run
time, go to "code_coverage" in the contract (ignore / expand, with a
reason). --code-only runs just this part and needs no specs.

With the field baseline scripts/api-contract-fields.json (the names of the
fields of every GET answer and every request body of the contract at the
last update, no more of the spec) it also reports, per request:

  field-new     a field the spec has and the baseline does not, that evo
                neither reads nor sends: `error` until evo reads it, or the
                request accepts "field-new:<field>" with a reason (release
                plan 1.6, the ratchet of the specs; «полнота ресурса», #496).
                Ignored without a declaration: deprecated fields, *_at and
                display* names, the fields of a mini reference ({id, name}),
                readOnly fields of a body. A new object is reported once,
                not with every field under it.
  field-gone    a field of the baseline the spec no longer has: `info`
  field-baseline-missing  a request without a baseline: `warn`

--update-fields rewrites the baseline from the specs, once the new fields
are read or accepted (a version's step, or a PR that reads a new field).

Findings are `error` (the request contradicts the spec), `warn` (the spec
says nothing, or the code reads a field the spec does not have) or `info`.
Each request may carry "accepted": {"<finding id>": "<why>"} for known,
deliberate deviations (live API behaviour that the spec lacks); those are
reported as accepted and do not fail the run. Exit status is 1 when any
unaccepted error remains, so a changed spec that breaks a request shows up
in CI.

Usage: python3 scripts/api-contract-check.py [--specs .cache/cloudru-specs]
           [--contract scripts/api-contract.json] [--root .] [--code-only]
           [--fields scripts/api-contract-fields.json] [--update-fields]
           [--markdown]
"""
import argparse
import fnmatch
import glob
import json
import os
import re
import sys

try:
    import yaml
except ImportError:  # pragma: no cover
    print("PyYAML is required: pip install pyyaml", file=sys.stderr)
    sys.exit(2)

METHODS = ("get", "post", "put", "patch", "delete")


class Spec:
    def __init__(self, path):
        self.path = path
        self.doc = yaml.safe_load(open(path, encoding="utf-8"))
        self.paths = self.doc.get("paths") or {}

    def resolve(self, node, stack=()):
        """Expand $ref recursively; recursive references stop at a stub."""
        if isinstance(node, dict):
            ref = node.get("$ref")
            if isinstance(ref, str) and ref.startswith("#/"):
                if ref in stack or len(stack) > 20:
                    return {"type": "object", "x-recursive": ref}
                cur = self.doc
                for part in ref[2:].split("/"):
                    cur = (cur or {}).get(part.replace("~1", "/").replace("~0", "~"), {})
                return self.resolve(cur, stack + (ref,))
            return {k: self.resolve(v, stack) for k, v in node.items()}
        if isinstance(node, list):
            return [self.resolve(v, stack) for v in node]
        return node

    def find_path(self, path):
        """Match a concrete or templated path; parameter names may differ."""
        if path in self.paths:
            return path
        want = re.sub(r"\{[^}]+\}", "{}", path)
        for p in self.paths:
            if re.sub(r"\{[^}]+\}", "{}", p) == want:
                return p
        return None

    def operation(self, method, path):
        p = self.find_path(path)
        if p is None:
            return None, None, None
        item = self.resolve(self.paths[p])
        op = item.get(method.lower())
        if op is None:
            return p, item, None
        params = list(item.get("parameters") or []) + list(op.get("parameters") or [])
        op = dict(op)
        op["_params"] = params
        return p, item, op


def merge_schema(s):
    """Flatten allOf; return (properties, required, additionalProperties)."""
    if not isinstance(s, dict):
        return {}, set(), None
    props, req, addl = {}, set(), s.get("additionalProperties")
    parts = [s] + list(s.get("allOf") or [])
    for part in parts:
        props.update(part.get("properties") or {})
        req.update(part.get("required") or [])
        if part.get("additionalProperties") is False:
            addl = False
    return props, req, addl


def body_schema(op):
    rb = op.get("requestBody")
    if rb:
        for ct, c in (rb.get("content") or {}).items():
            return c.get("schema") or {}
    for p in op["_params"]:
        if p.get("in") == "body":
            return p.get("schema") or {}
    return None


def success_schema(op):
    for code, r in (op.get("responses") or {}).items():
        if not str(code).startswith("2"):
            continue
        for ct, c in (r.get("content") or {}).items():
            return c.get("schema") or {}
        if "schema" in r:
            return r["schema"]
    return None


def variants(s):
    """A schema or, for oneOf/anyOf, each alternative (allOf is merged later)."""
    if isinstance(s, dict) and (s.get("oneOf") or s.get("anyOf")):
        return list(s.get("oneOf") or s.get("anyOf"))
    return [s]


def walk(schema, dotted):
    """Follow `a.b[].c` through a schema; return the leaf schema or None.

    A top-level array (the v1.1 VM create answers with a list) is entered
    without an explicit `[]`; oneOf/anyOf alternatives are all tried."""
    cur = schema
    segs = dotted.split(".")
    if isinstance(cur, dict) and cur.get("type") == "array" and not segs[0].startswith("[]"):
        cur = cur.get("items") or {}
    for seg in segs:
        is_list = seg.endswith("[]")
        name = seg[:-2] if is_list else seg
        if name:
            found = None
            for v in variants(cur):
                props, _, _ = merge_schema(v)
                if name in props:
                    found = props[name]
                    break
            if found is None:
                return None
            cur = found
        if is_list:
            if cur.get("type") != "array" and "items" not in cur:
                return None
            cur = cur.get("items") or {}
    return cur


def schema_fields(schema, prefix="", depth=0):
    """Every field path of schema, as `a.b[].c`, with its flags: readOnly,
    deprecated, and mini (a field of an object with no more than id and
    name). A top-level array is entered without `[]`, as walk() does."""
    out = {}
    if not isinstance(schema, dict) or depth > 8 or schema.get("x-recursive"):
        return out
    if depth == 0 and schema.get("type") == "array":
        schema = schema.get("items") or {}
    for v in variants(schema):
        props, _, _ = merge_schema(v)
        mini = depth > 0 and set(props) <= {"id", "name"}
        for name, sub in props.items():
            if not isinstance(sub, dict):
                continue
            path = prefix + name
            inner = sub
            while isinstance(inner, dict) and (inner.get("type") == "array" or "items" in inner) and not inner.get("properties"):
                path += "[]"
                inner = inner.get("items") or {}
            out.setdefault(path, {"readOnly": bool(sub.get("readOnly")), "deprecated": bool(sub.get("deprecated")), "mini": mini})
            for k, meta in schema_fields(inner, path + ".", depth + 1).items():
                out.setdefault(k, meta)
    return out


def request_fields(spec, req):
    """The fields of a request the baseline keeps: the answer of a GET, the
    body of any other method. None when the spec has no such operation."""
    _, _, op = spec.operation(req["method"], req["path"])
    if op is None:
        return None
    schema = success_schema(op) if req["method"] == "GET" else body_schema(op)
    return schema_fields(schema) if schema else {}


def request_key(svc, req):
    return f"{svc['name']} {req['method']} {req['path']}"


TIME_RE = re.compile(r"(_at|At)$")
DISPLAY_RE = re.compile(r"^display[_A-Z]")


def ignored_field(path, meta, body):
    last = path.rstrip("[]").split(".")[-1].rstrip("[]")
    return (meta["deprecated"] or meta["mini"] or (body and meta["readOnly"])
            or bool(TIME_RE.search(last)) or bool(DISPLAY_RE.match(last)))


def check_fields(svc, req, current, baseline):
    """field-new / field-gone / field-baseline-missing of one request."""
    out = []
    key = request_key(svc, req)
    if current is None:
        return out
    if key not in baseline:
        out.append(["warn", "field-baseline-missing", "у запроса нет эталона полей — обновите его (--update-fields)"])
        return out
    known = set(baseline[key])
    used = set(req.get("reads", [])) | set(req.get("body", []))
    body = req["method"] != "GET"
    reported = []
    for path in sorted(current):
        if path in known:
            continue
        if any(path.startswith(r + ".") or path.startswith(r + "[]") for r in reported):
            continue
        if path in used or any(u.startswith(path + ".") or u.startswith(path + "[]") for u in used):
            continue
        if ignored_field(path, current[path], body):
            continue
        reported.append(path)
        where = "тела запроса" if body else "ответа"
        out.append(["error", f"field-new:{path}",
                    f"новое поле {where} `{path}` после прошлого эталона: evo его не {'шлёт' if body else 'читает'} — прочитать или объявить accepted с причиной"])
    for path in sorted(known - set(current)):
        out.append(["info", f"field-gone:{path}", f"поля `{path}` в спецификации больше нет"])
    return out


def param_schema(p):
    return p.get("schema") or {k: p[k] for k in ("type", "enum", "maximum", "default") if k in p}


def check_request(spec, req):
    out = []  # (level, id, text)

    def add(level, fid, text):
        out.append([level, fid, text])

    method, path = req["method"], req["path"]
    p, item, op = spec.operation(method, path)
    if p is None:
        add("error", "path-missing", f"путь {path} отсутствует в спецификации")
        return out
    if op is None:
        have = [m.upper() for m in item if m in METHODS]
        add("error", "method-missing", f"метод {method} не описан для {p} (есть: {', '.join(have)})")
        return out
    if p != path:
        add("info", "path-param-name", f"в спецификации путь записан как {p}")
    if op.get("deprecated"):
        add("warn", "deprecated", "операция помечена deprecated")

    # --- query ---
    declared = {q["name"]: q for q in op["_params"] if q.get("in") == "query"}
    sent = {}
    for q in req.get("query", []):
        name, _, val = q.partition("=")
        sent[name] = val
    for name, q in declared.items():
        if q.get("required") and name not in sent:
            add("error", f"query-required:{name}", f"обязательный query-параметр `{name}` не передаётся")
    for name, val in sent.items():
        if name not in declared:
            add("warn", f"query-unknown:{name}", f"query-параметр `{name}` не описан в спецификации")
            continue
        sch = param_schema(declared[name])
        if val.lstrip("-").isdigit() and "maximum" in sch and int(val) > sch["maximum"]:
            add("error", f"query-max:{name}", f"`{name}={val}` больше максимума {sch['maximum']}")
        if sch.get("enum") and val and val not in [str(e) for e in sch["enum"]]:
            add("error", f"query-enum:{name}", f"`{name}={val}` вне enum {sch['enum']}")
    if "limit" in declared or "pageSize" in declared or "page_size" in declared or "pageToken" in declared or "page_token" in declared:
        if not req.get("paginates"):
            add("info", "pagination", "ответ постраничный; код читает только первую страницу")

    # --- body ---
    body_sent = req.get("body")
    bs = body_schema(op)
    if body_sent is not None:
        if bs is None:
            add("warn", "body-undeclared", "тело запроса не описано в спецификации")
        else:
            if bs.get("type") == "array":
                bs = bs.get("items") or {}
            props, required, addl = merge_schema(bs)
            sent_top = {f.split(".")[0].rstrip("[]") for f in body_sent}
            for f in required:
                if f not in sent_top:
                    add("error", f"body-required:{f}", f"обязательное поле тела `{f}` не передаётся")
            # One level down: required sub-fields of every object the code sends.
            for top in sorted(sent_top):
                sub = walk(bs, top)
                if not isinstance(sub, dict):
                    continue
                if sub.get("type") == "array":
                    sub = sub.get("items") or {}
                    sent_sub = {f.split(".")[1].rstrip("[]") for f in body_sent if f.startswith(top + "[].") }
                else:
                    sent_sub = {f.split(".")[1].rstrip("[]") for f in body_sent if f.startswith(top + ".")}
                if not sent_sub:
                    continue
                for v in variants(sub):
                    _, sub_req, _ = merge_schema(v)
                    for f in sorted(sub_req):
                        if f not in sent_sub:
                            add("error", f"body-required:{top}.{f}", f"обязательное поле тела `{top}.{f}` не передаётся")
                    break
            for f in body_sent:
                base = f.split(".")[0].rstrip("[]")
                if base not in props:
                    level = "error" if addl is False else "warn"
                    add(level, f"body-unknown:{base}",
                        f"поле тела `{base}` не описано" + (" (additionalProperties: false)" if addl is False else ""))
                elif "." in f or "[]" in f:
                    if walk(bs, f) is None:
                        add("warn", f"body-unknown:{f}", f"поле тела `{f}` не описано")
    elif bs is not None and (op.get("requestBody") or {}).get("required"):
        props, required, _ = merge_schema(bs)
        if required:
            add("error", "body-missing", f"спецификация требует тело с полями {sorted(required)}")

    # --- response fields read ---
    rs = success_schema(op)
    for f in req.get("reads", []):
        if rs is None:
            add("warn", "response-undeclared", "успешный ответ не описан в спецификации")
            break
        if walk(rs, f) is None:
            add("warn", f"read-unknown:{f}", f"код читает поле ответа `{f}`, которого нет в спецификации")

    # --- enums the code relies on ---
    for field, values in (req.get("enums") or {}).items():
        leaf = walk(rs or {}, field) if rs else None
        enum = (leaf or {}).get("enum")
        if not enum:
            add("info", f"enum-undeclared:{field}", f"для `{field}` спецификация не задаёт enum")
            continue
        missing = [v for v in values if v not in enum]
        if missing:
            add("warn", f"enum-unknown:{field}", f"код ждёт значения {missing} в `{field}`, в спецификации их нет (enum: {enum})")

    # --- statuses handled ---
    documented = {str(c) for c in (op.get("responses") or {})}
    for st in req.get("statuses", []):
        if str(st) not in documented:
            add("info", f"status-undocumented:{st}", f"код обрабатывает HTTP {st}, спецификация его не описывает")
    return out


# --- code coverage: every request the Go code makes is in the contract ---
#
# The contract is written by hand, so a request added to the code without an
# entry was never checked against a spec. code_sites() reads the string
# literals of internal/**/*.go (tests excluded) that look like API paths —
# "/api/…", "/v1/…", "/v2/…", "/v3/…", "/events/…" or an https URL on
# *.cloud.ru — with the `"lit" + expr + "lit"` chain that follows them, and
# check_coverage() reports each one that no request of the contract has
# (`error`, fails the run). A literal that is no request, or a template whose
# segment is chosen at run time, is listed in the contract's "code_coverage":
# "ignore" (with a reason) or "expand" (the concrete paths it stands for,
# each of which must then be in the contract).

CODE_PREFIXES = ("/api/", "/v1/", "/v2/", "/v3/", "/events/")
CLOUD_URL_RE = re.compile(r"^https://([a-z0-9-]+\.)*cloud\.ru(/|$|\?)")
HTTP_METHOD_RE = re.compile(r"\bhttp\.Method(Get|Post|Put|Patch|Delete)\b")
# Helpers of the clients that always GET (dns/dbaas getList/getObject, mk8s
# pages, compute listAll/listRaw/listProjectScoped, tools adminGET).
GET_HELPER_RE = re.compile(r"\b(getList|getObject|pages|listRaw|listProjectScoped|listAll|adminGET)\s*(\[[^\]]*\])?\s*\(")
PATH_HELPER_RE = re.compile(r'^func (\w+)\([^)]*\) string \{ return ("(?:/api/|/v[0-9]/|/events/)[^\n]*?) \}$', re.M)
FMT_VERB_RE = re.compile(r"%[-+# 0-9.]*[sdvq]")
HOLE = "\x00"  # a non-literal operand of a concatenation


def go_tokens(src):
    """A minimal Go lexer: yields (kind, value, offset) with kind str, nl,
    id or op; comments are skipped (a multi-line one counts as a newline)."""
    i, n = 0, len(src)
    while i < n:
        c = src[i]
        if c == "\n":
            yield ("nl", c, i)
            i += 1
        elif c in " \t\r":
            i += 1
        elif src.startswith("//", i):
            j = src.find("\n", i)
            i = n if j < 0 else j
        elif src.startswith("/*", i):
            j = src.find("*/", i + 2)
            j = n if j < 0 else j + 2
            if "\n" in src[i:j]:
                yield ("nl", "\n", i)
            i = j
        elif c == '"':
            j = i + 1
            while j < n and src[j] not in '"\n':
                j += 2 if src[j] == "\\" else 1
            yield ("str", src[i + 1:j], i)
            i = j + 1
        elif c == "`":
            j = src.find("`", i + 1)
            j = n if j < 0 else j
            yield ("str", src[i + 1:j], i)
            i = j + 1
        elif c == "'":
            j = i + 1
            while j < n and src[j] not in "'\n":
                j += 2 if src[j] == "\\" else 1
            yield ("op", "rune", i)
            i = j + 1
        elif c.isalnum() or c == "_":
            j = i
            while j < n and (src[j].isalnum() or src[j] == "_"):
                j += 1
            yield ("id", src[i:j], i)
            i = j
        else:
            yield ("op", c, i)
            i += 1


def skip_call(toks, k):
    """Index after the balanced (...) that starts at toks[k]."""
    depth = 0
    while k < len(toks):
        if toks[k][:2] == ("op", "("):
            depth += 1
        elif toks[k][:2] == ("op", ")"):
            depth -= 1
            if depth == 0:
                return k + 1
        k += 1
    return k


def concat_chain(toks, k, head=None):
    """The text of `"lit" + expr + "lit"…` starting at toks[k], a non-literal
    operand replaced by HOLE; returns (text, index of the token after it).
    With head, the chain starts with that text and toks[k] is the token
    after it (a call of a path helper)."""
    if head is None:
        parts = [toks[k][1]]
        k += 1
    else:
        parts = [head]
    while k < len(toks) and toks[k][:2] == ("op", "+"):
        k += 1
        while k < len(toks) and toks[k][0] == "nl":
            k += 1
        if k < len(toks) and toks[k][0] == "str":
            parts.append(toks[k][1])
            k += 1
            continue
        depth = 0
        while k < len(toks):
            kind, v, _ = toks[k]
            if kind == "op" and v in "([{":
                depth += 1
            elif kind == "op" and v in ")]}":
                if depth == 0:
                    break
                depth -= 1
            elif depth == 0 and (kind == "nl" or (kind == "op" and v in "+,;")):
                break
            k += 1
        parts.append(HOLE)
    return "".join(parts), k


def path_key(p):
    """Compare form of a path: no query, every parameter ({name}, %s, a
    concatenated value) as {}."""
    p = p.split("?", 1)[0]
    p = FMT_VERB_RE.sub("{}", p)
    p = re.sub(r"\{[^}]*\}", "{}", p)
    return p.replace(HOLE, "{}")


def code_sites(root):
    """Every API path literal of the Go code: file, line, path, host and the
    method when it is cheap to tell — http.MethodX in the statement, a GET
    helper in it, or the only http.MethodX of the rest of the function."""
    out = []
    base = os.path.join(root, "internal")
    for dirpath, dirs, files in os.walk(base):
        dirs.sort()
        for fn in sorted(files):
            if not fn.endswith(".go") or fn.endswith("_test.go"):
                continue
            full = os.path.join(dirpath, fn)
            rel = os.path.relpath(full, root).replace(os.sep, "/")
            src = open(full, encoding="utf-8").read()
            lines = src.split("\n")
            toks = list(go_tokens(src))
            # One-line path helpers, `func certPath(id string) string {
            # return "/v1/certificate/" + url.PathEscape(id) }`: their calls
            # start a path too (certPath(id)+":renew"), the literal in the
            # helper itself is no request.
            helpers = {}
            for m in PATH_HELPER_RE.finditer(src):
                htoks = list(go_tokens(m.group(2)))
                helpers[m.group(1)] = concat_chain(htoks, 0)[0]
            k = 0
            while k < len(toks):
                kind, val, off = toks[k]
                if kind == "id" and val in helpers and k + 1 < len(toks) and toks[k + 1][:2] == ("op", "(") \
                        and not (k > 0 and toks[k - 1][:2] == ("id", "func")):
                    text, end = concat_chain(toks, skip_call(toks, k + 1), head=helpers[val])
                    is_url = False
                elif kind == "str" and (val.startswith(CODE_PREFIXES) or CLOUD_URL_RE.match(val)):
                    if any(PATH_HELPER_RE.match(ln) for ln in lines[src.count("\n", 0, off):][:1]):
                        k += 1
                        continue
                    text, end = concat_chain(toks, k)
                    is_url = CLOUD_URL_RE.match(val) is not None
                else:
                    k += 1
                    continue
                host = ""
                if is_url:
                    host = re.split(r"[/?]", val[len("https://"):], maxsplit=1)[0]
                    text = text[len("https://") + len(host):]
                    if not text.split("?", 1)[0].strip("/"):
                        k = end  # a base URL, not a request
                        continue
                first = src.count("\n", 0, off) + 1
                last = src.count("\n", 0, toks[end - 1][2]) + 1
                stmt = "\n".join(lines[first - 1:last])
                method = None
                found = set(HTTP_METHOD_RE.findall(stmt))
                if len(found) == 1:
                    method = found.pop().upper()
                elif not found and GET_HELPER_RE.search(stmt):
                    method = "GET"
                elif not found and lines[first - 1].startswith("\t"):
                    rest = []
                    for ln in lines[last:]:
                        if ln.startswith("}"):
                            break
                        rest.append(ln)
                    found = set(HTTP_METHOD_RE.findall("\n".join(rest)))
                    if len(found) == 1:
                        method = found.pop().upper()
                out.append({"file": rel, "line": first, "host": host,
                            "raw": text.replace(HOLE, "{…}"), "path": path_key(text), "method": method})
                k = end
    return out


def check_coverage(contract, root):
    """Rows (service, where, code, level, id, text) of the coverage check, and
    the code sites of each contract request: {(method, path_key): [file:line]}."""
    cov = contract.get("code_coverage") or {}
    known = {}
    for svc in contract["services"]:
        for req in svc["requests"]:
            known.setdefault((req["method"], path_key(req["path"])), []).append(svc["name"])
    known_paths = {p for _, p in known}
    used = {}
    rows = []

    def rule_for(site, rules):
        for r in rules:
            if fnmatch.fnmatch(site["file"], r["file"]) and path_key(r["path"]) == site["path"] \
                    and (not r.get("method") or r["method"] == site["method"]):
                return r
        return None

    for site in code_sites(root):
        where = f"{site['file']}:{site['line']}"
        shown = (site["method"] or "?") + " " + (("https://" + site["host"]) if site["host"] else "") + site["raw"]
        if rule_for(site, cov.get("ignore") or []):
            continue
        exp = rule_for(site, cov.get("expand") or [])
        paths = [path_key(p) for p in exp["to"]] if exp else [site["path"]]
        for p in paths:
            if site["method"]:
                hit = (site["method"], p) in known
                if hit:
                    used.setdefault((site["method"], p), []).append(where)
            else:
                hit = p in known_paths
                for key in known:
                    if key[1] == p:
                        used.setdefault(key, []).append(where)
            if not hit:
                what = f"{site['method']} {p}" if site["method"] else p
                rows.append(("code", shown, where, "error", "code-uncovered",
                             f"код обращается к {what}, а в описи такого запроса нет — добавьте его в scripts/api-contract.json "
                             "(или в code_coverage.ignore/expand с причиной)"))
    return rows, used


def check_code_ref(req, used):
    """Findings on how the request is tied to the code: no call site found
    (a stale entry?), or a "code" reference that points elsewhere."""
    sites = used.get((req["method"], path_key(req["path"])), [])
    if not sites:
        return [["warn", "contract-unused", "в коде не найден запрос с таким путём и методом — запись устарела?"]]
    ref = req.get("code", "")
    if ref and ref not in sites and not any(ref.split(":")[0] == s.split(":")[0] for s in sites):
        return [["info", "code-ref", f"ссылка code={ref} не указывает на вызов; вызовы: {', '.join(sorted(set(sites)))}"]]
    if ref and ref not in sites:
        return [["info", "code-ref", f"строка в code={ref} устарела; вызовы: {', '.join(sorted(set(sites)))}"]]
    return []


FIELDS_COMMENT = ("Эталон полей спецификаций Cloud.ru для храповика field-new (scripts/api-contract-check.py): "
                  "по каждому запросу описи — пути полей ответа GET или тела запроса, только имена. "
                  "Обновляется флагом --update-fields, когда новые поля прочитаны или объявлены accepted.")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--specs", default=".cache/cloudru-specs")
    ap.add_argument("--contract", default=os.path.join(os.path.dirname(__file__), "api-contract.json"))
    ap.add_argument("--root", default=os.path.join(os.path.dirname(os.path.abspath(__file__)), os.pardir),
                    help="repository root whose internal/**/*.go is checked for requests missing from the contract")
    ap.add_argument("--code-only", action="store_true",
                    help="only check that every request of the code is in the contract (no specs needed)")
    ap.add_argument("--markdown", action="store_true", help="print a markdown table instead of plain lines")
    ap.add_argument("--fields", default=os.path.join(os.path.dirname(__file__), "api-contract-fields.json"),
                    help="the baseline of the fields of the specs (field-new)")
    ap.add_argument("--update-fields", action="store_true", help="rewrite the baseline of the fields from the specs")
    args = ap.parse_args()
    try:
        baseline_doc = json.load(open(args.fields, encoding="utf-8"))
    except FileNotFoundError:
        baseline_doc = {}
    baseline = baseline_doc.get("requests") or {}
    fresh = {}

    contract = json.load(open(args.contract, encoding="utf-8"))
    rows, used = check_coverage(contract, args.root)
    failed = sum(1 for r in rows if r[3] == "error")

    def emit(svc, req, findings):
        nonlocal failed
        accepted = req.get("accepted") or {}
        where = f"{req['method']} {req['path']}"
        if not findings:
            rows.append((svc["name"], where, req.get("code", ""), "ok", "", ""))
        for level, fid, text in findings:
            if fid in accepted or any(fnmatch.fnmatch(fid, a) for a in accepted):
                why = accepted.get(fid) or next(v for a, v in accepted.items() if fnmatch.fnmatch(fid, a))
                rows.append((svc["name"], where, req.get("code", ""), "accepted", fid, f"{text} — принято: {why}"))
                continue
            if level == "error":
                failed += 1
            rows.append((svc["name"], where, req.get("code", ""), level, fid, text))

    for svc in contract["services"]:
        if args.code_only:
            for req in svc["requests"]:
                emit(svc, req, check_code_ref(req, used))
            continue
        files = sorted(glob.glob(os.path.join(args.specs, svc["spec"])))
        if not files:
            rows.append((svc["name"], "—", "—", "error", "spec-missing", f"нет файла спецификации {svc['spec']}"))
            failed += 1
            continue
        spec = Spec(files[0])
        for req in svc["requests"]:
            current = request_fields(spec, req)
            # No operation in the spec (path-missing): an empty baseline, so
            # the operation's fields are new once the spec has it.
            fresh[request_key(svc, req)] = sorted(current or {})
            fields = [] if args.update_fields else check_fields(svc, req, current, baseline)
            emit(svc, req, check_code_ref(req, used) + check_request(spec, req) + fields)

    if args.update_fields and not args.code_only:
        doc = {"_comment": baseline_doc.get("_comment") or FIELDS_COMMENT, "requests": dict(sorted(fresh.items()))}
        with open(args.fields, "w", encoding="utf-8") as f:
            json.dump(doc, f, ensure_ascii=False, indent=1)
            f.write("\n")
        print(f"эталон полей: {len(fresh)} запросов → {args.fields}", file=sys.stderr)

    if args.markdown:
        print("| сервис | запрос | код | уровень | проверка | детали |")
        print("|---|---|---|---|---|---|")
        for r in rows:
            print("| " + " | ".join(str(c).replace("|", "\\|") for c in r) + " |")
    else:
        for r in rows:
            print(f"[{r[3]:8}] {r[0]:12} {r[1]:60} {r[4]:28} {r[5]}  ({r[2]})")
    counts = {}
    for r in rows:
        counts[r[3]] = counts.get(r[3], 0) + 1
    print(f"\n{len(rows)} строк: " + ", ".join(f"{k}={v}" for k, v in sorted(counts.items())), file=sys.stderr)
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
