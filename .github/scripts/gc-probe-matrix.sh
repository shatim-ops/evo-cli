#!/usr/bin/env bash
# Проба GC на Windows, третий запуск (weekly-race.yml, job gc-probe;
# process-handoff.md, «CI не от diff»). Три оси внутри job — -race, проверки
# сборщика GODEBUG, асинхронное вытеснение — восемь прогонов пакета по
# -count=3; Go и сборщик (GOEXPERIMENT) — матрица job. Прогон не
# останавливает следующий: код выхода каждого — в таблице сводки, первая
# строка падения рантайма — рядом, хвост вывода упавшего — в лог job.
#
#   gc-probe-matrix.sh <пакет>
set -uo pipefail

pkg=${1:?usage: gc-probe-matrix.sh <package>}
summary=${GITHUB_STEP_SUMMARY:-/dev/stdout}
out=$(mktemp -d)

{
	echo "### $(go version) · GOEXPERIMENT=${GOEXPERIMENT:-}"
	echo
	echo "| -race | GODEBUG | asyncpreemptoff | код | падение |"
	echo "|---|---|---|---|---|"
} >>"$summary"

for race in on off; do
	for checks in on off; do
		for preempt in on off; do
			flags=(-count=3 -timeout=20m)
			[ "$race" = on ] && flags+=(-race)
			debug=""
			[ "$checks" = on ] && debug="invalidptr=1,gccheckmark=1"
			if [ "$preempt" = off ]; then
				debug="${debug:+$debug,}asyncpreemptoff=1"
			fi
			log="$out/$race-$checks-$preempt.log"
			start=$(date +%s)
			GODEBUG="$debug" GORACE=atexit_sleep_ms=0 go test "${flags[@]}" "$pkg" >"$log" 2>&1
			rc=$?
			took=$(($(date +%s) - start))
			crash=$(grep -m1 -E 'fatal error:|^Exception 0x|^panic: ' "$log" | tr '|' '/' || true)
			if [ "$rc" -ne 0 ]; then
				echo "::group::-race=$race GODEBUG=$debug — код $rc"
				grep -m1 -n -A40 -E 'fatal error:|^Exception 0x|^panic: |^--- FAIL' "$log" || tail -n 60 "$log"
				echo "::endgroup::"
			fi
			echo "| $race | ${checks} | $([ "$preempt" = off ] && echo 1 || echo 0) | $rc (${took} с) | ${crash:-—} |" >>"$summary"
		done
	done
done
