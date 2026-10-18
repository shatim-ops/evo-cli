#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
# SPDX-FileContributor: Тимур Шакирбеков
# SPDX-License-Identifier: Apache-2.0
#
# Живые прогоны изменяющих инструментов evo на настоящем Cloud.ru, по одной
# области за вызов: cloud-live.sh ОБЛАСТЬ. Области: disk, net, dns, s3,
# secrets, ca, stack, mcp — их вызывает job live в cloud-e2e.yml после evo
# configure; sweep ПРЕФИКС [ОБЛАСТЬ] — удалить ресурсы с именами ПРЕФИКС*
# всех видов этого скрипта или только видов области (job cleanup, префикс
# evo-e2e-; страховка job live).
#
# Каждая область создаёт ресурсы с именами evo-e2e-<область>-<run>, в конце
# (trap EXIT, и после провала тоже) удаляет всё со своим префиксом и
# проверяет, что ничего не осталось; остаток — провал с видом и именем.
#
# Лог публичный: ID и IP маскируются (::add-mask::), ответы API целиком не
# печатаются — только статусы, reason, коды HTTP и счётчики; текст ошибок —
# через redact.sed и обрезанным. Значения секретов сравниваются по sha256 и
# в лог не попадают.
#
# Окружение: ZONE (имя зоны), IMAGE, FLAVOR, RUN (уникальный суффикс, по
# умолчанию GITHUB_RUN_ID); для правил групп безопасности — KEY_ID, SECRET
# (в evo нет удаления правила, его удаляет API напрямую, как job cleanup).
#
# Код выхода: 0 — все проверки области прошли и остатков нет; 1 — нет.
# A && ok || bad — ok всегда 0; фильтры jq — в одинарных кавычках; функции
# внутри областей вызываются через poll.
# shellcheck disable=SC2015,SC2016,SC2329
set -euo pipefail

area=${1:?нужна область: disk, net, dns, s3, secrets, ca, stack, mcp или sweep ПРЕФИКС}
here=$(cd "$(dirname "$0")" && pwd)
ZONE=${ZONE:-ru.AZ-2}
IMAGE=${IMAGE:-ubuntu-22.04}
FLAVOR=${FLAVOR:-lowcost10-2-4}
RUN=${RUN:-${GITHUB_RUN_ID:-local}}
RUN=$(printf '%s' "$RUN" | tr -cd 'a-z0-9' | cut -c1-16)
summary=${GITHUB_STEP_SUMMARY:-/dev/null}
tmp=$(mktemp -d)
failed=0
covered=()

# Тестовые сети и адреса: 198.18.0.0/15 — диапазон для тестов (RFC 2544),
# в интернете не маршрутизируется; 10.255.255.0/24 — запасная частная сеть
# для маршрута, если первую API не примет. Маршрут и правило никого не
# задевают.
ROUTE_CIDRS="198.18.255.0/24 10.255.255.0/24"
RULE_CIDR=198.18.255.1/32
RULE_PORT=18085
MCP_PORT=8086

redact() { sed -E -f "$here/redact.sed"; }
mask() {
	local v
	for v in "$@"; do
		[ -z "$v" ] || [ "$v" = null ] || echo "::add-mask::$v"
	done
}
row() { # проверка | итог | подробности
	printf '%-44s %-6s %s\n' "$1" "$2" "$3"
	printf '| %s | %s | %s | %s |\n' "$area" "$1" "$2" "$3" >>"$summary"
}
ok() { row "$1" ok "${2:-}"; }
bad() {
	row "$1" FAIL "${2:-}"
	echo "::error::$area: $1 — ${2:-}"
	failed=1
}
gap() { row "$1" "не покрыто" "${2:-}"; } # не проверено по причине вне evo
cov() { covered+=("$@"); }                  # изменяющий инструмент прошёл
# j — jq по ответу последнего вызова; ответ не JSON не обрывает скрипт.
j() { jq "$@" "$tmp/r.json" 2>/dev/null || true; }

# tc ИНСТРУМЕНТ АРГУМЕНТЫ... — evo --yes tools call … --json: ответ в
# $tmp/r.json, код в $rc, reason в $reason, текст ошибки (замаскированный,
# до 300 символов) в $why.
tc() {
	local name=$1 httpst
	shift
	set +e
	evo --yes tools call "$name" "$@" --json >"$tmp/r.json" 2>"$tmp/err"
	rc=$?
	set -e
	reason=$(j -r '.reason? // empty' | head -1)
	[ -n "$reason" ] || reason=$(jq -rR 'fromjson? | .error.reason? // empty' "$tmp/err" 2>/dev/null | head -1)
	[ -n "$reason" ] || reason=-
	why=$( (j -r '.error? // .message? // empty' | head -c 2000; jq -rR 'fromjson? | .error.message? // empty' "$tmp/err" 2>/dev/null | head -c 2000) | tr '\n' ' ' | redact | cut -c1-300)
	httpst=$(printf '%s' "$why" | grep -oE '(HTTP|status|статус|API)[ :]*[45][0-9]{2}' | grep -oE '[45][0-9]{2}' | head -1 || true)
	[ -z "$httpst" ] || reason="$reason, HTTP $httpst"
}
# must ПРОВЕРКА ИНСТРУМЕНТ АРГУМЕНТЫ... — tc; не 0 — bad и код 1.
must() {
	local label=$1
	shift
	tc "$@"
	[ "$rc" = 0 ] && return 0
	bad "$label" "код $rc, $reason: $why"
	return 1
}
# poll СЕКУНД ПАУЗА КОМАНДА... — повторять, пока КОМАНДА не вернёт 0.
poll() {
	local until=$(($(date +%s) + $1)) pause=$2
	shift 2
	while :; do
		"$@" && return 0
		[ "$(date +%s)" -lt "$until" ] || return 1
		sleep "$pause"
	done
}
# opwait ВИД OPERATION_ID СЕКУНД — ждать done=true операции <вид>_operation;
# ID ресурса — в $res_id. Код 1 — не дождались или операция с ошибкой.
opwait() {
	local kind=$1 op=$2 until=$(($(date +%s) + $3))
	res_id=
	while :; do
		tc "${kind}_operation" --arg operation_id="$op" --arg wait_seconds=120
		if [ "$rc" = 0 ] && [ "$(j -r .done)" = true ]; then
			res_id=$(j -r '.resourceId // .resource_id // empty')
			mask "$res_id"
			if [ "$(j -c '.error // null')" != null ]; then
				why=$(j -c '.error' | redact | cut -c1-300)
				return 1
			fi
			return 0
		fi
		[ "$(date +%s)" -lt "$until" ] || {
			why="операция не завершилась (код $rc, $reason)"
			return 1
		}
		sleep 10
	done
}
zone_id() {
	evo tools call list_zones --json | jq -r --arg n "$ZONE" '[.zones[]? | select(.name == $n) | .id][0] // empty'
}
# rand_secret — 24 случайных символа, буквы и цифры, сразу под маской.
rand_secret() {
	local v
	v=$(openssl rand -base64 48 | tr -dc 'A-Za-z0-9' | cut -c1-24)
	mask "$v"
	printf '%s' "$v"
}
sha() { printf '%s' "$1" | sha256sum | cut -c1-64; }
# gone ВИД ПРЕФИКС — ресурсов вида с таким именем нет (для poll: список
# читается заново на каждой попытке).
gone() { [ -z "$(lst "$1" "$2")" ]; }

