#!/usr/bin/env bash
# Удаление кластеров DBaaS e2e по имени: dbaas-delete.sh pg|kafka|redis ИМЯ.
# Вызывают job'ы dbaas-pg, dbaas-kafka, dbaas-redis (шаг удаления, всегда)
# и cleanup (cleanup_only) в cloud-e2e.yml после evo configure.
#
# Трогает только кластеры с именем ровно ИМЯ, и ИМЯ обязано начинаться с
# evo-e2e- (проект общий: у внешнего аудитора — audit-*). Каждый найденный
# кластер удаляется (evo --yes tools call <kind>_delete). Кластер, который
# ещё создаётся, Cloud.ru удалять отказывается: PostgreSQL и Redis — 400
# FailedPrecondition, Kafka — 500 «failed to get delete meta» (реестр DB-17,
# DB-19); тогда повтор раз в 60 с до 120 мин. PostgreSQL в FAILED публичный
# API не удаляет вовсе («may be delete only with force flag», DB-21) —
# ошибка сразу. Затем ждём до 30 мин, пока имя не пропадёт из <kind>_list.
# Кластер в ERASED или DELETED — удалён: платформа держит его в списке, а
# evo отвечает на его удаление already_deleted (D44, D26). Кластер в FAILED
# не повторяется: удаления не будет, ответ evo обязан назвать путь —
# консоль или поддержку (D44, ось 3 CLAUDE.md §6).
#
# Когда живых кластеров не осталось, удаление каждого виденного кластера
# повторяется без согласия (без --yes: записи нет): ждём already_deleted и
# existed false (D44, D26) — и для ERASED, и для пропавшего из списка.
#
# Лог публичный: ID только маскируются (::add-mask::), печатаются статусы,
# счётчики и reason; текст ошибок API — через redact.sed и обрезанным.
#
# LOG_PREFIX (необязательно) — префикс строк лога, когда несколько вызовов
# идут параллельно (job cleanup); команды ::add-mask:: и ::error:: идут без
# него, иначе GitHub их не распознаёт.
#
# DBAAS_POLL (необязательно) — пауза между опросами, с; 60 по умолчанию
# (офлайн-тест dbaas_delete_test.sh ставит 0).
#
# Код выхода: 0 — кластеров с этим именем нет; 1 — остались; 3 — не
# осталось, но повтор удаления ответил не already_deleted.
set -euo pipefail

kind=${1:?нужен вид: pg, kafka или redis}
name=${2:?нужно имя кластера}
case "$kind" in pg | kafka | redis) ;; *)
	echo "::error::неизвестный вид кластера: $kind"
	exit 2
	;;
esac
case "$name" in evo-e2e-*) ;; *)
	echo "::error::имя $name не evo-e2e-*: такие кластеры этот скрипт не трогает"
	exit 2
	;;
esac
redact_sed="$(dirname "$0")/redact.sed"
redact() { sed -E -f "$redact_sed"; }
say() { printf '%s%s\n' "${LOG_PREFIX:-}" "$*"; }
command -v evo >/dev/null || {
	echo "::error::нет evo в PATH — кластеры $name не проверены"
	exit 1
}
out="${RUNNER_TEMP:-/tmp}/dbaas-del-$kind.json"
poll=${DBAAS_POLL:-60}

# Повтор удаления каждого виденного кластера без согласия: already_deleted,
# existed false (D44, D26); код не 0 — хотя бы один ответил иначе.
repeat_deleted() {
	local id rc ans st ex bad=0
	for id in "${!seen[@]}"; do
		set +e
		evo tools call "${kind}_delete" --arg cluster_id="$id" --json >"$out" 2>/dev/null
		rc=$?
		set -e
		ans=$(cat "$out")
		st=$(jq -r '.status // "-"' <<<"$ans" 2>/dev/null || echo -)
		ex=$(jq -r '.existed | tostring' <<<"$ans" 2>/dev/null || echo -)
		if [ "$rc" = 0 ] && [ "$st" = already_deleted ] && [ "$ex" = false ]; then
			say "повтор удаления: already_deleted, existed false"
		else
			say "повтор удаления: код $rc, status $st, existed $ex, reason $(jq -r '.reason // "-"' <<<"$ans" 2>/dev/null || echo -)"
			echo "::error::повтор ${kind}_delete удалённого кластера $name — не already_deleted (D44, D26)"
			bad=1
		fi
	done
	return "$bad"
}

# [{id, status}] кластеров с этим именем; код не 0 — список не прочитан.
clusters() {
	evo tools call "${kind}_list" --json 2>/dev/null |
		jq -ce --arg n "$name" 'if (.clusters | type) == "array" then [.clusters[] | select(.name == $n) | {id, status: ((.status // "?") | tostring | ascii_upcase)}] else error("нет clusters") end' 2>/dev/null
}

