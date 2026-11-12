#!/usr/bin/env bash
# Проверка e2e_live_runs.sh (job ci) на заглушке gh: выражение называет
# ресурсы незавершённых запусков cloud-e2e и только их; список запусков не
# прочитан — код 1 и пустой вывод (уборка ничего не удаляет). Затем скрипты
# уборки на поддельном evo: dbaas-delete.sh по префиксу и
# e2e_rules_sweep.sh не трогают ресурсы живого запуска.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir "$tmp/bin"
# Заглушка gh api: номера запусков состояния из $RUNS/<состояние>; файла
# FAIL — сбой чтения. Каждый вызов — строка в $RUNS/calls.
cat >"$tmp/bin/gh" <<'GH'
#!/usr/bin/env bash
for a in "$@"; do case $a in repos/*) url=$a ;; esac; done
echo "$url" >>"$RUNS/calls"
[ ! -e "$RUNS/FAIL" ] || { echo "HTTP 502" >&2; exit 1; }
case "$url" in repos/o/r/actions/workflows/cloud-e2e.yml/runs\?status=*) ;; *) echo "заглушка gh: $url" >&2; exit 1 ;; esac
s=${url#*status=}
cat "$RUNS/${s%%&*}" 2>/dev/null || true
GH
chmod +x "$tmp/bin/gh"
# shellcheck source-path=SCRIPTDIR source=fake_evo.sh
. "$here/fake_evo.sh"
fake_evo_install "$tmp/bin"
export PATH="$tmp/bin:$PATH" GITHUB_REPOSITORY=o/r GH_TOKEN=t E2E_LIVE_TTL=0
fail=0
n=0
# world — новый мир: свой RUNNER_TEMP (кэш списка) и свои запуски.
world() {
	n=$((n + 1))
	export RUNS="$tmp/w$n" RUNNER_TEMP="$tmp/w$n" FAKE_DIR="$tmp/w$n"
	mkdir "$RUNS"
}
want() { if [ "$2" != "$3" ]; then printf 'FAIL %s\n  got:  %s\n  want: %s\n' "$1" "$2" "$3"; fail=1; fi; }

# Запуск 185 идёт, 186 ждёт одобрения, 12 — в очереди.
world
echo 185 >"$RUNS/in_progress"
echo 186 >"$RUNS/waiting"
echo 12 >"$RUNS/queued"
names=$(printf '%s\n' \
	"a	evo-e2e-pg-r185a1" \
	"b	evo-e2e-disk-r186a2-vm" \
	"c	fip-evo-e2e-r12a1" \
	"d	evo-e2e-secrets-r185a1/db" \
	"e	evo-e2e-dns-r185a1.test." \
	"f	evo-e2e-pg-r18a1" \
	"g	evo-e2e-pg-r1855a1" \
	"h	evo-e2e-kafka" \
	"i	evo-e2e-disk-r184a1-vm" \
	"j	evo-e2e-net-r1a1-b")
got=$(printf '%s\n' "$names" | "$here/e2e_live_runs.sh" drop | cut -f1 | tr '\n' ' ')
want "остаются только ресурсы завершённых запусков и имена без номера" "$got" "f g h i j "
want "каждое состояние, кроме completed, прочитано" "$(sed 's/.*status=//; s/&.*//' "$RUNS/calls" | sort | tr '\n' ' ')" "in_progress pending queued requested waiting "

# Незавершённых запусков нет: выражение не пусто и ничего не называет.
world
re=$("$here/e2e_live_runs.sh")
want "выражение без живых запусков" "$re" '(^|-)r(0)a[0-9]+([^0-9]|$)'
got=$(printf 'a\tevo-e2e-pg-r185a1\n' | "$here/e2e_live_runs.sh" drop | cut -f1)
want "без живых запусков остаток виден" "$got" a

# others — сколько незавершённых запусков, кроме своего (сама уборка тоже
# идёт): общие для всех прогонов правило и маршрут тестовой сети уборка
# снимает, только когда других нет.
world
echo 185 >"$RUNS/in_progress"
want "others: чужой живой запуск" "$("$here/e2e_live_runs.sh" others)" 1
want "others: жив только свой запуск" "$(GITHUB_RUN_NUMBER=185 "$here/e2e_live_runs.sh" others)" 0

