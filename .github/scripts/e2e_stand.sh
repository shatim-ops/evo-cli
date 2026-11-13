#!/usr/bin/env bash
# Стенд 1.9 (план, Г-50…Г-54): ресурсы, которые живут дольше запусков
# cloud-e2e, — на них до тега пробуют операции второго дня. Здесь —
# единственное место, где назван их префикс. Он не evo-e2e-*: уборка
# запусков (cleanup_only, уборка областей, dbaas-delete.sh) отбирает по
# evo-e2e- и стенд не видит; исключение — именем, а не пропуском вида
# ресурса. Закреплено TestWorkflows_CleanupLeavesTheStand и случаями
# «стенд» в cloud_live_ops_test.sh и dbaas_delete_test.sh.
#
# Подключение: . e2e_stand.sh — STAND_PREFIX, STAND_UNTIL и stand_names.
# Запуск: e2e_stand.sh prefix | names | list | sweep.
#   list  — всё содержимое стенда «вид<TAB>имя<TAB>состояние», возраст и срок;
#           только чтение. Список не прочитан — код 1, а не «стенда нет»;
#           стенд за сроком STAND_UNTIL или при выпущенном теге
#           (STAND_TAG_OUT=1) — код 1 (сигнал, как overdue у ВМ пробы цены:
#           уборка запусков стенд обходит, значит срок у него свой).
#           STAND_FORBIDDEN_OK="вид …" — для ключа только на чтение: 403 на
#           список такого вида — предупреждение «не проверен», а не код 1.
#   sweep — удаление стенда (Г-54): только с STAND_SWEEP, равным префиксу
#           (иначе отказ до чтения облака); удаляет то, что само показало
#           перечнем, и ничего сверх; порядок — STAND_KINDS; в конце list
#           пуст или код 1 с перечнем остатков.
# Нужны evo (configure), а для копий, планов защиты, групп безопасности и
# тегов — KEY_ID, SECRET, PROJECT (у evo их удаления нет: прямой запрос
# Compute и «Менеджера ресурсов»).
STAND_PREFIX=evo-stand-19
# Срок стенда: тег STAND_TAG или дата STAND_UNTIL (план 1.9, решение B) —
# что раньше. Выпущен ли тег, скрипту говорит вызывающий: STAND_TAG_OUT=1.
STAND_TAG=v1.9.0
STAND_UNTIL=2026-12-15
# Виды ресурсов стенда в порядке удаления: сначала то, что само тратит и
# порождает (планы защиты, копии), затем кластеры, ВМ, адреса, диски, сеть;
# тег «Менеджера ресурсов» — последним, после ресурсов, которым назначен
# (Вп-3, решено 12.11: один тег на ВМ стенда).
STAND_KINDS="pplan backup k8s pg vm fip disk subnet sg tag"
STAND_RM=https://resource-manager.api.cloud.ru/v1

# stand_names — «вид<TAB>имя» ресурсов, которые создаёт стенд (Г-51). Имена
# производных ресурсов evo строит сам от имени ВМ: адрес fip-<ВМ>-…,
# загрузочный диск <ВМ>-boot — их находит префикс.
stand_names() {
	printf '%s\t%s\n' \
		vm "$STAND_PREFIX-vm" \
		k8s "$STAND_PREFIX-k8s" \
		pg "$STAND_PREFIX-pg" \
		backup "$STAND_PREFIX-bk" \
		tag "$STAND_PREFIX-tag"
}


stand_err() { jq -r '[.error.reason? // empty, .error.message? // empty] | join(": ")' "$1" 2>/dev/null | sed -E -f "$stand_here/redact.sed" | cut -c1-200; }

# stand_api МЕТОД ПУТЬ — запрос Compute; код HTTP в $code, тело в $stand_tmp/api.json.
stand_api() {
	: >"$stand_tmp/api.json"
	code=$(curl -sS -o "$stand_tmp/api.json" -w '%{http_code}' -X "$1" "https://compute.api.cloud.ru/api/v1$2" -H "Authorization: Bearer $STAND_TOKEN" 2>/dev/null) || code=000
}
# stand_rm МЕТОД ПУТЬ — запрос «Менеджера ресурсов» (теги; хост тот же, что
# у пробы Г-17, заключение «Риска» 183); код HTTP в $code.
stand_rm() {
	: >"$stand_tmp/api.json"
	code=$(curl -sS --connect-timeout 10 --max-time 120 -o "$stand_tmp/api.json" -w '%{http_code}' -X "$1" "$STAND_RM$2" -H "Authorization: Bearer $STAND_TOKEN" 2>/dev/null) || code=000
}
# Токен старше STAND_TOKEN_TTL (10 мин) берётся заново: sweep ждёт удалений
# до получаса — дольше жизни одного токена.
stand_token() {
	[ -z "${STAND_TOKEN:-}" ] || [ $((SECONDS - ${STAND_TOKEN_AT:-0})) -ge "${STAND_TOKEN_TTL:-600}" ] || return 0
	STAND_TOKEN=
	[ -n "${KEY_ID:-}" ] && [ -n "${SECRET:-}" ] && [ -n "${PROJECT:-}" ] || return 1
	STAND_TOKEN=$(jq -n --arg k "$KEY_ID" --arg s "$SECRET" '{keyId:$k, secret:$s}' |
		curl -sS -X POST https://iam.api.cloud.ru/api/v1/auth/token -H 'Content-Type: application/json' --data-binary @- 2>/dev/null |
		jq -r '.access_token // .accessToken // empty') || STAND_TOKEN=
	[ -n "$STAND_TOKEN" ] || return 1
	STAND_TOKEN_AT=$SECONDS
	echo "::add-mask::$STAND_TOKEN"
}

