#!/usr/bin/env bash
# Проверка e2e_rules_sweep.sh (job ci) на фейках evo и curl: правило «evo:»
# порта e2e снимается, только если в его группе нет ВМ; группа с ВМ или
# непрочитанные интерфейсы — правило остаётся; идущий прогон e2e — ничего.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
mkdir "$dir/bin"
# Группы: sg-free (без ВМ), sg-used (ВМ пользователя), sg-err (интерфейсы
# не читаются), у каждой правило «evo:» порта 80; у sg-free ещё правило
# пользователя без «evo:» — его не трогаем никогда.
cat > "$dir/bin/evo" <<'FAKE'
#!/usr/bin/env bash
case "$*" in
  *vm_list*) cat "$FAKE_DIR/vms.json" ;;
  *list_security_groups*) cat <<'JSON'
{"security_groups":[
 {"id":"sg-free","rules":[{"id":"r-free","direction":"ingress","port_range":"80:80","description":"evo: deploy"},
                          {"id":"r-user","direction":"ingress","port_range":"8080:8080","description":"web"}]},
 {"id":"sg-used","rules":[{"id":"r-used","direction":"ingress","port_range":"80:80","description":"evo: deploy"}]},
 {"id":"sg-err","rules":[{"id":"r-err","direction":"ingress","port_range":"8080:8080","description":"evo: deploy"}]}]}
JSON
  ;;
esac
FAKE
cat > "$dir/bin/curl" <<'FAKE'
#!/usr/bin/env bash
# The https:// argument is the URL; -o FILE takes the body, -w the code.
out=/dev/stdout url= method=GET
while [ $# -gt 0 ]; do case $1 in -o) out=$2; shift ;; -X) method=$2; shift ;; https://*) url=$1 ;; esac; shift; done
echo "$method $url" >> "$FAKE_DIR/calls"
case "$url" in
  */auth/token) printf '{"access_token":"tok"}' ;;
  *security_group_id=sg-free*) printf '{"items":[{"id":"if-1","vm":null}]}' > "$out"; printf 200 ;;
  *security_group_id=sg-used*) printf '{"items":[{"id":"if-2","vm":{"id":"vm-u","name":"my-app"}}]}' > "$out"; printf 200 ;;
  *security_group_id=sg-err*) printf '{"message":"boom"}' > "$out"; printf 500 ;;
  */rules/*) printf 204 ;;
esac
FAKE
chmod +x "$dir/bin/evo" "$dir/bin/curl"
export FAKE_DIR=$dir PATH="$dir/bin:$PATH" KEY_ID=k SECRET=s PROJECT=p RUNNER_TEMP=$dir
fail=0
want() { if [ "$2" != "$3" ]; then printf 'FAIL %s\n  got:  %s\n  want: %s\n' "$1" "$2" "$3"; fail=1; fi; }

echo '{"vms":[{"name":"my-app"}]}' > "$dir/vms.json"
: > "$dir/calls"
"$here/e2e_rules_sweep.sh" > "$dir/out"
want "снято только правило группы без ВМ" "$(grep '^DELETE' "$dir/calls" | sed 's|.*/security-groups/||')" "sg-free/rules/r-free"
grep -q "sg-used\|занята\|ВМ" "$dir/out" || { echo "FAIL: о сохранённом правиле группы с ВМ не сказано"; fail=1; }

echo '{"vms":[{"name":"evo-e2e-app"}]}' > "$dir/vms.json"
: > "$dir/calls"
set +e
"$here/e2e_rules_sweep.sh" > "$dir/out" 2>&1; rc=$?
set -e
want "идущий прогон e2e — код 1" "$rc" 1
want "идущий прогон e2e — ни одного запроса" "$(cat "$dir/calls")" ""

[ "$fail" = 0 ] && echo "e2e_rules_sweep: ok"
exit "$fail"
