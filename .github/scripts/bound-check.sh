#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
# SPDX-FileContributor: Тимур Шакирбеков
# SPDX-License-Identifier: Apache-2.0
#
# Проект ресурса на сервере с --allow-project (REL12-3) — вживую, пока
# ресурс есть. Сервер с --allow-project перед вызовом по id читает ресурс и
# сверяет его проект: поле project_id/projectId ответа или список ресурсов
# проекта. Имена полей взяты из спецификаций; тестовый проект между прогонами
# пуст (cloud-readonly.sh находит пустые списки), поэтому проверка идёт
# внутри живых сценариев, сразу после создания ресурса.
#
# bound_check ИНСТРУМЕНТ АРГУМЕНТЫ_JSON ПОДПИСЬ [own] — два вызова одного *_get:
#   - на «своём» сервере (--allow-project "$PROJECT") — ответ без ошибки;
#   - на «чужом» (проект сервера и --allow-project — проект, которого ключ
#     не видит: клиент сервера одного проекта называет id ресурса другого) —
#     isError, reason NOT_FOUND, в тексте нет проекта.
#   own — только первый: у вида, который сервер сверяет по списку своего
#     проекта (ВМ, Kafka, Redis, DNS) или пускает в любой проект (публичный
#     образ), «чужой» проект 000…0 проверял бы ответ API на список
#     несуществующего проекта, а не evo; этот путь закрывают юнит-тесты
#     (TestProjectBound_ForeignResourceRefused).
#
# Два способа вызова:
#   . bound-check.sh — из сценария (cloud-live.sh): отчёт — его ok/bad/row,
#     серверы останавливает bound_stop из его trap EXIT;
#   bound-check.sh ИНСТРУМЕНТ АРГУМЕНТЫ_JSON ПОДПИСЬ — шаг job'а
#     (cloud-e2e.yml: dbaas-*, k8s-smoke): своя таблица в сводке шага, trap
#     EXIT останавливает серверы; код 1 — проверка не прошла.
#
# Серверы — два evo mcp --http на 127.0.0.1 (порты BOUND_PORT и
# BOUND_PORT+1) со случайным токеном: поднимаются при первом bound_check и
# живут до bound_stop. Лог публичный: печатаются имена инструментов, reason
# и текст ошибки через redact.sed (до 200 символов); ни id ресурса, ни
# проекта.
# Функции вызываются из сценария-хозяина: shellcheck считает их тела
# недостижимыми (SC2317) и функции неиспользуемыми (SC2329).
# shellcheck disable=SC2317,SC2329

bound_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
BOUND_FOREIGN=00000000-0000-4000-8000-000000000000
BOUND_PORT=${BOUND_PORT:-18095}
bound_state=  # пусто — не поднимались, up, down (причина — в $bound_why)
bound_why=
bound_pids=()
bound_tmp=
bound_token=
bound_sess_own=
bound_sess_foreign=

# Отчёт хозяина; без хозяина (шаг job'а) — строка в лог и в таблицу сводки.
declare -F redact >/dev/null || redact() { sed -E -f "$bound_dir/redact.sed"; }
declare -F row >/dev/null || row() {
	local d
	d=$(printf '%s' "$3" | redact)
	printf '%-44s %-6s %s\n' "$1" "$2" "$d"
	printf '| %s | %s | %s |\n' "$1" "$2" "$d" >>"${GITHUB_STEP_SUMMARY:-/dev/null}"
}
declare -F ok >/dev/null || ok() { row "$1" ok "${2:-}"; }
declare -F bad >/dev/null || bad() {
	row "$1" FAIL "${2:-}"
	echo "::error::$1 — $(printf '%s' "${2:-}" | redact)"
	failed=1
}

