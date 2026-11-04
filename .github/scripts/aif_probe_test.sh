#!/usr/bin/env bash
# Проверка aif_probe.py discover (ai-factory-probe) на локальных серверах
# MCP вместо AI Factory: один требует свой токен и отвечает JSON, другой
# открыт без токена и отвечает SSE с двумя страницами tools/list. Публичная
# строка — только классы и счётчики: без адреса, токена и имён
# инструментов; вызываются только list_* без обязательных аргументов и без
# пометки «удаляет»; сырой отчёт — в каталоге, без токена.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fail=0
work=$(mktemp -d)
trap 'kill "$pid" 2>/dev/null || true; rm -rf "$work"' EXIT
python3 -I - "$work/port" "$work/calls" <<'PY' &
import http.server, json, sys
port_file, calls = sys.argv[1], sys.argv[2]
TOOLS = [
    {"name": "list_things", "inputSchema": {"type": "object"}, "annotations": {"readOnlyHint": True}},
    {"name": "list_by_parent", "inputSchema": {"type": "object", "required": ["parent"]}},
    {"name": "list_and_purge", "inputSchema": {"type": "object"}, "annotations": {"destructiveHint": True}},
    {"name": "delete_thing", "inputSchema": {"type": "object", "required": ["id"]}, "annotations": {"destructiveHint": True}},
]
class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_POST(self):
        closed = self.path.startswith("/closed")
        if closed and self.headers.get("Authorization") != "Bearer tok-own-1234567":
            self.send_response(401); self.end_headers(); return
        m = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        if "id" not in m:
            self.send_response(202); self.end_headers(); return
        meth, p = m["method"], m.get("params") or {}
        if meth == "initialize":
            res = {"protocolVersion": "2025-06-18", "serverInfo": {"name": "fake"}, "capabilities": {"tools": {}}}
            if closed: res["instructions"] = "be careful"
        elif meth == "tools/list":
            if closed:
                res = {"tools": TOOLS[:2], "nextCursor": "p2"} if not p.get("cursor") else {"tools": TOOLS[2:]}
            else:
                res = {"tools": TOOLS[:1]}
        elif meth == "tools/call":
            open(calls, "a").write(p["name"] + "\n")
            res = {"content": [{"type": "text", "text": "[]"}], "isError": False}
        body = json.dumps({"jsonrpc": "2.0", "id": m["id"], "result": res}).encode()
        self.send_response(200)
        if closed:
            self.send_header("Content-Type", "application/json"); self.send_header("Mcp-Session-Id", "s1")
        else:
            self.send_header("Content-Type", "text/event-stream"); body = b"event: message\ndata: " + body + b"\n\n"
        self.end_headers(); self.wfile.write(body)
s = http.server.HTTPServer(("127.0.0.1", 0), H)
open(port_file, "w").write(str(s.server_port))
s.serve_forever()
PY
pid=$!
for _ in $(seq 1 50); do [ -s "$work/port" ] && break; sleep 0.1; done
port=$(cat "$work/port")
set +e
env -i PATH="$PATH" HOME="$HOME" \
	AIF_K8S_URL="http://127.0.0.1:$port/closed/mcp" AIF_K8S_TOKEN=tok-own-1234567 \
	AIF_S3_URL="http://127.0.0.1:$port/open/mcp" \
	python3 -I "$here/aif_probe.py" discover "$work/out" >"$work/log" 2>&1
rc=$?
set -e
[ "$rc" = 0 ] || { echo "FAIL: код $rc"; fail=1; }
check() { grep -qF -- "$2" "$work/log" || { printf 'FAIL %s: нет «%s»\n' "$1" "$2"; fail=1; }; }
check closed "k8s: без токена 401 чужой токен 401 свой токен 2xx инструментов 4 (tools/list ok; с аннотациями 3, только чтение 1, удаляющих 2;"
check closed-instr "instructions да) чтений 1: ok 1"
check open "s3: без токена 2xx, MCP открыт чужой токен 2xx, MCP открыт свой токен не задан"
check skipped "repo: адрес не задан — пропущен"
check warning "::warning::MCP без своего токена отвечает: 1 из 2 серверов"
[ "$(cat "$work/calls")" = list_things ] || { printf 'FAIL: вызваны %s, ожидался только list_things\n' "$(tr '\n' ' ' <"$work/calls")"; fail=1; }
! grep -Eq "127\.0\.0\.1|tok-own|list_things|delete_thing" "$work/log" || { echo "FAIL: адрес, токен или имя инструмента в публичном логе"; fail=1; }
! grep -rq "tok-own" "$work/out" || { echo "FAIL: токен в отчёте"; fail=1; }
[ "$(jq length "$work/out/k8s/tools.json")" = 4 ] || { echo "FAIL: в отчёте не 4 инструмента k8s"; fail=1; }
[ "$(jq -r '.[0].class' "$work/out/k8s/reads.json")" = ok ] || { echo "FAIL: чтение не записано"; fail=1; }
if [ "$fail" = 0 ]; then echo "aif_probe: ok"; else sed "s/^/  /" "$work/log"; fi
exit "$fail"
