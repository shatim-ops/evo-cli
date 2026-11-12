#!/usr/bin/env bash
# Проверка main_red.sh (workflow main-red): красный прогон ci на main
# заводит issue «main красный: ci» с упавшими job и строками «--- FAIL» или
# дописывает открытое (у ночного cloud-e2e — своё issue), зелёный — закрывает
# открытое, прочие исходы
# (cancelled) ничего не трогают. gh подменён: он пишет, что у него просили.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
cat >"$work/bin/gh" <<'SH'
#!/usr/bin/env bash
echo "$*" >>"$FAKE_CALLS"
case "$1 $2" in
  "issue list") for n in $FAKE_OPEN; do printf '%s\n' "$n"; done ;;
  "issue create" | "issue comment")
    [ "$2" != create ] || echo https://example.invalid/o/r/issues/55
    while [ $# -gt 0 ]; do [ "$1" = --body-file ] && cat "$2" >>"$FAKE_BODY"; [ "$1" = --body ] && printf '%s\n' "$2" >>"$FAKE_BODY"; shift; done ;;
  "issue close") ;;
  "pr list")
    [ -z "${FAKE_PR_LIST_FAIL:-}" ] || { echo "HTTP 502: Bad Gateway" >&2; exit 1; }
    for n in ${FAKE_PRS:-}; do printf '%s\n' "$n"; done ;;
  "pr comment")
    [ -z "${FAKE_PR_FAIL:-}" ] || { echo "HTTP 403: Resource not accessible by integration" >&2; exit 1; }
    while [ $# -gt 0 ]; do [ "$1" = --body ] && printf '%s\n' "$2" >>"$FAKE_BODY"; shift; done ;;
  api*)
    case "$2" in
      */jobs\?*) printf '%b' "$FAKE_JOBS" ;;
      */logs)
        [ -z "${FAKE_NO_LOG:-}" ] || { echo "HTTP 404: Not Found" >&2; exit 1; }
        # FAKE_LOG_LATE=N: the first N reads are refused, as for a log not
        # served yet.
        n=$(( $(cat "$FAKE_READS" 2>/dev/null || echo 0) + 1 )); echo "$n" >"$FAKE_READS"
        [ "$n" -gt "${FAKE_LOG_LATE:-0}" ] || { echo "HTTP 404: Not Found" >&2; exit 1; }
        printf '%b' "$FAKE_LOG" ;;
    esac ;;
esac
SH
chmod +x "$work/bin/gh"
fail=0
run() { # ИМЯ исход открытое ждём… (WORKFLOW — ci, если не задан)
  : >"$work/calls"
  : >"$work/body"
  rm -f "$work/reads"
  PATH="$work/bin:$PATH" FAKE_CALLS="$work/calls" FAKE_BODY="$work/body" FAKE_OPEN="$3" \
    FAKE_JOBS="${FAKE_JOBS:-}" FAKE_LOG="${FAKE_LOG:-}" FAKE_NO_LOG="${FAKE_NO_LOG:-}" \
    FAKE_READS="$work/reads" FAKE_LOG_LATE="${FAKE_LOG_LATE:-0}" FAKE_PRS="${FAKE_PRS:-}" FAKE_PR_FAIL="${FAKE_PR_FAIL:-}" FAKE_PR_LIST_FAIL="${FAKE_PR_LIST_FAIL:-}" LOG_RETRY_SLEEP=0 LOG_WAIT="${LOG_WAIT:-120}" \
    WORKFLOW="${WORKFLOW:-ci}" GH_REPO=o/r RUN_ID=41 RUN_URL=https://example.invalid/run/41 CONCLUSION="$2" HEAD_SHA=0123456789abcdef \
    "$here/main_red.sh" >/dev/null 2>"$work/stderr" || { echo "FAIL $1: скрипт упал"; cat "$work/stderr"; fail=1; return; }
  local name=$1
  shift 3
  local ok=1
  for want in "$@"; do
    case "$want" in
      calls:*) grep -q -- "${want#calls:}" "$work/calls" || ok=0 ;;
      nocalls:*) ! grep -q -- "${want#nocalls:}" "$work/calls" || ok=0 ;;
      body:*) grep -qF -- "${want#body:}" "$work/body" || ok=0 ;;
      nobody:*) ! grep -qF -- "${want#nobody:}" "$work/body" || ok=0 ;;
      stderr:*) grep -qF -- "${want#stderr:}" "$work/stderr" || ok=0 ;;
    esac
  done
  if [ "$ok" = 1 ]; then echo "ok   $name"; else echo "FAIL $name"; cat "$work/calls" "$work/body"; fail=1; fi
}

FAKE_JOBS='101\ttest (windows-latest)\n' \
  FAKE_LOG='2026-11-03T18:19:44.8214185Z --- FAIL: TestDeploy_X (0.07s)\n2026-11-03T18:19:44.8215750Z     deploy_bound_test.go:248: first\nok  \tpkg\n' \
  run "красный, issue нет — заводит с job и FAIL" failure "" \
  "calls:issue create --title main красный: ci" "body:test (windows-latest)" "body:--- FAIL: TestDeploy_X (0.07s)" "nocalls:issue close"
FAKE_JOBS='101\ttest (windows-latest)\n' FAKE_LOG='--- FAIL: TestA\n' \
  run "красный, issue открыто — дописывает" failure 7 \
  "calls:issue comment 7" "nocalls:issue create" "body:--- FAIL: TestA"
