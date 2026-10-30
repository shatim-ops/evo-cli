#!/usr/bin/env bash
# Проверка dbaas-passing-delete.sh (job ci) на поддельном evo: кластер
# создаётся и удаление отказано USAGE — 0; удаление принято или отказ не
# USAGE — 1; после отказа кластер удаляется — 1; кластер уже READY — не
# покрыто, 0; неверный вид — 2. Облака нет.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fail=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# shellcheck source-path=SCRIPTDIR source=fake_evo.sh
. "$here/fake_evo.sh"
fake_evo_install "$work/bin"

usage='{"error":{"code":1,"reason":"USAGE","message":"the cluster is still being created (DEPLOYING): wait"}}'
apierr='{"error":{"code":5,"reason":"API_ERROR","message":"400 FailedPrecondition"}}'

# case ИМЯ КОД СТРОКА get.0 get.1 [delete.json delete.rc] — ответы pg_get
# по очереди; delete — ответ pg_delete с --yes.
case_() {
	local name=$1 want=$2 line=$3 g0=$4 g1=$5 dj=${6:-} drc=${7:-0} rc
	export FAKE_DIR="$work/$name"
	mkdir -p "$FAKE_DIR"
	fake_evo_answer pg_get 0 0 "$g0"
	fake_evo_answer pg_get 1 0 "$g1"
	[ -z "$dj" ] || fake_evo_answer pg_delete.yes 0 "$drc" "$dj"
	set +e
	PATH="$work/bin:$PATH" "$here/dbaas-passing-delete.sh" pg cluster-1 >"$FAKE_DIR/out" 2>&1
	rc=$?
	set -e
	if [ "$rc" != "$want" ] || ! grep -qF -- "$line" "$FAKE_DIR/out"; then
		printf 'FAIL %s: код %s (ждём %s), строка «%s»\n' "$name" "$rc" "$want" "$line"
		sed 's/^/  | /' "$FAKE_DIR/out"
		fail=1
	fi
}

case_ refused 0 "1 из 1" '{"status":"DEPLOYING"}' '{"status":"DEPLOYING"}' "$usage" 1
case_ accepted 1 "ждали USAGE" '{"status":"CREATING"}' '{"status":"DELETING"}' '{"status":"deleting"}' 0
case_ other 1 "API_ERROR" '{"status":"DEPLOYING"}' '{"status":"DEPLOYING"}' "$apierr" 5
case_ gone_after 1 "удаление ушло" '{"status":"PROVISIONING"}' '{"status":"DELETING"}' "$usage" 1
case_ ready 0 "не покрыто" '{"status":"READY"}' '{"status":"READY"}'

# Ни одного удаления, когда кластер уже не создаётся.
if grep -q 'pg_delete' "$work/ready/evo-calls" 2>/dev/null; then
	echo "FAIL ready: вызван pg_delete"
	fail=1
fi
set +e
"$here/dbaas-passing-delete.sh" mysql x >/dev/null 2>&1
rc=$?
set -e
[ "$rc" = 2 ] || { echo "FAIL kind: код $rc, ждём 2"; fail=1; }

[ "$fail" = 0 ] && echo "dbaas-passing-delete: ok"
exit "$fail"
