#!/usr/bin/env bash
# Каждый живой прогон — отчёт (план 1.9, строка «Каждый живой прогон —
# отчёт», решение владельца 14.11): разбор api-probe 54, cloud-e2e 218 и 219
# пропущен на 3–4,5 часа — у прогона не было хозяина разбора, а платный
# остаток в проекте дошёл до владельца только строкой отчёта.
#
# live_run_report.sh open — по завершённому прогону (workflow
#   live-run-report, событие workflow_run): прогон, у которого начался хотя
#   бы один job вне NOT_LIVE, получает issue «Разбор живого прогона
#   <workflow> <номер>» с меткой LABEL; красный исход — в заголовке.
#   Повтор попытки — комментарий в то же issue. В тексте — только номер,
#   ссылка и исход: сведения о платформе — в отчёте evo-internal.
# live_run_report.sh check — раз в час: красный (код 1) список
#   - issue с меткой, открытое дольше 24 часов;
#   - issue, закрытое после SINCE без ссылки на отчёт (комментарий со
#     ссылкой на evo-internal: PR или файл в runs/);
#   - живой прогон после SINCE без issue (событие не дошло, job open упал).
#   Красный запуск по расписанию доводит до «релиза» main-red: issue «main
#   красный: live-run-report» «релиз» читает первым делом.
#
# Окружение: GH_REPO; для open — RUN_ID, RUN_NUMBER, RUN_URL, WORKFLOW,
# CONCLUSION, RUN_ATTEMPT; для check — SINCE (ISO 8601). NOW (секунды Unix)
# подменяет «сейчас» в проверке скрипта.
set -euo pipefail
LABEL=разбор-прогона
WORKFLOWS="cloud-e2e api-probe cloud-readonly-os stand"
# Job, которые облака не меняют и до одобрения идут сами: preflight читает
# ключом только на чтение (на main), live-plan облака не трогает, readonly
# читает ключом наблюдателя (окружение cloud-readonly) — ночной прогон
# cloud-e2e состоит только из него. Прогон, где начались только они
# (ночное чтение; weekly_stale.sh отменил неодобренный), — не живой:
# отчёта не нужно (cloud-e2e 220, 221 — ночные чтения; «релиз» 17.11).
NOT_LIVE="preflight live-plan readonly"
REPORT_RE='evo-internal(/pull/[0-9]+|#[0-9]+|/blob/[^ )]*/runs/)'
: "${GH_REPO:?}"

title_of() { printf 'Разбор живого прогона %s %s' "$1" "$2"; }

# api_fail WHAT — ответа GitHub нет: красный с причиной, а не «нечего
# разбирать» и не «всё разобрано» (ревью Qwen и «Риск» 278 к #2070:
# сбой сторожа — красная проверка, а не тихий зелёный).
# Вывод — в исходный stdout (fd 3): из подстановки $(…) строка не уходит
# в значение, а код 1 вызывающий передаёт дальше.
exec 3>&1
api_fail() {
	echo "::error::GitHub API не ответил ($1): проверка не состоялась" >&3
	exit 1
}

# live RUN_ID — начался ли job вне NOT_LIVE (хотя бы один шаг): 0 — да,
# 1 — нет; без ответа API — api_fail.
live() {
	local skip n
	skip=$(jq -nc --arg s "$NOT_LIVE" '$s | split(" ")')
	n=$(gh api "repos/$GH_REPO/actions/runs/$1/jobs?filter=all&per_page=100" \
		--jq "[.jobs[] | select((.name as \$n | $skip | index(\$n) | not) and ((.steps // []) | length > 0))] | length") ||
		api_fail "jobs прогона $1"
	[ "$n" != 0 ]
}

# issues STATE [SINCE] JQ — issue сторожа: метка — параметром -f, gh кодирует
# её в адресе. Метка кириллицей прямо в пути запроса — ответ GitHub не JSON
# («invalid character '<'»), и open падал на каждом прогоне (15–17.11:
# cloud-e2e 220, 221, stand 3, 4 остались без issue).
issues() {
	local st=$1 since=$2 q=$3
	gh api -X GET "repos/$GH_REPO/issues" -f labels="$LABEL" -f state="$st" -f per_page=100 \
		${since:+-f since="$since"} --paginate --jq "$q"
}

# issue_of TITLE — номер issue сторожа с этим заголовком (с «— красный» или
# без), открытого или закрытого; пусто — нет.
issue_of() {
	local all
	all=$(issues all "" ".[] | select(.pull_request | not) | select(.title == \"$1\" or (.title | startswith(\"$1 — \"))) | .number") ||
		api_fail "issue с меткой $LABEL"
	head -n1 <<<"$all"
}

