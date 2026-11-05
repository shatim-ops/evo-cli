#!/usr/bin/env bash
# Правило порта e2e после destroy (job e2e cloud-e2e). deploy открывает
# правило «evo:» для порта приложения в группе зоны, destroy его снимает.
# Сценарий проверяет это, только если правило открыл сам deploy: правила
# «evo:» порта не было в группе до deploy (BEFORE) и в state deploy было
# хоть одно открытое правило (OPENED). Иначе — провал «сценарий правила не
# проверил»: с 04.11 до run 174 шаг зеленел вхолостую — на 80 в общих
# группах было чужое правило, deploy ничего не открывал (release-plan 1.8).
# Правило снимается асинхронно — до 6 чтений с паузой E2E_RULE_WAIT.
# Проверка — e2e_port_rule_test.sh (job ci). Окружение: SG_ID, PORT, OPENED
# (число правил deploy в state до destroy), BEFORE (группы до deploy).
set -euo pipefail
: "${SG_ID:?}" "${PORT:?}" "${OPENED:?}" "${BEFORE:?}"
# The id is masked by the step that reads it from state; masked here too,
# as e2e_rules_sweep.sh does, so a reuse of the script keeps it out of the log.
echo "::add-mask::$SG_ID"
# $sg and $pr are jq's variables, not the shell's.
# shellcheck disable=SC2016
rule='[.security_groups[] | select(.id==$sg) | .rules[]? | select(.direction=="ingress" and (.port_range|tostring)==$pr and ((.description // "") | startswith("evo:") or startswith("cloudru-vm:")))] | length'
pr="$PORT:$PORT"
had=$(jq --arg sg "$SG_ID" --arg pr "$pr" "$rule" "$BEFORE")
if [ "$had" != 0 ]; then
	echo "::error::правило «evo:» для порта $PORT было в группе до deploy — destroy его не снимает, сценарий правила не проверил"
	exit 1
fi
if [ "$OPENED" = 0 ]; then
	echo "::error::deploy не открыл правило (opened_rules: 0): порт $PORT уже открыт в группе чужим правилом или deploy не дошёл до правил; сценарий правила не проверил"
	exit 1
fi
for _ in 1 2 3 4 5 6; do
	left=$(evo tools call list_security_groups --arg rules=true --json | jq --arg sg "$SG_ID" --arg pr "$pr" "$rule")
	[ "$left" = 0 ] && { echo "правило порта $PORT удалено"; exit 0; }
	sleep "${E2E_RULE_WAIT:-10}"
done
echo "::error::после destroy в группе deploy осталось правило «evo:» для порта $PORT"
exit 1
