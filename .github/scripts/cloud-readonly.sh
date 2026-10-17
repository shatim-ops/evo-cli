#!/usr/bin/env bash
# Проверки на настоящем Cloud.ru без платных ресурсов: только чтения API
# через CLI, все инструменты только для чтения без обязательных аргументов,
# MCP по stdio и по HTTP (гейт подтверждения, --allow-project, отказ без
# токена). Запускается job'ом readonly в cloud-e2e.yml после evo configure.
#
# Репозиторий публичный, лог — тоже: печатаются только имена, коды выхода,
# reason и счётчики, ни одного ответа API целиком. PROJECT (секрет) GitHub
# маскирует сам, но и его скрипт не печатает.
#
# Код выхода: 0 — все обязательные проверки прошли; 1 — нет. Необязательные
# (сервисы, которых может не быть в проекте) только попадают в таблицу.
set -euo pipefail

: "${PROJECT:?нужен PROJECT — UUID тестового проекта}"
tmp=$(mktemp -d)
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "$tmp"' EXIT
summary=${GITHUB_STEP_SUMMARY:-/dev/null}
failed=0

row() { # проверка | итог | подробности
	printf '%-45s %-5s %s\n' "$1" "$2" "$3"
	printf '| %s | %s | %s |\n' "$1" "$2" "$3" >>"$summary"
}
ok() { row "$1" ok "${2:-}"; }
bad() {
	row "$1" FAIL "${2:-}"
	failed=1
}
warn() { row "$1" warn "${2:-}"; }
# j — jq для подстановок: ответ не JSON (или пустой) не обрывает скрипт.
j() { jq "$@" 2>/dev/null || true; }

# run ИМЯ КОМАНДА... — stdout в $tmp/out, stderr в $tmp/err, код в $rc,
# reason ошибки (из JSON-ошибки CLI в stderr или ответа инструмента) в $reason.
run() {
	shift
	set +e
	"$@" >"$tmp/out" 2>"$tmp/err"
	rc=$?
	set -e
	reason=$(j -r '.error.reason? // empty' "$tmp/err" | head -1)
	[ -n "$reason" ] || reason=$(j -r '.reason? // empty' "$tmp/out" | head -1)
	[ -n "$reason" ] || reason=-
	# HTTP-статус из сообщения — без тела ответа и URL (в нём бывает проект).
	httpst=$(j -r '.error.message? // .error? // empty' "$tmp/err" "$tmp/out" | grep -oE '(статус|status|API|\()[ ]?[45][0-9]{2}' | grep -oE '[45][0-9]{2}' | head -1 || true)
	[ -z "$httpst" ] || reason="$reason, HTTP $httpst"
}

{
	echo "### Чтения на живом API"
	echo
	echo "| Проверка | Итог | Подробности |"
	echo "|---|---|---|"
} >>"$summary"

echo "== CLI"
run whoami evo whoami --json
if [ "$rc" = 0 ] && jq -e '.project_from == "profile" and .token_ttl_seconds > 0' "$tmp/out" >/dev/null; then
	ok "whoami --json" "проект из профиля, токен действует"
else
	bad "whoami --json" "код $rc, reason $reason"
fi

run en env EVO_LANG=en evo whoami
if [ "$rc" = 0 ] && grep -q '^Profile:' "$tmp/out"; then ok "EVO_LANG=en whoami" "вывод на английском"; else bad "EVO_LANG=en whoami" "код $rc"; fi

run doctor evo doctor --json
if [ "$rc" = 0 ] && jq -e '.ok' "$tmp/out" >/dev/null; then
	ok "doctor --json" "$(j -r '[.checks[].status] | group_by(.) | map("\(.[0]) \(length)") | join(", ")' "$tmp/out")"
else
	# kubectl/helm и прочее окружение раннера doctor тоже проверяет.
	warn "doctor --json" "код $rc, не прошли: $(j -r '[.checks[]? | select(.status=="fail") | .name] | join(", ")' "$tmp/out" 2>/dev/null)"
fi

# Ключ уровня проекта Organization API не отдаёт — это не ошибка (QA r3 C3).
run project evo project list --json
if [ "$rc" = 0 ] && jq -e 'has("projects")' "$tmp/out" >/dev/null; then
	ok "project list --json" "$(j -r 'if .projects then "проектов: \(.projects|length)" else "список недоступен (note), активный проект задан: \(.active != "")" end' "$tmp/out")"
else
	bad "project list --json" "код $rc, reason $reason"
fi

run zones evo list-zones --json
if [ "$rc" = 0 ] && jq -e 'any(.[]; .name == "ru.AZ-2")' "$tmp/out" >/dev/null; then
	ok "list-zones --json" "зон: $(j length "$tmp/out"), ru.AZ-2 есть"
