#!/usr/bin/env bash
# Проверка обёртки evo из evo_wrapper.sh (job ci): stderr в канал — через
# redact.sed, stderr в файл — как есть и дописан к возврату вызова, stdout —
# как есть (его читает jq), код выхода и сигнал — от evo.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
id=0b5e9a6c-1111-2222-3333-444455556666
cat > "$dir/evo-real" <<FAKE
#!/usr/bin/env bash
case "\$1" in
  out) echo "stdout $id"; echo "stderr $id" >&2; exit 7 ;;
  wait) trap 'echo "stderr прерван $id" >&2; exit 130' INT; echo "\$\$" >"$dir/pid"; echo "stderr готов $id" >&2; while :; do sleep 0.1; done ;;
esac
FAKE
chmod +x "$dir/evo-real"
"$here/evo_wrapper.sh" "$dir/evo-real" "$here/redact.sed" "$dir/evo"
fail=0
want() { if [ "$2" != "$3" ]; then printf 'FAIL %s\n  got:  %q\n  want: %q\n' "$1" "$2" "$3"; fail=1; fi; }

# stderr в канал (лог шага, 2>&1 | jq): маска; читатель ждёт конца канала.
set +e
got=$("$dir/evo" out 2>&1 >"$dir/out"); rc=$?
set -e
want "stderr в канал — через redact.sed" "$got" "stderr <id>"
want "stdout как есть" "$(cat "$dir/out")" "stdout $id"
want "код выхода evo" "$rc" 7

# stderr в файл: как есть и уже дописан, когда вызов вернулся.
set +e
"$dir/evo" out >/dev/null 2>"$dir/err"
set -e
want "stderr в файл — как есть, сразу" "$(cat "$dir/err")" "stderr $id"

# Ctrl-C (timeout -s INT, шаг «logs -f») доходит до evo, а не до обёртки.
# INT — всей группе timeout (evo и sed маски), как шлёт сам timeout, но не по
# часам, а когда первая строка evo прошла через sed: обработчик evo и
# ловушка sed уже стоят. По часам (timeout 1 с) INT на занятом раннере
# приходил раньше ловушек, и stderr пропадал (release rc.2, run 40100673928).
set +e
{ timeout --preserve-status -s INT 60 "$dir/evo" wait 2>&1 >/dev/null; echo "$?" >"$dir/int.rc"; } | cat >"$dir/int.out" &
reader=$!
ready=нет
for _ in $(seq 1 600); do grep -q 'готов' "$dir/int.out" 2>/dev/null && { ready=да; break; }; sleep 0.05; done
pid=$(cat "$dir/pid" 2>/dev/null)
pg=$(ps -o pgid= -p "${pid:-0}" 2>/dev/null | tr -d ' ')
own=$(ps -o pgid= -p $$ | tr -d ' ')
# Что evo игнорирует на входе (Linux): игнорируемый на входе INT bash
# перехватить не может (ubuntu-26.04, timeout из uutils: проба #321 висела).
ign=$(awk '/^SigIgn:/ {print $2}' "/proc/${pid:-0}/status" 2>/dev/null)
# timeout без своей группы (GNU делает setpgid) — INT ушёл бы и в этот тест.
if [ -n "$pg" ] && [ "$pg" != "$own" ]; then kill -INT -- "-$pg" 2>/dev/null; elif [ -n "$pid" ]; then kill -INT "$pid" 2>/dev/null; fi
# Не дождались — убить и упасть, а не висеть до лимита job.
for _ in $(seq 1 200); do [ -s "$dir/int.rc" ] && break; sleep 0.05; done
if [ ! -s "$dir/int.rc" ]; then
	printf 'FAIL evo не завершился после INT (SigIgn evo: %s)\n' "${ign:-?}"
	fail=1
	if [ -n "$pg" ] && [ "$pg" != "$own" ]; then kill -KILL -- "-$pg" 2>/dev/null; fi
	parent=$(ps -o ppid= -p "${pid:-0}" 2>/dev/null | tr -d ' ')
	[ -z "$pid" ] || kill -KILL "$pid" 2>/dev/null
	[ -z "$parent" ] || [ "$parent" = $$ ] || kill -KILL "$parent" 2>/dev/null
fi
wait "$reader"
set -e
want "evo запустился и вывел строку через sed" "$ready" да
want "timeout ведёт свою группу процессов" "$([ -n "$pg" ] && [ "$pg" != "$own" ] && echo да)" да
want "INT доходит до evo" "$(cat "$dir/int.rc" 2>/dev/null)" 130
want "stderr после INT — через redact.sed" "$(cat "$dir/int.out")" "$(printf 'stderr готов <id>\nstderr прерван <id>')"

[ "$fail" = 0 ] && echo "evo_wrapper: ok"
exit "$fail"
