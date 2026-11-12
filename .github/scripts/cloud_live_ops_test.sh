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
# очереди). Нет подходящей — так же по $FAKE_DIR/http.default (пустые
# списки копий и планов для уборки области); нет и там — 404 с телом {}.
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
: >"$FAKE_DIR/body"
case "$url" in */auth/token) printf '{"access_token":"tok"}'; exit 0 ;; esac
echo "$method ${url#https://compute.api.cloud.ru/api/v1}" >>"$FAKE_DIR/calls"
[ -z "$hdr" ] || : >"$hdr"
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
printf '%s' "$body" >"$out"
printf '%s' "$code"
FAKE
chmod +x "$tmp/bin/curl"
export PATH="$tmp/bin:$PATH" KEY_ID=k SECRET=s PROJECT=p RUN=r7a1 ZONE=ru.AZ-2 GITHUB_STEP_SUMMARY=/dev/null PROBE_PAUSE=0
fail=0
n=0
world() {
	n=$((n + 1))
	export FAKE_DIR="$tmp/w$n" RUNNER_TEMP="$tmp/w$n"
	mkdir "$FAKE_DIR"
	fake_evo_answer list_zones 0 0 '{"zones":[{"id":"z2","name":"ru.AZ-2"}]}'
	fake_evo_answer vm_list 0 0 '{"vms":[]}'
	fake_evo_answer list_floating_ips 0 0 '{"floating_ips":[]}'
	fake_evo_answer disk_list 0 0 '{"disks":[]}'
	printf '%s\n' 'GET /backups[?]* 200 {"items":[],"total":0}' 'GET /protection-plans[?]* 200 {"items":[],"total":0}' >"$FAKE_DIR/http.default"
}
# Уборка области читает списки планов защиты и копий (виды pplan и backup)
# дважды: в цикле удаления и при проверке остатков.
SW='GET /protection-plans?project_id=p&limit=1000;GET /backups?project_id=p&limit=1000;'
SW="$SW$SW"
want() { if [ "$2" != "$3" ]; then printf 'FAIL %s\n  got:  %s\n  want: %s\n' "$1" "$2" "$3"; fail=1; fi; }
calls() { tr '\n' ';' <"$FAKE_DIR/calls"; }

# Г-11: группа → пять правил → чтение → повтор правила → удаление правила
# и его повтор → удаление группы и его повтор. Группы после пробы нет.
world
cat >"$FAKE_DIR/http" <<'H'
POST /security-groups 201 {"id":"sg1","name":"evo-e2e-ops-r7a1-sg"}
POST /security-groups/sg1/rules 201 {"id":"rule1"}
GET /security-groups/sg1 200 {"id":"sg1","rules":[{"id":"rule1","remote_ip_prefix":"0.0.0.0/0"}]}
DELETE /security-groups/sg1/rules/rule1 204
DELETE /security-groups/sg1 204
H
fake_evo_answer list_security_groups 0 0 '{"security_groups":[]}'
rc=0
PROBES=g11 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-11: код" "$rc" 0
want "Г-11: запросы по порядку" "$(calls)" "POST /security-groups;POST /security-groups/sg1/rules;POST /security-groups/sg1/rules;POST /security-groups/sg1/rules;POST /security-groups/sg1/rules;POST /security-groups/sg1/rules;GET /security-groups/sg1;POST /security-groups/sg1/rules;GET /security-groups/sg1;DELETE /security-groups/sg1/rules/rule1;DELETE /security-groups/sg1/rules/rule1;DELETE /security-groups/sg1;DELETE /security-groups/sg1;$SW"
grep -q 'Г-11: повтор создания того же правила' "$tmp/out" || { echo "FAIL: нет строки о повторе правила"; fail=1; }
if grep -v '::add-mask::' "$tmp/out" | grep -q 'sg1\|rule1'; then echo "FAIL: id в строке лога"; fail=1; fi

