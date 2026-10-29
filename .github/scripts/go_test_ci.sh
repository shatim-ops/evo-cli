#!/usr/bin/env bash
# go test job ci (решения владельца #370, вариант 1, и #493):
#   - на Windows в pull request — только пакеты с кодом Windows
#     (windowsPRPackages); иначе — два прохода: все пакеты, кроме internal/tools, затем
#     internal/tools один. Сбой рантайма Go приходил только под нагрузкой
#     параллельных пакетов: 14 из 828 попыток 22–28.10, все в internal/tools;
#     так — 0 из 300 (проба ci-probe, run 40740766759, #494). Это обход, а
#     не исправление: причина в рантайме не найдена;
#   - сбой рантайма (go_runtime_crash.sh) без падения теста — код 0 и
#     retry=<пакеты> в GITHUB_OUTPUT: шаг повтора ci.yml запускает их один
#     раз, второй сбой подряд — красный. Падение теста рядом, стек через
#     internal/config, сбой без имени пакета — код 1, без повтора.
# Каждый повтор — шаг в истории job; go_runtime_crash_week.sh считает их
# за неделю.
#
#   go_test_ci.sh go test <флаги>
set -uo pipefail

here=$(dirname "$0")
logs=$(mktemp -d)
trap 'rm -rf "$logs"' EXIT
tools=$(go list ./internal/tools)

# windowsPRPackages — пакеты с кодом Windows (*_windows.go, runtime.GOOS):
# на pull request Windows гоняет только их, всё — main, ночной прогон и
# выпуск (решение владельца 29.10, аудит #578, п. 10: сбой рантайма Go на
# Windows — 14 из 828 попыток, а свой код Windows — три пакета). Список
# держит TestWorkflows_WindowsPRCoversWindowsCode.
windowsPRPackages=(./internal/cli ./internal/config ./internal/journal)

if [ "${RUNNER_OS:-}" = Windows ] && [ "${GITHUB_EVENT_NAME:-}" = pull_request ]; then
	passes=("${windowsPRPackages[*]}")
elif [ "${RUNNER_OS:-}" = Windows ]; then
	# shellcheck disable=SC2207 # import paths have no spaces
	rest=($(go list ./... | grep -vxF "$tools"))
	passes=("${rest[*]}" "$tools")
else
	passes=(./...)
fi

# notes — строки, не массив: пустой массив под set -u в bash 3.2 (macOS)
# — «unbound variable».
red=0 retry="" i=0 notes=""
for pkgs in "${passes[@]}"; do
	i=$((i + 1))
	log="$logs/$i.log"
	# shellcheck disable=SC2086 # the package list is split on purpose
	"$@" $pkgs 2>&1 | tee "$log"
	[ "${PIPESTATUS[0]}" = 0 ] && continue
	if grep -q -- '^--- FAIL' "$log" || ! sig=$(bash "$here/go_runtime_crash.sh" "$log"); then
		red=1
		continue
	fi
	failed=${sig##*| }
	if [ -z "$failed" ] || [ "$failed" = "$sig" ]; then
		echo "::error::сбой рантайма Go без имени пакета — повторять нечего: $sig"
		red=1
		continue
	fi
	notes+="::warning::сбой рантайма Go (#370): ${sig%% |*} — повтор: $failed"$'\n'
	retry="${retry:+$retry }$failed"
done

[ "$red" = 0 ] || exit 1
printf '%s' "$notes"
if [ -n "$retry" ]; then
	echo "retry=$retry" >>"${GITHUB_OUTPUT:-/dev/null}"
fi
exit 0
