#!/usr/bin/env bash
# Проверка pr_stuck.sh (workflow pr-stuck): открытый PR, который стоит
# молча, получает строку — один раз на голову. Два случая: конфликт с main
# (GitHub не запускает ci вовсе) и job прогона ci, отменённый без раннера
# (не красный, события подписчику нет). gh подменён: отдаёт JSON из файлов
# мира через --jq самого скрипта и пишет, что у него просили.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
cat >"$work/bin/gh" <<'SH'
#!/usr/bin/env bash
echo "$*" >>"$FAKE_DIR/calls"
q=.
args=("$@")
for ((i = 0; i < ${#args[@]}; i++)); do [ "${args[i]}" != --jq ] || q=${args[i + 1]}; done
give() { # ФАЙЛ УМОЛЧАНИЕ: JSON мира через фильтр вызова
  if [ -f "$FAKE_DIR/$1" ]; then jq -r "$q" "$FAKE_DIR/$1"; else jq -rn "$2 | $q"; fi
}
case "$1 $2" in
  "pr list") give prs.json '[]' ;;
  "pr comment")
    [ ! -f "$FAKE_DIR/comment.fail" ] || { echo "HTTP 403: Resource not accessible by integration" >&2; exit 1; }
    n=$3
    while [ $# -gt 0 ]; do [ "$1" != --body ] || printf '%s\n---\n' "$2" >>"$FAKE_DIR/said.$n"; shift; done ;;
  api*)
    case "$2" in
      */issues/*/comments*)
        n=${2#*/issues/}
        n=${n%%/*}
        [ ! -f "$FAKE_DIR/comments.$n.fail" ] || { echo "HTTP 502: Bad Gateway" >&2; exit 1; }
        give "comments.$n.json" '[]' ;;
      */actions/runs\?head_sha=*)
        sha=${2#*head_sha=}
        give "runs.${sha%%&*}.json" '{"workflow_runs": []}' ;;
      */actions/runs/*/jobs*)
        id=${2#*/actions/runs/}
        give "jobs.${id%%/*}.json" '{"jobs": []}' ;;
    esac ;;
esac
SH
chmod +x "$work/bin/gh"
fail=0
world() {
  export FAKE_DIR="$work/$1"
  mkdir -p "$FAKE_DIR"
  : >"$FAKE_DIR/calls"
}
run() { PATH="$work/bin:$PATH" GH_REPO=o/r "$here/pr_stuck.sh" >"$FAKE_DIR/out" 2>"$FAKE_DIR/err" || { echo "FAIL $1: скрипт упал"; cat "$FAKE_DIR/err"; fail=1; }; }
has() { grep -qF -- "$3" "$FAKE_DIR/$2" 2>/dev/null || { echo "FAIL $1: в $2 нет «$3»"; cat "$FAKE_DIR/$2" 2>/dev/null | sed 's/^/  | /'; fail=1; }; }
none() { if [ -e "$FAKE_DIR/$2" ]; then echo "FAIL $1: есть $2"; sed 's/^/  | /' "$FAKE_DIR/$2"; fail=1; fi; }

# PR с конфликтом: строка с головой и тем, что ci не запустится; черновик и
# PR без конфликта строки не получают.
world conflict
cat >"$FAKE_DIR/prs.json" <<'J'
[{"number": 21, "isDraft": false, "headRefOid": "aaaa1111bbbb2222", "mergeable": "CONFLICTING"},
 {"number": 22, "isDraft": true, "headRefOid": "cccc1111", "mergeable": "CONFLICTING"},
 {"number": 23, "isDraft": false, "headRefOid": "dddd1111", "mergeable": "MERGEABLE"},
 {"number": 24, "isDraft": false, "headRefOid": "eeee1111", "mergeable": "UNKNOWN"}]
J
run conflict
has conflict said.21 'конфликт с main'
has conflict said.21 'aaaa1111'
has conflict said.21 'GitHub не запускает ci'
has conflict said.21 '<!-- pr-stuck:conflict:aaaa1111bbbb2222 -->'
has conflict said.21 'уведомление workflow pr-stuck'
none conflict said.22
none conflict said.23
none conflict said.24

# Та же голова, строка уже оставлена: второй раз не пишет.
world conflict-again
cp "$work/conflict/prs.json" "$FAKE_DIR/prs.json"
echo '[{"body": "текст <!-- pr-stuck:conflict:aaaa1111bbbb2222 -->"}]' >"$FAKE_DIR/comments.21.json"
run conflict-again
none conflict-again said.21

