#!/usr/bin/env bash
# Красный прогон ci на main — в одно открытое issue, зелёный его закрывает;
# вызывается из .github/workflows/main-red.yml. Данные прогона — env, код PR
# не выполняется. Строки «--- FAIL» берутся из логов упавших job как есть:
# это вывод тестов этого репозитория.
# Обратные кавычки в строках — markdown, а не подстановка.
# shellcheck disable=SC2016
set -euo pipefail

: "${GH_REPO:?}" "${RUN_ID:?}" "${RUN_URL:?}" "${CONCLUSION:?}" "${HEAD_SHA:?}"
TITLE="main красный: ci"
MAX_FAILS=20

# The open issues of exactly this title: the search narrows, the title
# decides (a search is by words, not by the whole title).
all=$(gh issue list --state open --search "\"$TITLE\" in:title" --limit 100 --json number,title \
  --jq ".[] | select(.title == \"$TITLE\") | .number")
open=$(head -n 1 <<<"$all")

if [ "$CONCLUSION" = success ]; then
  # Every one of them: a duplicate made by hand is closed too.
  while read -r n; do
    [ -n "$n" ] || continue
    gh issue comment "$n" --body "Зелёный прогон $RUN_URL (\`${HEAD_SHA:0:8}\`): main снова зелёный."
    gh issue close "$n"
  done <<<"$all"
  exit 0
fi
[ "$CONCLUSION" = failure ] || exit 0

body=$(mktemp)
trap 'rm -f "$body"' EXIT
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
    # named as such, not taken for a log without failed tests.
    if ! log=$(gh api "repos/$GH_REPO/actions/jobs/$id/logs" 2>/dev/null); then
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
  gh issue create --title "$TITLE" --body-file "$body"
fi
