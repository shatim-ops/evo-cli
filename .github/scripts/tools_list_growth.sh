#!/usr/bin/env bash
# Храповик размера tools/list (решение владельца 29.10, #597, п. 12): в коде
# — только потолок (maxToolsListBytes, internal/mcp/limits_test.go); рост
# tools/list головы PR против базы требует в описании PR строку
# «tools/list: +N байт, потому что …» с точным N — список не растёт
# попутно (P7). Уменьшение строки не требует.
#
#   BODY=<описание PR> tools_list_growth.sh <base.json> <head.json>
set -euo pipefail

base=$(wc -c <"$1")
head=$(wc -c <"$2")
growth=$((head - base))
echo "tools/list: база $base байт, голова $head байт ($( ((growth > 0)) && echo +)$growth)"
[ "$growth" -le 0 ] && exit 0

want="tools/list: +$growth байт, потому что "
if grep -qF -- "$want" <<<"${BODY:-}" &&
	grep -F -- "$want" <<<"${BODY:-}" | grep -qE "потому что +[^ ]"; then
	echo "описание PR объясняет рост"
	exit 0
fi
echo "::error::tools/list вырос на $growth байт: добавьте в описание PR строку «${want}<причина>» — или сократите описания"
exit 1
