#!/usr/bin/env bash
# go test job ci (решения владельца #370, вариант 1, и #493):
#   - на Windows в pull request — только пакеты с кодом Windows
#     (windowsPRPackages); иначе — два прохода: все пакеты, кроме internal/tools, затем
#     internal/tools один. Сбой рантайма Go приходил только под нагрузкой
#     параллельных пакетов: 14 из 828 попыток 22–28.10, все в internal/tools;
#     так — 0 из 300 (проба ci-probe, run 40740766759, #494). Это обход, а
#     не исправление: причина в рантайме не найдена;
#   - не на Windows internal/cli идёт двумя процессами рядом с остальными
#     пакетами (мера М6 строки «CI» плана 1.9): тесты пакета
#     последовательны из-за общих переменных, под -race он был самым долгим
#     в job и на macOS дошёл до предела go test; делить можно только
#     процессами. Части — из одного списка тестов (go test -list): группа
#     имён cliFirstHalf и все остальные; тест вне частей или в обеих —
#     красный job до запуска;
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

# cliFirstHalf — имена тестов первой части internal/cli; вторая — все
# остальные. Группы «каждый инструмент × каждый мир»: около половины времени
# пакета под -race (замер 12.11).
cliFirstHalf='^Test(Parity|Consent|Contract|Human)'
together=no

# WINDOWS_PR_TOOLS=yes (classify.sh, строка wintools=): PR трогает код путей
# internal/tools — пакет идёт на Windows и в PR, вторым проходом, один,
# как на main (сбой рантайма Go приходил под нагрузкой соседних пакетов).
if [ "${RUNNER_OS:-}" = Windows ] && [ "${GITHUB_EVENT_NAME:-}" = pull_request ]; then
	passes=("${windowsPRPackages[*]}")
	[ "${WINDOWS_PR_TOOLS:-}" != yes ] || passes+=("$tools")
elif [ "${RUNNER_OS:-}" = Windows ]; then
	# shellcheck disable=SC2207 # import paths have no spaces
	rest=($(go list ./... | grep -vxF "$tools"))
	passes=("${rest[*]}" "$tools")
else
	cli=$(go list ./internal/cli)
	# shellcheck disable=SC2207 # import paths have no spaces
	rest=($(go list ./... | grep -vxF "$cli"))
	passes=("${rest[*]}")
	# Список не собрался (пакет не компилируется): один проход по всему,
	# ошибку покажет сам go test.
	if "$@" -list '.*' "$cli" >"$logs/list" 2>/dev/null; then
		grep -E '^(Test|Example|Fuzz)' "$logs/list" | sort -u >"$logs/all"
		grep -E "$cliFirstHalf" "$logs/all" >"$logs/a"
		grep -vE "$cliFirstHalf" "$logs/all" >"$logs/b"
		if [ ! -s "$logs/a" ] || [ ! -s "$logs/b" ] || [ -n "$(sort "$logs/a" "$logs/b" | uniq -d)" ] ||
			! sort -u "$logs/a" "$logs/b" | cmp -s - "$logs/all"; then
			echo "::error::части internal/cli не равны пакету: тест вне частей или в обеих (cliFirstHalf)"
			exit 1
		fi
		passes+=("-run ^($(paste -sd'|' "$logs/a"))\$ $cli" "-run ^($(paste -sd'|' "$logs/b"))\$ $cli")
		together=yes
	else
		passes=(./...)
	fi
fi

# notes — строки, не массив: пустой массив под set -u в bash 3.2 (macOS)
# — «unbound variable».
# Проходы: по очереди с выводом по ходу (Windows) либо все разом, вывод —
# по завершении, проход за проходом (together). GO_TEST_CI_PASS — номер
# прохода для поддельного go в go_test_ci_test.sh.
set -f # аргументы прохода делятся по пробелам; имена тестов — не шаблоны
i=0
for pkgs in "${passes[@]}"; do
	i=$((i + 1))
	if [ "$together" = yes ]; then
		# shellcheck disable=SC2086 # the arguments of the pass are split on purpose
		(
			GO_TEST_CI_PASS=$i "$@" $pkgs >"$logs/$i.log" 2>&1
			echo $? >"$logs/$i.rc"
		) &
	else
		# shellcheck disable=SC2086 # the package list is split on purpose
		GO_TEST_CI_PASS=$i "$@" $pkgs 2>&1 | tee "$logs/$i.log"
		echo "${PIPESTATUS[0]}" >"$logs/$i.rc"
	fi
done
wait
set +f

red=0 retry="" i=0 notes=""
for pkgs in "${passes[@]}"; do
	i=$((i + 1))
	log="$logs/$i.log"
	[ "$together" != yes ] || cat "$log"
	[ "$(cat "$logs/$i.rc" 2>/dev/null)" = 0 ] && continue
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
