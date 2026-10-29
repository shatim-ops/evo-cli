#!/usr/bin/env bash
# Сбои рантайма Go за неделю (#370, вариант 1): шаг повтора ci.yml
# («go test: повтор после сбоя рантайма Go») выполнялся — случай; счёт по
# всем попыткам job ci.yml за 7 дней, сводка — в сводке job. Случаев не
# меньше порога — код 1: weekly-race красный, обход split (#494) больше не
# держит.
#
#   go_runtime_crash_week.sh <порог>
set -euo pipefail

limit=${1:?usage: go_runtime_crash_week.sh <threshold>}
repo=${GITHUB_REPOSITORY:?}
summary=${GITHUB_STEP_SUMMARY:-/dev/stdout}
step='go test: повтор после сбоя рантайма Go'
since=$(date -u -d '7 days ago' +%Y-%m-%dT%H:%M:%SZ)

runs=$(gh api --paginate "repos/$repo/actions/workflows/ci.yml/runs?created=%3E%3D$since&per_page=100" |
	jq -r '.workflow_runs[].id')
rows=""
for id in $runs; do
	rows+=$(gh api --paginate "repos/$repo/actions/runs/$id/jobs?filter=all&per_page=100" |
		jq -r --arg s "$step" '.jobs[] | . as $j | .steps[]? | select(.name == $s and (.conclusion == "success" or .conclusion == "failure")) | "| \($j.name) | \(.conclusion) | \($j.html_url) |"')
	rows+=$'\n'
done
rows=$(grep -v '^$' <<<"$rows" || true)
n=0
[ -z "$rows" ] || n=$(wc -l <<<"$rows")

{
	echo "### Сбои рантайма Go за 7 дней с $since: случаев $n, порог $limit"
	if [ -n "$rows" ]; then
		echo
		echo "| job | повтор | ссылка |"
		echo "|---|---|---|"
		echo "$rows"
	fi
} >>"$summary"
if [ "$n" -ge "$limit" ]; then
	echo "::error::сбоев рантайма Go за неделю $n при пороге $limit (#370): обход split (#494) не держит — разбор в process-handoff.md"
	exit 1
fi
echo "сбоев рантайма Go за неделю: $n (порог $limit)"
