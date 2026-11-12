#!/usr/bin/env bash
# Уборка правил «evo:» для портов e2e (80/8080/8081/8082/8086 и порт
# приложения deploy 20000–29999, только одиночный N:N) в job cleanup
# cloud-e2e. Описание «evo: …» ставит любой evo deploy и vm create, поэтому
# правило снимается, только если в его группе безопасности нет ни одного
# сетевого интерфейса — ни ВМ, ни отвязанного порта:
# раньше снимались все, когда в проекте нет ВМ evo-*, и evo deploy под
# другим именем в том же проекте терял бы своё правило (release-plan 1.3,
# задача процесса). Есть ВМ evo-e2e-* — ничего не трогает: ВМ завершённого
# запуска — код 1, ВМ только живых запусков (e2e_live_runs.sh) — код 0. Признак — has_interfaces группы (Compute GET
# /api/v1/security-groups/{id}, PublicSecurityGroupResponse: обязательное
# boolean «наличие сетевых интерфейсов, которые добавлены в группу»).
# Снимается только при has_interfaces строго false: ответ не 200, поля нет
# или оно не boolean — правило остаётся (отказ в безопасную сторону, ревью
# «1.3 — релиза»: ошибка модели API не должна снять правило у ВМ
# пользователя). Исключение — правило с меткой evo «evo: port N (vm <id>)»
# (P6): ВМ метки нет в проекте — правило-сирота, снимается и в группе с
# интерфейсами; ВМ есть — остаётся (решение владельца 05.11, вариант А).
# Правила удаляет
# API напрямую: в evo нет команды удаления правила.
# Проверка — e2e_rules_sweep_test.sh (job ci). Окружение: KEY_ID, SECRET,
# PROJECT, RUNNER_TEMP.
set -euo pipefail
: "${KEY_ID:?}" "${SECRET:?}" "${PROJECT:?}" "${RUNNER_TEMP:?}"
compute=https://compute.api.cloud.ru/api/v1

# Список ВМ решает, что правило — сирота: только прочитанный целиком.
# evo отвечает ошибкой на неполный список (все страницы или ошибка), а
# ответ без массива vms — не «ВМ нет»: в обоих случаях ничего не снимаем.
evo tools call vm_list --json > "$RUNNER_TEMP/vms.json" ||
	{ echo "::error::vm_list не прочитан — правила «evo:» не трогаю"; exit 1; }
jq -e '.vms | type == "array"' "$RUNNER_TEMP/vms.json" >/dev/null ||
	{ echo "::error::в ответе vm_list нет массива vms — правила «evo:» не трогаю"; exit 1; }
# Уборка идёт рядом с живыми запусками (e2e_live_runs.sh): ВМ завершённого
# запуска — остаток, который шаг ВМ не убрал, код 1; ВМ только живых
# запусков — правила не трогаю, код 0 (их снимет следующая уборка).
live=$("$(dirname "$0")/e2e_live_runs.sh") ||
	{ echo "::error::живые запуски cloud-e2e неизвестны — правила «evo:» не трогаю"; exit 1; }
n=$(jq --arg live "$live" '[.vms[]? | select((.name // "") | (startswith("evo-e2e-") and (test($live) | not)))] | length' "$RUNNER_TEMP/vms.json")
[ "$n" = 0 ] || { echo "::error::в проекте есть ВМ evo-e2e-* завершённых запусков ($n) — правила «evo:» не трогаю"; exit 1; }
n=$(jq '[.vms[]? | select((.name // "") | startswith("evo-e2e-"))] | length' "$RUNNER_TEMP/vms.json")
[ "$n" = 0 ] || { echo "::notice::идёт запуск cloud-e2e (ВМ evo-e2e-*: $n) — правила «evo:» не трогаю"; exit 0; }
evo tools call list_security_groups --arg rules=true --json > "$RUNNER_TEMP/sg.json"
# Четвёртое поле — id ВМ из метки evo «evo: port N (vm <id>)» (P6) или «-».
rules=$(jq -r '.security_groups[] | .id as $g | .rules[]? | select(.direction == "ingress" and (.port_range == "80:80" or .port_range == "8080:8080" or .port_range == "8081:8081" or .port_range == "8082:8082" or .port_range == "8086:8086" or (.port_range | tostring | test("^(2[0-9]{4}):\\1$"))) and ((.description // "") | startswith("evo:"))) | "\($g) \(.id) \(.port_range) \((.description | capture("^evo: port [^ ]+ \\(vm (?<vm>[^ )]+)\\)$") | .vm) // "-")"' "$RUNNER_TEMP/sg.json")
[ -n "$rules" ] || { echo "правил «evo:» для 80/8080/8081/8082/8086 и 20000–29999 нет"; exit 0; }
TOKEN=$(jq -n --arg k "$KEY_ID" --arg s "$SECRET" '{keyId:$k, secret:$s}' |
	curl -sS -X POST https://iam.api.cloud.ru/api/v1/auth/token -H 'Content-Type: application/json' --data-binary @- |
	jq -r '.access_token // .accessToken // empty')
[ -n "$TOKEN" ] || { echo "::error::нет токена"; exit 1; }
echo "::add-mask::$TOKEN"
while read -r g r port vm; do
	echo "::add-mask::$g"
	echo "::add-mask::$r"
	# Метка evo называет ВМ (решение владельца 05.11, вариант А): ВМ нет в
	# проекте — правило-сирота, снимается и в группе с интерфейсами; ВМ есть —
	# правило остаётся, его закроет удаление той ВМ. vm_list прочитан
	# целиком — проверено выше.
	if [ "$vm" != - ]; then
		echo "::add-mask::$vm"
		if jq -e --arg id "$vm" 'any(.vms[]?; .id == $id)' "$RUNNER_TEMP/vms.json" >/dev/null; then
			echo "правило $port: ВМ метки есть — оставлено"
			continue
		fi
		code=$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE -H "Authorization: Bearer $TOKEN" \
			"$compute/security-groups/$g/rules/$r")
		echo "снято правило-сирота порта ${port%%:*}: ВМ метки в проекте нет, DELETE http=$code"
		continue
	fi
	code=$(curl -sS -o "$RUNNER_TEMP/sg-one.json" -w '%{http_code}' -H "Authorization: Bearer $TOKEN" \
		"$compute/security-groups/$g") || code=000
	busy=unknown
	[ "$code" != 200 ] || busy=$(jq -r 'if (.has_interfaces | type) == "boolean" then .has_interfaces else "unknown" end' "$RUNNER_TEMP/sg-one.json" 2>/dev/null || echo unknown)
	case "$busy" in
	false) ;;
	true)
		echo "правило $port: в группе есть интерфейсы (ВМ не из e2e) — оставлено"
		continue
		;;
	*)
		echo "::warning::правило $port: группа не прочитана или без has_interfaces (http=$code) — оставлено"
		continue
		;;
	esac
	code=$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE -H "Authorization: Bearer $TOKEN" \
		"$compute/security-groups/$g/rules/$r")
	echo "правило $port: в группе нет интерфейсов, DELETE http=$code"
done <<<"$rules"
