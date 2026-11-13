#!/usr/bin/env bash
# Проверка области ops cloud-live.sh (пробы графика 1.9, вход probes) на
# поддельном evo и заглушке curl, без облака: проба шлёт запросы по описи
# docs/internal/probes/requests-1.9.md в заданном порядке, убирает за собой
# и при отказе платформы не идёт дальше; уборка области находит и удаляет
# вид ресурса пробы (ось 3: проба без уборки своего вида не запускается).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir "$tmp/bin"
# shellcheck source-path=SCRIPTDIR source=fake_evo.sh
. "$here/fake_evo.sh"
fake_evo_install "$tmp/bin"
# Заглушка curl: «МЕТОД URL» — в $FAKE_DIR/calls; ответ — по первой строке
# $FAKE_DIR/http, чей образец (МЕТОД и окончание пути, glob) подходит:
# «МЕТОД ОБРАЗЕЦ КОД [ТЕЛО]»; «МЕТОД!» — строка на один запрос (следующий
# такой же запрос получит следующую подходящую строку: состояния по
# очереди; код 000 — обрыв без ответа; ТЕЛО «-» — пустое тело). Нет подходящей — так же по $FAKE_DIR/http.default (пустые
# списки копий и планов для уборки области); нет и там — 404 с телом {}.
# Заголовки ответа — строки $FAKE_DIR/http.h «МЕТОД ОБРАЗЕЦ ЗАГОЛОВОК», чей
# образец подходит (все такие, по порядку).
cat >"$tmp/bin/curl" <<'FAKE'
#!/usr/bin/env bash
out=/dev/stdout url= method=GET hdr=
while [ $# -gt 0 ]; do
	case $1 in
	-o) out=$2; shift ;; -D) hdr=$2; shift ;; -X) method=$2; shift ;; -w | -H | -m) shift ;;
	@-) cat >"$FAKE_DIR/body" ;; https://*) url=$1 ;;
	esac
	shift
done
# Тело запроса — ещё и в body.<МЕТОД>.<путь через точки> (последнее).
p=${url#https://compute.api.cloud.ru/api/v1/}
[ ! -s "$FAKE_DIR/body" ] || cp "$FAKE_DIR/body" "$FAKE_DIR/body.$method.$(printf '%s' "${p%%\?*}" | tr / .)"
[ ! -s "$FAKE_DIR/body" ] || { cat "$FAKE_DIR/body"; echo; } >>"$FAKE_DIR/bodies"
! grep -q remote_security_group_id "$FAKE_DIR/body" 2>/dev/null || cp "$FAKE_DIR/body" "$FAKE_DIR/body.$method.$(printf '%s' "${p%%\?*}" | tr / .).group"
: >"$FAKE_DIR/body"
case "$url" in */auth/token) printf '{"access_token":"tok"}'; exit 0 ;; esac
echo "$method ${url#https://compute.api.cloud.ru/api/v1}" >>"$FAKE_DIR/calls"
if [ -n "$hdr" ]; then
	: >"$hdr"
	while read -r m pat h; do
		# shellcheck disable=SC2254 # образец — glob
		case "$method ${url#https://compute.api.cloud.ru/api/v1}" in "$m "$pat) printf '%s\r\n' "$h" >>"$hdr" ;; esac
	done < <(cat "$FAKE_DIR/http.h" 2>/dev/null)
fi
code=404 body='{}' i=0
while read -r m pat c b; do
	i=$((i + 1))
	once=0
	case "$m" in *!) once=1 m=${m%!} ;; esac
	[ "$once" = 0 ] || ! grep -qx "$i" "$FAKE_DIR/http.used" 2>/dev/null || continue
	# shellcheck disable=SC2254 # образец — glob
	case "$method ${url#https://compute.api.cloud.ru/api/v1}" in "$m "$pat)
		code=$c
		body=${b:-{\}}
		[ "$once" = 0 ] || echo "$i" >>"$FAKE_DIR/http.used"
		break
		;;
	esac