# stand_lst ВИД — «id<TAB>имя<TAB>состояние» ресурсов стенда этого вида; код 1
# — список не прочитан. Свои — по префиксу; копии и планы защиты — ещё и по
# id ВМ и дисков стенда ($stand_tmp/owners): копию по расписанию платформа
# называет сама. Ресурс копии — в resource, ресурсы плана — списком
# resources[] (так план создаёт проба Г-16).
stand_lst() {
	local out q
	# shellcheck disable=SC2016 # $p — переменная jq, не shell
	q='select((.name // "") | startswith($p)) | "\(.id)\t\(.name)\t\(.status // .state // "")"'
	case "$1" in
	k8s) out=$(evo tools call k8s_list --json 2>/dev/null) && jq -r --arg p "$STAND_PREFIX" ".clusters[]? | $q" <<<"$out" ;;
	pg) out=$(evo tools call pg_list --json 2>/dev/null) && jq -r --arg p "$STAND_PREFIX" ".clusters[]? | $q" <<<"$out" ;;
	vm) out=$(evo tools call vm_list --json 2>/dev/null) && printf '%s' "$out" >"$stand_tmp/vms.json" && jq -r --arg p "$STAND_PREFIX" ".vms[]? | $q" <<<"$out" ;;
	fip) out=$(evo tools call list_floating_ips --json 2>/dev/null) && jq -r --arg p "fip-$STAND_PREFIX" ".floating_ips[]? | $q" <<<"$out" ;;
	disk) out=$(evo tools call disk_list --json 2>/dev/null) && jq -r --arg p "$STAND_PREFIX" ".disks[]? | $q" <<<"$out" ;;
	subnet) out=$(evo tools call list_subnets --json 2>/dev/null) && jq -r --arg p "$STAND_PREFIX" ".subnets[]? | $q" <<<"$out" ;;
	sg) out=$(evo tools call list_security_groups --json 2>/dev/null) && jq -r --arg p "$STAND_PREFIX" ".security_groups[]? | $q" <<<"$out" ;;
	backup | pplan)
		# Токен берёт stand_scan: команда маски не должна попасть в строки списка.
		[ -n "${STAND_TOKEN:-}" ] || return 1
		stand_api GET "/$([ "$1" = backup ] && echo backups || echo protection-plans)?project_id=$PROJECT&limit=1000"
		case "$code" in 2??) ;; *)
			echo "$code" >"$stand_tmp/code.$1"
			return 1
			;;
		esac
		# Список длиннее страницы — не прочитан: остаток был бы невидим.
		jq -e '(.items | type == "array") and ((.total // 0) <= (.items | length))' "$stand_tmp/api.json" >/dev/null 2>&1 || return 1
		jq -r --arg p "$STAND_PREFIX" --rawfile o "$stand_tmp/owners" '($o | split("\n") | map(select(. != ""))) as $own
			| .items[] | select(((.name // "") | startswith($p)) or any((.resource.resource_id?, .resources[]?.resource_id?) | strings; . as $r | $own | index($r)))
			| "\(.id)\t\(.name)\t\(.state // "")"' "$stand_tmp/api.json" ;;
	tag)
		# Постранично; ответ без массива tags — не прочитан, а не «тегов нет»:
		# тег стенда остался бы невидим уборке (урок Г-17, У7).
		[ -n "${STAND_TOKEN:-}" ] || return 1
		local tok='' prev='' pages=0
		while :; do
			stand_rm GET "/tags?projectId=$PROJECT&pageSize=100${tok:+&pageToken=$tok}"
			case "$code" in 2??) ;; *)
				echo "$code" >"$stand_tmp/code.$1"
				return 1
				;;
			esac
			jq -e '.tags | type == "array"' "$stand_tmp/api.json" >/dev/null 2>&1 || return 1
			jq -r --arg p "$STAND_PREFIX" '.tags[] | ((.key? // .tagKeyValue? // "") | tostring) as $k | select($k | startswith($p)) | "\(.id)\t\($k)\t"' "$stand_tmp/api.json"
			prev=$tok
			tok=$(jq -r '.nextPageToken // empty' "$stand_tmp/api.json" 2>/dev/null) || tok=
			[ -n "$tok" ] || break
			# Тот же токен — список ходит по кругу: не прочитан сразу.
			[ "$tok" != "$prev" ] || return 1
			pages=$((pages + 1))
			[ "$pages" -lt 50 ] || return 1
		done ;;
	*) return 1 ;;
	esac
}

