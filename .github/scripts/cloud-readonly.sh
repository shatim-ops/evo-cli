#!/usr/bin/env bash
# Проверки на настоящем Cloud.ru без платных ресурсов: только чтения API
# через CLI, все инструменты только для чтения без обязательных аргументов,
# MCP по stdio и по HTTP (--allow-project, отказ без токена; гейт
# подтверждения на настоящем ресурсе — cloud-live.sh). Запускается job'ом
# readonly в cloud-e2e.yml (Linux) и в
# cloud-readonly-os.yml (Windows, macOS) после evo configure.
#
# Репозиторий публичный, лог — тоже: печатаются только имена, коды выхода,
# reason и счётчики, ни одного ответа API целиком. PROJECT (секрет) GitHub
# маскирует сам, но и его скрипт не печатает.
#
# Код выхода: 0 — все обязательные проверки прошли; 1 — нет. Необязательные
# (сервисы, которых может не быть в проекте) только попадают в таблицу.
set -euo pipefail

: "${PROJECT:?нужен PROJECT — UUID тестового проекта}"
# На macOS нет timeout из coreutils; на Windows в PATH бывает timeout.exe
# системы с другим синтаксисом. Тогда — тот же предел через perl (он есть
# и на macOS, и в Git для Windows).
if ! timeout 5 true 2>/dev/null; then
	timeout() {
		local limit=$1
		shift
		perl -e 'alarm shift; exec @ARGV or die "exec: $!"' "$limit" "$@"
	}
fi
# jq для Windows пишет CRLF: \r в именах инструментов и в сравнениях.
case "$(uname -s)" in
MINGW* | MSYS* | CYGWIN*) jq() { command jq -b "$@"; } ;;
esac
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
skip() { row "$1" skip "${2:-}"; }
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
# список) и отвечает, что удалять нечего, до вопроса о подтверждении: код 0,
# status already_deleted, existed false (D26: повторное удаление — тот же
# итог, что первое; раньше — NOT_FOUND, release audit R-04). Код 3
# (confirmation_required) — гейт спросил бы согласие на удаление того, чего
# нет. NLB, VPC и compute на отсутствующий id отвечают 404; 403 вместо 404
# известен только у Artifact Registry и Certificate Manager (реестр AR-2,
# CRT-2) — здесь он лишь предупреждение (сервис может быть недоступен
# проекту).
h=$(openssl rand -hex 16)
nf="${h:0:8}-${h:8:4}-4${h:13:3}-8${h:17:3}-${h:20:12}"
for t in lb_delete tg_delete vpc_delete snat_delete; do
	run "$t" timeout 120 evo tools call "$t" --arg id="$nf" --json
	gone=$(j -r 'select(.status == "already_deleted" and .existed == false) | .status' "$tmp/out")
	case "$rc:${reason%%,*}" in
	0:-) if [ "$gone" = already_deleted ]; then
		ok "$t <нет такого> → already_deleted" "код 0, existed false"
	else
		bad "$t <нет такого> → already_deleted" "код 0 без already_deleted и existed false: что-то «удалилось»?"
	fi ;;
	3:*) bad "$t <нет такого> → already_deleted" "код 3: гейт спросил согласие на удаление того, чего нет" ;;
	*:API_ERROR) if [[ $reason == *"HTTP 403"* ]]; then
		warn "$t <нет такого> → already_deleted" "код $rc, reason $reason"
	else
		bad "$t <нет такого> → already_deleted" "код $rc, reason $reason"
	fi ;;
	*) bad "$t <нет такого> → already_deleted" "код $rc, reason $reason" ;;
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
# Изменяющий вызов без confirmed по тому, чего нет: ответ — already_deleted
# до вопроса о согласии, как у команды (D26). Гейт по stdio на настоящем
# ресурсе (deploy_apply и destroy без confirmed — ничего не изменилось)
# проверяет cloud-live.sh: здесь ресурса нет, а согласие спрашивают только
# про то, что вызов действительно изменит.
st=$(resp 4 | j -r '.result.content[0].text | fromjson? | select(.existed == false) | .status')
if [ "$st" = already_deleted ]; then ok "stdio: release_floating_ip <нет такого>" "already_deleted, existed false"; else bad "stdio: release_floating_ip <нет такого>" "status $st"; fi

echo "== MCP по HTTP"
# D45 (1.3.1): токен короче 32 символов на адресе, который видит сеть, —
# сервер не стартует (CONFIG_INVALID, код 2) и порт не слушается; на
# loopback — старт с предупреждением. short — не секрет: тестовая строка.
short='short-ci-token'
set +e
MCP_BEARER_TOKEN=$short timeout 10 evo mcp --http 0.0.0.0:18082 --allow-project "$PROJECT" >/dev/null 2>"$tmp/http-short.log"
rc=$?
set -e
if [ "$rc" = 2 ] && ! curl -fs -o /dev/null --max-time 2 http://127.0.0.1:18082/healthz; then
	ok "http: короткий токен на 0.0.0.0" "не стартует, код 2"
else
	bad "http: короткий токен на 0.0.0.0" "код $rc (ждём 2; 124 — сервер слушал до тайм-аута)"
fi
MCP_BEARER_TOKEN=$short evo mcp --http 127.0.0.1:18083 --allow-project "$PROJECT" 2>"$tmp/http-loop.log" &
loop_pid=$!
for _ in $(seq 1 20); do
	curl -fs -o /dev/null http://127.0.0.1:18083/healthz && break
	sleep 0.5
done
if curl -fs -o /dev/null http://127.0.0.1:18083/healthz && grep -q 'shorter than 32\|короче 32' "$tmp/http-loop.log"; then
	ok "http: короткий токен на loopback" "старт с предупреждением"
