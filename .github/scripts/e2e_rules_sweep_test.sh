#!/usr/bin/env bash
# Проверка e2e_rules_sweep.sh (job ci) на фейках evo и curl: правило «evo:»
# порта e2e снимается, только если группа отвечает has_interfaces: false;
# true, ответ не 200, поля нет или оно не boolean — правило остаётся
# (отказ в безопасную сторону); идущий прогон e2e — ничего; правило с
# меткой evo снимается, только если ВМ метки нет в проекте.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
mkdir "$dir/bin"
# shellcheck source-path=SCRIPTDIR source=fake_evo.sh
. "$here/fake_evo.sh"
fake_evo_install "$dir/bin"
# Уборка по префиксу спрашивает живые запуски cloud-e2e (e2e_live_runs.sh):
# заглушка gh — их нет; своя проверка — e2e_live_runs_test.sh.
printf '#!/bin/sh\n' >"$dir/bin/gh"
chmod +x "$dir/bin/gh"
export GITHUB_REPOSITORY=o/r E2E_LIVE_TTL=0
cat > "$dir/bin/curl" <<'FAKE'
#!/usr/bin/env bash
# The https:// argument is the URL; -o FILE takes the body, -w the code.
out=/dev/stdout url= method=GET
# --data-binary @- reads the body from stdin, as curl does: otherwise the
# writer of the pipe (jq) meets a closed pipe under pipefail (CI of #260).
while [ $# -gt 0 ]; do case $1 in -o) out=$2; shift ;; -X) method=$2; shift ;; @-) cat >/dev/null ;; https://*) url=$1 ;; esac; shift; done
echo "$method $url" >> "$FAKE_DIR/calls"
case "$url" in
  */auth/token) printf '{"access_token":"tok"}' ;;
  */rules/*) printf 204 ;;
  */security-groups/sg-free) printf '{"id":"sg-free","has_interfaces":false}' > "$out"; printf 200 ;;
  */security-groups/sg-used) printf '{"id":"sg-used","has_interfaces":true}' > "$out"; printf 200 ;;
  */security-groups/sg-err) printf '{"message":"boom"}' > "$out"; printf 500 ;;
  */security-groups/sg-renamed) printf '{"id":"sg-renamed","interfaces_present":false}' > "$out"; printf 200 ;;
  */security-groups/sg-string) printf '{"id":"sg-string","has_interfaces":"false"}' > "$out"; printf 200 ;;
esac
FAKE
chmod +x "$dir/bin/curl"
export FAKE_DIR=$dir PATH="$dir/bin:$PATH" KEY_ID=k SECRET=s PROJECT=p RUNNER_TEMP=$dir
# Ответ vm_list — в каждом случае ниже.
# Группы с правилом «evo:» порта e2e: sg-free (без интерфейсов), sg-used
# (с интерфейсами), sg-err (группа не читается), sg-renamed (ответ без
# has_interfaces), sg-string (has_interfaces строкой). У sg-free ещё правило
# пользователя без «evo:» — его не трогаем никогда.
fake_evo_answer list_security_groups 0 0 '{"security_groups":[
 {"id":"sg-free","rules":[{"id":"r-free","direction":"ingress","port_range":"80:80","description":"evo: deploy"},
                          {"id":"r-user","direction":"ingress","port_range":"8080:8080","description":"web"}]},
 {"id":"sg-used","rules":[{"id":"r-used","direction":"ingress","port_range":"80:80","description":"evo: deploy"}]},
 {"id":"sg-err","rules":[{"id":"r-err","direction":"ingress","port_range":"8080:8080","description":"evo: deploy"}]},
 {"id":"sg-renamed","rules":[{"id":"r-ren","direction":"ingress","port_range":"8081:8081","description":"evo: deploy"}]},
 {"id":"sg-string","rules":[{"id":"r-str","direction":"ingress","port_range":"8082:8082","description":"evo: deploy"}]}]}'
fail=0
want() { if [ "$2" != "$3" ]; then printf 'FAIL %s\n  got:  %s\n  want: %s\n' "$1" "$2" "$3"; fail=1; fi; }