# --- Поиск и удаление ресурсов по префиксу имени ---------------------------
# lst ВИД ПРЕФИКС — строки «id<TAB>имя» ресурсов вида с именем ПРЕФИКС*.
# Список не прочитан — код 1 (вид пропускается, но не считается чистым).
lst() {
	local kind=$1 p=$2 out
	case "$kind" in
	ca) out=$(evo tools call ca_list --json 2>"$tmp/lst.err") &&
		jq -r --arg p "$p" '.containers[]? | select((.name // "") | startswith($p)) | "\(.name)\t\(.name)"' <<<"$out" ;;
	ar) out=$(evo tools call ar_list --json 2>"$tmp/lst.err") &&
		jq -r --arg p "$p" '.registries[]? | select((.name // "") | startswith($p)) | "\(.id)\t\(.name)"' <<<"$out" ;;
	lb) out=$(evo tools call lb_list --json 2>"$tmp/lst.err") &&
		jq -r --arg p "$p" '.load_balancers[]? | select((.name // "") | startswith($p)) | "\(.id)\t\(.name)"' <<<"$out" ;;
	tg) out=$(evo tools call tg_list --json 2>"$tmp/lst.err") &&
		jq -r --arg p "$p" '.target_groups[]? | select((.name // "") | startswith($p)) | "\(.id)\t\(.name)"' <<<"$out" ;;
	vm) out=$(evo tools call vm_list --json 2>"$tmp/lst.err") &&
		jq -r --arg p "$p" '.vms[]? | select((.name // "") | startswith($p)) | "\(.id)\t\(.name)"' <<<"$out" ;;
	snat) out=$(evo tools call snat_list --json 2>"$tmp/lst.err") &&
		jq -r --arg p "$p" '.nat_gateways[]? | select((.name // "") | startswith($p)) | "\(.id)\t\(.name)"' <<<"$out" ;;
	fip) out=$(evo tools call list_floating_ips --json 2>"$tmp/lst.err") &&
		jq -r --arg p "fip-$p" '.floating_ips[]? | select(((.name // "") | startswith($p)) and .state != "error_deleting") | "\(.id)\t\(.name)"' <<<"$out" ;;
	disk) out=$(evo tools call disk_list --json 2>"$tmp/lst.err") &&
		jq -r --arg p "$p" '.disks[]? | select(((.name // "") | startswith($p)) and (((.attached_to // []) | length) == 0)) | "\(.id)\t\(.name)"' <<<"$out" ;;
	image) out=$(evo tools call image_list --arg type=private --json 2>"$tmp/lst.err") &&
		jq -r --arg p "$p" '.images[]? | select((.name // "") | startswith($p)) | "\(.id)\t\(.name)"' <<<"$out" ;;
	route)
		# маршруты без имени: свои — по тестовому CIDR, в любом VPC
		local v
		out=$(evo tools call vpc_list --json 2>"$tmp/lst.err") || return 1
		for v in $(jq -r '.vpcs[]?.id' <<<"$out"); do
			evo tools call vpc_routes --arg vpc_id="$v" --json 2>/dev/null |
				jq -r --arg v "$v" --arg c "$ROUTE_CIDRS" '($c | split(" ")) as $cs | .routes[]? | select(.subnet as $n | $cs | index($n)) | "\($v) \(.id)\troute \(.subnet)"' || true
		done ;;
	vpc) out=$(evo tools call vpc_list --json 2>"$tmp/lst.err") &&
		jq -r --arg p "$p" '.vpcs[]? | select((.name // "") | startswith($p)) | "\(.id)\t\(.name)"' <<<"$out" ;;
	dns) out=$(evo tools call dns_zones --json 2>"$tmp/lst.err") &&
		# id — «видимость id»: удаление зоны идёт в API её видимости
		jq -r --arg p "$p" '.zones[]? | select(((.name // "") | startswith($p)) or ((.domain // "") | startswith($p))) | "\(.visibility // "public") \(.id)\t\(.domain // .name)"' <<<"$out" ;;
	s3) out=$(evo tools call s3_buckets --json 2>"$tmp/lst.err") &&
		jq -r --arg p "$p" '.buckets[]? | select((.name // "") | startswith($p)) | "\(.name)\t\(.name)"' <<<"$out" ;;
	secret) out=$(evo tools call secret_list --json 2>"$tmp/lst.err") &&
		jq -r --arg p "$p" '.secrets[]? | select(((.path // .name // "") | startswith($p))) | "\(.path // .name)\t\(.path // .name)"' <<<"$out" ;;
	cert) out=$(evo tools call cert_list --json 2>"$tmp/lst.err") &&
		jq -r --arg p "$p" '.certificates[]? | select((.name // .displayName // "") | startswith($p)) | "\(.id)\t\(.name // .displayName)"' <<<"$out" ;;
	*) return 1 ;;
	esac
}
# del ВИД ID — удалить один ресурс (ответ не печатается: в нём ID).
del() {
	local kind=$1 id=$2
	case "$kind" in
	ca) tc ca_delete --arg name="$id" ;;
	ar) tc ar_delete --arg id="$id" ;;
	lb) tc lb_delete --arg id="$id" ;;
	tg) tc tg_delete --arg id="$id" ;;
	vm) tc destroy --arg vm_id="$id" ;;
	snat) tc snat_delete --arg id="$id" ;;
	fip) tc release_floating_ip --arg id="$id" ;;
	disk) tc disk_delete --arg disk_id="$id" ;;
	image) tc image_delete --arg image_id="$id" ;;
	route) tc vpc_route_delete --arg vpc_id="${id%% *}" --arg route_id="${id#* }" ;;
	vpc) tc vpc_delete --arg id="$id" ;;
	dns) tc dns_zone_delete --arg type="${id%% *}" --arg id="${id#* }" ;;
	s3)
		local k
		evo tools call s3_ls --arg bucket="$id" --json 2>/dev/null | jq -r '.objects[]?.key' >"$tmp/keys" || true
		while read -r k; do [ -z "$k" ] || tc s3_rm --arg bucket="$id" --arg key="$k"; done <"$tmp/keys"
		tc s3_bucket_delete --arg bucket="$id" ;;
	secret) tc secret_delete --arg path="$id" ;;
	cert) tc cert_delete --arg id="$id" ;;
	esac
}
KINDS="ca ar lb tg vm snat fip disk image route vpc dns s3 secret cert"
# area_kinds ОБЛАСТЬ — виды ресурсов, которые область создаёт: её уборка
# читает только их (список одного вида бывает медленным: ar_list 18.10 —
# ~9,5 мин). Маршрут тестовой сети — только у net.
area_kinds() {
	case "$1" in
	disk) echo "vm fip disk image" ;;
	net) echo "lb tg vm snat fip route vpc" ;;
	dns) echo "dns" ;;
	s3) echo "s3" ;;
	secrets) echo "secret cert" ;;
	ca) echo "ca ar" ;;
	stack) echo "lb tg vm fip secret" ;;
	mcp) echo "vm fip" ;;
	*) echo "${KINDS/route /}" ;;
	esac
}
# sweep ПРЕФИКС [ВИДЫ] — удалять ресурсы ПРЕФИКС* до 20 мин (удаления
# асинхронны: балансировщик, ВМ, VPC), затем остаток — провал по каждому.
sweep() {
	local p=$1 kinds=${2:-$KINDS} kind id name left t0 until=$(($(date +%s) + 1200))
	case "$p" in evo-e2e-*) ;; *)
		echo "::error::префикс $p не evo-e2e-*: такие ресурсы скрипт не трогает"
		return 1 ;;
	esac
	while :; do
		left=0
		for kind in $kinds; do
			t0=$(date +%s)
			lst "$kind" "$p" >"$tmp/l" 2>/dev/null || {
				echo "$kind: список не прочитан за $(($(date +%s) - t0)) с: $(jq -rR 'fromjson? | .error | "\(.reason): \(.message)"' "$tmp/lst.err" 2>/dev/null | head -1 | redact | cut -c1-240)"
				continue
			}
			[ $(($(date +%s) - t0)) -lt 60 ] || echo "$kind: список читался $(($(date +%s) - t0)) с"
			while IFS=$'\t' read -r id name; do
				[ -n "$id" ] || continue
				mask "$id" "${id%% *}" "${id#* }"
				left=$((left + 1))
				del "$kind" "$id"
				echo "удаление $kind $name: код $rc${reason:+, $reason}"
			done <"$tmp/l"
		done
		[ "$left" = 0 ] && break
		[ "$(date +%s)" -lt "$until" ] || break
		sleep 30
	done
	left=0
	for kind in $kinds; do
		lst "$kind" "$p" >"$tmp/l" 2>/dev/null || continue
		while IFS=$'\t' read -r _ name; do
			[ -n "$name" ] || continue
			echo "::error::после прогона остался $kind $name — удалите его в консоли Cloud.ru"
			left=$((left + 1))
		done <"$tmp/l"
	done
	[ "$left" = 0 ] && echo "остатков $p* нет"
	[ "$left" = 0 ]
}

# iam_token — токен IAM по KEY_ID/SECRET (под маской) для прямых запросов.
iam_token() {
	local t
	[ -n "${KEY_ID:-}" ] && [ -n "${SECRET:-}" ] || return 1
	t=$(jq -n --arg k "$KEY_ID" --arg s "$SECRET" '{keyId:$k, secret:$s}' |
		curl -sS -X POST https://iam.api.cloud.ru/api/v1/auth/token -H 'Content-Type: application/json' --data-binary @- |
		jq -r '.access_token // .accessToken // empty')
	mask "$t"
	printf '%s' "$t"
}

