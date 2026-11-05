#!/usr/bin/env bash
# Проверка e2e_port_rule.sh (job ci) на фейке evo: шаг правила порта e2e
# падает, если сценарий правила не проверил — deploy правило не открыл
# (opened_rules 0) или правило «evo:» порта было в группе до deploy; иначе —
# зелёный, только когда правило после destroy исчезло (разбор run 174:
# с 04.11 шаг зеленел вхолостую, правило на 80 было чужое).
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
mkdir "$dir/bin"
# shellcheck source-path=SCRIPTDIR source=fake_evo.sh
. "$here/fake_evo.sh"
fake_evo_install "$dir/bin"
export FAKE_DIR=$dir PATH="$dir/bin:$PATH" SG_ID=sg-1 PORT=21741 E2E_RULE_WAIT=0
fail=0
want() { if [ "$2" != "$3" ]; then printf 'FAIL %s\n  got:  %s\n  want: %s\n' "$1" "$2" "$3"; fail=1; fi; }
run() {
	rm -f "$dir"/.n.*
	set +e
	"$here/e2e_port_rule.sh" > "$dir/out" 2>&1; rc=$?
	set -e
}
none='{"security_groups":[{"id":"sg-1","rules":[{"direction":"ingress","port_range":"80:80","description":""}]}]}'
mine='{"security_groups":[{"id":"sg-1","rules":[{"direction":"ingress","port_range":"21741:21741","description":"evo: port 21741 (vm vm-1)"}]}]}'
other='{"security_groups":[{"id":"sg-2","rules":[{"direction":"ingress","port_range":"21741:21741","description":"evo: port 21741 (vm vm-2)"}]}]}'

printf '%s\n' "$none" > "$dir/before.json"
export BEFORE=$dir/before.json

OPENED=0 run
want "deploy не открыл правило — код 1" "$rc" 1
want "deploy не открыл правило — сказано" "$(grep -c 'сценарий правила не проверил' "$dir/out")" 1
want "id группы маскируется" "$(grep -c '^::add-mask::sg-1$' "$dir/out")" 1

printf '%s\n' "$mine" > "$dir/before.json"
OPENED=1 run
want "правило было до deploy — код 1" "$rc" 1
want "правило было до deploy — сказано" "$(grep -c 'сценарий правила не проверил' "$dir/out")" 1

printf '%s\n' "$none" > "$dir/before.json"
fake_evo_answer list_security_groups 0 0 "$mine"
fake_evo_answer list_security_groups 1 0 "$other"
OPENED=1 run
want "правило снято со второго чтения — код 0" "$rc" 0
want "правило снято — сказано" "$(grep -c '^правило порта 21741 удалено' "$dir/out")" 1

rm -f "$dir"/list_security_groups.*
fake_evo_answer list_security_groups 0 0 "$mine"
OPENED=1 run
want "правило осталось — код 1" "$rc" 1
want "правило осталось — сказано" "$(grep -c 'осталось правило «evo:» для порта 21741' "$dir/out")" 1

rm -f "$dir"/list_security_groups.*
fake_evo_answer list_security_groups 0 1 '{"error":{"reason":"API_ERROR","message":"boom"}}'
OPENED=1 run
want "группы не прочитаны — код не 0" "$([ "$rc" != 0 ] && echo yes)" yes

OPENED='' run
want "OPENED не задан — код не 0" "$([ "$rc" != 0 ] && echo yes)" yes

[ "$fail" = 0 ] && echo "e2e_port_rule: ok"
exit "$fail"
