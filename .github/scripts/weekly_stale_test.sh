#!/usr/bin/env bash
# Проверка weekly_stale.sh (job ci) на заглушке gh: отменяются только
# ждущие прогоны cloud-e2e от github-actions[bot], прогон человека остаётся.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cat >"$tmp/gh" <<'GH'
#!/usr/bin/env bash
# Заглушка gh api: отмена — в журнал $LOG; список — из $RUNS через
# jq-фильтр вызова, его адрес — в $GETS; прогон по id — его статус: из
# $NOW (id=статус через пробел: одобрен между списком и отменой) или
# из $RUNS.
if [ "$1 $2" = "api -X" ]; then echo "$4" >>"$LOG"; exit 0; fi
case "$2" in
*/actions/runs/*)
	id=${2##*/}
	for p in ${NOW:-}; do [ "${p%%=*}" = "$id" ] && { echo "${p#*=}"; exit 0; }; done
	jq -r ".workflow_runs[] | select(.id == $id) | .status // \"waiting\"" "$RUNS"
	;;
*)
	echo "$2" >>"$GETS"
	jq -r "$4" "$RUNS"
	;;
esac
GH
chmod +x "$tmp/gh"
export PATH="$tmp:$PATH" LOG="$tmp/log" RUNS="$tmp/runs.json" GETS="$tmp/gets"
fail=0
cat >"$RUNS" <<'J'
{"workflow_runs":[
 {"id":177,"created_at":"2026-11-07T11:43:02Z","triggering_actor":{"login":"github-actions[bot]"}},
 {"id":181,"created_at":"2026-11-10T09:17:17Z","triggering_actor":{"login":"owner"}}]}
J
out=$("$here/weekly_stale.sh" o/r)
got=$(cat "$LOG")
[ "$got" = "repos/o/r/actions/runs/177/cancel" ] || { echo "FAIL: отменены «$got», ждём только 177"; fail=1; }
case "$out" in *"отменён 177"*) ;; *) echo "FAIL: вывод «$out»"; fail=1 ;; esac
: >"$LOG"
echo '{"workflow_runs":[]}' >"$RUNS"
out=$("$here/weekly_stale.sh" o/r)
[ ! -s "$LOG" ] || { echo "FAIL: пустой список, а отмена была"; fail=1; }
case "$out" in *"нет"*) ;; *) echo "FAIL: вывод на пустом «$out»"; fail=1 ;; esac
# Сбой чтения списка (rate limit, 5xx): предупреждение, код 0 — недельный
# запуск идёт дальше; отмены нет.
: >"$LOG"
echo 'не JSON' >"$RUNS"
out=$("$here/weekly_stale.sh" o/r) || { echo "FAIL: сбой списка роняет недельный запуск"; fail=1; }
[ ! -s "$LOG" ] || { echo "FAIL: сбой списка, а отмена была"; fail=1; }
case "$out" in *"не прочитан"*) ;; *) echo "FAIL: вывод на сбое «$out»"; fail=1 ;; esac
# Список — ждущие прогоны cloud-e2e.yml, и только они.
[ "$(sort -u "$GETS")" = "repos/o/r/actions/workflows/cloud-e2e.yml/runs?status=waiting&per_page=100" ] ||
	{ echo "FAIL: читался список «$(sort -u "$GETS" | tr '\n' ' ')»"; fail=1; }
# --weekly-only (cloud-e2e-yield): только опознанный по имени недельный
# набор бота. Уборка остаётся — она в своей группе очереди и ручной запуск
# не держит («Риск», 307, У1); прогон со старым именем «cloud-e2e» (до
# run-name) или без имени не опознан и остаётся (Астра #2189); без ключа
# (перед новым недельным запуском) отменяются все ждущие прогоны бота.
cat >"$RUNS" <<'J'
{"workflow_runs":[
 {"id":222,"display_title":"cloud-e2e (weekly)","created_at":"2026-11-16T04:47:00Z","triggering_actor":{"login":"github-actions[bot]"}},
 {"id":223,"display_title":"cloud-e2e (cleanup_only)","created_at":"2026-11-15T05:13:00Z","triggering_actor":{"login":"github-actions[bot]"}},
 {"id":224,"display_title":"cloud-e2e","created_at":"2026-11-08T05:13:00Z","triggering_actor":{"login":"github-actions[bot]"}},
 {"id":225,"created_at":"2026-11-08T05:14:00Z","triggering_actor":{"login":"github-actions[bot]"}},
 {"id":226,"display_title":"cloud-e2e (weekly)","created_at":"2026-11-16T05:00:00Z","triggering_actor":{"login":"owner"}}]}
J
: >"$LOG"
"$here/weekly_stale.sh" o/r --weekly-only >/dev/null
got=$(cat "$LOG")
[ "$got" = "repos/o/r/actions/runs/222/cancel" ] || { echo "FAIL: --weekly-only отменил «$got», ждём только 222"; fail=1; }
: >"$LOG"
"$here/weekly_stale.sh" o/r >/dev/null
got=$(tr '\n' ' ' <"$LOG")
[ "$got" = "repos/o/r/actions/runs/222/cancel repos/o/r/actions/runs/223/cancel repos/o/r/actions/runs/224/cancel repos/o/r/actions/runs/225/cancel " ] ||
	{ echo "FAIL: без ключа отменены «$got», ждём 222–225"; fail=1; }
# Одобрен между списком и отменой (статус уже in_progress): не отменяется
# (Астра #2188); статус не прочитан — тоже.
: >"$LOG"
out=$(NOW="222=in_progress 223=" "$here/weekly_stale.sh" o/r)
got=$(tr '\n' ' ' <"$LOG")
[ "$got" = "repos/o/r/actions/runs/224/cancel repos/o/r/actions/runs/225/cancel " ] ||
	{ echo "FAIL: после одобрения отменены «$got», ждём 224 и 225"; fail=1; }
case "$out" in *"222"*"in_progress"*) ;; *) echo "FAIL: вывод про одобренный «$out»"; fail=1 ;; esac
rc=0
"$here/weekly_stale.sh" o/r --what >/dev/null 2>&1 || rc=$?
[ "$rc" = 2 ] || { echo "FAIL: неизвестный ключ — код $rc, ждём 2"; fail=1; }
[ "$fail" = 0 ] && echo "weekly_stale: ok"
exit "$fail"