# probe_ar_pages — как отвечает GET /v1/registries постранично (только
# чтение): по странице — HTTP, число реестров, длина токена, совпадает ли он
# с прошлым, хэш первого id (повторяется ли страница). Без ID и имён.
probe_ar_pages() {
	local token tok="" prev="" i code n first q
	token=$(iam_token) || { echo "probe ar: нет токена"; return 0; }
	for q in "" "&pageSize=100"; do
		tok=""
		prev=""
		for i in 1 2 3 4 5; do
			code=$(curl -sS -o "$tmp/arp" -w '%{http_code}' -H "Authorization: Bearer $token" \
				"https://ar.api.cloud.ru/v1/registries?projectId=${PROJECT:-}$q${tok:+&pageToken=$tok}" || echo 000)
			n=$(jq '(.registries // []) | length' "$tmp/arp" 2>/dev/null || echo "?")
			first=$(jq -r '(.registries // [])[0].id // ""' "$tmp/arp" 2>/dev/null | sha256sum | cut -c1-8)
			prev=$tok
			tok=$(jq -r '.nextPageToken // .next_page_token // empty' "$tmp/arp" 2>/dev/null)
			echo "probe ar${q:+ $q} стр.$i: http $code, реестров $n, ключи $(jq -c 'keys' "$tmp/arp" 2>/dev/null), токен ${#tok} симв., как прошлый: $([ -n "$tok" ] && [ "$tok" = "$prev" ] && echo да || echo нет), первый id #$first"
			[ -n "$tok" ] || break
		done
	done
}

# Правило группы безопасности на порт RULE_PORT из RULE_CIDR (open_ports):
# в evo нет удаления правила — DELETE в API, как шаг cleanup.
delete_test_rules() {
	local token sg r code n=0
	[ -n "${KEY_ID:-}" ] && [ -n "${SECRET:-}" ] || { echo "::warning::нет KEY_ID/SECRET — правило $RULE_PORT не удалено"; return 1; }
	evo tools call list_security_groups --arg rules=true --json >"$tmp/sg.json" || return 1
	jq -r --arg pr "$RULE_PORT:$RULE_PORT" --arg c "$RULE_CIDR" '.security_groups[] | .id as $g | .rules[]? | select(.direction == "ingress" and .port_range == $pr and .remote_ip_prefix == $c) | "\($g) \(.id)"' "$tmp/sg.json" >"$tmp/rules"
	[ -s "$tmp/rules" ] || return 0
	token=$(iam_token)
	[ -n "$token" ] || { echo "::warning::нет токена — правило $RULE_PORT не удалено"; return 1; }
	while read -r sg r; do
		mask "$sg" "$r"
		code=$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE -H "Authorization: Bearer $token" \
			"https://compute.api.cloud.ru/api/v1/security-groups/$sg/rules/$r")
		echo "правило $RULE_PORT: DELETE http=$code"
		n=$((n + 1))
	done <"$tmp/rules"
	return 0
}
rules_left() {
	evo tools call list_security_groups --arg rules=true --json |
		jq --arg pr "$RULE_PORT:$RULE_PORT" --arg c "$RULE_CIDR" '[.security_groups[] | .rules[]? | select(.direction == "ingress" and .port_range == $pr and .remote_ip_prefix == $c and (.state // "") != "deleting")] | length'
}

no_rules() { [ "$(rules_left)" = 0 ]; }

prefix="evo-e2e-$area-$RUN"
finish() {
	local rc_in=$?
	set +e
	if [ "$area" != sweep ]; then
		echo "== уборка $prefix*"
		[ "$area" != net ] || delete_test_rules
		sweep "$prefix" "$(area_kinds "$area")" || failed=1
		if [ "$area" = net ]; then
			if poll 120 10 no_rules; then ok "правило $RULE_PORT удалено"; else bad "правило $RULE_PORT удалено" "осталось после уборки"; fi
		fi
		echo "прошли изменяющие инструменты (${#covered[@]}): ${covered[*]:-}"
		printf '| %s | покрыто | %s | %s |\n' "$area" "${#covered[@]}" "${covered[*]:-—}" >>"$summary"
	fi
	rm -rf "$tmp"
	[ "$failed" = 0 ] && [ "$rc_in" = 0 ] || exit 1
	exit 0
}
trap finish EXIT

# --- Области ----------------------------------------------------------------

# ВМ, диски, образ из загрузочного диска.
area_disk() {
	local zid vm vm_ip user data boot img st=
	zid=$(zone_id)
	[ -n "$zid" ] || { bad "зона $ZONE" "не найдена"; return 1; }
	mkdir -p "$tmp/disk" && cd "$tmp/disk"
	vm="$prefix-vm"
	must provision provision --arg name="$vm" --arg region="$zid" --arg image="$IMAGE" --arg flavor="$FLAVOR" || return 1
	vm_ip=$(j -r '.vm_ip // empty')
	user=$(j -r '.ssh_user // "user1"')
	mask "$(j -r '.vm_id // empty')" "$vm_ip"
	[ -n "$vm_ip" ] || { bad provision "нет vm_ip: $(j -r .status)"; return 1; }
	ok provision "$(j -r .status), диск $(j -r .disk_gb) ГБ"
	cov provision

	must disk_create disk_create --arg name="$prefix-data" --arg size_gb=10 --arg zone="$ZONE" || return 1
	data=$(j -r '.disk_id // .disk.id // empty')
	mask "$data"
	[ -n "$data" ] || { bad disk_create "нет id диска"; return 1; }
	ok disk_create "$(j -r .status)"
	cov disk_create

	must disk_attach disk_attach --arg disk_id="$data" --arg vm="$vm" || return 1
	# disk_has ФИЛЬТР — jq -e по disk_list, $id — диск данных, $vm — имя ВМ.
	disk_has() { evo tools call disk_list --json | jq -e --arg id "$data" --arg vm "$vm" "$1" >/dev/null; }
	if poll 180 10 disk_has '.disks[] | select(.id == $id) | (.attached_to // []) | length > 0'; then
		ok disk_attach "диск в attached_to ВМ"
		cov disk_attach
	else
		bad disk_attach "за 3 мин диск не подключён"
		return 1
	fi

	# Состояние после подключения: resize в переходном состоянии — 422
	# disk_can_not_be_changed_from_current_state (run 39152048559).
	dstate() { evo tools call disk_list --json | jq -r --arg id "$data" '.disks[] | select(.id == $id) | .state'; }
	settled() { st=$(dstate); case "$st" in attaching | detaching | extending | creating | "") return 1 ;; *) return 0 ;; esac; }
	poll 300 10 settled || true
	ok "состояние подключённого диска" "$st"
	tc disk_resize --arg disk_id="$data" --arg size_gb=20
	if [ "$rc" = 0 ]; then
		if poll 180 10 disk_has '.disks[] | select(.id == $id) | .size_gb == 20'; then
			ok "disk_resize подключённого" "10 → 20 ГБ (state $st)"
			cov disk_resize
		else bad "disk_resize подключённого" "размер не 20 ГБ за 3 мин"; fi
	else
		row "disk_resize подключённого" отказ "state $st: код $rc, $reason: $(printf '%s' "$why" | grep -oE '"code":"[a-z_]+"' | head -1)"
	fi

	must disk_detach disk_detach --arg disk_id="$data" --arg vm="$vm" || return 1
	if poll 180 10 disk_has '.disks[] | select(.id == $id) | ((.attached_to // []) | length) == 0 and .state == "available"'; then
		ok disk_detach "available"
		cov disk_detach
	else bad disk_detach "за 3 мин не available"; fi

	if ! printf '%s\n' "${covered[@]}" | grep -qx disk_resize; then
		must "disk_resize отключённого" disk_resize --arg disk_id="$data" --arg size_gb=20 || return 1
		if poll 180 10 disk_has '.disks[] | select(.id == $id) | .size_gb == 20'; then
			ok "disk_resize отключённого" "10 → 20 ГБ"
			cov disk_resize
		else bad "disk_resize отключённого" "размер не 20 ГБ за 3 мин"; fi
	fi

	must disk_delete disk_delete --arg disk_id="$data" || return 1
	if poll 180 10 sh -c "! evo tools call disk_list --json | jq -e --arg id '$data' 'any(.disks[]; .id == \$id)' >/dev/null"; then
		ok disk_delete "диска нет в disk_list"
		cov disk_delete
	else bad disk_delete "диск в disk_list через 3 мин"; fi

	# Образ из загрузочного диска: ВМ выключается изнутри (vm stop в evo нет,
	# BACKLOG [04]), диск отключается, затем image_create.
	ssh_vm() {
		ssh -i .evo/id_ed25519 -o BatchMode=yes -o StrictHostKeyChecking=accept-new \
			-o UserKnownHostsFile="$tmp/known_hosts" -o ConnectTimeout=10 "$user@$vm_ip" "$@"
	}
	if ! poll 600 15 ssh_vm true 2>/dev/null; then bad "SSH к ВМ" "не поднялся за 10 мин"; return 1; fi
	ssh_vm 'sudo shutdown -h +0' >/dev/null 2>&1 || true
	vm_state() { evo tools call vm_get --arg name="$vm" --json 2>/dev/null | jq -r '.state // empty'; }
	stopped() { st=$(vm_state); case "$st" in stopped | shutoff | SHUTOFF | STOPPED) return 0 ;; *) return 1 ;; esac; }
	if poll 300 10 stopped; then ok "ВМ выключена изнутри" "state $st"; else
		bad "ВМ выключена изнутри" "state $(vm_state) через 5 мин"
		return 1
	fi
	boot=$(evo tools call disk_list --json | jq -r --arg vm "$vm" '[.disks[] | select(.bootable == true and ((.attached_to // []) | index($vm)))][0].id // empty')
	mask "$boot"
	[ -n "$boot" ] || { bad "загрузочный диск ВМ" "не найден в disk_list"; return 1; }
	must "disk_detach загрузочного" disk_detach --arg disk_id="$boot" --arg vm="$vm" || return 1
	if ! poll 300 10 sh -c "evo tools call disk_list --json | jq -e --arg id '$boot' '.disks[] | select(.id == \$id) | .state == \"available\"' >/dev/null"; then
		bad "disk_detach загрузочного" "за 5 мин не available"
		return 1
	fi
	ok "disk_detach загрузочного" "available"

	must image_create image_create --arg name="$prefix-img" --arg disk_id="$boot" --arg description="evo e2e live run" || return 1
	img=$(j -r '.image.id // empty')
	mask "$img"
	ok image_create "$(j -r .status)"
	cov image_create
	[ -n "$img" ] || img=$(lst image "$prefix-img" | cut -f1 | head -1)
	[ -n "$img" ] || { bad image_create "нет id образа"; return 1; }
	img_ready() { evo tools call image_get --arg image_id="$img" --json 2>/dev/null | jq -e '.ready == true' >/dev/null; }
	if poll 1800 30 img_ready; then ok "image_get ready" "образ готов"; else bad "image_get ready" "за 30 мин не ready: $(evo tools call image_get --arg image_id="$img" --json 2>/dev/null | jq -c '.zone_states // {}')"; fi

	must image_delete image_delete --arg image_id="$img" || return 1
	if poll 300 15 sh -c "! evo tools call image_list --arg type=private --json | jq -e --arg id '$img' 'any(.images[]?; .id == \$id)' >/dev/null"; then
		ok image_delete "образа нет в image_list"
		cov image_delete
	else bad image_delete "образ в image_list через 5 мин"; fi

	must "destroy ВМ без загрузочного диска" destroy --arg name="$vm" || return 1
	ok destroy "$(j -r .status)"
	cov destroy
	# загрузочный диск отключён от ВМ — его удаляет уборка (disk_delete)
}

