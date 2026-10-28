#!/usr/bin/env bash
# Проверка go_runtime_crash.sh (job ci): сигнатуры сбоя рантайма Go из
# случаев CI 22–28.10 узнаются и называют упавший пакет; падение теста,
# паника теста и стек через internal/config — не сбой рантайма.
set -uo pipefail
here=$(dirname "$0")
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
fail=0
tools=github.com/shatim-ops/evo-cli/internal/tools
check() { # ИМЯ ждём(crash|none) ждём-подстроку; лог — на stdin
	local got rc
	cat >"$dir/$1.log"
	got=$(bash "$here/go_runtime_crash.sh" "$dir/$1.log")
	rc=$?
	case "$2" in
	crash) [ "$rc" = 0 ] && [[ "$got" == *"$3"* ]] && [[ "$got" != *$'\r'* ]] && return ;;
	none) [ "$rc" = 1 ] && [ -z "$got" ] && return ;;
	esac
	printf 'FAIL %s: код %s, «%s»; ждём %s «%s»\n' "$1" "$rc" "$got" "$2" "$3"
	fail=1
}
printf 'ok  \tgithub.com/x/agentconf\t0.1s\r\nruntime: marked free object in span\r\nfatal error: found pointer to free object\r\n\r\ngoroutine 5 gp=0x1 m=3 [running]:\r\nruntime.bgsweep(0x0)\r\nFAIL\t%s\t21.3s\r\nFAIL\r\n' "$tools" |
	check free-object crash "found pointer to free object | $tools"
printf 'Exception 0xc0000005 0x0 0x8 0x7ff\r\nPC=0x7ff\r\nfatal error: unexpected signal during runtime execution\r\nruntime.adjustdefers(...)\r\nruntime.copystack(...)\r\nruntime.shrinkstack(...)\r\nFAIL\t%s\t30.0s\r\n' "$tools" |
	check signal crash "Exception 0xc0000005"
printf 'fatal error: checkmark found unmarked object\r\nruntime.runCheckmark()\r\nFAIL\t%s\t20.0s\r\n' "$tools" |
	check checkmark crash "checkmark found unmarked object | $tools"
printf -- '--- FAIL: TestSomething (0.01s)\n    x_test.go:10: want 1, got 2\nFAIL\n%s\t1.0s\n' "FAIL	$tools" |
	check test-fail none ""
printf 'panic: runtime error: index out of range [3] with length 3\n\ngoroutine 7 [running]:\nFAIL\t%s\t1.0s\n' "$tools" |
	check test-panic none ""
printf 'fatal error: unexpected signal during runtime execution\ngithub.com/shatim-ops/evo-cli/internal/config.privateACL(...)\n\tD:/a/evo-cli/internal/config/private_windows.go:42\nFAIL\tgithub.com/shatim-ops/evo-cli/internal/config\t1.0s\n' |
	check config-stack none ""
[ "$fail" = 0 ] && echo "go_runtime_crash: ok"
exit "$fail"
