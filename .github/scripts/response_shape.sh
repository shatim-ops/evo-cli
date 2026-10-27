#!/usr/bin/env bash
# Форма ответа сервиса Cloud.ru для публичного лога (E5, 1.4.0): статус,
# Content-Type, класс заголовка Server и первые 64 байта тела через
# redact.sed. URL (в нём проект) не печатается. Вызывает cloud-readonly.sh.

# response_shape URL ТОКЕН — «ok» или «warn», табуляция, подробности.
# ok — JSON, как у API сервиса; warn — другое: сверить с реестром
# расхождений (смена формы — поведение платформы, не дефект evo).
response_shape() {
  local dir hdr body code ct srv start
  dir=$(mktemp -d)
  hdr="$dir/h" body="$dir/b"
  code=$(curl -sS -o "$body" -D "$hdr" -w '%{http_code}' --max-time 30 -H "Authorization: Bearer $2" "$1" 2>/dev/null) || code=000
  ct=$(awk 'tolower($1)=="content-type:"{print tolower($2)}' "$hdr" 2>/dev/null | tr -d '\r;' | head -1)
  srv=$(awk 'tolower($1)=="server:"{print tolower($2)}' "$hdr" 2>/dev/null | tr -d '\r' | head -1)
  case "$srv" in envoy*) srv=envoy ;; nginx*) srv=nginx ;; istio*) srv=istio ;; "") srv=нет ;; *) srv=другой ;; esac
  start=$(head -c 64 "$body" 2>/dev/null | tr -d '\r\n' | sed -E -f "$(dirname "${BASH_SOURCE[0]}")/redact.sed")
  rm -rf "$dir"
  if [[ "$ct" == application/json* ]] && [ "$code" != 000 ]; then
    printf 'ok\tHTTP %s, %s, server %s, «%s»\n' "$code" "$ct" "$srv" "$start"
  else
    printf 'warn\tHTTP %s, %s, server %s, «%s» — сверить с реестром\n' "$code" "${ct:-без Content-Type}" "$srv" "$start"
  fi
}
