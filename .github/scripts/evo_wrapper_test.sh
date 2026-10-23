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
  wait) trap 'echo "stderr прерван $id" >&2; exit 130' INT; while :; do sleep 0.1; done ;;
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
set +e
got=$(timeout --preserve-status -s INT 1 "$dir/evo" wait 2>&1 >/dev/null); rc=$?
set -e
want "INT доходит до evo" "$rc" 130
want "stderr после INT — через redact.sed" "$got" "stderr прерван <id>"

[ "$fail" = 0 ] && echo "evo_wrapper: ok"
exit "$fail"
