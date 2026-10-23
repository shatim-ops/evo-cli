#!/usr/bin/env bash
# Проверка обёртки evo из evo_wrapper.sh (job ci): stderr — через redact.sed,
# stdout — как есть (его читает jq), код выхода и сигнал — от evo.
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

set +e
out=$("$dir/evo" out 2>"$dir/err"); rc=$?
set -e
want "stdout как есть" "$out" "stdout $id"
want "stderr через redact.sed" "$(cat "$dir/err")" "stderr <id>"
want "код выхода evo" "$rc" 7

set +e
E2E_RAW_STDERR=1 "$dir/evo" out >/dev/null 2>"$dir/err"
set -e
want "E2E_RAW_STDERR=1 — stderr как есть" "$(cat "$dir/err")" "stderr $id"

# Ctrl-C (timeout -s INT, шаг «logs -f») доходит до evo, а не до обёртки.
set +e
timeout --preserve-status -s INT 1 "$dir/evo" wait 2>"$dir/err"; rc=$?
set -e
sleep 0.2
want "INT доходит до evo" "$rc" 130
want "stderr после INT — через redact.sed" "$(cat "$dir/err")" "stderr прерван <id>"

[ "$fail" = 0 ] && echo "evo_wrapper: ok"
exit "$fail"
