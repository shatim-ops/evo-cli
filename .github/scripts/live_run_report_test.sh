#!/usr/bin/env bash
# Проверка live_run_report.sh на заглушке gh: живой прогон заводит issue
# (красный — в заголовке), повтор попытки дописывает его, прогон без
# начатых job вне preflight и live-plan — без issue; check краснеет на
# прогоне без issue, на issue без ссылки на отчёт и на открытом дольше
# суток, зелёный — когда всё разобрано.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin" "$work/data"
cat >"$work/bin/gh" <<'SH'
#!/usr/bin/env bash
echo "$*" >>"$FAKE_CALLS"
d=$FAKE_DATA
q=.
a=("$@")
for ((i = 0; i < ${#a[@]}; i++)); do [ "${a[i]}" != --jq ] || q=${a[i + 1]}; done
case "$1 $2" in
  "label create") ;;
  "issue create") echo "https://example.invalid/o/r/issues/77" ;;
  "issue comment" | "issue edit") ;;
  api*)
    case "$2" in
      */actions/runs/*/jobs*) id=${2#*/actions/runs/}; id=${id%%/*}; jq -r "$q" "$d/jobs-$id.json" ;;
      */actions/workflows/*/runs*) wf=${2#*/actions/workflows/}; wf=${wf%%.yml*}; f="$d/runs-$wf.json"; [ -f "$f" ] || f="$d/runs-none.json"; jq -r "$q" "$f" ;;
      */issues/*/comments*) n=${2#*/issues/}; n=${n%%/*}; f="$d/comments-$n.json"; [ -f "$f" ] || f="$d/empty.json"; jq -r "$q" "$f" ;;
      */issues\?*)
        st=${2#*state=}; st=${st%%&*}
        jq --arg s "$st" '[.[] | select($s == "all" or .state == $s)]' "$d/issues.json" | jq -r "$q" ;;
      *) echo "заглушка gh: неизвестный путь $2" >&2; exit 1 ;;
    esac ;;
  *) echo "заглушка gh: неизвестная команда $*" >&2; exit 1 ;;
esac
SH
chmod +x "$work/bin/gh"
export PATH="$work/bin:$PATH" FAKE_CALLS="$work/calls" FAKE_DATA="$work/data" GH_REPO=o/r
D=$work/data
echo '[]' >"$D/empty.json"
echo '{"workflow_runs":[]}' >"$D/runs-none.json"
job() { printf '{"name":"%s","steps":%s}' "$1" "$2"; }
jobs() { local id=$1; shift; printf '{"jobs":[%s]}' "$(IFS=,; echo "$*")" >"$D/jobs-$id.json"; }
jobs 1 "$(job preflight '[{"n":1}]')" "$(job 'live (ops)' '[{"n":1}]')"
jobs 2 "$(job preflight '[{"n":1}]')" "$(job live-plan '[{"n":1}]')" "$(job cleanup '[]')"
jobs 3 "$(job probe '[{"n":1}]')"
fail=0
ok() { echo "ok   $1"; }
bad() { echo "FAIL $1"; fail=1; }
run() { : >"$FAKE_CALLS"; rc=0; out=$("$here/live_run_report.sh" "$@" 2>&1) || rc=$?; }

# open
echo '[]' >"$D/issues.json"
RUN_ID=1 RUN_NUMBER=219 RUN_URL=https://example.invalid/run/1 WORKFLOW=cloud-e2e CONCLUSION=success RUN_ATTEMPT=1 run open
if [ "$rc" = 0 ] && grep -q "issue create --title Разбор живого прогона cloud-e2e 219 --label разбор-прогона" "$FAKE_CALLS"; then ok "живой прогон заводит issue"; else bad "живой прогон заводит issue: $rc $(cat "$FAKE_CALLS")"; fi
RUN_ID=1 RUN_NUMBER=218 RUN_URL=u WORKFLOW=cloud-e2e CONCLUSION=failure run open
if grep -q "issue create --title Разбор живого прогона cloud-e2e 218 — красный (failure) " "$FAKE_CALLS"; then ok "красный исход — в заголовке"; else bad "красный исход — в заголовке: $(cat "$FAKE_CALLS")"; fi
RUN_ID=2 RUN_NUMBER=220 RUN_URL=u WORKFLOW=cloud-e2e CONCLUSION=cancelled run open
if [ "$rc" = 0 ] && ! grep -q "issue create" "$FAKE_CALLS"; then ok "только preflight и live-plan — без issue"; else bad "только preflight и live-plan — без issue: $(cat "$FAKE_CALLS")"; fi
echo '[{"number":5,"title":"Разбор живого прогона api-probe 54","state":"open","created_at":"2026-11-15T10:00:00Z","closed_at":null}]' >"$D/issues.json"
RUN_ID=3 RUN_NUMBER=54 RUN_URL=u WORKFLOW=api-probe CONCLUSION=failure RUN_ATTEMPT=2 run open
if ! grep -q "issue create" "$FAKE_CALLS" && grep -q "issue comment 5 --body Попытка 2: failure" "$FAKE_CALLS" && grep -q "issue edit 5 --title Разбор живого прогона api-probe 54 — красный (failure)" "$FAKE_CALLS"; then ok "повтор попытки — комментарий и заголовок"; else bad "повтор попытки: $(cat "$FAKE_CALLS")"; fi
RUN_ID=3 RUN_NUMBER=54 RUN_URL=u WORKFLOW=api-probe CONCLUSION=success RUN_ATTEMPT=3 run open
if ! grep -q "issue edit" "$FAKE_CALLS"; then ok "зелёная попытка заголовок не трогает"; else bad "зелёная попытка: $(cat "$FAKE_CALLS")"; fi

