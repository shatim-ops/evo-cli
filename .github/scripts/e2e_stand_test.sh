#!/usr/bin/env bash
# Проверка e2e_stand.sh (job ci) на поддельных evo и curl: list находит всё
# содержимое стенда и только его (копия с чужим именем — по id ВМ стенда);
# непрочитанный список — не «пусто»; стенд за сроком — код 1; sweep без
# точного STAND_SWEEP отказывает до чтения облака, удаляет только
# показанное, в порядке «планы и копии → кластеры → ВМ → адреса → диски →
# сеть», и кончается пустым list или кодом 1 с перечнем. Облака нет.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fail=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# shellcheck source-path=SCRIPTDIR source=fake_evo.sh
. "$here/fake_evo.sh"
# shellcheck source-path=SCRIPTDIR source=e2e_stand.sh
. "$here/e2e_stand.sh"
fake_evo_install "$work/bin"
# Заглушка curl: токен IAM; иначе «МЕТОД путь» — в $FAKE_DIR/calls, ответ —
# первая строка $FAKE_DIR/http «МЕТОД ОБРАЗЕЦ КОД [ТЕЛО]» (МЕТОД! — разовая).
cat >"$work/bin/curl" <<'FAKE'
#!/usr/bin/env bash
out=/dev/stdout url= method=GET
while [ $# -gt 0 ]; do
	case $1 in
	-o) out=$2; shift ;; -X) method=$2; shift ;; -w | -H | -D | --data-binary) shift ;;
	https://*) url=$1 ;;
	esac
	shift
done
case "$url" in */auth/token) cat >/dev/null; printf '{"access_token":"tok"}'; exit 0 ;; esac
path=${url#https://compute.api.cloud.ru/api/v1}
echo "$method $path" >>"$FAKE_DIR/calls"
code=404 body='{}' i=0
while read -r m pat c b; do
	i=$((i + 1))
	once=0
	case "$m" in *!) once=1 m=${m%!} ;; esac
	[ "$once" = 0 ] || ! grep -qx "$i" "$FAKE_DIR/http.used" 2>/dev/null || continue
	# shellcheck disable=SC2254 # образец — glob
	case "$method $path" in "$m "$pat)
		code=$c body=${b:-{\}}
		[ "$once" = 0 ] || echo "$i" >>"$FAKE_DIR/http.used"
		break
		;;
	esac
done <"$FAKE_DIR/http"
printf '%s' "$body" >"$out"
printf '%s' "$code"
FAKE
chmod +x "$work/bin/curl"
export PATH="$work/bin:$PATH" KEY_ID=k SECRET=s PROJECT=p STAND_PAUSE=0 STAND_WAIT=1 STAND_TODAY=2026-11-20
S=$STAND_PREFIX

