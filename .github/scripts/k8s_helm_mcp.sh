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
#   k8s_helm_mcp.sh CLUSTER_ID CHART_DIR [configmap|pods]
#
# configmap (по умолчанию) — чарт из одного ConfigMap evo-e2e-helm (выход
# в интернет кластеру не нужен): готовность — по релизу и ConfigMap.
# pods — чарт с подом из образа (app=evo-e2e-pod; пункт А плана cloud-e2e
# rc 1.7, «да» владельца 04.11): после deployed поды Ready, после
# uninstall подов нет — образ тянется через SNAT зоны. Каждый вызов —
# сеанс evo mcp по stdio (mcp_stdio.sh); дольше MCP_LIMIT секунд — ошибка:
# предел ~45 с и запас на сеанс. Лог публичный: вывод — через redact.sed,
# id кластера не печатается.
set -uo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source-path=SCRIPTDIR source=mcp_stdio.sh
. "$here/mcp_stdio.sh"
redact() { sed -E -f "$here/redact.sed"; }
cluster=${1:?id кластера}
chart=${2:?каталог чарта}
mode=${3:-configmap}
case "$mode" in configmap | pods) ;; *)
	echo "::error::режим $mode: configmap или pods"
	exit 2
	;;
esac
# Скрипт id не печатает; маска — на случай текста ошибки, который его назовёт.
echo "::add-mask::$cluster"
MCP_LIMIT=${MCP_LIMIT:-75}
STATUS_SLEEP=${STATUS_SLEEP:-10}
POD_TRIES=${POD_TRIES:-18}
MCP_WORK=$(mktemp -d)
MCP_TOOLSETS=k8s
MCP_SESSION_LIMIT=$((MCP_LIMIT + 30))
export MCP_WORK MCP_TOOLSETS MCP_SESSION_LIMIT
trap 'rm -rf "$MCP_WORK"' EXIT
fail=0
err() { echo "::error::k8s_helm по MCP: $*"; fail=1; }
j() { jq "$@" "$MCP_WORK/r.json" 2>/dev/null; }
said() { printf '%s: isError %s, %s с, %s\n' "$1" "$MCP_ISERR" "$MCP_TOOK" "$(j -c '{status, reason, next_tool}' | redact)"; }
bounded() { [ "$MCP_TOOK" -le "$MCP_LIMIT" ] || err "$1: сеанс $MCP_TOOK с — дольше $MCP_LIMIT с"; }
kubectl_json() { # АРГУМЕНТЫ kubectl — его JSON (stdout), по MCP
	mcp_call k8s_kubectl "$(jq -cn --arg c "$cluster" --arg a "$1" '{cluster_id:$c, args:$a}')"
	j -r '.output // empty' | sed -n '/^{/,$p'
}
cm() { # значение data.k ConfigMap evo-e2e-helm в default
	kubectl_json "get configmap evo-e2e-helm -n default -o json" | jq -r '.data.k // empty' 2>/dev/null
}
pods() { # «всего готовых» подов релиза с образом
	kubectl_json "get pods -l app=evo-e2e-pod -n default -o json" |
		jq -r '[.items[]?] | "\(length) \([.[] | select(any(.status.conditions[]?; .type == "Ready" and .status == "True"))] | length)"' 2>/dev/null
}

release=evo-e2e-mcp
[ "$mode" = configmap ] || release=evo-e2e-pod
base=$(jq -cn --arg c "$cluster" --arg ch "$chart" --arg r "$release" '{cluster_id:$c, release:$r, chart:$ch, namespace:"default"}')
mcp_call k8s_helm "$(jq -c '. + {action:"install", confirmed:true}' <<<"$base")"
said "install"
bounded install
case "$MCP_ISERR/$(j -r .status)" in
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
	err "install: isError $MCP_ISERR, status $(j -r '.status // "-"'), $(j -r '.error // .output // empty' | redact | tail -c 300 | tr '\n' ' ')"
	next=
	;;
esac

if [ -n "$next" ]; then
	[ "$(jq -r .action <<<"$next")" = status ] || err "install: next_args.action $(jq -r .action <<<"$next"), ожидался status"
	deployed=
	for _ in 1 2 3 4 5; do
		mcp_call k8s_helm "$next"
		said "status"
		bounded status
		if [ "$MCP_ISERR" = false ] && j -r '.output // empty' | grep -Eq 'STATUS: deployed'; then
			deployed=1
			break
		fi
		sleep "$STATUS_SLEEP"
	done
	[ -n "$deployed" ] || err "status: релиз не deployed за 5 вызовов"
	if [ "$mode" = configmap ]; then
		v=$(cm)
		said "k8s_kubectl get configmap"
		bounded k8s_kubectl
		[ "$v" = helm ] || err "k8s_kubectl: ConfigMap релиза — «${v:-нет}», ожидался «helm»"
	else
		# deployed — манифесты приняты, не поды готовы (next инструмента):
		# готовность читается отдельно, каждым вызовом в пределе.
		ready=
		for ((i = 1; i <= POD_TRIES; i++)); do
			read -r n r <<<"$(pods)"
			bounded k8s_kubectl
			echo "k8s_kubectl get pods: подов ${n:-?}, готовых ${r:-?} (вызов $i)"
			if [ "${n:-0}" -gt 0 ] && [ "$n" = "$r" ]; then
				ready=1
				break
			fi
			sleep "$STATUS_SLEEP"
		done
		[ -n "$ready" ] || err "поды релиза не готовы за $POD_TRIES вызовов (образ не скачан? SNAT зоны, подсеть узлов)"
	fi
fi

mcp_call k8s_helm "$(jq -c '{cluster_id, release, namespace} + {action:"uninstall", confirmed:true}' <<<"$base")"
said "uninstall"
bounded uninstall
[ "$MCP_ISERR" = false ] || err "uninstall: isError $MCP_ISERR, $(j -r '.error // empty' | redact | cut -c1-200)"
if [ "$mode" = configmap ]; then
	[ -z "$(cm)" ] || err "uninstall: ConfigMap релиза остался"
else
	gone=
	for _ in 1 2 3 4 5 6; do
		read -r n _ <<<"$(pods)"
		[ "${n:-1}" = 0 ] && gone=1 && break
		sleep "$STATUS_SLEEP"
	done
	[ -n "$gone" ] || err "uninstall: поды релиза остались"
fi
[ "$fail" = 0 ] && echo "k8s_helm по MCP ($mode): install → deploying → status deployed → $([ "$mode" = pods ] && echo "поды Ready" || echo ConfigMap) → uninstall"
exit "$fail"
