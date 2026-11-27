#!/usr/bin/env bash
# П-68: маршрут в общей сети и правила с теми же параметрами принадлежат
# разным запускам. Проверяем адресата каждого удаления без доступа к облаку.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
finish_test() {
	local rc=$?
	if [ "$rc" -ne 0 ] && [ -f "${FAKE_DIR:-}/out" ]; then cat "$FAKE_DIR/out" >&2; fi
	rm -rf "$work"
}
trap finish_test EXIT
# shellcheck source-path=SCRIPTDIR source=fake_evo.sh
. "$here/fake_evo.sh"
fake_evo_install "$work/bin"
export PATH="$work/bin:$PATH" RUN=r5a1 KEY_ID=test SECRET=test PROJECT=test
export GITHUB_STEP_SUMMARY=/dev/null SWEEP_PAUSE=0 API_RETRY_PAUSE=0
prefix=evo-e2e-net-$RUN

world() {
	export FAKE_DIR="$work/$1" RUNNER_TEMP="$work/$1"
	mkdir "$FAKE_DIR"
	: >"$FAKE_DIR/actions"
	touch "$FAKE_DIR/foreign-rule" "$FAKE_DIR/old-rule"
	for tool in lb_list tg_list snat_list list_floating_ips disk_list list_subnets; do
		fake_evo_answer "$tool" 0 0 '{}'
	done
	# r5a10 начинается так же, как r5a1, но это другая попытка запуска.
	fake_evo_answer vm_list 0 0 '{"vms":[{"id":"other-vm","name":"evo-e2e-net-r5a10-a"}]}'
	fake_evo_answer list_zones 0 0 '{"zones":[]}'
	fake_evo_answer vpc_list 0 0 '{"vpcs":[{"id":"shared-vpc"},{"id":"another-vpc"}]}'
	fake_evo_answer vpc_route_create 0 0 '{"status":"creating","operation_id":"create-op","route_id":"own-route"}'
	fake_evo_answer vpc_operation 0 0 '{"done":true,"resourceId":"own-route"}'
	fake_evo_answer vpc_route_delete 0 0 '{"status":"deleting","operation_id":"delete-op"}'
}

