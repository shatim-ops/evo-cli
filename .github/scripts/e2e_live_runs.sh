#!/usr/bin/env bash
# Уборка cleanup_only идёт в своей очереди, рядом с живыми запусками
# cloud-e2e (решение владельца 12.11, план 1.9, Вп-14), и удаляет ресурсы
# только завершённых запусков. Имя каждого ресурса запуска несёт его номер
# (RUN = r<номер>a<попытка>, TestWorkflows_E2ENamesCarryTheRun): ресурс
# запуска, который ещё не завершён (идёт, в очереди, ждёт одобрения), уборка
# не трогает. Имена без номера запуска (старые evo-e2e-pg2…) — остатки, как
# и раньше.
#
#   e2e_live_runs.sh       — регулярное выражение (ERE, годится и jq test)
#                            имён ресурсов незавершённых запусков;
#   e2e_live_runs.sh drop  — фильтр stdin: убирает строки, последнее поле
#                            которых (через TAB) — имя такого ресурса.
#   e2e_live_runs.sh others — число незавершённых запусков, кроме своего
#                            (GITHUB_RUN_NUMBER): для ресурсов, общих для всех
#                            прогонов, у которых номера запуска в имени нет.
#   e2e_live_runs.sh keep  — фильтр stdin, как drop, но только по ресурсам
#                            пробы цены (ниже); список запусков не читает.
#
# Ресурсы пробы цены Г-20 (план 1.9: остановленная ВМ живёт сутки, потом
# читается её потребление) несут в имени слово g20 и переживают свой запуск:
# уборка их не трогает, пока её не запустили со словом g20 во входе probes
# (PROBES) — тогда они обычные остатки. Выражение без аргумента и drop
# включают их имена, пока слова нет: каждое место, которое спрашивает «чей
# это ресурс», получает ответ из одной точки. Уборка самой области в конце
# запуска (keep) не удаляет их никогда.
#
#   e2e_live_runs.sh overdue — срок этого исключения: stdin — ответ vm_list;
#                            ВМ пробы цены печатаются с возрастом. ВМ старше
#                            KEEP_HOURS (сутки пробы и запас), без времени
#                            создания или список не разобран — код 1: уборка,
#                            которую запустили без слова g20, краснеет, а не
#                            оставляет платный ресурс без срока.
#
# Номер 0 в выражении есть всегда (запуска 0 не бывает): выражение не пусто
# и тогда, когда незавершённых запусков нет. Список запусков не прочитан или
# в нём не числа — код 1 и пустой stdout: уборка, которая не знает живых
# запусков, не удаляет ничего (вызывающий обязан проверить код: присваивание
# отдельной командой, не подстановка в аргументе).
#
# Список читается через gh (GH_TOKEN, GITHUB_REPOSITORY; право actions:
# read) и держится E2E_LIVE_TTL секунд (60) в $RUNNER_TEMP: циклы удаления
# опрашивают облако раз в 30–60 с по два-четыре часа, а запуск, одобренный
# после начала уборки, должен попасть в список до своего первого ресурса —
# от постановки в очередь до первого создания проходят минуты (preflight,
# сборка).
# Проверка — e2e_live_runs_test.sh (job ci).
set -euo pipefail

# Состояния запуска GitHub, кроме completed.
STATES="queued in_progress waiting requested pending"
KEEP_RE='(^|-)g20(-|$)'
KEEP_HOURS=72
# with_keep ВЫРАЖЕНИЕ — оно же и имена ресурсов пробы цены, пока уборку не
# запустили со словом g20.
with_keep() {
	case " ${PROBES:-} " in *" g20 "*) printf '%s\n' "$1" ;; *) printf '%s|%s\n' "$1" "$KEEP_RE" ;; esac
}

live_re() {
	local cache="${RUNNER_TEMP:-/tmp}/e2e-live-runs" ttl=${E2E_LIVE_TTL:-60} s nums='' n re
	if [ -s "$cache" ] && [ $(($(date +%s) - $(stat -c %Y "$cache"))) -lt "$ttl" ]; then
		cat "$cache"
		return 0
	fi
	: "${GITHUB_REPOSITORY:?}"
	for s in $STATES; do
		n=$(gh api --paginate "repos/$GITHUB_REPOSITORY/actions/workflows/cloud-e2e.yml/runs?status=$s&per_page=100" \
			--jq '.workflow_runs[].run_number') || {
			echo "::error::запуски cloud-e2e ($s) не прочитаны — какие запуски живы, неизвестно: уборка ничего не удаляет" >&2
			return 1
		}
		nums="$nums $n"
	done
	re='(^|-)r(0'
	for n in $nums; do
		case "$n" in '' | *[!0-9]*)
			echo "::error::в списке запусков cloud-e2e не номер — уборка ничего не удаляет" >&2
			return 1
			;;
		esac
		re="$re|$n"
	done
	re="$re)a[0-9]+([^0-9]|\$)"
	printf '%s\n' "$re" >"$cache.$$"
	mv "$cache.$$" "$cache"
	printf '%s\n' "$re"
}

case "${1:-}" in
'')
	re=$(live_re)
	with_keep "$re"
	;;
drop)
	re=$(live_re)
	awk -F'\t' -v re="$(with_keep "$re")" '$NF !~ re'
	;;
keep) awk -F'\t' -v re="$KEEP_RE" '$NF !~ re' ;;
overdue)
	rows=$(jq -r --arg re "$KEEP_RE" --argjson now "$(date +%s)" '.vms[] | select((.name // "") | test("^evo-e2e-") and test($re))
		| "\(.name)\t\(.state // .status // "?")\t\(((.created_time // "") | fromdateiso8601? // null) as $t | if $t then (($now - $t) / 3600 | floor) else "?" end)"') || {
		echo "::error::список ВМ не разобран — жива ли ВМ пробы цены Г-20 и сколько ей, неизвестно"
		exit 1
	}
	late=0
	while IFS=$'\t' read -r name state hours; do
		[ -n "$name" ] || continue
		echo "ВМ пробы цены $name: $state, возраст $hours ч (срок $KEEP_HOURS ч)"
		case "$hours" in '?') late=1 ;; *) [ "$hours" -lt "$KEEP_HOURS" ] || late=1 ;; esac
	done <<<"$rows"
	[ "$late" = 0 ] || {
		echo "::error::ВМ пробы цены Г-20 старше $KEEP_HOURS ч или без времени создания: она, её диск и адрес платные. Прочитать потребление и удалить — cleanup_only со словом g20 во входе probes"
		exit 1
	}
	;;
others)
	re=$(live_re)
	n=0
	for r in $(printf '%s' "$re" | sed -E 's/^\(\^\|-\)r\(0//; s/\)a.*$//' | tr '|' ' '); do
		[ "$r" = "${GITHUB_RUN_NUMBER:-}" ] || n=$((n + 1))
	done
	echo "$n"
	;;
*)
	echo "::error::e2e_live_runs.sh: неизвестный режим $1" >&2
	exit 2
	;;
esac