fake_evo_answer vm_list 0 0 '{"vms":[{"name":"my-app"}]}'
: > "$dir/calls"
"$here/e2e_rules_sweep.sh" > "$dir/out"
want "снято только правило группы без ВМ" "$(grep '^DELETE' "$dir/calls" | sed 's|.*/security-groups/||')" "sg-free/rules/r-free"
want "оставлено правил" "$(grep -c 'оставлено' "$dir/out")" 4

fake_evo_answer vm_list 0 0 '{"vms":[{"name":"evo-e2e-app"}]}'
: > "$dir/calls"
set +e
"$here/e2e_rules_sweep.sh" > "$dir/out" 2>&1; rc=$?
set -e
want "идущий прогон e2e — код 1" "$rc" 1
want "идущий прогон e2e — ни одного запроса" "$(cat "$dir/calls")" ""

# Правило-сирота (решение владельца 05.11, вариант А): метка evo с id ВМ
# («evo: port N (vm <id>)»), а такой ВМ в проекте нет, — снимается и в
# группе с интерфейсами (sg-used), строкой «снято правило-сирота порта N».
# ВМ метки есть — правило остаётся, даже в группе без интерфейсов: его
# закроет удаление той ВМ (P6). Правило без метки — прежнее правило.
fake_evo_answer list_security_groups 0 0 '{"security_groups":[
 {"id":"sg-used","rules":[{"id":"r-orphan","direction":"ingress","port_range":"80:80","description":"evo: port 80 (vm vm-gone)"},
                          {"id":"r-live","direction":"ingress","port_range":"8080:8080","description":"evo: port 8080 (vm vm-here)"},
                          {"id":"r-old","direction":"ingress","port_range":"8081:8081","description":"evo: deploy"}]},
 {"id":"sg-free","rules":[{"id":"r-live2","direction":"ingress","port_range":"8082:8082","description":"evo: port 8082 (vm vm-here)"}]},
 {"id":"sg-err","rules":[{"id":"r-app","direction":"ingress","port_range":"21741:21741","description":"evo: port 21741 (vm vm-gone)"},
                         {"id":"r-wide","direction":"ingress","port_range":"21741:21749","description":"evo: port 21741 (vm vm-gone)"},
                         {"id":"r-span","direction":"ingress","port_range":"20000:29999","description":"evo: port 20000 (vm vm-gone)"},
                         {"id":"r-high","direction":"ingress","port_range":"31741:31741","description":"evo: port 31741 (vm vm-gone)"}]}]}'
fake_evo_answer vm_list 0 0 '{"vms":[{"id":"vm-here","name":"my-app"}]}'
: > "$dir/calls"
"$here/e2e_rules_sweep.sh" > "$dir/out"
# Порт приложения deploy e2e (20000–29999) — тоже порт e2e: его сирота
# снимается; диапазон и порт вне 20000–29999 — не порты e2e, не трогаются.
want "снята только сирота" "$(grep '^DELETE' "$dir/calls" | sed 's|.*/security-groups/||')" "$(printf 'sg-used/rules/r-orphan\nsg-err/rules/r-app')"
want "строка о сироте" "$(grep -c 'снято правило-сирота порта 80' "$dir/out")" 1
want "правила ВМ, которая есть, и без метки — оставлены" "$(grep -c 'оставлено' "$dir/out")" 3
want "правило ВМ, которая есть, в группе без интерфейсов — оставлено" "$(grep -c '^правило 8082:8082: ВМ метки есть — оставлено' "$dir/out")" 1

# Список ВМ не прочитан или без поля vms — ни одно правило не снимается:
# ВМ метки могла быть в непрочитанной части (решение владельца — снимать
# сироту, только когда список ВМ прочитан).
for bad in 'vm_list 1 {"error":{"reason":"API_ERROR","message":"boom"}}' 'vm_list 0 {"items":[]}'; do
	read -r tool code body <<<"$bad"
	fake_evo_answer "$tool" 0 "$code" "$body"
	: > "$dir/calls"
	set +e
	"$here/e2e_rules_sweep.sh" > "$dir/out" 2>&1; rc=$?
	set -e
	want "список ВМ не прочитан ($code) — код не 0" "$([ "$rc" != 0 ] && echo yes)" yes
	want "список ВМ не прочитан ($code) — ни одного DELETE" "$(grep -c '^DELETE' "$dir/calls")" 0
done

[ "$fail" = 0 ] && echo "e2e_rules_sweep: ok"
exit "$fail"