# Журналируем аргументы изменений рядом с ответами общей заглушки.
# «Облако» даёт одинаковую подсеть и в том же, и в другом VPC; собственный
# маршрут появляется после создания и исчезает только после его удаления по ID.
evo() {
	local group answer
	case " $* " in
	*' vpc_route_create '*)
		echo "$*" >>"$FAKE_DIR/route-creates"
		if command evo "$@"; then
			if [ ! -f "$FAKE_DIR/route-create-no-create" ]; then
				touch "$FAKE_DIR/own-route"
			fi
			return 0
		fi
		return $?
		;;
	*' vpc_routes '*)
		local routes='[{"id":"foreign-route","subnet":"198.18.255.0/24"}]'
		case " $* " in
		*' vpc_id=another-vpc '*) routes='[{"id":"other-vpc-route","subnet":"198.18.255.0/24"}]' ;;
		*' vpc_id=shared-vpc '*)
			if [ -e "$FAKE_DIR/own-route" ]; then
				routes=${routes%]}',{"id":"own-route","subnet":"198.18.255.0/24"}]'
			fi ;;
		esac
		printf '{"routes":%s}\n' "$routes" >"$FAKE_DIR/vpc_routes.0.json"
		;;
	*' list_security_groups '*)
		echo "project-wide: $*" >>"$FAKE_DIR/actions"
		jq -nc --argjson foreign "$(rule_page foreign)" --argjson old "$(rule_page old)" \
			'{security_groups:[{id:"foreign-sg",name:"shared",rules:$foreign.items},
				{id:"old-sg",name:"evo-e2e-net-r5a10-sg",rules:$old.items}]}' \
			>"$FAKE_DIR/list_security_groups.0.json"
		;;
	*' open_ports '* | *' close_ports '*)
		echo "$*" >>"$FAKE_DIR/actions"
		case " $* " in
		*' security_group=own-sg '*) group=own ;;
		*' security_group=foreign-sg '*) group=foreign ;;
		*' security_group=old-sg '*) group=old ;;
		*) return 1 ;;
		esac
		case " $* " in
		*' open_ports '*)
			if [ -e "$FAKE_DIR/$group-rule" ]; then answer='{"already_open":["18085"]}'; else answer='{"opened":["18085"]}'; fi
			touch "$FAKE_DIR/$group-rule"
			printf '%s\n' "$answer" >"$FAKE_DIR/open_ports.0.json"
			;;
		*' close_ports '*)
			if [ -e "$FAKE_DIR/$group-rule" ]; then answer='{"closed":["18085"]}'; else answer='{"status":"already_deleted"}'; fi
			case " $* " in *' --yes '*) rm -f "$FAKE_DIR/$group-rule" ;; esac
			printf '%s\n' "$answer" >"$FAKE_DIR/close_ports.0.json"
			;;
		esac
		;;
	*' vpc_route_delete '*)
		echo "$*" >>"$FAKE_DIR/actions"
		case " $* " in *' vpc_id=shared-vpc '*' route_id=own-route '*) rm -f "$FAKE_DIR/own-route" ;; esac
		;;
	*' destroy '*|*' sg_delete '*|*' sg_rule_delete '*) echo "$*" >>"$FAKE_DIR/actions" ;;
	esac
	command evo "$@"
}
# Одинаковые правила живут в трёх разных группах. Лишь одна из них
# принадлежит этому прогону, остальные должны пережить все проверки.
rule_page() {
	local items='[]'
	if [ -e "$FAKE_DIR/$1-rule" ]; then
		items=$(jq -nc --arg id "$1-rule" '[{id:$id, direction:"ingress", ip_protocol:"tcp", port_range:"18085:18085",
			remote_ip_prefix:"198.18.255.1/32", description:"opened via open_ports"}]')
	fi
	if [ "$1" = own ] && [ -f "$FAKE_DIR/own-sg" ]; then
		# Платформа создаёт исходящее правило вместе с группой. В ответе POST
		# есть только счётчик: ID становится известен из первого GET правил.
		items=$(jq -c '. + [{id:"initial-egress",direction:"egress"}]' <<<"$items")
		if [ -f "$FAKE_DIR/foreign-in-sg" ]; then
			items=$(jq -c '. + [{id:"added-by-other",direction:"ingress",ip_protocol:"tcp",
				port_range:"18085:18085",remote_ip_prefix:"198.18.255.1/32"}]' <<<"$items")
		fi
	fi
	if [ -f "$FAKE_DIR/incomplete-page" ]; then
		jq -c '{items:.,total:(length + 1)}' <<<"$items"
	elif [ -f "$FAKE_DIR/invalid-total" ]; then
		jq -c '{items:.,total:false}' <<<"$items"
	else
		jq -c '{items:.,total:length}' <<<"$items"
	fi
}
# HTTP с фиксированным состоянием групп. Любое удаление, в том числе
# запрос, который вернул бы ошибку, остаётся в журнале для сравнения.
curl() {
	local method=GET out=/dev/stdout path='' body='{}' sent='' code=500
	while [ "$#" -gt 0 ]; do
		case $1 in
		-X) method=$2; shift ;;
		-o) out=$2; shift ;;
		https://*) path=${1#*/api/v1} ;;
		@-) sent=$(cat) ;;
		esac
		shift
	done
	case "$method $path" in
	'POST /auth/token') echo '{"access_token":"fixture"}'; return ;;
	'POST /security-groups')
		jq -e '.name == "evo-e2e-net-r5a1-sg"' <<<"$sent" >/dev/null || return 1
		touch "$FAKE_DIR/own-sg"
		code=201 body='{"id":"own-sg","ingress_rules_count":0,"egress_rules_count":1}' ;;
	'GET /security-groups/own-sg')
		if [ -f "$FAKE_DIR/own-sg" ]; then
			code=200 body='{"id":"own-sg","name":"evo-e2e-net-r5a1-sg","state":"created","has_interfaces":false}'
		else code=404; fi ;;
	'GET /security-groups/own-sg/rules?offset=0&limit=100') code=200 body=$(rule_page own) ;;
	*'/security-groups/foreign-sg/'* | *'/security-groups/old-sg/'* | 'GET /security-groups?'*)
		echo "outside current group: $method $path" >>"$FAKE_DIR/actions"
		case "$method $path" in
		'GET /security-groups/foreign-sg/rules?'*) code=200 body=$(rule_page foreign) ;;
		'GET /security-groups/old-sg/rules?'*) code=200 body=$(rule_page old) ;;
		esac ;;
	'DELETE /security-groups/own-sg') rm -f "$FAKE_DIR/own-sg" "$FAKE_DIR/own-rule"; code=204 ;;
	esac
	[ "$method" != DELETE ] || echo "$method $path" >>"$FAKE_DIR/actions"
	printf '%s' "$body" >"$out"
	printf '%s' "$code"
}
export -f evo curl rule_page

