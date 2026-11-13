#!/usr/bin/env bash
# Удаление кластера Managed Kubernetes e2e по имени: k8s-delete.sh ИМЯ.
# Шаг «Удаление кластера (всегда)» job k8s-smoke в cloud-e2e.yml, после evo
# configure. Трогает только кластеры с именем ровно ИМЯ, ИМЯ — evo-e2e-*.
#
# Каждый найденный кластер удаляется (evo --yes tools call k8s_delete).
# Отказ платформы (кластер в обновлении, группа узлов в переходе) — не
# конец: удаление повторяется, когда состояние кластера сменилось, до
# предела шага K8S_DEL_WAIT (с, 1800 по умолчанию; с пробой Г-12 job
# передаёт больше — на обновление версии). Одна попытка без повтора
# оставляла платный кластер с узлами (заключение «Риска» 189, У2; урок
# остатка ВМ прогона 186). Остаток в конце — ::error:: поимённо: имя,
# состояние, число групп узлов; код 1.
#
# K8S_DEL_ONCE=true — прогон отменён: одна попытка, без ожидания; остаток
# уберёт cleanup_only. K8S_DEL_POLL — пауза опроса, с (60; тест — 0).
# Лог публичный: id только маскируются.
set -euo pipefail

name=${1:?нужно имя кластера}
case "$name" in evo-e2e-*) ;; *)
	echo "::error::имя $name не evo-e2e-*: такие кластеры этот скрипт не трогает"
	exit 2
	;;
esac
redact() { sed -E -f "$(dirname "$0")/redact.sed"; }
poll=${K8S_DEL_POLL:-60}
wait=${K8S_DEL_WAIT:-1800}
err="${RUNNER_TEMP:-/tmp}/k8s-del.err"

# rows — «id состояние» кластеров с именем $name.
rows() { evo tools call k8s_list --json | jq -r --arg n "$name" '.clusters[]? | select(.name == $n) | "\(.id) \(.status // "?")"'; }
# del ID — удаление; принято — код 0, отказ — строка с reason и код 1.
del() {
	local rc=0 why
	evo --json --yes tools call k8s_delete --arg cluster_id="$1" >/dev/null 2>"$err" || rc=$?
	[ "$rc" = 0 ] && { echo "удаление принято"; return 0; }
	why=$(jq -rR 'fromjson? | .error | "\(.reason // "?"): \(.message // "" | .[0:160])"' "$err" 2>/dev/null | head -1)
	echo "удаление отвергнуто (код $rc, ${why:-ответа нет}): повтор — когда состояние кластера сменится" | redact
	return 1
}

declare -A tried=() refused=()
start=$(date +%s)
first=1
while :; do
	list=$(rows)
	[ -n "$list" ] || {
		if [ "$first" = 1 ]; then echo "кластера $name нет"; else echo "кластер удалён"; fi
		exit 0
	}
	while read -r id st; do
		echo "::add-mask::$id"
		# Первая попытка — сразу; после отказа — только при смене состояния.
		if [ -z "${tried[$id]:-}" ] || { [ -n "${refused[$id]:-}" ] && [ "${tried[$id]}" != "$st" ]; }; then
			tried[$id]=$st
			if del "$id"; then unset "refused[$id]"; else refused[$id]=1; fi
		fi
	done <<<"$list"
	first=0
	if [ "${K8S_DEL_ONCE:-}" = true ]; then
		echo "::warning::прогон отменён: одна попытка удаления $name — остаток уберёт cleanup_only"
		exit 0
	fi
	echo "$(( ($(date +%s) - start) / 60 )) мин: $(awk '{print $2}' <<<"$list" | sort | uniq -c | awk '{printf "%s%s×%s", s, $2, $1; s=", "}')"
	if [ $(( $(date +%s) - start )) -ge "$wait" ]; then
		while read -r id st; do
			pools=$(evo tools call k8s_nodepools --arg cluster_id="$id" --json 2>/dev/null | jq '[.node_pools[]?] | length' 2>/dev/null || echo '?')
			echo "::error::кластер $name не удалился за $((wait / 60)) мин: состояние $st, групп узлов: $pools — платный остаток; удалите его в консоли Cloud.ru или cleanup_only, строка владельцу"
		done <<<"$list"
		exit 1
	fi
	sleep "$poll"
done
