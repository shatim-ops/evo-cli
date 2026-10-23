#!/usr/bin/env bash
# Уборка правил «evo:» для портов e2e (80/8080/8081/8082/8086) в job cleanup
# cloud-e2e. Описание «evo: …» ставит любой evo deploy и vm create, поэтому
# правило снимается, только если в его группе безопасности нет ни одной ВМ:
# раньше снимались все, когда в проекте нет ВМ evo-*, и evo deploy под
# другим именем в том же проекте терял бы своё правило (release-plan 1.3,
# задача процесса). Идёт прогон e2e (есть ВМ evo-e2e-*) — ничего не трогает,
# код 1. Интерфейсы группы не прочитаны — правило остаётся. Правила удаляет
# API напрямую: в evo нет команды удаления правила.
# Проверка — e2e_rules_sweep_test.sh (job ci). Окружение: KEY_ID, SECRET,
# PROJECT, RUNNER_TEMP.
set -euo pipefail
: "${KEY_ID:?}" "${SECRET:?}" "${PROJECT:?}" "${RUNNER_TEMP:?}"
compute=https://compute.api.cloud.ru/api/v1

n=$(evo tools call vm_list --json | jq '[.vms[]? | select((.name // "") | startswith("evo-e2e-"))] | length')
[ "$n" = 0 ] || { echo "::error::в проекте есть ВМ evo-e2e-* ($n) — идёт прогон e2e, правила «evo:» не трогаю"; exit 1; }
evo tools call list_security_groups --arg rules=true --json > "$RUNNER_TEMP/sg.json"
rules=$(jq -r '.security_groups[] | .id as $g | .rules[]? | select(.direction == "ingress" and (.port_range == "80:80" or .port_range == "8080:8080" or .port_range == "8081:8081" or .port_range == "8082:8082" or .port_range == "8086:8086") and ((.description // "") | startswith("evo:"))) | "\($g) \(.id) \(.port_range)"' "$RUNNER_TEMP/sg.json")
[ -n "$rules" ] || { echo "правил «evo:» для 80/8080/8081/8082/8086 нет"; exit 0; }
TOKEN=$(jq -n --arg k "$KEY_ID" --arg s "$SECRET" '{keyId:$k, secret:$s}' |
	curl -sS -X POST https://iam.api.cloud.ru/api/v1/auth/token -H 'Content-Type: application/json' --data-binary @- |
	jq -r '.access_token // .accessToken // empty')
[ -n "$TOKEN" ] || { echo "::error::нет токена"; exit 1; }
echo "::add-mask::$TOKEN"
while read -r g r port; do
	echo "::add-mask::$g"
	echo "::add-mask::$r"
	# ВМ в группе: интерфейсы с этой группой (Compute API, /interfaces).
	code=$(curl -sS -o "$RUNNER_TEMP/ifaces.json" -w '%{http_code}' -H "Authorization: Bearer $TOKEN" \
		"$compute/interfaces?project_id=$PROJECT&security_group_id=$g&limit=1000") || code=000
	used=-
	[ "$code" != 200 ] || used=$(jq '[.items[]? | select(.vm != null)] | length' "$RUNNER_TEMP/ifaces.json" 2>/dev/null || echo -)
	case "$used" in
	0) ;;
	-)
		echo "::warning::правило $port: интерфейсы группы не прочитаны (http=$code) — оставлено"
		continue
		;;
	*)
		echo "правило $port: в группе ВМ ($used), не из e2e — оставлено"
		continue
		;;
	esac
	code=$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE -H "Authorization: Bearer $TOKEN" \
		"$compute/security-groups/$g/rules/$r")
	echo "правило $port: в группе нет ВМ, DELETE http=$code"
done <<<"$rules"
