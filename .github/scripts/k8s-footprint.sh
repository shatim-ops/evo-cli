#!/usr/bin/env bash
# След кластера Managed Kubernetes в проекте (заключение «Риска» 189, У3):
# узлы, их диски, адреса, группы безопасности и балансировщики, которые
# заводит сам сервис, сторож по имени кластера не видит.
#
#   k8s-footprint.sh save ФАЙЛ  — до создания кластера: id ВМ, дисков,
#                                  адресов, групп безопасности и балансировщиков
#                                  проекта в ФАЙЛ (не в лог);
#   k8s-footprint.sh check ФАЙЛ — после удаления кластера: что появилось с
#                                  тех пор и осталось, по видам и числу.
#
# Ресурсы с именем evo-e2e-*, кроме evo-e2e-k8s-*, — других job того же
# запуска: они не след кластера и не считаются. Разница — ::warning:: с
# видами и числом, id только под маской; это не «ресурсов пробы: 0».
# Код 0 всегда, кроме непрочитанного списка (2): след без списка — не ноль.
set -euo pipefail
mode=${1:?save или check}
file=${2:?нужен файл}
kinds="vm_list:vms disk_list:disks list_floating_ips:floating_ips list_security_groups:security_groups lb_list:load_balancers"
# ids ИНСТРУМЕНТ ПОЛЕ — id ресурсов вида, кроме чужих evo-e2e-*.
ids() {
	evo tools call "$1" --json | jq -r --arg f "$2" '.[$f][]? | select(((.name // "") | startswith("evo-e2e-")) and (((.name // "") | startswith("evo-e2e-k8s-")) | not) | not) | .id // empty'
}
snap() {
	local k t f
	for k in $kinds; do
		t=${k%%:*} f=${k#*:}
		# «вид .» — вид прочитан (и пустым): строка есть в обоих снимках.
		ids "$t" "$f" | sed "s/^/$t /" || { echo "::warning::след кластера: $t не прочитан" >&2; return 2; }
		echo "$t ."
	done
}
case "$mode" in
save)
	snap >"$file" || exit 2
	echo "след кластера: до создания прочитано видов: $(grep -c ' \.$' "$file") из 5"
	;;
check)
	[ -f "$file" ] || { echo "::warning::след кластера: нет снимка до создания — не проверен"; exit 2; }
	now=$(snap) || exit 2
	new=$(comm -13 <(sort "$file") <(sort <<<"$now") | grep . || true)
	if [ -z "$new" ]; then
		echo "след кластера в проекте: 0"
		exit 0
	fi
	while read -r _ id; do echo "::add-mask::$id"; done <<<"$new"
	echo "::warning::след кластера в проекте после его удаления: $(cut -d' ' -f1 <<<"$new" | sort | uniq -c | awk '{printf "%s%s %s", s, $2, $1; s=", "}') — не «ресурсов пробы: 0»; что это, разобрать в отчёте прогона"
	;;
*)
	echo "::error::неизвестный режим: $mode"
	exit 2
	;;
esac
