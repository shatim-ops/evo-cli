#!/usr/bin/env bash
# Tab в zsh после install.sh (#718, T5–T6; план «Процесса» 1.7 §5) — на
# настоящем zsh, временном HOME и локальном выпуске (CLOUDRU_VM_BASE_URL
# file://, свой checksums.txt), без сети:
#   T5  в zsh без каталога $HOME в $fpath _evo ложится в
#       ${XDG_DATA_HOME:-$HOME/.local/share}/zsh/site-functions, а ~/.zshrc
#       получает один блок, который добавляет этот каталог в $fpath;
#       повторная установка блок не удваивает; --uninstall убирает и _evo,
#       и блок;
#   T6  привязка переживает второй compinit: после установки
#       zsh -i -c 'compinit; compinit; print -r -- ${_comps[evo]}' не пусто.
# Шаг CI — на macOS (zsh там есть всегда). Пока PENDING=1, правки
# install.sh нет, и проверка ждёт, что хотя бы одно условие не выполнено:
# строка снимается правкой «разработки» (Closes by 1.7).
#   T5  EVO_ZSHRC=0 (вид EVO_ACTIVE_HELP=0, #718): _evo ложится в
#       site-functions, ~/.zshrc не тронут, install.sh печатает строку с
#       $fpath, которую человек добавит сам; --uninstall убирает _evo.
set -euo pipefail

PENDING=0

zsh=$(command -v zsh) || { echo "install_zsh_test: нет zsh" >&2; exit 1; }
root=$(git rev-parse --show-toplevel)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

case "$(uname -s)" in Linux) os=linux ;; Darwin) os=darwin ;; *) echo "ОС $(uname -s)" >&2; exit 1 ;; esac
case "$(uname -m)" in x86_64 | amd64) arch=amd64 ;; aarch64 | arm64) arch=arm64 ;; *) echo "arch $(uname -m)" >&2; exit 1 ;; esac

mkdir -p "$work/pkg" "$work/rel/v9.9.9" "$work/home" "$work/home-off" "$work/bin"
(cd "$root" && go build -o "$work/pkg/evo" ./cmd/evo)
cp "$work/pkg/evo" "$work/pkg/cloudru-vm"
archive="evo_9.9.9_${os}_${arch}.tar.gz"
tar -czf "$work/rel/v9.9.9/$archive" -C "$work/pkg" evo cloudru-vm
if command -v sha256sum >/dev/null 2>&1; then sum=$(sha256sum "$work/rel/v9.9.9/$archive" | cut -d' ' -f1); else sum=$(shasum -a 256 "$work/rel/v9.9.9/$archive" | cut -d' ' -f1); fi
echo "$sum  $archive" >"$work/rel/v9.9.9/checksums.txt"

# home и zshrc_env — HOME установки и значение EVO_ZSHRC (пусто — правка
# ~/.zshrc по умолчанию).
home="$work/home"
zshrc_env=
run_install() {
	env -i PATH="$work/bin:/usr/bin:/bin:/usr/sbin:/sbin" HOME="$home" SHELL="$zsh" INSTALL_DIR="$work/bin" EVO_ZSHRC="$zshrc_env" \
		CLOUDRU_VM_VERSION=v9.9.9 CLOUDRU_VM_BASE_URL="file://$work/rel" sh "$root/install.sh" "$@" >"$work/install.log" 2>&1 ||
		{ cat "$work/install.log" >&2; return 1; }
}

failed=()
check() { # описание, команда…
	local what=$1
	shift
	if ! "$@"; then failed+=("$what"); fi
}

site="$work/home/.local/share/zsh/site-functions"
rc="$work/home/.zshrc"
blocks() {
	if [ -f "$rc" ]; then grep -c 'fpath.*zsh/site-functions' "$rc" || true; else echo 0; fi
}

