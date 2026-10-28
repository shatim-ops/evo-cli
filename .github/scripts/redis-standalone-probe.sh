#!/usr/bin/env bash
# Проба G140-6 (Grok 1.4.0, реестр к DB-3/DB-11): какое тело создания
# кластера Redis STANDALONE принимает API. evo шлёт replicas=0 (у
# STANDALONE нет реплик, redisShape), платформа отвечает 400 «replicas must
# be positive». Через evo пробу не провести: redis_create для STANDALONE
# отвергает replicas ≠ 0 до запроса. Поэтому — прямой POST /v1/clusters с
# телом, которое собрал бы evo (превью redis_create --dry-run, без записи):
#   1. replicas 0 — как шлёт evo: воспроизводит 400 (счёт «N из M»);
#   2. без поля replicas;
#   3. replicas 1.
# Первый принятый вариант останавливает пробу: кластер создан, и второй
# создавать незачем. Вызывает job redis-standalone-probe в cloud-e2e.yml
# (вход redis_standalone_probe); окружение: ZONE_ID (уже замаскирован),
# CLUSTER (имя, evo-e2e-*), KEY_ID, SECRET, PROJECT — ключ и проект прогона.
#
# Проба, не проверка: итог — строка «G140-6 проба: …» для реестра; код
# выхода 0, кроме неверного вызова. Принятый вариант пишет в $GITHUB_ENV
# PROBE_ACCEPTED (номер варианта) и CLUSTER_ID, если API его вернул;
# удаляет кластер следующий шаг job (dbaas-delete.sh, всегда). Пишет в API
# только POST /v1/clusters и обмен ключа на токен.
#
# Лог публичный: печатаются коды HTTP, имена полей и текст ошибки API —
# через redact.sed и обрезанным. ID, токен и тело запроса не печатаются.
set -euo pipefail

: "${ZONE_ID:?нужен ZONE_ID}"
: "${CLUSTER:?нужно имя кластера}"
case "$CLUSTER" in evo-e2e-*) ;; *)
	echo "имя $CLUSTER не evo-e2e-*: проба таких кластеров не создаёт"
	exit 2
	;;
esac
redact() { sed -E -f "$(dirname "$0")/redact.sed"; }
api=https://redis.api.cloud.ru
out="${RUNNER_TEMP:-/tmp}/redis-sa-probe.json"
err="${RUNNER_TEMP:-/tmp}/redis-sa-probe.err"
trap 'rm -f "$out" "$err"' EXIT

# 1. Тело — из превью evo: версия, спецификация, подсеть и диск, которые
# evo выбрал бы сам (--dry-run ничего не создаёт и не спрашивает согласия).
set +e
evo --dry-run --region "$ZONE_ID" tools call redis_create --arg name="$CLUSTER" --arg type=STANDALONE \
	--json >"$out" 2>"$err"
pv_rc=$?
set -e
wc=$(jq -c '.preview.details.will_create // .will_create // empty' "$out" 2>/dev/null) || wc=
if [ -z "$wc" ]; then
	echo "превью redis_create: код $pv_rc, $(jq -rR 'fromjson? | .error | "\(.reason // "-"): \(.message // "-")"' "$err" 2>/dev/null | sed -n 1p | redact | cut -c1-200)"
	echo "G140-6 проба: не проведена — превью redis_create STANDALONE без will_create"
	exit 0
fi
for f in version_id specification_id subnet_id; do
	v=$(jq -r --arg f "$f" '.[$f] // empty' <<<"$wc")
	[ -z "$v" ] || echo "::add-mask::$v"
done
echo "превью redis_create: $(jq -c '{type, shards, replicas, hosts_per_shard, storage_gb, availability_zone}' <<<"$wc")"

if [ -z "${KEY_ID:-}" ] || [ -z "${SECRET:-}" ] || [ -z "${PROJECT:-}" ]; then
	echo "G140-6 проба: не проведена — нет ключа или проекта"
	exit 0
fi
# Тело запроса токена — через stdin, не в argv.
token=$(jq -n --arg k "$KEY_ID" --arg s "$SECRET" '{keyId:$k, secret:$s}' |
	curl -sS -X POST https://iam.api.cloud.ru/api/v1/auth/token -H 'Content-Type: application/json' --data-binary @- 2>/dev/null |
	jq -r '.access_token // .accessToken // empty') || token=
[ -n "$token" ] && echo "::add-mask::$token"
if [ -z "$token" ]; then
	echo "G140-6 проба: не проведена — нет токена"
	exit 0
fi

# Тело evo (tools_redis.go, Execute): name, project_id, version_id,
# specification_id, storage_gb, shards, replicas, type, subnet_id (в
# единственном числе: subnet_ids роняет бэкенд). $1 — jq-правка replicas.
body() {
	jq -c --arg p "$PROJECT" "{name, project_id: \$p, version_id, specification_id, storage_gb, shards, replicas, type, subnet_id} | $1" <<<"$wc"
}
post() { # ТЕЛО на stdin → ответ в $out, код HTTP на stdout; 000 — не дошёл
	# stderr curl — в никуда: его текст об ошибке может нести URL.
	curl -sS -o "$out" -w '%{http_code}' -X POST "$api/v1/clusters" -H "Authorization: Bearer $token" \
		-H 'Content-Type: application/json' --data-binary @- 2>/dev/null || echo 000
}
show() { # КОД — поля ответа и текст ошибки
	local keys msg
	if jq -e . "$out" >/dev/null 2>&1; then
		keys=$(jq -c 'if type == "object" then keys else type end' "$out")
		msg=$(jq -r '.message // .error // empty | tostring' "$out" | redact | cut -c1-160)
	else
		keys="не JSON"
		msg=$(head -c 4000 "$out" | tr '\n\r' '  ' | redact | cut -c1-160)
	fi
	echo "http=$1, поля $keys${msg:+, «$msg»}"
}

names=("replicas 0 (как evo)" "без поля replicas" "replicas 1")
edits=('.replicas = 0' 'del(.replicas)' '.replicas = 1')
codes=()
accepted=
for i in 0 1 2; do
	code=$(body "${edits[$i]}" | post)
	codes+=("$code")
	echo "POST /v1/clusters, ${names[$i]}: $(show "$code")"
	case "$code" in
	2??)
		accepted=$((i + 1))
		# Ответ — операция; ID кластера в ней — resource_id (dbaas.Operation),
		# не нашёлся — следующий шаг ищет кластер по имени.
		id=$(jq -r '.resource_id // .resourceId // .cluster_id // empty' "$out" 2>/dev/null) || id=
		[ -z "$id" ] || echo "::add-mask::$id"
		if [ -n "${GITHUB_ENV:-}" ]; then
			echo "PROBE_ACCEPTED=$accepted" >>"$GITHUB_ENV"
			[ -z "$id" ] || echo "CLUSTER_ID=$id" >>"$GITHUB_ENV"
		fi
		break
		;;
	000)
		# Запрос не дошёл: принят ли он, неизвестно — дальше не создаём.
		echo "PROBE_ACCEPTED=unknown" >>"${GITHUB_ENV:-/dev/null}"
		break
		;;
	esac
done

if [ -n "$accepted" ]; then
	verdict="API принимает STANDALONE с телом «${names[$((accepted - 1))]}»"
else
	verdict="ни один вариант не принят"
fi
echo "G140-6 проба: $verdict (коды: $(
	IFS=,
	echo "${codes[*]}"
)). Что создано — шаг ниже (redis_get); кластер удаляет dbaas-delete.sh"