# Список не прочитан: код 1, вывода нет — и у фильтра тоже.
world
touch "$RUNS/FAIL"
if out=$("$here/e2e_live_runs.sh" 2>/dev/null); then echo "FAIL: сбой gh — код 0"; fail=1; fi
want "сбой gh: вывода нет" "${out:-}" ""
if out=$(printf 'a\tevo-e2e-pg-r1a1\n' | "$here/e2e_live_runs.sh" drop 2>/dev/null); then echo "FAIL: сбой gh у фильтра — код 0"; fail=1; fi
want "сбой gh у фильтра: ни одной строки" "${out:-}" ""

if out=$("$here/e2e_live_runs.sh" others 2>/dev/null); then echo "FAIL: сбой gh у others — код 0"; fail=1; fi
want "сбой gh у others: вывода нет" "${out:-}" ""

# В списке не номер — тоже отказ.
world
echo "185 x" >"$RUNS/in_progress"
if "$here/e2e_live_runs.sh" >/dev/null 2>&1; then echo "FAIL: не номер в списке — код 0"; fail=1; fi

# Кэш: второй вызов в пределах E2E_LIVE_TTL не идёт в gh.
world
echo 185 >"$RUNS/in_progress"
E2E_LIVE_TTL=600 "$here/e2e_live_runs.sh" >/dev/null
calls=$(wc -l <"$RUNS/calls")
E2E_LIVE_TTL=600 "$here/e2e_live_runs.sh" >/dev/null
want "кэш списка" "$(wc -l <"$RUNS/calls")" "$calls"

# dbaas-delete.sh по префиксу: кластер живого запуска 185 не удаляется и не
# считается остатком, кластер завершённого 184 удаляется.
world
echo 185 >"$RUNS/in_progress"
fake_evo_answer kafka_list 0 0 '{"clusters":[{"id":"a","name":"evo-e2e-kafka-r185a1","status":"RUNNING"},{"id":"b","name":"evo-e2e-kafka-r184a1","status":"RUNNING"}]}'
fake_evo_answer kafka_list 1 0 '{"clusters":[{"id":"a","name":"evo-e2e-kafka-r185a1","status":"RUNNING"},{"id":"b","name":"evo-e2e-kafka-r184a1","status":"ERASED"}]}'
fake_evo_answer kafka_delete.yes 0 0 '{"status":"deleting","id":"b"}'
fake_evo_answer kafka_delete 0 0 '{"status":"already_deleted","existed":false,"cluster_id":"b"}'
rc=0
DBAAS_POLL=0 "$here/dbaas-delete.sh" kafka 'evo-e2e-*' >"$tmp/out" 2>&1 || rc=$?
want "dbaas-delete по префиксу рядом с живым запуском: код" "$rc" 0
want "dbaas-delete по префиксу: одно удаление" "$(grep -c 'kafka_delete yes=1' "$FAKE_DIR/evo-calls")" 1

# Список запусков не прочитан — по префиксу не удаляется ничего, код не 0.
world
touch "$RUNS/FAIL"
fake_evo_answer kafka_list 0 0 '{"clusters":[{"id":"b","name":"evo-e2e-kafka-r184a1","status":"RUNNING"}]}'
fake_evo_answer kafka_delete.yes 0 0 '{"status":"deleting","id":"b"}'
rc=0
DBAAS_POLL=0 "$here/dbaas-delete.sh" kafka 'evo-e2e-*' >"$tmp/out" 2>&1 || rc=$?
[ "$rc" != 0 ] || { echo "FAIL: живые запуски неизвестны, а dbaas-delete по префиксу — код 0"; fail=1; }
if grep -q 'kafka_delete' "$FAKE_DIR/evo-calls" 2>/dev/null; then echo "FAIL: живые запуски неизвестны, а удаление было"; fail=1; fi

# Свой кластер job удаляет по точному имени без списка запусков: gh не нужен.
world
touch "$RUNS/FAIL"
fake_evo_answer kafka_list 0 0 '{"clusters":[{"id":"a","name":"evo-e2e-kafka-r185a1","status":"RUNNING"}]}'
fake_evo_answer kafka_list 1 0 '{"clusters":[]}'
fake_evo_answer kafka_delete.yes 0 0 '{"status":"deleting","id":"a"}'
fake_evo_answer kafka_delete 0 0 '{"status":"already_deleted","existed":false,"cluster_id":"a"}'
rc=0
DBAAS_POLL=0 "$here/dbaas-delete.sh" kafka evo-e2e-kafka-r185a1 >"$tmp/out" 2>&1 || rc=$?
want "точное имя: код" "$rc" 0
[ ! -e "$RUNS/calls" ] || { echo "FAIL: точное имя, а список запусков читался"; fail=1; }

