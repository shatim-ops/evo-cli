#!/usr/bin/env bash
# Проверка no_runner.sh (workflow no-runner): первая попытка прогона ci, в
# которой job отменён без раннера (раннер не выдан, ни одного шага, отмена
# через 14+ мин ожидания), повторяется один раз — и только когда других
# исходов, кроме успеха и пропуска, в ней нет и голова PR не сдвинулась.
# Всё прочее (настоящий провал, отмена новым пушем, вторая попытка) не
# трогается. gh подменён: он пишет, что у него просили.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
cat >"$work/bin/gh" <<'SH'
#!/usr/bin/env bash
echo "$*" >>"$FAKE_CALLS"
[ "$1" = api ] || { echo "gh: не api: $*" >&2; exit 2; }
shift
post=0
if [ "$1" = -X ]; then [ "$2" = POST ] && post=1; shift 2; fi
q=.
a=("$@")
for ((i = 0; i < ${#a[@]}; i++)); do [ "${a[i]}" != --jq ] || q=${a[i + 1]}; done
case "$1" in
*/actions/runs/*/attempts/1/jobs*)
	[ -z "${FAKE_JOBS_FAIL:-}" ] || { echo "HTTP 502: Bad Gateway" >&2; exit 1; }
	jq -r "$q" <<<"$FAKE_JOBS" ;;
*/git/ref/heads/*) jq -r "$q" <<<"{\"object\": {\"sha\": \"${FAKE_HEAD:-abc}\"}}" ;;
*/rerun-failed-jobs) [ "$post" = 1 ] || { echo "gh: повтор не POST" >&2; exit 2; } ;;
*) echo "gh: неизвестный путь $1" >&2; exit 2 ;;
esac
SH
chmod +x "$work/bin/gh"
export PATH="$work/bin:$PATH" FAKE_CALLS="$work/calls" GH_REPO=o/r RUN_ID=7 HEAD_BRANCH=feature/x
fail=0

# job JSON: name conclusion runner_id steps waited-minutes
job() {
	jq -nc --arg n "$1" --arg c "$2" --argjson r "$3" --argjson s "$4" --argjson m "$5" \
		'{name: $n, conclusion: $c, runner_id: $r, steps: [range($s) | {number: .}],
		  created_at: "2026-11-13T16:23:34Z",
		  completed_at: ("2026-11-13T16:23:34Z" | fromdateiso8601 + $m * 60 | todateiso8601)}'
}
jobs() { jq -sc '{jobs: .}'; }
starved=$(job "test (macos-latest)" cancelled null 0 15)
ubuntu=$(job "test (ubuntu-latest)" success 101 62 14)
skipped=$(job dev-build skipped null 0 0)

# run EVENT ATTEMPT HEAD_SHA — запуск скрипта; код и вывод — в $rc и $out.
run() {
	: >"$FAKE_CALLS"
	rc=0
	out=$(EVENT=$1 RUN_ATTEMPT=$2 HEAD_SHA=$3 "$here/no_runner.sh" 2>&1) || rc=$?
}
reruns() { grep -c 'rerun-failed-jobs' "$FAKE_CALLS" || true; }
check() {
	local what=$1 want=$2
	if [ "$rc" != 0 ]; then echo "FAIL $what: код $rc"; printf '%s\n' "$out" | sed 's/^/  | /'; fail=1; return; fi
	if [ "$(reruns)" != "$want" ]; then echo "FAIL $what: повторов $(reruns), ждали $want"; printf '%s\n' "$out" | sed 's/^/  | /'; fail=1; fi
}

# 1. push в main: macOS без раннера, прочее зелёное — повтор, строка-notice
# называет job (по ней считается «после»).
FAKE_JOBS=$(printf '%s\n' "$starved" "$ubuntu" "$skipped" | jobs) run push 1 abc
check "push, без раннера" 1
grep -q '::notice::.*test (macos-latest)' <<<"$out" || { echo "FAIL push: notice не называет job"; fail=1; }
grep -q '^api -X POST repos/o/r/actions/runs/7/rerun-failed-jobs' "$FAKE_CALLS" || { echo "FAIL push: повтор не того прогона"; cat "$FAKE_CALLS"; fail=1; }

# 2. PR на актуальной голове — повтор; «без раннера» и в форме runner_id 0
# и steps null (так тот же job отдаёт другой клиент API).
FAKE_HEAD=abc FAKE_JOBS=$(printf '%s\n' "$(jq -c '.runner_id = 0 | .steps = null' <<<"$starved")" "$ubuntu" | jobs) run pull_request 1 abc
check "PR на голове" 1

# 3. PR, голова сдвинулась (отмену мог сделать новый пуш; новый прогон и так
# идёт) — без повтора.
FAKE_HEAD=def FAKE_JOBS=$(printf '%s\n' "$starved" "$ubuntu" | jobs) run pull_request 1 abc
check "PR, голова сдвинулась" 0

# 4. Рядом настоящий провал — без повтора: повтор провала — решение «релиза»
# (CLAUDE.md §0, правило 2), а не автомата.
FAKE_JOBS=$(printf '%s\n' "$starved" "$(job "test (ubuntu-latest)" failure 101 62 9)" | jobs) run push 1 abc
check "рядом провал" 0
grep -q 'test (ubuntu-latest)' <<<"$out" || { echo "FAIL рядом провал: вывод не называет провал"; fail=1; }

# 5. Отмена с раннером (шаги шли) — не «без раннера».
FAKE_JOBS=$(printf '%s\n' "$(job "test (macos-latest)" cancelled 202 12 15)" "$ubuntu" | jobs) run push 1 abc
check "отмена с раннером" 0

# 6. Отмена без раннера раньше 14 мин (отмена пушем, отменой вручную) — нет.
FAKE_JOBS=$(printf '%s\n' "$(job "test (macos-latest)" cancelled null 0 3)" "$ubuntu" | jobs) run push 1 abc
check "отмена до срока ожидания" 0

# 7. Вторая попытка — никогда: второй отказ выдачи — «релизу» вручную.
FAKE_JOBS=$(printf '%s\n' "$starved" "$ubuntu" | jobs) run push 2 abc
check "вторая попытка" 0
if grep -q 'jobs' "$FAKE_CALLS"; then echo "FAIL вторая попытка: читал job"; fail=1; fi

# 8. Список job не прочитан — без повтора, код 0, причина в выводе.
FAKE_JOBS_FAIL=1 FAKE_JOBS='{}' run push 1 abc
check "список не прочитан" 0
grep -q 'не прочитан' <<<"$out" || { echo "FAIL список не прочитан: причины нет"; printf '%s\n' "$out"; fail=1; }

# 9. Без раннера — нет ни одного (весь прогон отменён другим путём) — нет.
FAKE_JOBS=$(printf '%s\n' "$ubuntu" "$skipped" | jobs) run push 1 abc
check "без раннера нет" 0

[ "$fail" = 0 ] || exit 1
echo "no-runner: ok"