else
	bad "list-zones --json" "код $rc, reason $reason"
fi

run images evo list-images --json
if [ "$rc" = 0 ] && jq -e 'length > 0' "$tmp/out" >/dev/null; then
	ok "list-images --json" "образов: $(j length "$tmp/out")"
else
	bad "list-images --json" "код $rc, reason $reason"
fi

for c in "vm list" "vm flavors" "vm zones"; do
	# shellcheck disable=SC2086 # подкоманда — два слова
	run vm evo $c --json
	if [ "$rc" = 0 ]; then ok "$c --json" ""; else bad "$c --json" "код $rc, reason $reason"; fi
done

# Несуществующая ВМ: ожидается «не найдено» (код 4, D11).
run vmget evo vm get 00000000-0000-4000-8000-000000000000 --json
if [ "$rc" = 4 ]; then ok "vm get <нет такой> → 4" "reason $reason"; else warn "vm get <нет такой> → 4" "код $rc, reason $reason"; fi

# Плагины mk8s: каталог общий (api-probe #5: 42 плагина), печатаются только
# счётчики. Версии cert-manager и «не найдено» — необязательные: состав
# каталога меняет Cloud.ru.
run addons evo k8s addons --json
if [ "$rc" = 0 ] && jq -e '.total > 0 and (.addons | length) == .total' "$tmp/out" >/dev/null; then
	ok "k8s addons --json" "плагинов: $(j -r '.total' "$tmp/out"), категорий: $(j -r '.categories | length' "$tmp/out")"
else
	bad "k8s addons --json" "код $rc, reason $reason"
fi
run addonver evo k8s addon-versions cert-manager --json
if [ "$rc" = 0 ] && jq -e '(.releases | length) > 0' "$tmp/out" >/dev/null; then
	ok "k8s addon-versions cert-manager --json" "версий: $(j -r '.releases | length' "$tmp/out"), со схемой: $(j -r '[.releases[] | select(.has_schema)] | length' "$tmp/out"), latest: $(j -r 'if .latest then "есть" else "нет" end' "$tmp/out")"
else
	warn "k8s addon-versions cert-manager --json" "код $rc, reason $reason"
fi
run addonnf evo k8s addon-versions no-such-addon-evo --json
if [ "$rc" = 4 ]; then ok "k8s addon-versions <нет такого> → 4" "reason $reason"; else warn "k8s addon-versions <нет такого> → 4" "код $rc, reason $reason"; fi

# Витрина: образы общего каталога и плагины mk8s одним списком. Печатаются
# только счётчики по видам; оба каталога должны прочитаться (warnings пуст).
run market evo marketplace list --json
if [ "$rc" = 0 ] && jq -e '(.items | length) == .shown and .counts["vm-os"] > 0 and .counts["k8s-addon"] > 0 and ((.warnings // []) | length) == 0' "$tmp/out" >/dev/null; then
	ok "marketplace list --json" "$(j -r '"vm-os: \(.counts["vm-os"]), vm-app: \(.counts["vm-app"]), k8s-addon: \(.counts["k8s-addon"]), скрыто: \(.hidden)"' "$tmp/out")"
else
	bad "marketplace list --json" "код $rc, reason $reason, предупреждений: $(j -r '(.warnings // []) | length' "$tmp/out")"
fi

echo "== Инструменты только для чтения"
# Без аргументов вызываются все, кроме читающих локальный state/каталог
# (status, list, logs) и пишущих план на диск (deploy_plan, k8s_plan).
# Обязательные — то, без чего не работает deploy; остальные зависят от
# сервисов, подключённых в проекте.
required=" list_zones list_images list_flavors list_disk_types list_subnets list_security_groups list_floating_ips vm_list disk_list "
skip=" status list logs deploy_plan k8s_plan "
total=0
passed=0
called=" "
for t in $(evo tools list --json | j -r '.[] | select(.mutating == false) | .name'); do
	case "$skip" in *" $t "*) continue ;; esac
	if ! evo tools describe "$t" --json | jq -e '((.inputSchema.required // []) | length) == 0' >/dev/null; then
		continue
	fi
	total=$((total + 1))
	called="$called$t "
	run "$t" timeout 120 evo tools call "$t" --json
	if [ "$rc" = 0 ]; then
		passed=$((passed + 1))
		ok "tools call $t" ""
	elif [[ $required == *" $t "* ]]; then
		bad "tools call $t" "код $rc, reason $reason"
	else
		warn "tools call $t" "код $rc, reason $reason"
	fi
