#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
# SPDX-FileContributor: Тимур Шакирбеков
# SPDX-License-Identifier: Apache-2.0
"""ai-factory-probe, phase discover: MCP servers of AI Factory as a client sees them.

    aif_probe.py discover OUT_DIR

Servers come from the environment: AIF_<S>_URL (the MCP endpoint) and
AIF_<S>_TOKEN for S in REPO, S3, AR, IAM, K8S; AIF_AUTH_SCHEME is "bearer"
(Authorization: Bearer, the default) or the name of the header that carries
the token. A server without a URL is skipped.

Per server: initialize without a token and with a random foreign one (is
the address open, Q1), then with its own: tools/list with every page (the
schemas, Q3) and each tool named list_* with no required argument and not
marked destructive, called once with no arguments (what it reads, Q2, Q10).
Nothing that writes is called in this phase.

The public log gets counts and classes only: no address, token, tool name
or answer. OUT_DIR gets the raw tools/list and answers for evo-internal
(aif_secret_scan.sh checks it against every secret before the push).
"""

import json
import os
import secrets
import sys
import time
import urllib.error
import urllib.request

SERVERS = ["REPO", "S3", "AR", "IAM", "K8S"]
PROTOCOL = "2025-06-18"
TIMEOUT = float(os.environ.get("AIF_TIMEOUT", "30"))
MAX_BODY = 4 << 20


