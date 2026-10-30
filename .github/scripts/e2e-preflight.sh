#!/usr/bin/env bash
# Проверка до прогона cloud-e2e (джоб preflight): только чтение, за минуту,
# до любого создания. Провал — один отчёт со всеми причинами (как отказы до
# вопроса у evo), и джобы с ресурсами не стартуют. Ретро релиза 1.5, run 147
# и 148: имена занимали остатки прошлых прогонов, в ru.AZ-3 у проекта не
# было подсети и группы безопасности — джобы падали после одобрения и часов
# ожидания.
#
# Причины провала:
#   - вход неверен: зона не имя, слово dbaas_services не pg, kafka, redis;
#   - зона джоба не найдена или закрыта (enabled=false);
#   - в зоне джоба у проекта нет подсети или группы безопасности;
#   - имя этого прогона (…-$RUN) уже занято ВМ или кластером;
#   - исчерпана квота ресурса прогона (ВМ, vCPU, RAM, адреса, диски);
#   - список, без которого не проверить, не прочитан.
# Строки отчёта, не провал: остатки прошлых прогонов evo-e2e-* (ВМ в ошибке,
# кластеры FAILED) — их убирает cleanup_only, а платформа — не всегда;
# открытая зона без подсети или группы, куда этот прогон не идёт (перезапуск
# туда не выйдет); квоты, которые не прочитаны или исчерпаны у других
# ресурсов (образы, CMP-16).
#
# Окружение — входы workflow: ZONE, K8S_ZONE, PG_ZONE, KAFKA_ZONE,
# REDIS_ZONE (пусто — ZONE), OS_SMOKE, K8S_SMOKE, DBAAS_SMOKE, DBAAS_SERVICES,
# REDIS_SA (true/false); RUN — суффикс прогона; PROJECT — для org_quotas.
# Лог публичный: печатаются имена зон и ресурсов evo-e2e-*, коды квот и
# числа; ID — никогда.
set -euo pipefail

reasons=()
notes=()
fail() { reasons+=("$1"); }
note() { notes+=("$1"); }

# call ПЕРЕМЕННАЯ ИНСТРУМЕНТ [АРГУМЕНТЫ] — ответ в ПЕРЕМЕННУЮ; не прочитан —
# причина с ошибкой evo (evo_err.sh: reason и текст через redact.sed) и
# пусто. Не $(call …): причина из подоболочки потерялась бы.
err="${RUNNER_TEMP:-/tmp}/e2e-preflight.err"
call() {
	local var=$1 out
	shift
	if out=$(evo tools call "$@" --json 2>"$err") && jq -e 'type == "object"' <<<"$out" >/dev/null 2>&1; then
		printf -v "$var" '%s' "$out"
	else
		printf -v "$var" ''
		fail "$1 не прочитан — проверку не провести: $("$(dirname "$0")/evo_err.sh" "$err")"
	fi
}

# Джобы прогона и их зоны: «джоб зона».
plan=()
zone_of() { printf '%s' "${1:-$ZONE}"; }
plan+=("e2e $ZONE")
[ "${OS_SMOKE:-}" != true ] || plan+=("os-smoke $ZONE")
[ "${K8S_SMOKE:-}" != true ] || plan+=("k8s $(zone_of "${K8S_ZONE:-}")")
declare -A cluster_kinds=()
[ "${K8S_SMOKE:-}" != true ] || cluster_kinds[k8s]=1
if [ "${DBAAS_SMOKE:-}" = true ]; then
	[ -n "${DBAAS_SERVICES// /}" ] || fail "dbaas_services пуст, а dbaas_smoke включён: не пойдёт ни один джоб DBaaS"
	for s in ${DBAAS_SERVICES:-}; do
		case "$s" in
		pg) plan+=("pg $(zone_of "${PG_ZONE:-}")") ;;
		kafka) plan+=("kafka $(zone_of "${KAFKA_ZONE:-}")") ;;
		redis) plan+=("redis $(zone_of "${REDIS_ZONE:-}")") ;;
		*)
			fail "dbaas_services: «$s» — не pg, kafka, redis; такой джоб не пойдёт"
			continue
			;;
		esac
		cluster_kinds[$s]=1
	done
fi
if [ "${REDIS_SA:-}" = true ]; then
	plan+=("redis-sa $(zone_of "${REDIS_ZONE:-}")")
	cluster_kinds[redis]=1
fi