# stand_scan ФАЙЛ — содержимое стенда «вид<TAB>id<TAB>имя<TAB>состояние» в
# порядке STAND_KINDS; код 1 — хотя бы один список не прочитан (названо).
stand_scan() {
	local kind bad=0 read_=" "
	: >"$1"
	: >"$stand_tmp/owners"
	rm -f "$stand_tmp"/code.*
	stand_token || true
	# ВМ и диски читаются первыми: их id — владельцы копий.
	for kind in vm disk $STAND_KINDS; do
		case "$read_" in *" $kind "*) continue ;; esac
		read_="$read_$kind "
		if stand_lst "$kind" >"$stand_tmp/rows.$kind"; then
			case "$kind" in vm | disk) cut -f1 "$stand_tmp/rows.$kind" >>"$stand_tmp/owners" ;; esac
		else
			: >"$stand_tmp/rows.$kind"
			# Ключ только на чтение видит не всё: названный вид на 403 — не
			# проверен, и это сказано, а не скрыто.
			if [ "$(cat "$stand_tmp/code.$kind" 2>/dev/null)" = 403 ] && [[ " ${STAND_FORBIDDEN_OK:-} " == *" $kind "* ]]; then
				echo "::warning::$kind: HTTP 403 этому ключу — вид не проверен; его читает list и sweep с ключом записи"
			else
				echo "::error::$kind: список не прочитан"
				bad=1
			fi
		fi
	done
	for kind in $STAND_KINDS; do
		[ ! -s "$stand_tmp/rows.$kind" ] || sed "s/^/$kind\t/" "$stand_tmp/rows.$kind" >>"$1"
	done
	return "$bad"
}
# stand_show ФАЙЛ — перечень без id: «вид<TAB>имя<TAB>состояние»; id — под маску.
stand_show() {
	cut -f2 "$1" | sed 's/^/::add-mask::/'
	cut -f1,3,4 "$1"
}

# stand_del ВИД ID — запрос удаления; код 0 — принят (или ресурса уже нет),
# причина отказа — в $why.
stand_del() {
	local tool arg
	why=
	case "$1" in
	backup | pplan | sg)
		stand_token || { why="нет токена IAM"; return 1; }
		case "$1" in backup) arg=backups ;; pplan) arg=protection-plans ;; sg) arg=security-groups ;; esac
		stand_api DELETE "/$arg/$2"
		case "$code" in 2?? | 404) return 0 ;; esac
		why="HTTP $code"
		return 1
		;;
	tag)
		stand_token || { why="нет токена IAM"; return 1; }
		stand_rm DELETE "/tags/$2?projectId=$PROJECT"
		case "$code" in 2?? | 404) return 0 ;; esac
		why="HTTP $code"
		return 1
		;;
	k8s) tool=k8s_delete arg=cluster_id ;;
	pg) tool=pg_delete arg=cluster_id ;;
	vm) tool=destroy arg=vm_id ;;
	fip) tool=release_floating_ip arg=id ;;
	disk) tool=disk_delete arg=disk_id ;;
	subnet) tool=subnet_delete arg=subnet_id ;;
	*) why="неизвестный вид"; return 1 ;;
	esac
	evo --yes tools call "$tool" --arg "$arg=$2" --json >/dev/null 2>"$stand_tmp/del.err" && return 0
	why=$(stand_err "$stand_tmp/del.err")
	return 1
}