want() { [ "$2" = "$3" ] || { echo "FAIL $1: «$2», ждём «$3»"; sed 's/^/  | /' "$FAKE_DIR/out"; fail=1; }; }
has() { grep -q -- "$2" "$FAKE_DIR/out" || { echo "FAIL $1: нет строки «$2»"; sed 's/^/  | /' "$FAKE_DIR/out"; fail=1; }; }
hasnt() { ! grep -q -- "$2" "$FAKE_DIR/out" || { echo "FAIL $1: есть строка «$2»"; sed 's/^/  | /' "$FAKE_DIR/out"; fail=1; }; }
run_() {
	set +e
	"$here/e2e_stand.sh" "$@" >"$FAKE_DIR/out" 2>&1
	rc=$?
	set -e
}
# world ИМЯ — пустой проект: чужие ресурсы есть, стенда нет.
world() {
	export FAKE_DIR="$work/$1"
	mkdir -p "$FAKE_DIR"
	: >"$FAKE_DIR/calls"
	: >"$FAKE_DIR/evo-calls"
	fake_evo_answer vm_list 0 0 '{"vms":[{"id":"x1","name":"evo-e2e-ops-r5a1-g10"},{"id":"x2","name":"prod-db"}]}'
	fake_evo_answer list_floating_ips 0 0 '{"floating_ips":[{"id":"x3","name":"fip-evo-e2e-ops-r5a1-g10-abc"}]}'
	fake_evo_answer disk_list 0 0 '{"disks":[{"id":"x4","name":"prod-data","attached_to":[]}]}'
	fake_evo_answer list_subnets 0 0 '{"subnets":[{"id":"x5","name":"Default_ru.AZ-2"}]}'
	fake_evo_answer list_security_groups 0 0 '{"security_groups":[{"id":"x6","name":"default_ru.AZ-2"}]}'
	fake_evo_answer k8s_list 0 0 '{"clusters":[{"id":"x7","name":"evo-e2e-k8s-r5a1"}]}'
	fake_evo_answer pg_list 0 0 '{"clusters":[{"id":"x8","name":"evo-e2e-pg-r5a1","status":"READY"}]}'
	cat >"$FAKE_DIR/http" <<'H'
GET /backups[?]* 200 {"items":[{"id":"x9","name":"evo-e2e-ops-r5a1-g16-bd","resource":{"resource_id":"x4"}}],"total":1}
GET /protection-plans[?]* 200 {"items":[],"total":0}
H
}
# full ИМЯ — тот же проект со стендом: каждый вид; копия с чужим именем
# сделана с ВМ стенда.
full() {
	world "$1"
	fake_evo_answer vm_list 0 0 '{"vms":[{"id":"x1","name":"evo-e2e-ops-r5a1-g10"},{"id":"svm","name":"'"$S"'-vm","created_time":"2026-11-18T10:00:00Z"}]}'
	fake_evo_answer list_floating_ips 0 0 '{"floating_ips":[{"id":"x3","name":"fip-evo-e2e-ops-r5a1-g10-abc"},{"id":"sfip","name":"fip-'"$S"'-vm-abc123"}]}'
	fake_evo_answer disk_list 0 0 '{"disks":[{"id":"x4","name":"prod-data","attached_to":[]},{"id":"sboot","name":"'"$S"'-vm-boot","attached_to":["'"$S"'-vm"]}]}'
	fake_evo_answer list_subnets 0 0 '{"subnets":[{"id":"x5","name":"Default_ru.AZ-2"},{"id":"ssn","name":"'"$S"'-sn"}]}'
	fake_evo_answer list_security_groups 0 0 '{"security_groups":[{"id":"x6","name":"default_ru.AZ-2"},{"id":"ssg","name":"'"$S"'-sg"}]}'
	fake_evo_answer k8s_list 0 0 '{"clusters":[{"id":"x7","name":"evo-e2e-k8s-r5a1"},{"id":"sk8s","name":"'"$S"'-k8s"}]}'
	fake_evo_answer pg_list 0 0 '{"clusters":[{"id":"x8","name":"evo-e2e-pg-r5a1","status":"READY"},{"id":"spg","name":"'"$S"'-pg","status":"READY"}]}'
	cat >"$FAKE_DIR/http" <<H
GET /backups[?]* 200 {"items":[{"id":"x9","name":"evo-e2e-ops-r5a1-g16-bd","resource":{"resource_id":"x4"}},{"id":"sbk","name":"$S-bk","resource":{"resource_id":"sboot"}},{"id":"sbk2","name":"auto-2026-11-18","resource":{"resource_id":"svm"}}],"total":3}
GET /protection-plans[?]* 200 {"items":[{"id":"spp","name":"$S-pp"},{"id":"spp2","name":"daily","resources":[{"resource_id":"svm","resource_type":"virtual_machine"}]},{"id":"x10","name":"prod-plan","resources":[{"resource_id":"x2"}]}],"total":3}
DELETE /backups/* 204
DELETE /protection-plans/* 204
DELETE /security-groups/* 204
H
}
# gone — после удалений стенда в списках нет (ответ со второго чтения).
gone() {
	fake_evo_answer vm_list 1 0 '{"vms":[{"id":"x1","name":"evo-e2e-ops-r5a1-g10"}]}'
	fake_evo_answer list_floating_ips 1 0 '{"floating_ips":[]}'
	fake_evo_answer disk_list 1 0 '{"disks":[]}'
	fake_evo_answer list_subnets 1 0 '{"subnets":[]}'
	fake_evo_answer list_security_groups 1 0 '{"security_groups":[]}'
	fake_evo_answer k8s_list 1 0 '{"clusters":[]}'
	fake_evo_answer pg_list 1 0 '{"clusters":[]}'
	{
		echo 'GET! /backups[?]* 200 {"items":[{"id":"sbk","name":"'"$S"'-bk","resource":{"resource_id":"sboot"}},{"id":"sbk2","name":"auto-2026-11-18","resource":{"resource_id":"svm"}}],"total":2}'
		echo 'GET! /protection-plans[?]* 200 {"items":[{"id":"spp","name":"'"$S"'-pp"},{"id":"spp2","name":"daily","resources":[{"resource_id":"svm"}]}],"total":2}'
		echo 'GET /backups[?]* 200 {"items":[],"total":0}'
		echo 'GET /protection-plans[?]* 200 {"items":[],"total":0}'
		echo 'DELETE /backups/* 204'
		echo 'DELETE /protection-plans/* 204'
		echo 'DELETE /security-groups/* 204'
	} >"$FAKE_DIR/http"
	for t in k8s_delete pg_delete destroy release_floating_ip disk_delete subnet_delete; do
		fake_evo_answer "$t.yes" 0 0 '{"status":"deleting"}'
	done
}

# list: стенда нет — так и сказано, код 0; чужое не названо.
world empty
run_ list
want "empty: код" "$rc" 0
has empty 'стенда нет'
hasnt empty 'evo-e2e-\|prod-'

# list: всё содержимое стенда, по одному на вид, с возрастом; чужое не
# названо; копия и план защиты с чужим именем найдены по id ВМ стенда (у
# плана ресурсы — списком resources[]); id — под маской.
full all
run_ list
want "all: код" "$rc" 0
for l in "pplan	$S-pp" "pplan	daily" "backup	$S-bk" "backup	auto-2026-11-18" "k8s	$S-k8s" "pg	$S-pg" "vm	$S-vm" "fip	fip-$S-vm-abc123" "disk	$S-vm-boot" "subnet	$S-sn" "sg	$S-sg"; do
	has all "^$l"
done
has all 'возраст стенда: 2 сут'
has all "срок: до $STAND_UNTIL"
hasnt all 'evo-e2e-\|prod-\|Default_\|default_'
if grep -v '::add-mask::' "$FAKE_DIR/out" | grep -qw 'svm\|sfip\|sboot\|sk8s\|spg\|sbk\|spp\|spp2\|ssn\|ssg'; then echo "FAIL all: id в строке лога"; fail=1; fi
if grep -q 'yes=1' "$FAKE_DIR/evo-calls" || grep -q '^DELETE' "$FAKE_DIR/calls"; then echo "FAIL all: list что-то удалял"; fail=1; fi

# list: стенд за сроком — код 1 (как overdue у запусков); пустой проект за
# сроком — не ошибка.
full late
STAND_TODAY=2026-12-16 run_ list
want "late: код" "$rc" 1
has late '::error::стенд за сроком'
world late-empty
STAND_TODAY=2026-12-16 run_ list
want "late-empty: код" "$rc" 0

# list: список не прочитан — не «стенда нет».
full unread
fake_evo_answer k8s_list 0 5 '{"error":{"code":5,"reason":"API_ERROR","message":"boom"}}'
run_ list
want "unread: код" "$rc" 1
has unread 'k8s: список не прочитан'
hasnt unread 'стенда нет'

# sweep: без точного подтверждения — отказ до чтения облака.
for confirm in "" "evo-stand" "${S}x" "evo-e2e-"; do
	full "refuse-$confirm"
	STAND_SWEEP=$confirm run_ sweep
	want "sweep с подтверждением «$confirm»: код" "$rc" 2
	if [ -s "$FAKE_DIR/evo-calls" ] || [ -s "$FAKE_DIR/calls" ]; then echo "FAIL sweep «$confirm»: облако читалось до отказа"; fail=1; fi
done

# sweep: перечень до первого удаления; порядок — планы и копии, кластеры,
# ВМ, адреса, диски, сеть; чужое не тронуто; в конце стенда нет — код 0.
full sweep
gone
STAND_SWEEP=$S run_ sweep
want "sweep: код" "$rc" 0
has sweep 'остатков стенда нет'
want "sweep: порядок удалений" "$({ grep -n '' "$FAKE_DIR/out" | grep -o 'удаление [a-z0-9]*' | awk '!seen[$0]++' | tr '\n' ';'; })" "удаление pplan;удаление backup;удаление k8s;удаление pg;удаление vm;удаление fip;удаление disk;удаление subnet;удаление sg;"
want "sweep: удаления evo" "$(grep 'yes=1' "$FAKE_DIR/evo-calls" | tr '\n' ';')" "k8s_delete yes=1;pg_delete yes=1;destroy yes=1;release_floating_ip yes=1;disk_delete yes=1;subnet_delete yes=1;"
want "sweep: удаления API" "$(grep '^DELETE' "$FAKE_DIR/calls" | tr '\n' ';')" "DELETE /protection-plans/spp;DELETE /protection-plans/spp2;DELETE /backups/sbk;DELETE /backups/sbk2;DELETE /security-groups/ssg;"
first_del=$(grep -n 'удаление ' "$FAKE_DIR/out" | head -1 | cut -d: -f1)
last_listed=$(grep -n "^sg	$S-sg" "$FAKE_DIR/out" | head -1 | cut -d: -f1)
[ -n "$first_del" ] && [ -n "$last_listed" ] && [ "$last_listed" -lt "$first_del" ] || { echo "FAIL sweep: перечень не напечатан до первого удаления"; fail=1; }

# sweep: кластер PostgreSQL в FAILED платформа не удаляет — код 1 и путь, а
# не «пусто»; остальное удалено.
full stuck
gone
fake_evo_answer pg_list 0 0 '{"clusters":[{"id":"spg","name":"'"$S"'-pg","status":"FAILED"}]}'
fake_evo_answer pg_list 1 0 '{"clusters":[{"id":"spg","name":"'"$S"'-pg","status":"FAILED"}]}'
fake_evo_answer pg_delete.yes 0 5 '{"error":{"code":5,"reason":"API_ERROR","message":"delete cluster: 500","hint":"Cloud.ru does not delete a FAILED cluster"}}'
STAND_SWEEP=$S run_ sweep
want "stuck: код" "$rc" 1
has stuck "::error::остаток стенда: pg $S-pg (FAILED).*только консоль Cloud.ru или поддержка"
hasnt stuck 'остатков стенда нет'

# sweep: появилось после перечня (не показано) — не удаляется, код 1.
full late-comer
gone
fake_evo_answer disk_list 1 0 '{"disks":[{"id":"snew","name":"'"$S"'-new","attached_to":[]}]}'
STAND_SWEEP=$S run_ sweep
want "late-comer: код" "$rc" 1
has late-comer "::error::остаток стенда: disk $S-new"
want "late-comer: удалён только показанный диск" "$(grep -c '^disk_delete yes=1' "$FAKE_DIR/evo-calls")" 1

[ "$fail" = 0 ] && echo "e2e-stand: ok"
exit "$fail"