# Сеть: VPC, ВМ без публичного IP за балансировщиком, FIP, правило, маршрут, SNAT.
area_net() {
	local zid a b fip fip_ip tg lb sg vpc dvpc ifc op n
	zid=$(zone_id)
	[ -n "$zid" ] || { bad "зона $ZONE" "не найдена"; return 1; }
	mkdir -p "$tmp/net" && cd "$tmp/net"

	must vpc_create vpc_create --arg name="$prefix-vpc" --arg description="evo e2e live run" || return 1
	op=$(j -r .operation_id)
	if opwait vpc "$op" 600; then
		vpc=$res_id
		[ -n "$vpc" ] || vpc=$(lst vpc "$prefix-vpc" | cut -f1 | head -1)
		ok vpc_create "операция done"
		cov vpc_create
		if [ -n "$vpc" ] && must vpc_get vpc_get --arg id="$vpc"; then ok vpc_get "$(j -r '.name == "'"$prefix-vpc"'"')"; fi
		if [ -n "$vpc" ] && must vpc_delete vpc_delete --arg id="$vpc"; then
			if opwait vpc "$(j -r .operation_id)" 600 && poll 300 15 gone vpc "$prefix-vpc"; then
				ok vpc_delete "VPC нет в vpc_list"
				cov vpc_delete
			else bad vpc_delete "$why"; fi
		fi
	else bad vpc_create "$why"; fi

	# ВМ A с публичным IP → destroy release_ip=false → адрес свободен.
	a="$prefix-a"
	must "provision (публичная)" provision --arg name="$a" --arg region="$zid" --arg image="$IMAGE" --arg flavor="$FLAVOR" || return 1
	fip_ip=$(j -r '.vm_ip // empty')
	mask "$(j -r '.vm_id // empty')" "$fip_ip"
	[ -n "$fip_ip" ] || { bad "provision (публичная)" "нет vm_ip"; return 1; }
	cov provision
	fip=$(evo tools call list_floating_ips --json | jq -r --arg ip "$fip_ip" '[.floating_ips[] | select(.ip_address == $ip)][0].id // empty')
	mask "$fip"
	[ -n "$fip" ] || { bad "FIP ВМ A" "не найден в list_floating_ips"; return 1; }
	must "destroy release_ip=false" destroy --arg name="$a" --arg release_ip=false || return 1
	cov destroy
	fip_free() { evo tools call list_floating_ips --json | jq -e --arg id "$fip" '.floating_ips[] | select(.id == $id) | .available == true' >/dev/null; }
	if poll 300 15 fip_free; then ok "destroy release_ip=false" "FIP остался свободным"; else bad "destroy release_ip=false" "FIP не свободен через 5 мин"; fi
	# адрес назван не префиксом ВМ B: уборка освобождает fip-<префикс>*, этот — по id
	echo "$fip" >"$tmp/fip_id"

	# ВМ B без публичного IP — цель балансировщика.
	b="$prefix-b"
	must "provision no_public_ip" provision --arg name="$b" --arg region="$zid" --arg image="$IMAGE" --arg flavor="$FLAVOR" --arg no_public_ip=true || return 1
	mask "$(j -r '.vm_id // empty')"
	if [ "$(j -r .private)" = true ]; then
		ok "provision no_public_ip" "private"
	else bad "provision no_public_ip" "private=$(j -r .private)"; fi

	must tg_create tg_create --arg name="$prefix-tg" --arg vm_id="$(evo tools call vm_get --arg name="$b" --json | jq -r .id)" || return 1
	if opwait lb "$(j -r .operation_id)" 900; then
		tg=$(lst tg "$prefix-tg" | cut -f1 | head -1)
		mask "$tg"
		ok tg_create "операция done"
		cov tg_create
	else
		bad tg_create "$why"
		return 1
	fi

	must lb_create lb_create --arg name="$prefix-lb" --arg target_group_id="$tg" --arg port=80 --arg zone="$ZONE" || return 1
	if opwait lb "$(j -r .operation_id)" 1500; then
		lb=$(lst lb "$prefix-lb" | cut -f1 | head -1)
		mask "$lb"
		tc lb_get --arg id="$lb"
		mask "$(j -r '.external_address.ipv4 // empty')"
		ok lb_create "status $(j -r '.status // "?"'), публичный IP: $(j -r 'if .external_address.ipv4 then "есть" else "нет" end')"
		cov lb_create
	else bad lb_create "$why"; fi
	lb=${lb:-$(lst lb "$prefix-lb" | cut -f1 | head -1)}
	if [ -n "$lb" ] && must lb_delete lb_delete --arg id="$lb"; then
		if opwait lb "$(j -r .operation_id)" 900 && poll 300 15 gone lb "$prefix-lb"; then
			ok lb_delete "балансировщика нет в lb_list"
			cov lb_delete
		else bad lb_delete "$why"; fi
	fi
	if must tg_delete tg_delete --arg id="$tg"; then
		if opwait lb "$(j -r .operation_id)" 600 && poll 300 15 gone tg "$prefix-tg"; then
			ok tg_delete "группы нет в tg_list"
			cov tg_delete
		else bad tg_delete "$why"; fi
	fi

	# Маршрут в дефолтном VPC: next hop — интерфейс ВМ B, пока у неё нет
	# публичного адреса. 18.10 (run 39152048559) — 400 InvalidArgument без
	# поля: пробуем две тестовые сети и записываем ответ.
	dvpc=$(evo tools call vpc_list --json | jq -r '[.vpcs[] | select(.default == true)][0].id // empty')
	ifc=$(evo tools call vm_get --arg name="$b" --json | jq -r '([.interfaces[] | select(.primary == true)] + .interfaces)[0].id // empty')
	mask "$dvpc" "$ifc"
	local cidr rid routed=
	if [ -z "$dvpc" ] || [ -z "$ifc" ]; then bad vpc_route_create "нет дефолтного VPC или интерфейса ВМ"; fi
	for cidr in $ROUTE_CIDRS; do
		[ -n "$dvpc" ] && [ -n "$ifc" ] || break
		tc vpc_route_create --arg vpc_id="$dvpc" --arg subnet="$cidr" \
			--arg next_hop_vm_id="$(evo tools call vm_get --arg name="$b" --json | jq -r .id)" --arg next_hop_interface_id="$ifc" --arg description="evo e2e live run"
		if [ "$rc" != 0 ]; then
			row "vpc_route_create $cidr" отказ "код $rc, $reason: $(printf '%s' "$why" | cut -c1-160)"
			continue
		fi
		if opwait vpc "$(j -r .operation_id)" 600; then
			ok "vpc_route_create $cidr" "операция done"
			cov vpc_route_create
			routed=$cidr
			break
		else bad "vpc_route_create $cidr" "$why"; fi
	done
	if [ -n "$routed" ]; then
		rid=$(evo tools call vpc_routes --arg vpc_id="$dvpc" --json | jq -r --arg c "$routed" '[.routes[] | select(.subnet == $c)][0].id // empty')
		mask "$rid"
		if [ -n "$rid" ] && must vpc_route_delete vpc_route_delete --arg vpc_id="$dvpc" --arg route_id="$rid"; then
			if opwait vpc "$(j -r .operation_id)" 600 && poll 300 15 gone route x; then
				ok vpc_route_delete "маршрута нет"
				cov vpc_route_delete
			else bad vpc_route_delete "$why"; fi
		elif [ -z "$rid" ]; then bad vpc_route_create "маршрута $routed нет в vpc_routes"; fi
	fi

	# FIP ВМ A → ВМ B.
	if must fip_attach fip_attach --arg fip="$fip" --arg vm="$b"; then
		if poll 180 10 sh -c "evo tools call vm_get --arg name='$b' --json | jq -e --arg ip '$fip_ip' '.public_ip == \$ip' >/dev/null"; then
			ok fip_attach "vm_get: public_ip — этот адрес"
			cov fip_attach
		else bad fip_attach "public_ip ВМ не тот за 3 мин"; fi
	fi

	# open_ports: правило с тестового адреса, повтор — already_open.
	sg=$(evo tools call list_security_groups --json | jq -r --arg z "$zid" --arg n "SSH-access_$ZONE" '([.security_groups[] | select(.name == $n)] + [.security_groups[] | select(.availability_zone_id == $z)])[0].id // empty')
	mask "$sg"
	if [ -n "$sg" ] && must open_ports open_ports --arg security_group="$sg" --arg ports="[$RULE_PORT]" --arg cidr="$RULE_CIDR"; then
		# порты в ответе — строки ("18085")
		if [ "$(j -c '(.opened // []) | map(tostring)')" = "[\"$RULE_PORT\"]" ]; then
			tc open_ports --arg security_group="$sg" --arg ports="[$RULE_PORT]" --arg cidr="$RULE_CIDR"
			if [ "$rc" = 0 ] && [ "$(j -c '(.already_open // []) | map(tostring)')" = "[\"$RULE_PORT\"]" ]; then
				ok open_ports "opened, повтор — already_open"
				cov open_ports
			else bad "open_ports повтор" "код $rc, already_open=$(j -c .already_open)"; fi
		else bad open_ports "opened=$(j -c .opened), failed=$(j -c .failed)"; fi
	fi

	# SNAT: только если в зоне шлюза нет (один на зону; чужой не трогаем).
	n=$(evo tools call snat_list --json | jq --arg z "$zid" --arg n "$ZONE" '[.nat_gateways[]? | select(.availability_zone_id == $z or .availability_zone == $n)] | length')
	if [ "$n" != 0 ]; then
		gap "snat_create, snat_delete" "в $ZONE уже есть SNAT-шлюз ($n) — один на зону, чужой не трогаем"
	elif must snat_create snat_create --arg name="$prefix-snat" --arg zone="$ZONE"; then
		ok snat_create "$(j -r .status)"
		cov snat_create
		local sid sst
		sid=$(lst snat "$prefix-snat" | cut -f1 | head -1)
		mask "$sid"
		# сразу после создания удаление — 422 nat_can_not_be_deleted_from_current_state
		# (run 39152048559): ждём устойчивого статуса
		snat_st() { sst=$(evo tools call snat_list --json | jq -r --arg id "$sid" '.nat_gateways[] | select(.id == $id) | .status'); case "$sst" in *creat* | *CREAT* | *pending* | *PENDING* | "") return 1 ;; esac; }
		poll 600 15 snat_st || true
		ok "SNAT после создания" "status $sst"
		if [ -n "$sid" ] && must snat_delete snat_delete --arg id="$sid"; then
			if poll 300 15 gone snat "$prefix-snat"; then ok snat_delete "шлюза нет в snat_list"; cov snat_delete; else bad snat_delete "шлюз в списке через 5 мин"; fi
		fi
	fi

	# destroy ВМ B освобождает и чужой ей по происхождению FIP.
	must "destroy ВМ B" destroy --arg name="$b" || return 1
	if poll 300 15 sh -c "! evo tools call list_floating_ips --json | jq -e --arg id '$fip' 'any(.floating_ips[]; .id == \$id and .state != \"error_deleting\")' >/dev/null"; then
		ok "destroy освобождает привязанный FIP" "адреса нет"
		rm -f "$tmp/fip_id"
	else
		# release_floating_ip — явно
		if must release_floating_ip release_floating_ip --arg id="$fip"; then
			ok release_floating_ip "$(j -r .status)"
			cov release_floating_ip
			rm -f "$tmp/fip_id"
		fi
	fi
	if [ -f "$tmp/fip_id" ]; then :; elif ! printf '%s\n' "${covered[@]}" | grep -qx release_floating_ip; then
		# адрес ушёл вместе с ВМ: release_floating_ip — на свободном адресе ВМ A не проверен
		gap release_floating_ip "адрес освободил destroy ВМ B; см. job mcp"
	fi
}

