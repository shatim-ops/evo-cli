#!/usr/bin/env bash
# Ожидание готовности кластера DBaaS в job cloud-e2e: dbaas-ready.sh ВИД СЕК
# (ВИД — pg, kafka, redis; кластер — $CLUSTER_ID, имя для лога — $CLUSTER).
# READY или RUNNING — 0; FAILED, ERROR, DELETING или срок — 1. Перед выходом
# по FAILED печатает причину: поля кластера с именами status, state, phase,
# reason, message, code, done, error*, fail* и операцию создания по $OP_ID
# (run 203: кластер PostgreSQL ушёл в FAILED через секунды после создания,
# причины в логе не было). Лог публичный: всё — через redact.sed, полный
# ответ не печатается. DBAAS_READY_PAUSE — пауза между чтениями (60 с).
set -euo pipefail
kind=${1:?вид: pg, kafka, redis}
limit=${2:?срок в секундах}
case "$kind" in pg | kafka | redis) ;; *) echo "::error::dbaas-ready: неизвестный вид «$kind»"; exit 2 ;; esac
: "${CLUSTER_ID:?CLUSTER_ID не задан}" "${CLUSTER:?CLUSTER не задан}"
here=$(cd "$(dirname "$0")" && pwd)
redact() { sed -E -f "$here/redact.sed"; }
# Only the fields that can name a cause, never the whole answer: a field is
# taken by its own name (the last path element), and not printed at all when
# any element of its path looks like a credential or an address. Credentials
# inside the text (URI userinfo, Bearer, key=value and key: value, quoted or
# not) are masked here; IDs and IPs — by
# redact.sed; the text is cut to 160 characters after that, so a cut never
# splits what a mask would have caught.
# shellcheck disable=SC2016 # jq programs: $p is jq's variable
cause='[paths(scalars) as $p
  | select(($p | last | tostring) | test("^(status|state|phase|reason|message|code|done)$|^(error|fail)|_(reason|message)$"; "i"))
  | select(any($p[] | tostring; test("pass|secret|token|key|cred|uri|url|dsn|conn|host|endpoint|kubeconfig|cert|address"; "i")) | not)
  | {key: ($p | map(tostring) | join(".")),
     value: (getpath($p) | if type == "string" then gsub("://[^/@ ]*@"; "://<cred>@") | gsub("bearer\\s+[^ \",]+"; "Bearer <secret>"; "i") | gsub("(?<k>pass\\w*|pwd|secret|token|key|bearer|authorization)\\\\?\"?\\s*[=:]\\s*\\\\?\"?[^ \",\\\\]*"; "\(.k)=<secret>"; "i") else . end)}]
  | from_entries'
cut160='map_values(if type == "string" then .[0:160] else . end)'
brief() { jq -c "$cause" | redact | jq -c "$cut160"; }

why() {
	local out
	if out=$(evo tools call "${kind}_get" --arg cluster_id="$CLUSTER_ID" --json 2>/dev/null) && jq -e 'type == "object"' <<<"$out" >/dev/null 2>&1; then
		echo "поля кластера: $(jq -c 'keys' <<<"$out")"
		echo "кластер: $(brief <<<"$out")"
	else
		echo "кластер: не прочитан"
	fi
	if [ -z "${OP_ID:-}" ]; then
		echo "операция создания: id операции нет — не читаю"
	elif out=$(evo tools call "${kind}_operation" --arg operation_id="$OP_ID" --json 2>/dev/null) && jq -e 'type == "object"' <<<"$out" >/dev/null 2>&1; then
		echo "операция создания: $(brief <<<"$out")"
	else
		echo "операция создания: не прочитана"
	fi
}

start=$(date +%s)
while :; do
	st=$(evo tools call "${kind}_get" --arg cluster_id="$CLUSTER_ID" --json 2>/dev/null |
		jq -r '(.status // .state // "?") | tostring | ascii_upcase') || st="?"
	echo "$((($(date +%s) - start) / 60)) мин: $st"
	# Exact statuses: the substring *READY* would match NOT_READY too.
	case "$st" in
	READY | RUNNING) exit 0 ;;
	*FAIL* | *ERROR* | *DELET*)
		why || true
		echo "::error::кластер $CLUSTER: $st"
		exit 1
		;;
	esac
	[ $(($(date +%s) - start)) -lt "$limit" ] || { echo "::error::кластер $CLUSTER не готов за $((limit / 60)) мин (PostgreSQL — реестр DB-20)"; exit 1; }
	sleep "${DBAAS_READY_PAUSE:-60}"
done
