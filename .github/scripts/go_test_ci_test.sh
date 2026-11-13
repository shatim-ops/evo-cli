#!/usr/bin/env bash
# Проверка go_test_ci.sh (job ci) на поддельном go: на Windows пакеты — два
# прохода, internal/tools один после остальных (проба #494: 0 сбоев рантайма
# из 300 против 14 из 828); сбой рантайма Go без падения теста — пакеты к
# одному повтору (#370, вариант 1) и код 0; падение теста, сбой со стеком
# через internal/config, сбой без имени пакета — код 1 без повтора.
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0
m=github.com/shatim-ops/evo-cli
tools=$m/internal/tools

# Поддельный go: list ./... — три пакета, list ./internal/tools — один; test — N-й вызов печатает
# test.N и выходит с кодом test.N.rc (нет файла — ok и код 0). Аргументы
# каждого test — строка в calls.
mkdir -p "$work/bin"
cat >"$work/bin/go" <<'FAKE'
#!/usr/bin/env bash
d=$FAKE_DIR
case "$1" in
list)
	[ "$2" = ./internal/tools ] && { echo github.com/shatim-ops/evo-cli/internal/tools; exit 0; }
	printf '%s\n' github.com/shatim-ops/evo-cli/cmd/evo github.com/shatim-ops/evo-cli/internal/tools github.com/shatim-ops/evo-cli/internal/cli ;;
test)
	shift
	echo "$*" >>"$d/calls"
	n=$(wc -l <"$d/calls")
	if [ -f "$d/test.$n" ]; then cat "$d/test.$n"; exit "$(cat "$d/test.$n.rc")"; fi
	echo "ok  	pkg	0.1s"
	;;
esac
FAKE
chmod +x "$work/bin/go"

# case ИМЯ ОС КОД ПОВТОР ВЫЗОВ1 [ВЫЗОВ2]: код выхода, строка retry= в
# GITHUB_OUTPUT и аргументы вызовов go test.
case_() {
	local name=$1 os=$2 want=$3 retry=$4 rc got calls
	shift 4
	PATH="$work/bin:$PATH" RUNNER_OS=$os GITHUB_EVENT_NAME=${EVENT:-push} GITHUB_OUTPUT="$FAKE_DIR/output" \
		"$here/go_test_ci.sh" go test -race -count=1 -shuffle=off >"$FAKE_DIR/out" 2>&1
	rc=$?
	got=$(sed -n 's/^retry=//p' "$FAKE_DIR/output" 2>/dev/null)
	calls=$(cat "$FAKE_DIR/calls" 2>/dev/null)
	local want_calls
	want_calls=$(printf '%s\n' "$@")
	if [ "$rc" != "$want" ] || [ "$got" != "$retry" ] || [ "$calls" != "$want_calls" ]; then
		printf 'FAIL %s: код %s (ждём %s), retry «%s» (ждём «%s»)\n  вызовы:\n%s\n  ждём:\n%s\n' \
			"$name" "$rc" "$want" "$got" "$retry" "$calls" "$want_calls"
		sed 's/^/  | /' "$FAKE_DIR/out"
		fail=1
	fi
}
fresh() {
	export FAKE_DIR="$work/$1"
	mkdir -p "$FAKE_DIR"
}
answer() { # N КОД; вывод — stdin
	cat >"$FAKE_DIR/test.$1"
	echo "$2" >"$FAKE_DIR/test.$1.rc"
}
flags="-race -count=1 -shuffle=off"
rest="$flags $m/cmd/evo $m/internal/cli"
alone="$flags $tools"
crash=$(printf 'runtime: marked free object in span\nfatal error: found pointer to free object\n\ngoroutine 5 [running]:\nruntime.bgsweep(0x0)\nFAIL\t%s\t21.3s\nFAIL\n' "$tools")

fresh linux
case_ linux Linux 0 "" "$flags ./..."

fresh windows
case_ windows Windows 0 "" "$rest" "$alone"

# Pull request на Windows — только пакеты с кодом Windows; Linux — всё.
fresh windows-pr
EVENT=pull_request case_ windows-pr Windows 0 "" "$flags ./internal/cli ./internal/config ./internal/intent ./internal/journal"
fresh linux-pr
EVENT=pull_request case_ linux-pr Linux 0 "" "$flags ./..."
# PR трогает код путей internal/tools (classify.sh, wintools=yes): пакет —
# вторым проходом, один; на Linux и в push это ничего не меняет.
fresh windows-pr-tools
EVENT=pull_request WINDOWS_PR_TOOLS=yes case_ windows-pr-tools Windows 0 "" "$flags ./internal/cli ./internal/config ./internal/intent ./internal/journal" "$alone"
fresh windows-pr-no-tools
EVENT=pull_request WINDOWS_PR_TOOLS=no case_ windows-pr-no-tools Windows 0 "" "$flags ./internal/cli ./internal/config ./internal/intent ./internal/journal"
fresh linux-pr-tools
EVENT=pull_request WINDOWS_PR_TOOLS=yes case_ linux-pr-tools Linux 0 "" "$flags ./..."
fresh windows-push-tools
WINDOWS_PR_TOOLS=yes case_ windows-push-tools Windows 0 "" "$rest" "$alone"

