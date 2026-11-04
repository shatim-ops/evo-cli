#!/usr/bin/env bash
# Проверка json_shape.sh (api-probe P50): форма JSON без значений чисел и
# длинных строк; тело не JSON — одна строка «не JSON», даже если оно
# начинается с числа (страница «404 page not found» печатала "число" и
# вслед запасную строку, run 41781117439).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source-path=SCRIPTDIR source=json_shape.sh
. "$here/json_shape.sh"
fail=0
check() { # имя, тело, ожидаемый вывод
  local got
  got=$(printf '%s' "$2" | json_shape)
  if [ "$got" != "$3" ]; then
    printf 'FAIL %s:\n  got:  %s\n  want: %s\n' "$1" "$got" "$3"
    fail=1
  fi
}
check router-404 '404 page not found' 'не JSON: 404 page not found'
check html '<html>gateway</html>' 'не JSON: <html>gateway</html>'
check empty '' 'не JSON: '
check null 'null' '"null"'
check number '12.5' '"число"'
check object '{"product":"mk8s","price":1.25,"note":"цена за час"}' '{"product":"mk8s","price":"число","note":"строка"}'
check array '{"items":[{"unit":"hour","v":3},{"unit":"x"}],"e":[]}' '{"items":[2,{"unit":"hour","v":"число"}],"e":[]}'
check long-body "$(printf 'x%.0s' $(seq 1 200))" "не JSON: $(printf 'x%.0s' $(seq 1 80))"
check two-docs '1 2' 'не JSON: 1 2'
[ "$fail" = 0 ] && echo "json_shape: ok"
exit "$fail"
