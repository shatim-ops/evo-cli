#!/usr/bin/env bash
# Проба «повтор создания пользователя и базы PostgreSQL с тем же именем»
# (план 1.9, строка «Повтор pg_db_create и pg_user_create после потерянного
# ответа…», пункт 2): что отвечает платформа на второй запрос создания, когда
# первый принят. evo объявляет повтор безопасным словами «второго с тем же
# именем PostgreSQL отвергает» — до двух прогонов это гипотеза.
#
# Идёт в job dbaas-pg после шага, который создал пользователя и базу через
# evo, и до их удаления. Запросы — прямые: evo повтор отвергает сам, до
# запроса (имя уже в списке), и ответа платформы не показал бы.
#   POST /v1/clusters/{id}/users      {name, password}  — тот же пользователь
#   POST /v1/clusters/{id}/databases  {name, owner}     — та же база
# Пароль повторного запроса — новый случайный (маскируется): принятый повтор
# сменил бы пароль пользователя пробы; он дальше нигде не нужен.
#
# Что записывается: код HTTP, имена полей ответа, машинный код и текст
# отказа; сколько пользователей и баз с этим именем в списке до запроса и
# через PG_REPEAT_PAUSE секунд после. Шаг не падает: любой исход — ответ
# платформы. Нового ресурса проба не заводит: имена — те, что уже созданы.
#
# Лог публичный: ID кластера и токен не печатаются, текст ошибки API — через
# redact.sed и обрезанным.
set -euo pipefail

: "${CLUSTER_ID:?нужен CLUSTER_ID}"
echo "::add-mask::$CLUSTER_ID"
user=${PG_USER:-evo_e2e_user}
db=${PG_DB:-evo_e2e_db}
redact() { sed -E -f "$(dirname "$0")/redact.sed"; }
api=https://postgresql.api.cloud.ru
out="${RUNNER_TEMP:-/tmp}/pg-repeat-probe.json"
trap 'rm -f "$out"' EXIT

if [ -z "${KEY_ID:-}" ] || [ -z "${SECRET:-}" ]; then
	echo "повтор создания: не проведена — нет ключа"
	exit 0
fi
# Тело запроса токена — через stdin, не в argv.
token=$(jq -n --arg k "$KEY_ID" --arg s "$SECRET" '{keyId:$k, secret:$s}' |
	curl -sS -X POST https://iam.api.cloud.ru/api/v1/auth/token -H 'Content-Type: application/json' --data-binary @- 2>/dev/null |
	jq -r '.access_token // .accessToken // empty') || token=
if [ -z "$token" ]; then
	echo "повтор создания: не проведена — токен IAM не получен"
	exit 0
fi
echo "::add-mask::$token"

# req МЕТОД ПУТЬ [ТЕЛО] — код HTTP в $code, тело ответа — в $out.
req() {
	: >"$out"
	if [ -n "${3:-}" ]; then
		code=$(printf '%s' "$3" | curl -sS -o "$out" -w '%{http_code}' -X "$1" "$api/v1/clusters/$CLUSTER_ID/$2" \
			-H "Authorization: Bearer $token" -H 'Content-Type: application/json' --data-binary @- 2>/dev/null) || code=000
	else
		code=$(curl -sS -o "$out" -w '%{http_code}' -X "$1" "$api/v1/clusters/$CLUSTER_ID/$2" -H "Authorization: Bearer $token" 2>/dev/null) || code=000
	fi
}
# count ПУТЬ КЛЮЧ ИМЯ — сколько записей списка носят ИМЯ («?» — не прочитан).
count() {
	req GET "$1"
	case "$code" in 2??) jq -r --arg k "$2" --arg n "$3" 'if (.[$k] | type) == "array" then ([.[$k][] | select(.name == $n)] | length) else "?" end' "$out" 2>/dev/null || echo '?' ;; *) echo '?' ;; esac
}
# answer — последний ответ одной строкой: код, поля, машинный код, текст.
answer() {
	local keys mc text
	keys=$(jq -c 'if type == "object" then keys else type end' "$out" 2>/dev/null) || keys='"не JSON"'
	case "$code" in
	2??) echo "принят: HTTP $code, поля ответа: $keys" ;;
	000) echo "ответа нет — исход запроса неизвестен" ;;
	*)
		mc=$(jq -r '[(if type == "array" then .[] else . end) | objects | .code?] | map(select(. != null) | tostring) | join(",")' "$out" 2>/dev/null) || mc=
		text=$(jq -r '[(if type == "array" then .[] else . end) | objects | .message? // empty] | join("; ")' "$out" 2>/dev/null | redact | cut -c1-160) || text=
		echo "отвергнут: HTTP $code, поля ответа: $keys, машинный код: ${mc:-нет}, текст: ${text:-нет}"
		;;
	esac
}

u0=$(count users users "$user")
d0=$(count databases databases "$db")
if [ "$u0" != 1 ] || [ "$d0" != 1 ]; then
	echo "повтор создания: не проведена — до запроса пользователей с именем пробы: $u0, баз: $d0 (нужно по одному: их создаёт предыдущий шаг)"
	exit 0
fi

raw=$(openssl rand -base64 48 | tr -dc 'A-Za-z0-9')
pw="${raw:0:16}Q_z7"
echo "::add-mask::$pw"
req POST users "$(jq -nc --arg c "$CLUSTER_ID" --arg n "$user" --arg p "$pw" '{cluster_id: $c, name: $n, password: $p}')"
ua=$(answer)
ucode=$code
req POST databases "$(jq -nc --arg c "$CLUSTER_ID" --arg n "$db" --arg o "$user" '{cluster_id: $c, name: $n, owner: $o}')"
da=$(answer)
dcode=$code

sleep "${PG_REPEAT_PAUSE:-30}"
u1=$(count users users "$user")
d1=$(count databases databases "$db")
echo "повтор создания пользователя: $ua; пользователей с этим именем: было $u0, через ${PG_REPEAT_PAUSE:-30} с — $u1"
echo "повтор создания базы: $da; баз с этим именем: было $d0, через ${PG_REPEAT_PAUSE:-30} с — $d1"
case "$ucode$dcode" in *2[0-9][0-9]*)
	echo "::warning::повтор создания PostgreSQL принят платформой (пользователь: HTTP $ucode, база: HTTP $dcode) — объявление evo «второго с тем же именем PostgreSQL отвергает» этим прогоном не подтверждено; пароль пользователя пробы мог смениться"
	;;
esac