# Настоящие функции и EXIT trap cloud-live.sh; заменён только выбор
# большой области, чтобы проверить один шаг без моделирования ВМ и балансировщика.
net_steps() {
	local trimmed="$work/cloud-live-trimmed.sh"
	sed '/^case "\$area" in$/,$d' "$here/cloud-live.sh" >"$trimmed"
	CLOUD_LIVE_TRIMMED=$trimmed bash -c '
		# shellcheck disable=SC1090
		. "$CLOUD_LIVE_TRIMMED"
		shift
		if [ "$1" = interrupted ]; then
			# Исходный процесс остановился до его EXIT trap. Постоянные квитанции
			# в RUNNER_TEMP останутся для отдельного workflow-шага always().
			trap '\''rm -rf "$tmp"'\'' EXIT
		fi
		shift
		"$@"
	' "$here/cloud-live.sh" net "$@"
}

check_actions() {
	local expected=$1
	[ -f "$FAKE_DIR/foreign-rule" ] && [ -f "$FAKE_DIR/old-rule" ] || { echo 'удалено чужое правило'; exit 1; }
	if ! diff -u <(printf '%s' "$expected" | sort) <(sort "$FAKE_DIR/actions"); then
		exit 1
	fi
}
own_route_delete='--yes tools call vpc_route_delete --arg vpc_id=shared-vpc --arg route_id=own-route --json'
own_group_delete='DELETE /security-groups/own-sg'
create_receipts() {
	net_create_group test-zone
	# В выделенной группе появился результат open_ports; такие же правила
	# уже существуют в чужой группе и в группе прежнего запуска.
	touch "$FAKE_DIR/own-rule" "$FAKE_DIR/own-route"
	net_record_opened_rule
	record_route_receipt shared-vpc create-op own-route
}
export -f create_receipts

# Штатный сценарий проверяет обе команды на собственной группе; код выхода
# и журнал также подтверждают, что EXIT убрал именно эту группу.
world ports
net_steps normal net_ports test-zone >"$FAKE_DIR/out" 2>&1
port_args='--arg security_group=own-sg --arg ports=[18085] --arg cidr=198.18.255.1/32 --json'
check_actions "$(printf '%s\n' "--yes tools call open_ports $port_args" "--yes tools call open_ports $port_args" \
	"--yes tools call close_ports $port_args" "--dry-run tools call close_ports $port_args" "$own_group_delete")"$'\n'

# Штатный шаг: после успешного создания в списке перед собственным стоит
# чужой маршрут в том же VPC с той же подсетью. Проверяем точный ID удаления.
world route
net_steps normal route_steps shared-vpc vm-owned iface-owned >"$FAKE_DIR/out" 2>&1
check_actions "$own_route_delete"$'\n'

# Потерянный ответ POST: инструмент может вернуть ID, найденный по подсети.
# Без операции создания такое совпадение не даёт права на удаление.
world lost
fake_evo_answer vpc_route_create 0 0 '{"status":"created","route_id":"foreign-route"}'
if net_steps normal route_steps shared-vpc vm-owned iface-owned >"$FAKE_DIR/out" 2>&1; then
	echo 'ждали отказ при непроверенной принадлежности маршрута'; exit 1
