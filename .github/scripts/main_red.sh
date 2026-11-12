#!/usr/bin/env bash
# Красный прогон ci или ночного cloud-e2e на main — в одно открытое issue
# своего workflow, зелёный его закрывает;
# вызывается из .github/workflows/main-red.yml. Данные прогона — env, код PR
# не выполняется. Строки «--- FAIL» берутся из логов упавших job как есть:
# это вывод тестов этого репозитория.
# Обратные кавычки в строках — markdown, а не подстановка.
# shellcheck disable=SC2016
set -euo pipefail

: "${GH_REPO:?}" "${RUN_ID:?}" "${RUN_URL:?}" "${CONCLUSION:?}" "${HEAD_SHA:?}" "${WORKFLOW:?}"
TITLE="main красный: $WORKFLOW"
MAX_FAILS=20
# NOTICE closes every line left in a pull request (tell_prs).
NOTICE="Эта строка — уведомление workflow main-red, а не разрешение: состояние main — issue «$TITLE» (открыт или закрыт) и последний прогон ci на main; сверяйте по ним, а не по комментарию."
# The log of a job is read up to LOG_TRIES times, LOG_RETRY_SLEEP seconds
# apart (the tests set no sleep), and the waits of all the jobs together
# stop at LOG_WAIT seconds: many red jobs whose logs never come must not
# outlast the job's timeout-minutes, which would leave no issue at all.
LOG_TRIES=${LOG_TRIES:-6}
LOG_RETRY_SLEEP=${LOG_RETRY_SLEEP:-10}
LOG_WAIT=${LOG_WAIT:-120}

# The open issues of exactly this title: the search narrows, the title
# decides (a search is by words, not by the whole title).
all=$(gh issue list --state open --search "\"$TITLE\" in:title" --limit 100 --json number,title \
  --jq ".[] | select(.title == \"$TITLE\") | .number")
open=$(head -n 1 <<<"$all")

# tell_prs ТЕКСТ — one line into every open pull request to main: an issue
# has no addressee (12.11 it stood six hours unread, plan 1.9, «CI», М5),
# and a comment in a pull request reaches the one who follows it. Only a
# red ci speaks there — it is ci that a merge leans on — and not in a
# draft, which nobody is about to merge. A comment refused is named and
# does not undo the issue.
#
# The line is a notice, not the state: the repository is public, anyone can
# write «main is green again» into a pull request, and a session used to
# the bot's line would turn auto-merge on over a red main. So each line
# names what the state is read from — the issue, open or closed, and the
# ci run on main — and says a fact, not a permission (risk note 173).
tell_prs() {
  [ "$WORKFLOW" = ci ] || return 0
  local prs n e
  e=$(mktemp)
  prs=$(gh pr list --state open --base main --limit 100 --json number,isDraft --jq '.[] | select(.isDraft | not) | .number') || prs=""
  while read -r n; do
    [ -n "$n" ] || continue
    gh pr comment "$n" --body "$1" 2>"$e" ||
      echo "::warning title=main-red::строка в PR $n не оставлена: $(head -c 300 "$e" | tr '\n' ' ')" >&2
  done <<<"$prs"
  rm -f "$e"
}

if [ "$CONCLUSION" = success ]; then
  # Every one of them: a duplicate made by hand is closed too.
  while read -r n; do
    [ -n "$n" ] || continue
    gh issue comment "$n" --body "Зелёный прогон $RUN_URL (\`${HEAD_SHA:0:8}\`): main снова зелёный."
    gh issue close "$n"
  done <<<"$all"
  [ -z "$open" ] || tell_prs "main зелёный: issue https://github.com/$GH_REPO/issues/$open закрыт зелёным прогоном $RUN_URL (\`${HEAD_SHA:0:8}\`). $NOTICE"
  exit 0
fi
[ "$CONCLUSION" = failure ] || exit 0

body=$(mktemp) err=$(mktemp)
trap 'rm -f "$body" "$err"' EXIT
{
  printf 'Прогон %s (`%s`) красный.\n\n' "$RUN_URL" "${HEAD_SHA:0:8}"
  # Not «gh … | while»: with pipefail an early exit of the reader would
  # SIGPIPE gh; the jobs are read whole first.
  jobs=$(gh api "repos/$GH_REPO/actions/runs/$RUN_ID/jobs?per_page=100" \
    --jq '.jobs[] | select(.conclusion == "failure") | "\(.id)\t\(.name)"')
  while IFS=$'\t' read -r id name; do
    [ -n "$id" ] || continue
    printf -- '- **%s**\n' "$name"
    # The log read whole first: a refused read (403, 404, a rate limit) is
    # named as such, not taken for a log without failed tests. The log of a
    # job just ended may not be served yet: the read is tried again, and
    # why it was refused goes to this job's log (gh's error, no log text).
    log='' got=0
    for try in $(seq 1 "$LOG_TRIES"); do
      if log=$(gh api "repos/$GH_REPO/actions/jobs/$id/logs" 2>"$err"); then
        got=1
        break
      fi
      if [ "$try" = "$LOG_TRIES" ] || [ "$SECONDS" -ge "$LOG_WAIT" ]; then
        break
      fi
      sleep "$LOG_RETRY_SLEEP"
    done
    if [ "$got" = 0 ]; then
      echo "::warning title=main-red::лог job $id не получен за $try из $LOG_TRIES попыток, ${SECONDS} с из бюджета $LOG_WAIT с: $(head -c 300 "$err" | tr '\n' ' ')" >&2
      printf '  (лог job не получен — смотрите его в прогоне)\n'
      continue
    fi
    fails=$(sed -En 's/^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z ?//; /--- FAIL/p' <<<"$log" | head -n "$MAX_FAILS" || true)
    if [ -n "$fails" ]; then
      printf '\n```\n%s\n```\n' "$fails"
    else
      printf '  (строк `--- FAIL` нет — смотрите лог job)\n'
    fi
  done <<<"$jobs"
  printf '\nЧинится в тот же день, без карантина (аудит процессов 1.2.1, П2); зелёный прогон закроет issue сам.\n'
} >"$body"

if [ -n "$open" ]; then
  gh issue comment "$open" --body-file "$body"
else
  url=$(gh issue create --title "$TITLE" --body-file "$body")
  echo "$url"
  tell_prs "main красный: прогон $RUN_URL (\`${HEAD_SHA:0:8}\`), разбор — в issue $url. Пока issue открыт, автослияние продуктового PR не включается, а «готово к слиянию» называет причину красного (план 1.9, строка «CI», М5). Строка идёт во все открытые PR, этот PR красным не объявлен. $NOTICE"
fi