run_install
check "T5: _evo в $site" test -f "$site/_evo"
check "T5: ~/.zshrc добавляет $site в \$fpath одним блоком" test "$(blocks)" -eq 1
# compinit без -u, как в ~/.zshrc у людей: каталог, который install.sh
# создаёт, проходит проверку безопасности compaudit.
# shellcheck disable=SC2016 # ${_comps[evo]} раскрывает zsh, не bash
comps=$(env -i PATH="$work/bin:/usr/bin:/bin" HOME="$work/home" SHELL="$zsh" "$zsh" -i -c 'autoload -Uz compinit; compinit; compinit; print -r -- ${_comps[evo]}' 2>/dev/null </dev/null || true)
check "T6: после второго compinit _comps[evo] не пусто" test -n "$comps"
run_install
check "T5: повторная установка — блок в ~/.zshrc один" test "$(blocks)" -eq 1
# Редактор добавил пробел в конце метки и сохранил файл с CRLF: повторная
# установка находит блок и заменяет его, а не дописывает второй (Qwen, #775).
cr=$(printf '\r')
sed "s/^\(# >>> evo.*\)\$/\1 /; s/^\(# <<< evo <<<\)\$/\1$cr/" "$rc" >"$rc.edited" && cat "$rc.edited" >"$rc" && rm -f "$rc.edited"
run_install
check "T5: метки с пробелом и \\r в конце — блок в ~/.zshrc один" test "$(blocks)" -eq 1
run_install --uninstall
check "T5: --uninstall убирает _evo" test ! -e "$site/_evo"
check "T5: --uninstall убирает блок из ~/.zshrc" test "$(blocks)" -eq 0

# EVO_ZSHRC=0: ~/.zshrc человека остаётся байт в байт.
home="$work/home-off"
zshrc_env=0
site="$home/.local/share/zsh/site-functions"
rc="$home/.zshrc"
printf '# свой ~/.zshrc\nautoload -Uz compinit && compinit\n' >"$rc"
cp "$rc" "$work/zshrc.before"
run_install
check "T5: EVO_ZSHRC=0 — _evo в $site" test -f "$site/_evo"
check "T5: EVO_ZSHRC=0 — ~/.zshrc не тронут" cmp -s "$rc" "$work/zshrc.before"
check "T5: EVO_ZSHRC=0 — install.sh печатает строку, которая добавляет site-functions в \$fpath" grep -q 'fpath.*zsh/site-functions' "$work/install.log"
run_install --uninstall
check "T5: EVO_ZSHRC=0, --uninstall убирает _evo" test ! -e "$site/_evo"
check "T5: EVO_ZSHRC=0, --uninstall не трогает ~/.zshrc без блока" cmp -s "$rc" "$work/zshrc.before"