# bound_post ПОРТ СЕССИЯ ТЕЛО — POST /mcp: JSON ответа (и из потока SSE) в
# stdout, Mcp-Session-Id — в $bound_tmp/session, код HTTP — в $bound_tmp/code.
bound_post() {
	local opts=(-s -D "$bound_tmp/h" -o "$bound_tmp/b" --max-time 300 -w '%{http_code}'
		-H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream'
		-H "Authorization: Bearer $bound_token")
	[ -z "$2" ] || opts+=(-H "Mcp-Session-Id: $2")
	: >"$bound_tmp/h"
	: >"$bound_tmp/b"
	curl "${opts[@]}" -d "$3" "http://127.0.0.1:$1/mcp" >"$bound_tmp/code" 2>/dev/null || echo 000 >"$bound_tmp/code"
	awk 'tolower($1)=="mcp-session-id:"{print $2}' "$bound_tmp/h" | tr -d '\r' >"$bound_tmp/session"
	if grep -q '^data: ' "$bound_tmp/b"; then sed -n 's/^data: //p' "$bound_tmp/b"; else cat "$bound_tmp/b"; fi
}

# bound_up ПОРТ PID ЖУРНАЛ — сервер ответил на /healthz и это он (процесс
# жив: порт мог быть занят другим), затем initialize и
# notifications/initialized; сессия — в $bound_sess. Код 1 — причина в
# $bound_why.
bound_up() {
	local healthy=
	bound_sess=
	for _ in $(seq 1 40); do
		kill -0 "$2" 2>/dev/null || break
		if curl -fs --max-time 2 -o /dev/null "http://127.0.0.1:$1/healthz"; then
			healthy=1
			break
		fi
		sleep 0.5
	done
	if ! kill -0 "$2" 2>/dev/null; then
		bound_why="сервер на порту $1 завершился: $(tail -1 "$3" | tr '\n' ' ' | redact | cut -c1-200)"
		return 1
	fi
	[ -n "$healthy" ] || {
		bound_why="сервер на порту $1 не ответил на /healthz за 20 с"
		return 1
	}
	bound_post "$1" "" '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"bound-check","version":"1"}}}' >/dev/null
	bound_sess=$(cat "$bound_tmp/session")
	[ -n "$bound_sess" ] || {
		bound_why="initialize на порту $1: HTTP $(cat "$bound_tmp/code"), сессии нет"
		return 1
	}
	bound_post "$1" "$bound_sess" '{"jsonrpc":"2.0","method":"notifications/initialized"}' >/dev/null
}

# bound_start — поднять оба сервера (один раз; повторно не пытается).
bound_start() {
	bound_state=down
	if [ -z "${PROJECT:-}" ]; then
		bound_why="нет PROJECT — UUID тестового проекта"
		return 1
	fi
	bound_tmp=$(mktemp -d)
	bound_token=$(openssl rand -hex 32)
	echo "::add-mask::$bound_token"
	CLOUDRU_PROJECT_ID=$PROJECT MCP_BEARER_TOKEN=$bound_token \
		evo mcp --http "127.0.0.1:$BOUND_PORT" --allow-project "$PROJECT" 2>"$bound_tmp/own.log" &
	bound_pids+=("$!")
	CLOUDRU_PROJECT_ID=$BOUND_FOREIGN MCP_BEARER_TOKEN=$bound_token \
		evo mcp --http "127.0.0.1:$((BOUND_PORT + 1))" --allow-project "$BOUND_FOREIGN" 2>"$bound_tmp/foreign.log" &
	bound_pids+=("$!")
	bound_up "$BOUND_PORT" "${bound_pids[0]}" "$bound_tmp/own.log" || return 1
	bound_sess_own=$bound_sess
	bound_up "$((BOUND_PORT + 1))" "${bound_pids[1]}" "$bound_tmp/foreign.log" || return 1
	bound_sess_foreign=$bound_sess
	bound_state=up
}

# bound_stop — остановить серверы (TERM, через 5 с — KILL) и убрать файлы.
bound_stop() {
	local p alive
	for p in "${bound_pids[@]}"; do kill "$p" 2>/dev/null || true; done
	for _ in $(seq 1 50); do
		alive=
		for p in "${bound_pids[@]}"; do kill -0 "$p" 2>/dev/null && alive=1; done
		[ -n "$alive" ] || break
		sleep 0.1
	done
	for p in "${bound_pids[@]}"; do
		kill -KILL "$p" 2>/dev/null || true
		wait "$p" 2>/dev/null || true
	done
	bound_pids=()
	[ -z "$bound_tmp" ] || rm -rf "$bound_tmp"
	bound_tmp=
	bound_state=
}

