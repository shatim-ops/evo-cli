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
# «МЕТОД ОБРАЗЕЦ КОД [ТЕЛО]». Строки без образца — 404 с телом {}.
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
case "$url" in */auth/token) printf '{"access_token":"tok"}'; exit 0 ;; esac
echo "$method ${url#https://compute.api.cloud.ru/api/v1}" >>"$FAKE_DIR/calls"
[ -z "$hdr" ] || : >"$hdr"
code=404 body='{}'
while read -r m pat c b; do
	# shellcheck disable=SC2254 # образец — glob
	case "$method ${url#https://compute.api.cloud.ru/api/v1}" in "$m "$pat) code=$c; body=${b:-{\}}; break ;; esac
done <"$FAKE_DIR/http"
printf '%s' "$body" >"$out"
printf '%s' "$code"
FAKE
chmod +x "$tmp/bin/curl"
export PATH="$tmp/bin:$PATH" KEY_ID=k SECRET=s PROJECT=p RUN=r7a1 ZONE=ru.AZ-2 GITHUB_STEP_SUMMARY=/dev/null
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
}
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
want "Г-11: запросы по порядку" "$(calls)" "POST /security-groups;POST /security-groups/sg1/rules;POST /security-groups/sg1/rules;POST /security-groups/sg1/rules;POST /security-groups/sg1/rules;POST /security-groups/sg1/rules;GET /security-groups/sg1;POST /security-groups/sg1/rules;GET /security-groups/sg1;DELETE /security-groups/sg1/rules/rule1;DELETE /security-groups/sg1/rules/rule1;DELETE /security-groups/sg1;DELETE /security-groups/sg1;"
grep -q 'Г-11: повтор создания того же правила' "$tmp/out" || { echo "FAIL: нет строки о повторе правила"; fail=1; }
if grep -v '::add-mask::' "$tmp/out" | grep -q 'sg1\|rule1'; then echo "FAIL: id в строке лога"; fail=1; fi

# Группа не создана (отказ платформы): дальше проба не идёт, код 0 — это
# ответ платформы, а не провал; правил и удалений нет.
world
echo 'POST /security-groups 422 {"code":"quota_exceeded","message":"limit"}' >"$FAKE_DIR/http"
fake_evo_answer list_security_groups 0 0 '{"security_groups":[]}'
rc=0
PROBES=g11 "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "Г-11, отказ создания: код" "$rc" 0
want "Г-11, отказ создания: один запрос" "$(calls)" "POST /security-groups;"
grep -q 'HTTP 422' "$tmp/out" || { echo "FAIL: отказ создания не назван кодом"; fail=1; }

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

# Слова проб нет — область ничего не шлёт.
world
: >"$FAKE_DIR/http"
fake_evo_answer list_security_groups 0 0 '{"security_groups":[]}'
rc=0
PROBES="" "$here/cloud-live.sh" ops >"$tmp/out" 2>&1 || rc=$?
want "без слов проб: код" "$rc" 0
[ ! -s "$FAKE_DIR/calls" ] || { echo "FAIL: без слов проб были запросы: $(calls)"; fail=1; }

[ "$fail" = 0 ] && echo "cloud-live ops: ok"
exit "$fail"