# Блок без пустой строки перед ним и блок в начале файла: --uninstall
# убирает строки между метками, файл — байт в байт прежний (#775).
# sh_install home [env…] [-- args…] — install.sh в home с zsh по умолчанию.
sh_install() {
	local h=$1
	shift
	local envs=()
	while [ $# -gt 0 ] && [ "$1" != -- ]; do envs+=("$1"); shift; done
	[ "${1:-}" = -- ] && shift
	env -i PATH="${pre_path:-}$work/bin:/usr/bin:/bin:/usr/sbin:/sbin" HOME="$h" SHELL="$zsh" INSTALL_DIR="$work/bin" \
		CLOUDRU_VM_VERSION=v9.9.9 CLOUDRU_VM_BASE_URL="file://$work/rel" ${envs[@]+"${envs[@]}"} sh "$root/install.sh" "$@" >"$work/install.log" 2>&1
}
for start in "a" ""; do
	h="$work/home-edge"
	rm -rf "$h" && mkdir -p "$h"
	if [ -n "$start" ]; then printf '%s\n' "$start" >"$h/.zshrc"; else : >"$h/.zshrc"; fi
	cp "$h/.zshrc" "$work/edge.before"
	sh_install "$h"
	# Пустую строку перед блоком человек убрал.
	awk 'NR > 1 && prev == "" && /^# >>> evo/ { lines[n - 1] = $0; prev = $0; next } { lines[n++] = $0; prev = $0 } END { for (i = 0; i < n; i++) print lines[i] }' "$h/.zshrc" >"$h/.zshrc.x" && cat "$h/.zshrc.x" >"$h/.zshrc" && rm -f "$h/.zshrc.x"
	sh_install "$h" -- --uninstall
	check "T5: блок без пустой строки перед ним (файл «${start:-пустой}») — --uninstall возвращает файл байт в байт" cmp -s "$h/.zshrc" "$work/edge.before"
done

# Конечную метку человек стёр: блок повреждён — ни --uninstall, ни повторная
# установка файл не трогают и называют, что убрать вручную.
h="$work/home-broken"
mkdir -p "$h" && printf 'top\n' >"$h/.zshrc"
sh_install "$h"
grep -v '^# <<< evo <<<' "$h/.zshrc" >"$h/.zshrc.x" && printf 'after\n' >>"$h/.zshrc.x" && cat "$h/.zshrc.x" >"$h/.zshrc" && rm -f "$h/.zshrc.x"
cp "$h/.zshrc" "$work/broken.before"
if sh_install "$h" -- --uninstall; then failed+=("T5: повреждённый блок — --uninstall отказывает"); fi
check "T5: повреждённый блок — --uninstall называет его" grep -q 'повреждён' "$work/install.log"
check "T5: повреждённый блок — --uninstall не трогает ~/.zshrc" cmp -s "$h/.zshrc" "$work/broken.before"
if sh_install "$h"; then failed+=("T5: повреждённый блок — установка отказывает"); fi
check "T5: повреждённый блок — установка не трогает ~/.zshrc" cmp -s "$h/.zshrc" "$work/broken.before"

# Запись ~/.zshrc оборвалась (подменённый cat падает после того, как
# оболочка обрезала файл): копия остаётся, путь к ней назван, код — ошибка.
h="$work/home-failwrite"
mkdir -p "$h" "$work/failbin" && printf 'mine\n' >"$h/.zshrc"
cp "$h/.zshrc" "$work/failwrite.before"
sh_install "$h"
cat >"$work/failbin/cat" <<'FAKE'
#!/bin/sh
case "${1:-}" in *.evo.*) exit 1 ;; esac
exec /bin/cat "$@"
FAKE
chmod +x "$work/failbin/cat"
if pre_path="$work/failbin:" sh_install "$h" -- --uninstall; then failed+=("T5: сбой записи — --uninstall отказывает"); fi
check "T5: сбой записи — путь копии в выводе" grep -q "$h/.zshrc.evo-backup" "$work/install.log"
check "T5: сбой записи — копия хранит файл с блоком" grep -q '^mine$' "$h/.zshrc.evo-backup"

# ZDOTDIR: блок — в $ZDOTDIR/.zshrc, ~/.zshrc не создаётся.
h="$work/home-zdot"
mkdir -p "$h/zd" && printf 'zd\n' >"$h/zd/.zshrc"
sh_install "$h" ZDOTDIR="$h/zd"
check "T5: ZDOTDIR — блок в \$ZDOTDIR/.zshrc" grep -q '^# >>> evo' "$h/zd/.zshrc"
check "T5: ZDOTDIR — ~/.zshrc не создан" test ! -e "$h/.zshrc"
# T6 с ZDOTDIR: zsh читает $ZDOTDIR/.zshrc, блок оттуда даёт привязку
# после второго compinit, как без ZDOTDIR.
# shellcheck disable=SC2016 # ${_comps[evo]} раскрывает zsh, не bash
zcomps=$(env -i PATH="$work/bin:/usr/bin:/bin" HOME="$h" ZDOTDIR="$h/zd" SHELL="$zsh" "$zsh" -i -c 'autoload -Uz compinit; compinit; compinit; print -r -- ${_comps[evo]}' 2>/dev/null </dev/null || true)
check "T6: ZDOTDIR — после второго compinit _comps[evo] не пусто" test -n "$zcomps"
sh_install "$h" ZDOTDIR="$h/zd" -- --uninstall
check "T5: ZDOTDIR — --uninstall убирает блок" test "$(cat "$h/zd/.zshrc")" = zd

if [ "$PENDING" = 1 ]; then
	if [ ${#failed[@]} -eq 0 ]; then
		echo "install_zsh_test: все условия T5–T6 выполнены — снимите PENDING (Closes by 1.7)" >&2
		exit 1
	fi
	echo "install_zsh_test: PENDING (Closes by 1.7), не выполнено: ${failed[*]}"
	exit 0
fi
if [ ${#failed[@]} -gt 0 ]; then
	printf 'install_zsh_test: не выполнено: %s\n' "${failed[@]}" >&2
	exit 1
fi
echo "install_zsh_test: OK"