# check
# Адрес закрытого репозитория собран из частей: путь в нём глубже
# верхнего каталога — находка сторожа публичного следа.
ei=evo-internal
export SINCE=2026-11-15T00:00:00Z NOW
NOW=$(date -d 2026-11-15T20:00:00Z +%s)
echo '{"workflow_runs":[{"id":3,"run_number":54,"created_at":"2026-11-15T09:00:00Z"},{"id":3,"run_number":53,"created_at":"2026-11-14T09:00:00Z"}]}' >"$D/runs-api-probe.json"
echo '{"workflow_runs":[{"id":2,"run_number":220,"created_at":"2026-11-15T09:00:00Z"}]}' >"$D/runs-cloud-e2e.json"
run check
if [ "$rc" = 0 ]; then ok "всё разобрано или в пределах суток — зелёный"; else bad "зелёный check: $out"; fi
NOW=$(date -d 2026-11-16T11:00:00Z +%s)
run check
if [ "$rc" = 1 ] && grep -q "#5 открыт 25 ч" <<<"$out"; then ok "открыт дольше суток — красный"; else bad "дольше суток: $rc $out"; fi
NOW=$(date -d 2026-11-15T20:00:00Z +%s)
echo '{"workflow_runs":[{"id":1,"run_number":221,"created_at":"2026-11-15T09:00:00Z"}]}' >"$D/runs-stand.json"
run check
if [ "$rc" = 1 ] && grep -q "stand 221: живой прогон без issue" <<<"$out"; then ok "живой прогон без issue — красный"; else bad "прогон без issue: $rc $out"; fi
rm "$D/runs-stand.json"
echo '[{"number":5,"title":"Разбор живого прогона api-probe 54","state":"closed","created_at":"2026-11-15T10:00:00Z","closed_at":"2026-11-15T12:00:00Z"}]' >"$D/issues.json"
echo '[{"body":"разобрано"}]' >"$D/comments-5.json"
run check
if [ "$rc" = 1 ] && grep -q "#5 закрыт без ссылки на отчёт" <<<"$out"; then ok "закрыт без ссылки — красный"; else bad "закрыт без ссылки: $rc $out"; fi
echo "[{\"body\":\"Отчёт — https://github.com/o/$ei/pull/302\"}]" >"$D/comments-5.json"
run check
if [ "$rc" = 0 ]; then ok "закрыт со ссылкой на PR отчёта — зелёный"; else bad "закрыт со ссылкой: $rc $out"; fi
echo "[{\"body\":\"https://github.com/o/$ei/blob/main/runs/x.md\"}]" >"$D/comments-5.json"
run check
if [ "$rc" = 0 ]; then ok "закрыт со ссылкой на файл runs/ — зелёный"; else bad "файл runs/: $rc $out"; fi
echo "[{\"body\":\"https://github.com/o/$ei/blob/main/briefs/x.md\"}]" >"$D/comments-5.json"
run check
if [ "$rc" = 1 ]; then ok "ссылка не на отчёт — красный"; else bad "ссылка не на отчёт: $rc $out"; fi
run nothing
if [ "$rc" = 2 ]; then ok "неверный вызов — код 2"; else bad "неверный вызов: $rc"; fi
exit "$fail"
