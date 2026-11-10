#!/usr/bin/env bash
# Проверка weekly_stale.sh (job ci) на заглушке gh: отменяются только
# ждущие прогоны cloud-e2e от github-actions[bot], прогон человека остаётся.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cat >"$tmp/gh" <<'GH'
#!/usr/bin/env bash
# Заглушка gh api: список — из $RUNS через jq-фильтр вызова, отмена — в журнал.
if [ "$1 $2" = "api -X" ]; then echo "$4" >>"$LOG"; exit 0; fi
jq -r "$4" "$RUNS"
GH
chmod +x "$tmp/gh"
export PATH="$tmp:$PATH" LOG="$tmp/log" RUNS="$tmp/runs.json"
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
[ "$fail" = 0 ] && echo "weekly_stale: ok"
exit "$fail"
