#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
# SPDX-FileContributor: Тимур Шакирбеков
# SPDX-License-Identifier: Apache-2.0
#
# Клиент evo mcp по stdio для живых сценариев cloud-e2e (k8s_helm_mcp.sh,
# area_mcp в cloud-live.sh): каждый вызов — свой сеанс (initialize +
# tools/call), как у клиента после перезапуска.
#
#   . mcp_stdio.sh
#   mcp_call ИМЯ АРГУМЕНТЫ_JSON   — ответ инструмента (текст) в
#       $MCP_WORK/r.json, isError в $MCP_ISERR («нет ответа», «тайм-аут
#       сеанса»), секунды сеанса в $MCP_TOOK;
#   mcp_follow ИМЯ АРГУМЕНТЫ_JSON [ВЫЗОВОВ] — mcp_call и повторы по
#       следующему шагу (D69, H14): пока ответ — deploying или creating с
#       next_tool этого же инструмента, вызывается next_tool с next_args;
#       $MCP_CALLS — сколько вызовов, $MCP_MAXTOOK — самый долгий сеанс,
#       $MCP_FOLLOW — «done» (ответ не в работе), «tool» (следующий шаг —
#       другой инструмент или без next_args), «max» (ВЫЗОВОВ кончились).
#
# Настройки: MCP_WORK (каталог, обязателен), MCP_TOOLSETS (--toolsets;
# пусто — без флага), MCP_SESSION_LIMIT (секунды timeout на сеанс),
# MCP_FOLLOW_SLEEP (пауза между повторами, секунды).
# Функции вызываются из сценария-хозяина: shellcheck считает их
# неиспользуемыми (SC2329).
# MCP_FOLLOW и другие итоги читает сценарий-хозяин (SC2034).
# shellcheck disable=SC2329,SC2034

MCP_ISERR='' MCP_TOOK=0 MCP_CALLS=0 MCP_MAXTOOK=0 MCP_FOLLOW=''
mcp_id=10

mcp_call() {
	local t0=$SECONDS rc ts=()
	[ -z "${MCP_TOOLSETS:-}" ] || ts=(--toolsets "$MCP_TOOLSETS")
	mcp_id=$((mcp_id + 1))
	printf '%s\n' \
		'{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"cloud-e2e","version":"1"}}}' \
		'{"jsonrpc":"2.0","method":"notifications/initialized"}' \
		"$(jq -cn --arg n "$1" --argjson a "$2" --argjson i "$mcp_id" '{jsonrpc:"2.0", id:$i, method:"tools/call", params:{name:$n, arguments:$a}}')" |
		timeout "${MCP_SESSION_LIMIT:-105}" evo mcp "${ts[@]}" >"${MCP_WORK:?}/out" 2>/dev/null
	rc=$?
	MCP_TOOK=$((SECONDS - t0))
	jq -c --argjson i "$mcp_id" 'select(.id == $i) | .result' "$MCP_WORK/out" >"$MCP_WORK/res" 2>/dev/null
	MCP_ISERR=$(jq -r '.isError // false' "$MCP_WORK/res" 2>/dev/null)
	[ -n "$MCP_ISERR" ] || MCP_ISERR="нет ответа"
	# 124 — сеанс оборвал timeout: не ответ сервера, а его отсутствие.
	[ "$rc" != 124 ] || MCP_ISERR="тайм-аут сеанса"
	jq -r '.content[0].text // empty' "$MCP_WORK/res" >"$MCP_WORK/r.json" 2>/dev/null || : >"$MCP_WORK/r.json"
}

mcp_follow() {
	local name=$1 args=$2 max=${3:-40} st nt
	MCP_CALLS=0 MCP_MAXTOOK=0 MCP_FOLLOW=''
	while :; do
		mcp_call "$name" "$args"
		MCP_CALLS=$((MCP_CALLS + 1))
		[ "$MCP_TOOK" -le "$MCP_MAXTOOK" ] || MCP_MAXTOOK=$MCP_TOOK
		st=$(jq -r '.status // empty' "$MCP_WORK/r.json" 2>/dev/null)
		case "$MCP_ISERR/$st" in
		false/deploying | false/creating) ;;
		*)
			MCP_FOLLOW="done"
			return 0
			;;
		esac
		nt=$(jq -r '.next_tool // empty' "$MCP_WORK/r.json" 2>/dev/null)
		args=$(jq -c '.next_args // empty' "$MCP_WORK/r.json" 2>/dev/null)
		if [ "$nt" != "$name" ] || [ -z "$args" ]; then
			MCP_FOLLOW=tool
			return 0
		fi
		if [ "$MCP_CALLS" -ge "$max" ]; then
			MCP_FOLLOW=max
			return 0
		fi
		sleep "${MCP_FOLLOW_SLEEP:-5}"
	done
}