# Новая голова с тем же конфликтом — новая строка.
world conflict-new-head
cp "$work/conflict/prs.json" "$FAKE_DIR/prs.json"
echo '[{"body": "<!-- pr-stuck:conflict:0000 -->"}]' >"$FAKE_DIR/comments.21.json"
run conflict-new-head
has conflict-new-head said.21 'конфликт с main'

# Комментарии PR не прочитаны: строка не пишется (иначе она повторялась бы
# каждый обход), отказ назван.
world comments-unread
cp "$work/conflict/prs.json" "$FAKE_DIR/prs.json"
: >"$FAKE_DIR/comments.21.fail"
run comments-unread
none comments-unread said.21
has comments-unread err 'комментарии PR 21 не прочитаны'
has comments-unread err 'HTTP 502'

# Job последнего прогона ci головы отменён без раннера (runner_id 0, шагов
# нет — форма job PR #1543, 12.11): строка называет job и прогон. Job,
# отменённый на ходу (раннер был, шаги есть), и красный job — не этот случай.
world norunner
echo '[{"number": 31, "isDraft": false, "headRefOid": "ffff2222", "mergeable": "MERGEABLE"}]' >"$FAKE_DIR/prs.json"
cat >"$FAKE_DIR/runs.ffff2222.json" <<'J'
{"workflow_runs": [
 {"id": 900, "name": "ci", "run_number": 10},
 {"id": 901, "name": "ci", "run_number": 12},
 {"id": 902, "name": "qwen-review", "run_number": 99}]}
J
cat >"$FAKE_DIR/jobs.901.json" <<'J'
{"jobs": [
 {"name": "test (macos-latest)", "conclusion": "cancelled", "runner_id": 0},
 {"name": "test (macos-13)", "conclusion": "cancelled", "runner_id": null, "steps": []},
 {"name": "wrapper", "conclusion": "cancelled"},
 {"name": "test (windows-latest)", "conclusion": "cancelled", "runner_id": 7, "steps": [{"name": "Set up job"}]},
 {"name": "test (ubuntu-latest)", "conclusion": "failure", "runner_id": 8, "steps": [{"name": "Set up job"}]},
 {"name": "docker", "conclusion": "success", "runner_id": 9, "steps": [{"name": "Set up job"}]}]}
J
run norunner
# runner_id 0, null и отсутствующий, steps пустой и отсутствующий — одно и то же.
has norunner said.31 'test (macos-latest), test (macos-13), wrapper'
has norunner said.31 'отменён без раннера'
has norunner said.31 'https://github.com/o/r/actions/runs/901'
has norunner said.31 '<!-- pr-stuck:norunner:ffff2222 -->'
if grep -q 'windows-latest\|ubuntu-latest\|docker' "$FAKE_DIR/said.31" 2>/dev/null; then echo "FAIL norunner: назван job, который раннер получил"; fail=1; fi
has norunner calls 'actions/runs/901/jobs'
if grep -q 'actions/runs/900/jobs' "$FAKE_DIR/calls"; then echo "FAIL norunner: читается не последний прогон ci"; fail=1; fi

# Прогон ci без таких job и PR без прогона ci — тишина.
world quiet
echo '[{"number": 41, "isDraft": false, "headRefOid": "aaaa3333", "mergeable": "MERGEABLE"}, {"number": 42, "isDraft": false, "headRefOid": "bbbb3333", "mergeable": "MERGEABLE"}]' >"$FAKE_DIR/prs.json"
echo '{"workflow_runs": [{"id": 950, "name": "ci", "run_number": 3}]}' >"$FAKE_DIR/runs.aaaa3333.json"
echo '{"jobs": [{"name": "docker", "conclusion": "success", "runner_id": 9, "steps": [{"name": "x"}]}]}' >"$FAKE_DIR/jobs.950.json"
run quiet
none quiet said.41
none quiet said.42

# Строка не принята: отказ назван, обход идёт дальше ко второму PR.
world refused
echo '[{"number": 51, "isDraft": false, "headRefOid": "aaaa4444", "mergeable": "CONFLICTING"}, {"number": 52, "isDraft": false, "headRefOid": "bbbb4444", "mergeable": "CONFLICTING"}]' >"$FAKE_DIR/prs.json"
: >"$FAKE_DIR/comment.fail"
run refused
has refused err 'строка в PR 51 не оставлена'
has refused err 'строка в PR 52 не оставлена'
has refused err 'HTTP 403'

[ "$fail" = 0 ] && echo "pr_stuck: ok"
exit "$fail"
