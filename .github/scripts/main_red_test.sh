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
    # gh prints the address of the comment it made — a caller that reads
    # its own stdout must not take it for an answer.
    [ "$2" != comment ] || echo "https://example.invalid/o/r/issues/$3#issuecomment-1"
    while [ $# -gt 0 ]; do [ "$1" = --body-file ] && cat "$2" >>"$FAKE_BODY"; [ "$1" = --body ] && printf '%s\n' "$2" >>"$FAKE_BODY"; shift; done ;;
  "issue close") ;;
  "pr list")
    [ -z "${FAKE_PR_LIST_FAIL:-}" ] || { echo "HTTP 502: Bad Gateway" >&2; exit 1; }
    # The list as gh gives it — JSON through the caller's own --jq, so the
    # filter of main_red.sh is what picks the numbers; «23d» is a draft.
    q=.
    while [ $# -gt 0 ]; do [ "$1" != --jq ] || q=$2; shift; done
    for n in ${FAKE_PRS:-}; do
      case "$n" in *d) printf '{"number":%s,"isDraft":true}\n' "${n%d}" ;; *) printf '{"number":%s,"isDraft":false}\n' "$n" ;; esac
    done | jq -s . | jq -r "$q" ;;
  "pr comment")
    [ -z "${FAKE_PR_FAIL:-}" ] || { echo "HTTP 403: Resource not accessible by integration" >&2; exit 1; }
    while [ $# -gt 0 ]; do [ "$1" = --body ] && printf '%s\n' "$2" >>"$FAKE_BODY"; shift; done ;;
  api*)
    q=.
    a=("$@")
    for ((i = 0; i < ${#a[@]}; i++)); do [ "${a[i]}" != --jq ] || q=${a[i + 1]}; done
    case "$2" in
      */issues/*/comments*)
        [ -z "${FAKE_MARKS_FAIL:-}" ] || { echo "HTTP 502: Bad Gateway" >&2; exit 1; }
        jq -r "$q" <<<"${FAKE_MARKS:-[]}" ;;
      */issues/[0-9]*)
        d='{"user": {"login": "github-actions[bot]"}, "body": ""}'
        jq -r "$q" <<<"${FAKE_ISSUE:-$d}" ;;
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
    FAKE_READS="$work/reads" FAKE_LOG_LATE="${FAKE_LOG_LATE:-0}" FAKE_PRS="${FAKE_PRS:-}" FAKE_PR_FAIL="${FAKE_PR_FAIL:-}" FAKE_MARKS="${FAKE_MARKS:-}" FAKE_MARKS_FAIL="${FAKE_MARKS_FAIL:-}" FAKE_ISSUE="${FAKE_ISSUE:-}" REPO_DIR="$work/repo" \
    HEAD_SHA="${HEAD_SHA:-0123456789abcdef}" FAKE_PR_LIST_FAIL="${FAKE_PR_LIST_FAIL:-}" LOG_RETRY_SLEEP=0 LOG_WAIT="${LOG_WAIT:-120}" \
    WORKFLOW="${WORKFLOW:-ci}" GH_REPO=o/r RUN_ID=41 RUN_URL=https://example.invalid/run/41 CONCLUSION="$2" \
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

# Репозиторий мира: красный коммит RED; NOFIX — после него менялся не файл
# упавшего теста; FIXA — менялся файл TestA; FIXAB — файлы обоих тестов.
git init -q "$work/repo"
git -C "$work/repo" config user.email t@example.invalid
git -C "$work/repo" config user.name t
commit() { git -C "$work/repo" add -A && git -C "$work/repo" commit -q -m "$1" && git -C "$work/repo" rev-parse HEAD; }
mkdir "$work/repo/pkg"
printf 'package pkg\n\nfunc TestA(t *testing.T) {}\n' >"$work/repo/pkg/a_test.go"
printf 'package pkg\n\nfunc TestB(t *testing.T) {}\n' >"$work/repo/pkg/b_test.go"
RED=$(commit red)
echo doc >"$work/repo/README"
NOFIX=$(commit nofix)
echo '// fixed' >>"$work/repo/pkg/a_test.go"
FIXA=$(commit fixa)
echo '// fixed' >>"$work/repo/pkg/b_test.go"
FIXAB=$(commit fixab)
bot() { printf '[{"user": {"login": "github-actions[bot]"}, "body": "%s"}]' "$1"; }

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
FAKE_MARKS=$(bot "<!-- main-red:red:$RED:TestA -->") HEAD_SHA=$FIXA \
  run "зелёный, файл упавшего теста менялся — закрывает" success 7 \
  "calls:issue comment 7" "calls:issue close 7" "nocalls:issue create" "body:pkg/a_test.go"
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
FAKE_MARKS=$(bot "<!-- main-red:red:$RED:TestA -->") HEAD_SHA=$FIXA \
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
FAKE_JOBS='101\ttest (windows-latest)\n' FAKE_LOG='--- FAIL: TestA\n' FAKE_PRS="21 22 23d" \
  run "красный, issue нет — строка в каждый открытый PR, кроме черновика" failure "" \
  "calls:issue create" "calls:pr list --state open --base main" "calls:pr comment 21" "calls:pr comment 22" \
  "body:main красный" "body:https://example.invalid/o/r/issues/55" "body:автослияние" \
  "body:Эта строка — уведомление workflow main-red" "body:состояние main — issue «main красный: ci»" \
  "nocalls:pr comment 23"
FAKE_JOBS='101\ttest (windows-latest)\n' FAKE_LOG='--- FAIL: TestA\n' FAKE_PRS="21 22" \
  run "красный, issue открыто — в PR второй раз не пишет" failure 7 \
  "calls:issue comment 7" "nocalls:pr comment"
FAKE_PRS="21 22" FAKE_MARKS=$(bot "<!-- main-red:red:$RED:TestA -->") HEAD_SHA=$FIXA \
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
# Автозакрытие по варианту Б (план 1.9, строка «CI», решение владельца
# 13.11; два эпизода ночи 12–13.11): зелёный закрывает issue, только если
# между красным и зелёным коммитом менялся файл упавшего теста. Иначе тест
# прошёл сам — issue остаётся открытым с пометкой, закрывает назвавший
# причину.
FAKE_JOBS='101\ttest (windows-latest)\n' FAKE_LOG='--- FAIL: TestA (0.07s)\n    --- FAIL: TestA/sub (0.01s)\n--- FAIL: TestB (0.01s)\n' \
  run "красный — issue несёт метку: коммит и упавшие тесты" failure "" \
  "body:<!-- main-red:red:0123456789abcdef:TestA,TestB -->" "body:прошёл без правки"
FAKE_PRS="21" FAKE_MARKS=$(bot "<!-- main-red:red:$RED:TestA -->") HEAD_SHA=$NOFIX \
  run "зелёный, файл упавшего теста не менялся — issue открыт, пометка" success 7 \
  "nocalls:issue close" "calls:issue comment 7" "body:прошёл без правки: TestA, ${RED:0:8}…${NOFIX:0:8}" \
  "body:в тот же день" "body:<!-- main-red:held -->" "nocalls:pr comment"
# Метка в теле issue — так её оставляет красный прогон, который issue завёл.
FAKE_ISSUE=$(printf '{"user": {"login": "github-actions[bot]"}, "body": "красный <!-- main-red:red:%s:TestA -->"}' "$RED") HEAD_SHA=$NOFIX \
  run "метка в теле issue от бота — читается" success 7 \
  "nocalls:issue close" "body:прошёл без правки: TestA"
FAKE_ISSUE=$(printf '{"user": {"login": "someone"}, "body": "<!-- main-red:red:%s:TestA -->"}' "$RED") HEAD_SHA=$FIXA \
  run "issue завёл не бот — метка в его теле не в счёт" success 7 \
  "nocalls:issue close" "body:упавший тест не назван"
FAKE_MARKS=$(bot "<!-- main-red:red:$RED:TestA,TestB -->") HEAD_SHA=$FIXA \
  run "зелёный, из двух упавших тестов менялся файл одного — открыт, назван второй" success 7 \
  "nocalls:issue close" "body:прошёл без правки: TestB, ${RED:0:8}…${FIXA:0:8}"
FAKE_MARKS=$(bot "<!-- main-red:red:$RED:TestA,TestB -->") HEAD_SHA=$FIXAB \
  run "зелёный, файлы обоих тестов менялись — закрывает" success 7 \
  "calls:issue close 7"
FAKE_MARKS=$(printf '[{"user": {"login": "someone"}, "body": "<!-- main-red:red:%s:TestA -->"}]' "$RED") HEAD_SHA=$FIXA \
  run "метка не от бота — не в счёт: тесты не названы, issue открыт" success 7 \
  "nocalls:issue close" "body:упавший тест не назван"
HEAD_SHA=$FIXA \
  run "красный ci без названных тестов — зелёный не закрывает" success 7 \
  "nocalls:issue close" "body:упавший тест не назван" "body:<!-- main-red:held -->"
FAKE_MARKS=$(bot "прошёл без правки <!-- main-red:held -->") HEAD_SHA=$NOFIX \
  run "пометка уже оставлена последним словом бота — второй зелёный молчит" success 7 \
  "nocalls:issue close" "nocalls:issue comment"
FAKE_MARKS=$(bot "<!-- main-red:red:00000000deadbeef00000000deadbeef00000000:TestA -->") HEAD_SHA=$FIXA \
  run "красного коммита в репозитории нет — сравнить нельзя, issue открыт" success 7 \
  "nocalls:issue close" "body:сравнить не удалось" "stderr:коммиты не сравнены"
FAKE_MARKS=$(bot "<!-- main-red:red:$RED:TestGone -->") HEAD_SHA=$FIXA \
  run "теста с таким именем в красном коммите нет — сравнить нельзя, issue открыт" success 7 \
  "nocalls:issue close" "body:сравнить не удалось"
FAKE_MARKS_FAIL=1 HEAD_SHA=$FIXA \
  run "метки issue не прочитаны — issue открыт, отказ назван" success 7 \
  "nocalls:issue close" "stderr:метки issue 7 не прочитаны"
WORKFLOW=stand \
  run "зелёный stand — закрывает как раньше: тестов у него нет" success 7 \
  "calls:issue close 7"
# Сравнение коммитов отдельной командой — её же шаг ci гоняет на раннере на
# двух настоящих эпизодах.
verdict() { (cd "$work/repo" && "$here/main_red.sh" changed "$1" "$2" "$3"); }
[ "$(verdict "$RED" "$NOFIX" TestA)" = "same pkg/a_test.go" ] || { echo "FAIL changed: не «same»"; fail=1; }
[ "$(verdict "$RED" "$FIXA" TestA)" = "changed pkg/a_test.go" ] || { echo "FAIL changed: не «changed»"; fail=1; }
[ "$(verdict "$RED" "$FIXA" TestGone 2>/dev/null)" = "unknown" ] || { echo "FAIL changed: не «unknown»"; fail=1; }
exit "$fail"
