#!/usr/bin/env bash
# Проверка aif_secret_scan.sh (ai-factory-probe): значение секрета в
# отчёте — код 1 и имя переменной, без значения и без файла; чистый отчёт —
# код 0; пустой каталог — ошибка; короткое значение — предупреждение.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fail=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/clean/k8s" "$work/dirty/s3" "$work/empty"
echo '{"tools":[{"name":"list_clusters"}]}' >"$work/clean/k8s/tools.json"
echo '{"answer":"token=tok-abcdef-123456 here"}' >"$work/dirty/s3/reads.json"
export AIF_S3_TOKEN=tok-abcdef-123456 AIF_K8S_URL=https://mcp.example.test/mcp SHORT=abc
# case ИМЯ КАТАЛОГ ОЖИДАЕМЫЙ_КОД
case_() {
	local rc
	set +e
	"$here/aif_secret_scan.sh" "$work/$2" AIF_S3_TOKEN AIF_K8S_URL SHORT UNSET_VAR >"$work/$1.out" 2>&1
	rc=$?
	set -e
	[ "$rc" = "$3" ] || { printf 'FAIL %s: код %s, ожидался %s\n' "$1" "$rc" "$3"; sed 's/^/  /' "$work/$1.out"; fail=1; }
}
case_ clean clean 0
case_ dirty dirty 1
case_ empty empty 1
grep -q 'AIF_S3_TOKEN' "$work/dirty.out" || { echo "FAIL dirty: имя переменной не названо"; fail=1; }
! grep -q 'tok-abcdef-123456\|reads.json\|/s3/' "$work/dirty.out" || { echo "FAIL dirty: значение или место секрета в выводе"; fail=1; }
grep -q '::warning::SHORT' "$work/clean.out" || { echo "FAIL clean: нет предупреждения о коротком значении"; fail=1; }
[ "$fail" = 0 ] && echo "aif_secret_scan: ok"
exit "$fail"