# DNS: приватная зона, запись A, удаление. Каждый вызов по зоне — с
# type=private: по умолчанию инструменты идут в API публичных зон.
area_dns() {
	local domain="$prefix.test." zid rid op
	must dns_zone_create dns_zone_create --arg domain="$domain" --arg type=private --arg description="evo e2e live run" || return 1
	op=$(j -r .operation_id)
	cov dns_zone_create
	# 18.10 (run 39152048559) операция создания приватной зоны не стала
	# done за 10 мин, хотя зона появилась в списке: ждём зону в списке
	if opwait dns "$op" 300; then ok "dns_operation создания" "done"; else row "dns_operation создания" "не done" "за 5 мин: $why"; fi
	poll 300 15 sh -c "evo tools call dns_zones --json | jq -e --arg d '$domain' 'any(.zones[]; (.domain // .name) == \$d)' >/dev/null" || true
	zid=$(lst dns "$prefix" | cut -f1 | head -1)
	zid=${zid#* }
	mask "$zid"
	[ -n "$zid" ] || { bad dns_zone_create "зоны нет в dns_zones"; return 1; }
	ok dns_zone_create "зона в dns_zones: $(evo tools call dns_zones --json | jq -c --arg id "$zid" '.zones[] | select(.id == $id) | {visibility, valid, delegated}')"
	must dns_zone_get dns_zone_get --arg id="$zid" --arg type=private && ok dns_zone_get "$(j -r '.visibility // .type // "прочитана"')"
	tc dns_zone_get --arg id="$zid"
	row "dns_zone_get без type" "$([ "$rc" = 0 ] && echo ответ || echo отказ)" "код $rc, $reason"
	must dns_record_create dns_record_create --arg zone_id="$zid" --arg type=private --arg name=www --arg record_type=A --arg value=198.18.255.10 --arg ttl=300 || return 1
	cov dns_record_create
	if opwait dns "$(j -r .operation_id)" 300; then ok "dns_operation записи" "done"; else row "dns_operation записи" "не done" "за 5 мин: $why"; fi
	rec() { evo tools call dns_records --arg zone_id="$zid" --arg type=private --json >"$tmp/recs" 2>/dev/null && jq -e '[.records[] | select((.name | startswith("www")) and ((.type // "") | ascii_downcase) == "a")] | length > 0' "$tmp/recs" >/dev/null; }
	if poll 300 15 rec; then
		rid=$(jq -r '[.records[] | select((.name | startswith("www")) and ((.type // "") | ascii_downcase) == "a")][0].id // empty' "$tmp/recs")
		mask "$rid"
		ok dns_record_create "запись A в dns_records, значение $(jq -r --arg id "$rid" '.records[] | select(.id == $id) | .values | index("198.18.255.10") | if . == null then "другое" else "верное" end' "$tmp/recs")"
	else
		bad dns_record_create "записи нет в dns_records за 5 мин"
		return 1
	fi
	must dns_record_get dns_record_get --arg record_id="$rid" --arg type=private && ok dns_record_get "ttl $(j -r '.ttl // "?"')"
	must dns_record_delete dns_record_delete --arg record_id="$rid" --arg type=private || return 1
	opwait dns "$(j -r .operation_id)" 300 || true
	norec() { evo tools call dns_records --arg zone_id="$zid" --arg type=private --json 2>/dev/null | jq -e --arg id "$rid" 'any(.records[]; .id == $id) | not' >/dev/null; }
	if poll 300 15 norec; then ok dns_record_delete "записи нет"; cov dns_record_delete; else bad dns_record_delete "запись осталась через 5 мин"; fi
	must dns_zone_delete dns_zone_delete --arg id="$zid" --arg type=private || return 1
	opwait dns "$(j -r .operation_id)" 300 || true
	if poll 600 15 gone dns "$prefix"; then
		ok dns_zone_delete "зоны нет"
		cov dns_zone_delete
	else bad dns_zone_delete "зона в списке через 10 мин"; fi
}

# Object Storage: бакет, объект, удаление.
area_s3() {
	local bucket content got
	bucket="$prefix-$(openssl rand -hex 3)"
	tc s3_buckets
	if [ "$reason" = CONFIG_INVALID ]; then gap "s3_*" "нет ключей S3 (CONFIG_INVALID, код $rc)"; return 0; fi
	must s3_bucket_create s3_bucket_create --arg bucket="$bucket" || return 1
	if [ -n "$(lst s3 "$bucket")" ]; then ok s3_bucket_create "бакет в s3_buckets"; cov s3_bucket_create; else bad s3_bucket_create "бакета нет в s3_buckets"; fi
	content="evo e2e $RUN $(openssl rand -hex 16)"
	must s3_put s3_put --arg bucket="$bucket" --arg key=live/hello.txt --arg content="$content" --arg content_type=text/plain || return 1
	[ "$(j -r .size)" = "${#content}" ] && ok s3_put "size ${#content}" && cov s3_put || bad s3_put "size $(j -r .size), ждали ${#content}"
	must s3_ls s3_ls --arg bucket="$bucket" && { [ "$(j -r .count)" = 1 ] && ok s3_ls "1 объект" || bad s3_ls "count $(j -r .count)"; }
	must s3_get s3_get --arg bucket="$bucket" --arg key=live/hello.txt && {
		got=$(j -r '.content // empty')
		[ "$(sha "$got")" = "$(sha "$content")" ] && ok s3_get "содержимое совпало (sha256)" || bad s3_get "содержимое другое, encoding $(j -r .encoding)"
	}
	tc s3_bucket_delete --arg bucket="$bucket"
	if [ "$rc" != 0 ]; then ok "s3_bucket_delete непустого" "отказ: код $rc, $reason"; else bad "s3_bucket_delete непустого" "удалил бакет с объектом"; return 1; fi
	must s3_rm s3_rm --arg bucket="$bucket" --arg key=live/hello.txt || return 1
	tc s3_ls --arg bucket="$bucket"
	[ "$(j -r '.count // 0')" = 0 ] && ok s3_rm "объектов 0" && cov s3_rm || bad s3_rm "count $(j -r .count)"
	must s3_bucket_delete s3_bucket_delete --arg bucket="$bucket" || return 1
	if poll 120 10 gone s3 "$bucket"; then ok s3_bucket_delete "бакета нет"; cov s3_bucket_delete; else bad s3_bucket_delete "бакет в списке"; fi
}

# Секреты (значения — только sha256) и сертификат: импорт, чтение, удаление.
area_secrets() {
	local path="$prefix/db" v1 v2 got cid crt key
	v1=$(rand_secret)
	v2=$(rand_secret)
	umask 077
	jq -n --arg p "$path" --arg v "$v1" '{path: $p, value: $v, description: "evo e2e live run"}' >"$tmp/s.json"
	must secret_create secret_create --args-file "$tmp/s.json" || return 1
	[ "$(j -r '.secret | tostring | contains("'"$v1"'")')" = false ] && ok secret_create "значение в ответе не вернулось" && cov secret_create || bad secret_create "значение в ответе"
	got=$(evo tools call secret_value --arg path="$path" --json 2>/dev/null | jq -r '.value // empty')
	[ "$(sha "$got")" = "$(sha "$v1")" ] && ok secret_value "v1 совпало (sha256)" || bad secret_value "v1 не совпало"
	jq -n --arg p "$path" --arg v "$v2" '{path: $p, value: $v}' >"$tmp/s.json"
	must secret_add_version secret_add_version --args-file "$tmp/s.json" || return 1
	rm -f "$tmp/s.json"
	got=$(evo tools call secret_value --arg path="$path" --json 2>/dev/null | jq -r '.value // empty')
	[ "$(sha "$got")" = "$(sha "$v2")" ] && ok secret_add_version "последняя версия — v2 (sha256)" && cov secret_add_version || bad secret_add_version "последняя версия не v2"
	must secret_get secret_get --arg path="$path" && ok secret_get "метаданные прочитаны"
	[ -n "$(lst secret "$path")" ] && ok secret_list "путь в списке" || bad secret_list "пути нет в secret_list"
	must secret_delete secret_delete --arg path="$path" || return 1
	if poll 120 10 gone secret "$path"; then ok secret_delete "секрета нет"; cov secret_delete; else bad secret_delete "секрет в списке"; fi
	tc secret_value --arg path="$path"
	[ "$rc" != 0 ] && ok "secret_value удалённого" "код $rc, $reason" || bad "secret_value удалённого" "значение вернулось"

	# Самоподписанный сертификат (cert_import берёт Base64 от PEM).
	openssl req -x509 -newkey rsa:2048 -nodes -days 2 -subj "/CN=$prefix.test" \
		-keyout "$tmp/key.pem" -out "$tmp/crt.pem" >/dev/null 2>&1
	crt=$(base64 -w0 <"$tmp/crt.pem")
	key=$(base64 -w0 <"$tmp/key.pem")
	jq -n --arg n "$prefix-cert" --arg c "$crt" --arg k "$key" '{name: $n, server_certificate: $c, private_key: $k, description: "evo e2e live run"}' >"$tmp/c.json"
	must cert_import cert_import --args-file "$tmp/c.json" || return 1
	rm -f "$tmp/c.json"
	cid=$(j -r '.certificate.id // empty')
	[ -n "$cid" ] || cid=$(lst cert "$prefix-cert" | cut -f1 | head -1)
	mask "$cid"
	[ -n "$cid" ] || { bad cert_import "нет id сертификата"; return 1; }
	ok cert_import "$(j -r .status)"
	cov cert_import
	must cert_get cert_get --arg id="$cid" && ok cert_get "status $(j -r '.status // "?"')"
	must cert_versions cert_versions --arg certificate_id="$cid" && ok cert_versions "версий $(j -r '(.versions // []) | length')"
	must cert_usage cert_usage --arg id="$cid" && ok cert_usage "прочитано"
	tc cert_private_key --arg certificate_id="$cid"
	if [ "$rc" = 0 ]; then
		# сверка по открытому ключу: формат закрытого (PKCS#1/PKCS#8) не важен
		got=$(j -r '.privateKey // .private_key // empty' | openssl pkey -pubout 2>/dev/null | sha256sum | cut -c1-64)
		if [ "$got" = "$(openssl x509 -pubkey -noout <"$tmp/crt.pem" | sha256sum | cut -c1-64)" ]; then
			ok cert_private_key "ключ — пара сертификату (sha256 открытого ключа)"
		else
			bad cert_private_key "ключ не пара сертификату: key_format $(j -r '.key_format // "-"'), ключи ответа $(j -c 'keys'), первая строка $(j -r '.privateKey // .private_key // ""' | head -1 | grep -oE '^-----BEGIN [A-Z ]+-----$' || echo '«не PEM»'), длина $(j -r '(.privateKey // .private_key // "") | length')"
		fi
	else bad cert_private_key "код $rc, $reason: $why"; fi
	rm -f "$tmp/key.pem"
	# cert_renew на импортированном: ждём отказ (перевыпуск — только Let's Encrypt)
	tc cert_renew --arg certificate_id="$cid"
	row "cert_renew импортированного" "$([ "$rc" = 0 ] && echo принят || echo отказ)" "код $rc, $reason: $(printf '%s' "$why" | cut -c1-160)"
	cov cert_renew
	must cert_delete cert_delete --arg id="$cid" || return 1
	if poll 120 10 gone cert "$prefix-cert"; then ok cert_delete "сертификата нет"; cov cert_delete; else bad cert_delete "сертификат в списке"; fi
	# Let's Encrypt без делегированной зоны: ждём отказ 400 (реестр CRT-3)
	tc cert_issue_le --arg name="$prefix-le" --arg dns_names="[\"$prefix.example.com\"]"
	row "cert_issue_le без делегированной зоны" "$([ "$rc" = 0 ] && echo принят || echo отказ)" "код $rc, $reason: $(printf '%s' "$why" | cut -c1-160)"
	cov cert_issue_le
}

# Artifact Registry и Container Apps: реестр, push образа, приложение.
area_ca() {
	local reg="evo-e2e-ca-$RUN" rid host app="$prefix-app" url code img
	probe_ar_pages
	must ar_create ar_create --arg name="$reg" --arg registry_type=DOCKER || return 1
	if opwait ar "$(j -r .operation_id)" 600; then ok ar_create "операция done"; cov ar_create; else bad ar_create "$why"; return 1; fi
	rid=$(lst ar "$reg" | cut -f1 | head -1)
	mask "$rid"
	host="$reg.cr.cloud.ru"
	mask "$host"
	img="$host/evo-e2e:live"
	docker pull -q nginxinc/nginx-unprivileged:alpine >/dev/null
	docker tag nginxinc/nginx-unprivileged:alpine "$img"
	if ! printf '%s' "$SECRET" | docker login "$host" -u "$KEY_ID" --password-stdin >"$tmp/dl" 2>&1; then
		gap "ca_*" "docker login отказан: $(grep -oE '[45][0-9]{2}[^"]{0,40}' "$tmp/dl" | head -1 | redact)"
		return 0
	fi
	if ! timeout 600 docker push -q "$img" >"$tmp/dp" 2>&1; then
		gap "ca_*" "push в реестр: $(tail -1 "$tmp/dp" | redact | cut -c1-160) (нет права push у SA или OPS-8)"
		docker logout "$host" >/dev/null 2>&1 || true
		return 0
	fi
	docker logout "$host" >/dev/null 2>&1 || true
	ok "push в Artifact Registry" "образ загружен"

	must ca_deploy ca_deploy --arg name="$app" --arg image="$img" --arg port=8080 --arg max_instances=1 || return 1
	ok ca_deploy "$(j -r .status)"
	cov ca_deploy
	app_url() { evo tools call ca_get --arg name="$app" --json 2>/dev/null | jq -r '[.. | strings | select(test("^https://"))][0] // empty'; }
	app_up() {
		url=$(app_url)
		[ -n "$url" ] || return 1
		mask "$url" "${url#https://}"
		code=$(curl -s -o "$tmp/page" -w '%{http_code}' --max-time 20 "$url" || echo 000)
		[ "$code" = 200 ] && grep -qi nginx "$tmp/page"
	}
	if poll 900 20 app_up; then ok "ca_get URL и HTTP" "200, nginx"; else bad "ca_get URL и HTTP" "за 15 мин нет ответа 200 (код ${code:-нет URL})"; fi
	must ca_logs ca_logs --arg name="$app" && ok ca_logs "строк $(j -r '(.logs // []) | length')"
	must ca_stop ca_stop --arg name="$app" && { ok ca_stop "$(j -r .status)"; cov ca_stop; }
	sleep 30
	must ca_start ca_start --arg name="$app" && {
		cov ca_start
		if poll 600 20 app_up; then ok ca_start "снова 200"; else bad ca_start "после start нет 200 за 10 мин (код ${code:-?})"; fi
	}
	must ca_delete ca_delete --arg name="$app" || return 1
	if poll 600 20 gone ca "$app"; then ok ca_delete "приложения нет"; cov ca_delete; else bad ca_delete "приложение в списке через 10 мин"; fi
	must ar_delete ar_delete --arg id="$rid" || return 1
	# операция удаления может не завершаться (реестр AR-3): ждём список
	if poll 600 20 gone ar "$reg"; then ok ar_delete "реестра нет"; cov ar_delete; else bad ar_delete "реестр в списке через 10 мин"; fi
}

