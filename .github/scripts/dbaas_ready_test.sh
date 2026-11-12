#!/usr/bin/env bash
# Проверка dbaas-ready.sh (job ci) на поддельном evo: READY — 0; срок вышел
# — 1; FAILED — 1 и в логе причина: поля кластера с ошибкой и операция
# создания, без ID и адресов (run 203: кластер FAILED через секунды, причины
# в логе не было). Облака нет.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fail=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# shellcheck source-path=SCRIPTDIR source=fake_evo.sh
. "$here/fake_evo.sh"
fake_evo_install "$work/bin"
export PATH="$work/bin:$PATH" CLUSTER=evo-e2e-pg-r1a1 CLUSTER_ID=cluster-1 DBAAS_READY_PAUSE=0

# run_ ИМЯ СЕКУНДЫ — код в $rc, вывод в $FAKE_DIR/out
run_() {
	set +e
	"$here/dbaas-ready.sh" pg "$2" >"$FAKE_DIR/out" 2>&1
	rc=$?
	set -e
}
world() { export FAKE_DIR="$work/$1"; mkdir -p "$FAKE_DIR"; }
want() { [ "$2" = "$3" ] || { echo "FAIL $1: «$2», ждём «$3»"; sed 's/^/  | /' "$FAKE_DIR/out"; fail=1; }; }
has() { grep -q -- "$2" "$FAKE_DIR/out" || { echo "FAIL $1: нет строки «$2»"; sed 's/^/  | /' "$FAKE_DIR/out"; fail=1; }; }

world ready
fake_evo_answer pg_get 0 0 '{"status":"DEPLOYING"}'
fake_evo_answer pg_get 1 0 '{"status":"READY"}'
run_ ready 60
want "ready: код" "$rc" 0
has ready 'мин: DEPLOYING'
has ready 'мин: READY'

# NOT_READY — не READY: ждём дальше, срок выходит.
world slow
fake_evo_answer pg_get 0 0 '{"status":"NOT_READY"}'
run_ slow 0
want "slow: код" "$rc" 1
has slow 'не готов за 0 мин'

# FAILED: причина — в логе, операция читается по OP_ID. В перечень полей
# попадают только имена (admin_password — имя, не значение); значения ID,
# адреса и пароля в лог не попадают.
world failed
fake_evo_answer pg_get 0 0 '{"id":"11111111-2222-3333-4444-555555555555","name":"evo-e2e-pg-r1a1","status":"FAILED","status_reason":"quota exceeded on 10.0.0.5","host":"10.0.0.5","admin_password":"s3cret","error":{"message":"no capacity"}}'
fake_evo_answer pg_operation 0 0 '{"id":"11111111-2222-3333-4444-555555555555","status":"failed","done":true,"error":{"code":8,"message":"resource exhausted in 11111111-2222-3333-4444-555555555555"}}'
OP_ID=op-1 run_ failed 60
want "failed: код" "$rc" 1
has failed '::error::кластер evo-e2e-pg-r1a1: FAILED'
has failed 'кластер: .*status_reason.*quota exceeded on <ip>'
has failed 'кластер: .*no capacity'
has failed 'операция создания: .*resource exhausted in <id>'
has failed 'поля кластера: .*admin_password'
if grep -q 's3cret\|10\.0\.0\.5\|11111111-2222' "$FAKE_DIR/out"; then echo "FAIL failed: в логе пароль, адрес или ID"; sed 's/^/  | /' "$FAKE_DIR/out"; fail=1; fi
grep -q '^pg_operation ' "$FAKE_DIR/evo-calls" || { echo "FAIL failed: операция не читалась"; fail=1; }

# Секреты под «причинными» путями (условия «Риска» к #1503): поле берётся по
# своему имени, а не по пути; путь со словом из запретного списка не
# печатается; учётные данные в URI и «ключ=значение» в тексте маскируются;
# текст режется до 160 знаков.
world nested
long=$(printf 'x%.0s' $(seq 1 300))
fake_evo_answer pg_get 0 0 '{"status":"FAILED","status_details":{"credentials":{"password":"NestedPw-2","adminPassword":"NestedPw-3","state":"CredState-9"},"connection":{"uri":"postgres://dbadmin:UriPw-4@db.internal/x","message":"ConnMsg-8"},"access_key":"AccessKey-5","kubeconfig":"Kube-6"},"conditions":[{"type":"Ready","reason":"NoCapacity","message":"dial postgres://u:UriPw-7@node/db failed token=Tok-1 password=Pw-0 '"$long"'","secret_ref":"Ref-7"},{"reason":"auth secret: Colon-8 token: Tok-12 pwd=Pwd-13 passwd: Pw-14","message":"got {\"password\":\"JsonPw-7\"} with Authorization: Bearer eyJhbGci.Jwt-15 sent"}]}'
run_ nested 60
want "nested: код" "$rc" 1
has nested 'кластер: .*"status":"FAILED"'
has nested 'кластер: .*"conditions.0.reason":"NoCapacity"'
has nested 'кластер: .*://<cred>@'
has nested 'кластер: .*token=<secret>'
has nested 'кластер: .*password=<secret>'
if grep -q 'NestedPw\|UriPw\|AccessKey-5\|Kube-6\|Ref-7\|Tok-1\|Pw-0\|CredState-9\|ConnMsg-8\|Colon-8\|Tok-12\|Pwd-13\|Pw-14\|JsonPw-7\|Jwt-15\|eyJ' "$FAKE_DIR/out"; then echo "FAIL nested: секрет в логе"; sed 's/^/  | /' "$FAKE_DIR/out"; fail=1; fi
if grep -q 'x\{161\}' "$FAKE_DIR/out"; then echo "FAIL nested: текст длиннее 160 знаков"; fail=1; fi

# Нет OP_ID — операцию не читаем и говорим это; чтение кластера упало — тоже
# строка, а не молчание.
world failed_noop
fake_evo_answer pg_get 0 0 '{"status":"FAILED"}'
fake_evo_answer pg_get 1 5 '{"error":{"code":5,"reason":"API_ERROR","message":"boom"}}'
run_ failed_noop 60
want "failed_noop: код" "$rc" 1
has failed_noop 'операция создания: id операции нет'
has failed_noop 'кластер: не прочитан'
if grep -q '^pg_operation ' "$FAKE_DIR/evo-calls"; then echo "FAIL failed_noop: операция читалась без id"; fail=1; fi

set +e
"$here/dbaas-ready.sh" mysql 60 >/dev/null 2>&1
rc=$?
set -e
want "kind: код" "$rc" 2

[ "$fail" = 0 ] && echo "dbaas-ready: ok"
exit "$fail"
