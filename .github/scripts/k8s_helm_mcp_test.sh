#!/usr/bin/env bash
# Проверка k8s_helm_mcp.sh (cloud-e2e, job k8s-smoke) без облака: evo —
# общая заглушка fake_evo.sh со сценарием mcp (сервер MCP по stdio на
# Python: ответ на tools/call по инструменту, action и режиму случая).
# Случаи режима configmap: install → deploying → status deployed →
# ConfigMap → uninstall — ok; install с ошибкой, сеанс дольше предела, релиз
# не deployed, ConfigMap после uninstall — ошибка. Режима pods: поды Ready со
# второго чтения, после uninstall их нет — ok; поды не готовы, поды после
# uninstall остались — ошибка.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fail=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# shellcheck source-path=SCRIPTDIR source=fake_evo.sh
. "$here/fake_evo.sh"
fake_evo_install "$work/bin"
mcp_py=$(cat <<'PY'
#!/usr/bin/env python3
import json, os, sys, time
f = os.environ["FAKE_DIR"]
mode = open(f + "/mode").read().strip()
gone = os.path.exists(f + "/uninstalled")
for line in sys.stdin:
    m = json.loads(line)
    if m.get("method") == "initialize":
        print(json.dumps({"jsonrpc": "2.0", "id": m["id"], "result": {"protocolVersion": "2025-06-18"}}), flush=True)
    if m.get("method") != "tools/call":
        continue
    name, a = m["params"]["name"], m["params"]["arguments"]
    open(f + "/calls", "a").write(name + " " + a.get("action", a.get("args", "")) + "\n")
    err, r = False, {}
    if name == "k8s_helm" and a["action"] == "install":
        open(f + "/release", "w").write(a["release"])
        if mode == "slow":
            time.sleep(2)
        if mode == "hang":
            time.sleep(5)
        if mode == "install-error":
            err, r = True, {"error": "helm: boom", "reason": "API_ERROR"}
        else:
            r = {"status": "deploying", "action": "install", "next_tool": "k8s_helm",
                 "next_args": {"cluster_id": a["cluster_id"], "action": "status", "release": a["release"], "namespace": "default"}}
    elif name == "k8s_helm" and a["action"] == "status":
        r = {"status": "ok", "output": "STATUS: pending-install" if mode == "never" else "NAME: evo-e2e-mcp\nSTATUS: deployed\n"}
    elif name == "k8s_helm" and a["action"] == "uninstall":
        open(f + "/uninstalled", "w").write("1")
        r = {"status": "ok"}
    elif name == "k8s_kubectl" and "get pods" in a["args"]:
        n = int(open(f + "/pods").read()) + 1 if os.path.exists(f + "/pods") else 1
        open(f + "/pods", "w").write(str(n))
        ready = mode != "pods-never" and (n > 1 or gone)
        pod = {"metadata": {"name": "evo-e2e-pod-1"}, "status": {"conditions": [{"type": "Ready", "status": "True" if ready else "False"}]}}
        items = [] if gone and mode != "pods-stay" else [pod, pod]
        r = {"status": "ok", "output": json.dumps({"items": items})}
    elif name == "k8s_kubectl":
        if gone and mode != "cm-stays":
            err, r = True, {"error": "NotFound", "output": "Error from server (NotFound)"}
        else:
            r = {"status": "ok", "output": "Warning: x\n" + json.dumps({"data": {"k": "helm"}})}
    print(json.dumps({"jsonrpc": "2.0", "id": m["id"], "result": {"isError": err, "content": [{"type": "text", "text": json.dumps(r)}]}}), flush=True)
PY
)
# run ИМЯ РЕЖИМ ОЖИДАЕМЫЙ_КОД [MCP_LIMIT] [РЕЖИМ_СКРИПТА]
run() {
	local d="$work/$1" rc
	mkdir -p "$d"
	printf '%s\n' "$2" >"$d/mode"
	printf '%s\n' "$mcp_py" | FAKE_DIR="$d" fake_evo_run mcp
	set +e
	PATH="$work/bin:$PATH" FAKE_DIR="$d" MCP_LIMIT="${4:-60}" STATUS_SLEEP=0 POD_TRIES=3 \
		"$here/k8s_helm_mcp.sh" 00000000-0000-4000-8000-0000000000c1 /tmp/chart "${5:-configmap}" >"$d/out" 2>&1
	rc=$?
	set -e
	if [ "$rc" != "$3" ]; then
		printf 'FAIL %s: код %s, ожидался %s\n' "$1" "$rc" "$3"
		sed 's/^/  /' "$d/out"
		fail=1
	fi
}
run ok ok 0
run install-error install-error 1
run slow slow 1 1
# Сеанс дольше предела + 30 с: timeout обрывает evo mcp (124); MCP_LIMIT=-28
# даёт timeout 2 с.
run hang hang 1 -28
grep -q 'install: isError тайм-аут сеанса' "$work/hang/out" || { echo "FAIL hang: нет «тайм-аут сеанса»"; sed 's/^/  /' "$work/hang/out"; fail=1; }
run never-deployed never 1
run cm-stays cm-stays 1
# Порядок вызовов успешного случая — как у агента по следующему шагу.
want='k8s_helm install
k8s_helm status
k8s_kubectl get configmap evo-e2e-helm -n default -o json
k8s_helm uninstall
k8s_kubectl get configmap evo-e2e-helm -n default -o json'
[ "$(cat "$work/ok/calls")" = "$want" ] || { printf 'FAIL ok: вызовы\n%s\n' "$(cat "$work/ok/calls")"; fail=1; }
! grep -v '^::add-mask::' "$work/ok/out" | grep -q 00000000-0000-4000-8000-0000000000c1 || { echo "FAIL ok: id кластера в логе"; fail=1; }
run pods-ok pods 0 60 pods
run pods-never pods-never 1 60 pods
grep -q 'поды релиза не готовы за 3 вызовов' "$work/pods-never/out" || { echo "FAIL pods-never: нет причины"; sed 's/^/  /' "$work/pods-never/out"; fail=1; }
run pods-stay pods-stay 1 60 pods
grep -q 'uninstall: поды релиза остались' "$work/pods-stay/out" || { echo "FAIL pods-stay: нет причины"; sed 's/^/  /' "$work/pods-stay/out"; fail=1; }
run bad-mode ok 2 60 nodes
# Режим pods: свой релиз, поды читаются до готовности и после uninstall.
want='k8s_helm install
k8s_helm status
k8s_kubectl get pods -l app=evo-e2e-pod -n default -o json
k8s_kubectl get pods -l app=evo-e2e-pod -n default -o json
k8s_helm uninstall
k8s_kubectl get pods -l app=evo-e2e-pod -n default -o json'
[ "$(cat "$work/pods-ok/calls")" = "$want" ] || { printf 'FAIL pods-ok: вызовы\n%s\n' "$(cat "$work/pods-ok/calls")"; fail=1; }
if [ "$(cat "$work/pods-ok/release")" != evo-e2e-pod ] || [ "$(cat "$work/ok/release")" != evo-e2e-mcp ]; then
	echo "FAIL: релизы режимов"
	fail=1
fi
grep -q 'поды Ready → uninstall' "$work/pods-ok/out" || { echo "FAIL pods-ok: нет итога"; sed 's/^/  /' "$work/pods-ok/out"; fail=1; }
[ "$fail" = 0 ] && echo "k8s_helm_mcp: ok"
exit "$fail"
