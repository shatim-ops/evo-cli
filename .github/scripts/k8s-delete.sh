#!/usr/bin/env bash
# Уборка только по ID из успешной операции создания K8s. После отказа
# DELETE можно повторить при смене состояния. Исчезновение проверяем
# по полному списку и GET с тем же ID. Имена сами по себе прав не дают.
set -euo pipefail
name=${1:?нужно имя кластера}
id=${2:-}
[[ "$name" == evo-e2e-* && "$id" =~ ^[A-Za-z0-9._-]{1,128}$ ]] ||
	{ echo "::error::нет допустимого ID из операции создания или имени прогона; исход неизвестен"; exit 2; }
poll=${K8S_DEL_POLL:-60}
wait=${K8S_DEL_WAIT:-1800}
err="${RUNNER_TEMP:-/tmp}/k8s-del.err"
echo "::add-mask::$id"

# Полный список кандидата. Неоднозначный или повреждённый ответ останавливает запись.
state() {
	evo tools call k8s_list --json | jq -er --arg id "$id" --arg n "$name" '
		.clusters | if type != "array" or any(.[]; (.id | type != "string" or length == 0)) then error("неполный список")
		else map(select(.id == $id)) | if length == 0 then "absent"
		elif length == 1 and .[0].name == $n then .[0].status | strings | select(length > 0)
		else error("не совпал объект") end end'
}
gone() {
	local rc=0
	evo tools call k8s_get --arg cluster_id="$id" --json >/dev/null 2>"$err" || rc=$?
	[ "$rc" = 4 ] && jq -e '.error.reason == "NOT_FOUND" and
		(.error.message | test("mk8s API 404: "; "i") and test("not found|не найден"; "i") and (test("<html|<body|gateway|no route"; "i") | not))' "$err" >/dev/null
}

tried='' accepted=false
start=$(date +%s)
while :; do
	st=$(state) || { echo "::error::ответ списка не подтверждает состояние своего кластера"; exit 1; }
	if [ "$st" = absent ]; then
		# Асинхронный CREATE может ещё не отразиться ни в списке, ни в GET.
		if [ -n "$tried" ] && gone; then echo "кластер удалён: ID отсутствует в списке и GET вернул 404"; exit 0; fi
	elif [ "$accepted" = false ] && [ "$tried" != "$st" ]; then
		tried=$st
		if evo --json --yes tools call k8s_delete --arg cluster_id="$id" >/dev/null 2>"$err"; then
			accepted=true
			echo "удаление принято"
		else
			why=$(jq -r '.error.reason // "?"' "$err" 2>/dev/null || echo '?')
			echo "удаление отвергнуто ($why), повтор при смене состояния"
		fi
	fi
	if [ "${K8S_DEL_ONCE:-}" = true ]; then
		echo "::error::прогон отменён, исход уборки своего кластера не подтверждён"; exit 1
	fi
	echo "$(( ($(date +%s) - start) / 60 )) мин: $st"
	if [ $(( $(date +%s) - start )) -ge "$wait" ]; then
		pools=$(evo tools call k8s_nodepools --arg cluster_id="$id" --json 2>/dev/null | jq '.node_pools | arrays | length' 2>/dev/null || echo '?')
		echo "::error::кластер $name: уборка не подтверждена, состояние $st, групп узлов: $pools"; exit 1
	fi
	sleep "$poll"
done