done < <(cat "$FAKE_DIR/http" "$FAKE_DIR/http.default" 2>/dev/null)
[ "$body" != - ] || body=
# Код 000 — curl не получил ответа: тела нет, код выхода 56 (обрыв).
[ "$code" != 000 ] || { printf '000'; exit 56; }
printf '%s' "$body" >"$out"
printf '%s' "$code"
FAKE
chmod +x "$tmp/bin/curl"
# Разовый ключ пробы (vm_meta) — без настоящего ssh-keygen.
cat >"$tmp/bin/ssh-keygen" <<'FAKE'
#!/usr/bin/env bash
while [ $# -gt 0 ]; do case $1 in -f) : >"$2"; echo "ssh-ed25519 AAAA evo-probe" >"$2.pub"; shift ;; esac; shift; done
FAKE
chmod +x "$tmp/bin/ssh-keygen"
export PATH="$tmp/bin:$PATH" KEY_ID=k SECRET=s PROJECT=p RUN=r7a1 ZONE=ru.AZ-2 GITHUB_STEP_SUMMARY=/dev/null PROBE_PAUSE=0 PROBE_WAIT_TWICE=1 SWEEP_PAUSE=1 API_RETRY_PAUSE=0
fail=0
n=0
RM=https://resource-manager.api.cloud.ru/v1
world() {
	n=$((n + 1))
	export FAKE_DIR="$tmp/w$n" RUNNER_TEMP="$tmp/w$n"
	mkdir "$FAKE_DIR"
	fake_evo_answer list_zones 0 0 '{"zones":[{"id":"z2","name":"ru.AZ-2"}]}'
	fake_evo_answer vm_list 0 0 '{"vms":[]}'
	fake_evo_answer list_floating_ips 0 0 '{"floating_ips":[]}'
	fake_evo_answer disk_list 0 0 '{"disks":[]}'
	fake_evo_answer list_subnets 0 0 '{"subnets":[]}'
	printf '%s\n' 'GET /backups[?]* 200 {"items":[],"total":0}' 'GET /protection-plans[?]* 200 {"items":[],"total":0}' \
		"GET $RM/tags[?]* 200 {\"tags\":[]}" >"$FAKE_DIR/http.default"
}
# Уборка области читает списки планов защиты, копий и тегов (виды pplan,
# backup и tag) дважды: в цикле удаления и при проверке остатков. Запрос к
# «Менеджеру ресурсов» заглушка пишет с хостом: он не Compute.
TAGS="GET $RM/tags?projectId=p&pageSize=100;"
SW1="GET /protection-plans?project_id=p&limit=1000;GET /backups?project_id=p&limit=1000;$TAGS"
SW="$SW1$SW1"
want() { if [ "$2" != "$3" ]; then printf 'FAIL %s\n  got:  %s\n  want: %s\n' "$1" "$2" "$3"; fail=1; fi; }
calls() { tr '\n' ';' <"$FAKE_DIR/calls"; }

# Г-11: группа → пять правил и правило с группой-источником → чтение правил
# списком группы (в карточке группы правил нет — только счётчики, прогон
# 205) → повтор правила → удаление правила: повтор сразу, ожидание
# исчезновения, повтор после → то же для группы. Группы после пробы нет.
world
cat >"$FAKE_DIR/http" <<'H'
POST /security-groups 201 {"id":"sg1","name":"evo-e2e-ops-r7a1-sg"}
GET! /security-groups/sg1 200 {"id":"sg1","state":"created"}
POST /security-groups/sg1/rules 201 {"id":"rule1"}
GET! /security-groups/sg1/rules/rule1 200 {"id":"rule1","remote_ip_prefix":null,"remote_security_group":{"id":"sg1","name":"evo-e2e-ops-r7a1-sg"}}
GET! /security-groups/sg1/rules/rule1 200 {"id":"rule1","remote_ip_prefix":"0.0.0.0/0","remote_security_group":null}
GET /security-groups/sg1/rules/rule1 404
GET /security-groups/sg1/rules?limit=100 200 {"items":[{"id":"rule1","remote_ip_prefix":"0.0.0.0/0","state":"created"},{"id":"rule1","port_range":"18092:18092","remote_ip_prefix":null,"remote_security_group":{"id":"sg1","name":"evo-e2e-ops-r7a1-sg"}},{"id":"rule1","port_range":"18093:18093","remote_ip_prefix":"0.0.0.0/0","remote_security_group":null},{"id":"rule1","ip_protocol":"tcp","port_range":"1:65535","remote_ip_prefix":"198.18.255.1/32"}],"total":4}
DELETE! /security-groups/sg1/rules/rule1 204
DELETE! /security-groups/sg1/rules/rule1 422 [{"code":"security_group_rule_can_not_be_deleted_from_current_state","message":"no"}]
DELETE /security-groups/sg1/rules/rule1 404 [{"code":"security_group_rule_not_found","message":"no"}]
DELETE! /security-groups/sg1 204
DELETE! /security-groups/sg1 422 [{"code":"security_group_can_not_be_deleted_from_current_state","message":"no"}]
DELETE /security-groups/sg1 404 [{"code":"security_group_not_found","message":"no"}]
GET /security-groups/sg1 404
H
fake_evo_answer list_security_groups 0 0 '{"security_groups":[]}'
rc=0
PROBES=g11 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-11: код" "$rc" 0
R=/security-groups/sg1/rules
want "Г-11: запросы по порядку" "$(calls)" "POST /security-groups;GET /security-groups/sg1;POST $R;POST $R;POST $R;POST $R;POST $R;POST $R;GET $R/rule1;POST $R;GET $R/rule1;POST $R;GET $R?limit=100;POST $R;GET $R?limit=100;DELETE $R/rule1;DELETE $R/rule1;GET $R/rule1;DELETE $R/rule1;DELETE /security-groups/sg1;DELETE /security-groups/sg1;GET /security-groups/sg1;DELETE /security-groups/sg1;$SW"
grep -q 'Г-11: готовность группы.*состояния: created' "$tmp/out" || { echo "FAIL: Г-11: готовность группы до правил не записана"; fail=1; }
grep -q 'Г-11: чтение правил.*HTTP 200, правил: 4, поля правила: \["id","remote_ip_prefix","state"\].*«весь интернет» хранится как: <ip>/0' "$tmp/out" || { echo "FAIL: Г-11: правила не прочитаны списком группы"; grep 'чтение' "$tmp/out" | sed 's/^/  | /'; fail=1; }
grep -q 'Г-11: удаление правила.*HTTP 204, повтор сразу — HTTP 422, машинный код security_group_rule_can_not_be_deleted_from_current_state.*состояния: нет (404).*повтор после исчезновения — HTTP 404, машинный код security_group_rule_not_found' "$tmp/out" || { echo "FAIL: Г-11: повтор удаления правила после исчезновения не записан"; grep 'удаление правила' "$tmp/out" | sed 's/^/  | /'; fail=1; }
grep -q 'Г-11: удаление группы.*повтор после исчезновения — HTTP 404, машинный код security_group_not_found' "$tmp/out" || { echo "FAIL: Г-11: повтор удаления группы после исчезновения не записан"; grep 'удаление группы' "$tmp/out" | sed 's/^/  | /'; fail=1; }
# Правило с группой-источником в ответе списка (по списку его узнаёт evo):
# форма источника и префикс — те же слова, что у чтения по id.
grep -q 'Г-11: чтение правил.*правило с группой-источником в списке: объект remote_security_group с полями \["id","name"\], remote_ip_prefix: null' "$tmp/out" || { echo "FAIL: Г-11: форма группы-источника в списке не записана"; grep 'чтение правил' "$tmp/out" | sed 's/^/  | /'; fail=1; }
# Гипотеза Г5 дельта-аудита 1.9: правило без источника в ответе списка (по
# списку evo решает, что останется открытым) и правило «все порты» — что
# платформа хранит в port_range; при пустом значении evo не посчитает
# правило ни открывающим порт, ни последним правилом SSH. Тело правила «все
# порты»: tcp, port_range «any», источник — тестовый адрес, не интернет.
grep -q 'Г-11: чтение правил.*правило без источника в списке: группы в ответе нет.*remote_ip_prefix: весь IPv4' "$tmp/out" || { echo "FAIL: Г-11: правило без источника в списке не записано"; grep 'чтение правил' "$tmp/out" | sed 's/^/  | /'; fail=1; }
jq -e 'select(.port_range == "any" and .ip_protocol == "tcp") | .remote_ip_prefix == "198.18.255.1/32"' "$FAKE_DIR/bodies" >/dev/null || { echo "FAIL: Г-11: тело правила «все порты» не то"; grep '"any"' "$FAKE_DIR/bodies"; fail=1; }
grep -q 'Г-11: правило «все порты».*HTTP 201, port_range в ответе создания: «нет поля».*в списке: «1:65535»' "$tmp/out" || { echo "FAIL: Г-11: что хранится у правила «все порты», не записано"; grep 'все порты' "$tmp/out" | sed 's/^/  | /'; fail=1; }
# Правило «откуда угодно» без источника вовсе: в теле нет ни адреса, ни
# группы; в строке — что платформа хранит в remote_ip_prefix (от этого
# зависит ветка «без префикса — любой» в sgrules.Coverage, план 1.9).
jq -e 'select(.port_range == "18093:18093") | (has("remote_ip_prefix") | not) and (has("remote_security_group_id") | not)' "$FAKE_DIR/bodies" >/dev/null ||
	{ echo "FAIL: Г-11: тело правила без источника не то"; grep 18093 "$FAKE_DIR/bodies"; fail=1; }
grep -q 'Г-11: правило без источника.*HTTP 201.*remote_ip_prefix в ответе создания: null.*remote_ip_prefix в чтении по id: весь IPv4.*remote_security_group: null' "$tmp/out" || { echo "FAIL: Г-11: что хранится у правила без источника, не записано"; grep 'без источника' "$tmp/out" | sed 's/^/  | /'; fail=1; }
# ICMP — с port_range any: без диапазона платформа отвечает 422 (прогон 205).
grep -q '"ip_protocol":"icmp","port_range":"any"' "$FAKE_DIR/bodies" || { echo "FAIL: Г-11: правило icmp без port_range any"; fail=1; }
grep -q 'Г-11: повтор создания того же правила' "$tmp/out" || { echo "FAIL: нет строки о повторе правила"; fail=1; }
# Правило с группой-источником: в теле — remote_security_group_id самой
# группы, без remote_ip_prefix; в строке — форма источника в ответе чтения.
jq -e '.remote_security_group_id == "sg1" and (has("remote_ip_prefix") | not)' "$FAKE_DIR/body.POST.security-groups.sg1.rules.group" >/dev/null ||
	{ echo "FAIL: Г-11: тело правила с группой-источником не то: $(cat "$FAKE_DIR/body.POST.security-groups.sg1.rules.group" 2>/dev/null)"; fail=1; }
grep -q 'Г-11: правило с группой-источником.*HTTP 201.*источник в ответе чтения по id: объект remote_security_group с полями \["id","name"\].*remote_ip_prefix: null' "$tmp/out" || { echo "FAIL: Г-11: форма группы-источника не записана"; grep 'группой-источником' "$tmp/out" | sed 's/^/  | /'; fail=1; }
if grep -v '::add-mask::' "$tmp/out" | grep -q 'sg1\|rule1'; then echo "FAIL: id в строке лога"; fail=1; fi

# Правило создано, а чтение по id не удалось (404 без тела): строка
# называет код чтения, а не «null» — непрочитанное не выдаётся за ответ
# платформы о том, что хранит правило.
world
cat >"$FAKE_DIR/http" <<'H'
POST /security-groups 201 {"id":"sg1","name":"evo-e2e-ops-r7a1-sg"}
GET! /security-groups/sg1 200 {"id":"sg1","state":"created"}
POST /security-groups/sg1/rules 201 {"id":"rule1"}
GET /security-groups/sg1/rules/rule1 404
GET /security-groups/sg1/rules?limit=100 200 {"items":[],"total":0}
DELETE /security-groups/sg1/rules/rule1 404 [{"code":"security_group_rule_not_found","message":"no"}]
DELETE! /security-groups/sg1 204
DELETE /security-groups/sg1 404 [{"code":"security_group_not_found","message":"no"}]
GET /security-groups/sg1 404
H
fake_evo_answer list_security_groups 0 0 '{"security_groups":[]}'
rc=0
PROBES=g11 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-11, правило не прочитано: код" "$rc" 0
for step in 'правило с группой-источником' 'правило без источника'; do
	grep -q "Г-11: $step.*HTTP 201.*чтение по id: HTTP 404" "$tmp/out" || { echo "FAIL: Г-11: $step — непрочитанное правило не названо кодом"; grep "$step" "$tmp/out" | sed 's/^/  | /'; fail=1; }
	if grep "Г-11: $step" "$tmp/out" | grep -q 'чтени[ия] по id.*remote_ip_prefix'; then echo "FAIL: Г-11: $step — префикс записан по непрочитанному правилу"; fail=1; fi
done

# Группа не создана (отказ платформы): дальше проба не идёт, код 0 — это
# ответ платформы, а не провал; правил и удалений нет.
world
echo 'POST /security-groups 422 {"code":"quota_exceeded","message":"limit of 3f2b8c1e-1111-4222-8333-444455556666"}' >"$FAKE_DIR/http"
fake_evo_answer list_security_groups 0 0 '{"security_groups":[]}'
rc=0
PROBES=g11 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-11, отказ создания: код" "$rc" 0
# Машинный код отказа — квота: строка считает, что осталось создано (ещё
# одно чтение списков копий и планов).
want "Г-11, отказ создания: один запрос" "$(calls)" "POST /security-groups;$SW1$SW"
grep -q 'квота по машинному коду: да' "$tmp/out" || { echo "FAIL: Г-11: отказ по квоте не назван"; fail=1; }
grep -q 'HTTP 422' "$tmp/out" || { echo "FAIL: отказ создания не назван кодом"; fail=1; }
# UUID из текста отказа платформы в лог не попадает (api_why идёт через redact.sed).
if grep -q '3f2b8c1e-1111-4222-8333-444455556666' "$tmp/out"; then echo "FAIL: id из текста отказа в логе"; fail=1; fi

# Проба оборвалась после создания (удаление группы не принято): уборка
# области находит группу по префиксу и удаляет её; остаток — провал.
world
cat >"$FAKE_DIR/http" <<'H'
POST /security-groups 201 {"id":"sg1"}
POST /security-groups/sg1/rules 500 {"message":"boom"}
GET /security-groups/sg1 200 {"id":"sg1","rules":[]}
DELETE /security-groups/sg1 500 {"message":"boom"}
DELETE /security-groups/sgleft 204
H
fake_evo_answer list_security_groups 0 0 '{"security_groups":[{"id":"sgleft","name":"evo-e2e-ops-r7a1-sg"},{"id":"other","name":"SSH-access_ru.AZ-2"}]}'
fake_evo_answer list_security_groups 1 0 '{"security_groups":[{"id":"other","name":"SSH-access_ru.AZ-2"}]}'
rc=0
# Группа в этом мире не исчезает: без PROBE_WAIT проба ждала её полные
# две минуты — 122 с из 180 с всего теста (замер 13.11).
# wall-clock: граница 30 с — доля продового ожидания (120 с).
t0=$SECONDS
PROBE_WAIT=1 PROBES=g11 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-11, остаток: код" "$rc" 0
[ $((SECONDS - t0)) -lt 30 ] || { echo "FAIL: Г-11, остаток: проба ждала неисчезающую группу $((SECONDS - t0)) с — предел ожидания не взят из PROBE_WAIT"; fail=1; }
grep -q 'Г-11: удаление группы.*не исчезло' "$tmp/out" || { echo "FAIL: Г-11, остаток: неисчезнувшая группа не названа"; grep 'удаление группы' "$tmp/out" | sed 's/^/  | /'; fail=1; }
grep -q '^DELETE /security-groups/sgleft$' "$FAKE_DIR/calls" || { echo "FAIL: уборка области не удалила группу пробы"; sed 's/^/  | /' "$tmp/out"; fail=1; }
if grep -q 'DELETE /security-groups/other' "$FAKE_DIR/calls"; then echo "FAIL: уборка удалила чужую группу"; fail=1; fi

# Общая уборка cleanup_only (sweep evo-e2e-) рядом с живым запуском 185:
# его группа не удаляется, группа завершённого запуска 184 удаляется
# (фильтр e2e_live_runs.sh действует на каждый вид, и на новый sg).
world
printf '#!/bin/sh\ncase "$*" in *status=in_progress*) echo 185 ;; esac\n' >"$tmp/bin/gh"
chmod +x "$tmp/bin/gh"
echo 'DELETE /security-groups/* 204' >"$FAKE_DIR/http"
fake_evo_answer list_security_groups 0 0 '{"security_groups":[{"id":"sglive","name":"evo-e2e-ops-r185a1-sg"},{"id":"sgdone","name":"evo-e2e-ops-r184a1-sg"}]}'
fake_evo_answer list_security_groups 1 0 '{"security_groups":[{"id":"sglive","name":"evo-e2e-ops-r185a1-sg"}]}'
rc=0
GITHUB_REPOSITORY=o/r GH_TOKEN=t E2E_LIVE_TTL=0 "$here/cloud-live.sh" sweep evo-e2e- ops >"$tmp/out" 2>&1 || rc=$?
want "общая уборка рядом с живым запуском: код" "$rc" 0
want "общая уборка: удалена только группа завершённого запуска" "$(grep -c '^DELETE /security-groups/' "$FAKE_DIR/calls") $(grep -c '^DELETE /security-groups/sgdone$' "$FAKE_DIR/calls")" "1 1"

# Г-10: ВМ → карточка → остановка и её повтор → запуск и его повтор →
# перезагрузка и повтор сразу → set-power по id, которого нет → flavor на
# шаг вверх: на запущенной отказ не по квоте, остановка, смена, возврат.
V='"id":"vm1","flavor":{"name":"lowcost10-2-4"},"interfaces":[{"ip":"203.0.113.7"}]'
g10_world() {
	world
	fake_evo_answer provision 0 0 '{"vm_id":"vm1","vm_ip":"203.0.113.7","status":"ok"}'
	fake_evo_answer list_flavors 0 0 '{"flavors":[{"name":"lowcost10-8-16","vcpus":8,"ram_gb":16},{"name":"lowcost10-4-8","vcpus":4,"ram_gb":8},{"name":"lowcost10-2-4","vcpus":2,"ram_gb":4},{"name":"gen-4-16","vcpus":4,"ram_gb":16}]}'
	fake_evo_answer list_security_groups 0 0 '{"security_groups":[]}'
	cat >"$FAKE_DIR/http" <<H
GET! /vms/vm1 200 {$V,"state":"running"}
POST! /vms/vm1/set-power 204
GET! /vms/vm1 200 {$V,"state":"stopping"}
GET! /vms/vm1 200 {$V,"state":"stopped"}
POST! /vms/vm1/set-power 422 {"code":"vm_wrong_state","message":"already stopped"}
POST! /vms/vm1/set-power 204
GET! /vms/vm1 200 {$V,"state":"starting"}
GET! /vms/vm1 200 {$V,"state":"running"}
POST! /vms/vm1/set-power 204
GET! /vms/vm1 200 {$V,"state":"running"}
POST! /vms/vm1/set-power 204
POST! /vms/vm1/set-power 422 {"code":"vm_wrong_state","message":"rebooting"}
GET! /vms/vm1 200 {$V,"state":"running"}
GET! /vms/vm1 200 {$V,"state":"rebooting"}
GET! /vms/vm1 200 {$V,"state":"running"}
POST /vms/00000000-0000-4000-8000-000000000000/set-power 404 {"code":"vm_not_found"}
H
}
g10_world
cat >>"$FAKE_DIR/http" <<H
PUT! /vms/vm1 422 {"code":"vm_must_be_stopped","message":"stop it"}
GET! /vms/vm1 200 {$V,"state":"running"}
POST! /vms/vm1/set-power 204
GET! /vms/vm1 200 {$V,"state":"stopped"}
PUT! /vms/vm1 200 {"state":"updating"}
GET! /vms/vm1 200 {"id":"vm1","state":"updating","flavor":{"name":"lowcost10-4-8"}}
GET! /vms/vm1 200 {"id":"vm1","state":"updating"}
GET! /vms/vm1 200 {"id":"vm1","state":"stopped","flavor":{"name":"lowcost10-4-8"}}
GET! /vms/vm1 200 {"id":"vm1","state":"stopped","flavor":{"name":"lowcost10-4-8"}}
PUT! /vms/vm1 200 {"state":"updating"}
GET /vms/vm1 200 {$V,"state":"stopped"}
H
rc=0
PROBES=g10 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-10: код" "$rc" 0
P='POST /vms/vm1/set-power;' G='GET /vms/vm1;'
want "Г-10: запросы по порядку" "$(calls)" "$G$P$G$G$P$P$G$G$P$G$P$P$G$G$G""POST /vms/00000000-0000-4000-8000-000000000000/set-power;PUT /vms/vm1;$G$P$G""PUT /vms/vm1;$G$G$G$G""PUT /vms/vm1;$G$G$G$SW"
want "Г-10: шаг вверх — соседний flavor семейства, возврат — исходный" "$(jq -r .flavor_name "$FAKE_DIR/body.PUT.vms.vm1")" lowcost10-2-4
for s in 'Г-10: остановка.*stopping → stopped' 'Г-10: остановка.*первое чтение после ответа: уже stopping' 'Г-10: запуск.*первое чтение после ответа: уже starting' 'Г-10: перезагрузка.*первое чтение после ответа: ещё running, смена состояния — в чтении через [0-9]* с' 'повтор остановки остановленной.*HTTP 422, машинный код vm_wrong_state' 'Г-10: запуск.*публичный адрес тот же: да' 'Г-10: перезагрузка.*running → rebooting → running' 'id, которого нет.*HTTP 404, машинный код vm_not_found' 'смена flavor на запущенной ВМ.*машинный код: vm_must_be_stopped' 'смена flavor на шаг вверх, ВМ остановленной.*в первом чтении после запроса: state updating, flavor целевой.*вышла в stopped.*тем, что просили' 'возврат исходного flavor, ВМ остановленной.*тем, что просили'; do
	grep -q "$s" "$tmp/out" || { echo "FAIL: Г-10: нет строки «$s»"; sed 's/^/  | /' "$tmp/out"; fail=1; }
done
if grep -v '::add-mask::' "$tmp/out" | grep -q 'vm1\|203\.0\.113\.7'; then echo "FAIL: Г-10: id или адрес в строке лога"; fail=1; fi
if grep -q 'квота по машинному коду' "$tmp/out"; then echo "FAIL: Г-10: отказ не по квоте назван квотой"; fail=1; fi

# Смена flavor принята на запущенной ВМ: состояние running — конечное, а не
# переходное (оканчивается на «ing»): ожидание не тянется до предела.
g10_world
cat >>"$FAKE_DIR/http" <<H
PUT /vms/vm1 200 {"state":"running"}
GET /vms/vm1 200 {"id":"vm1","state":"running","flavor":{"name":"lowcost10-2-4"}}
H
rc=0
PROBES=g10 timeout 30 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-10, смена на запущенной: не ждёт предела, код" "$rc" 0
grep -q 'смена flavor на шаг вверх, ВМ запущенной.*состояния: running,.*вышла в running' "$tmp/out" || { echo "FAIL: Г-10: running не принят как конечное состояние"; fail=1; }

# Возврат исходного flavor после первого круга не удался: второй круг (на
# остановленной) не шлётся — изменение не уходит в ВМ, которая не
# вернулась в исходное (заключение «Риска» 151).
g10_world
cat >>"$FAKE_DIR/http" <<H
PUT! /vms/vm1 200 {"state":"updating"}
PUT /vms/vm1 500 {"message":"boom"}
GET /vms/vm1 200 {"id":"vm1","state":"running","flavor":{"name":"lowcost10-4-8"}}
POST /vms/vm1/set-power 204
H
rc=0
PROBES=g10 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-10, возврат не удался: второго круга нет" "$(grep -c '^PUT' "$FAKE_DIR/calls") $(sed -n '/^PUT/,$p' "$FAKE_DIR/calls" | grep -c 'set-power' || true)" "2 0"
grep -q 'смена flavor на остановленной ВМ.*не покрыто.*второй круг не шлю' "$tmp/out" || { echo "FAIL: Г-10: пропуск второго круга не назван"; grep 'Г-10' "$tmp/out" | tail -4 | sed 's/^/  | /'; fail=1; }

# Отказ по квоте на смене flavor (машинный код, не текст): шесть полей
# одной строкой, остановки и второй попытки смены нет, возврата нет.
g10_world
echo 'PUT /vms/vm1 422 {"code":"organization_quota_exceeded","message":"limit of 3f2b8c1e-1111-4222-8333-444455556666 reached"}' >>"$FAKE_DIR/http"
rc=0
PROBES=g10 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-10, квота: код" "$rc" 0
want "Г-10, квота: одна попытка смены, остановки после неё нет" "$(sed -n '/^PUT/,$p' "$FAKE_DIR/calls" | grep -c '^PUT\|set-power')" 1
for s in 'отказ HTTP 422 (синхронно)' 'машинный код: organization_quota_exceeded' 'evo сегодня: операции в evo нет' 'квота по машинному коду: да' 'осталось создано: pplan 0, backup 0, vm 0, fip 0, disk 0, subnet 0, sg 0' 'строка Г-01'; do
	grep -qF "$s" "$tmp/out" || { echo "FAIL: Г-10, квота: нет поля «$s»"; grep 'смена flavor' "$tmp/out" | sed 's/^/  | /'; fail=1; }
done
if grep -q '3f2b8c1e-1111-4222-8333-444455556666' "$tmp/out"; then echo "FAIL: Г-10, квота: id из текста отказа в логе"; fail=1; fi
# Compute отвечает на отказ телом-массивом [{"message":…,"code":…}]
# (прогон 201): машинный код читается и из него — иначе квота не
# распознаётся, и проба идёт дальше.
g10_world
echo 'PUT /vms/vm1 422 [{"message":"Quota exceeded","code":"project_quota_exceeded"}]' >>"$FAKE_DIR/http"
rc=0
PROBES=g10 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-10, квота в теле-массиве: одна попытка смены" "$(sed -n '/^PUT/,$p' "$FAKE_DIR/calls" | grep -c '^PUT\|set-power')" 1
grep -qF 'машинный код: project_quota_exceeded' "$tmp/out" || { echo "FAIL: Г-10: машинный код из тела-массива не прочитан"; grep 'смена flavor' "$tmp/out" | sed 's/^/  | /'; fail=1; }

# Слово «quota» только в тексте, машинного кода нет — квотой не называется
# (разбор текста запрещён), проба идёт дальше: остановка и вторая попытка.
g10_world
cat >>"$FAKE_DIR/http" <<H
PUT /vms/vm1 422 {"message":"quota exceeded"}
POST /vms/vm1/set-power 204
GET /vms/vm1 200 {$V,"state":"stopped"}
H
rc=0
PROBES=g10 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-10, квота только в тексте: вторая попытка смены есть" "$(grep -c '^PUT' "$FAKE_DIR/calls")" 2
if grep -q 'квота по машинному коду' "$tmp/out"; then echo "FAIL: Г-10: квота названа по тексту"; fail=1; fi

# Г-20: со словом g20 ВМ называется …-g20, в конце остановлена и остаётся:
# уборка области её, её диск и адрес не удаляет и остатком не считает.
g10_world
cat >>"$FAKE_DIR/http" <<H
PUT /vms/vm1 422 {"code":"quota_exceeded"}
GET! /vms/vm1 200 {$V,"state":"running"}
GET /vms/vm1 200 {$V,"state":"stopped"}
POST /vms/vm1/set-power 204
H
fake_evo_answer vm_list 0 0 '{"vms":[{"id":"vm1","name":"evo-e2e-ops-r7a1-g20"}]}'
fake_evo_answer list_floating_ips 0 0 '{"floating_ips":[{"id":"f1","name":"fip-evo-e2e-ops-r7a1-g20","state":"active"}]}'
rc=0
PROBES="g10 g20" "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-20: код" "$rc" 0
grep -q 'Г-20: ВМ пробы цены оставлена.*state stopped' "$tmp/out" || { echo "FAIL: Г-20: нет строки об оставленной ВМ"; sed 's/^/  | /' "$tmp/out"; fail=1; }
want "Г-20: последний запрос set-power — остановка" "$(jq -r .state "$FAKE_DIR/body.POST.vms.vm1.set-power")" power_off
if grep -q '^destroy\|^release_floating_ip' "$FAKE_DIR/evo-calls"; then echo "FAIL: Г-20: уборка области удалила ВМ или адрес пробы цены"; fail=1; fi
# Г-20, возврат исходного flavor не удался: ВМ на шаг дороже под
# исключением не остаётся — проба удаляет её, её адрес и диск сразу
# (заключение «Риска» 136, К2).
g10_world
cat >>"$FAKE_DIR/http" <<H
PUT! /vms/vm1 200 {"state":"updating"}
GET! /vms/vm1 200 {"id":"vm1","state":"running","flavor":{"name":"lowcost10-4-8"}}
GET! /vms/vm1 200 {"id":"vm1","state":"running","flavor":{"name":"lowcost10-4-8"}}
PUT /vms/vm1 500 {"message":"boom"}
GET /vms/vm1 200 {"id":"vm1","state":"stopped","flavor":{"name":"lowcost10-4-8"}}
POST /vms/vm1/set-power 204
H
fake_evo_answer vm_list 0 0 '{"vms":[]}'
fake_evo_answer list_floating_ips 0 0 '{"floating_ips":[{"id":"f1","name":"fip-evo-e2e-ops-r7a1-g20","state":"active"}]}'
fake_evo_answer list_floating_ips 1 0 '{"floating_ips":[]}'
fake_evo_answer destroy 0 0 '{"status":"deleted"}'
fake_evo_answer release_floating_ip 0 0 '{"status":"released"}'
rc=0
PROBE_WAIT=1 PROBES="g10 g20" "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
grep -q 'Г-20: проба цены не состоялась' "$tmp/out" || { echo "FAIL: Г-20: ВМ не в исходном flavor, а строки «не состоялась» нет"; sed 's/^/  | /' "$tmp/out"; fail=1; }
if grep -q 'ВМ пробы цены оставлена' "$tmp/out"; then echo "FAIL: Г-20: ВМ с flavor на шаг выше оставлена"; fail=1; fi
want "Г-20, flavor не исходный: ВМ удалена, адрес освобождён" "$(grep -c '^destroy yes=1' "$FAKE_DIR/evo-calls") $(grep -c '^release_floating_ip yes=1' "$FAKE_DIR/evo-calls")" "1 1"

# Общая уборка завершённого запуска: без слова g20 ВМ пробы цены не
# трогает, со словом — удаляет (сутки прошли, потребление прочитано).
world
: >"$FAKE_DIR/http"
printf '#!/bin/sh\nexit 0\n' >"$tmp/bin/gh"
fake_evo_answer list_security_groups 0 0 '{"security_groups":[]}'
fake_evo_answer vm_list 0 0 '{"vms":[{"id":"vm1","name":"evo-e2e-ops-r5a1-g20"}]}'
rc=0
GITHUB_REPOSITORY=o/r GH_TOKEN=t E2E_LIVE_TTL=0 "$here/cloud-live.sh" sweep evo-e2e- ops >"$tmp/out" 2>&1 || rc=$?
want "общая уборка без слова g20: код" "$rc" 0
if grep -q '^destroy' "$FAKE_DIR/evo-calls"; then echo "FAIL: общая уборка без слова g20 удалила ВМ пробы цены"; fail=1; fi
world
: >"$FAKE_DIR/http"
fake_evo_answer list_security_groups 0 0 '{"security_groups":[]}'
fake_evo_answer vm_list 0 0 '{"vms":[{"id":"vm1","name":"evo-e2e-ops-r5a1-g20"}]}'
fake_evo_answer vm_list 1 0 '{"vms":[]}'
fake_evo_answer destroy 0 0 '{"status":"deleted"}'
rc=0
PROBES=g20 GITHUB_REPOSITORY=o/r GH_TOKEN=t E2E_LIVE_TTL=0 "$here/cloud-live.sh" sweep evo-e2e- ops >"$tmp/out" 2>&1 || rc=$?
want "общая уборка со словом g20: код" "$rc" 0
want "общая уборка со словом g20: ВМ пробы цены удалена" "$(grep -c '^destroy' "$FAKE_DIR/evo-calls")" 1

# Г-18: ВМ и подсеть → второй интерфейс, повтор его создания, список
# интерфейсов ВМ, удаление своих и повтор удаления → пересоздание из
# другого образа того же семейства: на запущенной отказ не по квоте,
# остановка, вторая попытка; что с адресом и загрузочным диском.
g18_world() {
	world
	fake_evo_answer provision 0 0 '{"vm_id":"vm1","vm_ip":"203.0.113.7","status":"ok"}'
	fake_evo_answer subnet_create 0 0 '{"subnet_id":"sn1","status":"ok"}'
	# Подсеть сначала создаётся, затем готова; дальше списки — для уборки.
	fake_evo_answer list_subnets 0 0 '{"subnets":[{"id":"sn1","name":"evo-e2e-ops-r7a1-g18-sn","state":"creating"}]}'
	fake_evo_answer list_subnets 1 0 '{"subnets":[{"id":"sn1","name":"evo-e2e-ops-r7a1-g18-sn","state":"CREATED"}]}'
	fake_evo_answer list_subnets 2 0 '{"subnets":[]}'
	fake_evo_answer list_security_groups 0 0 '{"security_groups":[]}'
	fake_evo_answer image_list 0 0 '{"images":[{"id":"img-d","name":"debian-12","min_cpu":1},{"id":"img-22","name":"ubuntu-22.04","min_cpu":1},{"id":"img-24","name":"ubuntu-24.04","min_disk_gb":10},{"id":"img-g","name":"ubuntu-gpu","min_disk_gb":100}]}'
	fake_evo_answer image_get 0 0 '{"fields":[{"name":"hostname","filled_by":"evo","role":"vm_name"},{"name":"password","filled_by":"evo","role":"password"}]}'
	fake_evo_answer disk_list 0 0 '{"disks":[{"id":"boot1","name":"evo-e2e-ops-r7a1-g18-boot","bootable":true,"size_gb":10,"attached_to":["evo-e2e-ops-r7a1-g18"]}]}'
}
g18_world
cat >"$FAKE_DIR/http" <<H
POST! /interfaces 201 {"id":"if1","state":"creating"}
GET! /vms/vm1 200 {$V,"state":"running"}
POST! /interfaces 422 {"code":"interface_name_exists","message":"dup"}
GET /interfaces?project_id=p&vm_id=vm1&limit=100 200 {"items":[{"id":"if0","name":"main"},{"id":"if1","name":"evo-e2e-ops-r7a1-g18-if"}],"total":2}
DELETE! /interfaces/if1 204
DELETE /interfaces/if1 404 {"code":"interface_not_found"}
GET! /vms/vm1 200 {$V,"state":"running"}
POST! /vms/vm1/rebuild 422 [{"code":"vm_can_not_be_changed","message":"Vm can not be changed from current state"}]
POST /vms/vm1/set-power 204
GET! /vms/vm1 200 {$V,"state":"stopped"}
POST! /vms/vm1/rebuild 200 {"id":"vm1","state":"rebuilding"}
GET! /vms/vm1 200 {$V,"state":"rebuilding"}
GET /vms/vm1 200 {$V,"state":"stopped","image":{"name":"ubuntu-24.04"}}
H
rc=0
PROBES=g18 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-18: код" "$rc" 0
want "Г-18: запросы по порядку" "$(calls)" "POST /interfaces;GET /vms/vm1;POST /interfaces;GET /interfaces?project_id=p&vm_id=vm1&limit=100;GET /interfaces/if1;DELETE /interfaces/if1;DELETE /interfaces/if1;GET /vms/vm1;POST /vms/vm1/rebuild;POST /vms/vm1/set-power;GET /vms/vm1;POST /vms/vm1/rebuild;GET /vms/vm1;GET /vms/vm1;$SW"
jq -e --arg p p '.vm_id == "vm1" and .subnet_id == "sn1" and .name == "evo-e2e-ops-r7a1-g18-if" and .project_id == $p' "$FAKE_DIR/body.POST.interfaces" >/dev/null ||
	{ echo "FAIL: Г-18: тело создания интерфейса не то: $(cat "$FAKE_DIR/body.POST.interfaces" 2>/dev/null)"; fail=1; }
# Другой образ — того же семейства, что образ ВМ, и не требует больше её flavor.
jq -e '.image_id == "img-24" and (has("image_name") | not) and .image_metadata.hostname == "evo-e2e-ops-r7a1-g18" and (.image_metadata.password | test("^Ev1-[0-9a-f]{32}$"))' "$FAKE_DIR/body.POST.vms.vm1.rebuild" >/dev/null ||
	{ echo "FAIL: Г-18: тело пересоздания не то: $(cat "$FAKE_DIR/body.POST.vms.vm1.rebuild" 2>/dev/null)"; fail=1; }
for s in 'Г-18: второй интерфейс.*HTTP 201' 'повтор создания с тем же именем.*HTTP 422, машинный код interface_name_exists' 'удаление интерфейса.*HTTP 204, повтор — HTTP 404' 'пересоздание на запущенной ВМ.*машинный код: vm_can_not_be_changed' 'Г-18: пересоздание из ubuntu-24.04.*на остановленной.*rebuilding → stopped.*образ тот, что просили.*публичный адрес тот же: да.*загрузочный диск тот же: да'; do
	grep -q "$s" "$tmp/out" || { echo "FAIL: Г-18: нет строки «$s»"; sed 's/^/  | /' "$tmp/out"; fail=1; }
done
if grep -v '::add-mask::' "$tmp/out" | grep -qw 'vm1\|if1\|sn1\|boot1\|203\.0\.113\.7'; then echo "FAIL: Г-18: id или адрес в строке лога"; fail=1; fi
# Чужой интерфейс ВМ (основной, не под именем пробы) проба не удаляет.
if grep -q 'DELETE /interfaces/if0' "$FAKE_DIR/calls"; then echo "FAIL: Г-18: удалён основной интерфейс ВМ"; fail=1; fi

if grep -q '^disk_delete' "$FAKE_DIR/evo-calls"; then echo "FAIL: Г-18: загрузочный диск тот же, а диск удалён"; fail=1; fi
# Интерфейс создаётся только в готовой подсети (прогон 201: запрос сразу
# после subnet_create получил «состояние подсети не даёт создать интерфейс»).
want "Г-18: подсеть читалась до готовности" "$(grep -c '^list_subnets' "$FAKE_DIR/evo-calls")" 4
grep -q 'Г-18: подсеть.*creating → CREATED' "$tmp/out" || { echo "FAIL: Г-18: готовность подсети не записана"; fail=1; }

# Отказ пересоздания не про состояние ВМ (проверка тела): остановки и второй
# попытки нет — прогон 201 останавливал ВМ зря.
g18_world
fake_evo_answer subnet_create 0 1 '{"error":{"reason":"USAGE","message":"cidr занят"}}'
cat >"$FAKE_DIR/http" <<H
GET /vms/vm1 200 {$V,"state":"running"}
POST /vms/vm1/rebuild 422 [{"message":"Input should be a valid UUID","code":"uuid_parsing"}]
H
rc=0
PROBES=g18 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-18, отказ не про состояние: одна попытка, без остановки" "$(grep -c 'rebuild' "$FAKE_DIR/calls") $(grep -c 'set-power' "$FAKE_DIR/calls" || true)" "1 0"

# Прогон 204. Список интерфейсов ВМ отвечает отказом: интерфейс удаляется по
# id из ответа создания, а отказ списка записан с машинным кодом. Запрос без
# ответа (обрыв, код 000) — не «отказ платформы»: строка «не покрыто», исход
# неизвестен; тело прошлого ответа за ответ не выдаётся.
g18_world
cat >"$FAKE_DIR/http" <<H
POST! /interfaces 201 {"id":"if1","state":"creating"}
GET! /vms/vm1 200 {$V,"state":"running"}
POST! /interfaces 422 [{"code":"only_one_interface","message":"dup"}]
GET /interfaces?* 422 [{"code":"list_refused","message":"no"}]
DELETE! /interfaces/if1 204
DELETE /interfaces/if1 404 [{"code":"interface_not_found"}]
GET /vms/vm1 200 {$V,"state":"running"}
POST /vms/vm1/rebuild 000
H
rc=0
PROBES=g18 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-18, список отказал: интерфейс удалён по id создания" "$(grep -c '^DELETE /interfaces/if1$' "$FAKE_DIR/calls")" 2
grep -q 'Г-18: удаление интерфейса.*список: HTTP 422, машинный код list_refused.*удаление интерфейса: HTTP 204' "$tmp/out" || { echo "FAIL: Г-18: отказ списка интерфейсов не записан или интерфейс не удалён"; grep 'Г-18' "$tmp/out" | sed 's/^/  | /'; fail=1; }
grep -q 'Г-18: пересоздание на запущенной ВМ.*не покрыто.*ответа нет (curl: код 56)' "$tmp/out" || { echo "FAIL: Г-18: обрыв без ответа записан не как «не покрыто»"; grep 'пересоздание' "$tmp/out" | sed 's/^/  | /'; fail=1; }
if grep -q 'отказ HTTP 000\|"state":"running"' "$tmp/out"; then echo "FAIL: Г-18: обрыв выдан за отказ или напечатано тело прошлого ответа"; fail=1; fi
want "Г-18, обрыв: второй попытки и остановки нет" "$(grep -c 'rebuild' "$FAKE_DIR/calls") $(grep -c 'set-power' "$FAKE_DIR/calls" || true)" "1 0"

# Прогон 207. Чтение без ответа повторяется: карточка ВМ прочитана со
# второго раза, исходный flavor известен. Первое правило группы отказано по
# состоянию — «повтор» повторяет правило, которое создано, а не первое в
# списке. Образ больше загрузочного диска ВМ для пересоздания не берётся.
world
fake_evo_answer list_security_groups 0 0 '{"security_groups":[]}'
cat >"$FAKE_DIR/http" <<'H'
POST /security-groups 201 {"id":"sg1","name":"evo-e2e-ops-r7a1-sg"}
GET! /security-groups/sg1 000
GET! /security-groups/sg1 200 {"id":"sg1","state":"created"}
POST! /security-groups/sg1/rules 422 [{"code":"security_group_state_blocks_security_group_rule_creation","message":"no"}]
POST /security-groups/sg1/rules 201 {"id":"rule2"}
GET! /security-groups/sg1/rules/rule2 200 {"id":"rule2"}
GET /security-groups/sg1/rules/rule2 404
GET /security-groups/sg1/rules?limit=100 200 {"items":[{"id":"rule2"}],"total":1}
DELETE /security-groups/sg1/rules/rule2 204
DELETE /security-groups/sg1 204
GET /security-groups/sg1 404
H
rc=0
PROBES=g11 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-11, группа не сразу готова: чтение без ответа повторено" "$(grep -c '^GET /security-groups/sg1$' "$FAKE_DIR/calls")" 3
grep -q 'Г-11: готовность группы.*состояния: created' "$tmp/out" || { echo "FAIL: Г-11: чтение группы после обрыва не повторено"; grep 'готовность' "$tmp/out" | sed 's/^/  | /'; fail=1; }
# Повтор — последний запрос правила в журнале тел: он повторяет второе
# правило (udp), а не первое (tcp), которое не создалось. По номеру строки
# сверять нельзя: журнал начинается с тела запроса токена, и прежняя
# проверка «восьмая строка» смотрела не на повтор.
[ "$(grep '"port_range":"18090:18090"' "$FAKE_DIR/bodies" | tail -1 | jq -r .ip_protocol)" = udp ] || { echo "FAIL: Г-11: повторено не то правило, что создано: $(grep '"port_range":"18090:18090"' "$FAKE_DIR/bodies" | tail -1)"; fail=1; }
[ "$(grep -c '"ip_protocol":"udp"' "$FAKE_DIR/bodies")" = 2 ] || { echo "FAIL: Г-11: правило udp отправлено не дважды (создание и повтор)"; fail=1; }
grep -q '^DELETE /security-groups/sg1/rules/rule2$' "$FAKE_DIR/calls" || { echo "FAIL: Г-11: удаляется не созданное правило"; fail=1; }

g18_world
fake_evo_answer subnet_create 0 1 '{"error":{"reason":"USAGE","message":"cidr занят"}}'
fake_evo_answer image_list 0 0 '{"images":[{"id":"img-22","name":"ubuntu-22.04","min_disk_gb":10},{"id":"img-24","name":"ubuntu-24.04","min_disk_gb":20}]}'
: >"$FAKE_DIR/http"
rc=0
PROBES=g18 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
grep -q 'Г-18: пересоздание.*не покрыто.*помещается на диск ВМ (10 ГБ)' "$tmp/out" || { echo "FAIL: Г-18: образ больше диска не отсеян"; grep 'пересоздание' "$tmp/out" | sed 's/^/  | /'; fail=1; }
if grep -q 'rebuild' "$FAKE_DIR/calls" 2>/dev/null; then echo "FAIL: Г-18: пересоздание ушло с образом больше диска"; fail=1; fi

# Пересоздание выдало ВМ новый загрузочный диск, а старый оставило без ВМ:
# под именем пробы уборка его может не найти — проба удаляет его по id
# (заключение «Риска» 140, К1).
g18_world
fake_evo_answer subnet_create 0 1 '{"error":{"reason":"USAGE","message":"cidr занят"}}'
# disk_list до пересоздания читают дважды: размер диска и id.
cp "$FAKE_DIR/disk_list.0.json" "$FAKE_DIR/disk_list.1.json"
fake_evo_answer disk_list 2 0 '{"disks":[{"id":"boot2","name":"auto","bootable":true,"size_gb":10,"attached_to":["evo-e2e-ops-r7a1-g18"]},{"id":"boot1","name":"old-boot","bootable":true,"attached_to":[]}]}'
fake_evo_answer disk_delete 0 0 '{"status":"deleted"}'
cat >"$FAKE_DIR/http" <<H
POST /vms/vm1/rebuild 200 {"id":"vm1"}
GET /vms/vm1 200 {$V,"state":"running","image":{"name":"ubuntu-24.04"}}
H
rc=0
PROBES=g18 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-18, диск сменился: код" "$rc" 0
want "Г-18, диск сменился: старый удалён по id" "$(grep -c '^disk_delete yes=1' "$FAKE_DIR/evo-calls")" 1
grep -q 'загрузочный диск тот же: нет.*старый диск без ВМ удалён по id' "$tmp/out" || { echo "FAIL: Г-18: удаление старого диска не названо"; sed 's/^/  | /' "$tmp/out"; fail=1; }

# Подсеть не создана: интерфейс не создаётся, пересоздание идёт; отказ по
# квоте на пересоздании — без остановки и второй попытки.
g18_world
fake_evo_answer subnet_create 0 1 '{"error":{"reason":"USAGE","message":"cidr занят"}}'
cat >"$FAKE_DIR/http" <<H
GET /vms/vm1 200 {$V,"state":"running"}
POST /vms/vm1/rebuild 422 {"code":"quota_exceeded","message":"no"}
H
rc=0
PROBES=g18 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-18, без подсети и с квотой: код" "$rc" 0
want "Г-18, без подсети и с квотой: запросы" "$(grep -c '/interfaces' "$FAKE_DIR/calls" || true) $(grep -c 'rebuild' "$FAKE_DIR/calls") $(grep -c 'set-power' "$FAKE_DIR/calls" || true)" "0 1 0"
grep -q 'Г-18: подсеть.*USAGE' "$tmp/out" || { echo "FAIL: Г-18: отказ подсети не назван"; fail=1; }

# Подсеть пробы — вид уборки области: остаток находится и удаляется.
g18_world
fake_evo_answer provision 0 1 '{"error":{"reason":"API_ERROR","message":"boom"}}'
fake_evo_answer list_subnets 0 0 '{"subnets":[{"id":"snleft","name":"evo-e2e-ops-r7a1-g18-sn"},{"id":"snother","name":"default"}]}'
fake_evo_answer list_subnets 1 0 '{"subnets":[{"id":"snother","name":"default"}]}'
fake_evo_answer subnet_delete 0 0 '{"status":"deleted"}'
: >"$FAKE_DIR/http"
rc=0
PROBES=g18 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-18, остаток подсети: код" "$rc" 0
want "Г-18, остаток подсети: удалена одна" "$(grep -c '^subnet_delete yes=1' "$FAKE_DIR/evo-calls")" 1

# Г-16: диск и ВМ → копия диска и копия ВМ (ожидание created) → список и
# фильтр по имени → восстановление диска и ВМ под именами с префиксом пробы
# → удаление копии, повтор сразу и после исчезновения → план защиты с
# выключенным расписанием: создание, список, удаление и повтор → копии
# ресурсов пробы под чужим именем (их создал бы план) — нет.
g16_world() {
	world
	fake_evo_answer disk_create 0 0 '{"disk_id":"d1","status":"ok"}'
	fake_evo_answer provision 0 0 '{"vm_id":"vm1","status":"ok"}'
	fake_evo_answer vm_get 0 0 '{"id":"vmr","state":"running"}'
	fake_evo_answer destroy 0 0 '{"status":"deleted"}'
	fake_evo_answer disk_delete 0 0 '{"status":"deleted"}'
	fake_evo_answer list_security_groups 0 0 '{"security_groups":[]}'
}
g16_world
fake_evo_answer disk_list 0 0 '{"disks":[{"id":"boot1","name":"evo-e2e-ops-r7a1-g16-boot","bootable":true,"disk_type":"SSD-NVMe","attached_to":["evo-e2e-ops-r7a1-g16"]},{"id":"rd1","name":"evo-e2e-ops-r7a1-g16-rd","attached_to":[]},{"id":"rb1","name":"evo-e2e-ops-r7a1-g16-rv-boot","bootable":true,"attached_to":["evo-e2e-ops-r7a1-g16-rv"]}]}'
# disk_list читают: появление восстановленного диска, счёт дисков после
# второго восстановления, загрузочный диск ВМ,
# диски восстановленной ВМ, диск после удаления копии; дальше — уборка.
for i in 1 2 3 4; do cp "$FAKE_DIR/disk_list.0.json" "$FAKE_DIR/disk_list.$i.json"; done
fake_evo_answer disk_list 5 0 '{"disks":[]}'
cat >"$FAKE_DIR/http" <<'H'
POST! /backups 201 {"id":"bd","state":"creating"}
POST! /backups 201 {"id":"bv","state":"creating"}
GET! /backups/bd 200 {"id":"bd","state":"creating","size":0}
GET! /backups/bd 200 {"id":"bd","state":"created","size":5,"type":"full","created_at":"2026-11-12T10:00:00Z"}
GET! /backups/bd 200 {"id":"bd","state":"created","size":5,"last_restore_time":"2026-11-12T10:05:00"}
GET! /backups/bd 200 {"id":"bd","state":"deleting"}
GET /backups/bd 404 {"code":"not_found"}
GET! /backups/bv 200 {"id":"bv","state":"created","size":9}
GET! /backups/bv 200 {"id":"bv","state":"created","size":9}
GET /backups/bv 404
GET! /backups?project_id=p&limit=1000 200 {"items":[{"id":"bd","name":"evo-e2e-ops-r7a1-g16-bd"},{"id":"bv","name":"evo-e2e-ops-r7a1-g16-bv"},{"id":"x","name":"someone"}],"total":3}
GET /backups?project_id=p&name=evo-e2e-ops-r7a1-g16-bd 200 {"items":[{"id":"bd","name":"evo-e2e-ops-r7a1-g16-bd"}],"total":1}
GET /disks/rd1 200 {"id":"rd1","project":{"id":"p"}}
POST! /backups/bd/action 201 {"id":"bd","state":"updating"}
POST! /backups/bd/action 409 {"code":"disk_name_exists","message":"no"}
POST /backups/bv/action 201 {"id":"bv","state":"updating"}
DELETE! /backups/bd 204
DELETE! /backups/bd 409 {"code":"conflict","message":"deleting"}
DELETE /backups/bd 404 {"code":"not_found"}
DELETE /backups/bv 204
POST /protection-plans 201 {"id":"pp","state":"created"}
GET! /protection-plans/pp 200 {"id":"pp","state":"created"}
GET /protection-plans/pp 404
GET! /protection-plans?project_id=p&limit=1000 200 {"items":[{"id":"pp","name":"evo-e2e-ops-r7a1-g16-pp"}],"total":1}
DELETE! /protection-plans/pp 204
DELETE /protection-plans/pp 404 {"code":"not_found"}
H
rc=0
PROBES=g16 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-16: код" "$rc" 0
grep -q 'Г-16: восстановление диска.*проект диска по карточке: проект пробы' "$tmp/out" || { echo "FAIL: Г-16: проект восстановленного диска не записан"; grep 'восстановление диска' "$tmp/out" | sed 's/^/  | /'; fail=1; }
L='?project_id=p&limit=1000'
want "Г-16: запросы по порядку" "$(calls)" "POST /backups;GET /backups/bd;GET /backups/bd;POST /backups;GET /backups/bv;GET /backups$L;GET /backups?project_id=p&name=evo-e2e-ops-r7a1-g16-bd;POST /backups/bd/action;GET /backups/bd;GET /disks/rd1;POST /backups/bd/action;POST /backups/bv/action;GET /backups/bv;DELETE /backups/bd;DELETE /backups/bd;GET /backups/bd;GET /backups/bd;DELETE /backups/bd;DELETE /backups/bv;DELETE /backups/bv;GET /backups/bv;DELETE /backups/bv;POST /protection-plans;GET /protection-plans/pp;GET /protection-plans$L;DELETE /protection-plans/pp;DELETE /protection-plans/pp;GET /protection-plans/pp;GET /backups$L;$SW"
for s in 'Г-16: копия диска, ожидание готовности' 'creating → created' 'Г-16: восстановление диска' 'Г-16: восстановление ВМ' 'повтор сразу — HTTP 409' 'после исчезновения — HTTP 404' 'Г-16: план защиты' 'Г-16: копии ресурсов пробы под чужим именем'; do
	grep -q "$s" "$tmp/out" || { echo "FAIL: Г-16: нет строки «$s»"; sed 's/^/  | /' "$tmp/out"; fail=1; }
done
# Гипотезы дельта-аудита 1.9 (Г3, Г7): вид времени у копии — от него зависят
# возраст копии и предупреждение о недавнем восстановлении; второе
# восстановление той же копии под тем же именем — отказ или второй диск.
grep -q 'Г-16: копия диска, ожидание готовности.*created_at: RFC3339 с зоной' "$tmp/out" || { echo "FAIL: Г-16: вид created_at не записан"; grep 'ожидание готовности' "$tmp/out" | sed 's/^/  | /'; fail=1; }
grep -q 'Г-16: восстановление диска.*last_restore_time: без зоны' "$tmp/out" || { echo "FAIL: Г-16: вид last_restore_time не записан"; grep 'восстановление диска' "$tmp/out" | sed 's/^/  | /'; fail=1; }
grep -q 'Г-16: второе восстановление той же копии под тем же именем.*HTTP 409, машинный код disk_name_exists.*дисков с этим именем: 1' "$tmp/out" || { echo "FAIL: Г-16: второе восстановление не записано"; grep 'второе восстановление' "$tmp/out" | sed 's/^/  | /'; fail=1; }
# Восстановление: копия восстанавливается в новый ресурс с именем пробы
# (уборка области найдёт его по префиксу); диск восстановленной ВМ назван
# и привязан к загрузочному диску исходной.
jq -e '.action == "restore" and .resource.target_config.name == "evo-e2e-ops-r7a1-g16-rv" and (.resource.target_config.disks == [{"name":"evo-e2e-ops-r7a1-g16-rv-boot","related_disk_id":"boot1","disk_type_name":"SSD-NVMe"}])' "$FAKE_DIR/body.POST.backups.bv.action" >/dev/null ||
	{ echo "FAIL: Г-16: тело восстановления ВМ не то: $(cat "$FAKE_DIR/body.POST.backups.bv.action" 2>/dev/null)"; fail=1; }
# План защиты не запускает копирование: расписание выключено, немедленного
# запуска нет.
jq -e '.execute_immediately == false and (.schedules | length == 1) and .schedules[0].enabled == false and .resources == [{"resource_id":"d1","resource_type":"disk"}]' "$FAKE_DIR/body.POST.protection-plans" >/dev/null ||
	{ echo "FAIL: Г-16: тело плана защиты не то: $(cat "$FAKE_DIR/body.POST.protection-plans" 2>/dev/null)"; fail=1; }
if grep -v '::add-mask::' "$tmp/out" | grep -qw 'bd\|bv\|pp\|boot1'; then echo "FAIL: Г-16: id в строке лога"; fail=1; fi

# Копия не создана (отказ платформы): восстановления и удаления этой копии
# нет, проба идёт дальше — отказ и есть ответ; код 0.
g16_world
fake_evo_answer provision 0 1 '{"error":{"reason":"QUOTA_EXCEEDED","message":"нет квоты"}}'
cat >"$FAKE_DIR/http" <<'H'
POST /backups 422 {"code":"backup_not_allowed","message":"no"}
POST /protection-plans 422 {"code":"x","message":"no"}
H
rc=0
PROBES=g16 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-16, отказы: код" "$rc" 0
want "Г-16, отказы: запросы" "$(calls)" "POST /backups;GET /backups$L;POST /protection-plans;GET /backups$L;$SW"
grep -q 'Г-16: копия диска.*отказ HTTP 422' "$tmp/out" || { echo "FAIL: Г-16: отказ копии не назван"; fail=1; }
grep -q 'Г-16: ВМ.*QUOTA_EXCEEDED' "$tmp/out" || { echo "FAIL: Г-16: отказ ВМ не назван"; fail=1; }

# После восстановления копию какое-то время нельзя удалить (прогон 202:
# 422 backup_restore_in_progress при состоянии created): проба повторяет
# удаление, пока запрет не снят, и записывает, сколько он держался.
g16_world
fake_evo_answer provision 0 1 '{"error":{"reason":"API_ERROR","message":"boom"}}'
fake_evo_answer disk_list 0 0 '{"disks":[{"id":"rd1","name":"evo-e2e-ops-r7a1-g16-rd","attached_to":[]}]}'
# disk_list читают: появление восстановленного диска и диск после удаления
# копии; дальше — уборка.
cp "$FAKE_DIR/disk_list.0.json" "$FAKE_DIR/disk_list.1.json"
fake_evo_answer disk_list 2 0 '{"disks":[]}'
cat >"$FAKE_DIR/http" <<'H'
POST /backups 201 {"id":"bd","state":"created"}
POST /backups/bd/action 201 {"resource":{}}
DELETE! /backups/bd 422 [{"message":"Backup cannot be deleted while restore is in progress","code":"backup_restore_in_progress"}]
DELETE! /backups/bd 422 [{"message":"Backup cannot be deleted while restore is in progress","code":"backup_restore_in_progress"}]
DELETE! /backups/bd 422 [{"message":"Backup cannot be deleted while restore is in progress","code":"backup_restore_in_progress"}]
DELETE! /backups/bd 204
DELETE /backups/bd 404 [{"code":"backup_not_found"}]
GET! /backups/bd 200 {"id":"bd","state":"created"}
GET! /backups/bd 200 {"id":"bd","state":"created","last_restore_time":"t"}
GET /backups/bd 404
POST /protection-plans 422 [{"code":"value_error"}]
H
rc=0
# Предел ожидания — с запасом, а не секунда: повторы кончаются ответом 204
# заглушки, а не часами. С PROBE_WAIT=1 на медленном раннере окно
# закрывалось до первого повтора, и случай краснел в чужих PR (#1535).
PROBE_WAIT=120 PROBES=g16 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-16, запрет после восстановления: код" "$rc" 0
grep -q 'Г-16: восстановление диска.*проект диска по карточке: не прочитан (HTTP 404)' "$tmp/out" || { echo "FAIL: Г-16: непрочитанная карточка диска не названа кодом"; grep 'восстановление диска' "$tmp/out" | sed 's/^/  | /'; fail=1; }
# В этом мире второе восстановление принято (2xx), а второго диска за
# предел ожидания нет: диск может появиться после уборки области — платный
# остаток, которого никто не видел (заключение «Риска» по #1616). Проба
# говорит это предупреждением и называет, что сделать после прогона.
grep -q '::warning::Г-16: второе восстановление принято, а второго диска за .* с нет' "$tmp/out" || { echo "FAIL: Г-16: принятое второе восстановление без диска не названо предупреждением"; grep 'второе восстановление' "$tmp/out" | sed 's/^/  | /'; fail=1; }
grep -q 'Г-16: второе восстановление.*через 30 минут — уборка области' "$tmp/out" || { echo "FAIL: Г-16: в строке нет отложенной уборки"; fail=1; }
want "Г-16, запрет после восстановления: DELETE до принятия и повтор после исчезновения" "$(grep -c '^DELETE /backups/bd$' "$FAKE_DIR/calls")" 6
grep -q 'удаление копии диска.*запрет «идёт восстановление» снят через .* с (попыток: 4).*повтор после исчезновения — HTTP 404' "$tmp/out" || { echo "FAIL: Г-16: время запрета после восстановления не записано"; grep 'удаление копии' "$tmp/out" | sed 's/^/  | /'; fail=1; }

# У загрузочного диска в disk_list нет типа: без него платформа отвергает
# восстановление ВМ (прогон 202), проба его не шлёт и говорит почему.
g16_world
fake_evo_answer disk_create 0 1 '{"error":{"reason":"API_ERROR","message":"boom"}}'
fake_evo_answer disk_list 0 0 '{"disks":[{"id":"boot1","name":"evo-e2e-ops-r7a1-g16-boot","bootable":true,"attached_to":["evo-e2e-ops-r7a1-g16"]}]}'
fake_evo_answer disk_list 1 0 '{"disks":[]}'
cat >"$FAKE_DIR/http" <<'H'
POST /backups 201 {"id":"bv","state":"created"}
GET! /backups/bv 200 {"id":"bv","state":"created"}
GET /backups/bv 404
DELETE /backups/bv 204
H
rc=0
PROBE_WAIT=1 PROBES=g16 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-16, диск без типа: код" "$rc" 0
if grep -q '^POST /backups/bv/action' "$FAKE_DIR/calls"; then echo "FAIL: Г-16: восстановление ВМ ушло без типа диска"; fail=1; fi
grep -q 'Г-16: восстановление ВМ.*тип не найден' "$tmp/out" || { echo "FAIL: Г-16: не сказано, что типа диска нет"; sed 's/^/  | /' "$tmp/out"; fail=1; }

# Копия создана, но не дошла до created (error_creating): из неё не
# восстанавливают; удаление копии и остальное идёт.
g16_world
fake_evo_answer provision 0 1 '{"error":{"reason":"API_ERROR","message":"boom"}}'
cat >"$FAKE_DIR/http" <<'H'
POST /backups 201 {"id":"bd","state":"creating"}
GET! /backups/bd 200 {"id":"bd","state":"error_creating"}
GET /backups/bd 404
DELETE /backups/bd 204
POST /protection-plans 422 {"code":"x"}
H
rc=0
PROBES=g16 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-16, копия не готова: код" "$rc" 0
if grep -q '/action' "$FAKE_DIR/calls"; then echo "FAIL: Г-16: восстановление из копии, которая не дошла до created"; fail=1; fi
grep -q 'Г-16: копия диска, ожидание готовности.*error_creating' "$tmp/out" || { echo "FAIL: Г-16: состояние неготовой копии не названо"; fail=1; }
want "Г-16, копия не готова: копия удалена" "$(grep -c '^DELETE /backups/bd$' "$FAKE_DIR/calls")" 3

# Копия ресурса пробы под чужим именем (её создал план защиты): уборка по
# префиксу её не найдёт — проба удаляет её по id; копию чужого ресурса не
# трогает. Остаток с именем пробы убирает уборка области (виды backup, pplan).
g16_world
fake_evo_answer disk_create 0 0 '{"disk_id":"3f2b8c1e-1111-4222-8333-44445555aaaa","status":"ok"}'
fake_evo_answer provision 0 1 '{"error":{"reason":"API_ERROR","message":"boom"}}'
cat >"$FAKE_DIR/http" <<'H'
POST /backups 500 {"message":"boom"}
POST /protection-plans 500 {"message":"boom"}
GET! /backups?project_id=p&limit=1000 200 {"items":[],"total":0}
GET! /backups?project_id=p&limit=1000 200 {"items":[{"id":"auto1","name":"plan-copy","resource":{"resource_id":"3f2b8c1e-1111-4222-8333-44445555aaaa"}},{"id":"foreign","name":"other 3f2b8c1e-1111-4222-8333-44445555aaaa","resource":{"resource_id":"zz"}}],"total":2}
GET! /protection-plans?project_id=p&limit=1000 200 {"items":[{"id":"ppleft","name":"evo-e2e-ops-r7a1-g16-pp"},{"id":"ppother","name":"prod-plan"}],"total":2}
GET! /backups?project_id=p&limit=1000 200 {"items":[{"id":"bleft","name":"evo-e2e-ops-r7a1-g16-bd"},{"id":"foreign","name":"other"}],"total":2}
DELETE /backups/* 204
DELETE /protection-plans/* 204
H
rc=0
PROBES=g16 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-16, остатки: код" "$rc" 0
want "Г-16, остатки: удалено" "$(grep '^DELETE' "$FAKE_DIR/calls" | sort | tr '\n' ';')" "DELETE /backups/auto1;DELETE /backups/bleft;DELETE /protection-plans/ppleft;"

# id диска пробы — не UUID (один символ): отбор «чужих» копий по нему
# выключен, ни одна копия не тронута (заключение «Риска» 133, К1).
g16_world
fake_evo_answer disk_create 0 0 '{"disk_id":"a","status":"ok"}'
fake_evo_answer provision 0 1 '{"error":{"reason":"API_ERROR","message":"boom"}}'
cat >"$FAKE_DIR/http" <<'H'
POST /backups 500 {"message":"boom"}
POST /protection-plans 500 {"message":"boom"}
GET! /backups?project_id=p&limit=1000 200 {"items":[],"total":0}
GET! /backups?project_id=p&limit=1000 200 {"items":[{"id":"x1","name":"a-backup","resource":{"resource_id":"a"}},{"id":"x2","name":"data","resource":{"resource_id":"abc"}}],"total":2}
DELETE /backups/* 204
H
rc=0
PROBES=g16 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-16, id не UUID: код" "$rc" 0
if grep -q '^DELETE' "$FAKE_DIR/calls"; then echo "FAIL: Г-16: по id не вида UUID удалена копия: $(grep '^DELETE' "$FAKE_DIR/calls" | tr '\n' ' ')"; fail=1; fi
grep -q 'не UUID: отбор по ним выключен' "$tmp/out" || { echo "FAIL: Г-16: выключенный отбор не назван"; fail=1; }

# Восстановленной ВМ платформа выдала публичный адрес не под именем пробы
# (уборка ищет адреса по имени fip-<префикс> и его не найдёт): проба
# удаляет ВМ и освобождает адрес по id (заключение 133, К2).
g16_world
fake_evo_answer disk_create 0 1 '{"error":{"reason":"API_ERROR","message":"boom"}}'
fake_evo_answer disk_list 0 0 '{"disks":[{"id":"boot1","name":"evo-e2e-ops-r7a1-g16-boot","bootable":true,"disk_type":"SSD-NVMe","attached_to":["evo-e2e-ops-r7a1-g16"]}]}'
fake_evo_answer vm_list 0 0 '{"vms":[{"id":"rvid","name":"evo-e2e-ops-r7a1-g16-rv"}]}'
fake_evo_answer vm_list 1 0 '{"vms":[]}'
fake_evo_answer release_floating_ip 0 0 '{"status":"released"}'
cat >"$FAKE_DIR/http" <<'H'
POST /backups 201 {"id":"bv","state":"created"}
GET! /backups/bv 200 {"id":"bv","state":"created"}
GET! /backups/bv 200 {"id":"bv","state":"created"}
GET /backups/bv 404
POST /backups/bv/action 201 {"id":"bv"}
GET /vms/rvid 200 {"id":"rvid","interfaces":[{"floating_ip":{"id":"fipx","name":"auto-fip"}},{"floating_ip":null}]}
DELETE /backups/bv 204
H
rc=0
PROBES=g16 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
grep -q 'публичных адресов у неё: 1, из них не под именем пробы: 1' "$tmp/out" || { echo "FAIL: Г-16: адрес восстановленной ВМ не назван"; sed 's/^/  | /' "$tmp/out"; fail=1; }
want "Г-16, адрес восстановленной ВМ: ВМ удалена, адрес освобождён" "$(grep -c '^destroy yes=1' "$FAKE_DIR/evo-calls") $(grep -c '^release_floating_ip yes=1' "$FAKE_DIR/evo-calls")" "1 1"
if grep -v '::add-mask::' "$tmp/out" | grep -qw 'fipx\|rvid'; then echo "FAIL: Г-16: id адреса или ВМ в строке лога"; fail=1; fi

# Восстановление ВМ принято, а ВМ не появилась: предупреждение о дисках,
# которые уборка по имени не найдёт.
g16_world
fake_evo_answer disk_create 0 1 '{"error":{"reason":"API_ERROR","message":"boom"}}'
fake_evo_answer disk_list 0 0 '{"disks":[{"id":"boot1","name":"evo-e2e-ops-r7a1-g16-boot","bootable":true,"disk_type":"SSD-NVMe","attached_to":["evo-e2e-ops-r7a1-g16"]}]}'
fake_evo_answer vm_get 0 1 '{"error":{"reason":"NOT_FOUND","message":"нет"}}'
cat >"$FAKE_DIR/http" <<'H'
POST /backups 201 {"id":"bv","state":"created"}
GET! /backups/bv 200 {"id":"bv","state":"created"}
GET! /backups/bv 200 {"id":"bv","state":"created"}
GET /backups/bv 404
POST /backups/bv/action 201 {"id":"bv"}
DELETE /backups/bv 204
H
rc=0
PROBE_WAIT=1 PROBES=g16 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
grep -q '::warning::Г-16: восстановление ВМ принято, но ВМ не появилась' "$tmp/out" || { echo "FAIL: Г-16: нет предупреждения о ВМ, которая не появилась"; fail=1; }

# Список копий не полон (total больше, чем отдано) или не прочитан: вид не
# считается чистым — уборка области падает, а не говорит «остатков нет».
world
: >"$FAKE_DIR/http"
echo 'GET /backups[?]* 200 {"items":[],"total":1001}' >"$FAKE_DIR/http.default"
fake_evo_answer list_security_groups 0 0 '{"security_groups":[]}'
rc=0
PROBES="" "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "список копий не полон и список планов не прочитан: код" "$rc" 1
for s in 'backup: список не прочитан' 'pplan: список не прочитан'; do
	grep -q "$s" "$tmp/out" || { echo "FAIL: непрочитанный список не назван: $s"; sed 's/^/  | /' "$tmp/out"; fail=1; }
done

# Стенд (план 1.9, Г-50): общая уборка завершённых запусков видит в
# списках ВМ, адрес, диск, подсеть, группу, копию и план стенда и ничего из
# них не удаляет; уборка по префиксу стенда — отказ до чтения списков.
# shellcheck source-path=SCRIPTDIR source=e2e_stand.sh
. "$here/e2e_stand.sh"
world
: >"$FAKE_DIR/http"
printf '#!/bin/sh\nexit 0\n' >"$tmp/bin/gh"
chmod +x "$tmp/bin/gh"
S=$STAND_PREFIX
fake_evo_answer vm_list 0 0 '{"vms":[{"id":"svm","name":"'"$S"'-vm"}]}'
fake_evo_answer list_floating_ips 0 0 '{"floating_ips":[{"id":"sfip","name":"fip-'"$S"'-vm-abc123","available":true}]}'
fake_evo_answer disk_list 0 0 '{"disks":[{"id":"sdisk","name":"'"$S"'-data","attached_to":[]}]}'
fake_evo_answer list_subnets 0 0 '{"subnets":[{"id":"ssn","name":"'"$S"'-sn"}]}'
fake_evo_answer list_security_groups 0 0 '{"security_groups":[{"id":"ssg","name":"'"$S"'-sg"}]}'
printf '%s\n' 'GET /backups[?]* 200 {"items":[{"id":"sbk","name":"'"$S"'-bk"}],"total":1}' 'GET /protection-plans[?]* 200 {"items":[{"id":"spp","name":"'"$S"'-pp"}],"total":1}' \
	"GET $RM/tags[?]* 200 {\"tags\":[{\"id\":\"stag\",\"tagKeyValue\":\"$S-tag=x\"}]}" >"$FAKE_DIR/http.default"
rc=0
PROBES=g20 GITHUB_REPOSITORY=o/r GH_TOKEN=t E2E_LIVE_TTL=0 "$here/cloud-live.sh" sweep evo-e2e- ops >"$tmp/out" 2>&1 || rc=$?
want "стенд, общая уборка: код" "$rc" 0
if grep -q '^DELETE' "$FAKE_DIR/calls" || grep -q 'yes=1' "$FAKE_DIR/evo-calls"; then echo "FAIL: общая уборка тронула стенд: $(calls) $(tr '\n' ';' <"$FAKE_DIR/evo-calls")"; fail=1; fi
world
: >"$FAKE_DIR/http"
rc=0
GITHUB_REPOSITORY=o/r GH_TOKEN=t E2E_LIVE_TTL=0 "$here/cloud-live.sh" sweep "$S" ops >"$tmp/out" 2>&1 || rc=$?
want "стенд, уборка по его префиксу: отказ" "$rc" 1
grep -q 'не evo-e2e-\*' "$tmp/out" || { echo "FAIL: отказ уборки по префиксу стенда не назван"; sed 's/^/  | /' "$tmp/out"; fail=1; }
if [ -s "$FAKE_DIR/calls" ] || grep -q 'yes=1' "$FAKE_DIR/evo-calls" 2>/dev/null; then echo "FAIL: уборка по префиксу стенда что-то слала"; fail=1; fi

# Общая уборка без PROJECT (шаг без секрета проекта): списки копий и планов
# не прочитаны — провал, а не молчаливый пропуск вида.
world
: >"$FAKE_DIR/http"
printf '#!/bin/sh\nexit 0\n' >"$tmp/bin/gh"
chmod +x "$tmp/bin/gh"
rc=0
PROJECT='' GITHUB_REPOSITORY=o/r GH_TOKEN=t E2E_LIVE_TTL=0 "$here/cloud-live.sh" sweep evo-e2e-ops-r7a1 ops >"$tmp/out" 2>&1 || rc=$?
want "уборка без PROJECT: код" "$rc" 1
grep -q 'нет PROJECT' "$tmp/out" || { echo "FAIL: уборка без PROJECT не назвала причину"; fail=1; }

# Отказ по квоте останавливает свою пробу, а не следующую: после квоты на
# группе безопасности (Г-11) проба Г-16 делает копию своей ВМ.
g16_world
fake_evo_answer disk_create 0 1 '{"error":{"reason":"API_ERROR","message":"boom"}}'
cat >"$FAKE_DIR/http" <<'H'
POST /security-groups 422 {"code":"quota_exceeded"}
POST /backups 500 {"message":"boom"}
H
rc=0
PROBES="g11 g16" "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "квота в Г-11 не выключает Г-16: копия ВМ запрошена" "$(grep -c '^POST /backups$' "$FAKE_DIR/calls")" 1

# Область dbchange (пробы Г-13…Г-15): изменение готового кластера DBaaS
# прямым PATCH. PostgreSQL: класс на шаг вверх → возврат класса → диск
# больше → число узлов; после каждого принятого шага — ожидание исходного
# состояния кластера. Ресурсов область не создаёт.
S='https://postgresql.api.cloud.ru/v1/clusters/c1'
db_world() {
	world
	fake_evo_answer pg_specs 0 0 '{"specifications":[{"id":"s1","cpu":2,"memory_gb":4,"deployment_mode":"standard","max_hosts":3},{"id":"s3","cpu":8,"memory_gb":16,"deployment_mode":"standard"},{"id":"s2","cpu":4,"memory_gb":8,"deployment_mode":"standard"},{"id":"h2","cpu":4,"memory_gb":4,"deployment_mode":"ha"}]}'
	fake_evo_answer redis_specs 0 0 '{"specifications":[{"id":"s1","cpu":2,"memory_gb":4,"deployment_mode":"standard"},{"id":"s2","cpu":4,"memory_gb":8,"deployment_mode":"standard"}]}'
}
C='"id":"c1","specification_id":"s1","instances":1,"storage":{"pg_data_gb":20}'
db_world
cat >"$FAKE_DIR/http" <<H
GET! $S 200 {$C,"status":"RUNNING"}
PATCH! $S 200 {"id":"op1"}
GET! $S 200 {$C,"status":"UPDATING"}
GET! $S 200 {$C,"status":"RUNNING"}
PATCH! $S 200 {"id":"op2"}
GET! $S 200 {$C,"status":"UPDATING"}
GET! $S 200 {$C,"status":"RUNNING"}
PATCH! $S 400 {"code":"INVALID_ARGUMENT","message":"storage step is 50"}
PATCH! $S 200 {"id":"op4"}
GET! $S 200 {$C,"status":"UPDATING"}
GET $S 200 {$C,"status":"RUNNING"}
H
: >"$FAKE_DIR/bodies"
rc=0
DB_KIND=pg DB_CLUSTER_ID=c1 DB_SETTLE=0 "$here/cloud-live.sh" dbchange >"$tmp/out" 2>&1 || rc=$?
want "Г-13: код" "$rc" 0
want "Г-13: запросы по порядку" "$(sed "s#$S#C#" "$FAKE_DIR/calls" | tr '\n' ';')" "GET C;PATCH C;GET C;GET C;PATCH C;GET C;GET C;PATCH C;PATCH C;GET C;GET C;"
want "Г-13: тела PATCH — класс вверх, возврат, диск, узлы" "$(jq -c 'select(has("keyId") | not) | del(.cluster_id)' "$FAKE_DIR/bodies" | tr '\n' ';')" '{"specification_id":"s2"};{"specification_id":"s1"};{"storage":{"pg_data_gb":30}};{"instances":2};'
for s in 'Г-13: класс на шаг вверх.*HTTP 200.*UPDATING → RUNNING' 'Г-13: возврат класса.*HTTP 200' 'Г-13: диск больше.*отказ HTTP 400.*машинный код: INVALID_ARGUMENT' 'Г-13: число узлов.*HTTP 200'; do
	grep -q "$s" "$tmp/out" || { echo "FAIL: Г-13: нет строки «$s»"; sed 's/^/  | /' "$tmp/out"; fail=1; }
done
if grep -v '::add-mask::' "$tmp/out" | grep -qw 'c1\|op1'; then echo "FAIL: Г-13: id в строке лога"; fail=1; fi
if grep -q '^DELETE\|yes=1' "$FAKE_DIR/calls" "$FAKE_DIR/evo-calls"; then echo "FAIL: dbchange что-то удаляла или создавала через evo"; fail=1; fi

# Отказ по квоте на классе вверх (машинный код): возврата и следующих шагов нет.
db_world
cat >"$FAKE_DIR/http" <<H
GET $S 200 {$C,"status":"RUNNING"}
PATCH $S 429 {"code":"RESOURCE_EXHAUSTED","message":"quota of 3f2b8c1e-1111-4222-8333-444455556666","details":[{"reason":"QUOTA_EXCEEDED"}]}
H
rc=0
DB_KIND=pg DB_CLUSTER_ID=c1 DB_SETTLE=0 "$here/cloud-live.sh" dbchange >"$tmp/out" 2>&1 || rc=$?
want "Г-13, квота: код" "$rc" 0
want "Г-13, квота: один PATCH" "$(grep -c '^PATCH' "$FAKE_DIR/calls")" 1
grep -q 'квота по машинному коду: да' "$tmp/out" || { echo "FAIL: Г-13: квота не названа"; sed 's/^/  | /' "$tmp/out"; fail=1; }
# То же, когда details — объект, а не массив объектов.
db_world
cat >"$FAKE_DIR/http" <<H
GET $S 200 {$C,"status":"RUNNING"}
PATCH $S 429 {"code":"RESOURCE_EXHAUSTED","details":{"reason":"QUOTA_EXCEEDED"}}
H
rc=0
DB_KIND=pg DB_CLUSTER_ID=c1 DB_SETTLE=0 "$here/cloud-live.sh" dbchange >"$tmp/out" 2>&1 || rc=$?
want "Г-13, квота, details — объект: один PATCH" "$(grep -c '^PATCH' "$FAKE_DIR/calls")" 1

# Отказ с пустым телом (прогон 212, DB-4, DB-28): причина — в заголовках
# grpc-status и grpc-message (значение в процентной кодировке gRPC), строка
# называет их; отказ без тела и без этих заголовков так и назван — «не
# реализовано» отличается от «значение не принято».
db_world
cat >"$FAKE_DIR/http" <<H
GET $S 200 {$C,"status":"RUNNING"}
PATCH! $S 501 -
PATCH $S 400 -
H
printf '%s\n' "PATCH $S grpc-status: 12" "PATCH $S grpc-message: method%20is%20not%20implemented%3A%203f2b8c1e-1111-4222-8333-444455556666" >"$FAKE_DIR/http.h"
rc=0
DB_KIND=pg DB_CLUSTER_ID=c1 DB_SETTLE=0 "$here/cloud-live.sh" dbchange >"$tmp/out" 2>&1 || rc=$?
want "Г-13, пустое тело: код" "$rc" 0
grep -q 'Г-13: класс на шаг вверх.*отказ HTTP 501.*машинный код: UNIMPLEMENTED.*тело пустое; grpc-status 12 (UNIMPLEMENTED); grpc-message: method is not implemented: <id>' "$tmp/out" || { echo "FAIL: Г-13: отказ с пустым телом не назван по заголовкам grpc"; sed 's/^/  | /' "$tmp/out"; fail=1; }
if grep -q '3f2b8c1e-1111-4222-8333-444455556666' "$tmp/out"; then echo "FAIL: id из grpc-message в логе"; fail=1; fi
db_world
cat >"$FAKE_DIR/http" <<H
GET $S 200 {$C,"status":"RUNNING"}
PATCH $S 400 -
H
rc=0
DB_KIND=pg DB_CLUSTER_ID=c1 DB_SETTLE=0 "$here/cloud-live.sh" dbchange >"$tmp/out" 2>&1 || rc=$?
grep -q 'Г-13: класс на шаг вверх.*отказ HTTP 400.*машинный код: нет.*текст: тело пустое, заголовков grpc-status и grpc-message нет' "$tmp/out" || { echo "FAIL: Г-13: отказ без тела и без заголовков не назван"; sed 's/^/  | /' "$tmp/out"; fail=1; }
# Код вне 0–16 (ревью Qwen на #1664): назван числом, не пустыми скобками;
# «+» в grpc-message — буква, а не пробел (процентная кодировка gRPC — не
# форма x-www-form-urlencoded).
db_world
cat >"$FAKE_DIR/http" <<H
GET $S 200 {$C,"status":"RUNNING"}
PATCH $S 400 -
H
printf '%s\n' "PATCH $S grpc-status: 17" "PATCH $S grpc-message: a+b%20c" >"$FAKE_DIR/http.h"
rc=0
DB_KIND=pg DB_CLUSTER_ID=c1 DB_SETTLE=0 "$here/cloud-live.sh" dbchange >"$tmp/out" 2>&1 || rc=$?
grep -q 'Г-13: класс на шаг вверх.*машинный код: код 17.*grpc-status 17 (код 17); grpc-message: a+b c' "$tmp/out" || { echo "FAIL: Г-13: код gRPC вне 0–16 или «+» в тексте названы не так"; sed 's/^/  | /' "$tmp/out"; fail=1; }

# Кластер не вернулся в исходное состояние после смены класса: в него
# уходит только возврат класса; PATCH диска и узлов не шлётся — каждый
# PATCH поверх незавершённого повышает шанс кластера, который API не
# удаляет (заключение «Риска» 144, К1).
db_world
cat >"$FAKE_DIR/http" <<H
GET! $S 200 {$C,"status":"RUNNING"}
PATCH $S 200 {"id":"op"}
GET $S 200 {$C,"status":"UPDATING"}
H
: >"$FAKE_DIR/bodies"
rc=0
DB_KIND=pg DB_CLUSTER_ID=c1 DB_WAIT=1 DB_RETURN_WAIT=1 DB_SETTLE=0 "$here/cloud-live.sh" dbchange >"$tmp/out" 2>&1 || rc=$?
want "Г-13, кластер не вернулся: код" "$rc" 0
want "Г-13, кластер не вернулся: только класс и его возврат" "$(jq -c 'select(has("keyId") | not) | del(.cluster_id)' "$FAKE_DIR/bodies" | tr '\n' ';')" '{"specification_id":"s2"};{"specification_id":"s1"};'
grep -q 'кластер не вернулся в RUNNING, дальше не меняю' "$tmp/out" || { echo "FAIL: Г-13: остановка пробы не названа"; sed 's/^/  | /' "$tmp/out"; fail=1; }
# Срок всей пробы вышел: новый шаг не шлётся (К2).
db_world
cat >"$FAKE_DIR/http" <<H
GET $S 200 {$C,"status":"RUNNING"}
PATCH $S 200 {"id":"op"}
H
rc=0
DB_KIND=pg DB_CLUSTER_ID=c1 DB_BUDGET=0 DB_SETTLE=0 "$here/cloud-live.sh" dbchange >"$tmp/out" 2>&1 || rc=$?
want "Г-13, срок пробы вышел: ни одного PATCH" "$(grep -c '^PATCH' "$FAKE_DIR/calls" || true)" 0
grep -q 'срок пробы' "$tmp/out" || { echo "FAIL: Г-13: истёкший срок не назван"; fail=1; }

# Redis: класс вверх и возврат, диск больше, затем одна попытка уменьшить
# диск — ожидаемый отказ записан, кластер не тронут дальше.
R='https://redis.api.cloud.ru/v1/clusters/c1'
db_world
cat >"$FAKE_DIR/http" <<H
PATCH! $R 200 {"id":"op"}
PATCH! $R 200 {"id":"op"}
PATCH! $R 200 {"id":"op"}
PATCH! $R 400 {"code":"FAILED_PRECONDITION","message":"cannot shrink"}
GET $R 200 {"id":"c1","specification_id":"s1","storage_gib":8,"status":"RUNNING"}
H
: >"$FAKE_DIR/bodies"
rc=0
DB_KIND=redis DB_CLUSTER_ID=c1 DB_SETTLE=0 "$here/cloud-live.sh" dbchange >"$tmp/out" 2>&1 || rc=$?
want "Г-15: код" "$rc" 0
want "Г-15: тела PATCH" "$(jq -c 'select(has("keyId") | not) | del(.cluster_id)' "$FAKE_DIR/bodies" | tr '\n' ';')" '{"specification_id":"s2"};{"specification_id":"s1"};{"storage_gib":9};{"storage_gib":8};'
grep -q 'Г-15: диск меньше (ожидаемый отказ).*отказ HTTP 400' "$tmp/out" || { echo "FAIL: Г-15: отказ уменьшения диска не записан"; sed 's/^/  | /' "$tmp/out"; fail=1; }

# Kafka: диск больше, класс вверх, возврат — поле диска storage_gib.
K='https://kafka.api.cloud.ru/v1/clusters/c1'
db_world
fake_evo_answer kafka_specs 0 0 '{"specifications":[{"id":"k1","cpu":2,"memory_gb":4,"deployment_mode":"standard"},{"id":"k2","cpu":4,"memory_gb":8,"deployment_mode":"standard"}]}'
cat >"$FAKE_DIR/http" <<H
PATCH $K 200 {"id":"op"}
GET $K 200 {"cluster":{"id":"c1","specification_id":"k1","storage_gib":100,"status":"RUNNING"}}
H
: >"$FAKE_DIR/bodies"
rc=0
DB_KIND=kafka DB_CLUSTER_ID=c1 DB_SETTLE=0 "$here/cloud-live.sh" dbchange >"$tmp/out" 2>&1 || rc=$?
want "Г-14: код" "$rc" 0
want "Г-14: тела PATCH" "$(jq -c 'select(has("keyId") | not) | del(.cluster_id)' "$FAKE_DIR/bodies" | tr '\n' ';')" '{"storage_gib":110};{"specification_id":"k2"};{"specification_id":"k1"};'
grep -q 'Г-14: возврат класса.*HTTP 200' "$tmp/out" || { echo "FAIL: Г-14: возврат класса не записан"; sed 's/^/  | /' "$tmp/out"; fail=1; }

# Нет кластера или вида — область отказывает, запросов нет.
db_world
: >"$FAKE_DIR/http"
rc=0
DB_KIND=mysql DB_CLUSTER_ID=c1 "$here/cloud-live.sh" dbchange >"$tmp/out" 2>&1 || rc=$?
want "dbchange, чужой вид: код" "$rc" 1
[ ! -s "$FAKE_DIR/calls" ] || { echo "FAIL: dbchange с чужим видом слала запросы"; fail=1; }

# Область k8schange (проба Г-12 с Г6): группа узлов кластера job k8s-smoke
# прямыми запросами mk8s v3 — масштаб 1→2 и повтор сразу, 2→1, версия
# кластера, затем группы, вторая группа (evo) и её удаление, пока она
# создаётся, масштаб до 0, удаление последней группы. Каждая строка —
# первое чтение после ответа и форма error операции; отказ — ответ, не
# провал; шаг поверх незавершённого и после срока пробы не шлётся.
MK=https://mk8s.api.cloud.ru/v3
P=$MK/nodePools/np1 P2=$MK/nodePools/np2 KC=$MK/clusters/c1 O=$MK/operations
pool() { printf '{"id":"%s","status":"OBJECT_STATUS_%s","scalePolicy":{"fixedScale":{"count":%s}},"version":"v1.31.1"}' "${3:-np1}" "$1" "$2"; }
k8s_world() {
	world
	fake_evo_answer k8s_nodepools 0 0 '{"node_pools":[{"id":"np1","name":"np-e2e","status":"OBJECT_STATUS_RUNNING","desired":1}]}'
	fake_evo_answer k8s_add_nodepool 0 0 '{"status":"creating","nodepool_id":"np2","operation_id":"op9"}'
}
k8s_world
cat >"$FAKE_DIR/http" <<H
PATCH! $P 200 {"id":"op1","done":false}
GET! $P 200 $(pool RUNNING 2)
PATCH! $P 200 {"id":"op2","done":false}
GET! $O/op1 200 {"id":"op1","done":false,"error":{}}
GET! $P 200 $(pool SCALING_UP 2)
GET! $P 200 $(pool RUNNING 2)
GET! $O/op1 200 {"id":"op1","done":true}
GET! $O/op2 200 {"id":"op2","done":true,"error":null}
PATCH! $P 200 {"id":"op3"}
GET! $P 200 $(pool SCALING_DOWN 1)
GET! $P 200 $(pool RUNNING 1)
POST $KC:upgrade 200 {"id":"op4"}
GET! $KC 200 {"id":"c1","status":"OBJECT_STATUS_UPGRADING","controlPlane":{"version":"v1.31.1"}}
GET! $KC 200 {"id":"c1","status":"OBJECT_STATUS_RUNNING","controlPlane":{"version":"v1.32.1"}}
POST $P:upgrade 400 {"code":"FAILED_PRECONDITION","message":"pool is busy"}
DELETE! $P2 409 {"code":"FAILED_PRECONDITION","message":"node pool is being created"}
GET! $P2 200 $(pool PROVISIONING 1 np2)
GET! $P2 200 $(pool RUNNING 1 np2)
DELETE! $P2 200 {"id":"op5"}
GET! $P2 200 $(pool DELETING 1 np2)
GET! $P2 404 {"code":"NOT_FOUND"}
DELETE! $P2 404 {"code":"NOT_FOUND","message":"node pool not found"}
PATCH! $P 400 {"code":"INVALID_ARGUMENT","message":"count must be at least 1"}
DELETE! $P 200 {"id":"op6"}
GET! $P 200 $(pool DELETING 1)
GET $P 404 {"code":"NOT_FOUND"}
GET $O/* 200 {"done":true,"error":{"code":0}}
H
: >"$FAKE_DIR/bodies"
rc=0
K8S_CLUSTER_ID=c1 K8S_UPGRADE_TO=v1.32.1 K8S_SETTLE=1000 "$here/cloud-live.sh" k8schange >"$tmp/out" 2>&1 || rc=$?
want "Г-12: код" "$rc" 0
want "Г-12: тела запросов — масштаб 2, повтор 2, 1, версия кластера и группы, 0" "$(jq -c 'select(has("keyId") | not)' "$FAKE_DIR/bodies" | tr '\n' ';')" '{"scalePolicy":{"fixedScale":{"count":2}}};{"scalePolicy":{"fixedScale":{"count":2}}};{"scalePolicy":{"fixedScale":{"count":1}}};{"version":"v1.32.1"};{"version":"v1.32.1"};{"scalePolicy":{"fixedScale":{"count":0}}};'
# Обновление версии — долгий и необратимый шаг — идёт после второй группы:
# зависшее обновление не съедает её (заключение «Риска» 189, У1).
first() { grep -n "$1" "$FAKE_DIR/calls" | head -1 | cut -d: -f1; }
[ "$(first "^DELETE $P2")" -lt "$(first ':upgrade$')" ] || { echo "FAIL: Г-12: обновление версии раньше второй группы"; sed 's/^/  | /' "$FAKE_DIR/calls"; fail=1; }
want "Г-12: удаления — вторая группа (отказ, готовая, повтор) и последняя" "$(grep '^DELETE' "$FAKE_DIR/calls" | sed "s#$MK##" | tr '\n' ';')" "DELETE /nodePools/np2;DELETE /nodePools/np2;DELETE /nodePools/np2;DELETE /nodePools/np1;"
for s in 'Г-12: масштаб 1→2.*первое чтение после ответа: OBJECT_STATUS_RUNNING/2 узл.*повтор сразу: HTTP 200, вторая операция; операция сразу: done=false, error: {}; состояния: .*SCALING_UP.*операция в конце: done=true, error: нет поля; вторая операция в конце: done=true, error: null' \
	'Г-12: масштаб 2→1.*SCALING_DOWN/1 узл.* → OBJECT_STATUS_RUNNING/1 узл' \
	'Г-12: обновление версии кластера.*UPGRADING/v1.31.1 → OBJECT_STATUS_RUNNING/v1.32.1.*операция в конце: done=true, error: объект, ключи code' \
	'Г-12: обновление версии группы.*отказ HTTP 400.*машинный код: FAILED_PRECONDITION' \
	'Г-12: удаление создающейся группы.*отказ HTTP 409' 'Г-12: вторая группа готова.*PROVISIONING.* → OBJECT_STATUS_RUNNING' \
	'Г-12: удаление готовой группы.*DELETING/1 узл.* → нет (404)' 'Г-12: повтор удаления после исчезновения.*отказ HTTP 404.*NOT_FOUND' \
	'Г-12: масштаб 1→0.*отказ HTTP 400.*evo сегодня: k8s_nodepool_scale' 'Г-12: удаление последней группы.*DELETING/1 узл.* → нет (404)'; do
	grep -q "$s" "$tmp/out" || { echo "FAIL: Г-12: нет строки «$s»"; sed 's/^/  | /' "$tmp/out"; fail=1; }
done
if grep -v '::add-mask::' "$tmp/out" | grep -qw 'c1\|np1\|np2\|op1\|op2'; then echo "FAIL: Г-12: id в строке лога"; fail=1; fi
want "Г-12: через evo создана только вторая группа" "$(grep -c 'yes=1' "$FAKE_DIR/evo-calls")" 1
grep -q '^k8s_add_nodepool yes=1' "$FAKE_DIR/evo-calls" || { echo "FAIL: Г-12: вторая группа не через evo"; fail=1; }

# Масштаб 1→2 не дошёл до конца (группа так и в SCALING_UP): дальше ни
# одного запроса записи — масштаб, версия и удаления поверх незавершённого
# не шлются; вторая группа не создаётся.
k8s_world
cat >"$FAKE_DIR/http" <<H
PATCH $P 200 {"id":"op1"}
GET $P 200 $(pool SCALING_UP 2)
GET $O/* 200 {"done":false}
H
: >"$FAKE_DIR/bodies"
rc=0
K8S_CLUSTER_ID=c1 K8S_UPGRADE_TO=v1.32.1 K8S_WAIT=1 "$here/cloud-live.sh" k8schange >"$tmp/out" 2>&1 || rc=$?
want "Г-12, группа не устоялась: код" "$rc" 0
want "Г-12, группа не устоялась: только масштаб и его повтор" "$(grep -c '^PATCH' "$FAKE_DIR/calls")" 2
want "Г-12, группа не устоялась: ни версии, ни удалений" "$(grep -c '^POST\|^DELETE' "$FAKE_DIR/calls" || true)" 0
if grep -q 'k8s_add_nodepool' "$FAKE_DIR/evo-calls"; then echo "FAIL: Г-12: вторая группа поверх незавершённого масштаба"; fail=1; fi
grep -q 'прошлый шаг не завершился' "$tmp/out" || { echo "FAIL: Г-12: остановка не названа"; sed 's/^/  | /' "$tmp/out"; fail=1; }

# Срок пробы вышел — ни одного запроса записи; версии некуда обновлять —
# строка «не покрыто», запросов обновления нет.
k8s_world
: >"$FAKE_DIR/http"
rc=0
K8S_CLUSTER_ID=c1 K8S_BUDGET=0 "$here/cloud-live.sh" k8schange >"$tmp/out" 2>&1 || rc=$?
want "Г-12, срок вышел: код" "$rc" 0
want "Г-12, срок вышел: запросов к mk8s нет" "$(if [ -f "$FAKE_DIR/calls" ]; then grep -c mk8s "$FAKE_DIR/calls" || true; else echo 0; fi)" 0
grep -q 'нет K8S_UPGRADE_TO' "$tmp/out" || { echo "FAIL: Г-12: версия без цели не названа"; fail=1; }
grep -q 'срок пробы' "$tmp/out" || { echo "FAIL: Г-12: истёкший срок не назван"; fail=1; }

# Не одна группа в кластере или нет кластера — запросов нет.
k8s_world
fake_evo_answer k8s_nodepools 0 0 '{"node_pools":[{"id":"np1"},{"id":"np3"}]}'
: >"$FAKE_DIR/http"
rc=0
K8S_CLUSTER_ID=c1 "$here/cloud-live.sh" k8schange >"$tmp/out" 2>&1 || rc=$?
want "Г-12, две группы: код" "$rc" 0
[ ! -s "$FAKE_DIR/calls" ] || { echo "FAIL: Г-12 с двумя группами слала запросы"; fail=1; }
k8s_world
rc=0
"$here/cloud-live.sh" k8schange >"$tmp/out" 2>&1 || rc=$?
want "Г-12 без кластера: код" "$rc" 1
[ ! -s "$FAKE_DIR/calls" ] || { echo "FAIL: Г-12 без кластера слала запросы"; fail=1; }

# Слова проб нет — область ничего не шлёт.
world
: >"$FAKE_DIR/http"
fake_evo_answer list_security_groups 0 0 '{"security_groups":[]}'
rc=0
PROBES="" "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "без слов проб: код" "$rc" 0
want "без слов проб: только списки уборки" "$(calls)" "$SW"
# Строка списка — только «id<TAB>имя»: команда маски токена в список не
# попадает (иначе уборка шлёт DELETE по ней).
if grep -q '^DELETE' "$FAKE_DIR/calls"; then echo "FAIL: уборка без остатков что-то удаляла: $(calls)"; fail=1; fi

# Г-17: первое чтение тегов → тег в «Менеджере ресурсов»: создание, чтение,
# изменение → диск и ВМ с tag_ids в запросе создания → отбор по тегу в
# Compute и в «Менеджере» → диск с tag_names → снятие и назначение диску →
# ВМ удалена → удаление назначенного тега и повтор → диски удалены.
# Заключение «Риска» 183: запись — только после чтения (У2), только по id
# своего тега и на свои ВМ и диск (У3), удаление тега — после удаления ВМ
# (У4); все запросы к «Менеджеру» — на хост из спецификации (У1).
G=evo-e2e-ops-r7a1-g17
g17_world() {
	world
	fake_evo_answer list_subnets 0 0 '{"subnets":[{"id":"sn0","name":"default","availability_zone_id":"z2"}]}'
	fake_evo_answer image_get 0 0 '{"fields":[{"name":"hostname","filled_by":"evo","role":"vm_name"},{"name":"password","filled_by":"evo","role":"password"}]}'
	fake_evo_answer destroy 0 0 '{"status":"deleted"}'
	fake_evo_answer disk_delete 0 0 '{"status":"deleted"}'
	fake_evo_answer list_security_groups 0 0 '{"security_groups":[]}'
}
g17_world
cat >"$FAKE_DIR/http" <<H
GET! $RM/tags[?]projectId=p&pageSize=100 200 {"tags":[{"id":"other","tagKeyValue":"env=prod","projectId":"p"}]}
GET! $RM/tags[?]projectId=p&pageSize=100 200 {"tags":[{"id":"other","tagKeyValue":"env=prod","projectId":"p"},{"id":"t1","tagKeyValue":"$G=probe","projectId":"p"}]}
GET! $RM/tags[?]projectId=p&pageSize=100 200 {"tags":[{"id":"other","tagKeyValue":"env=prod","projectId":"p"},{"id":"t1","tagKeyValue":"$G=probe","projectId":"p"}]}
POST $RM/tags 200 {"id":"t1","tagKeyValue":"$G=probe","projectId":"p","customerId":"c-secret"}
GET $RM/tags/t1[?]* 200 {"id":"t1","tagKeyValue":"$G=probe","projectId":"p"}
GET $RM/tags[?]projectId=p&key=* 200 {"tags":[{"id":"t1"}]}
PATCH $RM/tags/t1 200 {"id":"t1"}
POST! /disks 201 {"id":"d1"}
POST! /disks 201 {"id":"d2"}
GET /disks/d1 200 {"id":"d1","tags":[{"id":"t1","name":"$G"}]}
GET /disks/d2 200 {"id":"d2","tags":[{"id":"t1","name":"$G"}]}
POST .1/vms 201 [{"id":"vm1"}]
GET /vms/vm1 200 {"id":"vm1","tags":[{"id":"t1","name":"$G"}]}
GET /vms[?]* 200 {"items":[{"id":"vm1"}]}
GET $RM/resources[?]* 200 {"resources":[{"resourceId":"d1","resourceTypeName":"disk"},{"resourceId":"vm1","resourceTypeName":"vm"}]}
POST $RM/tags/t1/unassignResource 200 {}
POST $RM/tags/t1/assignResource 200 {}
DELETE! $RM/tags/t1[?]projectId=p 200 {}
DELETE $RM/tags/t1[?]projectId=p 404 {"code":5,"message":"not found"}
H
rc=0
PROBE_WAIT=1 PROBES=g17 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-17: код" "$rc" 0
want "Г-17: запросы по порядку" "$(calls)" "${TAGS}POST $RM/tags;${TAGS}GET $RM/tags/t1?projectId=p;GET $RM/tags?projectId=p&key=$G;PATCH $RM/tags/t1;POST /disks;GET /disks/d1;POST .1/vms;GET /vms/vm1;GET /vms?project_id=p&tag_ids=t1&limit=100;GET $RM/resources?projectIds=p&tagIds=t1&pageSize=100;POST /disks;GET /disks/d2;${TAGS}POST $RM/tags/t1/unassignResource;GET /disks/d1;POST $RM/tags/t1/assignResource;GET /disks/d1;DELETE $RM/tags/t1?projectId=p;DELETE $RM/tags/t1?projectId=p;GET /disks/d1;${TAGS}$SW"
want "Г-17: тело создания тега" "$(jq -c . "$FAKE_DIR/body.POST.https:..resource-manager.api.cloud.ru.v1.tags" 2>/dev/null)" "{\"key\":\"$G\",\"value\":\"probe\",\"description\":\"evo e2e probe\",\"projectId\":\"p\"}"
want "Г-17: ВМ и оба диска удалены" "$(grep -c '^destroy yes=1' "$FAKE_DIR/evo-calls") $(grep -c '^disk_delete yes=1' "$FAKE_DIR/evo-calls")" "1 2"
for s in 'Г-17: чтение тегов до записи.*тегов: 1.*тегов другого проекта в ответе: 0' "Г-17: создание тега.*пара в ответе: «$G=probe», проект тега: проект пробы" 'Г-17: чтение тега.*список уборки показывает созданный тег: да.*отбор по ключу — HTTP 200, найдено: 1' 'Г-17: изменение тега.*HTTP 200' 'Г-17: диск с tag_ids в запросе создания.*HTTP 201; tags в карточке диска: 1, поля тега: \["id","name"\]' 'Г-17: ВМ с tag_ids в запросе создания.*tags в карточке ВМ: 1.*отбор ВМ по tag_ids — HTTP 200, найдено: 1' 'Г-17: ресурсы по тегу в «Менеджере».*найдено: 2, виды: \["disk","vm"\]' 'Г-17: диск с tag_names.*id тега тот же, что у тега «Менеджера»: да; тегов с ключом пробы в «Менеджере»: 1' 'Г-17: снятие и назначение тега диску.*снятие — HTTP 200.*назначение — HTTP 200' 'Г-17: удаление тега, назначенного диску.*HTTP 200, повтор — HTTP 404.*tags диска после удаления тега: 1.*в «Менеджере»: 0'; do
	grep -q "$s" "$tmp/out" || { echo "FAIL: Г-17: нет строки «$s»"; grep 'Г-17' "$tmp/out" | sed 's/^/  | /'; fail=1; }
done
# Тело ВМ несёт tag_ids; в журнале нет id и значений полей тега (в объекте
# тега — организация: customerId).
jq -e '.[0].tag_ids == ["t1"]' "$FAKE_DIR/body.POST.https:..compute.api.cloud.ru.api.v1.1.vms" >/dev/null 2>&1 || { echo "FAIL: Г-17: в теле ВМ нет tag_ids"; fail=1; }
if grep -v '::add-mask::' "$tmp/out" | grep -q 't1\|vm1\|c-secret\|env=prod'; then echo "FAIL: Г-17: id или значение чужого поля в строке лога"; grep -v '::add-mask::' "$tmp/out" | grep 't1\|vm1\|c-secret\|env=prod' | sed 's/^/  | /'; fail=1; fi
if grep -q '::warning::Г-17' "$tmp/out"; then echo "FAIL: Г-17: предупреждение при одной сущности"; fail=1; fi
# У3: ни одной записи по чужому тегу и ни одного запроса вне двух хостов.
if grep -E '^(POST|PATCH|DELETE|PUT) ' "$FAKE_DIR/calls" | grep -q 'other'; then echo "FAIL: Г-17: запись по чужому тегу"; fail=1; fi
if grep -q '^[A-Z]* https://' "$FAKE_DIR/calls" && grep '^[A-Z]* https://' "$FAKE_DIR/calls" | grep -qv "^[A-Z]* $RM/"; then echo "FAIL: Г-17: запрос на хост вне Compute и «Менеджера»"; fail=1; fi

# У2: первое чтение не прошло — ни одной записи.
g17_world
echo "GET! $RM/tags[?]* 403 {\"code\":7,\"message\":\"permission denied\"}" >"$FAKE_DIR/http"
rc=0
PROBES=g17 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-17, чтение отказано: код" "$rc" 0
want "Г-17, чтение отказано: записи нет" "$(calls)" "$TAGS$SW"
grep -q 'Г-17.*не покрыто.*первое чтение тегов не прошло: HTTP 403.*запись не начинаю' "$tmp/out" || { echo "FAIL: Г-17: отказ первого чтения не назван"; grep 'Г-17' "$tmp/out" | sed 's/^/  | /'; fail=1; }

# У2: список проекта отдаёт тег другого проекта — тег шире проекта, записи нет.
g17_world
echo "GET! $RM/tags[?]* 200 {\"tags\":[{\"id\":\"o\",\"tagKeyValue\":\"a=b\",\"projectId\":\"another\"}]}" >"$FAKE_DIR/http"
rc=0
PROBES=g17 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-17, тег шире проекта: записи нет" "$(calls)" "$TAGS$SW"
grep -q 'Г-17.*тег шире проекта — не покрыто' "$tmp/out" || { echo "FAIL: Г-17: «тег шире проекта» не назван"; fail=1; }

# У5: tag_names принят, а тег на диске — не тег «Менеджера»: предупреждение
# об остатке неизвестной сущности; со словом g17nonames во входе probes шаг не идёт.
g17_world
cat >"$FAKE_DIR/http" <<H
GET! $RM/tags[?]projectId=p&pageSize=100 200 {"tags":[]}
GET! $RM/tags[?]projectId=p&pageSize=100 200 {"tags":[{"id":"t1","tagKeyValue":"$G=probe","projectId":"p"}]}
GET! $RM/tags[?]projectId=p&pageSize=100 200 {"tags":[{"id":"t1","tagKeyValue":"$G=probe","projectId":"p"}]}
POST $RM/tags 200 {"id":"t1","tagKeyValue":"$G=probe","projectId":"p"}
POST! /disks 422 {"code":"tag_not_found","message":"no such tag"}
POST! /disks 201 {"id":"d1"}
POST! /disks 201 {"id":"d2"}
GET /disks/d1 200 {"id":"d1","tags":[]}
GET /disks/d2 200 {"id":"d2","tags":[{"id":"zz","name":"$G"}]}
POST .1/vms 422 {"code":"tag_not_found","message":"no such tag"}
DELETE $RM/tags/t1[?]* 200 {}
H
rc=0
PROBE_WAIT=1 PROBES=g17 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-17, две сущности: код" "$rc" 0
want "Г-17, две сущности: три запроса диска (с tag_ids, без тега, с tag_names)" "$(grep -c '^POST /disks$' "$FAKE_DIR/calls")" 3
for s in 'Г-17: диск с tag_ids в запросе создания.*отказ HTTP 422.*машинный код: tag_not_found' 'Г-17: ВМ с tag_ids в запросе создания.*отказ HTTP 422' 'Г-17: диск с tag_names.*id тега тот же, что у тега «Менеджера»: нет; тегов с ключом пробы в «Менеджере»: 1' '::warning::Г-17: диск с tag_names принят, а тег на нём — не тег «Менеджера»'; do
	grep -q "$s" "$tmp/out" || { echo "FAIL: Г-17, две сущности: нет строки «$s»"; grep 'Г-17' "$tmp/out" | sed 's/^/  | /'; fail=1; }
done
if grep -q 'квота по машинному коду' "$tmp/out"; then echo "FAIL: Г-17: отказ не по квоте назван квотой"; fail=1; fi
g17_world
cat >"$FAKE_DIR/http" <<H
POST $RM/tags 200 {"id":"t1","tagKeyValue":"$G=probe","projectId":"p"}
POST /disks 201 {"id":"d1"}
GET /disks/d1 200 {"id":"d1","tags":[]}
POST .1/vms 422 {"code":"x","message":"no"}
DELETE $RM/tags/t1[?]* 200 {}
H
rc=0
PROBE_WAIT=1 PROBES="g17 g17nonames" "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-17, без tag_names: один запрос диска" "$(grep -c '^POST /disks$' "$FAKE_DIR/calls")" 1
grep -q 'Г-17: диск с tag_names.*не повторяется: остаток неубираем' "$tmp/out" || { echo "FAIL: Г-17: пропуск tag_names не назван"; fail=1; }
# У7: тег создан, а список, которым читает уборка, его не показывает (в этом
# мире список отвечает объектом без tags): предупреждение, и счёт «тегов
# пробы в Менеджере» — «не установлено», а не зелёный ноль. Свой тег проба
# удаляет по id.
g17_world
cat >"$FAKE_DIR/http" <<H
GET $RM/tags[?]* 200 {}
POST $RM/tags 200 {"id":"t1","tagKeyValue":"$G=probe","projectId":"p"}
POST /disks 201 {"id":"d1"}
GET /disks/d1 200 {"id":"d1","tags":[]}
POST .1/vms 422 {"code":"x","message":"no"}
DELETE $RM/tags/t1[?]* 200 {}
H
rc=0
PROBE_WAIT=1 PROBES="g17 g17nonames" "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-17, список слеп: код" "$rc" 0
for s in '::warning::Г-17: список тегов не показывает созданный тег: уборка по списку слепа' 'Г-17: чтение тега.*список уборки показывает созданный тег: нет' 'Г-17: удаление тега, назначенного диску.*тегов с ключом пробы в «Менеджере»: не установлено'; do
	grep -q "$s" "$tmp/out" || { echo "FAIL: Г-17, список слеп: нет строки «$s»"; grep 'Г-17' "$tmp/out" | sed 's/^/  | /'; fail=1; }
done
want "Г-17, список слеп: свой тег удалён по id" "$(grep -c "^DELETE $RM/tags/t1?projectId=p$" "$FAKE_DIR/calls")" 2

# Уборка области: тег пробы, оставшийся после обрыва, удаляется по ключу;
# чужой тег проекта — нет. Тег читается постранично.
world
cat >"$FAKE_DIR/http" <<H
GET! $RM/tags[?]projectId=p&pageSize=100 200 {"tags":[{"id":"other","tagKeyValue":"env=prod"}],"nextPageToken":"n2"}
GET! $RM/tags[?]projectId=p&pageSize=100&pageToken=n2 200 {"tags":[{"id":"tleft","tagKeyValue":"$G=probe"}]}
DELETE $RM/tags/tleft[?]projectId=p 200 {}
H
fake_evo_answer list_security_groups 0 0 '{"security_groups":[]}'
rc=0
PROBES="" "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "уборка тега: код" "$rc" 0
want "уборка тега: удалён только тег пробы" "$(grep '^DELETE' "$FAKE_DIR/calls" | tr '\n' ';')" "DELETE $RM/tags/tleft?projectId=p;"
# Проект без тегов отвечает объектом без поля tags — это пустой список, а
# не «не прочитан»: уборка зелёная. Ответ не объект — не прочитан.
world
echo "GET $RM/tags[?]* 200 {}" >"$FAKE_DIR/http"
fake_evo_answer list_security_groups 0 0 '{"security_groups":[]}'
rc=0
PROBES="" "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "уборка тега, ответ без tags: код" "$rc" 0
world
echo "GET $RM/tags[?]* 200 []" >"$FAKE_DIR/http"
fake_evo_answer list_security_groups 0 0 '{"security_groups":[]}'
rc=0
SWEEP_WAIT=1 PROBES="" "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
[ "$rc" != 0 ] || { echo "FAIL: уборка тега: ответ-массив вместо объекта — зелёная уборка"; fail=1; }
# Список тегов не прочитан — вид не считается чистым: уборка красная.
world
echo "GET $RM/tags[?]* 500 {\"message\":\"boom\"}" >"$FAKE_DIR/http"
fake_evo_answer list_security_groups 0 0 '{"security_groups":[]}'
rc=0
SWEEP_WAIT=1 PROBES="" "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
[ "$rc" != 0 ] || { echo "FAIL: уборка тега: непрочитанный список тегов — зелёная уборка"; fail=1; }

[ "$fail" = 0 ] && echo "cloud-live ops: ok"
exit "$fail"
