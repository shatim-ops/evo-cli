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
# строка снимается правкой «разработки» (Closes by 1.7). Переменная,
# которой человек отключает правку ~/.zshrc, — её выбор; проверка
# добавится с ней.
set -euo pipefail

PENDING=1

zsh=$(command -v zsh) || { echo "install_zsh_test: нет zsh" >&2; exit 1; }
root=$(git rev-parse --show-toplevel)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

case "$(uname -s)" in Linux) os=linux ;; Darwin) os=darwin ;; *) echo "ОС $(uname -s)" >&2; exit 1 ;; esac
case "$(uname -m)" in x86_64 | amd64) arch=amd64 ;; aarch64 | arm64) arch=arm64 ;; *) echo "arch $(uname -m)" >&2; exit 1 ;; esac

mkdir -p "$work/pkg" "$work/rel/v9.9.9" "$work/home" "$work/bin"
(cd "$root" && go build -o "$work/pkg/evo" ./cmd/evo)
cp "$work/pkg/evo" "$work/pkg/cloudru-vm"
archive="evo_9.9.9_${os}_${arch}.tar.gz"
tar -czf "$work/rel/v9.9.9/$archive" -C "$work/pkg" evo cloudru-vm
if command -v sha256sum >/dev/null 2>&1; then sum=$(sha256sum "$work/rel/v9.9.9/$archive" | cut -d' ' -f1); else sum=$(shasum -a 256 "$work/rel/v9.9.9/$archive" | cut -d' ' -f1); fi
echo "$sum  $archive" >"$work/rel/v9.9.9/checksums.txt"

run_install() {
	env -i PATH="$work/bin:/usr/bin:/bin:/usr/sbin:/sbin" HOME="$work/home" SHELL="$zsh" INSTALL_DIR="$work/bin" \
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
run_install --uninstall
check "T5: --uninstall убирает _evo" test ! -e "$site/_evo"
check "T5: --uninstall убирает блок из ~/.zshrc" test "$(blocks)" -eq 0

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