def status_class(code):
    """The class of an HTTP status for the public log."""
    if code is None:
        return "нет ответа"
    if 200 <= code < 300:
        return "2xx"
    if code in (401, 403, 404, 405, 406, 415, 429):
        return str(code)
    return "%dxx" % (code // 100)


class Session:
    """One MCP session over streamable HTTP."""

    def __init__(self, url, header=None):
        self.url, self.header, self.sid, self.next_id = url, header, None, 0

    def post(self, msg):
        """POST one JSON-RPC message: (status, the response message or None)."""
        body = json.dumps(msg).encode()
        req = urllib.request.Request(self.url, data=body, method="POST")
        req.add_header("Content-Type", "application/json")
        req.add_header("Accept", "application/json, text/event-stream")
        req.add_header("MCP-Protocol-Version", PROTOCOL)
        if self.header:
            req.add_header(*self.header)
        if self.sid:
            req.add_header("Mcp-Session-Id", self.sid)
        try:
            with urllib.request.urlopen(req, timeout=TIMEOUT) as r:
                code, ctype = r.status, r.headers.get("Content-Type", "")
                self.sid = r.headers.get("Mcp-Session-Id") or self.sid
                raw = r.read(MAX_BODY)
        except urllib.error.HTTPError as e:
            return e.code, None
        except (urllib.error.URLError, OSError, ValueError):
            return None, None
        if "id" not in msg:
            return code, None
        return code, parse(raw, ctype, msg["id"])

    def call(self, method, params=None):
        self.next_id += 1
        m = {"jsonrpc": "2.0", "id": self.next_id, "method": method}
        if params is not None:
            m["params"] = params
        return self.post(m)

    def initialize(self):
        code, res = self.call("initialize", {
            "protocolVersion": PROTOCOL, "capabilities": {},
            "clientInfo": {"name": "evo-ai-factory-probe", "version": "1"}})
        if res is not None and "result" in res:
            self.post({"jsonrpc": "2.0", "method": "notifications/initialized"})
        return code, res


def parse(raw, ctype, want):
    """The JSON-RPC answer with id want, from a JSON body or an SSE stream."""
    text = raw.decode("utf-8", "replace")
    if "text/event-stream" in ctype:
        for block in text.split("\n\n"):
            data = "\n".join(l[5:].lstrip() for l in block.splitlines() if l.startswith("data:"))
            try:
                m = json.loads(data)
            except ValueError:
                continue
            if isinstance(m, dict) and m.get("id") == want:
                return m
        return None
    try:
        m = json.loads(text)
    except ValueError:
        return None
    return m if isinstance(m, dict) else None


def auth_header(token):
    scheme = os.environ.get("AIF_AUTH_SCHEME", "bearer").strip() or "bearer"
    if scheme.lower() == "bearer":
        return ("Authorization", "Bearer " + token)
    return (scheme, token)


def open_class(url, header):
    """What initialize answers with this header: the status class, and
    whether it gave a session (the address serves MCP)."""
    code, res = Session(url, header).initialize()
    return {"http": status_class(code), "mcp": bool(res and "result" in res)}


def list_tools(s):
    tools, cursor = [], None
    for _ in range(50):
        params = {"cursor": cursor} if cursor else {}
        code, res = s.call("tools/list", params)
        if not res or "result" not in res:
            return tools, status_class(code)
        tools += res["result"].get("tools", [])
        cursor = res["result"].get("nextCursor")
        if not cursor:
            return tools, "ok"
    return tools, "страниц больше 50"


def readable(t):
    """A tool this phase may call: list_*, no required argument, not marked
    destructive or as a write."""
    ann = t.get("annotations") or {}
    req = (t.get("inputSchema") or {}).get("required") or []
    return (t.get("name", "").startswith("list_") and not req
            and not ann.get("destructiveHint") and ann.get("readOnlyHint", True) is not False)


def probe(name, url, token, out):
    row = {"server": name.lower()}
    row["no_token"] = open_class(url, None)
    row["foreign_token"] = open_class(url, auth_header(secrets.token_hex(20)))
    if not token:
        row["own_token"] = "не задан"
        return row
    s = Session(url, auth_header(token))
    t0 = time.monotonic()
    code, res = s.initialize()
    row["own_token"] = {"http": status_class(code), "mcp": bool(res and "result" in res)}
    if not row["own_token"]["mcp"]:
        return row
    srv = res["result"]
    tools, how = list_tools(s)
    d = os.path.join(out, name.lower())
    os.makedirs(d, exist_ok=True)
    dump(os.path.join(d, "initialize.json"), srv)
    dump(os.path.join(d, "tools.json"), tools)
    ann = [t.get("annotations") or {} for t in tools]
    row["tools"] = {
        "list": how, "count": len(tools),
        "with_annotations": sum(1 for a in ann if a),
        "read_only": sum(1 for a in ann if a.get("readOnlyHint") is True),
        "destructive": sum(1 for a in ann if a.get("destructiveHint") is True),
        "schema_bytes": len(json.dumps(tools, ensure_ascii=False).encode()),
        "instructions": bool(srv.get("instructions")),
    }
    calls = []
    for t in tools:
        if not readable(t):
            continue
        c0 = time.monotonic()
        code, r = s.call("tools/call", {"name": t["name"], "arguments": {}})
        took = round(time.monotonic() - c0, 2)
        if r is None:
            cls = status_class(code)
        elif "error" in r:
            cls = "jsonrpc_error"
        elif (r.get("result") or {}).get("isError"):
            cls = "isError"
        else:
            cls = "ok"
        calls.append({"tool": t["name"], "class": cls, "seconds": took, "answer": r})
    dump(os.path.join(d, "reads.json"), calls)
    row["reads"] = {"called": len(calls)}
    for c in calls:
        row["reads"][c["class"]] = row["reads"].get(c["class"], 0) + 1
    row["seconds"] = round(time.monotonic() - t0, 1)
    return row


def dump(path, v):
    with open(path, "w", encoding="utf-8") as f:
        json.dump(v, f, ensure_ascii=False, indent=1)


def public(row):
    """One line of the public log: counts and classes only."""
    parts = ["%s:" % row["server"],
             "без токена %s%s" % (row["no_token"]["http"], ", MCP открыт" if row["no_token"]["mcp"] else ""),
             "чужой токен %s%s" % (row["foreign_token"]["http"], ", MCP открыт" if row["foreign_token"]["mcp"] else "")]
    own = row["own_token"]
    if isinstance(own, str):
        parts.append("свой токен " + own)
        return " ".join(parts)
    parts.append("свой токен %s" % own["http"])
    if "tools" in row:
        t = row["tools"]
        parts.append("инструментов %d (tools/list %s; с аннотациями %d, только чтение %d, удаляющих %d; схемы %d Б; instructions %s)"
                     % (t["count"], t["list"], t["with_annotations"], t["read_only"], t["destructive"],
                        t["schema_bytes"], "да" if t["instructions"] else "нет"))
    if "reads" in row:
        r = dict(row["reads"])
        n = r.pop("called")
        parts.append("чтений %d: %s" % (n, ", ".join("%s %d" % kv for kv in sorted(r.items())) or "—"))
    return " ".join(parts)


def main(argv):
    if len(argv) != 3 or argv[1] != "discover":
        print("usage: aif_probe.py discover OUT_DIR", file=sys.stderr)
        return 2
    out = argv[2]
    os.makedirs(out, exist_ok=True)
    rows = []
    for name in SERVERS:
        url = os.environ.get("AIF_%s_URL" % name, "").strip()
        if not url:
            print("%s: адрес не задан — пропущен" % name.lower())
            continue
        row = probe(name, url, os.environ.get("AIF_%s_TOKEN" % name, "").strip(), out)
        rows.append(row)
        print(public(row), flush=True)
    dump(os.path.join(out, "summary.json"), rows)
    # Q1: an address that serves MCP without its own token is the finding
    # of the phase — a warning on the run, so it is not lost in a green
    # one; the step stays green, its job is to record.
    open_ = [r["server"] for r in rows if r["no_token"]["mcp"] or r["foreign_token"]["mcp"]]
    if open_:
        print("::warning::MCP без своего токена отвечает: %d из %d серверов" % (len(open_), len(rows)))
    return 0 if rows else 1


if __name__ == "__main__":
    sys.exit(main(sys.argv))
