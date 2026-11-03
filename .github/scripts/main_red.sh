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

open=$(gh issue list --state open --search "\"$TITLE\" in:title" --json number --jq '.[0].number // empty')

if [ "$CONCLUSION" = success ]; then
  if [ -n "$open" ]; then
    gh issue comment "$open" --body "Зелёный прогон $RUN_URL (\`${HEAD_SHA:0:8}\`): main снова зелёный."
    gh issue close "$open"
  fi
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
    fails=$(gh api "repos/$GH_REPO/actions/jobs/$id/logs" 2>/dev/null |
      sed -En 's/^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z ?//; /--- FAIL/p' | head -n "$MAX_FAILS" || true)
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
