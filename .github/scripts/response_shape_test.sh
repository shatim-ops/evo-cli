#!/usr/bin/env bash
# Проверка response_shape.sh (job ci) на локальном сервере: JSON — ok,
# страница шлюза или нет ответа — warn; тело — через redact.sed, URL в
# строке нет (E5).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source-path=SCRIPTDIR source=response_shape.sh
. "$here/response_shape.sh"
fail=0
work=$(mktemp -d)
port_file="$work/port"
python3 -I - "$port_file" <<'PY' &
import http.server, sys
class H(http.server.BaseHTTPRequestHandler):
    server_version, sys_version = "envoy", ""
    def do_GET(self):
        if self.path.startswith("/json/"):
            body, ct = b'{"error":"bad","id":"11110000-0000-4000-8000-000000000001"}', "application/json; charset=utf-8"
        else:
            body, ct = b"<html><body>gateway</body></html>", "text/html"
        self.send_response(400 if self.path.startswith("/json/") else 404)
        self.send_header("Content-Type", ct)
        self.end_headers()
        self.wfile.write(body)
    def log_message(self, *a):
        pass
s = http.server.HTTPServer(("127.0.0.1", 0), H)
open(sys.argv[1], "w").write(str(s.server_port))
s.serve_forever()
PY
srv=$!
trap 'kill $srv 2>/dev/null || true; rm -rf "$work"' EXIT
for _ in $(seq 1 50); do [ -s "$port_file" ] && break; sleep 0.1; done
port=$(cat "$port_file")
check() { # ИМЯ URL ждём-итог ждём-подстроку не-ждём
  local got
  got=$(response_shape "$2" token)
  if [[ "$got" != "$3"$'\t'* ]] || [[ "$got" != *"$4"* ]] || { [ -n "$5" ] && [[ "$got" == *"$5"* ]]; }; then
    printf 'FAIL %s\n  got: %s\n' "$1" "$got"
    fail=1
  fi
}
check "JSON API" "http://127.0.0.1:$port/json/project-secret" ok 'HTTP 400, application/json, server envoy' 'project-secret'
check "UUID в теле маскируется" "http://127.0.0.1:$port/json/x" ok '«' '11110000-0000-4000-8000-000000000001'
check "страница шлюза" "http://127.0.0.1:$port/html/project-secret" warn 'text/html' 'project-secret'
check "нет ответа" "http://127.0.0.1:1/x" warn 'HTTP 000' ''
[ "$fail" = 0 ] && echo "response_shape: ok"
exit "$fail"
