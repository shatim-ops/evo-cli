#!/usr/bin/env bash
# Храповик размера tools/list (решения владельца 29.10, #597, п. 12, и
# 04.11): рост tools/list головы PR против базы — только с причиной (D68,
# P13). Число считает эта проверка и пишет в summary прогона; человек
# пишет в описании PR только причину: строку «tools/list: рост — <причина>».
# Ошибались в числе и разметке, а не в причине. Уменьшение и ноль ничего не
# требуют. Потолки (maxToolsListBytes и пределы описаний) — не здесь:
# их держат тесты internal/mcp, которые tools-list.yml гоняет на голове.
#
#   BODY=<описание PR> tools_list_growth.sh <base.json> <head.json>
set -euo pipefail

base=$(wc -c <"$1")
head=$(wc -c <"$2")
growth=$((head - base))
sign=''
[ "$growth" -le 0 ] || sign=+
line="tools/list: база $base байт, голова $head байт ($sign$growth байт)"
echo "$line"
[ -z "${GITHUB_STEP_SUMMARY:-}" ] || echo "$line" >>"$GITHUB_STEP_SUMMARY"
[ "$growth" -le 0 ] && exit 0

want="tools/list: рост — "
# Причина — не отписка: не короче 10 знаков после тире.
while IFS= read -r l; do
	reason=${l#*"$want"}
	reason=${reason#"${reason%%[![:space:]]*}"}
	if [ "${#reason}" -ge 10 ]; then
		echo "описание PR объясняет рост: ${reason:0:200}"
		exit 0
	fi
done < <(grep -F -- "$want" <<<"${BODY:-}" || true)
echo "::error::tools/list вырос на $growth байт: добавьте в описание PR строку «${want}<причина, не короче 10 знаков>» (число писать не нужно — его считает проверка) — или сократите описания"
exit 1
