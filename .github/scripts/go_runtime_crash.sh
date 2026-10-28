#!/usr/bin/env bash
# Сбой рантайма Go в выводе go test (process-handoff.md, «CI не от diff»;
# решение владельца #370): печатает первую строку сигнатуры и пакеты,
# которые упали, код 0; не сбой рантайма — пусто, код 1. Сигнатуры —
# закрытый список случаев CI 22–28.10 (14 из 828 попыток Windows, все в
# internal/tools): «found pointer to free object», «unexpected signal during
# runtime execution», «checkmark found unmarked object», «fatal error:
# fault», «Exception 0xc0000005». Стек через internal/config
# (private_windows.go — единственный unsafe evo) — не рантайм, а дефект
# продукта: код 1.
#
#   go_runtime_crash.sh <лог go test>
set -uo pipefail

log=${1:?usage: go_runtime_crash.sh <go test log>}
sig=$(grep -m1 -E '^fatal error: (found pointer to free object|unexpected signal during runtime execution|checkmark found unmarked object|fault)|^Exception 0xc0000005' "$log") || exit 1
if grep -q -E 'internal/config\.|private_windows\.go' "$log"; then
	exit 1
fi
pkgs=$(grep -E '^FAIL[[:space:]]+[^[:space:]]+[[:space:]]' "$log" | awk '{print $2}' | sort -u | tr '\n' ' ')
echo "${sig%$'\r'} | ${pkgs% }"
