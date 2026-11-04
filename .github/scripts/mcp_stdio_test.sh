#!/usr/bin/env bash
# Проверка mcp_stdio.sh (клиент evo mcp по stdio для cloud-e2e) без облака:
# evo — общая заглушка fake_evo.sh со сценарием mcp (сервер MCP на Python).
# mcp_follow: deploying дважды, затем ok — три вызова, done, аргументы
# повтора — из next_args; next_tool другого инструмента — tool; ответ в
# работе без конца — max; ошибка — done с isError true; сеанс дольше
# MCP_SESSION_LIMIT — «тайм-аут сеанса».
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fail=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# shellcheck source-path=SCRIPTDIR source=fake_evo.sh
. "$here/fake_evo.sh"
# shellcheck source-path=SCRIPTDIR source=mcp_stdio.sh
. "$here/mcp_stdio.sh"
fake_evo_install "$work/bin"
mcp_py=$(cat <<'PY'
#!/usr/bin/env python3
import json, os, sys, time
f = os.environ["FAKE_DIR"]
mode = open(f + "/mode").read().strip()
for line in sys.stdin:
    m = json.loads(line)
    if m.get("method") == "initialize":
        print(json.dumps({"jsonrpc": "2.0", "id": m["id"], "result": {"protocolVersion": "2025-06-18"}}), flush=True)
    if m.get("method") != "tools/call":
        continue
    name, a = m["params"]["name"], m["params"]["arguments"]
    n = int(open(f + "/n").read()) + 1 if os.path.exists(f + "/n") else 1
    open(f + "/n", "w").write(str(n))
    open(f + "/calls", "a").write(name + " " + json.dumps(a, sort_keys=True) + "\n")
    err, r = False, {"status": "ok"}
    if mode == "hang":
        time.sleep(5)
    if mode == "error":
        err, r = True, {"error": "boom", "reason": "API_ERROR"}
    elif mode == "endless" or (mode == "done" and n < 3):
        r = {"status": "deploying", "next_tool": name, "next_args": {"confirmed": True, "step": n}}
    elif mode == "other":
        r = {"status": "creating", "next_tool": "vm_get", "next_args": {"vm_id": "x"}}
    print(json.dumps({"jsonrpc": "2.0", "id": m["id"], "result": {"isError": err, "content": [{"type": "text", "text": json.dumps(r)}]}}), flush=True)
PY
)
# case ИМЯ РЕЖИМ ВЫЗОВОВ_МАКС ОЖИДАЕМОЕ — ОЖИДАЕМОЕ: «FOLLOW CALLS ISERR STATUS»
case_() {
	local d="$work/$1" got
	mkdir -p "$d/w"
	printf '%s\n' "$2" >"$d/mode"
	printf '%s\n' "$mcp_py" | FAKE_DIR="$d" fake_evo_run mcp
	got=$(
		export PATH="$work/bin:$PATH" FAKE_DIR="$d" MCP_WORK="$d/w" MCP_FOLLOW_SLEEP=0 MCP_SESSION_LIMIT=2
		mcp_follow deploy '{"confirmed":true}' "$3"
		st=$(jq -r '.status // empty' "$MCP_WORK/r.json")
		echo "$MCP_FOLLOW $MCP_CALLS $MCP_ISERR ${st:--}"
	)
	[ "$got" = "$4" ] || { printf 'FAIL %s: «%s», ожидалось «%s»\n' "$1" "$got" "$4"; fail=1; }
}
case_ "done" "done" 10 "done 3 false ok"
case_ other other 10 "tool 1 false creating"
case_ endless endless 4 "max 4 false deploying"
case_ error error 10 "done 1 true -"
case_ hang hang 10 "done 1 тайм-аут сеанса -"
# Повтор — с next_args прошлого ответа, а не с первыми аргументами.
want='deploy {"confirmed": true}
deploy {"confirmed": true, "step": 1}
deploy {"confirmed": true, "step": 2}'
[ "$(cat "$work/done/calls")" = "$want" ] || { printf 'FAIL done: вызовы\n%s\n' "$(cat "$work/done/calls")"; fail=1; }
[ "$fail" = 0 ] && echo "mcp_stdio: ok"
exit "$fail"
