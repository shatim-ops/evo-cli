#!/usr/bin/env bash
# Проверка aif_push.sh (ai-factory-probe) на локальном репозитории вместо
# evo-internal: отчёт — в новую ветку probe/ai-factory-<run>, main не
# меняется; та же ветка второй раз и номер прогона не числом — отказ; токена
# нет ни в выводе, ни в истории.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fail=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
git init -q --bare "$work/internal.git"
git init -q "$work/seed"
(cd "$work/seed" && git config user.name t && git config user.email t@example.test &&
	echo base >README.md && git add README.md && git commit -q -m base &&
	git branch -M main && git -c push.negotiate=false push -q "$work/internal.git" main)
main_before=$(git -C "$work/internal.git" rev-parse main)
mkdir -p "$work/report/k8s"
echo '{"count":3}' >"$work/report/k8s/tools.json"
export AIF_INTERNAL_REMOTE="$work/internal.git" EVO_INTERNAL_PUSH_TOKEN=ghp_fake_token_0123456789
run() { # ИМЯ RUN ОЖИДАЕМЫЙ_КОД
	local rc
	set +e
	"$here/aif_push.sh" "$work/report" "$2" >"$work/$1.out" 2>&1
	rc=$?
	set -e
	[ "$rc" = "$3" ] || { printf 'FAIL %s: код %s, ожидался %s\n' "$1" "$rc" "$3"; sed 's/^/  /' "$work/$1.out"; fail=1; }
}
run first 4242 0
run again 4242 1
run bad-run 'x;rm' 1
git -C "$work/internal.git" rev-parse -q --verify refs/heads/probe/ai-factory-4242 >/dev/null ||
	{ echo "FAIL first: нет ветки probe/ai-factory-4242"; fail=1; }
[ "$(git -C "$work/internal.git" rev-parse main)" = "$main_before" ] || { echo "FAIL first: main изменён"; fail=1; }
[ "$(git -C "$work/internal.git" show probe/ai-factory-4242:runs/ai-factory-probe-4242/k8s/tools.json)" = '{"count":3}' ] ||
	{ echo "FAIL first: отчёта нет в ветке"; fail=1; }
[ "$(git -C "$work/internal.git" for-each-ref --format='%(refname)' refs/heads | wc -l)" = 2 ] ||
	{ echo "FAIL: веток не две (main и отчёт)"; fail=1; }
! cat "$work"/*.out | grep -v '^::add-mask::' | grep -q 'ghp_fake_token_0123456789' || { echo "FAIL: токен в выводе"; fail=1; }
! git -C "$work/internal.git" log --all -p | grep -q 'ghp_fake_token' || { echo "FAIL: токен в истории"; fail=1; }
grep -q '::add-mask::ghp_fake_token_0123456789' "$work/first.out" || { echo "FAIL first: токен без маски"; fail=1; }
[ "$fail" = 0 ] && echo "aif_push: ok"
exit "$fail"
