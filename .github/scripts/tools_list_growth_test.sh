#!/usr/bin/env bash
# Проверка tools_list_growth.sh на образцах: рост без строки, со строкой,
# с неверным числом, без причины, уменьшение.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
printf '[{"name":"a"}]' >"$dir/base.json"
printf '[{"name":"a"},{"name":"b"}]' >"$dir/head.json"
grow=$(($(wc -c <"$dir/head.json") - $(wc -c <"$dir/base.json")))
fail=0
check() { # имя, ожидаемый код, описание PR, база, голова
	local got=0
	BODY=$3 "$here/tools_list_growth.sh" "$4" "$5" >/dev/null 2>&1 || got=$?
	if [ "$got" = "$2" ]; then echo "ok   $1"; else echo "FAIL $1: код $got, ждали $2"; fail=1; fi
}
check "рост без строки" 1 "Правка описаний." "$dir/base.json" "$dir/head.json"
check "рост со строкой" 0 "Текст.
tools/list: +$grow байт, потому что новый инструмент b" "$dir/base.json" "$dir/head.json"
check "неверное число" 1 "tools/list: +1 байт, потому что b" "$dir/base.json" "$dir/head.json"
check "без причины" 1 "tools/list: +$grow байт, потому что " "$dir/base.json" "$dir/head.json"
check "уменьшение" 0 "" "$dir/head.json" "$dir/base.json"
check "без изменений" 0 "" "$dir/base.json" "$dir/base.json"
exit $fail