# Зоны прогона: «зона» → «джоб, джоб».
declare -A jobs_in=()
for p in "${plan[@]}"; do
	j=${p%% *} z=${p#* }
	if ! printf '%s' "$z" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$'; then
		fail "зона джоба $j: «$z» — не имя зоны"
		continue
	fi
	jobs_in[$z]="${jobs_in[$z]:+${jobs_in[$z]}, }$j"
done

call zones list_zones
call subnets list_subnets
call groups list_security_groups
if [ -n "$zones" ] && [ -n "$subnets" ] && [ -n "$groups" ]; then
	# has ВИД ID_ЗОНЫ ИМЯ_ЗОНЫ — есть подсеть или группа этой зоны (без
	# availability_zone_id — по имени, как pg_create в cloud-e2e).
	has() {
		local list=$subnets field=subnets
		[ "$1" = subnet ] || { list=$groups field=security_groups; }
		jq -e --arg f "$field" --arg z "$2" --arg n "$3" \
			'[.[$f][]? | select(.availability_zone_id == $z or ((.availability_zone_id // "") == "" and ((.name // "") | contains($n))))] | length > 0' \
			<<<"$list" >/dev/null
	}
	for z in "${!jobs_in[@]}"; do
		row=$(jq -c --arg n "$z" '[.zones[]? | select(.name == $n)][0] // empty' <<<"$zones")
		if [ -z "$row" ]; then
			fail "$z (${jobs_in[$z]}): зоны нет в list_zones"
			continue
		fi
		if [ "$(jq -r '.enabled' <<<"$row")" != true ]; then
			fail "$z (${jobs_in[$z]}): зона закрыта (enabled=false) — Cloud.ru в ней сейчас не создаёт; другая зона — входом"
			continue
		fi
		id=$(jq -r '.id' <<<"$row")
		has subnet "$id" "$z" || fail "$z (${jobs_in[$z]}): нет подсети проекта в зоне — создайте Default_$z или возьмите другую зону"
		has group "$id" "$z" || fail "$z (${jobs_in[$z]}): нет группы безопасности проекта в зоне — создайте SSH-access_$z или возьмите другую зону"
	done
	while read -r id z; do
		if [ -z "$id" ] || [ -n "${jobs_in[$z]:-}" ]; then
			continue
		fi
		miss=()
		has subnet "$id" "$z" || miss+=("подсети")
		has group "$id" "$z" || miss+=("группы безопасности")
		[ "${#miss[@]}" = 0 ] || note "$z: открыта, но нет ${miss[*]} — перезапуск туда не выйдет"
	done < <(jq -r '.zones[]? | select(.enabled == true) | "\(.id) \(.name)"' <<<"$zones")
fi

# Имена: этого прогона заняты — провал; остатки прошлых — строки отчёта.
call vms vm_list
if [ -n "$vms" ]; then
	while read -r n st; do
		[ -n "$n" ] || continue
		case "$n" in
		*-"$RUN") fail "имя $n занято (ВМ, $st): у прогона имена с суффиксом $RUN" ;;
		*) case "$st" in error* | *error*) note "остаток: ВМ $n: $st" ;; esac ;;
		esac
	done < <(jq -r '.vms[]? | select((.name // "") | startswith("evo-e2e-")) | "\(.name) \(.state // "?")"' <<<"$vms")
fi
for k in "${!cluster_kinds[@]}"; do
	call list "${k}_list"
	[ -n "$list" ] || continue
	while read -r n st; do
		[ -n "$n" ] || continue
		case "$n" in
		*-"$RUN") fail "имя $n занято (кластер $k, $st): у прогона имена с суффиксом $RUN" ;;
		*) case "$st" in FAILED | ERROR* | *ERROR) note "остаток: $k $n: $st" ;; esac ;;
		esac
	done < <(jq -r '.clusters[]? | select((.name // "") | startswith("evo-e2e-")) | "\(.name) \((.status // "?") | tostring | ascii_upcase)"' <<<"$list")
done

# Квоты: ресурс прогона исчерпан — провал. Коды квот платформы не
# документированы: ресурс прогона — по слову кода (гипотеза Процесса, 1.6).
if q=$(evo tools call org_quotas --arg project_id="${PROJECT:-}" --json 2>/dev/null) && [ "$(jq -r '.status // "ok"' <<<"$q" 2>/dev/null)" = ok ]; then
	while IFS=$'\t' read -r code limit usage; do
		[ -n "$code" ] || continue
		run_res=false
		for w in $(tr '._-' '   ' <<<"$code"); do
			case "$w" in vm | vms | instance | instances | cpu | vcpu | vcpus | ram | memory | fip | floating | ip | ips | disk | disks | volume | volumes) run_res=true ;; esac
		done
		if [ "$run_res" = true ]; then
			fail "квота $code исчерпана: $usage из $limit"
		else
			note "квота $code исчерпана: $usage из $limit (не ресурс прогона)"
		fi
	done < <(jq -r '.quota_usages[]? | select((.limit | type) == "number" and .limit > 0 and (.usage | type) == "number" and .usage >= .limit) | [.resource, .limit, .usage] | @tsv' <<<"$q")
else
	note "квоты не прочитаны (у ключа нет области организации или ответ не ok) — не проверены"
fi

{
	echo "### cloud-e2e preflight, прогон $RUN"
	echo
	for p in "${plan[@]}"; do echo "- джоб ${p%% *}: зона ${p#* }"; done
	echo
	if [ "${#reasons[@]}" = 0 ]; then
		echo "preflight: можно запускать"
	else
		echo "preflight: нельзя запускать, причин: ${#reasons[@]}"
		for r in "${reasons[@]}"; do echo "- $r"; done
	fi
	if [ "${#notes[@]}" != 0 ]; then
		echo
		echo "Отчёт (не провал):"
		for n in "${notes[@]}"; do echo "- $n"; done
	fi
} | tee -a "${GITHUB_STEP_SUMMARY:-/dev/null}"
for r in "${reasons[@]}"; do echo "::error::preflight: $r"; done
[ "${#reasons[@]}" = 0 ]
