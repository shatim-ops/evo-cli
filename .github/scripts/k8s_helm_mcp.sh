#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
# SPDX-FileContributor: Тимур Шакирбеков
# SPDX-License-Identifier: Apache-2.0
#
# cloud-e2e, job k8s-smoke: k8s_helm и k8s_kubectl по MCP с пределом вызова
# (D69, строка H14 docs/mcp.md; строка плана 1.7 «cloud-e2e k8s-smoke на
# rc.1»). По MCP install не ждёт подов: ответ status deploying и следующий
# шаг (next_tool, next_args), затем action=status говорит, что релиз
# deployed. Шаг CLI (evo tools call) предела не держит и этого не видит.
#
#   k8s_helm_mcp.sh CLUSTER_ID CHART_DIR
#
# Чарт — локальный, из одного ConfigMap evo-e2e-helm (выход в интернет
# кластеру не нужен): подов у него нет, готовность читается по релизу и
# ConfigMap. Каждый вызов — отдельный сеанс evo mcp по stdio (initialize +
# tools/call), как у клиента после перезапуска; дольше MCP_LIMIT секунд —
# ошибка: предел ~45 с и запас на сеанс. Лог публичный: вывод — через
# redact.sed, id кластера не печатается.
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
redact() { sed -E -f "$here/redact.sed"; }
cluster=${1:?id кластера}
chart=${2:?каталог чарта}
MCP_LIMIT=${MCP_LIMIT:-75}
STATUS_SLEEP=${STATUS_SLEEP:-10}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0 id=10 iserr= took=
err() { echo "::error::k8s_helm по MCP: $*"; fail=1; }

# call ИМЯ АРГУМЕНТЫ_JSON — ответ инструмента (текст) в $work/r.json,
# isError в $iserr, секунды сеанса в $took.
call() {
	local t0=$SECONDS
	id=$((id + 1))
	printf '%s\n' \
		'{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"cloud-e2e","version":"1"}}}' \
		'{"jsonrpc":"2.0","method":"notifications/initialized"}' \
		"$(jq -cn --arg n "$1" --argjson a "$2" --argjson i "$id" '{jsonrpc:"2.0", id:$i, method:"tools/call", params:{name:$n, arguments:$a}}')" |
		timeout $((MCP_LIMIT + 30)) evo mcp --toolsets k8s >"$work/out" 2>/dev/null
	took=$((SECONDS - t0))
	jq -c --argjson i "$id" 'select(.id == $i) | .result' "$work/out" >"$work/res" 2>/dev/null
	iserr=$(jq -r '.isError // false' "$work/res" 2>/dev/null)
	[ -n "$iserr" ] || iserr="нет ответа"
	jq -r '.content[0].text // empty' "$work/res" >"$work/r.json" 2>/dev/null || : >"$work/r.json"
}
j() { jq "$@" "$work/r.json" 2>/dev/null; }
said() { printf '%s: isError %s, %s с, %s\n' "$1" "$iserr" "$took" "$(j -c '{status, reason, next_tool}' | redact)"; }
bounded() { [ "$took" -le "$MCP_LIMIT" ] || err "$1: сеанс $took с — дольше $MCP_LIMIT с"; }
cm() { # значение data.k ConfigMap evo-e2e-helm в default, по MCP
	call k8s_kubectl "$(jq -cn --arg c "$cluster" '{cluster_id:$c, args:"get configmap evo-e2e-helm -n default -o json"}')"
	j -r '.output // empty' | sed -n '/^{/,$p' | jq -r '.data.k // empty' 2>/dev/null
}

base=$(jq -cn --arg c "$cluster" --arg ch "$chart" '{cluster_id:$c, release:"evo-e2e-mcp", chart:$ch, namespace:"default"}')
call k8s_helm "$(jq -c '. + {action:"install", confirmed:true}' <<<"$base")"
said "install"
bounded install
case "$iserr/$(j -r .status)" in
false/deploying)
	[ "$(j -r '.next_tool // empty')" = k8s_helm ] || err "install: next_tool $(j -r '.next_tool // "нет"'), ожидался k8s_helm"
	next=$(j -c '.next_args // empty')
	;;
false/ok)
	# Helm уложился в предел и ответил сам: предел не сработал, но честно.
	echo "::warning::k8s_helm по MCP: install ответил ok в пределе — deploying не проверен"
	next=$(jq -c '{cluster_id, release, namespace} + {action:"status"}' <<<"$base")
	;;
*)
	err "install: isError $iserr, status $(j -r '.status // "-"'), $(j -r '.error // .output // empty' | redact | tail -c 300 | tr '\n' ' ')"
	next=
	;;
esac

if [ -n "$next" ]; then
	[ "$(jq -r .action <<<"$next")" = status ] || err "install: next_args.action $(jq -r .action <<<"$next"), ожидался status"
	deployed=
	for _ in 1 2 3 4 5; do
		call k8s_helm "$next"
		said "status"
		bounded status
		if [ "$iserr" = false ] && j -r '.output // empty' | grep -Eq 'STATUS: deployed'; then
			deployed=1
			break
		fi
		sleep "$STATUS_SLEEP"
	done
	[ -n "$deployed" ] || err "status: релиз не deployed за 5 вызовов"
	v=$(cm)
	said "k8s_kubectl get configmap"
	bounded k8s_kubectl
	[ "$v" = helm ] || err "k8s_kubectl: ConfigMap релиза — «${v:-нет}», ожидался «helm»"
fi

call k8s_helm "$(jq -c '{cluster_id, release, namespace} + {action:"uninstall", confirmed:true}' <<<"$base")"
said "uninstall"
bounded uninstall
[ "$iserr" = false ] || err "uninstall: isError $iserr, $(j -r '.error // empty' | redact | cut -c1-200)"
[ -z "$(cm)" ] || err "uninstall: ConfigMap релиза остался"
[ "$fail" = 0 ] && echo "k8s_helm по MCP: install → deploying → status deployed → ConfigMap → uninstall"
exit "$fail"
