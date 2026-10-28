#!/usr/bin/env bash
# Проба сбоя рантайма Go на Windows (строка плана 1.5 «Падение рантайма Go
# на Windows», #493): N раз подряд команда go test одного варианта CI,
# каждый прогон — ok, сбой рантайма (go_runtime_crash.sh) или падение
# теста. Итог — строка «вариант: прогонов N, сбоев K, падений тестов F» и
# таблица сбоев в сводке job. «До» — история CI: 14 сбоев на 828 попыток
# `test (windows-latest)` 22–28.10, все в internal/tools.
#   together — как ci.yml: go test -race ./... (все пакеты параллельно);
#   split    — все пакеты, кроме internal/tools, затем internal/tools один
#              (проба 23–25.10: tools один — 0 из 192);
#   norace   — go test ./... без -race.
#
#   ci-probe-loop.sh <вариант> <N>
set -uo pipefail

arm=${1:?usage: ci-probe-loop.sh together|split|norace N}
n=${2:?usage: ci-probe-loop.sh together|split|norace N}
here=$(dirname "$0")
summary=${GITHUB_STEP_SUMMARY:-/dev/stdout}
out=$(mktemp -d)
export GORACE=atexit_sleep_ms=0

run() {
	case "$arm" in
	together) go test -race -count=1 -shuffle=off ./... ;;
	split)
		# shellcheck disable=SC2046 # the package list is split on purpose
		go test -race -count=1 -shuffle=off $(go list ./... | grep -v '/internal/tools$') &&
			go test -race -count=1 -shuffle=off ./internal/tools/
		;;
	norace) go test -count=1 -shuffle=off ./... ;;
	*)
		echo "unknown arm $arm" >&2
		return 2
		;;
	esac
}

crashes=0 fails=0 rows=""
for i in $(seq 1 "$n"); do
	log="$out/$i.log"
	start=$(date +%s)
	run >"$log" 2>&1
	rc=$?
	took=$(($(date +%s) - start))
	if [ "$rc" -eq 2 ] && grep -q '^unknown arm' "$log"; then
		cat "$log" >&2
		exit 2
	fi
	if [ "$rc" -eq 0 ]; then
		echo "$arm $i: ok (${took} s)"
	elif sig=$(bash "$here/go_runtime_crash.sh" "$log"); then
		crashes=$((crashes + 1))
		rows+="| $i | ${took} с | ${sig//|//} |"$'\n'
		echo "$arm $i: сбой рантайма — $sig"
		echo "::group::$arm $i — стек"
		grep -m1 -n -A60 -E '^fatal error:|^Exception 0x' "$log"
		echo "::endgroup::"
	else
		fails=$((fails + 1))
		echo "$arm $i: падение теста, код $rc"
		grep -m5 -E '^--- FAIL|^FAIL' "$log"
	fi
done

{
	echo "### $arm: прогонов $n, сбоев рантайма $crashes, падений тестов $fails"
	echo
	go version
	if [ -n "$rows" ]; then
		echo
		echo "| прогон | время | сигнатура и пакеты |"
		echo "|---|---|---|"
		printf '%s' "$rows"
	fi
} >>"$summary"
echo "$arm: прогонов $n, сбоев рантайма $crashes, падений тестов $fails"
