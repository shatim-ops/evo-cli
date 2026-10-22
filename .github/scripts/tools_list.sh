#!/usr/bin/env bash
# tools/list сервера evo mcp по stdio — массив инструментов, как его видит
# агент. Вызов: tools_list.sh <бинарь evo>. Ни конфигурации, ни ключей не
# нужно: initialize и tools/list в облако не ходят.
set -euo pipefail
home=$(mktemp -d)
trap 'rm -rf "$home"' EXIT
printf '%s\n' \
	'{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}' \
	'{"jsonrpc":"2.0","method":"notifications/initialized"}' \
	'{"jsonrpc":"2.0","id":2,"method":"tools/list"}' |
	HOME=$home USERPROFILE=$home "$1" mcp | jq -ce 'select(.id == 2) | .result.tools'
