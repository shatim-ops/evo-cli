#!/usr/bin/env bash
# D61 вживую: dbaas-passing-delete.sh pg|kafka|redis CLUSTER_ID. Вызывают
# job'ы dbaas-pg, dbaas-kafka, dbaas-redis сразу после create, пока кластер
# создаётся (cloud-e2e.yml, план rc.1 1.6, пункт Г).
#
# Кластер в DEPLOYING, CREATING или PROVISIONING (engine.PassingDeleteState):
# <kind>_delete с --yes обязан отказать до вопроса — USAGE, код 1, — и
# кластер остаётся (не DELETING и не пропал из <kind>_get). Удаление
# принято или отказ другой — провал: D61 не держит, а кластер, возможно,
# удаляется (его уберёт шаг удаления job'а).
#
# Кластер уже не создаётся (READY и т.п.) — проверка не покрыта, не провал:
# строка «не покрыто» с состоянием; создание у платформы бывает быстрее
# первого опроса.
#
# Лог публичный: печатаются вид, состояние, код и reason; ID — никогда.
#
# Код выхода: 0 — отказ до вопроса (1 из 1) или не покрыто; 1 — провал;
# 2 — неверный вызов.
set -euo pipefail

kind=${1:?нужен вид: pg, kafka или redis}
id=${2:?нужен ID кластера}
case "$kind" in pg | kafka | redis) ;; *)
	echo "::error::неизвестный вид кластера: $kind"
	exit 2
	;;
esac
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# status — состояние кластера заглавными; «?» — не прочитано.
status() {
	evo tools call "${kind}_get" --arg cluster_id="$id" --json 2>/dev/null |
		jq -r '(.status // .state // "?") | tostring | ascii_upcase' 2>/dev/null || echo "?"
}

st=$(status)
case "$st" in
DEPLOYING | CREATING | PROVISIONING) ;;
*)
	echo "D61 ${kind}: не покрыто — кластер уже $st, а не «создаётся»"
	exit 0
	;;
esac

set +e
evo --yes tools call "${kind}_delete" --arg cluster_id="$id" --json >"$tmp/out" 2>"$tmp/err"
rc=$?
set -e
reason=$(jq -rR 'fromjson? | .error.reason? // empty' "$tmp/err" 2>/dev/null | head -1)
after=$(status)
echo "D61 ${kind}: состояние $st, ${kind}_delete --yes: код $rc, ${reason:-нет reason}; после: $after"
if [ "$rc" = 1 ] && [ "$reason" = USAGE ]; then
	case "$after" in
	*DELET* | *ERAS* | "?")
		echo "::error::D61 ${kind}: отказ USAGE, но кластер $after — удаление ушло или кластер не читается"
		exit 1
		;;
	esac
	echo "D61 ${kind}: отказ до вопроса, кластер на месте — 1 из 1"
	exit 0
fi
echo "::error::D61 ${kind}: удаление кластера в $st — код $rc, ${reason:-нет reason}; ждали USAGE и код 1 до вопроса"
exit 1
