#!/usr/bin/env bash
# Проверка tools_list_growth.sh на образцах: рост без строки, со строкой
# причины, без причины, уменьшение; число роста — в summary прогона.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
printf '[{"name":"a"}]' >"$dir/base.json"
printf '[{"name":"a"},{"name":"b"}]' >"$dir/head.json"
grow=$(($(wc -c <"$dir/head.json") - $(wc -c <"$dir/base.json")))
fail=0
check() { # имя, ожидаемый код, описание PR, база, голова
	local got=0 out
	out=$(BODY=$3 "$here/tools_list_growth.sh" "$4" "$5" 2>&1) || got=$?
	if [ "$got" = "$2" ]; then echo "ok   $1"; else
		echo "FAIL $1: код $got, ждали $2"; printf '%s\n' "$out" | sed 's/^/  /'; fail=1
	fi
}
check "рост без строки" 1 "Правка описаний." "$dir/base.json" "$dir/head.json"
check "рост со строкой" 0 "Текст.
tools/list: рост — новый инструмент b" "$dir/base.json" "$dir/head.json"
check "без причины" 1 "tools/list: рост — " "$dir/base.json" "$dir/head.json"
check "отписка" 1 "tools/list: рост — ." "$dir/base.json" "$dir/head.json"
check "старая форма с числом — не та строка" 1 "tools/list: +$grow байт, потому что новый инструмент b" "$dir/base.json" "$dir/head.json"
check "спецсимволы в описании" 0 "\$(id) \`x\` [a-z]* \\n
tools/list: рост — новый инструмент b, \$HOME" "$dir/base.json" "$dir/head.json"
check "уменьшение" 0 "" "$dir/head.json" "$dir/base.json"
check "без изменений" 0 "" "$dir/base.json" "$dir/base.json"
# Число считает проверка: оно в summary прогона, человеку его писать не нужно.
sum=$(mktemp)
BODY="tools/list: рост — новый инструмент b" GITHUB_STEP_SUMMARY=$sum "$here/tools_list_growth.sh" "$dir/base.json" "$dir/head.json" >/dev/null
if grep -qF -- "+$grow байт" "$sum"; then echo "ok   число роста — в summary"; else echo "FAIL число роста не в summary: $(cat "$sum")"; fail=1; fi
rm -f "$sum"
exit $fail