# bound_call ПОРТ СЕССИЯ ИНСТРУМЕНТ АРГУМЕНТЫ — ответ инструмента: isError в
# $bound_err ("?" — ответа нет), текст в $bound_text, reason в $bound_reason,
# сообщение (замаскированное, до 200 символов) в $bound_msg.
bound_call() {
	local body
	body=$(jq -cn --arg n "$3" --argjson a "$4" '{jsonrpc:"2.0", id:9, method:"tools/call", params:{name:$n, arguments:$a}}')
	bound_post "$1" "$2" "$body" | jq -c 'select(.id == 9)' >"$bound_tmp/r" 2>/dev/null || : >"$bound_tmp/r"
	# не «.result.isError // "?"»: в jq // считает false пустым
	bound_err=$(jq -r 'if .result == null then "?" else (.result.isError == true | tostring) end' "$bound_tmp/r" 2>/dev/null || true)
	[ -n "$bound_err" ] || bound_err="?"
	bound_text=$(jq -r '.result.content[0].text // empty' "$bound_tmp/r" 2>/dev/null || true)
	bound_reason=$(jq -r '.reason? // empty' <<<"$bound_text" 2>/dev/null || true)
	[ -n "$bound_reason" ] || bound_reason=-
	# сначала маска, потом обрезка: обрезанный UUID redact.sed не узнает
	bound_msg=$( (jq -r 'objects | [.error, .hint] | map(select(. != null) | tostring) | join(" ")' <<<"$bound_text" 2>/dev/null ||
		true; jq -r '.error.message? // empty' "$bound_tmp/r" 2>/dev/null || true) | tr '\n' ' ' | redact | cut -c1-200)
	[ -n "$bound_msg" ] || bound_msg="HTTP $(cat "$bound_tmp/code"), ответа инструмента нет"
}

# bound_check ИНСТРУМЕНТ АРГУМЕНТЫ_JSON ПОДПИСЬ [own] — см. шапку.
bound_check() {
	local tool=$1 args=$2 label=$3 only=${4:-}
	[ -n "$bound_state" ] || bound_start || true
	if [ "$bound_state" != up ]; then
		bad "$label" "серверы --allow-project не поднялись: $bound_why"
		return 0
	fi
	if ! jq -e 'type == "object"' >/dev/null 2>&1 <<<"$args"; then
		bad "$label" "аргументы $tool — не объект JSON"
		return 0
	fi
	bound_call "$BOUND_PORT" "$bound_sess_own" "$tool" "$args"
	if [ "$bound_err" = false ]; then
		ok "$label: свой проект" "проект ресурса разрешён"
	else
		bad "$label: свой проект" "isError $bound_err, reason $bound_reason: $bound_msg"
	fi
	if [ "$only" = own ]; then
		row "$label: чужой проект" skip "вид по списку проекта или публичный: юнит-тест"
		return 0
	fi
	bound_call "$((BOUND_PORT + 1))" "$bound_sess_foreign" "$tool" "$args"
	if [ "$bound_err" != true ] || [ "$bound_reason" != NOT_FOUND ]; then
		bad "$label: чужой проект" "ждали NOT_FOUND; isError $bound_err, reason $bound_reason: $bound_msg"
	elif grep -qiF -e "$PROJECT" -e "$BOUND_FOREIGN" <<<"$bound_text"; then
		bad "$label: чужой проект" "NOT_FOUND, но ответ называет проект"
	else
		ok "$label: чужой проект" "NOT_FOUND, проект не назван"
	fi
}

# Шаг job'а: bound-check.sh ИНСТРУМЕНТ АРГУМЕНТЫ_JSON ПОДПИСЬ.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	set -euo pipefail
	[ "$#" = 3 ] || [ "$#" = 4 ] || {
		echo "вызов: bound-check.sh ИНСТРУМЕНТ АРГУМЕНТЫ_JSON ПОДПИСЬ [own]" >&2
		exit 2
	}
	failed=0
	trap bound_stop EXIT
	{
		echo
		echo "### Проект ресурса на сервере с --allow-project"
		echo
		echo "| Проверка | Итог | Подробности |"
		echo "|---|---|---|"
	} >>"${GITHUB_STEP_SUMMARY:-/dev/null}"
	bound_check "$@"
	exit "$failed"
fi
