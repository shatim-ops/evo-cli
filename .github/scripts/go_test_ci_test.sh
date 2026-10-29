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
EVENT=pull_request case_ windows-pr Windows 0 "" "$flags ./internal/cli ./internal/config ./internal/journal"
fresh linux-pr
EVENT=pull_request case_ linux-pr Linux 0 "" "$flags ./..."

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

[ "$fail" = 0 ] && echo "go_test_ci: ok"
exit "$fail"