stand_list() {
	local scan="$stand_tmp/scan" today born days rc=0
	stand_scan "$scan" || rc=1
	if [ "$rc" = 0 ] && [ ! -s "$scan" ]; then
		echo "стенда нет: ресурсов $STAND_PREFIX* не найдено"
		return 0
	fi
	stand_show "$scan"
	today=${STAND_TODAY:-$(date -u +%F)}
	born=$(jq -r --arg p "$STAND_PREFIX" '[.vms[]? | select((.name // "") | startswith($p)) | .created_time // empty] | min // empty | .[0:10]' "$stand_tmp/vms.json" 2>/dev/null) || born=
	if [ -n "$born" ] && days=$((($(date -u -d "$today" +%s) - $(date -u -d "$born" +%s)) / 86400)) 2>/dev/null; then
		echo "возраст стенда: $days сут (по ВМ); срок: до $STAND_UNTIL или тега $STAND_TAG"
	else
		echo "возраст стенда: не определён (ВМ стенда или её времени создания нет); срок: до $STAND_UNTIL или тега $STAND_TAG"
	fi
	if [ -s "$scan" ] && [ "${STAND_TAG_OUT:-}" = 1 ]; then
		echo "::error::тег $STAND_TAG выпущен, а стенд не убран: уберите его (e2e_stand.sh sweep)"
		rc=1
	fi
	if [ -s "$scan" ] && [[ "$today" > "$STAND_UNTIL" ]]; then
		echo "::error::стенд за сроком ($STAND_UNTIL): уберите его (e2e_stand.sh sweep) или продлите срок решением владельца"
		rc=1
	fi
	return "$rc"
}

stand_sweep() {
	local snap="$stand_tmp/snap" now="$stand_tmp/now" kind id name state until left rc=0
	declare -A failed=() stuck=()
	# Подтверждение — точное равенство префиксу, до любого чтения облака.
	if [ "${STAND_SWEEP:-}" != "$STAND_PREFIX" ]; then
		echo "::error::уборка стенда — только с STAND_SWEEP, равным его префиксу: ждём «$STAND_PREFIX», получено «${STAND_SWEEP:-}»" >&2
		return 2
	fi
	# Послабление ключа чтения — только у list: удаление требует полного снимка.
	STAND_FORBIDDEN_OK=
	if ! stand_scan "$snap"; then
		echo "::error::содержимое стенда прочитано не целиком — не удаляю: остаток был бы невидим"
		return 1
	fi
	if [ ! -s "$snap" ]; then
		echo "стенда нет: ресурсов $STAND_PREFIX* не найдено"
		return 0
	fi
	echo "== стенд $STAND_PREFIX: будет удалено"
	stand_show "$snap"
	while IFS=$'\t' read -r kind id name state; do
		if stand_del "$kind" "$id"; then
			echo "удаление $kind $name: принято"
		else
			echo "удаление $kind $name: отказ — $why"
			failed[$id]=1
			# Кластер в FAILED платформа не удаляет: повтор не поможет.
			case "$kind:${state^^}" in k8s:*FAIL* | pg:*FAIL*) stuck[$id]=1 ;; esac
		fi
	done <"$snap"
	# Удаления асинхронны: ждём, пока показанное исчезнет; отказанное (диск
	# до удаления ВМ, подсеть до удаления кластера) повторяем.
	until=$(($(date +%s) + ${STAND_WAIT:-1800}))
	while :; do
		stand_scan "$now" || rc=1
		left=0
		while IFS=$'\t' read -r kind id name state; do
			grep -q "^$kind	$id	" "$snap" || continue
			[ -z "${stuck[$id]:-}" ] || continue
			left=$((left + 1))
			[ -n "${failed[$id]:-}" ] || continue
			if stand_del "$kind" "$id"; then
				unset "failed[$id]"
				echo "удаление $kind $name: принято (повтор)"
			fi
		done <"$now"
		if [ "$left" = 0 ] || [ "$rc" != 0 ] || [ "$(date +%s)" -ge "$until" ]; then break; fi
		echo "жду удаления: осталось $left, до предела $((until - $(date +%s))) с"
		sleep "${STAND_PAUSE:-30}"
	done
	[ "$rc" = 0 ] || { echo "::error::содержимое стенда после удалений прочитано не целиком"; return 1; }
	if [ ! -s "$now" ]; then
		echo "остатков стенда нет"
		return 0
	fi
	while IFS=$'\t' read -r kind id name state; do
		if [ -n "${stuck[$id]:-}" ]; then
			echo "::error::остаток стенда: $kind $name ($state) — Cloud.ru его не удаляет: только консоль Cloud.ru или поддержка"
		else
			echo "::error::остаток стенда: $kind $name${state:+ ($state)}"
		fi
	done <"$now"
	return 1
}

# Запущен, а не подключён.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	case "${1:-}" in
	prefix) printf '%s\n' "$STAND_PREFIX" ;;
	names) stand_names ;;
	list | sweep)
		set -uo pipefail
		stand_here=$(cd "$(dirname "$0")" && pwd)
		stand_tmp=$(mktemp -d)
		trap 'rm -rf "$stand_tmp"' EXIT
		"stand_$1"
		;;
	*)
		echo "::error::e2e_stand.sh: prefix, names, list или sweep" >&2
		exit 2
		;;
	esac
fi
