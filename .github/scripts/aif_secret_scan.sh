#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
# SPDX-FileContributor: Тимур Шакирбеков
# SPDX-License-Identifier: Apache-2.0
#
# ai-factory-probe: отчёт до push в evo-internal сверяется со значениями
# всех секретов окружения, а не с шаблоном (условие владельца 04.11).
#
#   aif_secret_scan.sh КАТАЛОГ ИМЯ...
#
# ИМЯ — переменная окружения с секретом. Значение короче 6 знаков не
# сверяется (совпадёт с любым текстом), такая переменная называется
# предупреждением. Нашлось значение — код 1 и имена переменных, без
# значений и без файлов: в публичный лог не уходит ни секрет, ни его
# место. Пустой каталог — тоже ошибка: сверять нечего, push бессмыслен.
set -uo pipefail
dir=${1:?каталог отчёта}
shift
[ -d "$dir" ] && [ -n "$(find "$dir" -type f -print -quit)" ] ||
	{ echo "::error::отчёт пуст: $dir"; exit 1; }
found=()
for name in "$@"; do
	v=${!name:-}
	[ -n "$v" ] || continue
	if [ "${#v}" -lt 6 ]; then
		echo "::warning::$name короче 6 знаков — не сверяется"
		continue
	fi
	if grep -rqF -- "$v" "$dir"; then
		found+=("$name")
	fi
done
if [ "${#found[@]}" -gt 0 ]; then
	echo "::error::в отчёте значения секретов: ${found[*]} — push отменён"
	exit 1
fi
echo "секретов в отчёте нет: сверено переменных $#"
