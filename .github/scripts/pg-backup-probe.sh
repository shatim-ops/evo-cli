#!/usr/bin/env bash
# Проба реестра DB-22 (G13-27): что отвечает список резервных копий кластера
# PostgreSQL после его удаления. Документация Cloud.ru: при удалении кластера
# удаляются «а также резервные копии кластера»
# (https://cloud.ru/docs/paas-postgresql/ug/topics/guides__deleting-cluster).
# Отдельный GET по каждой сохранённой паре (ID, имя) после удаления родителя.
# Общий ответ 404 или отсутствие родителя не доказывают удаление копии.
# Имена и исходные ID остаются на раннере. Публичная строка связывает номер
# копии с сохранённым отпечатком без раскрытия её идентификатора.
set -euo pipefail

: "${CLUSTER_ID:?нужен CLUSTER_ID}"
# ID и имя копии скрипт не печатает; маска — второй рубеж, если их унесёт
# чужой текст (ошибка API идёт ещё и через redact.sed).
echo "::add-mask::$CLUSTER_ID"
redact() { sed -E -f "$(dirname "$0")/redact.sed"; }
api=https://postgresql.api.cloud.ru
out="${RUNNER_TEMP:-/tmp}/pg-backup-probe.json"
err="${RUNNER_TEMP:-/tmp}/pg-backup-probe.err"
trap 'rm -f "$out" "$err"' EXIT
receipts="$(dirname "$0")/pg-backup-receipts.py"
targets="$RUNNER_TEMP/pg-backup-targets"
umask 077
python3 "$receipts" paths > "$targets"
unknown() {
	while read -r n _; do
		echo "DB-22: копия №$n, исход неизвестен ($1)"
	done < "$targets"
}
if ! evo tools call pg_list --json >"$out" 2>/dev/null || ! jq -e --arg id "$CLUSTER_ID" '
	(.clusters | type) == "array" and all(.clusters[]; (.id | type) == "string") and
	all(.clusters[] | select(.id == $id); .status == "ERASED" or .status == "DELETED")' "$out" >/dev/null; then
	unknown "удаление родительского кластера не подтверждено"
	exit 0
fi

# 1. evo pg_backups по ID удалённого кластера. Ошибка инструмента — в
# stderr ({"error":…}), stdout тогда пуст (QA-12): на rc.2 проба выбрасывала
# stderr и печатала пустой reason (run 40093421741).
set +e
evo tools call pg_backups --arg cluster_id="$CLUSTER_ID" --json >"$out" 2>"$err"
evo_rc=$?
set -e
evo_reason=$(jq -r '.reason // empty' "$out" 2>/dev/null) || evo_reason=
[ -n "$evo_reason" ] || evo_reason=$(jq -rR 'fromjson? | .error.reason? // empty' "$err" 2>/dev/null | sed -n 1p) || evo_reason=
evo_n=$(jq -r 'if (.backups | type) == "array" then (.backups | length) else "-" end' "$out" 2>/dev/null || echo '?')
echo "evo pg_backups: код $evo_rc, reason ${evo_reason:--}, копий ${evo_n:--}, поля $(jq -c 'keys' "$out" 2>/dev/null || echo '-')"
[ "$evo_rc" = 0 ] || echo "evo pg_backups: $(jq -rR 'fromjson? | .error.message? // empty' "$err" 2>/dev/null | sed -n 1p | redact | cut -c1-200)"

# 2. Те же копии прямым GET API: список и копия по имени.
if [ -z "${KEY_ID:-}" ] || [ -z "${SECRET:-}" ]; then
	unknown "нет ключа для адресного чтения"
	exit 0
fi
# Тело запроса токена — через stdin, не в argv.
token=$(jq -n --arg k "$KEY_ID" --arg s "$SECRET" '{keyId:$k, secret:$s}' |
	curl --max-time 30 -sS -X POST https://iam.api.cloud.ru/api/v1/auth/token -H 'Content-Type: application/json' --data-binary @- 2>/dev/null |
	jq -r '.access_token // .accessToken // empty') || token=
[ -n "$token" ] && echo "::add-mask::$token"
get() { # ПУТЬ → тело в $out, код HTTP на stdout; 000 — запрос не дошёл
	# stderr curl — в никуда: его текст об ошибке может нести URL с ID.
	curl --max-time 30 -sS -o "$out" -w '%{http_code}' -H "Authorization: Bearer $token" "$api$1" 2>/dev/null || true
}
show() { # КОД — что ответил API: поля и текст ошибки; тело не JSON — его
	# начало (на rc.2 список ответил 400 не JSON, и проба не показала ничего)
	local keys msg
	if jq -e . "$out" >/dev/null 2>&1; then
		keys=$(jq -c 'if type == "object" then keys else type end' "$out")
		msg=$(jq -r '.message // .error // empty | tostring' "$out" | redact | cut -c1-160)
	else
		keys="не JSON"
		# маска до обрезки: обрезанный ID redact.sed уже не узнаёт
		msg=$(head -c 4000 "$out" | tr '\n\r' '  ' | redact | cut -c1-160)
	fi
	echo "http=$1, поля $keys${msg:+, «$msg»}"
}
if [ -z "$token" ]; then
	unknown "нет токена для адресного чтения"
	exit 0
fi
cluster_code=$(get "/v1/clusters/$CLUSTER_ID")
if { [ "$cluster_code" = 404 ] && jq -e '
	(.message | strings | ascii_downcase) as $msg |
	(.code == 5 or .code == "5" or .code == "NOT_FOUND") and
	(["cluster not found", "cluster does not exist", "postgresql cluster not found"] | index($msg) != null)
	' "$out" >/dev/null 2>&1; } ||
	{ [ "$cluster_code" = 200 ] && jq -e --arg id "$CLUSTER_ID" '.id == $id and
		(.status == "ERASED" or .status == "DELETED")' "$out" >/dev/null; }; then
	echo "PostgreSQL: отсутствие своего кластера подтверждено адресной проверкой (HTTP $cluster_code)"
else
	echo "::warning::PostgreSQL: отсутствие своего кластера не подтверждено (HTTP ${cluster_code:-000})"
	unknown "исход удаления родительского кластера не установлен"
	exit 0
fi
list_code=$(get "/v1/clusters/$CLUSTER_ID/backups")
list_n=$(jq -r 'if (.backups | type) == "array" then (.backups | length) else "-" end' "$out" 2>/dev/null || echo '?')
echo "GET /v1/clusters/{id}/backups: $(show "$list_code"), копий $list_n"
inventory="$RUNNER_TEMP/pg-after-delete-list.json"
if [ "$list_code" = 200 ]; then cp "$out" "$inventory"; else : > "$inventory"; fi
while read -r n encoded; do
	one_code=$(get "/v1/clusters/$CLUSTER_ID/backups/$encoded")
	python3 "$receipts" result "$n" "${one_code:-000}" "$out" "$inventory"
done < "$targets"
