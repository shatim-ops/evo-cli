#!/usr/bin/env bash
# Проверка live_forms_import.py: новая форма добавляется только формой
# (вид из KINDS, путь из имён и []), значения, хосты вне описи и поля сверх
# формы не проходят; запрос, который каталог знает, не меняется.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cat >"$tmp/cat.json" <<'J'
[{"id": "known", "request": "GET https://compute.api.cloud.ru/api/v1/disks/{disk_id}", "status": 200, "source": "s", "recorded": "2026-11-01", "paths": {"id": "uuid"}}]
J
cat >"$tmp/a.json" <<'J'
[
 {"id": "new", "request": "GET https://compute.api.cloud.ru/api/v1/backups/{backup_id}", "status": 200, "source": "run 1", "recorded": "2026-11-17",
  "paths": {"id": "uuid", "name": "web-01", "resource.size": "number", "tags.00000000-0000-4000-8000-000000000001": "name"}, "body": "secret"},
 {"id": "known2", "request": "GET https://compute.api.cloud.ru/api/v1/disks/{disk_id}", "status": 200, "paths": {"id": "uuid", "size": "number"}},
 {"id": "elsewhere", "request": "GET https://example.invalid/x", "status": 200, "paths": {"id": "uuid"}}
]
J
out=$(python3 -I "$here/live_forms_import.py" "$tmp/cat.json" "$tmp/a.json")
fail=0
ok() { echo "ok   $1"; }
bad() { echo "FAIL $1"; fail=1; }
got=$(jq -c '[.[] | .id]' "$tmp/cat.json")
if [ "$got" = '["known","new"]' ]; then ok "добавлена только новая форма описи"; else bad "строки каталога: $got"; fi
got=$(jq -c '.[] | select(.id == "new") | .paths' "$tmp/cat.json")
if [ "$got" = '{"id":"uuid","resource.size":"number"}' ]; then ok "значение и путь с id не прошли"; else bad "пути: $got"; fi
if jq -e '.[] | select(.id == "new") | has("body") | not' "$tmp/cat.json" >/dev/null; then ok "поля сверх формы не прошли"; else bad "поле body в каталоге"; fi
got=$(jq -c '.[] | select(.id == "known") | .paths' "$tmp/cat.json")
if [ "$got" = '{"id":"uuid"}' ]; then ok "известный запрос не изменён"; else bad "known: $got"; fi
if grep -q 'differs from the catalogue: known: 1 path' <<<"$out"; then ok "отличие названо"; else bad "вывод: $out"; fi
[ "$fail" = 0 ] && echo "live_forms_import: ok"
exit "$fail"