fi
check_actions ''

# Страховка другого шага CI, на том же runner. Старые группы, маршруты и
# соседняя попытка остаются на месте. Повтор уже завершённой уборки безопасен.
world sweep
net_steps interrupted create_receipts >"$FAKE_DIR/out" 2>&1
"$here/cloud-live.sh" sweep "$prefix" net >>"$FAKE_DIR/out" 2>&1
check_actions "$own_group_delete"$'\n'"$own_route_delete"$'\n'
: >"$FAKE_DIR/actions"
"$here/cloud-live.sh" sweep "$prefix" net >>"$FAKE_DIR/out" 2>&1
check_actions ''

# Отказ живой области до первого create тоже проходит через EXIT. В проекте
# есть совпадения; квитанций этого запуска нет, ни одного DELETE быть не должно.
world early
if "$here/cloud-live.sh" net >"$FAKE_DIR/out" 2>&1; then echo 'нет зоны: ждали отказ'; exit 1; fi
grep -Fq 'не найдена' "$FAKE_DIR/out"
check_actions ''

# Чужое правило появилось внутри собственной группы. Удаление группы целиком
# снесло бы и его: группа остаётся, уборка сообщает о незавершённости.
world mixed
net_steps interrupted net_create_group test-zone >"$FAKE_DIR/out" 2>&1
touch "$FAKE_DIR/foreign-in-sg"
if "$here/cloud-live.sh" sweep "$prefix" net >>"$FAKE_DIR/out" 2>&1; then
	echo 'ждали ошибку уборки группы с чужим правилом'; exit 1
fi
check_actions ''

# Неполный список и неверный тип счётчика не позволяют удалить группу.
for pagination_error in incomplete-page invalid-total; do
	world "$pagination_error"
	net_steps interrupted net_create_group test-zone >"$FAKE_DIR/out" 2>&1
	touch "$FAKE_DIR/$pagination_error"
	if "$here/cloud-live.sh" sweep "$prefix" net >>"$FAKE_DIR/out" 2>&1; then
		echo "ждали ошибку пагинации: $pagination_error"; exit 1
	fi
	check_actions ''
done
# Подтверждённый отказ VPC API HTTP 400 с префиксом инструмента (evo tools call):
# квитанция очищается, второй CIDR пробуется и проходит; после подтверждения
# удаления маршрута квитанция закрывается, чтобы удаление VPC прогона не давало 404 в sweep.
world route-400-then-ok
fake_evo_answer vpc_route_create 0 5 '{"error":{"reason":"API_ERROR","message":"vpc_route_create: create route: vpc API 400: invalid subnet"}}'
fake_evo_answer vpc_route_create 1 0 '{"status":"creating","operation_id":"create-op","route_id":"own-route"}'
net_steps normal route_steps shared-vpc vm-owned iface-owned >"$FAKE_DIR/out" 2>&1
[ "$(wc -l <"$FAKE_DIR/route-creates")" -eq 2 ] || { echo 'ждали 2 попытки после подтверждённого 400'; exit 1; }
[ ! -e "$RUNNER_TEMP/$prefix-route.json" ] || { echo 'квитанция маршрута должна быть закрыта после подтверждённого удаления'; exit 1; }
grep -Fq 'подтверждённый отказ сервиса' "$FAKE_DIR/out"
grep -Fq 'invalid subnet' "$FAKE_DIR/out"
check_actions "$own_route_delete"$'\n'
# Имитируем уже удалённый VPC прогона (404 на vpc_routes): sweep не должен падать.
fake_evo_answer vpc_routes 0 5 '{"error":{"reason":"NOT_FOUND","message":"vpc_routes: vpc API 404: NotFound"}}'
: >"$FAKE_DIR/actions"
"$here/cloud-live.sh" sweep "$prefix" net >>"$FAKE_DIR/out" 2>&1
check_actions ''

