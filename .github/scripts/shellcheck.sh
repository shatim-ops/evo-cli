#!/usr/bin/env bash
# Одна версия shellcheck в CI и у сессий (план 1.9, строка «Локальная
# проверка скриптов — той же версией shellcheck, что в CI»; #1523, #1457):
# раннер брал shellcheck из своего образа, сессии — свой, и замечание,
# на котором падал раннер, локально не выходило — красный круг CI стоил
# 15–20 минут.
#
# Вызов: shellcheck.sh ФАЙЛ… — shellcheck версии VERSION: установленный, если его
# версия та же, иначе релиз koalaman/shellcheck (Linux x86_64), сверенный
# по SHA256 и сохранённый в кэше (SHELLCHECK_CACHE, по умолчанию
# ~/.cache/evo-shellcheck). Другая платформа без нужной версии — отказ с
# причиной, а не проверка чужой версией. Версию меняет PR, который
# прогоняет её по всем скриптам CI.
set -euo pipefail
VERSION=0.11.0
SHA256=8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198
have=$(shellcheck --version 2>/dev/null | sed -n 's/^version: //p' || true)
if [ "$have" = "$VERSION" ]; then
	exec shellcheck "$@"
fi
dir=${SHELLCHECK_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/evo-shellcheck}/$VERSION
bin=$dir/shellcheck
if [ ! -x "$bin" ]; then
	if [ "$(uname -s)/$(uname -m)" != Linux/x86_64 ]; then
		echo "shellcheck.sh: нужен shellcheck $VERSION (сейчас: ${have:-нет}); на $(uname -s)/$(uname -m) поставьте его сами" >&2
		exit 2
	fi
	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT
	curl -sSfL -o "$tmp/sc.tar.xz" \
		"https://github.com/koalaman/shellcheck/releases/download/v$VERSION/shellcheck-v$VERSION.linux.x86_64.tar.xz"
	if ! echo "$SHA256  $tmp/sc.tar.xz" | sha256sum -c --status; then
		echo "shellcheck.sh: SHA256 релиза shellcheck $VERSION не совпал — не запускаю" >&2
		exit 2
	fi
	tar -xJf "$tmp/sc.tar.xz" -C "$tmp"
	mkdir -p "$dir"
	install -m 0755 "$tmp/shellcheck-v$VERSION/shellcheck" "$bin"
fi
exec "$bin" "$@"