# Стек: ВМ без публичного IP, секрет, внутренний балансировщик.
area_stack() {
	local job st sv
	sv=$(rand_secret)
	umask 077
	jq -n --arg n "$prefix" --arg i "$IMAGE" --arg z "$ZONE" --arg f "$FLAVOR" --arg s "$sv" '{manifest: {
		name: $n,
		vm: {private: true, image: $i, zone: $z, flavor: $f},
		secrets: [{path: ($n + "/token"), value: $s}],
		nlb: {name: ($n + "-lb"), port: 80, external: false}
	}}' >"$tmp/m.json"
	must stack_plan stack_plan --args-file "$tmp/m.json" || return 1
	rm -f "$tmp/m.json"
	job=$(j -r .job_id)
	ok stack_plan "шагов $(j -r '.steps | length'), стоимость $(j -c '.cost.monthly_rub // .cost.total_monthly_rub // .cost' | cut -c1-60)"
	must stack_apply stack_apply --arg job_id="$job" || return 1
	cov stack_apply
	st=$(j -r .status)
	local until=$(($(date +%s) + 2700))
	while [ "$st" != "done" ] && [ "$st" != error ] && [ "$(date +%s)" -lt "$until" ]; do
		tc stack_status --arg job_id="$job" --arg wait_seconds=240
		st=$(j -r '.status // "?"')
		echo "stack_status: $st, готово $(j -r '.done | length'), идёт $(j -r '.running | length'), ждёт $(j -r '.pending | length')"
	done
	if [ "$st" = "done" ]; then ok "stack_apply → done" "шаги: $(j -r '.done | join(",")')"; else bad "stack_apply → done" "status $st: $(j -r '.error // empty' | redact | cut -c1-200)"; fi
	# повторный apply готового стека ничего не создаёт
	tc stack_apply --arg job_id="$job"
	local nvm
	nvm=$(lst vm "$prefix" | wc -l)
	[ "$nvm" -le 1 ] && ok "stack_apply повтор" "ВМ стека: $nvm" || bad "stack_apply повтор" "ВМ стека: $nvm"
	[ -n "$(lst secret "$prefix/token")" ] && ok "секрет стека" "в secret_list" || bad "секрет стека" "нет в secret_list"
	must stack_destroy stack_destroy --arg job_id="$job" || return 1
	cov stack_destroy
	st=$(j -r .status)
	for _ in 1 2 3 4 5 6; do
		[ "$st" = destroyed ] && [ "$(j -r '(.deleting // []) | length')" = 0 ] && break
		sleep 60
		tc stack_destroy --arg job_id="$job"
		st=$(j -r '.status // "?"')
	done
	[ "$st" = destroyed ] && ok stack_destroy "удалено $(j -r '.deleted | length')" || bad stack_destroy "status $st, failed $(j -r '.failed | length')"
	tc stack_status --arg job_id="$job"
	[ "$(j -r .status)" = destroyed ] && ok "stack_status после destroy" destroyed || bad "stack_status после destroy" "$(j -r .status)"
	# остатки (ВМ, балансировщик, группа, секрет) проверяет уборка по префиксу
}