done
# Пустой или упавший tools list не должен молча пропустить обязательные.
for t in $required; do
	case "$called" in *" $t "*) ;; *) bad "tools call $t" "не вызван: нет в tools list" ;; esac
done
echo "прошли $passed из $total"
echo "| **инструменты только для чтения** | $passed / $total | |" >>"$summary"

echo "== Превью удаления несуществующего ресурса"
# Удаляющие инструменты без --yes (и никогда с ним: confirmed=true здесь не
# передаётся) по случайному UUID: превью сначала ищет ресурс (GET, у SNAT —
# список) и отказывает NOT_FOUND, код 4, до вопроса о подтверждении
# (release audit, R-04). Код 3 (confirmation_required) — гейт спросил бы
# согласие на удаление того, чего нет; код 0 — что-то «удалилось». NLB, VPC
# и compute на отсутствующий id отвечают 404; 403 вместо 404 известен только
# у Artifact Registry и Certificate Manager (реестр AR-2, CRT-2) — здесь он
# лишь предупреждение (сервис может быть недоступен проекту).
h=$(openssl rand -hex 16)
nf="${h:0:8}-${h:8:4}-4${h:13:3}-8${h:17:3}-${h:20:12}"
for t in lb_delete tg_delete vpc_delete snat_delete; do
	run "$t" timeout 120 evo tools call "$t" --arg id="$nf" --json
	case "$rc:${reason%%,*}" in
	4:NOT_FOUND) ok "$t <нет такого> → NOT_FOUND" "код 4" ;;
	0:* | 3:*) bad "$t <нет такого> → NOT_FOUND" "код $rc, reason $reason: превью не проверило, что ресурс есть" ;;
	*:API_ERROR) if [[ $reason == *"HTTP 403"* ]]; then
		warn "$t <нет такого> → NOT_FOUND" "код $rc, reason $reason"
	else
		bad "$t <нет такого> → NOT_FOUND" "код $rc, reason $reason"
	fi ;;
	*) bad "$t <нет такого> → NOT_FOUND" "код $rc, reason $reason" ;;
	esac
done

echo "== MCP по stdio"
init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"cloud-e2e","version":"1"}}}'
ntools=$(evo tools list --json | j length)
printf '%s\n' "$init" \
	'{"jsonrpc":"2.0","method":"notifications/initialized"}' \
	'{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
	'{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"vm_list","arguments":{}}}' \
	'{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"release_floating_ip","arguments":{"id":"00000000-0000-4000-8000-000000000000"}}}' |
	timeout 120 evo mcp >"$tmp/stdio" 2>/dev/null || true
resp() { jq -c --argjson id "$1" 'select(.id == $id)' "$tmp/stdio"; }
if [ "$(resp 1 | j -r '.result.serverInfo.name')" = evo ]; then ok "stdio: initialize" "$(resp 1 | j -r '.result.protocolVersion')"; else bad "stdio: initialize" ""; fi
n=$(resp 2 | j '.result.tools | length')
if [ "$n" = "$ntools" ]; then ok "stdio: tools/list" "инструментов: $n"; else bad "stdio: tools/list" "$n, а tools list — $ntools"; fi
if [ "$(resp 3 | j '.result.isError')" = false ]; then ok "stdio: vm_list" "живой ответ без ошибки"; else bad "stdio: vm_list" "$(resp 3 | j -r '.result.content[0].text | fromjson? | .reason // "-"')"; fi
st=$(resp 4 | j -r '.result.content[0].text | fromjson? | .status')
if [ "$st" = confirmation_required ]; then ok "stdio: гейт без confirmed" "release_floating_ip → $st"; else bad "stdio: гейт без confirmed" "status $st"; fi

echo "== MCP по HTTP"
token=$(openssl rand -hex 32)
MCP_BEARER_TOKEN=$token evo mcp --http 127.0.0.1:18080 --allow-project "$PROJECT" 2>"$tmp/http-a.log" &
MCP_BEARER_TOKEN=$token evo mcp --http 127.0.0.1:18081 --allow-project 00000000-0000-4000-8000-000000000000 2>"$tmp/http-b.log" &
for _ in $(seq 1 20); do
	curl -fs -o /dev/null http://127.0.0.1:18080/healthz && curl -fs -o /dev/null http://127.0.0.1:18081/healthz && break
	sleep 0.5
done