start=$(date +%s)
retry_until=$((start + 7200)) # отказы «ещё создаётся» — до 120 мин
wait_until=0                  # после того как все удаления приняты — 30 мин
read_ok=0                     # список хоть раз прочитан
declare -A accepted=()
declare -A seen=() # id всех кластеров с этим именем, виденных в списке
while :; do
	now=$(date +%s)
	min=$(((now - start) / 60))
	if ! list=$(clusters); then
		say "$min мин: ${kind}_list не прочитан"
		# Ни разу за 10 мин: не сбой API, а нет доступа — ждать 120 мин незачем.
		[ "$read_ok" = 1 ] || [ "$((now - start))" -lt 600 ] || {
			echo "::error::${kind}_list не читается 10 мин — кластеры $name не проверены"
			exit 1
		}
	else
		read_ok=1
		for id in $(jq -r '.[].id' <<<"$list"); do
			echo "::add-mask::$id"
			seen[$id]=1
		done
		say "$min мин: кластеров $name: $(jq length <<<"$list"), статусы $(jq -c '[.[].status]' <<<"$list")"
		list=$(jq -c '[.[] | select(.status != "ERASED" and .status != "DELETED")]' <<<"$list")
		if [ "$(jq length <<<"$list")" = 0 ]; then
			say "кластеров $name нет (в списке — только ERASED или DELETED, если есть)"
			repeat_deleted || exit 3
			exit 0
		fi
		pending=0
		while read -r id st; do
			[ -n "$id" ] || continue
			case "$st" in DELET*) accepted[$id]=1 ;; esac
			[ -z "${accepted[$id]:-}" ] || continue
			set +e
			evo --yes tools call "${kind}_delete" --arg cluster_id="$id" --json >"$out" 2>/dev/null
			rc=$?
			set -e
			ans=$(cat "$out")
			dst=$(jq -r '.status // "-"' <<<"$ans" 2>/dev/null || echo -)
			reason=$(jq -r '.reason // "-"' <<<"$ans" 2>/dev/null || echo -)
			text=$(jq -r '[(.error // ""), (.hint // "")] | join(" ")' <<<"$ans" 2>/dev/null || true)
			if [ "$rc" = 0 ] && [ "$dst" = deleting ]; then
				say "удаление ($st): принято"
				accepted[$id]=1
			elif [ "$reason" = NOT_FOUND ]; then
				say "удаление ($st): кластера уже нет"
				accepted[$id]=1
			elif [ "$st" = FAILED ]; then
				say "удаление ($st): код $rc, reason $reason: $(redact <<<"$text" | cut -c1-300)"
				if grep -Eqi 'консол|console|поддержк|support' <<<"$text"; then
					echo "::error::кластер $name FAILED: Cloud.ru его не удаляет; путь — в ответе evo (D44, реестр DB-21)"
				else
					echo "::error::кластер $name FAILED: ответ evo не называет путь — консоль или поддержку (D44)"
				fi
				exit 1
			elif grep -qi 'force flag' <<<"$text"; then
				say "удаление ($st): код $rc, reason $reason: $(redact <<<"$text" | cut -c1-300)"
				echo "::error::кластер $name FAILED: удалите в консоли Cloud.ru (реестр DB-21)"
				exit 1
			elif grep -Eqi 'FailedPrecondition|failed to get delete meta|DEPLOYING|still being created' <<<"$text"; then
				say "удаление ($st): Cloud.ru отказал — кластер ещё создаётся (DB-17, DB-19), повтор через 60 с"
				pending=$((pending + 1))
			else
				say "удаление ($st): код $rc, reason $reason: $(redact <<<"$text" | cut -c1-300)"
				pending=$((pending + 1))
			fi
		done < <(jq -r '.[] | "\(.id) \(.status)"' <<<"$list")
		if [ "$pending" = 0 ] && [ "$wait_until" = 0 ]; then
			wait_until=$((now + 1800))
		fi
	fi
	now=$(date +%s)
	if [ "$wait_until" != 0 ]; then
		[ "$now" -lt "$wait_until" ] || {
			echo "::error::кластер $name не удалился за 30 мин после принятого удаления — удалите его в консоли Cloud.ru"
			exit 1
		}
	else
		[ "$now" -lt "$retry_until" ] || {
			echo "::error::Cloud.ru 120 мин не принимал удаление кластера $name — удалите его в консоли Cloud.ru"
			exit 1
		}
	fi
	sleep "$poll"
done
