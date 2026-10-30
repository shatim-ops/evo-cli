#!/usr/bin/env bash
# Проверка pg-schedule-probe.sh (job ci) на поддельном evo и поддельном
# dbaas-delete.sh: отказ платформы — строка «P45: … отказ» с reason, без
# удаления; принятое расписание — удаление по имени evo-e2e-pgsch<N>-…;
# удаление не прошло — код 1; CLUSTER не evo-e2e-pg-* — код 2. Облака нет.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fail=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# shellcheck source-path=SCRIPTDIR source=fake_evo.sh
. "$here/fake_evo.sh"
fake_evo_install "$work/bin"
# Скрипт зовёт dbaas-delete.sh из своего каталога: копия рядом с поддельным.
mkdir -p "$work/scripts"
cp "$here/pg-schedule-probe.sh" "$here/redact.sed" "$work/scripts/"

refused='{"error":{"code":5,"reason":"API_ERROR","message":"400 InvalidArgument: invalid backup schedule"}}'

# case ИМЯ КОД КОД_УДАЛЕНИЯ ОТВЕТ1 КОД1 ОТВЕТ2 КОД2 — ответы pg_create
# с --yes по очереди; поддельный dbaas-delete.sh пишет имя в deleted и
# выходит с КОД_УДАЛЕНИЯ.
case_() {
	local name=$1 want=$2 delrc=$3 rc
	export FAKE_DIR="$work/$name"
	mkdir -p "$FAKE_DIR"
	fake_evo_answer pg_create.yes 0 "$5" "$4"
	fake_evo_answer pg_create.yes 1 "$7" "$6"
	# shellcheck disable=SC2016 # $2 — аргумент поддельного скрипта, не этого
	printf '#!/usr/bin/env bash\necho "$2" >>"%s/deleted"\nexit %s\n' "$FAKE_DIR" "$delrc" >"$work/scripts/dbaas-delete.sh"
	chmod +x "$work/scripts/dbaas-delete.sh"
	set +e
	PATH="$work/bin:$PATH" CLUSTER=evo-e2e-pg-r7a1 SPEC_ID=s SUBNET_ID=n "$work/scripts/pg-schedule-probe.sh" >"$FAKE_DIR/out" 2>&1
	rc=$?
	set -e
	if [ "$rc" != "$want" ]; then
		printf 'FAIL %s: код %s, ждём %s\n' "$name" "$rc" "$want"
		sed 's/^/  | /' "$FAKE_DIR/out"
		fail=1
	fi
}
has() { grep -qF -- "$2" "$work/$1/out" || { printf 'FAIL %s: нет строки «%s»\n' "$1" "$2"; sed 's/^/  | /' "$work/$1/out"; fail=1; }; }

case_ refused 0 0 "$refused" 5 "$refused" 5
has refused "P45: «@daily» — отказ: код 5, API_ERROR: 400 InvalidArgument"
has refused "P45: «*/5 * * * *» — отказ"
[ ! -f "$work/refused/deleted" ] || { echo "FAIL refused: удаление без созданного кластера"; fail=1; }

case_ accepted 0 0 '{"status":"creating","cluster_id":"c1"}' 0 "$refused" 5
has accepted "P45: «@daily» — принят платформой"
grep -qx 'evo-e2e-pgsch1-r7a1' "$work/accepted/deleted" 2>/dev/null || { echo "FAIL accepted: не удалён evo-e2e-pgsch1-r7a1"; fail=1; }

case_ notdeleted 1 1 '{"status":"creating"}' 0 '{"status":"creating"}' 0
has notdeleted "не удалён"

set +e
CLUSTER=prod-db SPEC_ID=s SUBNET_ID=n "$here/pg-schedule-probe.sh" >/dev/null 2>&1
rc=$?
set -e
[ "$rc" = 2 ] || { echo "FAIL name: код $rc, ждём 2"; fail=1; }

[ "$fail" = 0 ] && echo "pg-schedule-probe: ok"
exit "$fail"
