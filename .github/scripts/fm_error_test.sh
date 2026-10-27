#!/usr/bin/env bash
# Проверка fm_error.sh (job ci): на 401, 403 и 404 Foundation Models текст
# отказа — одна строка без тела ответа (E7), на прочих — сообщение API.
set -euo pipefail
# shellcheck source-path=SCRIPTDIR source=fm_error.sh
. "$(dirname "$0")/fm_error.sh"
fail=0
resp=$(mktemp)
trap 'rm -f "$resp"' EXIT
# Тело шлюза с тем, чего в публичном комментарии быть не должно.
leak='{"error":{"message":"denied by gw-internal.example at 10.1.2.3"}}'
check() { # КОД ТЕЛО ждём-подстроку не-ждём-подстроку
  local got
  printf '%s' "$2" >"$resp"
  got=$(fm_error "$1" "$resp")
  if [[ "$got" != *"$3"* ]] || { [ -n "$4" ] && [[ "$got" == *"$4"* ]]; } || [[ "$got" == *$'\n'* ]]; then
    printf 'FAIL %s\n  got:  %s\n  want: «%s», без «%s», одной строкой\n' "$1" "$got" "$3" "$4"
    fail=1
  fi
}
check 401 "$leak" 'CLOUDRU_FM_API_KEY' 'gw-internal'
check 403 "$leak" 'не принят этим сервисом' '10.1.2.3'
check 404 "$leak" 'FM_BASE_URL' 'gw-internal'
check 404 '<html>404 Not Found</html>' 'эндпоинт сменился' 'html'
# Прочие коды — сообщение API, как раньше.
check 400 '{"error":{"message":"model is not found"}}' 'model is not found' ''
check 500 '{"detail":"overloaded"}' 'ответил 500: overloaded' ''
check 502 'not json' 'без описания' ''
[ "$fail" = 0 ] && echo "fm_error: ok"
exit "$fail"
