#!/usr/bin/env bash
# Проверка list_filter_probe.sh без облака: curl — заглушка с ответами IAM
# и Compute. Случаи: облако отбирает как evo (same), отдаёт весь список
# (ignored), отбирает иначе (diff); повтор id в ответе облака — не diff;
# id не попадают ни в stdout, ни в stderr.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"
cat >"$work/bin/curl" <<'FAKE'
#!/usr/bin/env bash
out=""; url=""; post=0
while [ $# -gt 0 ]; do
	case "$1" in
	-o) out=$2; shift 2 ;;
	-w | -H | --data-binary) shift 2 ;;
	-X) post=1; shift 2 ;;
	-sS) shift ;;
	*) url=$1; shift ;;
	esac
done
if [ "$post" = 1 ]; then cat >/dev/null; echo '{"access_token":"tok-fake"}'; exit 0; fi
all='{"items":[{"id":"00000000-0000-4000-8000-0000000000e1","state":"running","availability_zone":{"id":"za"},"flavor":{"id":"f1"}},{"id":"00000000-0000-4000-8000-0000000000e2","state":"stopped","availability_zone":{"id":"za"},"flavor":{"id":"f1"}},{"id":"00000000-0000-4000-8000-0000000000e3","state":"running","availability_zone":{"id":"zb"},"flavor":{"id":"f2"}}]}'
case "$url" in
*vms*availability_zone_ids=za*) b='{"items":[{"id":"00000000-0000-4000-8000-0000000000e1"},{"id":"00000000-0000-4000-8000-0000000000e2"}]}' ;;
*vms*statuses=running*) b='{"items":[{"id":"00000000-0000-4000-8000-0000000000e1"}]}' ;;
*vms*statuses=stopped*) b='{"items":[{"id":"00000000-0000-4000-8000-0000000000e2"},{"id":"00000000-0000-4000-8000-0000000000e2"}]}' ;;
*vms*) b=$all ;;
*) b='{"items":[]}' ;;
esac
printf '%s' "$b" >"$out"
printf 200
FAKE
chmod +x "$work/bin/curl"
out=$(PATH="$work/bin:$PATH" KEY_ID=k SECRET=s PROJECT=p GITHUB_WORKSPACE="$here/../.." bash "$here/list_filter_probe.sh" 2>&1)
fail=0
want() { grep -qE "$1" <<<"$out" || { echo "FAIL: нет строки /$1/"; fail=1; }; }
want '^vms_zone +значение #1: всего=3 облако=2 evo=2 — same$'
want '^vms_zone +значение #2: всего=3 облако=3 evo=1 — ignored$'
want '^vms_state +значение #1: всего=3 облако=1 evo=2 — diff$'
want '^vms_state +значение #2: всего=3 облако=1 evo=1 — same$'
want '^disks_zone +всего=0, значений нет — не проверено$'
if grep -qE '0000000000e[0-9]|tok-fake|za|zb|f1|f2' <<<"$(grep -v '^::add-mask::' <<<"$out")"; then
	echo "FAIL: в выводе id, токен или значения"
	fail=1
fi
[ "$fail" = 0 ] && echo "list_filter_probe: ok"
exit "$fail"