# Группа не создана (отказ платформы): дальше проба не идёт, код 0 — это
# ответ платформы, а не провал; правил и удалений нет.
world
echo 'POST /security-groups 422 {"code":"quota_exceeded","message":"limit of 3f2b8c1e-1111-4222-8333-444455556666"}' >"$FAKE_DIR/http"
fake_evo_answer list_security_groups 0 0 '{"security_groups":[]}'
rc=0
PROBES=g11 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-11, отказ создания: код" "$rc" 0
want "Г-11, отказ создания: один запрос" "$(calls)" "POST /security-groups;$SW"
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
PROBES=g11 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-11, остаток: код" "$rc" 0
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
fake_evo_answer disk_list 0 0 '{"disks":[{"id":"boot1","name":"evo-e2e-ops-r7a1-g16-boot","bootable":true,"attached_to":["evo-e2e-ops-r7a1-g16"]},{"id":"rd1","name":"evo-e2e-ops-r7a1-g16-rd","attached_to":[]},{"id":"rb1","name":"evo-e2e-ops-r7a1-g16-rv-boot","bootable":true,"attached_to":["evo-e2e-ops-r7a1-g16-rv"]}]}'
# disk_list читают: появление восстановленного диска, загрузочный диск ВМ,
# диски восстановленной ВМ, диск после удаления копии; дальше — уборка.
for i in 1 2 3; do cp "$FAKE_DIR/disk_list.0.json" "$FAKE_DIR/disk_list.$i.json"; done
fake_evo_answer disk_list 4 0 '{"disks":[]}'
cat >"$FAKE_DIR/http" <<'H'
POST! /backups 201 {"id":"bd","state":"creating"}
POST! /backups 201 {"id":"bv","state":"creating"}
GET! /backups/bd 200 {"id":"bd","state":"creating","size":0}
GET! /backups/bd 200 {"id":"bd","state":"created","size":5,"type":"full"}
GET! /backups/bd 200 {"id":"bd","state":"created","size":5,"last_restore_time":"t"}
GET! /backups/bd 200 {"id":"bd","state":"deleting"}
GET /backups/bd 404 {"code":"not_found"}
GET! /backups/bv 200 {"id":"bv","state":"created","size":9}
GET! /backups/bv 200 {"id":"bv","state":"created","size":9}
GET /backups/bv 404
GET! /backups?project_id=p&limit=1000 200 {"items":[{"id":"bd","name":"evo-e2e-ops-r7a1-g16-bd"},{"id":"bv","name":"evo-e2e-ops-r7a1-g16-bv"},{"id":"x","name":"someone"}],"total":3}
GET /backups?project_id=p&name=evo-e2e-ops-r7a1-g16-bd 200 {"items":[{"id":"bd","name":"evo-e2e-ops-r7a1-g16-bd"}],"total":1}
POST /backups/bd/action 201 {"id":"bd","state":"updating"}
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
L='?project_id=p&limit=1000'
want "Г-16: запросы по порядку" "$(calls)" "POST /backups;GET /backups/bd;GET /backups/bd;POST /backups;GET /backups/bv;GET /backups$L;GET /backups?project_id=p&name=evo-e2e-ops-r7a1-g16-bd;POST /backups/bd/action;GET /backups/bd;POST /backups/bv/action;GET /backups/bv;DELETE /backups/bd;DELETE /backups/bd;GET /backups/bd;GET /backups/bd;DELETE /backups/bd;DELETE /backups/bv;GET /backups/bv;POST /protection-plans;GET /protection-plans/pp;GET /protection-plans$L;DELETE /protection-plans/pp;DELETE /protection-plans/pp;GET /protection-plans/pp;GET /backups$L;$SW"
for s in 'Г-16: копия диска, ожидание готовности' 'creating → created' 'Г-16: восстановление диска' 'Г-16: восстановление ВМ' 'повтор сразу — HTTP 409' 'после исчезновения — HTTP 404' 'Г-16: план защиты' 'Г-16: копии ресурсов пробы под чужим именем'; do
	grep -q "$s" "$tmp/out" || { echo "FAIL: Г-16: нет строки «$s»"; sed 's/^/  | /' "$tmp/out"; fail=1; }
done
# Восстановление: копия восстанавливается в новый ресурс с именем пробы
# (уборка области найдёт его по префиксу); диск восстановленной ВМ назван
# и привязан к загрузочному диску исходной.
jq -e '.action == "restore" and .resource.target_config.name == "evo-e2e-ops-r7a1-g16-rv" and (.resource.target_config.disks == [{"name":"evo-e2e-ops-r7a1-g16-rv-boot","related_disk_id":"boot1"}])' "$FAKE_DIR/body.POST.backups.bv.action" >/dev/null ||
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
fake_evo_answer provision 0 1 '{"error":{"reason":"API_ERROR","message":"boom"}}'
cat >"$FAKE_DIR/http" <<'H'
POST /backups 500 {"message":"boom"}
POST /protection-plans 500 {"message":"boom"}
GET! /backups?project_id=p&limit=1000 200 {"items":[],"total":0}
GET! /backups?project_id=p&limit=1000 200 {"items":[{"id":"auto1","name":"plan-copy","resource":{"id":"d1"}},{"id":"foreign","name":"other","resource":{"id":"zz"}}],"total":2}
GET! /protection-plans?project_id=p&limit=1000 200 {"items":[{"id":"ppleft","name":"evo-e2e-ops-r7a1-g16-pp"},{"id":"ppother","name":"prod-plan"}],"total":2}
GET! /backups?project_id=p&limit=1000 200 {"items":[{"id":"bleft","name":"evo-e2e-ops-r7a1-g16-bd"},{"id":"foreign","name":"other"}],"total":2}
DELETE /backups/* 204
DELETE /protection-plans/* 204
H
rc=0
PROBES=g16 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-16, остатки: код" "$rc" 0
want "Г-16, остатки: удалено" "$(grep '^DELETE' "$FAKE_DIR/calls" | sort | tr '\n' ';')" "DELETE /backups/auto1;DELETE /backups/bleft;DELETE /protection-plans/ppleft;"

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

[ "$fail" = 0 ] && echo "cloud-live ops: ok"
exit "$fail"