case "${1:-}" in
open)
	: "${RUN_ID:?}" "${RUN_NUMBER:?}" "${RUN_URL:?}" "${WORKFLOW:?}" "${CONCLUSION:?}"
	if ! live "$RUN_ID"; then
		echo "$WORKFLOW $RUN_NUMBER: начались только $NOT_LIVE или ничего — облака не трогал, разбора не нужно"
		exit 0
	fi
	t=$(title_of "$WORKFLOW" "$RUN_NUMBER")
	red=
	[ "$CONCLUSION" = success ] || red=" — красный ($CONCLUSION)"
	n=$(issue_of "$t") || exit 1
	if [ -n "$n" ]; then
		gh issue comment "$n" --body "Попытка ${RUN_ATTEMPT:-?}: $CONCLUSION — $RUN_URL" >/dev/null
		[ -z "$red" ] || gh issue edit "$n" --title "$t$red" >/dev/null
		echo "$WORKFLOW $RUN_NUMBER: попытка ${RUN_ATTEMPT:-?} — комментарий в #$n"
		exit 0
	fi
	gh label create "$LABEL" --force --color D93F0B \
		--description "Разбор живого прогона: закрыть ссылкой на отчёт в evo-internal" >/dev/null
	body=$(printf '%s\n' \
		"Прогон: $RUN_URL" \
		"Исход: $CONCLUSION" \
		"Хозяин разбора: «Процесс»." \
		"" \
		"Отчёт — в evo-internal, каталог runs/; сведения о платформе сюда не пишутся." \
		"Закрыть комментарием со ссылкой на отчёт или его PR в evo-internal; открытое дольше 24 часов — красный live-run-report.")
	url=$(gh issue create --title "$t$red" --label "$LABEL" --body "$body")
	echo "$WORKFLOW $RUN_NUMBER: $url"
	;;
check)
	: "${SINCE:?}"
	now=${NOW:-$(date +%s)}
	bad=0
	# Окно — SINCE, но не дальше 7 суток назад: проверка идёт раз в час, и
	# старше недели её красное уже было; так число запросов не растёт с
	# числом прогонов (общий часовой лимит токена; остаток «Риска» 259).
	since_s=$(date -d "$SINCE" +%s)
	week=$((now - 7 * 86400))
	[ "$since_s" -ge "$week" ] || since_s=$week
	from=$(date -u -d "@$since_s" +%Y-%m-%dT%H:%M:%SZ)
	open_list=$(issues open "" '.[] | select(.pull_request | not) | [.number, .created_at, .title] | @tsv') ||
		api_fail "открытые issue"
	closed_list=$(issues closed "$from" '.[] | select(.pull_request | not) | [.number, .closed_at] | @tsv') ||
		api_fail "закрытые issue"
	# Открытые дольше суток.
	while IFS=$'\t' read -r n created t; do
		[ -n "$n" ] || continue
		age=$(((now - $(date -d "$created" +%s)) / 3600))
		if [ "$age" -ge 24 ]; then
			echo "::error::#$n открыт $age ч без отчёта: $t"
			bad=1
		fi
	done <<<"$open_list"
	# Закрытые после SINCE без ссылки на отчёт.
	while IFS=$'\t' read -r n closed; do
		[ -n "$n" ] || continue
		[ "$(date -d "$closed" +%s)" -ge "$since_s" ] || continue
		bodies=$(gh api "repos/$GH_REPO/issues/$n/comments?per_page=100" --paginate --jq '.[].body') ||
			api_fail "комментарии #$n"
		if ! grep -Eq "$REPORT_RE" <<<"$bodies"; then
			echo "::error::#$n закрыт без ссылки на отчёт в evo-internal"
			bad=1
		fi
	done <<<"$closed_list"
	# Живые прогоны после SINCE без issue.
	for wf in $WORKFLOWS; do
		runs=$(gh api "repos/$GH_REPO/actions/workflows/$wf.yml/runs?status=completed&created=>=${from%%T*}&per_page=100" \
			--jq ".workflow_runs[] | select(.created_at >= \"$from\") | [.id, .run_number] | @tsv") ||
			api_fail "прогоны $wf"
		while IFS=$'\t' read -r id num; do
			[ -n "$id" ] || continue
			live "$id" || continue
			n=$(issue_of "$(title_of "$wf" "$num")") || exit 1
			if [ -z "$n" ]; then
				echo "::error::$wf $num: живой прогон без issue разбора"
				bad=1
			fi
		done <<<"$runs"
	done
	[ "$bad" = 0 ] && echo "живые прогоны после $from разобраны или в пределах суток"
	exit "$bad"
	;;
*)
	echo "вызов: live_run_report.sh open|check" >&2
	exit 2
	;;
esac