else
	bad "http: короткий токен на loopback" "сервер не поднялся или нет предупреждения"
fi
kill "$loop_pid" 2>/dev/null || true
wait "$loop_pid" 2>/dev/null || true

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

# Свой ресурс по id на сервере с --allow-project (REL12-3): перед вызовом
# сервер читает ресурс и сравнивает его проект (поле project_id или
# projectId ответа API) со списком флага. Имена полей взяты из
# спецификаций; если живой ответ их не содержит, *_get своего ресурса
# получит отказ («names no project») — здесь, а не у пользователя. Берётся
# первый ресурс из *_list проекта; пустой список — skip: между прогонами
# тестовый проект пуст, и проект ресурса проверяют живые прогоны сразу
# после создания (bound-check.sh). Печатаются только
# имена инструментов и reason, ни одного id.
treason() { j -r '.result.content[0].text | fromjson? | .reason // "-"' "$tmp/r"; }
# firstid [ПОЛЕ] — поле (по умолчанию id) первого элемента первого
# непустого массива в ответе инструмента.
# shellcheck disable=SC2016 # $f — переменная jq, не shell
firstid() { j -r --arg f "${1:-id}" '.result.content[0].text | fromjson? | [.[]? | arrays | .[]? | objects | .[$f]? // empty | strings] | first // empty' "$tmp/r"; }
# owned ИНСТРУМЕНТ АРГУМЕНТ ID [ЛИШНЕЕ] — вызов *_get своего ресурса.
owned() {
	call 18080 "$sa" "$1" "{\"$2\":\"$3\"${4:-}}" >"$tmp/r"
	if [ "$(j '.result.isError' "$tmp/r")" = false ]; then
		ok "http: $1 своего ресурса" "проект ресурса разрешён"
	else
		bad "http: $1 своего ресурса" "reason $(treason)"
	fi
}
# listed СПИСОК АРГУМЕНТЫ — первый id из списка в $rid; пустой — skip,
# список недоступен (сервиса нет в проекте) — warn.
listed() {
	rid=
	call 18080 "$sa" "$1" "$2" >"$tmp/r"
	if [ "$(j '.result.isError' "$tmp/r")" != false ]; then
		warn "http: $1 → *_get своего ресурса" "$1: reason $(treason)"
		return 1
	fi
	rid=$(firstid)
	if [ -z "$rid" ]; then
		skip "http: $1 → *_get своего ресурса" "ресурсов нет — проверяется в живых прогонах (bound-check.sh)"
		return 1
	fi
}
for pair in pg_list:pg_get:id k8s_list:k8s_get:cluster_id vpc_list:vpc_get:id cert_list:cert_get:id lb_list:lb_get:id tg_list:tg_get:id; do
	IFS=: read -r list get arg <<<"$pair"
	if listed "$list" '{}'; then owned "$get" "$arg" "$rid"; fi
done
# Образ по UUID: свой (private) — проверка поля project_id; без своих —
# первый из каталога (ветка общего образа).
call 18080 "$sa" image_list '{"type":"private"}' >"$tmp/r"
rid=$(firstid)
if [ -n "$rid" ] || listed image_list '{}'; then owned image_get image_id "$rid"; fi
# Зона DNS и её запись: видимость — из dns_zones.
if listed dns_zones '{}'; then
	zone=$rid
	vis=$(firstid visibility)
	owned dns_zone_get id "$zone" ",\"type\":\"$vis\""
	if listed dns_records "{\"zone_id\":\"$zone\",\"type\":\"$vis\"}"; then
		owned dns_record_get record_id "$rid" ",\"type\":\"$vis\""
	fi
fi

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
if grep -q 'cluster_id\|{{.data}}' "$tmp/http-a.log"; then bad "http: журнал без аргументов" "аргументы в stderr сервера"; else ok "http: журнал без аргументов" "строк: $(wc -l <"$tmp/http-a.log" | tr -d ' ')"; fi

# E5 (1.4.0): форма ответа monitoring и audit (response_shape.sh). Не FAIL:
# сменившаяся форма — поведение платформы, строка реестра расхождений, а
# не дефект evo. Нужны KEY_ID и SECRET шага.
# shellcheck source-path=SCRIPTDIR source=response_shape.sh
. "$(dirname "$0")/response_shape.sh"
iam=
if [ -n "${KEY_ID:-}" ] && [ -n "${SECRET:-}" ]; then
	iam=$(jq -n --arg k "$KEY_ID" --arg s "$SECRET" '{keyId:$k, secret:$s}' |
		curl -sS --max-time 30 -X POST https://iam.api.cloud.ru/api/v1/auth/token -H 'Content-Type: application/json' --data-binary @- 2>/dev/null |
		j -r '.access_token // .accessToken // empty')
fi
if [ -n "$iam" ]; then
	[ -z "${GITHUB_ACTIONS:-}" ] || echo "::add-mask::$iam"
	for svc in monitoring audit; do
		case "$svc" in
		monitoring) url="https://monitoring.api.cloud.ru/v1/$PROJECT/export_metrics" ;;
		audit) url="https://audit.api.cloud.ru/events/list/$PROJECT" ;;
		esac
		IFS=$'\t' read -r verdict details < <(response_shape "$url" "$iam")
		if [ "$verdict" = ok ]; then ok "форма ответа: $svc" "$details"; else warn "форма ответа: $svc" "$details"; fi
	done
else
	skip "форма ответа: monitoring, audit" "нет KEY_ID и SECRET шага или токена IAM"
fi

exit "$failed"