# http PORT TOKEN BODY [SESSION] — тело ответа (JSON или SSE) → JSON в stdout,
# код HTTP в $tmp/code, Mcp-Session-Id в $tmp/session.
http() {
	local args=(-s -D "$tmp/headers" -o "$tmp/body" -w '%{http_code}'
		-H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream')
	[ -z "$2" ] || args+=(-H "Authorization: Bearer $2")
	[ -z "${4:-}" ] || args+=(-H "Mcp-Session-Id: $4")
	curl "${args[@]}" -d "$3" "http://127.0.0.1:$1/mcp" >"$tmp/code" || echo 000 >"$tmp/code"
	awk 'tolower($1)=="mcp-session-id:"{print $2}' "$tmp/headers" | tr -d '\r' >"$tmp/session"
	if grep -q '^data: ' "$tmp/body"; then sed -n 's/^data: //p' "$tmp/body"; else cat "$tmp/body"; fi
}
call() { # PORT SESSION ИМЯ АРГУМЕНТЫ — текст ответа инструмента и isError
	http "$1" "$token" "{\"jsonrpc\":\"2.0\",\"id\":9,\"method\":\"tools/call\",\"params\":{\"name\":\"$3\",\"arguments\":$4}}" "$2"
}

http 18080 "" "$init" >/dev/null
if [ "$(cat "$tmp/code")" = 401 ]; then ok "http: без токена → 401" ""; else bad "http: без токена → 401" "код $(cat "$tmp/code")"; fi
http 18080 "wrong-$token" "$init" >/dev/null
if [ "$(cat "$tmp/code")" = 401 ]; then ok "http: чужой токен → 401" ""; else bad "http: чужой токен → 401" "код $(cat "$tmp/code")"; fi

http 18080 "$token" "$init" >"$tmp/r"
sa=$(cat "$tmp/session")
if [ "$(j -r '.result.serverInfo.name' "$tmp/r")" = evo ] && [ -n "$sa" ]; then ok "http: initialize" "сессия выдана"; else bad "http: initialize" "код $(cat "$tmp/code")"; fi
http 18080 "$token" '{"jsonrpc":"2.0","method":"notifications/initialized"}' "$sa" >/dev/null
http 18080 "$token" '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' "$sa" >"$tmp/r"
n=$(j '.result.tools | length' "$tmp/r")
if [ "$n" = "$ntools" ]; then ok "http: tools/list" "инструментов: $n"; else bad "http: tools/list" "$n, а tools list — $ntools"; fi

call 18080 "$sa" vm_list '{}' >"$tmp/r"
if [ "$(j '.result.isError' "$tmp/r")" = false ]; then ok "http: vm_list в разрешённом проекте" ""; else bad "http: vm_list в разрешённом проекте" "$(j -r '.result.content[0].text | fromjson? | .reason // "-"' "$tmp/r")"; fi

call 18080 "$sa" k8s_kubeconfig '{"cluster_id":"00000000-0000-4000-8000-000000000000"}' >"$tmp/r"
if [ "$(j '.result.isError' "$tmp/r")" = true ] && jq -r '.result.content[0].text' "$tmp/r" | grep -qi 'allow-secret-export'; then
	ok "http: k8s_kubeconfig отключён" ""
else
	bad "http: k8s_kubeconfig отключён" "ответ без отказа по --allow-secret-export"
fi

call 18080 "$sa" k8s_kubectl '{"cluster_id":"00000000-0000-4000-8000-000000000000","args":"get secret x --template={{.data}}"}' >"$tmp/r"
# Отказ политики HTTP (QA r3 M1), а не ошибка kubectl на несуществующем кластере.
if j -r '.result.content[0].text' "$tmp/r" | grep -q 'disabled over HTTP'; then
	ok "http: kubectl get secret --template отклонён" "политикой HTTP"
else
	bad "http: kubectl get secret --template отклонён" "нет отказа политики"
fi

http 18081 "$token" "$init" >/dev/null
sb=$(cat "$tmp/session")
http 18081 "$token" '{"jsonrpc":"2.0","method":"notifications/initialized"}' "$sb" >/dev/null
call 18081 "$sb" vm_list '{}' >"$tmp/r"
r=$(j -r '.result.content[0].text | fromjson? | .reason // "-"' "$tmp/r")
if [ "$(j '.result.isError' "$tmp/r")" = true ] && [ "$r" = CONFIG_INVALID ]; then
	ok "http: --allow-project чужого проекта" "vm_list отклонён ($r)"
else
	bad "http: --allow-project чужого проекта" "isError $(j '.result.isError' "$tmp/r"), reason $r"
fi

# Журнал сервера: одна строка на вызов, аргументы — никогда.
if grep -q 'cluster_id\|{{.data}}' "$tmp/http-a.log"; then bad "http: журнал без аргументов" "аргументы в stderr сервера"; else ok "http: журнал без аргументов" "строк: $(wc -l <"$tmp/http-a.log")"; fi

exit "$failed"
