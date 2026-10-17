#!/usr/bin/env bash
# Проверка redact.sed на строках вывода evo (preview.go, network.go, init.go).
set -euo pipefail
sed_file="$(dirname "$0")/redact.sed"
fail=0
check() {
  local got
  got=$(printf '%s\n' "$1" | sed -E -f "$sed_file")
  if [ "$got" != "$2" ]; then
    printf 'FAIL\n  in:   %s\n  got:  %s\n  want: %s\n' "$1" "$got" "$2"
    fail=1
  fi
}
check "  сеть:       подсеть Default_ru.AZ-2, группа безопасности SSH-access_ru.AZ-2" \
      "  сеть:       подсеть Default_ru.AZ-2, группа безопасности SSH-access_ru.AZ-2"
check "  сеть:       подсеть app-net, группа безопасности web-sg" \
      "  сеть:       подсеть <name>, группа безопасности <name>"
check "80 открываются для 1.2.3.4/0 в группе безопасности my-sg (она общая для всех ВМ в ней)" \
      "80 открываются для <ip>/0 в группе безопасности <name> (она общая для всех ВМ в ней)"
check "в зоне ru.AZ-2 несколько групп безопасности (a-sg, b-sg), группы по умолчанию среди них нет: взята a-sg, в ней deploy откроет порты compose" \
      "в зоне ru.AZ-2 несколько групп безопасности (<names>), группы по умолчанию среди них нет: взята <name>, в ней deploy откроет порты compose"
check "группа безопасности SSH-access_ru.AZ-1-web" "группа безопасности <name>"
check "Ошибка: в проекте нет группы безопасности для зоны ru.AZ-3" "Ошибка: в проекте нет группы безопасности для зоны ru.AZ-3"
check "в проекте нет подсети для зоны ru.AZ-3" "в проекте нет подсети для зоны ru.AZ-3"
check "группа безопасности общая: другие ВМ в ней" "группа безопасности общая: другие ВМ в ней"
check "ru.AZ-2 не предлагается первой: в ней нет подсети проекта; подсеть есть в ru.AZ-1" \
      "ru.AZ-2 не предлагается первой: в ней нет подсети проекта; подсеть есть в ru.AZ-1"
check "ВМ 0b5e9a6c-1111-2222-3333-444455556666 (10.0.0.5)" "ВМ <id> (<ip>)"
check "реестр 0b5e9a6c11112222333344445555666f готов" "реестр <id> готов"
check "docker push myreg.cr.cloud.ru/app:1" "docker push <registry>.cr.cloud.ru/app:1"
check "server: https://abc123.mk8s.ru-central-1.cloud.ru:6443" "server: https://<host>:6443"
check "коммит 60f3e13f22e913240461e8839d99f634f2e69651" "коммит 60f3e13f22e913240461e8839d99f634f2e69651"
# Имя в конце значения JSON: маска съедает и кавычку со скобкой, поэтому в
# cloud-e2e сначала jq, потом redact.sed и обрезка (ревью Grok по #88).
check '{"status":"error","error":"не найдена подсеть evo-e2e-net"}' '{"status":"error","error":"не найдена подсеть <name>'
[ "$fail" = 0 ] && echo "redact.sed: все образцы замаскированы верно"
exit "$fail"
