#!/usr/bin/env bash
# Проверка e2e-preflight.sh (job ci) на поддельном evo: все причины провала —
# одним отчётом, а не первой; остатки прошлых прогонов и открытая зона без
# подсети, в которую прогон не идёт, — строками отчёта, не провалом; квота
# ресурса прогона исчерпана — провал, квота образов (CMP-16) — нет. Облака нет.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# shellcheck source-path=SCRIPTDIR source=fake_evo.sh
. "$here/fake_evo.sh"
fake_evo_install "$work/bin"
fail=0

zones='{"zones":[{"id":"z1","name":"ru.AZ-1","enabled":true},{"id":"z2","name":"ru.AZ-2","enabled":true},{"id":"z3","name":"ru.AZ-3","enabled":false}]}'
subnets='{"subnets":[{"id":"s1","name":"Default_ru.AZ-1","availability_zone_id":"z1"},{"id":"s2","name":"Default_ru.AZ-2","availability_zone_id":"z2"}]}'
groups='{"security_groups":[{"id":"g1","name":"SSH-access_ru.AZ-1","availability_zone_id":"z1"},{"id":"g2","name":"SSH-access_ru.AZ-2","availability_zone_id":"z2"}]}'
none='{"clusters":[]}'
quotas='{"quota_usages":[{"resource":"compute.vm.count","limit":10,"usage":2},{"resource":"compute.image.custome","limit":2,"usage":2}]}'

# fresh ИМЯ — свой каталог ответов: всё в порядке, кроме того, что случай
# переложит после.
fresh() {
	export FAKE_DIR="$work/$1"
	mkdir -p "$FAKE_DIR"
	fake_evo_answer list_zones 0 0 "$zones"
	fake_evo_answer list_subnets 0 0 "$subnets"
	fake_evo_answer list_security_groups 0 0 "$groups"
	fake_evo_answer vm_list 0 0 '{"vms":[]}'
	for k in k8s pg kafka redis; do fake_evo_answer "${k}_list" 0 0 "$none"; done
	fake_evo_answer org_quotas 0 0 "$quotas"
}
# run ИМЯ КОД [ПЕРЕМЕННАЯ=ЗНАЧЕНИЕ …] — прогон с входами по умолчанию
# (зона ru.AZ-2, все дорогие джобы выключены) и заменами.
run() {
	local name=$1 want=$2 rc
	shift 2
	set +e
	env PATH="$work/bin:$PATH" RUN=r7a1 PROJECT=p ZONE=ru.AZ-2 K8S_ZONE= PG_ZONE= KAFKA_ZONE= REDIS_ZONE= \
		OS_SMOKE=false K8S_SMOKE=false DBAAS_SMOKE=false DBAAS_SERVICES='pg kafka redis' REDIS_SA=false \
		GITHUB_STEP_SUMMARY="$FAKE_DIR/summary" RUNNER_TEMP="$FAKE_DIR" "$@" "$here/e2e-preflight.sh" >"$FAKE_DIR/out" 2>&1
	rc=$?
	set -e
	if [ "$rc" != "$want" ]; then
		printf 'FAIL %s: код %s, ждём %s\n' "$name" "$rc" "$want"
		sed 's/^/  | /' "$FAKE_DIR/out"
		fail=1
	fi
}
has() {
	grep -qF -- "$2" "$FAKE_DIR/out" || { printf 'FAIL %s: нет строки «%s»\n' "$1" "$2"; sed 's/^/  | /' "$FAKE_DIR/out"; fail=1; }
}
hasnt() {
	! grep -qF -- "$2" "$FAKE_DIR/out" || { printf 'FAIL %s: лишняя строка «%s»\n' "$1" "$2"; fail=1; }
}

# Всё в порядке: код 0, отчёт в summary; списки DBaaS не читаются, раз
# DBaaS не идёт; квота образов исчерпана — строка, не провал.
fresh ok
run ok 0
has ok "preflight: можно запускать"
has ok "compute.image.custome"
grep -q 'pg_list' "$FAKE_DIR/evo-calls" && { echo "FAIL ok: прочитан pg_list без dbaas_smoke"; fail=1; }
[ -s "$FAKE_DIR/summary" ] || { echo "FAIL ok: отчёта в GITHUB_STEP_SUMMARY нет"; fail=1; }

# Три причины сразу: зона pg закрыта, в зоне kafka нет такой зоны, слово
# dbaas_services не из pg, kafka, redis. Код 1, все три в отчёте.
fresh many
run many 1 DBAAS_SMOKE=true PG_ZONE=ru.AZ-3 KAFKA_ZONE=ru.AZ-9 DBAAS_SERVICES='pg kafka postgres'
has many "dbaas_services: «postgres»"
has many "ru.AZ-3 (pg): зона закрыта"
has many "ru.AZ-9 (kafka): зоны нет"
has many "причин: 3"

# Зона прогона без подсети и группы — провал; открытая зона ru.AZ-1 без
# них, куда прогон не идёт, — строка «перезапуск туда не выйдет».
fresh nosubnet
fake_evo_answer list_subnets 0 0 '{"subnets":[]}'
fake_evo_answer list_security_groups 0 0 '{"security_groups":[]}'
run nosubnet 1
has nosubnet "ru.AZ-2 (e2e): нет подсети проекта"
has nosubnet "ru.AZ-2 (e2e): нет группы безопасности"
has nosubnet "ru.AZ-1: открыта, но нет подсети"

# Остатки прошлых прогонов — списком, не провал: ВМ в ошибке, кластер
# FAILED; чужое (audit-*) не считается.
fresh leftovers
fake_evo_answer vm_list 0 0 '{"vms":[{"id":"v","name":"evo-e2e-app","state":"error"},{"id":"w","name":"audit-vm","state":"error"}]}'
fake_evo_answer pg_list 0 0 '{"clusters":[{"id":"c","name":"evo-e2e-pg2","status":"FAILED"}]}'
run leftovers 0 DBAAS_SMOKE=true DBAAS_SERVICES=pg
has leftovers "ВМ evo-e2e-app: error"
has leftovers "pg evo-e2e-pg2: FAILED"
hasnt leftovers "audit-vm"

# Имя этого прогона занято — провал.
fresh taken
fake_evo_answer vm_list 0 0 '{"vms":[{"id":"v","name":"evo-e2e-app-r7a1","state":"running"}]}'
run taken 1
has taken "имя evo-e2e-app-r7a1 занято"

# Квота ВМ исчерпана — провал; квоты не читаются (нет области
# организации) — строка, не провал.
fresh quota
fake_evo_answer org_quotas 0 0 '{"quota_usages":[{"resource":"compute.vm.count","limit":10,"usage":10}]}'
run quota 1
has quota "квота compute.vm.count исчерпана: 10 из 10"
fresh noquota
fake_evo_answer org_quotas 0 0 '{"status":"unavailable","quota_usages":[]}'
run noquota 0
has noquota "квоты не прочитаны"

# Список не читается — провал с названием списка, а не молчание.
fresh noread
fake_evo_answer list_zones 0 5 '{"error":{"code":5,"reason":"API_ERROR","message":"boom"}}'
run noread 1
has noread "list_zones не прочитан — проверку не провести: API_ERROR: boom"

[ "$fail" = 0 ] && echo "e2e-preflight: ok"
exit "$fail"
