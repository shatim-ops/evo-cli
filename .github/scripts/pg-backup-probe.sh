#!/usr/bin/env bash
# Проба реестра DB-22 (G13-27): что отвечает список резервных копий кластера
# PostgreSQL после его удаления. Документация Cloud.ru: при удалении кластера
# удаляются «а также резервные копии кластера»
# (https://cloud.ru/docs/paas-postgresql/ug/topics/guides__deleting-cluster).
# Ожидание — копий нет; расхождение с документацией — копия найдена; ответ
# не 200 и не 404 на список — отдельное расхождение API. Начисления проба не
# видит. Вызывает job dbaas-pg в cloud-e2e.yml
# после шага удаления кластера; окружение: CLUSTER_ID (кластер прогона,
# уже замаскирован), BACKUP_NAME (копия, сделанная pg_backup_create; нет —
# проба не проводится),
# KEY_ID, SECRET — ключ прогона.
#
# Проба, не проверка: итог — строка «DB-22 проба: …» для реестра, код выхода
# всегда 0, кроме неверного вызова. Ничего не создаёт и не удаляет: evo
# pg_backups и два GET API. По спецификации PostgreSQL (BackupService) у
# копии нет ни DELETE, ни списка по проекту — только список и чтение по
# имени внутри кластера; восстановление в новый кластер (recovery_spec.
# backup_id в создании) создало бы платный кластер и здесь не проверяется.
#
# Лог публичный: печатаются коды HTTP, reason, имена полей и количества,
# текст ошибки API — через redact.sed и обрезанным. ID и токен не печатаются.
set -euo pipefail

: "${CLUSTER_ID:?нужен CLUSTER_ID}"
redact() { sed -E -f "$(dirname "$0")/redact.sed"; }
api=https://postgresql.api.cloud.ru
out="${RUNNER_TEMP:-/tmp}/pg-backup-probe.json"
trap 'rm -f "$out"' EXIT

# Копии не было (pg_backup_create упал раньше): вопрос DB-22 не задан.
if [ -z "${BACKUP_NAME:-}" ]; then
	echo "DB-22 проба: не проведена — ручной копии в прогоне нет"
	exit 0
fi

# Кластер обязан быть удалён: иначе проба отвечает не на вопрос DB-22.
if ! evo tools call pg_list --json >"$out" 2>/dev/null; then
	echo "DB-22 проба: не проведена — pg_list не прочитан"
	exit 0
fi
if jq -e --arg id "$CLUSTER_ID" 'any(.clusters[]?; .id == $id)' "$out" >/dev/null; then
	echo "DB-22 проба: не проведена — кластер ещё в pg_list ($(jq -r --arg id "$CLUSTER_ID" '[.clusters[] | select(.id == $id) | .status][0]' "$out"))"
	exit 0
fi

# 1. evo pg_backups по ID удалённого кластера.
set +e
evo tools call pg_backups --arg cluster_id="$CLUSTER_ID" --json >"$out" 2>/dev/null
evo_rc=$?
set -e
evo_reason=$(jq -r '.reason // .error.reason // "-"' "$out" 2>/dev/null || echo '?')
evo_n=$(jq -r 'if (.backups | type) == "array" then (.backups | length) else "-" end' "$out" 2>/dev/null || echo '?')
echo "evo pg_backups: код $evo_rc, reason $evo_reason, копий $evo_n, поля $(jq -c 'keys' "$out" 2>/dev/null || echo '-')"
[ "$evo_rc" = 0 ] || echo "evo pg_backups: $(jq -r '.error // empty | tostring' "$out" 2>/dev/null | redact | cut -c1-200)"

# 2. Те же копии прямым GET API: список и копия по имени.
if [ -z "${KEY_ID:-}" ] || [ -z "${SECRET:-}" ]; then
	echo "DB-22 проба: evo pg_backups — код $evo_rc ($evo_reason, копий $evo_n); API не проверен: нет ключа"
	exit 0
fi
# Тело запроса токена — через stdin, не в argv.
token=$(jq -n --arg k "$KEY_ID" --arg s "$SECRET" '{keyId:$k, secret:$s}' |
	curl -sS -X POST https://iam.api.cloud.ru/api/v1/auth/token -H 'Content-Type: application/json' --data-binary @- |
	jq -r '.access_token // .accessToken // empty') || token=
[ -n "$token" ] && echo "::add-mask::$token"
get() { # ПУТЬ → тело в $out, код HTTP на stdout
	curl -sS -o "$out" -w '%{http_code}' -H "Authorization: Bearer $token" "$api$1" || echo 000
}
show() { # КОД — что ответил API: поля и текст ошибки
	local keys msg
	keys=$(jq -c 'if type == "object" then keys else type end' "$out" 2>/dev/null || echo '-')
	msg=$(jq -r '.message // .error // empty | tostring' "$out" 2>/dev/null | redact | cut -c1-160)
	echo "http=$1, поля $keys${msg:+, «$msg»}"
}
if [ -z "$token" ]; then
	echo "DB-22 проба: evo pg_backups — код $evo_rc ($evo_reason, копий $evo_n); API не проверен: нет токена"
	exit 0
fi
list_code=$(get "/v1/clusters/$CLUSTER_ID/backups")
list_n=$(jq -r 'if (.backups | type) == "array" then (.backups | length) else "-" end' "$out" 2>/dev/null || echo '?')
echo "GET /v1/clusters/{id}/backups: $(show "$list_code"), копий $list_n"
one_code=$(get "/v1/clusters/$CLUSTER_ID/backups/$BACKUP_NAME")
echo "GET /v1/clusters/{id}/backups/{name}: $(show "$one_code"), статус $(jq -r '.status // "-"' "$out" 2>/dev/null || echo '?')"

# Итог под гипотезу документации: копии уходят вместе с кластером. Копия,
# которая читается по имени, существует — расхождение при любом ответе списка.
if { [ "$list_code" = 200 ] && [ "$list_n" != 0 ] && [ "$list_n" != "-" ]; } || [ "$one_code" = 200 ]; then
	verdict="РАСХОЖДЕНИЕ с документацией: копия удалённого кластера найдена"
elif { [ "$list_code" = 200 ] && [ "$list_n" = 0 ]; } || [ "$list_code" = 404 ]; then
	verdict="как в документации: копий нет"
else
	verdict="копий не видно, но список отвечает $list_code вместо пустого списка или 404 — расхождение API"
fi
echo "DB-22 проба: $verdict (API: список — $list_code, копий $list_n; копия по имени — $one_code;" \
	"evo pg_backups — код $evo_rc, $evo_reason). DELETE копии и списка по проекту в спецификации нет; начисления не проверялись"
