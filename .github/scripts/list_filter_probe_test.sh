#!/usr/bin/env bash
# Проверка list_filter_probe.sh без облака: curl — заглушка с ответами IAM
# и Compute. Случаи: облако отбирает как evo (same), отдаёт весь список
# (ignored), отбирает иначе (diff); повтор id в ответе облака — не diff;
# id не попадают ни в stdout, ни в stderr. STRICT=1: параметр облака с
# итогом не same — выход 1 и ::error:: с его именем; без STRICT — выход 0.
# Список, заполнивший страницу, — «не проверено» и ::warning::.
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
all='{"items":[{"id":"00000000-0000-4000-8000-0000000000e1","state":"running","availability_zone":{"id":"za"},"flavor":{"id":"f1"},"image":{"id":"i1"}},{"id":"00000000-0000-4000-8000-0000000000e2","state":"stopped","availability_zone":{"id":"za"},"flavor":{"id":"f1"},"image":{"id":"i1"}},{"id":"00000000-0000-4000-8000-0000000000e3","state":"running","availability_zone":{"id":"zb"},"flavor":{"id":"f2"}}]}'
if [ -n "${FAKE_BROKEN:-}" ]; then
	case "$url" in
	*vms/*) b='{"id":"x","state":{"leak":"SECRETCARD"},"image":"SECRETIMG2"}' ;;
	*vms*) b='{"items":[{"id":"bad id?x=SECRETID","state":{"leak":"SECRETSTATE"},"image":"SECRETIMG","availability_zone":{"id":"za"},"flavor":{"id":"f1"}}]}' ;;
	*) b='{"items":[]}' ;;
	esac
	printf '%s' "$b" >"$out"
	printf 200
	exit 0
fi
# FAKE_STRICT_OK: облако отбирает, как evo, по каждому параметру IN_CLOUD, а
# список состояний через запятую отвергает 422 (cloud-e2e run 216).
if [ -n "${FAKE_STRICT_OK:-}" ]; then
	case "$url" in
	*vms*statuses=running%2Cstopped*) printf '{"code":"invalid"}' >"$out"; printf 422; exit 0 ;;
	*vms*availability_zone_ids=zb*) printf '{"items":[{"id":"00000000-0000-4000-8000-0000000000e3"}]}' >"$out"; printf 200; exit 0 ;;
	*vms*statuses=running*) printf '{"items":[{"id":"00000000-0000-4000-8000-0000000000e1"},{"id":"00000000-0000-4000-8000-0000000000e3"}]}' >"$out"; printf 200; exit 0 ;;
	esac
fi
case "$url" in
*vms/00000000-0000-4000-8000-0000000000e3) b='{"id":"00000000-0000-4000-8000-0000000000e3","state":"running"}' ;;
*vms/00000000-*) b='{"id":"x","state":"running","image":{"id":"i1","name":"img-name-secret"}}' ;;
*vms*image_ids=i1*) b='{"items":[{"id":"00000000-0000-4000-8000-0000000000e1"},{"id":"00000000-0000-4000-8000-0000000000e2"}]}' ;;
*vms*vm_ids=*) id=${url##*vm_ids=}; id=${id%%&*}; b="{\"items\":[{\"id\":\"$id\"}]}" ;;
*vms*statuses=running%2Cstopped*) b=$all ;;
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
fail=0
out=$(PATH="$work/bin:$PATH" KEY_ID=k SECRET=s PROJECT=p GITHUB_WORKSPACE="$here/../.." bash "$here/list_filter_probe.sh" 2>&1) ||
	{ echo "FAIL: без STRICT выход не 0"; fail=1; }
want() { grep -qE "$1" <<<"$out" || { echo "FAIL: нет строки /$1/"; fail=1; }; }
want '^vms_zone +значение #1: всего=3 облако=2 evo=2 — same$'
want '^vms_zone +значение #2: всего=3 облако=3 evo=1 — ignored$'
want '^vms_state +значение #1: всего=3 облако=1 evo=2 — diff$'
want '^vms_state +значение #2: всего=3 облако=1 evo=1 — same$'
want '^disks_zone +всего=0, значений нет — не проверено$'
# Проба 1.9 (строка плана «Проба чтения»): отбор ВМ по образу и по id,
# состояния списком; образ в карточке; время чтения карточек.
want '^vms_image +значение #1: всего=3 облако=2 evo=2 — same$'
want '^vms_ids +значение #3: всего=3 облако=1 evo=1 — same$'
want '^vms_states_list +значение #1: всего=3 облако=3 evo=3 — same$'
want '^vm_card_image +карточек=3: с image\.id=2, с image\.name=2$'
want '^vm_card_reads +чтений=50 \(ВМ 3\): http 200=50, 429=0, прочие=0, за [0-9]+ мс$'
if grep -q 'img-name-secret\|i1' <<<"$(grep -v '^::add-mask::' <<<"$out")"; then echo "FAIL: в выводе образ"; fail=1; fi
if grep -qE '0000000000e[0-9]|tok-fake|za|zb|f1|f2' <<<"$(grep -v '^::add-mask::' <<<"$out")"; then
	echo "FAIL: в выводе id, токен или значения"
	fail=1
fi
if grep -q '^::error::' <<<"$out"; then
	echo "FAIL: без STRICT — ::error::"
	fail=1
fi
# Повреждённый ответ (#1757, #1758): state — объект, image — строка, id не
# формы UUID. Ни фрагмента ответа в выводе (stdout и stderr): ошибки jq и
# curl скрыты счётом, id чужой формы в URL не идёт.
broken=$(PATH="$work/bin:$PATH" FAKE_BROKEN=1 KEY_ID=k SECRET=s PROJECT=p GITHUB_WORKSPACE="$here/../.." bash "$here/list_filter_probe.sh" 2>&1) || true
if grep -q 'SECRET' <<<"$(grep -v '^::add-mask::' <<<"$broken")"; then
	echo "FAIL: повреждённый ответ — его фрагмент в выводе:"; grep 'SECRET' <<<"$broken" | sed 's/^/  | /'; fail=1
fi
grep -qE '^vm_card_image +id не формы UUID: 1 — карточки не читались$' <<<"$broken" || { echo "FAIL: id чужой формы не назван"; printf '%s\n' "$broken" | sed 's/^/  | /'; fail=1; }
code=0
strict=$(PATH="$work/bin:$PATH" STRICT=1 KEY_ID=k SECRET=s PROJECT=p GITHUB_WORKSPACE="$here/../.." bash "$here/list_filter_probe.sh" 2>&1) || code=$?
if [ "$code" != 1 ] || ! grep -qE '^::error::.* vms:availability_zone_ids vms:statuses — ' <<<"$strict"; then
	echo "FAIL: STRICT=1 — выход $code, нужна ошибка по vms:availability_zone_ids и vms:statuses:"
	grep '^::error::' <<<"$strict" || true
	fail=1
fi
# Проверка-вопрос (ASKS): форма, которой evo облаку не шлёт, — states у evo
# отбирает локально. Её отказ печатается, но строгую рамку не роняет
# (cloud-e2e run 216: 422 на «a,b» засчитан как расхождение vms:statuses).
code=0
ok=$(PATH="$work/bin:$PATH" FAKE_STRICT_OK=1 STRICT=1 KEY_ID=k SECRET=s PROJECT=p GITHUB_WORKSPACE="$here/../.." bash "$here/list_filter_probe.sh" 2>&1) || code=$?
if [ "$code" != 0 ] || grep -q '^::error::' <<<"$ok" || ! grep -qE '^vms_states_list +значение #1: http=422 — error$' <<<"$ok"; then
	echo "FAIL: STRICT=1, IN_CLOUD как у evo, список через запятую — 422: выход $code, ждали 0 и строку отказа без ::error::"
	printf '%s\n' "$ok" | grep -E '::error::|vms_state' | sed 's/^/  | /'
	fail=1
fi
# Полная страница: в заглушке у vms 3 элемента — при PROBE_PAGE=3 vms не
# проверяется, а говорит об этом.
page=$(PATH="$work/bin:$PATH" PROBE_PAGE=3 KEY_ID=k SECRET=s PROJECT=p GITHUB_WORKSPACE="$here/../.." bash "$here/list_filter_probe.sh" 2>&1)
if ! grep -qE '^vms_zone +всего=3 — страница полная, не проверено$' <<<"$page" ||
	! grep -q '^::warning::vms_zone: список заполнил страницу (3)' <<<"$page" ||
	grep -qE '^vms_zone +значение' <<<"$page"; then
	echo "FAIL: полная страница не названа или проверена"
	fail=1
fi
[ "$fail" = 0 ] && echo "list_filter_probe: ok"
exit "$fail"
