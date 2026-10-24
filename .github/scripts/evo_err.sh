#!/usr/bin/env bash
# evo_err.sh ФАЙЛ — причина и текст ошибки evo из сохранённого stderr вызова
# с --json: ошибка инструмента — {"error":…} в stderr, stdout пуст (QA-12).
# Шаги cloud-e2e читали .error из stdout и выбрасывали stderr — причина
# терялась (pg_create, run 40118457142). Нет JSON ошибки — начало stderr.
# Всё через redact.sed (лог публичный) и до 300 символов: маска до обрезки,
# обрезанный ID она не узнаёт.
set -euo pipefail
f=${1:?нужен файл stderr}
here=$(cd "$(dirname "$0")" && pwd)
line=$(jq -rR 'fromjson? | .error? | select(type == "object") | "\(.reason // "-"): \(.message // "")"' "$f" 2>/dev/null | sed -n 1p) || line=
[ -n "$line" ] || line=$(head -c 4000 "$f" 2>/dev/null | tr '\n\r' '  ') || line=
printf '%s\n' "$line" | sed -E -f "$here/redact.sed" | cut -c1-300
