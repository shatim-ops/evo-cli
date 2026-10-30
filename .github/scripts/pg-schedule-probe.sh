#!/usr/bin/env bash
# Проба P45 (план cloud-e2e rc.1 1.6, пункт Ж; FB15-32; реестр DB-6):
# принимает ли Managed PostgreSQL расписание резервных копий «@daily» и
# «*/5 * * * *». Документация даёт пример `15 14 * * *`, спецификация у
# BackupSpec.schedule противоречит сама себе. evo расписание не проверяет,
# поэтому — pg_create с backup_schedule, по кластеру на расписание; ожидание
# — отказ платформы до создания. Принятый кластер — платный: он сразу
# удаляется dbaas-delete.sh (повторы, пока «ещё создаётся»).
#
# Вызывает job dbaas-pg (вход probes: p45); окружение — CLUSTER (имя
# кластера прогона, evo-e2e-pg-…), SPEC_ID и SUBNET_ID (его спецификация и
# подсеть). Имена кластеров пробы — evo-e2e-pgsch1-…, evo-e2e-pgsch2-….
#
# Проба, не проверка: итог — строки «P45: …» для реестра. Код выхода 0 —
# пробы прошли и кластеров пробы не осталось; 1 — принятый кластер не
# удалён (остаток уберёт cleanup_only); 2 — неверный вызов.
#
# Лог публичный: печатаются расписание, код, reason и текст ошибки API через
# redact.sed (обрезанный); ID — никогда.
set -euo pipefail

: "${CLUSTER:?нужен CLUSTER}" "${SPEC_ID:?нужен SPEC_ID}" "${SUBNET_ID:?нужен SUBNET_ID}"
case "$CLUSTER" in evo-e2e-pg-*) ;; *)
	echo "::error::CLUSTER $CLUSTER не evo-e2e-pg-*"
	exit 2
	;;
esac
echo "::add-mask::$SPEC_ID"
echo "::add-mask::$SUBNET_ID"
here=$(cd "$(dirname "$0")" && pwd)
redact() { sed -E -f "$here/redact.sed"; }
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

left=0 i=0
for sched in "@daily" "*/5 * * * *"; do
	i=$((i + 1))
	name="evo-e2e-pgsch$i-${CLUSTER#evo-e2e-pg-}"
	set +e
	evo --yes tools call pg_create --arg name="$name" --arg specification_id="$SPEC_ID" --arg subnet_id="$SUBNET_ID" \
		--arg backup_schedule="$sched" --json >"$tmp/out" 2>"$tmp/err"
	rc=$?
	set -e
	if [ "$rc" = 0 ]; then
		id=$(jq -r '.cluster_id // empty' "$tmp/out" 2>/dev/null || true)
		[ -z "$id" ] || echo "::add-mask::$id"
		echo "P45: «$sched» — принят платформой, кластер $name создаётся; удаление"
		if ! "$here/dbaas-delete.sh" pg "$name"; then
			echo "::error::P45: кластер $name не удалён — его уберёт cleanup_only"
			left=1
		fi
		continue
	fi
	reason=$(jq -rR 'fromjson? | .error.reason? // empty' "$tmp/err" 2>/dev/null | head -1)
	msg=$(jq -rR 'fromjson? | .error.message? // empty' "$tmp/err" 2>/dev/null | head -1 | redact | cut -c1-200)
	echo "P45: «$sched» — отказ: код $rc, ${reason:-нет reason}: ${msg:-нет текста}"
done
exit "$left"