# Подтверждённый HTTP 400 на обе тестовые сети: квитанция очищена, sweep проходит чисто.
world route-400-both
touch "$FAKE_DIR/route-create-no-create"
fake_evo_answer vpc_route_create 0 5 '{"error":{"reason":"API_ERROR","message":"vpc_route_create: create route: vpc API 400: invalid field vpcId"}}'
net_steps normal route_steps shared-vpc vm-owned iface-owned >"$FAKE_DIR/out" 2>&1
[ "$(wc -l <"$FAKE_DIR/route-creates")" -eq 2 ] || { echo 'ждали проверку обеих подсетей при 400'; exit 1; }
[ ! -e "$RUNNER_TEMP/$prefix-route.json" ] || { echo 'квитанция должна быть удалена после подтверждённого 400'; exit 1; }
grep -Fq 'invalid field vpcId' "$FAKE_DIR/out"
"$here/cloud-live.sh" sweep "$prefix" net >>"$FAKE_DIR/out" 2>&1
check_actions ''

# Неопределённый исход (500, 408, 429, потерянный ответ с warning, обрыв сети):
# повторный POST запрещён (ровно 1 вызов), квитанция остаётся для диагностики sweep.
for kind in 500 408 429 lostwarn unreachable; do
	world "route-uncertain-$kind"
	touch "$FAKE_DIR/route-create-no-create"
	case "$kind" in
	500) err='{"error":{"reason":"API_ERROR","message":"vpc_route_create: create route: vpc API 500: internal error"}}' ;;
	408) err='{"error":{"reason":"API_ERROR","message":"vpc_route_create: create route: vpc API 408: request timeout"}}' ;;
	429) err='{"error":{"reason":"API_RATE_LIMITED","message":"vpc_route_create: create route: vpc API 429: too many requests"}}' ;;
	lostwarn) err='{"error":{"reason":"API_ERROR","message":"vpc_route_create: create route: vpc API 400: proxy","details":{"warning":"the static route may have been made"}}}' ;;
	unreachable) err='{"error":{"reason":"API_UNREACHABLE","message":"vpc_route_create: create route: vpc request failed: EOF"}}' ;;
	esac
	fake_evo_answer vpc_route_create 0 5 "$err"
	if net_steps normal route_steps shared-vpc vm-owned iface-owned >"$FAKE_DIR/out" 2>&1; then
		echo "ждали отказ при неопределённом исходе: $kind"; exit 1
	fi
	[ "$(wc -l <"$FAKE_DIR/route-creates")" -eq 1 ] || { echo "повторный POST при неопределённом исходе: $kind"; exit 1; }
	[ -e "$RUNNER_TEMP/$prefix-route.json" ] || { echo "квитанция пропала при неопределённом исходе: $kind"; exit 1; }
	grep -Fq 'исход запроса неизвестен; повтор не отправлен' "$FAKE_DIR/out"
	check_actions ''
done

# Отказ чтения списка до POST: повтор не нужен, пустая квитанция убирается.
world route-prepost
touch "$FAKE_DIR/route-create-no-create"
fake_evo_answer vpc_route_create 0 5 '{"error":{"reason":"API_ERROR","message":"vpc_route_create: check the destination is free: list routes: vpc API 500: unavailable"}}'
if net_steps normal route_steps shared-vpc vm-owned iface-owned >"$FAKE_DIR/out" 2>&1; then
	echo 'ждали отказ при ошибке чтения списка до POST'; exit 1
fi
[ "$(wc -l <"$FAKE_DIR/route-creates")" -eq 1 ] || { echo 'ждали ровно 1 вызов при ошибке чтения списка до POST'; exit 1; }
[ ! -e "$RUNNER_TEMP/$prefix-route.json" ] || { echo 'пустая квитанция осталась после ошибки до POST'; exit 1; }
grep -Fq 'до POST не прочитан список маршрутов' "$FAKE_DIR/out"
check_actions ''

echo 'cloud-live net cleanup: ok'