# Сбой рантайма в internal/tools: код 0, tools — к повтору.
fresh crash
answer 2 1 <<<"$crash"
case_ crash Windows 0 "$tools" "$rest" "$alone"

# Сбой рантайма на Linux: тот же повтор.
fresh crash-linux
answer 1 1 <<<"$crash"
case_ crash-linux Linux 0 "$tools" "$flags ./..."

# Падение теста — не сбой: код 1, без повтора.
fresh test-fail
answer 1 1 <<<"$(printf -- '--- FAIL: TestX (0.01s)\nFAIL\nFAIL\t%s/internal/cli\t1.0s\n' "$m")"
case_ test-fail Windows 1 "" "$rest" "$alone"

# Сбой рантайма в tools и падение теста в другом пакете: повтор спрятал бы
# падение — код 1.
fresh crash-and-fail
answer 1 1 <<<"$(printf -- '--- FAIL: TestX (0.01s)\nFAIL\nFAIL\t%s/internal/cli\t1.0s\n' "$m")"
answer 2 1 <<<"$crash"
case_ crash-and-fail Windows 1 "" "$rest" "$alone"

# Стек через internal/config — дефект продукта, не повтор (#370).
fresh config-stack
answer 1 1 <<<"$(printf 'fatal error: unexpected signal during runtime execution\n%s/internal/config.privateACL(...)\n\tD:/a/evo-cli/internal/config/private_windows.go:42\nFAIL\t%s/internal/config\t1.0s\n' "$m" "$m")"
case_ config-stack Linux 1 "" "$flags ./..."

# Сбой без строки FAIL пакета: повторять нечего — код 1.
fresh no-package
answer 1 1 <<<"$(printf 'fatal error: found pointer to free object\nruntime.bgsweep(0x0)\n')"
case_ no-package Linux 1 "" "$flags ./..."

# Время пакетов — в сводке прогона (план 1.9, строка «CI», М3): самые
# долгие пакеты по ОС; пакет дольше половины предела -timeout — заметная
# строка и warning, job не красный. times_ ИМЯ ОС КОД ФЛАГИ…: код выхода;
# сводка — в $FAKE_DIR/summary, вывод — в $FAKE_DIR/out.
times_() {
	local name=$1 os=$2 want=$3 rc
	shift 3
	PATH="$work/bin:$PATH" RUNNER_OS=$os GITHUB_EVENT_NAME=push GITHUB_OUTPUT="$FAKE_DIR/output" GITHUB_STEP_SUMMARY="$FAKE_DIR/summary" \
		"$here/go_test_ci.sh" go test "$@" >"$FAKE_DIR/out" 2>&1
	rc=$?
	[ "$rc" = "$want" ] || { echo "FAIL $name: код $rc (ждём $want)"; sed 's/^/  | /' "$FAKE_DIR/out"; fail=1; }
}
has() { grep -q -- "$3" "$FAKE_DIR/$2" 2>/dev/null || { echo "FAIL $1: в $2 нет «$3»"; sed 's/^/  | /' "$FAKE_DIR/$2" 2>/dev/null; fail=1; }; }
hasnt() { if grep -q -- "$3" "$FAKE_DIR/$2" 2>/dev/null; then echo "FAIL $1: в $2 есть «$3»"; fail=1; fi; }
slow=$(printf 'ok  \t%s/internal/cli\t640.2s\nok  \t%s/cmd/evo\t3.1s\n?   \t%s/internal/x\t[no test files]\nok  \t%s/internal/tools\t216.0s\n' "$m" "$m" "$m" "$m")

# Пакет дольше половины предела (20m → сигнал от 600 с): строка и warning,
# код 0; в таблице — по убыванию, без пакетов без тестов.
fresh times-over
answer 1 0 <<<"$slow"
times_ times-over macOS 0 -race -timeout 20m
has times-over summary 'Время пакетов go test — macOS'
has times-over summary 'предел пакета 1200 с, сигнал от 600 с'
has times-over summary '^| internal/cli | 640 |'
has times-over summary 'internal/cli — 640 с: больше половины предела'
has times-over out '^::warning::go test, macOS: internal/cli — 640 с, больше половины предела пакета (1200 с)'
hasnt times-over summary 'internal/x'
[ "$(grep -c '^| [a-z]' "$FAKE_DIR/summary")" = 3 ] || { echo "FAIL times-over: строк таблицы не три"; cat "$FAKE_DIR/summary"; fail=1; }
[ "$(grep '^| internal' "$FAKE_DIR/summary" | head -1)" = '| internal/cli | 640 |' ] || { echo "FAIL times-over: первый в таблице — не самый долгий"; fail=1; }