# cloud-live.sh sweep evo-e2e- (job cleanup): бакет живого запуска 185 не
# удаляется и не считается остатком, бакет завершённого 184 удаляется.
world
echo 185 >"$RUNS/in_progress"
fake_evo_answer s3_buckets 0 0 '{"buckets":[{"name":"evo-e2e-s3-r185a1-aa"},{"name":"evo-e2e-s3-r184a1-bb"}]}'
fake_evo_answer s3_buckets 1 0 '{"buckets":[{"name":"evo-e2e-s3-r185a1-aa"}]}'
fake_evo_answer s3_ls 0 0 '{"objects":[]}'
fake_evo_answer s3_bucket_delete 0 0 '{"status":"deleted"}'
rc=0
"$here/cloud-live.sh" sweep evo-e2e- s3 >"$tmp/out" 2>&1 || rc=$?
want "sweep evo-e2e- рядом с живым запуском: код" "$rc" 0
grep -q 'правило и маршрут тестовой сети не трогаю' "$tmp/out" || { echo "FAIL: идёт запуск, а правило и маршрут тестовой сети не оставлены"; sed 's/^/  | /' "$tmp/out"; fail=1; }
want "sweep evo-e2e-: одно удаление бакета" "$(grep -c 's3_bucket_delete yes=1' "$FAKE_DIR/evo-calls")" 1
if grep -q 'r185a1' "$tmp/out"; then echo "FAIL: бакет живого запуска назван в уборке"; sed 's/^/  | /' "$tmp/out"; fail=1; fi

# Страховка области — sweep своего префикса с номером прогона: свой бакет
# удаляется, хотя запуск жив, и список запусков не читается.
world
touch "$RUNS/FAIL"
fake_evo_answer s3_buckets 0 0 '{"buckets":[{"name":"evo-e2e-s3-r185a1-aa"}]}'
fake_evo_answer s3_buckets 1 0 '{"buckets":[]}'
fake_evo_answer s3_ls 0 0 '{"objects":[]}'
fake_evo_answer s3_bucket_delete 0 0 '{"status":"deleted"}'
rc=0
"$here/cloud-live.sh" sweep evo-e2e-s3-r185a1 s3 >"$tmp/out" 2>&1 || rc=$?
want "страховка области: код" "$rc" 0
want "страховка области: свой бакет удалён" "$(grep -c 's3_bucket_delete yes=1' "$FAKE_DIR/evo-calls")" 1
[ ! -e "$RUNS/calls" ] || { echo "FAIL: страховка области читала список запусков"; fail=1; }

# Список запусков не прочитан — sweep evo-e2e- ничего не удаляет, код не 0.
world
touch "$RUNS/FAIL"
fake_evo_answer s3_buckets 0 0 '{"buckets":[{"name":"evo-e2e-s3-r184a1-bb"}]}'
fake_evo_answer s3_bucket_delete 0 0 '{"status":"deleted"}'
rc=0
timeout 20 "$here/cloud-live.sh" sweep evo-e2e- s3 >"$tmp/out" 2>&1 || rc=$?
[ "$rc" != 0 ] || { echo "FAIL: живые запуски неизвестны, а sweep evo-e2e- — код 0"; fail=1; }
if grep -q 's3_bucket_delete' "$FAKE_DIR/evo-calls"; then echo "FAIL: живые запуски неизвестны, а бакет удалён"; fail=1; fi

# e2e_rules_sweep.sh: ВМ только живого запуска — правила не тронуты, код 0;
# ВМ завершённого запуска — как раньше, код 1.
world
echo 185 >"$RUNS/in_progress"
fake_evo_answer vm_list 0 0 '{"vms":[{"id":"v","name":"evo-e2e-r185a1"}]}'
rc=0
KEY_ID=k SECRET=s PROJECT=p "$here/e2e_rules_sweep.sh" >"$tmp/out" 2>&1 || rc=$?
want "правила рядом с живым запуском: код" "$rc" 0
if grep -q 'list_security_groups' "$FAKE_DIR/evo-calls"; then echo "FAIL: идёт запуск, а группы читались"; fail=1; fi
world
echo 185 >"$RUNS/in_progress"
fake_evo_answer vm_list 0 0 '{"vms":[{"id":"v","name":"evo-e2e-r184a1"}]}'
rc=0
KEY_ID=k SECRET=s PROJECT=p "$here/e2e_rules_sweep.sh" >"$tmp/out" 2>&1 || rc=$?
want "правила при ВМ завершённого запуска: код" "$rc" 1

[ "$fail" = 0 ] && echo "e2e_live_runs: ok"
exit "$fail"
