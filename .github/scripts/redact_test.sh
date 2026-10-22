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
# SEC-6: IPv6. Маскируется адрес с «::» или из трёх групп и больше с буквой
# a-f; время и таймстемпы GitHub остаются.
check "адрес 2001:db8::1 открыт" "адрес <ipv6> открыт"
check "listen [::1]:8080" "listen [<ipv6>]:8080"
check "fe80::a1b2:3c4d%eth0 up" "<ipv6>%eth0 up"
check "подсеть 2001:db8::/32" "подсеть <name>"
check "сеть 2001:db8::/32 и 2001:db8:abcd::/48" "сеть <ipv6>/32 и <ipv6>/48"
check "ВМ 2001:db8:0:0:1:2:3:4 и 2001:0db8:85a3:0000:0000:8a2e:0370:7334" "ВМ <ipv6> и <ipv6>"
check "dial tcp [2001:db8:1:2::5]:443: i/o timeout" "dial tcp [<ipv6>]:443: i/o timeout"
check "21:07:13 шаг 3 из 5" "21:07:13 шаг 3 из 5"
check "2026-10-19T21:07:13.4332220Z ##[group]Run evo deploy" "2026-10-19T21:07:13.4332220Z ##[group]Run evo deploy"
check "длилось 01:02:03, до 12:30" "длилось 01:02:03, до 12:30"
check "std::string, Foo::Bar" "std::string, Foo::Bar"
# Две группы или группы без букв a-f — не IPv6: «ab:cd», «1:2:3», порт.
check "метка ab:cd, версия 1:2:3, порт 10:22" "метка ab:cd, версия 1:2:3, порт 10:22"
check "::notice title=x::готово" "::notice title=x::готово"
# 32-значный ID, приклеенный через «_», и UUID с хвостом.
check "registry reg_0b5e9a6c11112222333344445555666f created" "registry reg_<id> created"
check "файл 0b5e9a6c11112222333344445555666f_key.pem" "файл <id>_key.pem"
check "x_0b5e9a6c11112222333344445555666f_y" "x_<id>_y"
check "диск 0b5e9a6c-1111-2222-3333-444455556666abc и disk_0b5e9a6c-1111-2222-3333-444455556666_boot" \
      "диск <id> и disk_<id>_boot"
# Поддомены cloud.ru — имена ресурсов; публичные хосты API и входа остаются.
check "GET https://evo-e2e-bucket.s3.cloud.ru/key" "GET https://<bucket>.s3.cloud.ru/key"
check "endpoint https://s3.cloud.ru, region ru-central-1" "endpoint https://s3.cloud.ru, region ru-central-1"
check "app https://my-app-1a2b.containers.cloud.ru готово" "app https://<host>.cloud.ru готово"
check "db pg-123.private.cloud.ru:5432" "db <host>.cloud.ru:5432"
check "POST https://compute.api.cloud.ru/api/v1/vms и https://foundation-models.api.cloud.ru/v1" \
      "POST https://compute.api.cloud.ru/api/v1/vms и https://foundation-models.api.cloud.ru/v1"
check "токен https://iam.api.cloud.ru/api/v1/auth/token, https://id.cloud.ru/auth, https://auth.iam.cloud.ru/auth/system/openid/token" \
      "токен https://iam.api.cloud.ru/api/v1/auth/token, https://id.cloud.ru/auth, https://auth.iam.cloud.ru/auth/system/openid/token"
check "ключ — https://console.cloud.ru, документация https://cloud.ru/docs" "ключ — https://console.cloud.ru, документация https://cloud.ru/docs"
check "хост x.compute.api.cloud.ru" "хост <host>.cloud.ru"
# Break r4 (R4-13): полная форма IPv6 без букв, хосты в другом регистре,
# «_» в имени бакета, hex-токены длиннее 32 цифр.
check "addr 2001:0:0:0:0:0:0:1 end" "addr <ipv6> end"
check "dial tcp [2001:0:0:0:0:0:0:1]:443" "dial tcp [<ipv6>]:443"
check "app MyApp.Containers.Cloud.RU ready" "app <host>.cloud.ru ready"
check "bucket my_bucket.s3.cloud.ru" "bucket <bucket>.s3.cloud.ru"
check "GET https://My-Bucket.S3.Cloud.Ru/key" "GET https://<bucket>.s3.cloud.ru/key"
check "docker push MyReg.CR.cloud.ru/app:1" "docker push <registry>.cr.cloud.ru/app:1"
check "host evil_console.cloud.ru и x_s3.cloud.ru" "host <host>.cloud.ru и <host>.cloud.ru"
check "token 0000000000000000000000000000000000000000000000000000000000000000" "token <id>"
check "Bearer 0b5e9a6c11112222333344445555666f0b5e9a6c11112222333344445555666f end" "Bearer <id> end"
check "ключ 0b5e9a6c11112222333344445555666f0 и key_0b5e9a6c11112222333344445555666f0b5e9a6c11112222333344445555666f" \
      "ключ <id> и key_<id>"
# Не задевается: хэш коммита (40 цифр), короткий хэш, версия, время.
check "сборка 60f3e13 из 60f3e13f22e913240461e8839d99f634f2e69651, evo 1.4.2, 21:07:13" \
      "сборка 60f3e13 из 60f3e13f22e913240461e8839d99f634f2e69651, evo 1.4.2, 21:07:13"
check "2026-10-19T21:07:13.4332220Z 0b5e9a6c111122223333444455556" "2026-10-19T21:07:13.4332220Z 0b5e9a6c111122223333444455556"
# R4-14: публичный хост в другом регистре остаётся.
check "via Console.cloud.ru ok" "via Console.cloud.ru ok"
check "via S3.cloud.ru ok" "via S3.cloud.ru ok"
check "via IAM.API.cloud.ru ok" "via IAM.API.cloud.ru ok"
check "via Auth.IAM.cloud.ru ok, https://ID.Cloud.RU/auth" "via Auth.IAM.cloud.ru ok, https://ID.Cloud.RU/auth"
# Имя в конце значения JSON: маска съедает и кавычку со скобкой, поэтому в
# cloud-e2e сначала jq, потом redact.sed и обрезка (ревью Grok по #88).
check '{"status":"error","error":"не найдена подсеть evo-e2e-net"}' '{"status":"error","error":"не найдена подсеть <name>'
# FB122-1 (дельта-аудит Fable 1.2.2): пароль ВМ в конверте ошибки --json
# (details.password, с 1.2.2) и строкой «Пароль ВМ: …» после ошибки — в
# публичный лог не попадает; соседние поля и текст остаются.
check '    "password": "aB3#x-Yz9!kq",' '    "password":"<password>",'
check '{"error":{"code":5,"details":{"vm_id":"web","password":"aB3#x-Yz9!kq"}}}' '{"error":{"code":5,"details":{"vm_id":"web","password":"<password>"}}}'
check '{"admin_password":"p\"q","x":1}' '{"admin_password":"<password>","x":1}'
check "Пароль ВМ: aB3#x-Yz9!kq (записан только здесь — сохраните: evo его не хранит)" \
      "Пароль ВМ: <password> (записан только здесь — сохраните: evo его не хранит)"
check "VM password: aB3#x-Yz9!kq (shown only here — save it: evo keeps no copy)" \
      "VM password: <password> (shown only here — save it: evo keeps no copy)"
check "password_required: true, пароль задаётся --password-file" "password_required: true, пароль задаётся --password-file"
[ "$fail" = 0 ] && echo "redact.sed: все образцы замаскированы верно"
exit "$fail"
