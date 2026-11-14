#!/usr/bin/env bash
# Проверка shellcheck.sh на заглушках shellcheck и curl: установленный
# той же версии shellcheck запускается сам; другой версии — нет, берётся
# релиз из кэша или скачивается и сверяется по SHA256; несовпадение суммы —
# отказ без запуска.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin" "$work/rel/shellcheck-v0.11.0"
want=$(sed -n 's/^VERSION=//p' "$here/shellcheck.sh")
fake() { # путь, версия, метка
	# shellcheck disable=SC2016 # $1 и $* — заглушки, не этого скрипта
	printf '#!/usr/bin/env bash\n[ "${1:-}" != --version ] || { echo "ShellCheck"; echo "version: %s"; exit 0; }\necho "%s $*" >>"$CALLS"\n' "$2" "$3" >"$1"
	chmod +x "$1"
}
cat >"$work/bin/curl" <<'SH'
#!/usr/bin/env bash
echo "curl $*" >>"$CALLS"
while [ $# -gt 0 ]; do [ "$1" = -o ] && cp "$FAKE_TAR" "$2"; shift; done
SH
chmod +x "$work/bin/curl"
fake "$work/rel/shellcheck-v0.11.0/shellcheck" "$want" release
tar -cJf "$work/good.tar.xz" -C "$work/rel" "shellcheck-v0.11.0"
echo other >"$work/bad.tar.xz"
export CALLS=$work/calls PATH="$work/bin:$PATH" SHELLCHECK_CACHE=$work/cache
fail=0
check() { # имя, ожидаемый код, ожидаемая строка в CALLS (или -)
	if [ "$rc" = "$2" ] && { [ "$3" = - ] || grep -qF -- "$3" "$CALLS"; }; then echo "ok   $1"; else echo "FAIL $1: код $rc"; cat "$CALLS"; fail=1; fi
}
run() { : >"$CALLS"; rc=0; "$here/shellcheck.sh" a.sh >/dev/null 2>"$work/err" || rc=$?; }
patch_sum() { # подменяет сумму на сумму FAKE_TAR в копии скрипта
	sed "s/^SHA256=.*/SHA256=$(sha256sum "$FAKE_TAR" | cut -d' ' -f1)/" "$here/shellcheck.sh" >"$work/sc.sh"
	chmod +x "$work/sc.sh"
}

fake "$work/bin/shellcheck" "$want" installed
run
check "установлена та же версия — она" 0 "installed a.sh"
grep -q curl "$CALLS" && { echo "FAIL та же версия — без скачивания"; fail=1; }

fake "$work/bin/shellcheck" 0.9.0 installed
export FAKE_TAR=$work/good.tar.xz
patch_sum
: >"$CALLS"; rc=0; "$work/sc.sh" a.sh >/dev/null 2>"$work/err" || rc=$?
check "другая версия — релиз скачан и запущен" 0 "release a.sh"
grep -q "installed a.sh" "$CALLS" && { echo "FAIL другая версия запущена"; fail=1; }
: >"$CALLS"; rc=0; "$work/sc.sh" a.sh >/dev/null 2>"$work/err" || rc=$?
check "второй раз — из кэша, без скачивания" 0 "release a.sh"
grep -q curl "$CALLS" && { echo "FAIL кэш не использован"; fail=1; }

rm -rf "$work/cache"
export FAKE_TAR=$work/bad.tar.xz
run
check "сумма не совпала — отказ" 2 -
grep -q "release a.sh\|installed a.sh" "$CALLS" && { echo "FAIL при несовпадении суммы что-то запущено"; fail=1; }
grep -q SHA256 "$work/err" || { echo "FAIL отказ не называет SHA256"; fail=1; }

# Другая платформа без нужной версии — отказ с причиной, без скачивания.
cat >"$work/bin/uname" <<'SH'
#!/usr/bin/env bash
case "$1" in -s) echo Darwin ;; -m) echo arm64 ;; esac
SH
chmod +x "$work/bin/uname"
rm -rf "$work/cache"
run
check "другая платформа — отказ" 2 -
grep -q "curl\|release a.sh\|installed a.sh" "$CALLS" && { echo "FAIL другая платформа: что-то скачано или запущено"; fail=1; }
grep -q "Darwin/arm64" "$work/err" || { echo "FAIL отказ не называет платформу"; fail=1; }
exit "$fail"
