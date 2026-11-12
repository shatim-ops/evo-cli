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

# Время пакетов — в сводку прогона, по ОС (план 1.9, строка «CI», М3):
# пять самых долгих и сигнал, когда пакет идёт дольше половины предела
# -timeout. Рост времени виден до отказа: 12.11 internal/cli на macOS дошёл
# до предела за сутки, и заметил это только красный job. Сигнал — строка
# сводки и warning, job не красный. Предел берётся из флагов этого же
# вызова go test, второго числа нет. Сортировка — в awk: sort на Windows
# может оказаться не тем.
limit=""
prev=""
for a in "$@"; do
	case "$a" in -timeout=*) limit=${a#-timeout=} ;; esac
	[ "$prev" != -timeout ] || limit=$a
	prev=$a
done
flag=$limit
case "$limit" in
*[!0-9]*[hms] | [hms]) limit="" ;;
*h) limit=$((${limit%h} * 3600)) ;;
*m) limit=$((${limit%m} * 60)) ;;
*s) limit=${limit%s} ;;
*) limit="" ;;
esac
[ "$limit" != 0 ] || limit=""
# Предел есть, но не число с h, m или s (составной «1h30m», «0»): сигнал
# не должен пропасть молча.
if [ -n "$flag" ] && [ -z "$limit" ]; then
	echo "::warning::go test: предел -timeout «$flag» не разобран — сигнала о времени пакетов не будет (go_test_ci.sh понимает число с h, m или s)"
fi
module=${tools%/internal/tools}
# Путь сводки идёт в awk через окружение, а не -v: в значении -v awk
# разбирает escape-последовательности, и путь Windows (D:\a\_temp\…)
# становился другим файлом.
cat "$logs"/*.log 2>/dev/null | SUMMARY="${GITHUB_STEP_SUMMARY:-/dev/null}" awk -F'\t' -v os="${RUNNER_OS:-local}" -v limit="$limit" -v module="$module" '
	{ sub(/\r$/, "") } # строка на Windows может кончаться CR
	($1 ~ /^(ok|FAIL)[ ]*$/) && $3 ~ /^[0-9.]+s$/ {
		p = $2
		if (module != "" && index(p, module "/") == 1) p = substr(p, length(module) + 2)
		t = int($3 + 0)
		if (!(p in sec) || t > sec[p]) sec[p] = t
	}
	END {
		summary = ENVIRON["SUMMARY"]
		n = 0
		for (p in sec) n++
		if (n == 0) {
			# Ничего не разобрано — это не «всё быстро»: сказать прямо.
			print "::warning::go test, " os ": время пакетов не разобрано — в выводе нет строк «ok/FAIL пакет время»"
			exit
		}
		head = "### Время пакетов go test — " os
		if (limit != "") head = head " (предел пакета " limit " с, сигнал от " int(limit / 2) " с)"
		print head "\n\n| Пакет | Секунд |\n|---|---|" >>summary
		for (k = 0; k < 5 && k < n; k++) {
			best = ""
			for (p in sec) if (!(p in done) && (best == "" || sec[p] > sec[best] || (sec[p] == sec[best] && p < best))) best = p
			done[best] = 1
			print "| " best " | " sec[best] " |" >>summary
		}
		print "" >>summary
		if (limit == "") exit
		for (p in sec) if (sec[p] > limit / 2) {
			print "**" p " — " sec[p] " с: больше половины предела пакета (" limit " с). Время растёт — снижать до отказа, а не после.**\n" >>summary
			print "::warning::go test, " os ": " p " — " sec[p] " с, больше половины предела пакета (" limit " с)"
		}
	}'

[ "$red" = 0 ] || exit 1
printf '%s' "$notes"
if [ -n "$retry" ]; then
	echo "retry=$retry" >>"${GITHUB_OUTPUT:-/dev/null}"
fi
exit 0
