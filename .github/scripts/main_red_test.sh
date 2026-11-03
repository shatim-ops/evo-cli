#!/usr/bin/env bash
# Проверка main_red.sh (workflow main-red): красный прогон ci на main
# заводит issue «main красный: ci» с упавшими job и строками «--- FAIL» или
# дописывает открытое, зелёный — закрывает открытое, прочие исходы
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
    while [ $# -gt 0 ]; do [ "$1" = --body-file ] && cat "$2" >>"$FAKE_BODY"; [ "$1" = --body ] && printf '%s\n' "$2" >>"$FAKE_BODY"; shift; done ;;
  "issue close") ;;
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
run() { # ИМЯ исход открытое ждём…
  : >"$work/calls"
  : >"$work/body"
  rm -f "$work/reads"
  PATH="$work/bin:$PATH" FAKE_CALLS="$work/calls" FAKE_BODY="$work/body" FAKE_OPEN="$3" \
    FAKE_JOBS="${FAKE_JOBS:-}" FAKE_LOG="${FAKE_LOG:-}" FAKE_NO_LOG="${FAKE_NO_LOG:-}" \
    FAKE_READS="$work/reads" FAKE_LOG_LATE="${FAKE_LOG_LATE:-0}" LOG_RETRY_SLEEP=0 \
    GH_REPO=o/r RUN_ID=41 RUN_URL=https://example.invalid/run/41 CONCLUSION="$2" HEAD_SHA=0123456789abcdef \
    "$here/main_red.sh" >/dev/null 2>"$work/stderr" || { echo "FAIL $1: скрипт упал"; cat "$work/stderr"; fail=1; return; }
  local name=$1
  shift 3
  local ok=1
  for want in "$@"; do
    case "$want" in
      calls:*) grep -q -- "${want#calls:}" "$work/calls" || ok=0 ;;
      nocalls:*) ! grep -q -- "${want#nocalls:}" "$work/calls" || ok=0 ;;
      body:*) grep -qF -- "${want#body:}" "$work/body" || ok=0 ;;
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
  "stderr:HTTP 404: Not Found"
FAKE_JOBS='105\ttest (windows-latest)\n' FAKE_LOG='--- FAIL: TestLate\n' FAKE_LOG_LATE=2 \
  run "лог выложен не сразу — третья попытка его читает" failure "" \
  "calls:issue create" "body:--- FAIL: TestLate" "nocalls:issue close"
run "зелёный, два открытых — закрывает оба" success "7 8" \
  "calls:issue close 7" "calls:issue close 8" "nocalls:issue create"
exit "$fail"