# MCP по stdio (deploy_plan → deploy_apply → deploy → destroy) и по HTTP
# (provision → destroy), гейт подтверждения на обоих.
area_mcp() {
	local zid id=10 vm="$prefix-vm" ip token sess vid
	zid=$(zone_id)
	[ -n "$zid" ] || { bad "зона $ZONE" "не найдена"; return 1; }
	mkdir -p "$tmp/mcp" && cd "$tmp/mcp"
	printf 'services:\n  web:\n    image: nginx:alpine\n    ports:\n      - "%s:80"\n' "$MCP_PORT" >docker-compose.yml
	# stdio ИМЯ АРГУМЕНТЫ_JSON — один сеанс MCP (initialize + вызов), ответ
	# инструмента (текст) в $tmp/r.json, isError в $iserr. План deploy_plan
	# живёт в файле каталога — следующий сеанс его видит, как у клиента после
	# перезапуска.
	stdio() {
		id=$((id + 1))
		printf '%s\n' \
			'{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"cloud-live","version":"1"}}}' \
			'{"jsonrpc":"2.0","method":"notifications/initialized"}' \
			"$(jq -cn --arg n "$1" --argjson a "$2" --argjson i "$id" '{jsonrpc:"2.0", id:$i, method:"tools/call", params:{name:$n, arguments:$a}}')" |
			timeout 2400 evo mcp >"$tmp/stdio" 2>/dev/null || true
		jq -c --argjson i "$id" 'select(.id == $i) | .result' "$tmp/stdio" >"$tmp/res" 2>/dev/null || true
		iserr=$(jq -r '.isError // false' "$tmp/res" 2>/dev/null || echo "?")
		jq -r '.content[0].text // empty' "$tmp/res" >"$tmp/r.json" 2>/dev/null || : >"$tmp/r.json"
	}
	stdio deploy_plan "$(jq -cn --arg n "$vm" --arg z "$ZONE" --arg i "$IMAGE" --arg f "$FLAVOR" --argjson p "$MCP_PORT" \
		'{mode:"manual", name:$n, product:"evo-e2e", zone:$z, image:$i, flavor:$f, compose_file:"docker-compose.yml", ports:[$p]}')"
	local plan
	plan=$(j -r '.plan_id // empty')
	[ "$iserr" = false ] && [ -n "$plan" ] && ok "stdio deploy_plan" "₽/мес $(j -r '.cost.total_monthly_rub // "?"')" || { bad "stdio deploy_plan" "isError $iserr, $(j -r '.reason // .error // empty' | redact | cut -c1-200)"; return 1; }
	stdio deploy_apply "$(jq -cn --arg p "$plan" '{plan_id:$p}')"
	[ "$(j -r .status)" = confirmation_required ] && [ -z "$(lst vm "$vm")" ] && ok "stdio deploy_apply без confirmed" "confirmation_required, ВМ нет" || bad "stdio deploy_apply без confirmed" "status $(j -r .status)"
	stdio deploy_apply "$(jq -cn --arg p "$plan" '{plan_id:$p, confirmed:true}')"
	vid=$(j -r '.vm_id // empty')
	ip=$(j -r '.public_ip // empty')
	mask "$vid" "$ip"
	[ "$iserr" = false ] && [ -n "$vid" ] && ok "stdio deploy_apply" "status $(j -r .status), $(j -r .elapsed_sec) с" && cov deploy_apply || { bad "stdio deploy_apply" "isError $iserr, $(j -r '.reason // .error // empty' | redact | cut -c1-200)"; return 1; }
	stdio deploy "$(jq -cn --arg r "$zid" --arg i "$IMAGE" --arg f "$FLAVOR" '{confirmed:true, region:$r, image:$i, flavor:$f}')"
	if [ "$iserr" = false ]; then
		cov deploy
		ip=${ip:-$(evo tools call vm_get --arg vm_id="$vid" --json | jq -r '.public_ip // empty')}
		mask "$ip"
		if poll 300 10 sh -c "curl -fsS --max-time 10 'http://$ip:$MCP_PORT/' | grep -qi nginx"; then ok "stdio deploy" "HTTP $MCP_PORT: nginx"; else bad "stdio deploy" "нет HTTP на $MCP_PORT за 5 мин"; fi
		[ "$(lst vm "$vm" | wc -l)" = 1 ] && ok "deploy после deploy_apply" "та же ВМ, второй нет" || bad "deploy после deploy_apply" "ВМ с именем $vm: $(lst vm "$vm" | wc -l); всего ВМ $prefix*: $(lst vm "$prefix" | wc -l)"
	else bad "stdio deploy" "$(j -r '.reason // "-"'): $(j -r '.error // empty' | redact | cut -c1-200)"; fi
	stdio status '{}'
	[ "$iserr" = false ] && ok "stdio status" "$(j -r '.status // .health // "?"')" || bad "stdio status" "isError"
	stdio logs '{"tail":20}'
	[ "$iserr" = false ] && ok "stdio logs" "прочитаны" || bad "stdio logs" "$(j -r '.reason // "-"')"
	stdio destroy "$(jq -cn --arg v "$vid" '{vm_id:$v}')"
	[ "$(j -r .status)" = confirmation_required ] && [ "$(j -r '.target.name // empty')" = "$vm" ] && ok "stdio destroy без confirmed" "превью называет ВМ" || bad "stdio destroy без confirmed" "status $(j -r .status)"
	stdio destroy "$(jq -cn --arg v "$vid" '{vm_id:$v, confirmed:true}')"
	[ "$iserr" = false ] && ok "stdio destroy vm_id" "$(j -r .status), правила: закрыто $(j -r '(.security_group_rules.closed // []) | length')" && cov destroy || bad "stdio destroy vm_id" "$(j -r '.reason // "-"'): $(j -r '.error // empty' | redact | cut -c1-200)"
	if poll 600 20 gone vm "$vm"; then ok "ВМ удалена" ""; else bad "ВМ удалена" "в vm_list через 10 мин"; fi

	# HTTP: provision → destroy по имени.
	token=$(openssl rand -hex 32)
	mask "$token"
	MCP_BEARER_TOKEN=$token evo mcp --http 127.0.0.1:18090 --allow-project "${PROJECT:?нужен PROJECT}" 2>"$tmp/http.log" &
	http_pid=$!
	poll 20 1 curl -fs -o /dev/null http://127.0.0.1:18090/healthz || { bad "http: сервер" "не поднялся"; return 1; }
	hpost() {
		curl -s -D "$tmp/h" -o "$tmp/b" -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
			-H "Authorization: Bearer $token" ${sess:+-H "Mcp-Session-Id: $sess"} --max-time 2400 -d "$1" http://127.0.0.1:18090/mcp >/dev/null || true
		if grep -q '^data: ' "$tmp/b"; then sed -n 's/^data: //p' "$tmp/b"; else cat "$tmp/b"; fi
	}
	sess=
	hpost '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"cloud-live","version":"1"}}}' >/dev/null
	sess=$(awk 'tolower($1)=="mcp-session-id:"{print $2}' "$tmp/h" | tr -d '\r')
	hpost '{"jsonrpc":"2.0","method":"notifications/initialized"}' >/dev/null
	hcall() {
		hpost "$(jq -cn --arg n "$1" --argjson a "$2" '{jsonrpc:"2.0", id:9, method:"tools/call", params:{name:$n, arguments:$a}}')" >"$tmp/res"
		iserr=$(jq -r '.result.isError // "?"' "$tmp/res" 2>/dev/null || echo "?")
		jq -r '.result.content[0].text // empty' "$tmp/res" >"$tmp/r.json" 2>/dev/null || : >"$tmp/r.json"
	}
	local hvm="$prefix-http"
	hcall provision "$(jq -cn --arg n "$hvm" --arg r "$zid" --arg i "$IMAGE" --arg f "$FLAVOR" '{name:$n, region:$r, image:$i, flavor:$f}')"
	[ "$(j -r .status)" = confirmation_required ] && [ -z "$(lst vm "$hvm")" ] && ok "http provision без confirmed" "confirmation_required, ВМ нет" || bad "http provision без confirmed" "status $(j -r .status)"
	hcall provision "$(jq -cn --arg n "$hvm" --arg r "$zid" --arg i "$IMAGE" --arg f "$FLAVOR" '{name:$n, region:$r, image:$i, flavor:$f, confirmed:true}')"
	mask "$(j -r '.vm_id // empty')" "$(j -r '.vm_ip // empty')"
	[ "$iserr" = false ] && [ "$(j -r .status)" = ok ] && ok "http provision" "ВМ создана" && cov provision || bad "http provision" "isError $iserr, $(j -r '.reason // "-"'): $(j -r '.error // empty' | redact | cut -c1-200)"
	hcall destroy "$(jq -cn --arg n "$hvm" '{name:$n, confirmed:true}')"
	[ "$iserr" = false ] && ok "http destroy name" "$(j -r .status)" || bad "http destroy name" "isError $iserr, $(j -r '.reason // "-"')"
	if poll 600 20 gone vm "$hvm"; then ok "http: ВМ удалена" ""; else bad "http: ВМ удалена" "в vm_list через 10 мин"; fi
	kill "$http_pid" 2>/dev/null || true

	# release_floating_ip на свободном адресе: ВМ с публичным IP, destroy без освобождения
	tc provision --arg name="$prefix-fip" --arg region="$zid" --arg image="$IMAGE" --arg flavor="$FLAVOR"
	ip=$(j -r '.vm_ip // empty')
	mask "$(j -r '.vm_id // empty')" "$ip"
	if [ "$rc" = 0 ] && [ -n "$ip" ]; then
		local fid
		fid=$(evo tools call list_floating_ips --json | jq -r --arg ip "$ip" '[.floating_ips[] | select(.ip_address == $ip)][0].id // empty')
		mask "$fid"
		tc release_floating_ip --arg id="$fid"
		[ "$rc" != 0 ] && ok "release_floating_ip занятого" "отказ: код $rc" || bad "release_floating_ip занятого" "освободил адрес ВМ"
		must "destroy release_ip=false" destroy --arg name="$prefix-fip" --arg release_ip=false &&
			poll 300 15 sh -c "evo tools call list_floating_ips --json | jq -e --arg id '$fid' '.floating_ips[] | select(.id == \$id) | .available == true' >/dev/null" &&
			must release_floating_ip release_floating_ip --arg id="$fid" && {
			ok release_floating_ip "$(j -r .status)"
			cov release_floating_ip
		}
	else bad "provision для release_floating_ip" "код $rc, $reason"; fi
}

case "$area" in
disk | net | dns | s3 | secrets | ca | stack | mcp)
	echo "== $area: префикс $prefix"
	{
		echo
		echo "### Живой прогон: $area"
		echo
		echo "| Область | Проверка | Итог | Подробности |"
		echo "|---|---|---|---|"
	} >>"$summary"
	"area_$area" || failed=1
	;;
sweep)
	area=sweep
	p=${2:?нужен префикс, например evo-e2e-}
	# sweep ПРЕФИКС [ОБЛАСТЬ]: с областью — только её виды; без — все.
	# Маршрут и правило тестовой сети — общие для всех прогонов: их убирает
	# только net и общая уборка (evo-e2e-), не страховка другой области.
	if [ -n "${3:-}" ]; then kinds=$(area_kinds "$3"); else kinds=$KINDS; fi
	case "$p" in
	evo-e2e- | evo-e2e-net-*) delete_test_rules || failed=1 ;;
	*) kinds=${kinds/route /} ;;
	esac
	sweep "$p" "$kinds" || failed=1
	;;
*)
	echo "::error::неизвестная область: $area"
	exit 2
	;;
esac