# Все пакеты в пределах: таблица есть, сигнала нет. Предел в форме -timeout=30m.
fresh times-under
answer 1 0 <<<"$slow"
times_ times-under Linux 0 -race -timeout=30m
has times-under summary 'предел пакета 1800 с, сигнал от 900 с'
hasnt times-under summary 'больше половины'
hasnt times-under out '::warning::go test'

# Предел не задан: таблица без порога и без сигнала.
fresh times-no-limit
answer 1 0 <<<"$slow"
times_ times-no-limit Linux 0 -race
has times-no-limit summary 'Время пакетов go test — Linux'
hasnt times-no-limit summary 'сигнал'
hasnt times-no-limit out '::warning::go test'

# Предел задан, но в форме, которую скрипт не разбирает (составной):
# сигнал не пропадает молча — warning называет флаг, таблица без порога.
fresh times-odd-limit
answer 1 0 <<<"$slow"
times_ times-odd-limit Linux 0 -race -timeout 1h30m
has times-odd-limit out '^::warning::go test: предел -timeout «1h30m» не разобран'
has times-odd-limit summary 'Время пакетов go test — Linux'
hasnt times-odd-limit summary 'сигнал от'

# Путь сводки с обратными слэшами (Windows: D:\\a\\_temp\\…): awk -v
# разбирает в значении escape-последовательности и писал бы в другой файл
# (прогон ci PR #1571: «escape sequence \\_ treated as plain _»; в имени теста — \\t и \\n, их разбирает любой awk).
fresh times-backslash
answer 1 0 <<<"$slow"
PATH="$work/bin:$PATH" RUNNER_OS=Windows GITHUB_EVENT_NAME=pull_request GITHUB_OUTPUT="$FAKE_DIR/output" GITHUB_STEP_SUMMARY="$FAKE_DIR/a\\tmp\\nstep" \
	"$here/go_test_ci.sh" go test -race -timeout 20m >"$FAKE_DIR/out" 2>&1
[ -s "$FAKE_DIR/a\\tmp\\nstep" ] || { echo "FAIL times-backslash: сводка не в файле с обратными слэшами в имени"; ls "$FAKE_DIR"; fail=1; }
hasnt times-backslash out 'escape sequence'

# Строки go test с CR в конце (Windows) разбираются так же.
fresh times-cr
answer 1 0 <<<"$(printf 'ok  \t%s/internal/cli\t640.2s\r\nok  \t%s/cmd/evo\t3.1s\r\n' "$m" "$m")"
times_ times-cr Windows 0 -race -timeout 20m
has times-cr summary '^| internal/cli | 640 |'
has times-cr out '^::warning::go test, Windows: internal/cli — 640 с'

# Ни одной строки времени не разобрано: не тишина, а warning — пустая
# сводка не должна читаться как «всё быстро».
fresh times-none
answer 1 0 <<<"PASS"
times_ times-none Linux 0 -race -timeout 20m
has times-none out '^::warning::go test, Linux: время пакетов не разобрано'

# Ровно половина предела — ещё не сигнал.
fresh times-edge
answer 1 0 <<<"$(printf 'ok  \t%s/internal/cli\t600.0s\n' "$m")"
times_ times-edge Linux 0 -race -timeout 20m
hasnt times-edge out '::warning::go test'

# Упавший пакет тоже в таблице (FAIL с временем); код красный, как был.
fresh times-fail
answer 1 1 <<<"$(printf -- '--- FAIL: TestX (0.01s)\nFAIL\nFAIL\t%s/internal/cli\t700.0s\n' "$m")"
times_ times-fail Linux 1 -race -timeout 20m
has times-fail summary '^| internal/cli | 700 |'

# Два прохода Windows — одна таблица на оба.
fresh times-windows
answer 1 0 <<<"$(printf 'ok  \t%s/internal/cli\t100.0s\n' "$m")"
answer 2 0 <<<"$(printf 'ok  \t%s/internal/tools\t300.0s\n' "$m")"
times_ times-windows Windows 0 -race -timeout 20m
[ "$(grep -c 'Время пакетов go test' "$FAKE_DIR/summary")" = 1 ] || { echo "FAIL times-windows: таблица не одна"; fail=1; }
[ "$(grep '^| internal' "$FAKE_DIR/summary" | head -1)" = '| internal/tools | 300 |' ] || { echo "FAIL times-windows: порядок"; cat "$FAKE_DIR/summary"; fail=1; }

[ "$fail" = 0 ] && echo "go_test_ci: ok"
exit "$fail"
