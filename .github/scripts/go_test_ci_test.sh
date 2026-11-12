#!/usr/bin/env bash
# Проверка go_test_ci.sh (job ci) на поддельном go: на Windows пакеты — два
# прохода, internal/tools один после остальных (проба #494: 0 сбоев рантайма
# из 300 против 14 из 828); сбой рантайма Go без падения теста — пакеты к
# одному повтору (#370, вариант 1) и код 0; падение теста, сбой со стеком
# через internal/config, сбой без имени пакета — код 1 без повтора. Не на
# Windows internal/cli идёт двумя процессами рядом с остальными пакетами
# (мера М6 строки «CI» плана 1.9): части — из одного списка тестов, пустая
# часть — код 1 до запуска, список не собрался — один проход по всему.
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0
m=github.com/shatim-ops/evo-cli
tools=$m/internal/tools

# Поддельный go: list ./... — три пакета, list ./internal/tools и
# ./internal/cli — по одному; test -list — имена из файла tests (нет файла —
# четыре имени), код list.rc; test — проход N (GO_TEST_CI_PASS) печатает
# test.N и выходит с кодом test.N.rc (нет файла — ok и код 0). Аргументы
# прохода N — файл call.N: проходы не на Windows идут разом.
mkdir -p "$work/bin"
cat >"$work/bin/go" <<'FAKE'
#!/usr/bin/env bash
d=$FAKE_DIR
case "$1" in
list)
	[ "$2" = ./internal/tools ] && { echo github.com/shatim-ops/evo-cli/internal/tools; exit 0; }
	[ "$2" = ./internal/cli ] && { echo github.com/shatim-ops/evo-cli/internal/cli; exit 0; }
	printf '%s\n' github.com/shatim-ops/evo-cli/cmd/evo github.com/shatim-ops/evo-cli/internal/tools github.com/shatim-ops/evo-cli/internal/cli ;;
test)
	shift
	case " $* " in *" -list "*)
		if [ -f "$d/tests" ]; then cat "$d/tests"; else printf '%s\n' TestParityA TestOther ExampleB TestConsentA; fi
		echo "ok  	pkg	0.1s"
		exit "$(cat "$d/list.rc" 2>/dev/null || echo 0)" ;;
	esac
	n=$GO_TEST_CI_PASS
	echo "$*" >"$d/call.$n"
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
	calls=$(cat "$FAKE_DIR"/call.* 2>/dev/null)
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
# Не на Windows: остальные пакеты и две части internal/cli.
others="$flags $m/cmd/evo $tools"
half1="$flags -run ^(TestConsentA|TestParityA)\$ $m/internal/cli"
half2="$flags -run ^(ExampleB|TestOther)\$ $m/internal/cli"
crash=$(printf 'runtime: marked free object in span\nfatal error: found pointer to free object\n\ngoroutine 5 [running]:\nruntime.bgsweep(0x0)\nFAIL\t%s\t21.3s\nFAIL\n' "$tools")

fresh linux
case_ linux Linux 0 "" "$others" "$half1" "$half2"
fresh macos
case_ macos macOS 0 "" "$others" "$half1" "$half2"

# Одна часть пуста (все тесты пакета — из группы первой части): части не
# равны пакету — код 1, ни одного прохода.
fresh one-half-empty
printf '%s\n' TestParityA TestConsentA >"$FAKE_DIR/tests"
case_ one-half-empty Linux 1 ""

# Список тестов не собрался (пакет не компилируется): один проход по всему.
fresh no-list
echo 1 >"$FAKE_DIR/list.rc"
case_ no-list Linux 0 "" "$flags ./..."

# Сбой рантайма во второй части internal/cli: пакет — к повтору целиком.
fresh crash-cli-half
answer 3 1 <<<"$(printf 'fatal error: found pointer to free object\n\ngoroutine 5 [running]:\nruntime.bgsweep(0x0)\nFAIL\t%s/internal/cli\t21.3s\nFAIL\n' "$m")"
case_ crash-cli-half Linux 0 "$m/internal/cli" "$others" "$half1" "$half2"

# Падение теста в первой части internal/cli — код 1.
fresh fail-cli-half
answer 2 1 <<<"$(printf -- '--- FAIL: TestParityA (0.01s)\nFAIL\nFAIL\t%s/internal/cli\t1.0s\n' "$m")"
case_ fail-cli-half Linux 1 "" "$others" "$half1" "$half2"

fresh windows
case_ windows Windows 0 "" "$rest" "$alone"

# Pull request на Windows — только пакеты с кодом Windows; Linux — всё.
fresh windows-pr
EVENT=pull_request case_ windows-pr Windows 0 "" "$flags ./internal/cli ./internal/config ./internal/journal"
fresh linux-pr
EVENT=pull_request case_ linux-pr Linux 0 "" "$others" "$half1" "$half2"
# PR трогает код путей internal/tools (classify.sh, wintools=yes): пакет —
# вторым проходом, один; на Linux и в push это ничего не меняет.
fresh windows-pr-tools
EVENT=pull_request WINDOWS_PR_TOOLS=yes case_ windows-pr-tools Windows 0 "" "$flags ./internal/cli ./internal/config ./internal/journal" "$alone"
fresh windows-pr-no-tools
EVENT=pull_request WINDOWS_PR_TOOLS=no case_ windows-pr-no-tools Windows 0 "" "$flags ./internal/cli ./internal/config ./internal/journal"
fresh linux-pr-tools
EVENT=pull_request WINDOWS_PR_TOOLS=yes case_ linux-pr-tools Linux 0 "" "$others" "$half1" "$half2"
fresh windows-push-tools
WINDOWS_PR_TOOLS=yes case_ windows-push-tools Windows 0 "" "$rest" "$alone"

# Сбой рантайма в internal/tools: код 0, tools — к повтору.
fresh crash
answer 2 1 <<<"$crash"
case_ crash Windows 0 "$tools" "$rest" "$alone"

# Сбой рантайма на Linux: тот же повтор.
fresh crash-linux
answer 1 1 <<<"$crash"
case_ crash-linux Linux 0 "$tools" "$others" "$half1" "$half2"

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
case_ config-stack Linux 1 "" "$others" "$half1" "$half2"

# Сбой без строки FAIL пакета: повторять нечего — код 1.
fresh no-package
answer 1 1 <<<"$(printf 'fatal error: found pointer to free object\nruntime.bgsweep(0x0)\n')"
case_ no-package Linux 1 "" "$others" "$half1" "$half2"

[ "$fail" = 0 ] && echo "go_test_ci: ok"
exit "$fail"