FAKE_JOBS='102\tshellcheck\n' FAKE_LOG='SC2016\n' \
  run "красный без строк FAIL — называет job" failure "" \
  "calls:issue create" "body:**shellcheck**" "body:смотрите лог job"
run "зелёный, issue открыто — закрывает" success 7 \
  "calls:issue comment 7" "calls:issue close 7" "nocalls:issue create"
run "зелёный, issue нет — тишина" success "" \
  "nocalls:issue comment" "nocalls:issue create" "nocalls:issue close"
run "cancelled — тишина" cancelled 7 \
  "nocalls:issue comment" "nocalls:issue create" "nocalls:issue close"
FAKE_JOBS='103\ttest (macos-latest)\n' FAKE_NO_LOG=1 \
  run "красный, лог не получен — так и сказано" failure "" \
  "calls:issue create" "body:**test (macos-latest)**" "body:лог job не получен" "nocalls:issue close" \
  "stderr:HTTP 404: Not Found" "stderr:за 6 из 6 попыток"
FAKE_JOBS='105\ttest (windows-latest)\n' FAKE_LOG='--- FAIL: TestLate\n' FAKE_LOG_LATE=2 \
  run "лог выложен не сразу — третья попытка его читает" failure "" \
  "calls:issue create" "body:--- FAIL: TestLate" "nocalls:issue close"
FAKE_JOBS='106\ta\n107\tb\n' FAKE_NO_LOG=1 LOG_WAIT=0 \
  run "логов нет, бюджет ожидания кончился — issue всё равно заводится" failure "" \
  "calls:issue create" "body:**a**" "body:**b**" "stderr:за 1 из 6 попыток" "stderr:из бюджета 0 с"
run "зелёный, два открытых — закрывает оба" success "7 8" \
  "calls:issue close 7" "calls:issue close 8" "nocalls:issue create"
FAKE_JOBS='104\treadonly\n' FAKE_LOG='FAIL\n' WORKFLOW=cloud-e2e \
  run "ночной cloud-e2e красный — своё issue" failure "" \
  "calls:issue create --title main красный: cloud-e2e" "body:**readonly**"
# У красного main есть адресат (план 1.9, строка «CI», М5): issue шесть
# часов никто не читал, а событие в открытом PR доходит до его автора.
# Новый красный ci — строка в каждый открытый PR в main со ссылкой на
# issue; зелёный, закрывший issue, — строка «снова зелёный». Дописывание
# открытого issue, cloud-e2e и stand в PR не пишут: слияние держит ci.
FAKE_JOBS='101\ttest (windows-latest)\n' FAKE_LOG='--- FAIL: TestA\n' FAKE_PRS="21 22" \
  run "красный, issue нет — строка в каждый открытый PR" failure "" \
  "calls:issue create" "calls:pr list --state open --base main" "calls:pr comment 21" "calls:pr comment 22" \
  "body:main красный" "body:https://example.invalid/o/r/issues/55" "body:автослияние" \
  "body:Эта строка — уведомление workflow main-red" "body:состояние main — issue «main красный: ci»" \
  "calls:isDraft"
FAKE_JOBS='101\ttest (windows-latest)\n' FAKE_LOG='--- FAIL: TestA\n' FAKE_PRS="21 22" \
  run "красный, issue открыто — в PR второй раз не пишет" failure 7 \
  "calls:issue comment 7" "nocalls:pr comment"
FAKE_PRS="21 22" \
  run "зелёный закрыл issue — строка в каждый открытый PR" success 7 \
  "calls:issue close 7" "calls:pr comment 21" "calls:pr comment 22" \
  "body:https://github.com/o/r/issues/7 закрыт зелёным прогоном https://example.invalid/run/41" \
  "body:Эта строка — уведомление workflow main-red" "nobody:запрет" "nobody:Запрет" "nobody:снят"
FAKE_PRS="21 22" \
  run "зелёный, issue не было — в PR тишина" success "" \
  "nocalls:pr comment" "nocalls:pr list"
FAKE_JOBS='104\treadonly\n' FAKE_LOG='FAIL\n' WORKFLOW=cloud-e2e FAKE_PRS="21" \
  run "красный cloud-e2e — в PR не пишет" failure "" \
  "calls:issue create --title main красный: cloud-e2e" "nocalls:pr comment" "nocalls:pr list"
FAKE_JOBS='101\ttest (windows-latest)\n' FAKE_LOG='--- FAIL: TestA\n' FAKE_PRS="21 22" FAKE_PR_FAIL=1 \
  run "строка в PR не принята — issue заведено, отказ назван" failure "" \
  "calls:issue create" "calls:pr comment 21" "calls:pr comment 22" "stderr:строка в PR 21 не оставлена" "stderr:HTTP 403"
FAKE_JOBS='101\ttest (windows-latest)\n' FAKE_LOG='--- FAIL: TestA\n' FAKE_PRS="21" FAKE_PR_LIST_FAIL=1 \
  run "список PR не получен — issue заведено, отказ назван, а не принят за «PR нет»" failure "" \
  "calls:issue create" "nocalls:pr comment" "stderr:список открытых PR не получен" "stderr:HTTP 502"
exit "$fail"
