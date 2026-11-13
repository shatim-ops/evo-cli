#!/usr/bin/env bash
# Проверки вызываются косвенно (poll, must, cov): shellcheck до 0.11 считает
# их тела недостижимыми (SC2317, info) — ложное срабатывание.
# shellcheck disable=SC2317
# SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
# SPDX-FileContributor: Тимур Шакирбеков
# SPDX-License-Identifier: Apache-2.0
#
# Живые прогоны изменяющих инструментов evo на настоящем Cloud.ru, по одной
# области за вызов: cloud-live.sh ОБЛАСТЬ. Области: disk, net, dns, s3,
# secrets, ca, stack, mcp — их вызывает job live в cloud-e2e.yml после evo
# configure; pgnet — job dbaas-pg (вход probes: access); sweep ПРЕФИКС [ОБЛАСТЬ] — удалить ресурсы с именами ПРЕФИКС*
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
# (правило закрывает close_ports; что он оставил, уборка удаляет API
# напрямую, как job cleanup).
#
# Код выхода: 0 — все проверки области прошли и остатков нет; 1 — нет.
# A && ok || bad — ok всегда 0; фильтры jq — в одинарных кавычках; функции
# внутри областей вызываются через poll.
# shellcheck disable=SC2015,SC2016,SC2329
set -euo pipefail

area=${1:?нужна область: disk, net, dns, s3, secrets, ca, stack, mcp, pgnet, ops, dbchange или sweep ПРЕФИКС}
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
# Предел сеанса evo mcp по stdio в area_mcp: ~45 с вызова (D69) и запас на
# старт; повторов по next_tool — до MCP_FOLLOW_MAX (паузы MCP_FOLLOW_SLEEP).
MCP_LIMIT=${MCP_LIMIT:-75}
MCP_FOLLOW_MAX=${MCP_FOLLOW_MAX:-60}

redact() { sed -E -f "$here/redact.sed"; }
# shellcheck source-path=SCRIPTDIR source=mcp_stdio.sh
. "$here/mcp_stdio.sh"
mask() {
	local v
	for v in "$@"; do
		[ -z "$v" ] || [ "$v" = null ] || echo "::add-mask::$v"
	done
}
row() { # проверка | итог | подробности — всё через redact.sed: подробности
	# берутся из ответов API (в них бывают ID, run 39167011818)
	local d
	d=$(printf '%s' "$3" | redact)
	printf '%-44s %-6s %s\n' "$1" "$2" "$d"
	printf '| %s | %s | %s | %s |\n' "$area" "$1" "$2" "$d" >>"$summary"
}
ok() { row "$1" ok "${2:-}"; }
bad() {
	row "$1" FAIL "${2:-}"
	echo "::error::$area: $1 — $(printf '%s' "${2:-}" | redact)"
	failed=1
}
gap() { row "$1" "не покрыто" "${2:-}"; } # не проверено по причине вне evo
cov() { covered+=("$@"); }                  # изменяющий инструмент прошёл
# bound_check — проект ресурса на серверах с --allow-project (REL12-3) сразу
# после создания ресурса; серверы останавливает finish.
# shellcheck source-path=SCRIPTDIR source=bound-check.sh
. "$here/bound-check.sh"
# shellcheck source-path=SCRIPTDIR source=e2e_quota.sh
. "$here/e2e_quota.sh"
# j — jq по ответу последнего вызова; ответ не JSON не обрывает скрипт.
j() { jq "$@" "$tmp/r.json" 2>/dev/null || true; }

# tc ИНСТРУМЕНТ АРГУМЕНТЫ... — evo --yes tools call … --json: ответ в
# $tmp/r.json, код в $rc, reason в $reason, текст ошибки (замаскированный,
# до 300 символов) в $why. При ошибке инструмента stdout пуст (QA-12): reason
# и текст — из {"error":…} в stderr; ответ без поля error (stack_destroy с
# failed) там целиком, в .error.message.
tc() {
	local name=$1 httpst
	shift
	set +e
	evo --yes tools call "$name" "$@" --json >"$tmp/r.json" 2>"$tmp/err"
	rc=$?
	set -e
	reason=$(jq -rR 'fromjson? | .error.reason? // empty' "$tmp/err" 2>/dev/null | head -1)
	[ -n "$reason" ] || reason=$(j -r '.reason? // empty' | head -1)
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
# wait_seconds — не больше maximum схемы (45, D69): больше — USAGE, а не
# ожидание (rc.1 1.7, run 41814931228); держит TestScripts_ArgsWithinSchema.
opwait() {
	local kind=$1 op=$2 until=$(($(date +%s) + $3))
	res_id=
	while :; do
		tc "${kind}_operation" --arg operation_id="$op" --arg wait_seconds=45
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
# rand_secret — 24 случайных символа, буквы и цифры, в $SECRET_VALUE, сразу
# под маской (не через $(…): иначе ::add-mask:: попадёт в значение).
rand_secret() {
	SECRET_VALUE=$(openssl rand -base64 48 | tr -dc 'A-Za-z0-9' | cut -c1-24)
	mask "$SECRET_VALUE"
}
sha() { printf '%s' "$1" | sha256sum | cut -c1-64; }
# gone ВИД ПРЕФИКС — ресурсов вида с таким именем нет (для poll: список
# читается заново на каждой попытке). Список не прочитан (в том числе
# «список не полон» — API_ERROR) — не «нет»: код 1, poll ждёт дальше.
gone() {
	local o
	o=$(lst "$1" "$2") || return 1
	[ -z "$o" ]
}

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
		local v r
		out=$(evo tools call vpc_list --json 2>"$tmp/lst.err") || return 1
		for v in $(jq -r '.vpcs[]?.id' <<<"$out"); do
			# маршруты VPC не прочитаны — список не прочитан, не «маршрутов нет»
			r=$(evo tools call vpc_routes --arg vpc_id="$v" --json 2>"$tmp/lst.err") || return 1
			jq -r --arg v "$v" --arg c "$ROUTE_CIDRS" '($c | split(" ")) as $cs | .routes[]? | select(.subnet as $n | $cs | index($n)) | "\($v) \(.id)\troute \(.subnet)"' <<<"$r"
		done ;;
	subnet) out=$(evo tools call list_subnets --json 2>"$tmp/lst.err") &&
		jq -r --arg p "$p" '.subnets[]? | select((.name // "") | startswith($p)) | "\(.id)\t\(.name)"' <<<"$out" ;;
	vpc) out=$(evo tools call vpc_list --json 2>"$tmp/lst.err") &&
		jq -r --arg p "$p" '.vpcs[]? | select((.name // "") | startswith($p)) | "\(.id)\t\(.name)"' <<<"$out" ;;
	dns) out=$(evo tools call dns_zones --json 2>"$tmp/lst.err") &&
		# id — «видимость id»: удаление зоны идёт в API её видимости
		# зона, удаление которой платформа уже приняла (state …DELET…), — не
		# остаток прогона, а ожидание облака: её показывает deleting_zones
		jq -r --arg p "$p" '.zones[]? | select(((.name // "") | startswith($p)) or ((.domain // "") | startswith($p))) | select(((.state // "") | ascii_upcase | test("DELET")) | not) | "\(.visibility // "public") \(.id)\t\(.domain // .name)"' <<<"$out" ;;
	s3) out=$(evo tools call s3_buckets --json 2>"$tmp/lst.err") &&
		jq -r --arg p "$p" '.buckets[]? | select((.name // "") | startswith($p)) | "\(.name)\t\(.name)"' <<<"$out" ;;
	secret) out=$(evo tools call secret_list --json 2>"$tmp/lst.err") &&
		jq -r --arg p "$p" '.secrets[]? | select(((.path // .name // "") | startswith($p))) | "\(.path // .name)\t\(.path // .name)"' <<<"$out" ;;
	cert) out=$(evo tools call cert_list --json 2>"$tmp/lst.err") &&
		jq -r --arg p "$p" '.certificates[]? | select((.name // .displayName // "") | startswith($p)) | "\(.id)\t\(.name // .displayName)"' <<<"$out" ;;
	# Группы безопасности проб (область ops): только свои, по префиксу.
	sg) out=$(evo tools call list_security_groups --json 2>"$tmp/lst.err") &&
		jq -r --arg p "$p" '.security_groups[]? | select((.name // "") | startswith($p)) | "\(.id)\t\(.name)"' <<<"$out" ;;
	# Копии и планы защиты проб (область ops): у evo их нет — список прямо.
	backup) lst_api backups "$p" ;;
	pplan) lst_api protection-plans "$p" ;;
	# Теги проб (область ops): у evo их нет — список «Менеджера ресурсов».
	tag) lst_tags "$p" ;;
	*) return 1 ;;
	esac
}
# lst_api ПУТЬ ПРЕФИКС — lst для вида, которого в evo нет: GET списка
# compute (project_id, limit — наибольший по спецификации) с токеном IAM.
# Нет PROJECT или токена, не 2xx, ответ без items или отдано меньше, чем
# total, — список не прочитан (код 1, причина — в lst.err, как у evo).
# stdout — только строки списка: токен берёт sweep до чтения видов (маска
# токена — команда в stdout шага), здесь — лишь запасной путь, в stderr.
lst_api() {
	local why=
	if [ -z "${PROJECT:-}" ]; then why="нет PROJECT"
	elif ! iam_fresh >&2; then why="нет токена IAM (KEY_ID, SECRET)"
	else
		api GET "https://compute.api.cloud.ru/api/v1/$1?project_id=$PROJECT&limit=1000"
		case "$code" in
		2??) jq -e '(.items | type == "array") and ((.total // 0) <= (.items | length))' "$tmp/api.json" >/dev/null 2>&1 ||
			why="список не полон или без items" ;;
		*) why="HTTP $code: $(api_why)" ;;
		esac
	fi
	[ -z "$why" ] || { jq -nc --arg m "$1: $why" '{error: {reason: "API_ERROR", message: $m}}' >"$tmp/lst.err"; return 1; }
	jq -r --arg p "$2" '.items[] | select((.name // "") | startswith($p)) | "\(.id)\t\(.name)"' "$tmp/api.json"
}
# RM — «Менеджер ресурсов»: адрес — как в опубликованной спецификации
# (scripts/cloudru-specs.py, resource-manager), константой: токен IAM не
# уходит на адрес из окружения, входа запуска или ответа платформы
# (заключение «Риска» 183, У1; держит TestWorkflows_ProbeHostsAreDeclared).
RM=https://resource-manager.api.cloud.ru/v1
# lst_tags ПРЕФИКС — lst для тегов: все страницы GET /v1/tags проекта, свои —
# по ключу (key или начало tagKeyValue: ответ отдаёт пару одной строкой).
# Не 2xx, ответ без tags или страниц больше 50 — список не прочитан (код 1).
lst_tags() {
	local why='' tok='' pages=0
	: >"$tmp/tags.l"
	if [ -z "${PROJECT:-}" ]; then why="нет PROJECT"
	elif ! iam_fresh >&2; then why="нет токена IAM (KEY_ID, SECRET)"
	else
		while :; do
			api GET "$RM/tags?projectId=$PROJECT&pageSize=100${tok:+&pageToken=$tok}"
			case "$code" in 2??) ;; *) why="HTTP $code: $(api_why)"; break ;; esac
			jq -e '.tags | type == "array"' "$tmp/api.json" >/dev/null 2>&1 || { why="ответ без tags"; break; }
			jq -r --arg p "$1" '.tags[] | ((.key? // .tagKeyValue? // "") | tostring) as $k | select($k | startswith($p)) | "\(.id)\t\($k)"' "$tmp/api.json" >>"$tmp/tags.l"
			tok=$(jq -r '.nextPageToken // empty' "$tmp/api.json" 2>/dev/null) || tok=
			[ -n "$tok" ] || break
			pages=$((pages + 1))
			[ "$pages" -lt 50 ] || { why="страниц больше 50"; break; }
		done
	fi
	[ -z "$why" ] || { jq -nc --arg m "tags: $why" '{error: {reason: "API_ERROR", message: $m}}' >"$tmp/lst.err"; return 1; }
	cat "$tmp/tags.l"
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
	subnet) tc subnet_delete --arg subnet_id="$id" ;;
	vpc) tc vpc_delete --arg id="$id" ;;
	dns)
		# Связь с VPC (D66) — отвязка и повтор, а не тот же USAGE 20 мин.
		tc dns_zone_delete --arg type="${id%% *}" --arg id="${id#* }"
		if dns_linked && [ "${id%% *}" = private ]; then
			if dns_unlink "${id#* }"; then
				tc dns_zone_delete --arg type="${id%% *}" --arg id="${id#* }"
			else echo "отвязка зоны от VPC: $why"; fi
		fi ;;
	s3)
		local k
		evo tools call s3_ls --arg bucket="$id" --json 2>/dev/null | jq -r '.objects[]?.key' >"$tmp/keys" || true
		while read -r k; do [ -z "$k" ] || tc s3_rm --arg bucket="$id" --arg key="$k"; done <"$tmp/keys"
		tc s3_bucket_delete --arg bucket="$id" ;;
	secret) tc secret_delete --arg path="$id" ;;
	cert) tc cert_delete --arg id="$id" ;;
	# У evo нет удаления группы безопасности: прямой DELETE (как правила в
	# e2e_rules_sweep.sh); код HTTP — в $reason.
	sg)
		rc=1 reason="нет токена IAM"
		if [ -n "${TOKEN:-}" ] || iam_token; then
			api DELETE "https://compute.api.cloud.ru/api/v1/security-groups/$id"
			reason="HTTP $code"
			case "$code" in 2?? | 404) rc=0 ;; esac
		fi ;;
	backup | pplan)
		rc=1 reason="нет токена IAM"
		if iam_fresh; then
			api DELETE "https://compute.api.cloud.ru/api/v1/$([ "$kind" = backup ] && echo backups || echo protection-plans)/$id"
			reason="HTTP $code"
			case "$code" in 2?? | 404) rc=0 ;; esac
		fi ;;
	tag)
		rc=1 reason="нет токена IAM"
		if iam_fresh && [ -n "${PROJECT:-}" ]; then
			api DELETE "$RM/tags/$id?projectId=$PROJECT"
			reason="HTTP $code"
			case "$code" in 2?? | 404) rc=0 ;; esac
		fi ;;
	esac
}
# План защиты и копия — раньше ВМ и диска, которые они держат; тег — после
# ресурсов, которым назначен.
KINDS="pplan backup ca ar lb tg vm snat fip disk image route subnet vpc dns s3 secret cert sg tag"
# area_kinds ОБЛАСТЬ — виды ресурсов, которые область создаёт: её уборка
# читает только их (список одного вида бывает медленным: ar_list 18.10 —
# ~9,5 мин). Маршрут тестовой сети — только у net.
area_kinds() {
	case "$1" in
	disk) echo "vm fip disk image" ;;
	net) echo "lb tg vm snat fip disk route subnet vpc" ;;
	dns) echo "dns vpc" ;;
	s3) echo "s3" ;;
	secrets) echo "secret cert" ;;
	ca) echo "ca ar" ;;
	stack) echo "lb tg vm fip secret" ;;
	mcp) echo "vm fip" ;;
	pgnet) echo "vm fip subnet" ;;
	ops) echo "pplan backup vm fip disk subnet sg tag" ;;
	dbchange) echo "" ;; # ресурсов не создаёт: кластер — у job
	*) echo "${KINDS/route /}" ;;
	esac
}
# lst_done ВИД ПРЕФИКС — lst для уборки. Префикс своего прогона (область и
# её страховка sweep evo-e2e-<область>-<прогон>) — всё; префикс без номера
# прогона (sweep evo-e2e- из job cleanup, done_only) идёт рядом с живыми
# запусками и видит только ресурсы завершённых (e2e_live_runs.sh). Какие
# запуски живы, неизвестно — список считается непрочитанным: код 1.
done_only=0
lst_done() {
	lst "$1" "$2" >"$tmp/l.all" 2>/dev/null || return 1
	# Ресурсы пробы цены Г-20 переживают свой запуск: уборка области их не
	# трогает, общая — только со словом g20 (e2e_live_runs.sh).
	if [ "$done_only" = 1 ]; then
		"$here/e2e_live_runs.sh" drop <"$tmp/l.all"
	else "$here/e2e_live_runs.sh" keep <"$tmp/l.all"; fi
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
		case " $kinds " in *" backup "* | *" pplan "* | *" tag "*) iam_fresh || true ;; esac
		for kind in $kinds; do
			t0=$(date +%s)
			lst_done "$kind" "$p" >"$tmp/l" || {
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
		sleep "${SWEEP_PAUSE:-30}"
	done
	left=0
	for kind in $kinds; do
		# Список не прочитан (сбой или «список не полон») — остатки этого
		# вида не проверены: провал, а не «остатков нет».
		lst_done "$kind" "$p" >"$tmp/l" || {
			echo "::error::$kind: список не прочитан — остатки $p* не проверены: $(jq -rR 'fromjson? | .error | "\(.reason): \(.message)"' "$tmp/lst.err" 2>/dev/null | head -1 | redact | cut -c1-240)"
			left=$((left + 1))
			continue
		}
		while IFS=$'\t' read -r _ name; do
			[ -n "$name" ] || continue
			echo "::error::после прогона остался $kind $name — удалите его в консоли Cloud.ru"
			left=$((left + 1))
		done <"$tmp/l"
	done
	case " $kinds " in *" dns "*) deleting_zones "$p" ;; esac
	[ "$left" = 0 ] && echo "остатков $p* нет"
	[ "$left" = 0 ]
}

# deleting_zones ПРЕФИКС — зоны ПРЕФИКС*, удаление которых принято, но не
# завершено: предупреждение с состоянием (18.10 приватная зона висела в
# «Удаляется» часами, а DELETE раз за разом принимался).
deleting_zones() {
	local out
	out=$(evo tools call dns_zones --json 2>/dev/null) || return 0
	jq -r --arg p "$1" '.zones[]? | select(((.domain // .name // "") | startswith($p)) and ((.state // "") | ascii_upcase | test("DELET"))) | "\(.domain // .name)\t\(.state)"' <<<"$out" |
		while IFS=$'\t' read -r name st; do
			echo "::warning::зона $name удаляется облаком ($st) — удаление принято, ждём платформу"
		done
}

# iam_token — токен IAM по KEY_ID/SECRET в $TOKEN (под маской) для прямых
# запросов. Не через $(…): ::add-mask:: должен уйти в stdout шага.
iam_token() {
	TOKEN=
	[ -n "${KEY_ID:-}" ] && [ -n "${SECRET:-}" ] || return 1
	TOKEN=$(jq -n --arg k "$KEY_ID" --arg s "$SECRET" '{keyId:$k, secret:$s}' |
		curl -sS -X POST https://iam.api.cloud.ru/api/v1/auth/token -H 'Content-Type: application/json' --data-binary @- |
		jq -r '.access_token // .accessToken // empty')
	mask "$TOKEN"
	TOKEN_AT=$SECONDS
	[ -n "$TOKEN" ]
}
# iam_fresh — токен IAM не старше 10 минут: пробы области ops и уборка их
# видов идут десятки минут, дольше жизни одного токена.
iam_fresh() {
	[ -n "${TOKEN:-}" ] && [ $((SECONDS - ${TOKEN_AT:-0})) -lt 600 ] && return 0
	iam_token
}

# probe_ar_pages — как отвечает GET /v1/registries постранично (только
# чтение): по странице — HTTP, число реестров, длина токена, совпадает ли он
# с прошлым, хэш первого id (повторяется ли страница). Без ID и имён.
probe_ar_pages() {
	local tok="" prev="" i code n first q
	iam_token || { echo "probe ar: нет токена"; return 0; }
	for q in "" "&pageSize=100"; do
		tok=""
		prev=""
		for i in 1 2 3 4 5; do
			code=$(curl -sS -o "$tmp/arp" -w '%{http_code}' -H "Authorization: Bearer $TOKEN" \
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

# probe_dns_op ID — сырой ответ GET /v1/operations/{id} DNS (только
# чтение): ключи, done, поля состояния. Без ID и имён.
probe_dns_op() {
	local code
	iam_token || { echo "probe dns: нет токена"; return 0; }
	code=$(curl -sS -o "$tmp/dop" -w '%{http_code}' -H "Authorization: Bearer $TOKEN" "https://dns.api.cloud.ru/v1/operations/$1" || echo 000)
	echo "probe dns operation: http $code, ключи $(jq -c 'keys' "$tmp/dop" 2>/dev/null), done $(jq -c '.done' "$tmp/dop" 2>/dev/null), состояние $(jq -c '{status, state, metadata: (.metadata | if type == "object" then keys else . end), error: (.error | if . == null then null else (tostring | .[0:120]) end)}' "$tmp/dop" 2>/dev/null | redact)"
}

# Правило группы безопасности на порт RULE_PORT из RULE_CIDR (open_ports):
# закрывает его close_ports в области net; если правило осталось (close_ports
# упал или не дошёл), уборка удаляет его DELETE в API, как шаг cleanup.
delete_test_rules() {
	local sg r code n=0
	[ -n "${KEY_ID:-}" ] && [ -n "${SECRET:-}" ] || { echo "::warning::нет KEY_ID/SECRET — правило $RULE_PORT не удалено"; return 1; }
	evo tools call list_security_groups --arg rules=true --json >"$tmp/sg.json" || return 1
	jq -r --arg pr "$RULE_PORT:$RULE_PORT" --arg c "$RULE_CIDR" '.security_groups[] | .id as $g | .rules[]? | select(.direction == "ingress" and .port_range == $pr and .remote_ip_prefix == $c) | "\($g) \(.id)"' "$tmp/sg.json" >"$tmp/rules"
	[ -s "$tmp/rules" ] || return 0
	iam_token || { echo "::warning::нет токена — правило $RULE_PORT не удалено"; return 1; }
	while read -r sg r; do
		mask "$sg" "$r"
		code=$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE -H "Authorization: Bearer $TOKEN" \
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
# port_ranges — значения port_range всех правил ingress из RULE_CIDR (адрес
# одинаков во всех прогонах, правила с него ставит только область net), в
# любом формате: что отдал API (без ID).
port_ranges() {
	evo tools call list_security_groups --arg rules=true --json |
		jq -c --arg c "$RULE_CIDR" '[.security_groups[] | .rules[]? | select(.direction == "ingress" and .remote_ip_prefix == $c) | .port_range] | unique'
}

prefix="evo-e2e-$area-$RUN"
finish() {
	local rc_in=$?
	set +e
	bound_stop
	if [ "$area" != sweep ]; then
		echo "== уборка $prefix*"
		[ "$area" != net ] || delete_test_rules
		# Область без своих видов ресурсов (dbchange) убирать нечего.
		[ -z "$(area_kinds "$area")" ] || sweep "$prefix" "$(area_kinds "$area")" || failed=1
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

# probe_on СЛОВО — платная проба включена входом probes (план cloud-e2e
# rc.1 1.6): p36, p41, p44; shared (D81, 05.11); cmp7 (CMP-7, 10.11); r3f1 (R-59, 10.11).
probe_on() { case " ${PROBES:-} " in *" $1 "*) return 0 ;; *) return 1 ;; esac; }
# api МЕТОД URL [ТЕЛО] — прямой запрос к API с токеном IAM ($TOKEN, под
# маской): ответ в $tmp/api.json, код HTTP в $code; тело — через stdin, не
# в argv. Ни ответ, ни тело в лог не идут.
# Заголовки ответа — в $tmp/api.h (api_rid). API_RID, если задан, уходит
# заголовком X-Request-Id: по нему команда платформы находит запрос в своих
# журналах (CMP-7, вопрос Compute 10.11).
api_once() {
	local rid=()
	[ -z "${API_RID:-}" ] || rid=(-H "X-Request-Id: $API_RID")
	# Заголовки и тело прошлого ответа — не этого запроса: без ответа
	# (обрыв, код 000) прошлое тело читалось как ответ (прогон 204).
	rm -f "$tmp/api.h"
	: >"$tmp/api.json" # пустой, а не удалённый: jq читает его без ошибки
	API_RC=0
	if [ -n "${3:-}" ]; then
		code=$(printf '%s' "$3" | curl -sS -D "$tmp/api.h" -o "$tmp/api.json" -w '%{http_code}' -X "$1" "$2" \
			-H "Authorization: Bearer $TOKEN" "${rid[@]}" -H 'Content-Type: application/json' --data-binary @- 2>/dev/null) || { API_RC=$? code=000; }
	else
		code=$(curl -sS -D "$tmp/api.h" -o "$tmp/api.json" -w '%{http_code}' -X "$1" "$2" -H "Authorization: Bearer $TOKEN" "${rid[@]}" 2>/dev/null) || { API_RC=$? code=000; }
	fi
}
# api — запрос к API; чтение (GET) без ответа (код 000) повторяется до двух
# раз: в прогоне 207 одно чтение карточки ВМ осталось без ответа, и проба
# потеряла исходный flavor. Запись не повторяется — её исход неизвестен.
api() {
	local again=0
	api_once "$@"
	while [ "$code" = 000 ] && [ "$1" = GET ] && [ "$again" -lt 2 ]; do
		again=$((again + 1))
		sleep "${API_RETRY_PAUSE:-5}"
		api_once "$@"
	done
}
# api_rid — заголовки id запроса последнего ответа api (request, trace,
# correlation id; только они — не токены и не куки), «имя=значение» через
# пробел. Дефисы значения — точками: иначе redact.sed примет UUID за id
# ресурса и замаскирует то, что нужно команде платформы.
api_rid() {
	[ -f "$tmp/api.h" ] || return 0
	tr -d '\r' <"$tmp/api.h" |
		awk -F': *' 'tolower($1) ~ /^(x-)?(request|trace|correlation)-?id$|^traceparent$|^x-amzn-request-?id$/ && $2 ~ /^[A-Za-z0-9._:-]+$/ { gsub(/-/, ".", $2); printf "%s%s=%s", sep, tolower($1), $2; sep=" " }'
}
# api_why — текст ошибки ответа api, через redact.sed и обрезанный.
# Ответ без этих полей (не JSON, пустой) — его начало или «тело пустое»:
# «HTTP 422: » без текста не отличает отказ платформы от ошибки пробы.
api_why() {
	local w
	w=$(jq -r '[(if type == "array" then .[] else . end) | objects | [.code?, .message?, .detail?, .error?] | map(select(. != null) | tostring) | join(": ")] | join("; ")' "$tmp/api.json" 2>/dev/null) || w=
	[ -n "$w" ] || w=$(head -c 200 "$tmp/api.json" 2>/dev/null | tr '\n' ' ')
	printf '%s\n' "${w:-тело пустое}" | redact | cut -c1-200
}

# dns_linked — последний ответ tc — отказ D66 «зона связана с VPC»: USAGE
# до вопроса, а не сбой (зона без vpc_id связана с VPC по умолчанию, D65).
dns_linked() { [ "$rc" = 1 ] && [ "${reason%%,*}" = USAGE ] && printf '%s' "$why" | grep -q 'связан'; }
# dns_unlink ID — снять связи приватной зоны с VPC (PATCH linkedVpcIds: []
# в API DNS) и дождаться операции: evo этого не делает (D66 отказывает с
# путём; отвязка самим dns_zone_delete — бэклог 1.8), а без отвязки зону
# не удалить ни сценарию, ни уборке (rc.1 и rc.2 1.7: зона осталась, уборка
# 20 мин повторяла тот же USAGE). Код 1 — не отвязана, причина в $why.
dns_unlink() {
	local op
	iam_token || { why="нет токена IAM (KEY_ID, SECRET)"; return 1; }
	api PATCH "https://dns.api.cloud.ru/v1/privateZones/$1" '{"linkedVpcIds":[]}'
	[ "${code#2}" != "$code" ] || { why="PATCH linkedVpcIds: HTTP $code: $(api_why)"; return 1; }
	op=$(jq -r '.id // empty' "$tmp/api.json")
	[ -z "$op" ] || { mask "$op"; opwait dns "$op" 300; }
}

# VM_TAG_IDS — JSON-массив id тегов для tag_ids тела (проба Г-17); пусто —
# поля нет.
# vm_post NAME FLAVOR IMAGE ZID SIZE DISK_TYPE INTERFACES — прямой POST
# /api/v1.1/vms телом, как его шлёт evo (provision.CreateVMRequest): зона и
# подсеть — по id (availability_zone_id, subnet_id), flavor, образ и тип
# диска — по имени, image_metadata — по полям образа (vm_meta). Поля
# availability_zone_name и subnet_name API ВМ не принимает: 422 без текста
# (P41 rc 1.6, CMP7 прогон 181); без image_metadata — 422
# image_metadata_fields_missing (CMP7 прогон 183).
vm_post() {
	local meta
	# Без полей образа тело не собрать (прогон 184: пустые метаданные дали
	# пустое тело и 422 «Field required: body») — отказ пробы, не запрос.
	vm_pw
	if ! meta=$(vm_meta "$3" "$1") || ! printf '%s' "$meta" | jq -e 'type == "object"' >/dev/null 2>&1; then
		code=000
		jq -nc --arg w "$(cat "$tmp/vm_meta.why" 2>/dev/null || true)" '{message: ("image_metadata не собраны: " + $w)}' >"$tmp/api.json"
		return 0
	fi
	api POST https://compute.api.cloud.ru/api/v1.1/vms "$(jq -nc --arg p "$PROJECT" --arg n "$1" --arg f "$2" --arg i "$3" \
		--arg z "$4" --argjson sz "$5" --arg d "$6" --argjson ifs "$7" --argjson m "$meta" --argjson tags "${VM_TAG_IDS:-null}" \
		'[{project_id: $p, name: $n, availability_zone_id: $z, flavor_name: $f, image_name: $i, image_metadata: $m,
		   disks: [{name: ($n + "-boot"), size: $sz, disk_type_name: $d}], interfaces: $ifs}
		  + (if $tags == null then {} else {tag_ids: $tags} end)]')"
}
# vm_meta IMAGE HOSTNAME — image_metadata для прямого POST ВМ по полям
# образа из image_get (images.Fields — то же правило, что у provision):
# поля, которые заполняет evo, — одноразовым ключом пробы, именем ВМ,
# пользователем user1 и случайным паролем (маскируется); поля со значением
# по умолчанию — им. Поле, которое должен задать человек, — пусто: API
# ответит отказом, и строка пробы его назовёт. Отказ — с причиной в
# $tmp/vm_meta.why: ошибка image_get (reason и message evo) — прогоны 184 и
# 185 ушли на строку «не отдал fields» без причины (stderr — в /dev/null).
# vm_pw — случайный пароль ВМ проб в $VM_PW, под маской. Отдельно от
# vm_meta и не через $(…): её вывод — JSON метаданных, и команда маски в нём
# ломала разбор — проба писала «image_get не отдал fields» и не слала запрос.
vm_pw() {
	[ -z "${VM_PW:-}" ] || return 0
	# Правила пароля ВМ по спецификации Compute (VmSetPasswordRequest):
	# заглавная и строчная буква, цифра, спецсимвол. Пароль из одних
	# hex-цифр платформа отвергла (прогон 204, vm_metadata_validation).
	VM_PW=Ev1-$(openssl rand -hex 16)
	mask "$VM_PW"
}
vm_meta() {
	local k pw out rc=0
	echo "image_get $1 не отдал fields" >"$tmp/vm_meta.why"
	[ -f "$tmp/vmprobe_key.pub" ] || ssh-keygen -q -t ed25519 -N '' -C evo-probe -f "$tmp/vmprobe_key" >/dev/null 2>&1 ||
		{ echo "ssh-keygen не создал ключ пробы" >"$tmp/vm_meta.why"; return 1; }
	k=$(cat "$tmp/vmprobe_key.pub")
	pw=${VM_PW:-}
	[ -n "$pw" ] || { echo "пароль пробы не задан: vm_pw до vm_meta" >"$tmp/vm_meta.why"; return 1; }
	out=$(evo tools call image_get --arg image="$1" --json 2>"$tmp/vm_meta.err") || rc=$?
	if [ "$rc" -ne 0 ]; then
		printf 'image_get %s: код %s: %s\n' "$1" "$rc" "$(jq -r '.error | "\(.reason // "") \(.message // "")"' "$tmp/vm_meta.err" 2>/dev/null ||
			tail -1 "$tmp/vm_meta.err" | cut -c1-200)" >"$tmp/vm_meta.why"
		return 1
	fi
	printf '%s' "$out" | jq -e '.fields | type == "array"' >/dev/null 2>&1 || return 1
	printf '%s' "$out" | jq -c --arg k "$k" --arg h "$2" --arg pw "$pw" '
		[.fields[]? | select(.filled_by == "evo" or .filled_by == "default")
		 | {key: .name, value: (if .role == "ssh_key" then $k elif .role == "vm_name" then $h
			elif .role == "login" then "user1" elif .role == "password" then $pw else (.default_value // "") end)}
		 | select(.value != "")] | from_entries'
}
# zone_subnet ZID — id первой подсети зоны (для interfaces[].subnet_id).
zone_subnet() { evo tools call list_subnets --json | jq -r --arg z "$1" '[.subnets[]? | select(.availability_zone_id == $z) | .id // "" | select(. != "")][0] // empty'; }
# regular_if SUBNET_ID — interfaces с одним regular-интерфейсом в подсети.
regular_if() { jq -nc --arg s "$1" '[{type: "regular", subnet_id: $s, interface_security_enabled: false}]'; }

# Проба P36 (пункт Д; FB15-5): что отвечает платформа на создание ВМ с
# flavor меньше min_cpu образа — 4xx до создания или ВМ в
# error_creating/running. evo такой вызов отвергает до вопроса (D54), поэтому
# — прямой POST /api/v1.1/vms (vm_post). Итог — строка «P36: …» (ответ
# платформы, не провал); созданную ВМ убирает уборка области по префиксу.
probe_p36() {
	local zid=$1 fl vcpus img sn dt vst=
	iam_token || { gap P36 "нет токена IAM (KEY_ID, SECRET)"; return 0; }
	[ -n "${PROJECT:-}" ] || { gap P36 "нет PROJECT"; return 0; }
	read -r fl vcpus < <(evo tools call list_flavors --arg zone="$ZONE" --arg gpu=false --arg spot=false --json 2>/dev/null |
		jq -r '[.flavors[]? | select((.vcpus // 0) > 0)] | sort_by(.vcpus, .ram_gb) | .[0] // {} | "\(.name // "") \(.vcpus // 0)"') || true
	[ -n "${fl:-}" ] || { gap P36 "flavor в $ZONE не прочитан"; return 0; }
	img=
	for t in marketplace public; do
		img=$(evo tools call image_list --arg type="$t" --json 2>/dev/null |
			jq -r --argjson c "$vcpus" '[.images[]? | select((.min_cpu // 0) > $c and ((.name // "") | test("^[a-zA-Z][a-zA-Z0-9._-]{0,63}$"))) | .name][0] // empty') || true
		[ -z "$img" ] || break
	done
	[ -n "$img" ] || { gap P36 "в image_list нет образа с min_cpu больше $vcpus у $fl"; return 0; }
	sn=$(zone_subnet "$zid")
	dt=$(evo tools call list_disk_types --json 2>/dev/null | jq -r '[.disk_types[]? | .name // "" | select(ascii_downcase == "ssd")][0] // "SSD"') || dt=SSD
	[ -n "$sn" ] || { gap P36 "нет подсети в $ZONE"; return 0; }
	mask "$sn"
	vm_post "$prefix-p36" "$fl" "$img" "$zid" 50 "$dt" "$(regular_if "$sn")"
	case "$code" in
	2??)
		vm_st() { vst=$(evo tools call vm_get --arg name="$prefix-p36" --json 2>/dev/null | jq -r '.state // empty'); case "$vst" in "" | creating | *pending*) return 1 ;; esac; }
		poll 300 15 vm_st || true
		ok "P36: flavor $fl ($vcpus vCPU) ниже min_cpu образа $img" "принято (HTTP $code), ВМ: ${vst:-не появилась за 5 мин}"
		;;
	*) ok "P36: flavor $fl ($vcpus vCPU) ниже min_cpu образа $img" "отказ HTTP $code: $(api_why)" ;;
	esac
}

# Проба CMP-7 (реестр CMP-7; «да» владельца 10.11, план 1.8 #1095): что
# отвечает платформа на DELETE ВМ в creating — 409 или 422 (код HTTP и code
# ответа). ВМ — прямым POST /api/v1.1/vms ($FLAVOR, $IMAGE, подсеть зоны
# области); её состояние читается прямо перед DELETE, и DELETE идёт сразу,
# пока ВМ создаётся. Итог — строка «CMP7: …» (ответ платформы, не провал);
# ВМ, которую DELETE не удалил, убирает уборка области по префиксу. Счёт
# «N из M» — по прогонам (≥2), а не по пробам одного прогона.
probe_cmp7() {
	local zid=$1 sn vid='' st t
	iam_token || { gap CMP7 "нет токена IAM (KEY_ID, SECRET)"; return 0; }
	[ -n "${PROJECT:-}" ] || { gap CMP7 "нет PROJECT"; return 0; }
	sn=$(zone_subnet "$zid")
	[ -n "$sn" ] || { gap CMP7 "нет подсети в $ZONE"; return 0; }
	mask "$sn"
	vm_post "$prefix-cmp7" "$FLAVOR" "$IMAGE" "$zid" 20 SSD "$(regular_if "$sn")"
	case "$code" in 2??) ;; *)
		gap "CMP7: ВМ для пробы" "не создана, HTTP $code: $(api_why)"
		return 0
		;;
	esac
	vid=$(jq -r '(if type == "array" then .[0] else (.vms[0]? // .) end) | .id? // empty' "$tmp/api.json" 2>/dev/null) || vid=
	vm_id() { vid=$(evo tools call vm_get --arg name="$prefix-cmp7" --json 2>/dev/null | jq -r '.id // empty'); [ -n "$vid" ]; }
	[ -n "$vid" ] || poll 60 3 vm_id || true
	[ -n "$vid" ] || { gap "CMP7: ВМ для пробы" "id не прочитан за 60 с"; return 0; }
	mask "$vid"
	api GET "https://compute.api.cloud.ru/api/v1/vms/$vid"
	st=$(jq -r '.state // .status // empty' "$tmp/api.json" 2>/dev/null) || st=
	t=$(date -u +%Y-%m-%dT%H:%M:%S.%3NZ)
	# Свой id запроса (без дефисов — его не тронет redact) и id из ответа:
	# по ним Compute находит DELETE в своих журналах (вопрос 10.11).
	API_RID="evo-cmp7-${RUN:-local}-$(openssl rand -hex 8)"
	api DELETE "https://compute.api.cloud.ru/api/v1/vms/$vid"
	ok "CMP7: DELETE ВМ в состоянии ${st:-не прочитано} ($t)" "HTTP $code: $(api_why); X-Request-Id отправлен: $API_RID; из ответа: $(api_rid || true)"
	API_RID=
}

# Проба R3F1 (R-59, раунд 3 Grok F1; «да» владельца 10.11): SIGKILL процесса
# evo через 3 с после «Подтверждено» в disk_create (HDD, 1 ГБ). Записывается,
# есть ли след: записи disk_create в журнале evo (журнал включается на время
# пробы) и диск в облаке (и его состояние). Это ответ evo и платформы, а не
# провал; диск удаляется здесь же, остаток — уборкой области по префиксу.
# Задержка kill — несколько попыток (R3F1_DELAYS, по умолчанию 2; 2,4 и
# 2,8 с), у каждой свой диск: одна задержка то не успевает отправить POST
# (раунд 6, 1 мс; прогон 184, 0,5 и 1,5 с — диска нет), то опаздывает —
# evo уже ответил сам (прогоны 183 и 184, 3 с).
# Строка «kill: yes» и «диск в облаке: есть» — наблюдение; остальные
# попытки — контрольная группа.
probe_r3f1() {
	local d n=0
	for d in ${R3F1_DELAYS:-2 2.4 2.8}; do
		n=$((n + 1))
		r3f1_once "$d" "$n"
	done
}

r3f1_once() {
	local delay=$1 name="$prefix-r3f1-$2" tag="R3F1 ($1 с)" pid killed=no jpath jn0 jn1 jlast pend disk='' i
	evo diag on >/dev/null 2>&1 || { gap "$tag" "evo diag on не сработал"; return 0; }
	jpath=$(evo diag status --json 2>/dev/null | jq -r '.path // empty')
	# Считаются подтверждённые вызовы: --yes пишет и запись превью
	# (confirmation_required), а она не след создания (прогон 181: одна
	# запись превью, диск в облаке есть).
	jcount() { if [ -n "$jpath" ] && [ -f "$jpath" ]; then grep '"tool":"disk_create"' "$jpath" | grep -c '"confirmed":true' || true; else echo 0; fi; }
	jn0=$(jcount)
	: >"$tmp/r3f1.err"
	evo --yes tools call disk_create --arg name="$name" --arg size_gb=1 --arg disk_type=HDD --arg zone="$ZONE" --json \
		>"$tmp/r3f1.out" 2>"$tmp/r3f1.err" &
	pid=$!
	# Опрос stderr до 30 с с шагом 20 мс: согласие — конверт --json
	# {"status":"confirmed"} или строка «Подтверждено:» / «Confirmed:».
	# kill — через $delay после согласия, не сразу: раунд 6 Grok (реестр
	# находок № 70) убивал через 1 мс — POST ещё не ушёл, исход INCONCLUSIVE.
	for i in $(seq 1 1500); do
		grep -qE '"status":"confirmed"|^(Подтверждено|Confirmed): ' "$tmp/r3f1.err" && {
			sleep "$delay"
			kill -9 "$pid" 2>/dev/null && killed=yes
			break
		}
		kill -0 "$pid" 2>/dev/null || break
		sleep 0.02
	done
	wait "$pid" 2>/dev/null || true
	if [ "$killed" != yes ]; then
		gap "$tag: kill после «Подтверждено»" "evo завершился сам (шаг $i): $(head -c 300 "$tmp/r3f1.err" | tr '\n' ' ' | redact)"
	fi
	jn1=$(jcount)
	# Намерение (R-59, #1127): evo doctor после kill называет незавершённый
	# вызов — проверка «pending operation»; до #1127 строк нет.
	pend=$(evo doctor --json 2>/dev/null | jq -r --arg n "$name" '[.checks[]? | select(.name == "pending operation" and (.detail | contains($n)))] | length' 2>/dev/null) || pend=
	jlast=$(if [ -n "$jpath" ] && [ -f "$jpath" ]; then grep '"tool":"disk_create"' "$jpath" | tail -1 | jq -c '{confirmed, preview, error, reason}' 2>/dev/null || true; fi)
	evo diag off >/dev/null 2>&1 || true
	# Диск в облаке: до 60 с — POST мог уйти перед kill и дойти позже.
	r3f1_disk() { disk=$(evo tools call disk_list --json 2>/dev/null | jq -c --arg n "$name" '[.disks[]? | select(.name == $n) | {id, state: (.state // .status // null)}][0] // empty'); [ -n "$disk" ]; }
	poll 60 5 r3f1_disk || true
	mask "$(printf '%s' "$disk" | jq -r '.id // empty' 2>/dev/null)"
	row "$tag: след в журнале evo" "$([ "${jn1:-0}" -gt "${jn0:-0}" ] && echo есть || echo нет)" \
		"подтверждённых записей disk_create: было ${jn0:-0}, стало ${jn1:-0}${jlast:+; последняя запись disk_create $jlast}; kill: $killed"
	row "$tag: незавершённый вызов в evo doctor" "$([ "${pend:-0}" -gt 0 ] && echo есть || echo нет)" \
		"проверок «pending operation» с именем диска: ${pend:-не прочитано}; kill: $killed"
	row "$tag: диск в облаке" "$([ -n "$disk" ] && echo есть || echo нет)" \
		"$([ -n "$disk" ] && printf 'состояние %s' "$(printf '%s' "$disk" | jq -r '.state // "не прочитано"')" || echo 'за 60 с не появился')"
	# Намерение пробы снимается здесь: диск пробы удаляется ниже (или его
	# нет), а следующие команды области иначе писали бы о нём в stderr.
	grep -l -- "$name" .evo/intents/*.json 2>/dev/null | xargs -r rm -f
	[ -n "$disk" ] || return 0
	r3f1_ready() { r3f1_disk && ! printf '%s' "$disk" | jq -e '.state | tostring | test("creat")' >/dev/null; }
	poll 300 10 r3f1_ready || true
	tc disk_delete --arg disk_id="$(printf '%s' "$disk" | jq -r .id)"
	if [ "$rc" = 0 ]; then ok "$tag: диск пробы удалён" "disk_delete: $reason"
	else gap "$tag: диск пробы удалён" "код $rc, $reason: $why — уборка области по префиксу"; fi
}

# Проба P41 (пункт Е; FB15-21): что отвечает платформа на группу целей с
# ВМ, у которой интерфейс direct_ip (публичный адрес на интерфейсе, а не
# через FIP). evo tg_create такую цель пускает (превью называет тип), а ВМ
# с direct_ip evo не создаёт — прямой POST /api/v1.1/vms. Итог — строка
# «P41: …» (ответ платформы, не провал); группу и ВМ убирает уборка.
probe_p41() {
	local zid=$1 vid vst=
	iam_token || { gap P41 "нет токена IAM (KEY_ID, SECRET)"; return 0; }
	[ -n "${PROJECT:-}" ] || { gap P41 "нет PROJECT"; return 0; }
	vm_post "$prefix-p41" "$FLAVOR" "$IMAGE" "$zid" 20 SSD '[{"type":"direct_ip","interface_security_enabled":false}]'
	case "$code" in 2??) ;; *)
		gap "P41: ВМ с direct_ip" "не создана, HTTP $code: $(api_why)"
		return 0
		;;
	esac
	vm_st() { vst=$(evo tools call vm_get --arg name="$prefix-p41" --json 2>/dev/null | jq -r '.state // empty'); [ "$vst" = running ]; }
	if ! poll 600 15 vm_st; then
		gap "P41: ВМ с direct_ip" "не running за 10 мин (${vst:-нет})"
		return 0
	fi
	vid=$(evo tools call vm_get --arg name="$prefix-p41" --json | jq -r '.id // empty')
	mask "$vid"
	tc tg_create --arg name="$prefix-p41tg" --arg vm_id="$vid"
	if [ "$rc" != 0 ]; then
		ok "P41: tg_create с целью direct_ip" "отказ: код $rc, $reason: $(printf '%s' "$why" | cut -c1-200)"
	elif opwait lb "$(j -r .operation_id)" 900; then
		ok "P41: tg_create с целью direct_ip" "принято, операция done"
	else ok "P41: tg_create с целью direct_ip" "операция с ошибкой: $why"; fi
}

# Проба SHARED (D81, CMP-27; «да» владельца 05.11 на платную пробу с
# записью): подключает ли платформа один диск к двум ВМ. Документация
# Evolution: «одновременно только к одному ресурсу»; спецификация compute:
# shared у создания диска и vms массивом. Диски — прямым POST /disks
# (shared у evo disk_create нет): общий (shared: true) и обычный, оба 10 ГБ
# SSD в зоне области. Каждый подключается evo disk_attach к ВМ1, затем к ВМ2
# (вторая ВМ прогона без публичного адреса); итог — что ответил evo на
# второе подключение и сколько ВМ у диска по GET /disks/{id}. Строки
# «SHARED: …» — ответ платформы, не провал. Уборка — отключение от обеих ВМ
# и удаление дисков здесь же; остаток убирает sweep области по префиксу.
probe_shared() {
	local vm1=$1 zid=$2 vm2="$prefix-vm2" kind id body n second att2
	iam_token || { gap SHARED "нет токена IAM (KEY_ID, SECRET)"; return 0; }
	[ -n "${PROJECT:-}" ] || { gap SHARED "нет PROJECT"; return 0; }
	tc provision --arg name="$vm2" --arg region="$zid" --arg image="$IMAGE" --arg flavor="$FLAVOR" --arg no_public_ip=true
	[ "$rc" = 0 ] || { gap SHARED "вторая ВМ не создана: код $rc, $reason"; return 0; }
	for kind in shared plain; do
		body=$(jq -nc --arg p "$PROJECT" --arg n "$prefix-$kind" --arg z "$ZONE" --argjson s "$([ "$kind" = shared ] && echo true || echo false)" \
			'{project_id: $p, name: $n, size: 10, availability_zone_name: $z, disk_type_name: "SSD", shared: $s}')
		api POST https://compute.api.cloud.ru/api/v1/disks "$body"
		case "$code" in 2??) ;; *)
			ok "SHARED: создание диска $kind" "отказ HTTP $code: $(api_why)"
			continue
			;;
		esac
		id=$(jq -r '.id // empty' "$tmp/api.json" 2>/dev/null) || id=
		[ -n "$id" ] || id=$(lst disk "$prefix-$kind" | cut -f1 | head -1) || id=
		mask "$id"
		[ -n "$id" ] || { gap "SHARED: диск $kind" "нет id после создания; остаток — до уборки области (sweep)"; continue; }
		ready() { evo tools call disk_list --json | jq -e --arg id "$id" '.disks[] | select(.id == $id) | .state == "available"' >/dev/null; }
		poll 300 10 ready || { gap "SHARED: диск $kind" "не available за 5 мин; остаток — до уборки области (sweep)"; continue; }
		tc disk_attach --arg disk_id="$id" --arg vm="$vm1"
		[ "$rc" = 0 ] || { gap "SHARED: диск $kind к ВМ1" "код $rc, $reason; остаток — до уборки области (sweep)"; continue; }
		one() { evo tools call disk_list --json | jq -e --arg id "$id" '.disks[] | select(.id == $id) | ((.attached_to // []) | length) >= 1 and .state == "in_use"' >/dev/null; }
		if poll 300 10 one; then
			tc disk_attach --arg disk_id="$id" --arg vm="$vm2"
			second="код $rc, $reason: $(printf '%s' "$why" | cut -c1-160)"
			att2=$rc
		else
			# Второе подключение к диску, который не стал in_use, мерило бы
			# не то: его нет, строка отчёта говорит почему.
			second="не проверялось: диск не in_use за 5 мин после подключения к ВМ1"
			att2=skip
		fi
		# Число ВМ — по GET /disks/{id}; принятое подключение ко второй ВМ
		# ждём до 3 мин (поле vms обновляется асинхронно), отказ — одно чтение.
		vms_n() {
			api GET "https://compute.api.cloud.ru/api/v1/disks/$id"
			n=$(jq -r '(.vms // []) | length' "$tmp/api.json" 2>/dev/null) || n=
			[ "${n:-0}" -ge 2 ]
		}
		if [ "$att2" = 0 ]; then poll 180 10 vms_n || true; else vms_n || true; fi
		ok "SHARED: диск $kind (shared $(jq -r '.shared' "$tmp/api.json" 2>/dev/null)) ко второй ВМ" "evo: $second; ВМ у диска по GET: ${n:-?}"
		[ "$att2" != 0 ] || tc disk_detach --arg disk_id="$id" --arg vm="$vm2"
		tc disk_detach --arg disk_id="$id" --arg vm="$vm1"
		free() { evo tools call disk_list --json | jq -e --arg id "$id" '.disks[] | select(.id == $id) | ((.attached_to // []) | length) == 0 and .state == "available"' >/dev/null; }
		poll 300 10 free || true
		tc disk_delete --arg disk_id="$id"
		[ "$rc" = 0 ] || row "SHARED: удаление диска $kind" "остаток" "код $rc, $reason — до уборки области (sweep)"
	done
	tc destroy --arg name="$vm2"
	[ "$rc" = 0 ] || row "SHARED: удаление ВМ2" "остаток" "код $rc, $reason — до уборки области (sweep)"
}

# --- Области ----------------------------------------------------------------

# ВМ, диски, образ из загрузочного диска.
area_disk() {
	local zid vm vm_ip vid user data boot img st='' early warn
	zid=$(zone_id)
	[ -n "$zid" ] || { bad "зона $ZONE" "не найдена"; return 1; }
	mkdir -p "$tmp/disk" && cd "$tmp/disk"
	! probe_on p36 || probe_p36 "$zid"
	! probe_on cmp7 || probe_cmp7 "$zid"
	! probe_on r3f1 || probe_r3f1
	vm="$prefix-vm"
	must provision provision --arg name="$vm" --arg region="$zid" --arg image="$IMAGE" --arg flavor="$FLAVOR" || return 1
	vm_ip=$(j -r '.vm_ip // empty')
	user=$(j -r '.ssh_user // "user1"')
	vid=$(j -r '.vm_id // empty')
	mask "$vid" "$vm_ip"
	[ -n "$vm_ip" ] || { bad provision "нет vm_ip: $(j -r .status)"; return 1; }
	ok provision "$(j -r .status), диск $(j -r .disk_gb) ГБ"
	cov provision
	# Вторая ВМ и диски пробы — с префиксом области: sweep их уберёт.
	! probe_on shared || probe_shared "$vm" "$zid"
	warn=$(j -c '.warning // .warnings // empty' | redact | cut -c1-300)
	row "provision: предупреждение" "$([ -n "$warn" ] && echo есть || echo нет)" "$warn"

	# stderr ssh — в $tmp/ssh.err: причина провала в лог через redact.sed
	# (адрес маскируется), а не «не поднялся» без класса (rc.1 1.7).
	ssh_vm() {
		ssh -i .evo/id_ed25519 -o BatchMode=yes -o StrictHostKeyChecking=accept-new \
			-o UserKnownHostsFile="$tmp/known_hosts" -o ConnectTimeout=10 "$user@$vm_ip" "$@" 2>"$tmp/ssh.err"
	}
	vm_state() { evo tools call vm_get --arg name="$vm" --json 2>/dev/null | jq -r '.state // empty'; }
	# Проба (решение владельца 04.11, «проба, потом решаем»; rc.2 1.7: SSH к
	# ВМ disk — 3 из 3 «port 22: Connection timed out» в двух зонах, ВМ
	# deploy в той же зоне доступна): отвечает ли SSH сразу после provision,
	# до операций с диском. Исход финальной проверки ниже различает (а)
	# «отвечал сразу, после дисков перестал» и (б) «не отвечал сразу».
	local t0=$SECONDS
	if poll 600 15 ssh_vm true; then
		early="отвечал через $((SECONDS - t0)) с"
		row "SSH сразу после provision" ok "$early"
		# Факты сети и у ВМ с рабочим SSH — для сравнения с провалом.
		[ -z "$vid" ] || "$here/ssh_diag.sh" "$vid" "$vm_ip"
	else
		early="не отвечал 10 мин"
		row "SSH сразу после provision" "нет" "ssh — $(tail -1 "$tmp/ssh.err" | redact | cut -c1-200); ВМ $(vm_state)"
		[ -z "$vid" ] || "$here/ssh_diag.sh" "$vid" "$vm_ip"
	fi

	# D58 (1.6): зона из настроек по имени. CLOUDRU_REGION с именем зоны,
	# которой нет, — отказ до вопроса (USAGE), диска нет; диск данных —
	# без аргумента zone, зона — CLOUDRU_REGION=$ZONE по имени (раньше имя
	# уходило в availability_zone_id и платформа отвергала его после «да»).
	local region_was=${CLOUDRU_REGION-} region_set=${CLOUDRU_REGION+x}
	export CLOUDRU_REGION="$ZONE-no-such-zone"
	tc disk_create --arg name="$prefix-nozone" --arg size_gb=10
	if [ "$rc" = 1 ] && [ "${reason%%,*}" = USAGE ] && [ -z "$(lst disk "$prefix-nozone")" ]; then
		ok "disk_create: зоны с таким именем нет" "USAGE до вопроса, диска нет (D58)"
	else bad "disk_create: зоны с таким именем нет" "код $rc, $reason; дисков $prefix-nozone: $(lst disk "$prefix-nozone" | wc -l)"; fi
	export CLOUDRU_REGION="$ZONE"
	tc disk_create --arg name="$prefix-data" --arg size_gb=10
	if [ -n "$region_set" ]; then export CLOUDRU_REGION="$region_was"; else unset CLOUDRU_REGION; fi
	[ "$rc" = 0 ] || { bad "disk_create, зона из CLOUDRU_REGION по имени" "код $rc, $reason: $why"; return 1; }
	ok "disk_create: зона из CLOUDRU_REGION по имени" "создан — ушёл UUID (D58)"
	local t0=$SECONDS # ответ disk_create: от него — время до attached (N3)
	data=$(j -r '.disk_id // .disk.id // empty')
	mask "$data"
	[ -n "$data" ] || { bad disk_create "нет id диска"; return 1; }
	ok disk_create "$(j -r .status)"
	cov disk_create
	# Диск по id читать нечем (disk_get нет), а disk_* по id — изменяющие:
	# проект диска сверяется по списку проекта, как у ВМ (vm_get, область mcp).
	row "allow-project: диск" skip "нет чтения диска по id; проверка по списку — как у vm_get (область mcp)"

	# N3 (1.3.1): disk_attach сразу после disk_create сам ждёт готовности
	# диска; живой диск 27.10 был готов через 149 с, а attach ждал 2 мин и
	# получал 422 «no openstack_id». Сколько ушло от create до attached —
	# строкой отчёта (t0 — после ответа disk_create).
	must disk_attach disk_attach --arg disk_id="$data" --arg vm="$vm" || return 1
	# disk_has ФИЛЬТР — jq -e по disk_list, $id — диск данных, $vm — имя ВМ.
	disk_has() { evo tools call disk_list --json | jq -e --arg id "$data" --arg vm "$vm" "$1" >/dev/null; }
	if poll 180 10 disk_has '.disks[] | select(.id == $id) | (.attached_to // []) | length > 0'; then
		ok disk_attach "диск в attached_to ВМ, $((SECONDS - t0)) с от ответа disk_create, без 422"
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
	# D59 (1.6): удаление подключённого диска — отказ до вопроса (USAGE), и
	# с --yes; диск на месте и подключён.
	tc disk_delete --arg disk_id="$data"
	if [ "$rc" = 1 ] && [ "${reason%%,*}" = USAGE ] && disk_has '.disks[] | select(.id == $id) | (.attached_to // []) | length > 0'; then
		ok "disk_delete подключённого" "USAGE до вопроса, диск подключён (D59)"
	else bad "disk_delete подключённого" "код $rc, $reason; диск в attached_to: $(disk_has '.disks[] | select(.id == $id) | (.attached_to // []) | length > 0' && echo да || echo нет)"; return 1; fi
	tc disk_resize --arg disk_id="$data" --arg size_gb=20
	if [ "$rc" = 0 ]; then
		if poll 180 10 disk_has '.disks[] | select(.id == $id) | .size_gb == 20'; then
			ok "disk_resize подключённого" "10 → 20 ГБ (state $st)"
			cov disk_resize
		else bad "disk_resize подключённого" "размер не 20 ГБ за 3 мин"; fi
	else
		row "disk_resize подключённого" отказ "state $st: код $rc, $reason: $(printf '%s' "$why" | grep -oE '"code":"[a-z_]+"' | head -1)"
	fi

	# После resize диск снова в переходном состоянии (extending): detach в
	# нём — 422 disk_can_not_be_detached_from_current_state (cloud-e2e
	# кандидата 1.5, run 142, #521). Ждём то же, что перед resize.
	poll 300 10 settled || true
	ok "состояние перед disk_detach" "$st"
	must disk_detach disk_detach --arg disk_id="$data" --arg vm="$vm" || return 1
	if poll 180 10 disk_has '.disks[] | select(.id == $id) | ((.attached_to // []) | length) == 0 and .state == "available"'; then
		ok disk_detach "available (до detach: $st)"
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
	# «нет в списке» — только по прочитанному списку: ошибка disk_list
	# (в том числе «список не полон») — не «диска нет»
	if poll 180 10 sh -c "evo tools call disk_list --json | jq -e --arg id '$data' '(.disks | type) == \"array\" and (any(.disks[]; .id == \$id) | not)' >/dev/null"; then
		ok disk_delete "диска нет в disk_list"
		cov disk_delete
	else bad disk_delete "диск в disk_list через 3 мин"; fi

	# image_steps — образ, созданный tc image_create (ответ в $tmp/r.json):
	# готовность, проект на --allow-project, удаление.
	image_steps() {
		img=$(j -r '.image.id // empty')
		mask "$img"
		ok image_create "$(j -r .status)"
		cov image_create
		[ -n "$img" ] || img=$(lst image "$prefix-img" | cut -f1 | head -1)
		[ -n "$img" ] || { bad image_create "нет id образа"; return 1; }
		img_ready() { evo tools call image_get --arg image_id="$img" --json 2>/dev/null | jq -e '.ready == true' >/dev/null; }
		if poll 1800 30 img_ready; then ok "image_get ready" "образ готов"; else bad "image_get ready" "за 30 мин не ready: $(evo tools call image_get --arg image_id="$img" --json 2>/dev/null | jq -c '.zone_states // {}')"; fi
		bound_check image_get "$(jq -cn --arg i "$img" '{image_id: $i}')" "allow-project: image_get" own

		must image_delete image_delete --arg image_id="$img" || return 1
		if poll 300 15 sh -c "evo tools call image_list --arg type=private --json | jq -e --arg id '$img' '(.images | type) == \"array\" and (any(.images[]; .id == \$id) | not)' >/dev/null"; then
			ok image_delete "образа нет в image_list"
			cov image_delete
		else bad image_delete "образ в image_list через 5 мин"; fi
	}

	# Образ из загрузочного диска: ВМ выключается изнутри (vm stop в evo нет,
	# BACKLOG [04]), диск отключается, затем image_create.
	if ! poll 600 15 ssh_vm true; then
		# Факты сети — ssh_diag.sh (общий с job e2e): адрес и floating IP,
		# правило TCP/22 в группе, ответ порта (rc.2 1.7: 2 из 2 «timed out»).
		local key=нет
		[ -s .evo/id_ed25519 ] && key=есть
		[ -z "$vid" ] || "$here/ssh_diag.sh" "$vid" "$vm_ip"
		# (а) — сразу после provision отвечал, после дисков нет; (б) — не
		# отвечал и сразу.
		bad "SSH к ВМ" "не поднялся за 10 мин ($([ "${early%% *}" = отвечал ] && echo "а: после операций с диском" || echo "б: не отвечал и сразу после provision"); сразу — $early): ssh — $(tail -1 "$tmp/ssh.err" | redact | cut -c1-200); ключ .evo/id_ed25519 $key; пользователь $user; ВМ $(vm_state)"
		return 1
	fi
	# Код ssh — в строку провала (прогон 185: running через 5 мин, а дошла
	# ли команда, не видно). 255 с «closed by remote host» — ВМ уже гасла.
	local sd_rc=0 sd_err
	ssh_vm 'sudo shutdown -h +0' >/dev/null || sd_rc=$?
	sd_err=$(tail -1 "$tmp/ssh.err" 2>/dev/null | redact | cut -c1-200)
	stopped() { st=$(vm_state); case "$st" in stopped | shutoff | SHUTOFF | STOPPED) return 0 ;; *) return 1 ;; esac; }
	if poll 300 10 stopped; then ok "ВМ выключена изнутри" "state $st"; else
		bad "ВМ выключена изнутри" "state $(vm_state) через 5 мин; ssh shutdown: код $sd_rc${sd_err:+, $sd_err}"
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

	tc image_create --arg name="$prefix-img" --arg disk_id="$boot" --arg description="evo e2e live run"
	if [ "$rc" != 0 ] && image_quota_exceeded "$why"; then
		# Квота организации на пользовательские образы (CMP-16) — не evo:
		# шаги образа не покрыты, ВМ и диск удаляются дальше как обычно.
		gap image_create "квота организации на пользовательские образы (CMP-16): $reason"
		gap image_delete "образа нет: квота (CMP-16)"
	elif [ "$rc" != 0 ]; then
		bad image_create "код $rc, $reason: $why"
		return 1
	else
		image_steps || return 1
	fi

	must "destroy ВМ без загрузочного диска" destroy --arg name="$vm" || return 1
	ok destroy "$(j -r .status)"
	cov destroy
	# загрузочный диск отключён от ВМ — его удаляет уборка (disk_delete)
}

# subnet_steps VPC — subnet_create и subnet_delete (1.6, #567: новые
# create и delete входят в живой прогон своей версии, CLAUDE.md §5) в
# пустом VPC прогона: создание видно в list_subnets; второе имя — отказ
# до вопроса (USAGE), второй подсети нет; удаление (подсеть, которая ещё
# создаётся, — USAGE до вопроса, D61: повтор до 5 мин); подсети нет в
# list_subnets; повтор удаления — already_deleted (D26). Сеть 10.255.254.0/24
# — в новом VPC, ни с чем не пересекается.
subnet_steps() {
	local v=$1 sn sname="$prefix-sn"
	must subnet_create subnet_create --arg name="$sname" --arg cidr=10.255.254.0/24 --arg vpc_id="$v" --arg zone="$ZONE" || return 0
	sn=$(j -r '.subnet_id // empty')
	mask "$sn"
	[ -n "$sn" ] || { bad subnet_create "нет subnet_id в ответе"; return 0; }
	sn_listed() { evo tools call list_subnets --json | jq -e --arg id "$sn" 'any(.subnets[]?; .id == $id)' >/dev/null; }
	if poll 120 10 sn_listed; then ok subnet_create "подсеть в list_subnets"; else bad subnet_create "подсети нет в list_subnets за 2 мин"; fi
	tc subnet_create --arg name="$sname" --arg cidr=10.255.253.0/24 --arg vpc_id="$v" --arg zone="$ZONE"
	if [ "$rc" = 1 ] && [ "${reason%%,*}" = USAGE ] && [ "$(lst subnet "$sname" | wc -l)" = 1 ]; then
		ok "subnet_create с занятым именем" "USAGE до вопроса, подсеть одна"
	else bad "subnet_create с занятым именем" "код $rc, $reason; подсетей $sname: $(lst subnet "$sname" | wc -l)"; fi
	sn_del() { tc subnet_delete --arg subnet_id="$sn"; [ "$rc" = 0 ]; }
	if poll 300 15 sn_del; then
		if poll 300 15 sh -c "evo tools call list_subnets --json | jq -e --arg id '$sn' '(.subnets | type) == \"array\" and (any(.subnets[]; .id == \$id) | not)' >/dev/null"; then
			ok subnet_delete "подсети нет в list_subnets"
			cov subnet_create subnet_delete
		else bad subnet_delete "подсеть в list_subnets через 5 мин"; fi
	else bad subnet_delete "код $rc, $reason: $why"; return 0; fi
	tc subnet_delete --arg subnet_id="$sn"
	if [ "$rc" = 0 ] && [ "$(j -r '.status // empty')" = already_deleted ]; then
		ok "subnet_delete повтор" already_deleted
	else bad "subnet_delete повтор" "код $rc, status $(j -r '.status // empty'), $reason"; fi
}

# Сеть: VPC, ВМ без публичного IP за балансировщиком, FIP, правило, маршрут, SNAT.
area_net() {
	local zid a b fip fip_ip tg lb sg vpc dvpc ifc op n
	zid=$(zone_id)
	[ -n "$zid" ] || { bad "зона $ZONE" "не найдена"; return 1; }
	mkdir -p "$tmp/net" && cd "$tmp/net"
	! probe_on p41 || probe_p41 "$zid"

	must vpc_create vpc_create --arg name="$prefix-vpc" --arg description="evo e2e live run" || return 1
	op=$(j -r .operation_id)
	if opwait vpc "$op" 600; then
		vpc=$res_id
		[ -n "$vpc" ] || vpc=$(lst vpc "$prefix-vpc" | cut -f1 | head -1)
		ok vpc_create "операция done"
		cov vpc_create
		if [ -n "$vpc" ] && must vpc_get vpc_get --arg id="$vpc"; then ok vpc_get "$(j -r '.name == "'"$prefix-vpc"'"')"; fi
		[ -z "$vpc" ] || bound_check vpc_get "$(jq -cn --arg i "$vpc" '{id: $i}')" "allow-project: vpc_get"
		[ -z "$vpc" ] || subnet_steps "$vpc"
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

	# D57 (1.6): подсеть по имени. Имени, которого нет, — отказ до записи с
	# прежним reason обработчика (CONFIG_INVALID «не из проекта», код 2; D57:
	# «отказ и reason не меняются»), ВМ нет; ВМ B создаётся с подсетью зоны по имени (имя
	# единственное в проекте — два совпадения D57 отвергает): платформа
	# приняла бы имя в поле id только как 4xx, так что созданная ВМ значит,
	# что ушёл UUID.
	tc provision --arg name="$prefix-nosn" --arg region="$zid" --arg image="$IMAGE" --arg flavor="$FLAVOR" --arg subnet_id="$prefix-no-such-subnet"
	if [ "$rc" = 2 ] && [ "${reason%%,*}" = CONFIG_INVALID ] && [ -z "$(lst vm "$prefix-nosn")" ]; then
		ok "provision: подсети с таким именем нет" "CONFIG_INVALID до записи, ВМ нет (D57)"
	else bad "provision: подсети с таким именем нет" "код $rc, $reason; ВМ $prefix-nosn: $(lst vm "$prefix-nosn" | wc -l)"; fi
	local snname
	snname=$(evo tools call list_subnets --json | jq -r --arg z "$zid" '[.subnets[]? | .name // "" | select(. != "")] as $all
		| [.subnets[]? | select(.availability_zone_id == $z) | .name // "" | select(. != "") | select(. as $n | [$all[] | ascii_downcase | select(. == ($n | ascii_downcase))] | length == 1)][0] // empty')

	# ВМ B без публичного IP — цель балансировщика; подсеть — по имени (D57).
	b="$prefix-b"
	if [ -z "$snname" ]; then
		gap "provision: подсеть по имени" "в $ZONE нет подсети с единственным в проекте именем"
		must "provision no_public_ip" provision --arg name="$b" --arg region="$zid" --arg image="$IMAGE" --arg flavor="$FLAVOR" --arg no_public_ip=true || return 1
	else
		must "provision no_public_ip, подсеть по имени" provision --arg name="$b" --arg region="$zid" --arg image="$IMAGE" --arg flavor="$FLAVOR" --arg no_public_ip=true --arg subnet_id="$snname" || return 1
		ok "provision: подсеть по имени" "ВМ создана — ушёл UUID (D57)"
	fi
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
		[ -z "$tg" ] || bound_check tg_get "$(jq -cn --arg i "$tg" '{id: $i}')" "allow-project: tg_get"
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
		[ -z "$lb" ] || bound_check lb_get "$(jq -cn --arg i "$lb" '{id: $i}')" "allow-project: lb_get"
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

	# close_ports (D37): правило open_ports видно в формате, который сверяет
	# close_ports ("P:P"), закрывается, повтор — already_deleted (D26). Без
	# «видно до закрытия» отсутствие правила после ничего не доказывает: при
	# другом формате port_range close_ports ответил бы already_deleted при
	# открытом порте (Qwen на #315). Подписи проверок «видно до close_ports»
	# и вызовы ниже читает TestWorkflows_RuleSeenBeforeItIsCheckedGone:
	# правка подписи — вместе с тестом.
	if [ -n "$sg" ] && printf '%s\n' "${covered[@]}" | grep -qx open_ports; then
		n=$(rules_left)
		if [ "$n" != 0 ]; then
			ok "правило $RULE_PORT видно до close_ports" "port_range $RULE_PORT:$RULE_PORT, правил $n; port_range правил из $RULE_CIDR: $(port_ranges)"
			if must close_ports close_ports --arg security_group="$sg" --arg ports="[$RULE_PORT]" --arg cidr="$RULE_CIDR"; then
				if [ "$(j -c '(.closed // []) | map(tostring)')" = "[\"$RULE_PORT\"]" ] && poll 120 10 no_rules; then
					tc close_ports --arg security_group="$sg" --arg ports="[$RULE_PORT]" --arg cidr="$RULE_CIDR"
					if [ "$rc" = 0 ] && [ "$(j -r '.status // empty')" = already_deleted ]; then
						ok close_ports "closed, правила нет, повтор — already_deleted"
						cov close_ports
					else bad "close_ports повтор" "код $rc, status $(j -r '.status // empty'), $reason"; fi
				else bad close_ports "closed=$(j -c .closed), правил $RULE_PORT:$RULE_PORT осталось $(rules_left)"; fi
			fi
		else
			bad "правило $RULE_PORT видно до close_ports" "правил $RULE_PORT:$RULE_PORT нет; port_range правил из $RULE_CIDR: $(port_ranges) — close_ports их не найдёт (в реестр расхождений)"
		fi
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
	if poll 300 15 sh -c "evo tools call list_floating_ips --json | jq -e --arg id '$fip' '(.floating_ips | type) == \"array\" and (any(.floating_ips[]; .id == \$id and .state != \"error_deleting\") | not)' >/dev/null"; then
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

# dnslink_state КОГДА ЗОНА — связи приватной зоны и DNS-сервера evo-e2e-dns
# (п. 27): state и число linkedVpcIds зоны, usedIn сервера и упоминает ли
# usedIn эту зону; код HTTP чтения зоны — в $zcode. Печатаются количества
# и признаки, ID — никогда.
dnslink_state() {
	local zs ss
	api GET "https://dns.api.cloud.ru/v1/privateZones/$2"
	zcode=$code
	if [ "$code" = 200 ]; then
		zs="зона: state $(jq -r '.state // "?"' "$tmp/api.json"), linkedVpcIds $(jq '.linkedVpcIds // [] | length' "$tmp/api.json")"
	else zs="зона: HTTP $code"; fi
	api GET "https://dns.api.cloud.ru/v1/dnsServers?projectId=$PROJECT&pageSize=100"
	if [ "$code" = 200 ]; then
		ss="сервер evo-e2e-dns: $(jq -c --arg z "$2" '[.servers[]? | select(.name == "evo-e2e-dns")][0] // null | if . == null then "нет" else {usedIn: (.usedIn // [] | length), keys: ((.usedIn // [])[0] // {} | keys), zone: ([(.usedIn // [])[] | tostring | contains($z)] | any)} end' "$tmp/api.json")"
	else ss="dnsServers: HTTP $code"; fi
	row "п. 27: связи $1" чтение "$zs; $ss"
}

# Проба п. 27 (решение владельца 30.10, #597, п. 27; run 151): приватная
# зона, привязанная к VPC с DNS-сервером, — DELETE принят, зона остаётся
# «ok»; консоль требует снять связи. Перед обычным удалением зоны области:
# D66 — dns_zone_delete со связями отказывает до вопроса (USAGE, без записи;
# до слияния D66 evo шлёт DELETE — строка это и скажет); H3 — прямой DELETE
# со связями и итог его операции (evo с D66 его не шлёт); H1 — снимает ли
# PATCH linkedVpcIds: [] связь с VPC; H2 — снимается ли при этом связь с
# DNS-сервером (usedIn). У evo PATCH зоны нет — прямой запрос. Проба, не
# проверка: строки, а не FAIL. Зона дальше удаляется обычным шагом области;
# связи не снялись — он отказывает (D66): FAIL и остаток, путь — консоль
# (снять связи и удалить).
probe_dnslink() {
	local zid=$1
	iam_token && [ -n "${PROJECT:-}" ] || { gap "п. 27" "нет токена IAM или PROJECT"; return 0; }
	dnslink_state "до" "$zid"
	tc dns_zone_delete --arg id="$zid" --arg type=private
	row "п. 27 D66: dns_zone_delete со связями" "$([ "$rc" = 1 ] && [ "${reason%%,*}" = USAGE ] && echo отказ || echo "код $rc")" "$reason: $(printf '%s' "$why" | cut -c1-160)"
	api DELETE "https://dns.api.cloud.ru/v1/privateZones/$zid"
	dnslink_op "п. 27 H3: DELETE со связями"
	dnslink_state "после DELETE" "$zid"
	[ "$zcode" != 404 ] || return 0
	api PATCH "https://dns.api.cloud.ru/v1/privateZones/$zid" '{"linkedVpcIds":[]}'
	dnslink_op "п. 27 H1: PATCH linkedVpcIds: []"
	dnslink_state "после PATCH" "$zid"
}

# dnslink_op СТРОКА — итог прямого запроса api пробы п. 27: код HTTP и
# итог операции из ответа (.id), или текст ошибки.
dnslink_op() {
	local op
	if [ "${code#2}" = "$code" ]; then row "$1" "HTTP $code" "$(api_why)"; return 0; fi
	op=$(jq -r '.id // empty' "$tmp/api.json")
	mask "$op"
	if [ -z "$op" ]; then row "$1" "HTTP $code" "без операции, ключи $(jq -c 'keys' "$tmp/api.json")"
	elif opwait dns "$op" 300; then row "$1" "HTTP $code" "операция done без ошибки"
	else row "$1" "HTTP $code" "операция: $why"; fi
}

# dns_delete_op ЧТО — итог операции удаления из последнего ответа (j):
# строка, а не провал (провал — объект остался в списке). Операция с ошибкой
# или не done — её состояние пробой: run 151 (30.10) зона после ~40
# принятых удалений осталась «ok», а итог операции не читался.
dns_delete_op() {
	local op
	op=$(j -r '.operation_id // empty')
	[ -n "$op" ] || { row "dns_operation удаления $1" "нет id" "в ответе нет operation_id"; return 0; }
	mask "$op"
	if opwait dns "$op" 300; then ok "dns_operation удаления $1" "done"; else
		row "dns_operation удаления $1" "не done" "за 5 мин: $why"
		probe_dns_op "$op"
	fi
}

area_dns() {
	local domain="$prefix.test." zid rid op nv
	# D65 (1.6, #676; решение владельца #597, п. 25): приватная зона к VPC
	# без DNS-сервера — отказ до вопроса (USAGE), зоны нет. Пустой VPC
	# прогона DNS-сервера не имеет; он удаляется сразу после проверки.
	must "vpc_create (без DNS-сервера)" vpc_create --arg name="$prefix-nodns" --arg description="evo e2e live run" || return 1
	if opwait vpc "$(j -r .operation_id)" 600; then
		nv=$res_id
		[ -n "$nv" ] || nv=$(lst vpc "$prefix-nodns" | cut -f1 | head -1)
		mask "$nv"
		tc dns_zone_create --arg domain="$prefix-nodns.test." --arg type=private --arg vpc_id="$nv"
		if [ "$rc" = 1 ] && [ "${reason%%,*}" = USAGE ] && [ -z "$(lst dns "$prefix-nodns")" ]; then
			ok "dns_zone_create: VPC без DNS-сервера" "USAGE до вопроса, зоны нет (D65)"
		else bad "dns_zone_create: VPC без DNS-сервера" "код $rc, $reason; зон $prefix-nodns: $(lst dns "$prefix-nodns" | wc -l)"; fi
		if [ -n "$nv" ] && must "vpc_delete (без DNS-сервера)" vpc_delete --arg id="$nv"; then
			opwait vpc "$(j -r .operation_id)" 600 || bad "vpc_delete (без DNS-сервера)" "$why"
		fi
	else bad "vpc_create (без DNS-сервера)" "$why"; fi
	must dns_zone_create dns_zone_create --arg domain="$domain" --arg type=private --arg description="evo e2e live run" || return 1
	op=$(j -r .operation_id)
	cov dns_zone_create
	# 18.10 (run 39152048559) операция создания приватной зоны не стала
	# done за 10 мин, хотя зона появилась в списке: ждём зону в списке
	if opwait dns "$op" 300; then ok "dns_operation создания" "done"; else
		row "dns_operation создания" "не done" "за 5 мин: $why"
		probe_dns_op "$op"
	fi
	poll 300 15 sh -c "evo tools call dns_zones --json | jq -e --arg d '$domain' 'any(.zones[]; (.domain // .name) == \$d)' >/dev/null" || true
	zid=$(lst dns "$prefix" | cut -f1 | head -1)
	zid=${zid#* }
	mask "$zid"
	[ -n "$zid" ] || { bad dns_zone_create "зоны нет в dns_zones"; return 1; }
	ok dns_zone_create "зона в dns_zones: $(evo tools call dns_zones --json | jq -c --arg id "$zid" '.zones[] | select(.id == $id) | {visibility, valid, delegated}')"
	must dns_zone_get dns_zone_get --arg id="$zid" --arg type=private && ok dns_zone_get "$(j -r '.visibility // .type // "прочитана"')"
	bound_check dns_zone_get "$(jq -cn --arg i "$zid" '{id: $i, type: "private"}')" "allow-project: dns_zone_get" own
	tc dns_zone_get --arg id="$zid"
	row "dns_zone_get без type" "$([ "$rc" = 0 ] && echo ответ || echo отказ)" "код $rc, $reason"
	must dns_record_create dns_record_create --arg zone_id="$zid" --arg type=private --arg name=www --arg record_type=A --arg value=198.18.255.10 --arg ttl=300 || return 1
	cov dns_record_create
	op=$(j -r .operation_id)
	if opwait dns "$op" 300; then ok "dns_operation записи" "done"; else
		row "dns_operation записи" "не done" "за 5 мин: $why"
		probe_dns_op "$op"
	fi
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
	bound_check dns_record_get "$(jq -cn --arg i "$rid" '{record_id: $i, type: "private"}')" "allow-project: dns_record_get" own
	must dns_record_delete dns_record_delete --arg record_id="$rid" --arg type=private || return 1
	dns_delete_op записи
	norec() { evo tools call dns_records --arg zone_id="$zid" --arg type=private --json 2>/dev/null | jq -e --arg id "$rid" 'any(.records[]; .id == $id) | not' >/dev/null; }
	if poll 300 15 norec; then ok dns_record_delete "записи нет"; cov dns_record_delete; else bad dns_record_delete "запись осталась через 5 мин"; fi
	! probe_on dnslink || probe_dnslink "$zid"
	# Зона без vpc_id связана с VPC по умолчанию (D65), её удаление — отказ
	# до вопроса (D66): это проверка, а не провал; затем отвязка и удаление.
	tc dns_zone_delete --arg id="$zid" --arg type=private
	if dns_linked; then
		ok "dns_zone_delete со связью с VPC" "USAGE до вопроса (D66)"
		if dns_unlink "$zid"; then ok "отвязка зоны от VPC (API, PATCH linkedVpcIds)" "done"; else
			bad "отвязка зоны от VPC (API, PATCH linkedVpcIds)" "$why"
			return 1
		fi
		must dns_zone_delete dns_zone_delete --arg id="$zid" --arg type=private || return 1
	elif [ "$rc" != 0 ]; then
		bad dns_zone_delete "код $rc, $reason: $why"
		return 1
	fi
	dns_delete_op зоны
	# приватная зона 18.10 удалялась ~11,5 мин (run 39160570296)
	local t0=$SECONDS
	if poll 1500 20 gone dns "$prefix"; then
		ok dns_zone_delete "зоны нет через $(((SECONDS - t0) / 60)) мин"
		cov dns_zone_delete
	else bad dns_zone_delete "зона в списке через 25 мин: $(evo tools call dns_zones --json | jq -c --arg d "$domain" '[.zones[] | select((.domain // .name) == $d) | {state, visibility}]')"; fi
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
	rand_secret
	v1=$SECRET_VALUE
	rand_secret
	v2=$SECRET_VALUE
	umask 077
	jq -n --arg p "$path" --arg v "$v1" '{path: $p, value: $v, description: "evo e2e live run"}' >"$tmp/s.json"
	must secret_create secret_create --args-file "$tmp/s.json" || return 1
	[ "$(j -r '.secret | tostring | contains("'"$v1"'")')" = false ] && ok secret_create "значение в ответе не вернулось" && cov secret_create || bad secret_create "значение в ответе"
	got=$(evo --yes tools call secret_value --arg path="$path" --json 2>/dev/null | jq -r '.value // empty')
	[ "$(sha "$got")" = "$(sha "$v1")" ] && ok secret_value "v1 совпало (sha256)" || bad secret_value "v1 не совпало"
	jq -n --arg p "$path" --arg v "$v2" '{path: $p, value: $v}' >"$tmp/s.json"
	must secret_add_version secret_add_version --args-file "$tmp/s.json" || return 1
	rm -f "$tmp/s.json"
	got=$(evo --yes tools call secret_value --arg path="$path" --json 2>/dev/null | jq -r '.value // empty')
	[ "$(sha "$got")" = "$(sha "$v2")" ] && ok secret_add_version "последняя версия — v2 (sha256)" && cov secret_add_version || bad secret_add_version "последняя версия не v2"
	must secret_get secret_get --arg path="$path" && ok secret_get "метаданные прочитаны"
	[ -n "$(lst secret "$path")" ] && ok secret_list "путь в списке" || bad secret_list "пути нет в secret_list"
	must secret_delete secret_delete --arg path="$path" || return 1
	if poll 120 10 gone secret "$path"; then ok secret_delete "секрета нет"; cov secret_delete; else bad secret_delete "секрет в списке"; fi
	tc secret_value --arg path="$path"
	[ "$rc" != 0 ] && ok "secret_value удалённого" "код $rc, $reason" || bad "secret_value удалённого" "значение вернулось"
	# Проба P44 (пункт Ж; FB15-28): секрет в папке $prefix, которой не было,
	# создан (выше) — остаётся ли папка после удаления секрета. Осталась —
	# строка и удаление папки прямым DELETE (у evo инструмента папок нет).
	# Папка — только своя: путь собран из префикса прогона области secrets
	# (evo-e2e-secrets-<run>, run — [a-z0-9]), чужую папку проекта DELETE не
	# задевает (удаление чужого — только владелец, CLAUDE.md §4).
	if probe_on p44; then
		case "$prefix" in evo-e2e-secrets-[a-z0-9]*) ;; *)
			bad P44 "префикс $prefix не evo-e2e-secrets-<run> — папку не трогаю"
			return 1
			;;
		esac
		if iam_token && [ -n "${PROJECT:-}" ]; then
			api GET "https://secretmanager.api.cloud.ru/v2/folder/$prefix?projectId=$PROJECT"
			case "$code" in
			200)
				api DELETE "https://secretmanager.api.cloud.ru/v2/folder/$prefix?projectId=$PROJECT"
				ok "P44: папка после удаления секрета" "осталась (HTTP 200); удаление папки — HTTP $code"
				;;
			404) ok "P44: папка после удаления секрета" "нет (HTTP 404)" ;;
			*) ok "P44: папка после удаления секрета" "HTTP $code: $(api_why)" ;;
			esac
		else gap P44 "нет токена IAM или PROJECT"; fi
	fi

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
	bound_check cert_get "$(jq -cn --arg i "$cid" '{id: $i}')" "allow-project: cert_get"
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
	local reg="evo-e2e-ca-$RUN" rid host app="$prefix-app" url code img n
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
	app_url() {
		evo tools call ca_get --arg name="$app" --json 2>/dev/null >"$tmp/caget" || return 0
		# публичный адрес — configuration.ingress.publicUri (run 39164767739:
		# первым в ответе идёт internalUri, снаружи недоступный)
		jq -r '(.configuration.ingress.publicUri // empty) | if . == "" then . elif test("^https?://") then . else "https://" + . end' "$tmp/caget"
	}
	ca_shape() { jq -c '{keys: keys, status: (.status // .state // null), strings: [paths(strings) | map(tostring) | join(".")] | .[0:40]}' "$tmp/caget" 2>/dev/null | redact | cut -c1-600; }
	app_up() {
		url=$(app_url)
		[ -n "$url" ] || return 1
		mask "$url" "${url#https://}"
		code=$(curl -s -o "$tmp/page" -w '%{http_code}' --max-time 20 "$url" || echo 000)
		[ "$code" = 200 ] && grep -qi nginx "$tmp/page"
	}
	if poll 900 20 app_up; then ok "ca_get URL и HTTP" "200, nginx"; else bad "ca_get URL и HTTP" "за 15 мин нет ответа 200 (код ${code:-нет URL}); ca_get: $(ca_shape)"; fi
	# 18.10: 0 строк сразу после обслуженных запросов; задержка поставки
	# логов не описана — пустой ответ повторяем через минуту
	must ca_logs ca_logs --arg name="$app" && {
		n=$(j -r '(.logs // []) | length')
		if [ "$n" = 0 ]; then
			sleep 60
			must ca_logs ca_logs --arg name="$app" && n="0, через 60 с $(j -r '(.logs // []) | length')"
		fi
		ok ca_logs "строк $n$(j -r 'if .truncated then ", truncated" else "" end')"
	}
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
	rand_secret
	sv=$SECRET_VALUE
	umask 077
	jq -n --arg n "$prefix" --arg i "$IMAGE" --arg z "$ZONE" --arg f "$FLAVOR" --arg s "$sv" '{manifest: {
		name: $n,
		vm: {private: true, image: $i, zone: $z, flavor: $f},
		secrets: [{path: ($n + "/token"), value: $s}],
		nlb: {name: ($n + "-lb"), port: 80, external: false}
	}}' >"$tmp/m.json"
	# D64 (1.6): тот же стек с публичной записью DNS на внутренний
	# балансировщик (dns.target по умолчанию — lb, nlb.external: false) —
	# отказ stack_plan до вопроса (USAGE) и до чтений облака, задания нет.
	# zone_id — нулевой UUID: проверка идёт раньше, чем evo читает зону.
	jq '.manifest.dns = {zone_id: "00000000-0000-0000-0000-000000000000", name: "app"}' "$tmp/m.json" >"$tmp/m-dns.json"
	tc stack_plan --args-file "$tmp/m-dns.json"
	rm -f "$tmp/m-dns.json"
	if [ "$rc" = 1 ] && [ "${reason%%,*}" = USAGE ] && [ -z "$(j -r '.job_id // empty')" ]; then
		ok "stack_plan: DNS на внутренний балансировщик" "USAGE до вопроса, задания нет (D64)"
	else bad "stack_plan: DNS на внутренний балансировщик" "код $rc, $reason, job_id: $(j -r 'if .job_id then "есть" else "нет" end')"; fi
	must stack_plan stack_plan --args-file "$tmp/m.json" || return 1
	rm -f "$tmp/m.json"
	job=$(j -r .job_id)
	ok stack_plan "шагов $(j -r '.steps | length'), стоимость $(j -c '.cost.monthly_rub // .cost.total_monthly_rub // .cost' | cut -c1-60)"
	must stack_apply stack_apply --arg job_id="$job" || return 1
	cov stack_apply
	st=$(j -r .status)
	local until=$(($(date +%s) + 2700))
	while [ "$st" != "done" ] && [ "$st" != error ] && [ "$(date +%s)" -lt "$until" ]; do
		# wait_seconds — не больше 20 (maximum схемы stack_status, D69)
		tc stack_status --arg job_id="$job" --arg wait_seconds=20
		st=$(j -r '.status // "?"')
		echo "stack_status: $st, готово $(j -r '.done | length'), идёт $(j -r '.running | length'), ждёт $(j -r '.pending | length')" | redact
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
	local repeats=0
	for _ in 1 2 3 4 5 6; do
		[ "$st" = destroyed ] && [ "$(j -r '(.deleting // []) | length')" = 0 ] && break
		repeats=$((repeats + 1))
		echo "stack_destroy: $st, удалено $(j -c '.deleted // []'), удаляется $(j -c '.deleting // []'), не удалено $(j -c '[.failed[]? | if type == "object" then (.step // .key // .name) else . end]')${why:+; код $rc: $why}" | redact
		sleep 60
		tc stack_destroy --arg job_id="$job"
		st=$(j -r '.status // "?"')
	done
	if [ "$st" = destroyed ]; then
		ok stack_destroy "удалено $(j -r '.deleted | length')"
		# повторный вызов, пока ресурсы удаляются, не должен ломать задание (D6, #118)
		[ "$repeats" = 0 ] || ok "stack_destroy повторный ($repeats)" "destroyed: $(j -c '.deleted // []')"
	else
		bad stack_destroy "status $st, удалено $(j -c '.deleted // []'), не удалено: $(j -c '[.failed[]? | if type == "object" then {step: (.step // .key // .name), error: ((.error // .reason // "") | tostring | .[0:200])} else . end]' | redact | cut -c1-500)${why:+; код $rc: $why}"
	fi
	tc stack_status --arg job_id="$job"
	[ "$(j -r .status)" = destroyed ] && ok "stack_status после destroy" destroyed || bad "stack_status после destroy" "$(j -r .status)"
	# остатки (ВМ, балансировщик, группа, секрет) проверяет уборка по префиксу
}

# Откуда доступен кластер PostgreSQL (реестр DB-5: документация говорит
# то «из той же подсети», то «из той же VPC»; план cloud-e2e rc.1 1.6,
# пункт А). Job dbaas-pg, кластер READY: PG_CLUSTER_ID и PG_SUBNET_ID —
# кластер и его подсеть. ВМ A — в подсети кластера, ВМ B — в новой подсети
# основного VPC проекта (subnet_create без vpc_id); с каждой — TCP на 5432
# адреса из connection_string, три попытки. Итог — строки «N из 3»: это
# ответ платформы, а не evo, и не провал; провал — шаги evo (ВМ, подсеть,
# удаление). Подсеть кластера не основная (не Default_<зона>) — VPC не
# сверен: не покрыто. Вывод о платформе — из двух прогонов (CLAUDE.md §0).
area_pgnet() {
	local zid cs host sname other cidr a b ip_a ip_b user n
	[ -n "${PG_CLUSTER_ID:-}" ] && [ -n "${PG_SUBNET_ID:-}" ] || { bad pgnet "нет PG_CLUSTER_ID или PG_SUBNET_ID"; return 1; }
	mask "$PG_CLUSTER_ID" "$PG_SUBNET_ID"
	zid=$(zone_id)
	[ -n "$zid" ] || { bad "зона $ZONE" "не найдена"; return 1; }
	mkdir -p "$tmp/pgnet" && cd "$tmp/pgnet"

	must pg_get pg_get --arg cluster_id="$PG_CLUSTER_ID" || return 1
	cs=$(j -r '.connection_string // empty')
	# Строка подключения бывает с паролем (postgresql://user:pass@host…):
	# в лог она не идёт (печатается только её тип), маска — второй рубеж.
	mask "$cs"
	host=$(printf '%s' "$cs" | grep -oE 'host=[^ ;,]+' | head -1 | cut -d= -f2)
	[ -n "$host" ] || host=$(printf '%s' "$cs" | grep -oE '@[^:/@ ]+' | head -1 | cut -c2-)
	[ -n "$host" ] || host=$(printf '%s' "$cs" | grep -oE '^[A-Za-z0-9.-]+(:[0-9]+)?$' | cut -d: -f1)
	[ -n "$host" ] || host=$(printf '%s' "$cs" | sed -nE 's#^[a-z]+://([^:/@?]+).*#\1#p' | head -1)
	mask "$host"
	if [ -z "$host" ]; then
		# Форма строки без значений (буквы и цифры — x) и ключи ответа: по
		# ним следующий прогон разберёт адрес (run 150 — «не разобран»).
		gap "DB-5: адрес кластера" "connection_string не разобран: форма «$(printf '%s' "$cs" | tr 'A-Za-z0-9' 'x' | tr -s 'x' | cut -c1-120)», ключи pg_get $(j -c 'keys' | cut -c1-300)"
		return 0
	fi
	sname=$(evo tools call list_subnets --json | jq -r --arg id "$PG_SUBNET_ID" '[.subnets[]? | select(.id == $id) | .name // ""][0] // ""')
	case "$sname" in Default_*) ;; *)
		gap "DB-5: подсеть кластера" "не основная подсеть зоны — её VPC прогон не сверяет"
		return 0
		;;
	esac

	for cidr in 10.254.240.0/28 10.254.241.0/28 10.254.242.0/28; do
		tc subnet_create --arg name="$prefix-sn" --arg cidr="$cidr" --arg zone="$ZONE"
		[ "$rc" = 0 ] && break
		row "subnet_create $cidr" отказ "код $rc, $reason: $(printf '%s' "$why" | cut -c1-160)"
	done
	[ "$rc" = 0 ] || { bad "subnet_create (вторая подсеть VPC)" "три сети отвергнуты"; return 1; }
	other=$(j -r '.subnet_id // empty')
	mask "$other"
	ok "subnet_create (вторая подсеть VPC)" "$cidr"

	a="$prefix-a"
	b="$prefix-b"
	must "provision в подсети кластера" provision --arg name="$a" --arg region="$zid" --arg image="$IMAGE" --arg flavor="$FLAVOR" --arg subnet_id="$PG_SUBNET_ID" || return 1
	ip_a=$(j -r '.vm_ip // empty')
	user=$(j -r '.ssh_user // "user1"')
	mask "$(j -r '.vm_id // empty')" "$ip_a"
	must "provision в другой подсети" provision --arg name="$b" --arg region="$zid" --arg image="$IMAGE" --arg flavor="$FLAVOR" --arg subnet_id="$other" || return 1
	ip_b=$(j -r '.vm_ip // empty')
	mask "$(j -r '.vm_id // empty')" "$ip_b"
	[ -n "$ip_a" ] && [ -n "$ip_b" ] || { bad provision "нет vm_ip у ВМ"; return 1; }
	cov provision

	# reach IP — TCP на 5432 кластера с ВМ: «N из 3» в $n.
	reach() {
		local ip=$1 i
		n=0
		if ! poll 600 15 ssh -i .evo/id_ed25519 -o BatchMode=yes -o StrictHostKeyChecking=accept-new \
			-o UserKnownHostsFile="$tmp/known_hosts" -o ConnectTimeout=10 "$user@$ip" true 2>/dev/null; then
			n=-
			return 0
		fi
		for i in 1 2 3; do
			if ssh -i .evo/id_ed25519 -o BatchMode=yes -o UserKnownHostsFile="$tmp/known_hosts" -o ConnectTimeout=10 "$user@$ip" \
				"timeout 5 bash -c '</dev/tcp/$host/5432'" 2>/dev/null; then n=$((n + 1)); fi
			[ "$i" = 3 ] || sleep 10
		done
	}
	reach "$ip_a"
	if [ "$n" = - ]; then bad "SSH к ВМ в подсети кластера" "не поднялся за 10 мин"; else ok "DB-5: доступ из подсети кластера" "TCP 5432: $n из 3"; fi
	reach "$ip_b"
	if [ "$n" = - ]; then bad "SSH к ВМ в другой подсети" "не поднялся за 10 мин"; else ok "DB-5: доступ из другой подсети того же VPC" "TCP 5432: $n из 3"; fi

	must "destroy ВМ A" destroy --arg name="$a" || return 1
	must "destroy ВМ B" destroy --arg name="$b" || return 1
	cov destroy
	sn_del() { tc subnet_delete --arg subnet_id="$other"; [ "$rc" = 0 ]; }
	if poll 600 15 sn_del; then ok "subnet_delete (вторая подсеть)" "$(j -r '.status // empty')"; else bad "subnet_delete (вторая подсеть)" "код $rc, $reason: $why"; fi
	# остатки (ВМ, адреса, подсеть) проверяет уборка по префиксу
}

# MCP по stdio (deploy_plan → deploy_apply → deploy → destroy) и по HTTP
# (provision → destroy), гейт подтверждения на обоих. По stdio каждый вызов —
# свой сеанс (mcp_stdio.sh) в пределе MCP_LIMIT: deploy_apply и deploy по
# MCP не ждут ВМ дольше ~45 с (D69, H14) — ответ creating или deploying со
# следующим шагом, area следует ему до готовности (mcp_follow), как агент.
area_mcp() {
	local zid vm="$prefix-vm" ip token sess vid
	zid=$(zone_id)
	[ -n "$zid" ] || { bad "зона $ZONE" "не найдена"; return 1; }
	mkdir -p "$tmp/mcp" && cd "$tmp/mcp"
	printf 'services:\n  web:\n    image: nginx:alpine\n    ports:\n      - "%s:80"\n' "$MCP_PORT" >docker-compose.yml
	# Ответ инструмента — в $tmp/r.json (его читает j), isError — в $iserr.
	# План deploy_plan живёт в файле каталога — следующий сеанс его видит,
	# как у клиента после перезапуска.
	local MCP_WORK=$tmp MCP_TOOLSETS='' MCP_SESSION_LIMIT=$((MCP_LIMIT + 30))
	export MCP_WORK MCP_TOOLSETS MCP_SESSION_LIMIT
	stdio() {
		mcp_call "$@"
		iserr=$MCP_ISERR
		[ "$MCP_TOOK" -le "$MCP_LIMIT" ] || bad "stdio $1: предел вызова" "сеанс $MCP_TOOK с — дольше $MCP_LIMIT с (D69)"
	}
	# follow ИМЯ АРГУМЕНТЫ — вызов и повторы по next_tool до ответа не в
	# работе; каждый сеанс в пределе, следующий шаг — тот же инструмент.
	follow() {
		mcp_follow "$1" "$2" "$MCP_FOLLOW_MAX"
		iserr=$MCP_ISERR
		echo "stdio $1: вызовов $MCP_CALLS, самый долгий сеанс $MCP_MAXTOOK с, итог $MCP_FOLLOW, status $(j -r '.status // "-"')"
		[ "$MCP_MAXTOOK" -le "$MCP_LIMIT" ] || bad "stdio $1: предел вызова" "сеанс $MCP_MAXTOOK с — дольше $MCP_LIMIT с (D69)"
		[ "$MCP_FOLLOW" = "done" ] || bad "stdio $1: следующий шаг" "$MCP_FOLLOW после $MCP_CALLS вызовов: $(j -c '{status, next_tool}' | redact)"
	}
	stdio deploy_plan "$(jq -cn --arg n "$vm" --arg z "$ZONE" --arg i "$IMAGE" --arg f "$FLAVOR" --argjson p "$MCP_PORT" \
		'{mode:"manual", name:$n, product:"evo-e2e", zone:$z, image:$i, flavor:$f, compose_file:"docker-compose.yml", ports:[$p]}')"
	local plan
	plan=$(j -r '.plan_id // empty')
	[ "$iserr" = false ] && [ -n "$plan" ] && ok "stdio deploy_plan" "₽/мес $(j -r '.cost.total_monthly_rub // "?"')" || { bad "stdio deploy_plan" "isError $iserr, $(j -r '.reason // .error // empty' | redact | cut -c1-200)"; return 1; }
	stdio deploy_apply "$(jq -cn --arg p "$plan" '{plan_id:$p}')"
	[ "$(j -r .status)" = confirmation_required ] && [ -z "$(lst vm "$vm")" ] && ok "stdio deploy_apply без confirmed" "confirmation_required, ВМ нет" || bad "stdio deploy_apply без confirmed" "status $(j -r .status)"
	follow deploy_apply "$(jq -cn --arg p "$plan" '{plan_id:$p, confirmed:true}')"
	vid=$(j -r '.vm_id // empty')
	ip=$(j -r '.public_ip // empty')
	mask "$vid" "$ip"
	[ "$iserr" = false ] && [ -n "$vid" ] && [ "$(j -r .status)" != creating ] && ok "stdio deploy_apply" "status $(j -r .status), вызовов $MCP_CALLS" && cov deploy_apply || { bad "stdio deploy_apply" "isError $iserr, $(j -r '.reason // .error // empty' | redact | cut -c1-200)"; return 1; }
	follow deploy "$(jq -cn --arg r "$zid" --arg i "$IMAGE" --arg f "$FLAVOR" '{confirmed:true, region:$r, image:$i, flavor:$f}')"
	if [ "$iserr" = false ] && [ "$(j -r .status)" = ok ]; then
		cov deploy
		ip=${ip:-$(evo tools call vm_get --arg vm_id="$vid" --json | jq -r '.public_ip // empty')}
		mask "$ip"
		# Сеть ВМ сразу после deploy и при отказе HTTP (mcp 186–192: HTTP нет,
		# logs — SSH_TIMEOUT, класс не установлен): ВМ, адрес, правила группы
		# из state на 22 и на порт приложения, ответ портов с раннера.
		local sg
		sg=$(jq -r '.deployments[0].security_group_id // empty' .evo/state.json 2>/dev/null)
		mask "$sg"
		net_diag() {
			echo "$1:"
			"$here/ssh_diag.sh" "$vid" "$ip" "$sg"
			SSH_PORT=$MCP_PORT RULE_PORT=$MCP_PORT "$here/ssh_diag.sh" "$vid" "$ip" "$sg"
		}
		net_diag "сеть ВМ сразу после deploy"
		if poll 300 10 sh -c "curl -fsS --max-time 10 'http://$ip:$MCP_PORT/' | grep -qi nginx"; then ok "stdio deploy" "HTTP $MCP_PORT: nginx"; else
			local probe
			probe=$(j -r '.log // ""' | { grep -E ':[0-9]+ -> ' || true; } | redact | tr '\n' ' ' | cut -c1-200)
			local tail
			tail=$(j -r '.log // ""' | grep -v '^[[:space:]]*$' | tail -n 4 | redact | tr '\n' '|' | cut -c1-300)
			bad "stdio deploy" "нет HTTP на $MCP_PORT за 5 мин; проба evo в ответе deploy: ${probe:-строк пробы нет}; конец log deploy: ${tail:-пусто}"
			net_diag "сеть ВМ после 5 мин без HTTP"
		fi
		[ "$(lst vm "$vm" | wc -l)" = 1 ] && ok "deploy после deploy_apply" "та же ВМ, второй нет" || bad "deploy после deploy_apply" "ВМ с именем $vm: $(lst vm "$vm" | wc -l); всего ВМ $prefix*: $(lst vm "$prefix" | wc -l)"
	else bad "stdio deploy" "status $(j -r '.status // "-"'), $(j -r '.reason // "-"'): $(j -r '.error // empty' | redact | cut -c1-200)"; fi
	bound_check vm_get "$(jq -cn --arg v "$vid" '{vm_id: $v}')" "allow-project: vm_get" own
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
			-H "Authorization: Bearer $token" ${sess:+-H "Mcp-Session-Id: $sess"} --max-time 2400 -w '%{http_code}' -d "$1" http://127.0.0.1:18090/mcp >"$tmp/hcode" || true
		if grep -q '^data: ' "$tmp/b"; then sed -n 's/^data: //p' "$tmp/b"; else cat "$tmp/b"; fi
	}
	sess=
	hpost '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"cloud-live","version":"1"}}}' >/dev/null
	sess=$(awk 'tolower($1)=="mcp-session-id:"{print $2}' "$tmp/h" | tr -d '\r')
	hpost '{"jsonrpc":"2.0","method":"notifications/initialized"}' >/dev/null
	hcall() {
		hpost "$(jq -cn --arg n "$1" --argjson a "$2" '{jsonrpc:"2.0", id:9, method:"tools/call", params:{name:$n, arguments:$a}}')" >"$tmp/res"
		# поток SSE: до ответа (id 9) идут уведомления о ходе — берём ответ
		jq -c 'select(.id == 9)' "$tmp/res" >"$tmp/res9" 2>/dev/null || : >"$tmp/res9"
		echo "http $1: HTTP $(cat "$tmp/hcode" 2>/dev/null), сообщений в ответе $(jq -s length "$tmp/res" 2>/dev/null || echo "?"), методы уведомлений $(jq -sc '[.[] | .method // empty] | unique' "$tmp/res" 2>/dev/null), ключи ответа $(jq -c 'keys' "$tmp/res9" 2>/dev/null), ошибка JSON-RPC $(jq -c '.error // null | if . == null then null else {code, message: (.message | tostring | .[0:200])} end' "$tmp/res9" 2>/dev/null)" | redact
		# не «.result.isError // "?"»: в jq // считает false пустым
		iserr=$(jq -r 'if .result == null then "?" else (.result.isError == true | tostring) end' "$tmp/res9" 2>/dev/null || echo "?")
		jq -r '.result.content[0].text // empty' "$tmp/res9" >"$tmp/r.json" 2>/dev/null || : >"$tmp/r.json"
	}
	# hfollow ИМЯ АРГУМЕНТЫ — hcall и повторы по next_tool, пока ответ
	# creating/deploying/deleting того же инструмента (D69, H14: по HTTP —
	# тот же предел ~45 с); $hcalls — сколько вызовов.
	hfollow() {
		local name=$1 args=$2
		hcalls=0
		while :; do
			hcall "$name" "$args"
			hcalls=$((hcalls + 1))
			case "$iserr/$(j -r '.status // empty')" in false/creating | false/deploying | false/deleting) ;; *) return 0 ;; esac
			[ "$(j -r '.next_tool // empty')" = "$name" ] || return 0
			args=$(j -c '.next_args // empty')
			[ -n "$args" ] && [ "$hcalls" -lt "$MCP_FOLLOW_MAX" ] || return 0
			sleep 5
		done
	}
	local hvm="$prefix-http"
	hcall provision "$(jq -cn --arg n "$hvm" --arg r "$zid" --arg i "$IMAGE" --arg f "$FLAVOR" '{name:$n, region:$r, image:$i, flavor:$f}')"
	[ "$(j -r .status)" = confirmation_required ] && [ -z "$(lst vm "$hvm")" ] && ok "http provision без confirmed" "confirmation_required, ВМ нет" || bad "http provision без confirmed" "status $(j -r .status)"
	hfollow provision "$(jq -cn --arg n "$hvm" --arg r "$zid" --arg i "$IMAGE" --arg f "$FLAVOR" '{name:$n, region:$r, image:$i, flavor:$f, confirmed:true}')"
	mask "$(j -r '.vm_id // empty')" "$(j -r '.vm_ip // empty')"
	[ "$iserr" = false ] && [ "$(j -r .status)" = ok ] && ok "http provision" "ВМ создана, вызовов $hcalls" && cov provision || bad "http provision" "isError $iserr, $(j -r '.reason // "-"'): $(j -r '.error // empty' | redact | cut -c1-200)"
	hfollow destroy "$(jq -cn --arg n "$hvm" '{name:$n, confirmed:true}')"
	[ "$iserr" = false ] && ok "http destroy name" "$(j -r .status), вызовов $hcalls" || bad "http destroy name" "isError $iserr, $(j -r '.reason // "-"')"
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

# --- Область ops: пробы операций второго дня (план 1.9, раздел 1.3) ----------
# Кода этих операций в evo ещё нет: проба шлёт запросы прямо, по описи
# docs/internal/probes/requests-1.9.md, и пишет ответ платформы — код HTTP,
# имена полей, состояния; значения, id и адреса — нет. Строка пробы — ответ
# платформы, а не провал: вывод делается из двух прогонов. Область идёт
# только по словам входа probes (g11…), своих проверок evo у неё нет;
# ресурсы — evo-e2e-ops-<прогон>-*, уборка — sweep области (виды —
# area_kinds ops: проба без уборки своего вида ресурса не запускается).
C1=https://compute.api.cloud.ru/api/v1
# keys_of — имена полей верхнего уровня последнего ответа api.
keys_of() { jq -c 'if type == "object" then keys else type end' "$tmp/api.json" 2>/dev/null || echo '"не JSON"'; }

# Проба Г-11: своя группа безопасности и её правила. Группа — без
# интерфейсов, правило «весь интернет» в ней никого не открывает. Что
# записывается: коды создания группы и правил (TCP, UDP, ICMP, вход и выход,
# источник CIDR и 0.0.0.0/0), что группа хранит как источник «весь
# интернет», ответ на повтор того же правила и число правил после него,
# коды удаления правила и его повтора, удаления группы и его повтора.
# ICMP идёт с пустым port_range: поле обязательно, формат для ICMP
# спецификация не называет — отказ и его текст и есть ответ.
probe_g11() {
	local zid=$1 sg rule by_group by_none any_made first='' made='' n0 n1 r t0 dir proto ports cidr
	iam_token || { gap "Г-11" "нет токена IAM (KEY_ID, SECRET)"; return 0; }
	[ -n "${PROJECT:-}" ] || { gap "Г-11" "нет PROJECT"; return 0; }
	api POST "$C1/security-groups" "$(jq -nc --arg p "$PROJECT" --arg n "$prefix-sg" --arg z "$zid" \
		'{project_id: $p, name: $n, availability_zone_id: $z, description: "evo e2e probe"}')"
	case "$code" in 2??) ;; *)
		refusal "Г-11: создание группы" синхронно
		return 0
		;;
	esac
	sg=$(jq -r '.id? // empty' "$tmp/api.json" 2>/dev/null) || sg=
	ok "Г-11: создание группы" "HTTP $code, поля ответа: $(keys_of)"
	[ -n "$sg" ] || sg=$(lst sg "$prefix-sg" | cut -f1 | head -1) || sg=
	[ -n "$sg" ] || { gap "Г-11: группа" "id не прочитан — остаток уберёт уборка области"; return 0; }
	mask "$sg"
	# Группа принимает правила не сразу: первое правило сразу после её
	# создания получило отказ по состоянию группы (прогон 207).
	t0=$SECONDS
	ops_settle "$C1/security-groups/$sg" 120 || true
	ok "Г-11: готовность группы" "состояния: $chain, $((SECONDS - t0)) с"
	rule_body() {
		jq -nc --arg d "$1" --arg pr "$2" --arg ports "$3" --arg c "$4" \
			'{direction: $d, ether_type: "IPv4", ip_protocol: $pr, port_range: $ports, remote_ip_prefix: $c, description: "evo e2e probe"}'
	}
	while read -r dir proto ports cidr; do
		api POST "$C1/security-groups/$sg/rules" "$(rule_body "$dir" "$proto" "$ports" "$cidr")"
		rule=$(jq -r '.id? // empty' "$tmp/api.json" 2>/dev/null) || rule=
		mask "$rule"
		# Первое созданное правило: его удаляет проба и его же повторяет.
		[ -n "$first" ] || [ -z "$rule" ] || { first=$rule made="$dir $proto $ports $cidr"; }
		ok "Г-11: правило $dir $proto $ports $([ "$cidr" = 0.0.0.0/0 ] && echo "весь интернет" || echo CIDR)" \
			"HTTP $code$(case "$code" in 2??) echo ", поля ответа: $(keys_of)" ;; *) echo ": $(api_why)" ;; esac)"
	done <<-RULES
		ingress tcp 18090:18090 198.18.255.1/32
		ingress udp 18090:18090 198.18.255.1/32
		ingress icmp any 198.18.255.1/32
		egress tcp 18090:18090 198.18.255.1/32
		ingress tcp 18091:18091 0.0.0.0/0
	RULES
	# Правило с группой-источником (сама группа пробы): в какой форме
	# чтение отдаёт источник — объектом remote_security_group или плоским
	# полем — и что при этом стоит в remote_ip_prefix (данные «разработки A»
	# к sg_rule_delete: от формы зависит, назовёт ли evo источник группой).
	api POST "$C1/security-groups/$sg/rules" "$(jq -nc --arg g "$sg" \
		'{direction: "ingress", ether_type: "IPv4", ip_protocol: "tcp", port_range: "18092:18092", remote_security_group_id: $g, description: "evo e2e probe"}')"
	rule=$(jq -r '(if type == "array" then .[0] else . end) | .id? // empty' "$tmp/api.json" 2>/dev/null) || rule=
	mask "$rule"
	case "$code" in
	2??)
		r="HTTP $code"
		if [ -n "$rule" ]; then
			api GET "$C1/security-groups/$sg/rules/$rule"
			r="$r; чтение по id: $(case "$code" in 2??) echo "источник в ответе чтения по id: $(source_kind); remote_ip_prefix: $(prefix_kind)" ;; *) brief ;; esac)"
		fi
		ok "Г-11: правило с группой-источником" "$r"
		;;
	*) ok "Г-11: правило с группой-источником" "$(brief)" ;;
	esac
	# Правило «откуда угодно» без источника вовсе — ни адреса, ни группы:
	# что платформа хранит в remote_ip_prefix — пустое значение или
	# 0.0.0.0/0. От этого зависит, читать ли правило без префикса как «для
	# любого адреса» (sgrules.Coverage, строка плана 1.9). Группа пробы ни к
	# чему не привязана; правило уходит вместе с ней.
	api POST "$C1/security-groups/$sg/rules" "$(jq -nc \
		'{direction: "ingress", ether_type: "IPv4", ip_protocol: "tcp", port_range: "18093:18093", description: "evo e2e probe"}')"
	rule=$(jq -r '(if type == "array" then .[0] else . end) | .id? // empty' "$tmp/api.json" 2>/dev/null) || rule=
	mask "$rule"
	case "$code" in
	2??)
		r="HTTP $code, remote_ip_prefix в ответе создания: $(prefix_kind)"
		if [ -n "$rule" ]; then
			api GET "$C1/security-groups/$sg/rules/$rule"
			r="$r; чтение по id: $(case "$code" in 2??) echo "remote_ip_prefix в чтении по id: $(prefix_kind); remote_security_group: $(jq -r '.remote_security_group? | if . == null then "null" else "объект" end' "$tmp/api.json" 2>/dev/null || echo '?')" ;; *) brief ;; esac)"
		fi
		ok "Г-11: правило без источника" "$r"
		;;
	*) ok "Г-11: правило без источника" "$(brief)" ;;
	esac
	# Правило «все порты» (гипотеза Г5 дельта-аудита 1.9): что платформа
	# хранит в port_range — от этого зависит, посчитает ли evo правило
	# открывающим порт. «any» — значение, которое платформа приняла у icmp;
	# для tcp оно не описано. Источник — тестовый адрес, не интернет; группа
	# ни к чему не привязана. Ответ создания здесь, список — ниже.
	api POST "$C1/security-groups/$sg/rules" "$(rule_body ingress tcp any 198.18.255.1/32)"
	mask "$(jq -r '(if type == "array" then .[0] else . end) | .id? // empty' "$tmp/api.json" 2>/dev/null || true)"
	any_made=$(case "$code" in 2??) echo "HTTP $code, port_range в ответе создания: «$(jq -r '(if type == "array" then .[0] else . end) | if has("port_range") then (.port_range | tostring) else "нет поля" end' "$tmp/api.json" 2>/dev/null || echo '?')»" ;; *) brief ;; esac)
	# Правила — отдельным списком группы: в карточке группы их нет, только
	# счётчики (прогон 205 читал карточку и видел «правил: 0»).
	rules_get() { api GET "$C1/security-groups/$sg/rules?limit=100"; }
	count_rules() { jq -r '.items? | length' "$tmp/api.json" 2>/dev/null || echo "?"; }
	# Правило с группой-источником в ответе списка: по списку, а не по
	# чтению по id, evo узнаёт такое правило (sgrules, данные «разработки A»).
	by_group='first(.items[]? | select(.port_range? == "18092:18092"))'
	by_none='first(.items[]? | select(.port_range? == "18093:18093"))'
	rules_get
	n0=$(count_rules)
	ok "Г-11: чтение правил" "HTTP $code$(why_tail), правил: $n0, поля правила: $(jq -c '[.items[]? | keys] | first // null' "$tmp/api.json" 2>/dev/null || echo '?'), источник «весь интернет» хранится как: $(jq -r '[.items[]? | select(.port_range? == "18091:18091" or .remote_ip_prefix? == "0.0.0.0/0") | .remote_ip_prefix // "null"] | first // "правило не найдено"' "$tmp/api.json" 2>/dev/null || echo '?'), правило с группой-источником в списке: $(if jq -e "$by_group" "$tmp/api.json" >/dev/null 2>&1; then echo "$(source_kind "$by_group"), remote_ip_prefix: $(prefix_kind "$by_group")"; else echo "не найдено"; fi), правило без источника в списке: $(if jq -e "$by_none" "$tmp/api.json" >/dev/null 2>&1; then echo "$(source_kind "$by_none"), remote_ip_prefix: $(prefix_kind "$by_none")"; else echo "не найдено"; fi)"
	# В списке правило «все порты» — единственное tcp с тестовым адресом и не
	# с портом пробы.
	ok "Г-11: правило «все порты»" "$any_made; в списке: «$(jq -r '[.items[]? | select(.ip_protocol? == "tcp" and .remote_ip_prefix? == "198.18.255.1/32" and .port_range? != "18090:18090") | .port_range | tostring] | first // "не найдено"' "$tmp/api.json" 2>/dev/null || echo '?')»"
	# Повтор правила, которое создано (не первого в списке: в прогоне 207
	# оно не создалось, и «повтор» создал его впервые): 2xx и новое
	# правило, 2xx без нового или отказ.
	if [ -n "$made" ]; then
		# shellcheck disable=SC2086 # четыре слова правила — четыре аргумента
		api POST "$C1/security-groups/$sg/rules" "$(rule_body $made)"
		mask "$(jq -r '.id? // empty' "$tmp/api.json" 2>/dev/null || true)"
		r=$(brief)
		rules_get
		n1=$(count_rules)
		ok "Г-11: повтор создания того же правила" "$r; правил было $n0, стало $n1"
	else
		gap "Г-11: повтор создания того же правила" "ни одно правило с адресом не создано — повторять нечего"
	fi
	# g11_delete ЧТО URL — удаление, повтор сразу, ожидание исчезновения и
	# повтор после него (D26: что отвечает удаление уже удалённого; повтор
	# сразу в прогоне 205 получил отказ по состоянию, а не «нет такого»).
	g11_delete() {
		api DELETE "$2"
		r="HTTP $code$(why_tail)"
		api DELETE "$2"
		r="$r, повтор сразу — $(brief)"
		t0=$SECONDS
		if ops_settle "$2" 120 gone; then
			r="$r; состояния: $chain, $((SECONDS - t0)) с"
			api DELETE "$2"
			r="$r; повтор после исчезновения — $(brief)"
		else
			r="$r; не исчезло за 2 мин (состояния: $chain) — повтор после исчезновения не шлю"
		fi
		ok "Г-11: удаление $1" "$r"
	}
	if [ -n "$first" ]; then
		g11_delete правила "$C1/security-groups/$sg/rules/$first"
	else
		gap "Г-11: удаление правила" "ни одно правило не создано"
	fi
	g11_delete группы "$C1/security-groups/$sg"
}

# ops_settle URL СЕКУНД [gone] — читать карточку ресурса, пока её state
# переходный (…ing); с gone — пока карточка есть вообще. Итог: $st —
# последнее состояние («нет (404)» — карточки нет), $chain — состояния по
# порядку. Код 1 — не дождались. Пауза между чтениями — PROBE_PAUSE.
PROBE_PAUSE=${PROBE_PAUSE:-20}
# PROBE_WAIT — предел ожиданий через poll (появление восстановленного
# ресурса, исчезновение ВМ), секунд; проверка на заглушках ставит 1.
ops_settle() {
	local url=$1 until=$(($(date +%s) + $2)) s
	st='' chain=''
	while :; do
		iam_fresh || true
		api GET "$url"
		case "$code" in
		2??) s=$(jq -r '.state? // "без state"' "$tmp/api.json" 2>/dev/null) || s="не JSON" ;;
		404) s="нет (404)" ;;
		*) s="HTTP $code" ;;
		esac
		[ "$s" = "$st" ] || chain="${chain:+$chain → }$s"
		st=$s
		# running оканчивается на «ing», но это конечное состояние ВМ.
		case "${3:-}:$code:$s" in gone:2??:*) ;; *:running) return 0 ;; *:2??:*ing) ;; *) return 0 ;; esac
		[ "$(date +%s)" -lt "$until" ] || return 1
		sleep "$PROBE_PAUSE"
	done
}
# why_tail — «: текст отказа» последнего ответа api, если он не 2xx.
why_tail() { case "$code" in 2??) ;; *) echo ": $(api_why)" ;; esac; }
# ops_reach URL СЕКУНД СОСТОЯНИЕ [changed] — читать карточку, пока state не
# станет СОСТОЯНИЕМ; с changed — не раньше, чем state хоть раз сменился
# (перезагрузка: running → … → running). Итог — $st и $chain, как у
# ops_settle. Код 1 — не дождались или state error*.
ops_reach() {
	local url=$1 until=$(($(date +%s) + $2)) s n=0
	st='' chain=''
	while :; do
		iam_fresh || true
		api GET "$url"
		case "$code" in
		2??) s=$(jq -r '.state? // "без state"' "$tmp/api.json" 2>/dev/null) || s="не JSON" ;;
		*) s="HTTP $code" ;;
		esac
		[ "$s" = "$st" ] || { chain="${chain:+$chain → }$s"; n=$((n + 1)); }
		st=$s
		if [ "$s" = "$3" ] && { [ -z "${4:-}" ] || [ "$n" -gt 1 ]; }; then return 0; fi
		case "$s" in error*) return 1 ;; esac
		[ "$(date +%s)" -lt "$until" ] || return 1
		sleep "$PROBE_PAUSE"
	done
}
# mcode — машинный код причины последнего ответа api (поля code,
# error_code, type и reason в details — так отвечают API DBaaS; тело —
# объект или массив объектов, как у Compute, прогон 201), без разбора
# текста.
mcode() { jq -r '[(if type == "array" then .[] else . end) | objects | (.code?, .error_code?, .type?, (.details? | if type == "array" then .[] else . end | objects | .reason?))] | map(select(type == "string")) | unique | join(",")' "$tmp/api.json" 2>/dev/null || true; }
# refusal ПРОВЕРКА ШАГ — отказ платформы на создающий или меняющий запрос
# (последний ответ api) одной строкой, поля по условиям «Риска» (R-77):
# код HTTP и шаг (синхронно или в операции); имена полей тела и машинный
# код причины; текст до 160 знаков без id; чем на это отвечает evo сегодня
# (EVO_NOW; у проб операций, которых в evo нет, — так и сказано). Отказ по
# квоте — только по машинному коду, не по тексту: тогда ещё что осталось
# созданным (счёт по видам области; уберёт уборка области) и напоминание о
# строке Г-01, а QUOTA_HIT=1 — проба больше ничего не создаёт и не
# повторяет запрос с другими параметрами (повтор — отдельный запуск).
QUOTA_HIT=0
refusal() {
	local mc d kind left=''
	# No answer at all is not a refusal of the platform: the outcome of the
	# request is unknown (run 204 printed it as «отказ HTTP 000»).
	if [ "$code" = 000 ]; then
		gap "$1" "ответа нет (curl: код ${API_RC:-?}) — исход запроса неизвестен; evo сегодня: ${EVO_NOW:-операции в evo нет}"
		return 0
	fi
	mc=$(mcode)
	d="отказ HTTP $code ($2); поля тела: $(keys_of); машинный код: ${mc:-нет}; текст: $(api_why | cut -c1-160); evo сегодня: ${EVO_NOW:-операции в evo нет}"
	case "$mc" in *[Qq][Uu][Oo][Tt][Aa]*)
		QUOTA_HIT=1
		for kind in $(area_kinds ops); do
			left="$left${left:+, }$kind $(if o=$(lst "$kind" "$prefix" 2>/dev/null); then printf '%s' "$o" | grep -c . || true; else echo '?'; fi)"
		done
		d="$d; квота по машинному коду: да — проба больше не создаёт и не повторяет; осталось создано: $left (уберёт уборка области); строка Г-01 по этому типу — в отчёте прогона"
		;;
	esac
	ok "$1" "$d"
}

# Проба Г-16: резервные копии и план защиты. Диск 10 ГБ и ВМ без
# публичного адреса создаёт evo; дальше — прямые запросы. Что записывается:
# коды и поля ответов создания копии диска и копии ВМ, состояния копии до
# готовности и время, список копий и фильтр по имени, восстановление (оно
# создаёт новый ресурс: диск и ВМ под именами пробы, диск ВМ назван и
# привязан к загрузочному диску исходной — иначе уборка по префиксу его не
# найдёт), удаление копии с повтором сразу и после исчезновения, остаётся
# ли восстановленный диск без копии; план защиты диска с выключенным
# расписанием и без немедленного запуска (копирования он не начинает):
# создание, список, удаление и повтор. В конце — копии ресурсов пробы под
# чужим именем (их создал бы план): уборка по префиксу их не видит, проба
# удаляет их по id. Отказ платформы на любом шаге — ответ, не провал;
# остальное с именем пробы убирает уборка области. Ожидания ограничены
# (копия — 15 мин, восстановление — 10 и 5, удаление — 5): вместе с уборкой
# проба укладывается в предел job live и при худшем ходе; не дождалась —
# строка называет последнее состояние, проба идёт дальше.
probe_g16() {
	local zid=$1 n=$prefix-g16 disk='' vm='' boot bd='' bv='' pp r r2 t0 id left
	iam_fresh || { gap "Г-16" "нет токена IAM (KEY_ID, SECRET)"; return 0; }
	[ -n "${PROJECT:-}" ] || { gap "Г-16" "нет PROJECT"; return 0; }
	tc disk_create --arg name="$n-d" --arg size_gb=10 --arg zone="$zid"
	if [ "$rc" = 0 ]; then
		disk=$(j -r '.disk_id // .disk.id // empty')
		mask "$disk"
	fi
	[ -n "$disk" ] || gap "Г-16: диск" "disk_create: код $rc, $reason: $why"
	tc provision --arg name="$n" --arg region="$zid" --arg image="$IMAGE" --arg flavor="$FLAVOR" --arg no_public_ip=true
	if [ "$rc" = 0 ]; then
		vm=$(j -r '.vm_id // empty')
		mask "$vm"
	fi
	[ -n "$vm" ] || gap "Г-16: ВМ" "provision: код $rc, $reason: $why"
	# g16_backup ЧЕГО ИМЯ ТИП ID — копия ресурса; id готовой копии — в $bid.
	g16_backup() {
		bid=
		api POST "$C1/backups" "$(jq -nc --arg p "$PROJECT" --arg n "$2" --arg t "$3" --arg id "$4" \
			'{project_id: $p, name: $n, description: "evo e2e probe", resource: {resource_id: $id, resource_type: $t}}')"
		case "$code" in 2??) ;; *)
			refusal "Г-16: копия $1" синхронно
			return 0
			;;
		esac
		bid=$(jq -r '.id? // empty' "$tmp/api.json" 2>/dev/null) || bid=
		mask "$bid"
		ok "Г-16: копия $1" "HTTP $code, state $(jq -r '.state? // "нет"' "$tmp/api.json" 2>/dev/null || echo '?'), поля ответа: $(keys_of)"
		[ -n "$bid" ] || { gap "Г-16: копия $1" "id не прочитан — остаток уберёт уборка области"; return 0; }
		t0=$SECONDS
		ops_settle "$C1/backups/$bid" 900 || true
		ok "Г-16: копия $1, ожидание готовности" "состояния: $chain; $((SECONDS - t0)) с; type $(jq -r '.type? // "нет"' "$tmp/api.json" 2>/dev/null || echo '?'), size больше нуля: $(jq -r 'if (.size? // 0) > 0 then "да" else "нет" end' "$tmp/api.json" 2>/dev/null || echo '?'); created_at: $(time_form created_at); поля карточки: $(keys_of)"
		[ "$st" = created ] || bid_bad=1
	}
	local bid bid_bad=0 bd_ok=0 bv_ok=0 rd_seen=0
	if [ -n "$disk" ]; then
		g16_backup диска "$n-bd" disk "$disk"
		bd=$bid
		[ -z "$bd" ] || [ "$bid_bad" = 1 ] || bd_ok=1
	fi
	bid_bad=0
	if [ -n "$vm" ] && [ "$QUOTA_HIT" = 0 ]; then
		g16_backup ВМ "$n-bv" virtual_machine "$vm"
		bv=$bid
		[ -z "$bv" ] || [ "$bid_bad" = 1 ] || bv_ok=1
	fi
	api GET "$C1/backups?project_id=$PROJECT&limit=1000"
	r="HTTP $code$(why_tail), поля ответа: $(keys_of), поля элемента: $(jq -c '[.items[]? | keys] | first // null' "$tmp/api.json" 2>/dev/null || echo '?'), копий пробы: $(jq -r --arg p "$n-b" '[.items[]? | select((.name // "") | startswith($p))] | length' "$tmp/api.json" 2>/dev/null || echo '?')"
	if [ -n "$bd" ]; then
		api GET "$C1/backups?project_id=$PROJECT&name=$n-bd"
		r="$r; фильтр name по полному имени: HTTP $code, найдено $(jq -r '.items? | length' "$tmp/api.json" 2>/dev/null || echo '?')"
	fi
	ok "Г-16: список копий" "$r"
	# g16_restore ЧЕГО ID_КОПИИ TARGET_CONFIG
	g16_restore() {
		api POST "$C1/backups/$2/action" "$(jq -nc --argjson c "$3" '{action: "restore", resource: {target_config: $c}}')"
		r="HTTP $code$(case "$code" in 2??) echo ", поля ответа: $(keys_of)" ;; *) echo ": $(api_why)" ;; esac)"
		case "$code" in 2??) ;; *)
			refusal "Г-16: восстановление $1" синхронно
			return 1
			;;
		esac
		t0=$SECONDS
		ops_settle "$C1/backups/$2" 600 || true
		r="$r; состояния копии: $chain; last_restore_time: $(time_form last_restore_time)"
	}
	if [ "$bd_ok" = 1 ] && [ "$QUOTA_HIT" = 0 ]; then
		if g16_restore диска "$bd" "$(jq -nc --arg n "$n-rd" '{name: $n, description: "evo e2e probe"}')"; then
			rd_there() { [ -n "$(lst disk "$n-rd")" ]; }
			rd_seen=0
			if poll "${PROBE_WAIT:-300}" "$PROBE_PAUSE" rd_there; then rd_seen=1 r="$r; новый диск появился за $((SECONDS - t0)) с"; else r="$r; нового диска нет за 5 мин после копии"; fi
			ok "Г-16: восстановление диска" "$r"
			# Второе восстановление той же копии под тем же именем (гипотеза
			# Г3 дельта-аудита 1.9, FB19I-18): отказ платформы или второй
			# диск с тем же именем — на этом стоит, может ли evo отличить
			# «восстановлено» от «восстановлено дважды». Второй диск, если
			# появится, носит имя пробы — его уберёт уборка области.
			if [ "$rd_seen" = 1 ]; then
				api POST "$C1/backups/$bd/action" "$(jq -nc --arg n "$n-rd" '{action: "restore", resource: {target_config: {name: $n, description: "evo e2e probe"}}}')"
				r=$(brief)
				case "$code" in 2??)
					ops_settle "$C1/backups/$bd" 600 || true
					rd_two() { [ "$(lst disk "$n-rd" | wc -l)" -ge 2 ]; }
					# Свой предел, не PROBE_WAIT: второго диска может не
					# быть вовсе, и ждать его общий предел незачем.
					r="$r; состояния копии: $chain"
					# Принято, а диска нет: он может появиться после уборки
					# области — платный остаток, которого никто не видел.
					# Проба не делает вид, что убрала: говорит, что сделать.
					if ! poll "${PROBE_WAIT_TWICE:-120}" "$PROBE_PAUSE" rd_two; then
						r="$r; второго диска за ${PROBE_WAIT_TWICE:-120} с нет — через 30 минут — уборка области по префиксу (диск может появиться позже)"
						echo "::warning::Г-16: второе восстановление принято, а второго диска за ${PROBE_WAIT_TWICE:-120} с нет — диск с именем пробы может появиться после уборки: через 30 минут запустите уборку области (sweep) и запишите в отчёте прогона «дисков с именем пробы: 0»"
					fi
					;;
				esac
				ok "Г-16: второе восстановление той же копии под тем же именем" "$r; дисков с этим именем: $(if o=$(lst disk "$n-rd"); then grep -c . <<<"$o" || true; else echo '?'; fi)"
			fi
		fi
	fi
	if [ "$bv_ok" = 1 ] && [ "$QUOTA_HIT" = 0 ]; then
		# The platform refuses a restore whose disk has no type (run 202:
		# 422 value_error), so the type of the source boot disk is sent.
		local boot_type=''
		IFS=$'\t' read -r boot boot_type < <(evo tools call disk_list --json 2>/dev/null | jq -r --arg vm "$n" '[.disks[]? | select(.bootable == true and ((.attached_to // []) | index($vm)))][0] | [.id // "", .disk_type // ""] | @tsv') || true
		mask "$boot"
		if [ -z "$boot" ] || [ -z "$boot_type" ]; then
			gap "Г-16: восстановление ВМ" "загрузочный диск ВМ или его тип не найден в disk_list — без них восстановление не шлю"
		elif g16_restore ВМ "$bv" "$(jq -nc --arg n "$n-rv" --arg b "$boot" --arg t "$boot_type" '{name: $n, description: "evo e2e probe", disks: [{name: ($n + "-boot"), related_disk_id: $b, disk_type_name: $t}]}')"; then
			local vst=''
			rv_up() { vst=$(evo tools call vm_get --arg name="$n-rv" --json 2>/dev/null | jq -r '.state // empty') || vst=; case "$vst" in "" | creating | *pending*) return 1 ;; esac; }
			# ВМ из копии создаётся дольше 5 минут (прогон 206: 312 с в creating):
			# ждём до 10.
			poll "${PROBE_WAIT:-600}" "$PROBE_PAUSE" rv_up || true
			# Восстановление принято, а ВМ не видна: её диски, если платформа
			# назвала их по-своему, не найдёт ни проба, ни уборка по имени.
			[ -n "$vst" ] || echo "::warning::Г-16: восстановление ВМ принято, но ВМ не появилась — после прогона проверьте в консоли Cloud.ru диски без ВМ, созданные в это время"
			# Диски восстановленной ВМ: с именем пробы их найдёт уборка
			# области; с другим — проба удаляет сама, после ВМ.
			evo tools call disk_list --json 2>/dev/null | jq -r --arg vm "$n-rv" --arg p "$prefix" \
				'.disks[]? | select((.attached_to // []) | index($vm)) | "\(.id)\t\(if (.name // "") | startswith($p) then "свой" else "чужой" end)"' >"$tmp/g16.rv" || : >"$tmp/g16.rv"
			# Публичные адреса восстановленной ВМ (у исходной их нет; что
			# восстановление делает с сетью, спецификация не говорит): из её
			# карточки. Уборка находит адрес только по имени fip-<префикс>.
			: >"$tmp/g16.fip"
			local rvid
			rvid=$(lst vm "$n-rv" | cut -f1 | head -1) || rvid=
			mask "$rvid"
			if [ -n "$rvid" ]; then
				api GET "$C1/vms/$rvid"
				jq -r --arg p "fip-$prefix" '.interfaces[]?.floating_ip? | select(type == "object" and (.id // "") != "") | "\(.id)\t\(if (.name // "") | startswith($p) then "свой" else "чужой" end)"' \
					"$tmp/api.json" >"$tmp/g16.fip" 2>/dev/null || : >"$tmp/g16.fip"
			fi
			ok "Г-16: восстановление ВМ" "$r; ВМ: ${vst:-не появилась за 5 мин после копии}, $((SECONDS - t0)) с; дисков у неё: $(wc -l <"$tmp/g16.rv" | tr -d ' '), из них не под именем пробы: $(grep -c 'чужой$' "$tmp/g16.rv" || true); публичных адресов у неё: $(wc -l <"$tmp/g16.fip" | tr -d ' '), из них не под именем пробы: $(grep -c 'чужой$' "$tmp/g16.fip" || true)"
			if grep -q 'чужой$' "$tmp/g16.rv" "$tmp/g16.fip"; then
				[ -z "$rvid" ] || del vm "$rvid"
				poll "${PROBE_WAIT:-300}" "$PROBE_PAUSE" gone vm "$n-rv" || true
				left=0
				while IFS=$'\t' read -r id _; do
					mask "$id"
					tc disk_delete --arg disk_id="$id"
					[ "$rc" = 0 ] || left=$((left + 1))
				done < <(grep 'чужой$' "$tmp/g16.rv")
				while IFS=$'\t' read -r id _; do
					mask "$id"
					tc release_floating_ip --arg id="$id"
					[ "$rc" = 0 ] || left=$((left + 1))
				done < <(grep 'чужой$' "$tmp/g16.fip")
				if [ "$left" = 0 ]; then ok "Г-16: диски и адреса восстановленной ВМ не под именем пробы" "удалены по id"
				else bad "Г-16: диски и адреса восстановленной ВМ не под именем пробы" "не удалено: $left — удалите в консоли Cloud.ru диски без ВМ и публичные адреса, созданные этим прогоном"; fi
			fi
		fi
	fi
	# Ожидания восстановления (poll) токен не обновляют — до удалений.
	iam_fresh || true
	# g16_delete ЧЕГО ID ПРЕДЕЛ_СЕК — удаление копии: пока идёт восстановление из неё,
	# платформа отказывает (422 backup_restore_in_progress при состоянии
	# created; прогоны 202 и 206) — запрос повторяется до снятия запрета,
	# время записывается; затем повтор сразу, ожидание исчезновения и
	# повтор после него. Итог — в $r.
	g16_delete() {
		local tries=1 held=$SECONDS until
		api DELETE "$C1/backups/$2"
		r="HTTP $code$(why_tail)"
		# Предел — от первого отказа: время самого запроса в него не входит.
		until=$(($(date +%s) + ${PROBE_WAIT:-$3}))
		while [ "$code" = 422 ] && [[ ",$(mcode)," == *,backup_restore_in_progress,* ]] && [ "$(date +%s)" -lt "$until" ]; do
			sleep "$PROBE_PAUSE"
			iam_fresh || true
			api DELETE "$C1/backups/$2"
			tries=$((tries + 1))
		done
		if [ "$tries" -gt 1 ]; then
			case "$code" in
			2??) r="$r; запрет «идёт восстановление» снят через $((SECONDS - held)) с (попыток: $tries) — HTTP $code" ;;
			*) r="$r; запрет «идёт восстановление» не снят за $((SECONDS - held)) с (попыток: $tries) — HTTP $code$(why_tail)" ;;
			esac
		fi
		api DELETE "$C1/backups/$2"
		r="$r, повтор сразу — HTTP $code$(why_tail)"
		t0=$SECONDS
		ops_settle "$C1/backups/$2" 300 gone || true
		r="$r; состояния: $chain, $((SECONDS - t0)) с"
		api DELETE "$C1/backups/$2"
		r="$r; повтор после исчезновения — HTTP $code$(why_tail)"
	}
	if [ -n "$bd" ]; then
		# Диск восстановлен давно (пока ждали ВМ): запрет, если есть, короткий.
		g16_delete диска "$bd" 300
		[ "$bd_ok" = 0 ] || r="$r; восстановленный диск без копии: $(if o=$(lst disk "$n-rd"); then [ -n "$o" ] && echo есть || echo нет; else echo "список не прочитан"; fi)"
		ok "Г-16: удаление копии диска" "$r"
	fi
	if [ -n "$bv" ]; then
		g16_delete ВМ "$bv" 600
		ok "Г-16: удаление копии ВМ" "$r"
	fi
	if [ -n "$disk" ] && [ "$QUOTA_HIT" = 0 ]; then
		api POST "$C1/protection-plans" "$(jq -nc --arg p "$PROJECT" --arg n "$n-pp" --arg id "$disk" --arg t "$(date -u -d '+30 days' +%Y-%m-%dT03:00:00Z)" \
			'{project_id: $p, name: $n, description: "evo e2e probe", resources: [{resource_id: $id, resource_type: "disk"}],
			  schedules: [{start_time: $t, days_of_week: [1], enabled: false}], execute_immediately: false, type: "full"}')"
		pp=$(jq -r '.id? // empty' "$tmp/api.json" 2>/dev/null) || pp=
		mask "$pp"
		case "$code" in
		2??)
			r="HTTP $code, поля ответа: $(keys_of)"
			if [ -n "$pp" ]; then
				ops_settle "$C1/protection-plans/$pp" 300 || true
				r="$r; состояния: $chain"
				api GET "$C1/protection-plans?project_id=$PROJECT&limit=1000"
				r="$r; список: HTTP $code, поля элемента: $(jq -c '[.items[]? | keys] | first // null' "$tmp/api.json" 2>/dev/null || echo '?')"
				api DELETE "$C1/protection-plans/$pp"
				r="$r; удаление: HTTP $code$(why_tail)"
				api DELETE "$C1/protection-plans/$pp"
				r2="повтор сразу — HTTP $code$(why_tail)"
				ops_settle "$C1/protection-plans/$pp" 300 gone || true
				r="$r, $r2; состояния: $chain"
			else r="$r; id не прочитан — остаток уберёт уборка области"; fi
			ok "Г-16: план защиты" "$r"
			;;
		*) refusal "Г-16: план защиты" синхронно ;;
		esac
	fi
	# Копии, в которых назван диск или ВМ пробы, но имя не пробы.
	iam_fresh || true
	api GET "$C1/backups?project_id=$PROJECT&limit=1000"
	case "$code" in 2??) ;; *)
		gap "Г-16: копии ресурсов пробы под чужим именем" "список не прочитан: HTTP $code: $(api_why)"
		return 0
		;;
	esac
	# Отбор — по полю resource.resource_id копии (спецификация), равному id
	# диска или ВМ пробы, и только если этот id — UUID: id другого вида
	# (пустой, обрезанный) отбор выключает — чужую копию проба не удаляет.
	local uuid='^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' d='' v=''
	if [[ $disk =~ $uuid ]]; then d=$disk; fi
	if [[ $vm =~ $uuid ]]; then v=$vm; fi
	if [ -z "$d$v" ]; then
		gap "Г-16: копии ресурсов пробы под чужим именем" "id диска и ВМ пробы — не UUID: отбор по ним выключен"
		return 0
	fi
	jq -r --arg p "$prefix" --arg d "$d" --arg v "$v" \
		'.items[]? | select(((.name // "") | startswith($p) | not) and ((.resource.resource_id? // "") as $r | $r != "" and ($r == $d or $r == $v))) | .id' "$tmp/api.json" >"$tmp/g16.stray" 2>/dev/null || : >"$tmp/g16.stray"
	left=0 r=0
	while read -r id; do
		[ -n "$id" ] || continue
		mask "$id"
		r=$((r + 1))
		api DELETE "$C1/backups/$id"
		case "$code" in 2?? | 404) ;; *) left=$((left + 1)) ;; esac
	done <"$tmp/g16.stray"
	if [ "$left" = 0 ]; then ok "Г-16: копии ресурсов пробы под чужим именем" "найдено $r, удалено по id"
	else bad "Г-16: копии ресурсов пробы под чужим именем" "найдено $r, не удалено $left — удалите в консоли Cloud.ru копии диска и ВМ этого прогона"; fi
}

# power СЛОВО — set-power ВМ пробы (её адрес — в $url вызывающей пробы);
# код — в $code.
power() { api POST "$url/set-power" "$(jq -nc --arg s "$1" '{state: $s}')"; }
# time_form ПОЛЕ — в каком виде ответ отдаёт время: «RFC3339 с зоной», «без
# зоны», «нет» или «иной вид». evo считает возраст копии и предупреждает о
# недавнем восстановлении по этим полям; вид без зоны разбор отбросит молча
# (гипотеза Г7 дельта-аудита 1.9). Значение в лог не идёт — только вид.
time_form() {
	jq -er --arg f "$1" '.[$f] | if . == null then "нет"
		elif type != "string" then "иной вид (\(type))"
		elif test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\\.[0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2})$") then "RFC3339 с зоной"
		elif test("^[0-9]{4}-[0-9]{2}-[0-9]{2}[T ][0-9]{2}:[0-9]{2}:[0-9]{2}(\\.[0-9]+)?$") then "без зоны"
		else "иной вид" end' "$tmp/api.json" 2>/dev/null || echo '?'
}
# prefix_kind [ОТБОР] — что правило хранит в remote_ip_prefix, словами,
# которые маска адресов не стирает: null, пусто, весь IPv4, весь IPv6 или
# другой CIDR. source_kind [ОТБОР] — в какой форме ответ отдаёт
# группу-источник. ОТБОР — фильтр jq до правила в ответе (по умолчанию сам
# ответ). Один разбор на все шаги Г-11, которые это записывают.
prefix_kind() { jq -er "${1:-.}"' | (if type == "array" then .[0] else . end) | .remote_ip_prefix? | if . == null then "null" elif . == "" then "пусто" elif . == "0.0.0.0/0" then "весь IPv4" elif . == "::/0" then "весь IPv6" else "CIDR" end' "$tmp/api.json" 2>/dev/null || echo '?'; }
source_kind() { jq -er "${1:-.}"' | if (.remote_security_group? | type) == "object" then "объект remote_security_group с полями \(.remote_security_group | keys | tojson)" elif has("remote_security_group_id") then "плоское поле remote_security_group_id" else "группы в ответе нет, поля: \(keys | tojson)" end' "$tmp/api.json" 2>/dev/null || echo "не разобран"; }
# brief — последний ответ api коротко: код, а у отказа — машинный код и текст.
brief() { echo "HTTP $code$(case "$code" in 2??) ;; *) echo ", машинный код $(mcode): $(api_why | cut -c1-160)" ;; esac)"; }

# Проба Г-10 (и Г-20): действия над ВМ. ВМ с диском и публичным адресом
# создаёт evo; дальше — прямые запросы set-power и PUT (ответ 204 без id
# операции — ход читается состоянием ВМ). Что записывается: остановка —
# состояния и время, остался ли публичный адрес в карточке; повтор
# остановки остановленной; запуск — состояния, время, тот же ли адрес;
# повтор запуска запущенной; перезагрузка и её повтор сразу; set-power по
# id, которого нет; смена flavor на один шаг вверх в том же семействе —
# сначала на запущенной (нужна ли остановка), при отказе не по квоте — на
# остановленной; возврат исходного flavor — в любом случае. Отказ по квоте
# (refusal) — конец пробы: без повтора с другими параметрами.
# Со словом g20 ВМ называется …-g20 и в конце остаётся остановленной: её не
# трогает ни уборка области, ни общая уборка, пока ту не запустят со словом
# g20 (e2e_live_runs.sh) — сутки спустя, после чтения потребления.
probe_g10() {
	local zid=$1 n=$prefix-g10 vm ip0 f0 up r t0 keep=0
	! probe_on g20 || { keep=1 n=$prefix-g20; }
	iam_fresh || { gap "Г-10" "нет токена IAM (KEY_ID, SECRET)"; return 0; }
	tc provision --arg name="$n" --arg region="$zid" --arg image="$IMAGE" --arg flavor="$FLAVOR"
	[ "$rc" = 0 ] || { gap "Г-10: ВМ" "provision: код $rc, $reason: $why"; return 0; }
	vm=$(j -r '.vm_id // empty')
	ip0=$(j -r '.vm_ip // empty')
	mask "$vm" "$ip0"
	[ -n "$vm" ] || { gap "Г-10: ВМ" "provision не отдал vm_id"; return 0; }
	local url="$C1/vms/$vm"
	has_ip() { if [ -n "$ip0" ] && grep -qF "$ip0" "$tmp/api.json" 2>/dev/null; then echo да; else echo нет; fi; }
	api GET "$url"
	f0=$(jq -r '.flavor.name? // empty' "$tmp/api.json" 2>/dev/null) || f0=
	case "$code" in
	2??) ok "Г-10: карточка ВМ" "HTTP $code, state $(jq -r '.state? // "нет"' "$tmp/api.json" 2>/dev/null || echo '?'), публичный адрес в карточке: $(has_ip), поля: $(keys_of)" ;;
	*) gap "Г-10: карточка ВМ" "не прочитана: HTTP $code$(why_tail) — исходный flavor неизвестен, смена flavor не пойдёт" ;;
	esac
	# g10_stop ПРОВЕРКА — остановка и ожидание stopped; код 1 — отказ.
	g10_stop() {
		power power_off
		case "$code" in 2??) ;; *) refusal "$1" синхронно; return 1 ;; esac
		r="HTTP $code"
		t0=$SECONDS
		ops_reach "$url" "${PROBE_WAIT:-600}" stopped || true
		ok "$1" "$r; состояния: $chain, $((SECONDS - t0)) с; публичный адрес в карточке остановленной: $(has_ip)"
	}
	if g10_stop "Г-10: остановка"; then
		power power_off
		ok "Г-10: повтор остановки остановленной" "$(brief)"
		power power_on
		case "$code" in
		2??)
			r="HTTP $code"
			t0=$SECONDS
			ops_reach "$url" 600 running || true
			ok "Г-10: запуск" "$r; состояния: $chain, $((SECONDS - t0)) с; публичный адрес тот же: $(has_ip)"
			power power_on
			ok "Г-10: повтор запуска запущенной" "$(brief)"
			;;
		*) refusal "Г-10: запуск" синхронно ;;
		esac
	fi
	api GET "$url"
	if [ "$(jq -r '.state? // empty' "$tmp/api.json" 2>/dev/null)" = running ]; then
		power reboot
		case "$code" in
		2??)
			r="HTTP $code"
			power reboot
			r="$r; повтор сразу — $(brief)"
			t0=$SECONDS
			ops_reach "$url" 600 running changed || true
			ok "Г-10: перезагрузка" "$r; состояния: $chain, $((SECONDS - t0)) с"
			;;
		*) refusal "Г-10: перезагрузка" синхронно ;;
		esac
	fi
	api POST "$C1/vms/00000000-0000-4000-8000-000000000000/set-power" '{"state":"power_on"}'
	ok "Г-10: set-power по id, которого нет" "$(brief)"
	# Смена flavor: один шаг вверх в том же семействе (имя до первого дефиса).
	up=$(evo tools call list_flavors --arg zone="$ZONE" --arg gpu=false --arg spot=false --json 2>/dev/null |
		jq -r --arg f "${f0:-$FLAVOR}" '(.flavors // []) as $all | ($all[] | select(.name == $f)) as $cur
			| [$all[] | select((.name | split("-")[0]) == ($f | split("-")[0]) and ([.vcpus, .ram_gb] > [$cur.vcpus, $cur.ram_gb]))]
			| sort_by(.vcpus, .ram_gb) | .[0].name // empty' 2>/dev/null) || up=
	if [ -z "$up" ]; then
		gap "Г-10: смена flavor" "в list_flavors нет flavor на шаг выше ${f0:-$FLAVOR} в его семействе"
	else
		resize() { api PUT "$url" "$(jq -nc --arg f "$1" '{flavor_name: $f}')"; }
		flavor_now() { api GET "$url"; jq -r '.flavor.name? // "?"' "$tmp/api.json" 2>/dev/null || echo '?'; }
		# resized ПРОВЕРКА ЦЕЛЕВОЙ ПРЕЖНИЙ — после принятой смены: какой flavor
		# показывает карточка, пока ВМ в updating (целевой или прежний — по
		# этому шаг evo узнаёт свою операцию), состояния по порядку, в каком
		# состоянии питания ВМ вышла и какой flavor в итоге.
		resized() {
			local mid f
			t0=$SECONDS
			api GET "$url"
			f=$(jq -r '.flavor.name? // "?"' "$tmp/api.json" 2>/dev/null) || f='?'
			mid="в первом чтении после запроса: state $(jq -r '.state? // "?"' "$tmp/api.json" 2>/dev/null || echo '?'), flavor $(if [ "$f" = "$2" ]; then echo целевой; elif [ "$f" = "$3" ]; then echo прежний; else echo "другой или не назван"; fi)"
			ops_settle "$url" 600 || true
			r="$r; $mid; состояния: $chain, $((SECONDS - t0)) с; вышла в $st"
			ok "$1" "$r; flavor стал $([ "$(flavor_now)" = "$2" ] && echo "тем, что просили" || echo "другим")"
		}
		# g10_round ГДЕ — шаг вверх и возврат исходного flavor на ВМ в её
		# нынешнем состоянии питания. Возврат — в любом случае, если flavor
		# сменился. Код 1 — отказ шага вверх (записан).
		g10_round() {
			resize "$up"
			case "$code" in 2??) ;; *) refusal "Г-10: смена flavor на $1 ВМ" синхронно; return 1 ;; esac
			r="HTTP $code на $1"
			resized "Г-10: смена flavor на шаг вверх, ВМ $1" "$up" "${f0:-$FLAVOR}"
			resize "${f0:-$FLAVOR}"
			case "$code" in
			2??)
				r="HTTP $code на $1"
				resized "Г-10: возврат исходного flavor, ВМ $1" "${f0:-$FLAVOR}" "$up"
				;;
			*) bad "Г-10: возврат исходного flavor, ВМ $1" "HTTP $code: $(api_why) — ВМ осталась с flavor на шаг выше; её удалит уборка$([ "$keep" = 1 ] && echo " со словом g20 — запустите её, не дожидаясь суток")" ;;
			esac
		}
		# На запущенной ВМ, затем на остановленной (нужна ли остановка; в
		# каком состоянии ВМ выходит из updating). Квота — конец пробы.
		g10_round запущенной || true
		# Второй круг — только в ВМ, которая вернулась в исходное: flavor по
		# карточке исходный, состояние устоявшееся. Иначе изменение ушло бы
		# в ВМ с неизвестным исходом (заключение «Риска» 151).
		if [ "$QUOTA_HIT" = 0 ]; then
			local f1 s1
			api GET "$url"
			f1=$(jq -r '.flavor.name? // empty' "$tmp/api.json" 2>/dev/null) || f1=
			s1=$(jq -r '.state? // empty' "$tmp/api.json" 2>/dev/null) || s1=
			if [ -z "$f0" ] || [ "$f1" != "$f0" ]; then
				gap "Г-10: смена flavor на остановленной ВМ" "flavor ВМ не подтверждён исходным после первого круга — второй круг не шлю"
			elif [ "$s1" != running ] && [ "$s1" != stopped ]; then
				gap "Г-10: смена flavor на остановленной ВМ" "ВМ в состоянии ${s1:-не прочитано} — второй круг не шлю"
			elif g10_stop "Г-10: остановка перед сменой flavor на остановленной ВМ"; then
				g10_round остановленной || true
			fi
		fi
	fi
	[ "$keep" = 1 ] || return 0
	api GET "$url"
	if [ "$(jq -r '.state? // empty' "$tmp/api.json" 2>/dev/null)" != stopped ]; then
		power power_off
		ops_reach "$url" 600 stopped || true
	fi
	api GET "$url"
	# Под исключением уборки остаётся только ВМ в исходном flavor: на шаг
	# дороже она и стоит больше, и цену меряет не ту. Flavor не подтверждён
	# — проба удаляет ВМ, её адрес и диск сама (уборка области их обходит).
	if [ -n "$f0" ] && [ "$(jq -r '.flavor.name? // empty' "$tmp/api.json" 2>/dev/null)" = "$f0" ]; then
		ok "Г-20: ВМ пробы цены оставлена" "state $(jq -r '.state? // "?"' "$tmp/api.json" 2>/dev/null || echo '?'), flavor исходный; ВМ, диск и адрес живут до уборки cleanup_only со словом g20 во входе probes (срок — 72 ч, дольше уборка без слова краснеет)"
		return 0
	fi
	local kind id left=0
	del vm "$vm"
	[ "$rc" = 0 ] || left=1
	poll "${PROBE_WAIT:-300}" "$PROBE_PAUSE" gone vm "$n" || left=1
	for kind in fip disk; do
		while IFS=$'\t' read -r id _; do
			[ -n "$id" ] || continue
			mask "$id"
			del "$kind" "$id"
			[ "$rc" = 0 ] || left=1
		done < <(lst "$kind" "$n" || true)
	done
	if [ "$left" = 0 ]; then ok "Г-20: проба цены не состоялась" "flavor ВМ не подтверждён исходным — ВМ, адрес и диск удалены сразу; нужен новый запуск"
	else bad "Г-20: проба цены не состоялась" "flavor ВМ не подтверждён исходным, а удаление не прошло — ВМ, адрес или диск остались: запустите cleanup_only со словом g20 во входе probes"; fi
}

# Проба Г-17: теги. Тег создаётся в «Менеджере ресурсов» (пара «ключ —
# значение», спецификация resource-manager); Compute принимает у ВМ и диска
# tag_ids и tag_names (одно имя) и называет источником другой адрес —
# одна ли это сущность, спецификации не говорят, и это главный вопрос пробы
# (волна В6). Что записывается: первое чтение тегов проекта (до любой
# записи); создание тега, чтение по id и по ключу, изменение; диск и ВМ с
# tag_ids в запросе создания — принят ли, что в tags карточки; отбор ВМ по
# тегу в Compute и ресурсов по тегу в «Менеджере»; диск с tag_names = имени
# того же тега; снятие и назначение тега диску через «Менеджер»; удаление
# тега, назначенного диску, его повтор и tags диска после.
# Условия заключения «Риска» 183: до записи — чтение, и тег шире проекта —
# конец пробы (У2); изменение, удаление и назначение — только по id тега из
# ответа своего создания и только на ВМ и диск этого запуска (У3); удаление
# назначенного тега — на диске, после удаления ВМ, последним (У4); попытка
# tag_names вводит только имя уже созданного тега и отключается словом
# G17_TAG_NAMES=no — вторым прогоном, если первый оставил тег, которого
# «Менеджер» не видит (У5).
probe_g17() {
	local zid=$1 n=$prefix-g17 tag='' disk='' disk2='' vm='' sn r wider n1 same id did left=0
	iam_token || { gap "Г-17" "нет токена IAM (KEY_ID, SECRET)"; return 0; }
	[ -n "${PROJECT:-}" ] || { gap "Г-17" "нет PROJECT"; return 0; }
	# tag_of — тег в ответе: сам объект или в поле tag.
	tag_of() { jq -r "(.tag? // .) | $1" "$tmp/api.json" 2>/dev/null || true; }
	# tags_n — сколько тегов в карточке ресурса Compute (последний ответ).
	tags_n() { jq -r 'if has("tags") then (.tags | if type == "array" then "\(length), поля тега: \((.[0]? // {}) | keys | tojson)" else type end) else "поля tags нет" end' "$tmp/api.json" 2>/dev/null || echo '?'; }
	# mine_n — сколько тегов с ключом пробы видит «Менеджер» («?» — список не прочитан).
	mine_n() { local o; if o=$(lst_tags "$n"); then grep -c . <<<"$o" || true; else echo '?'; fi; }
	api GET "$RM/tags?projectId=$PROJECT&pageSize=100"
	case "$code" in 2??) ;; *)
		gap "Г-17" "первое чтение тегов не прошло: $(brief) — запись не начинаю"
		return 0
		;;
	esac
	wider=$(jq -r --arg p "$PROJECT" '[.tags[]? | select(has("projectId") and .projectId != $p)] | length' "$tmp/api.json" 2>/dev/null) || wider='?'
	ok "Г-17: чтение тегов до записи" "HTTP $code, тегов: $(jq -r '.tags? | if type == "array" then length else "нет поля tags" end' "$tmp/api.json" 2>/dev/null || echo '?'), поля ответа: $(keys_of), поля тега: $(jq -c '(.tags[0]? // {}) | keys' "$tmp/api.json" 2>/dev/null || echo '?'), тегов другого проекта в ответе: $wider"
	if [ "$wider" != 0 ]; then
		gap "Г-17" "тег шире проекта — не покрыто: список проекта отдаёт теги другого проекта ($wider); запись не начинаю"
		return 0
	fi
	api POST "$RM/tags" "$(jq -nc --arg p "$PROJECT" --arg k "$n" '{key: $k, value: "probe", description: "evo e2e probe", projectId: $p}')"
	case "$code" in 2??) ;; *)
		refusal "Г-17: создание тега" синхронно
		return 0
		;;
	esac
	tag=$(tag_of '.id? // empty')
	mask "$tag"
	ok "Г-17: создание тега" "HTTP $code, поля ответа: $(keys_of), пара в ответе: «$(tag_of '.tagKeyValue? // "нет поля"' | cut -c1-120)», проект тега: $(jq -r --arg p "$PROJECT" '(.tag? // .) | if has("projectId") | not then "поля нет" elif .projectId == $p then "проект пробы" else "другой" end' "$tmp/api.json" 2>/dev/null || echo '?')"
	[ -n "$tag" ] || { gap "Г-17: тег" "id не прочитан — остаток уберёт уборка области по ключу"; return 0; }
	if [ "$(jq -r --arg p "$PROJECT" '(.tag? // .) | has("projectId") and .projectId != $p' "$tmp/api.json" 2>/dev/null)" = true ]; then
		api DELETE "$RM/tags/$tag?projectId=$PROJECT"
		gap "Г-17" "тег шире проекта — не покрыто: созданный тег не в проекте пробы; удаление своего тега — $(brief)"
		return 0
	fi
	api GET "$RM/tags/$tag?projectId=$PROJECT"
	r="чтение по id — HTTP $code$(why_tail), поля: $(jq -c '(.tag? // .) | if type == "object" then keys else type end' "$tmp/api.json" 2>/dev/null || echo '?')"
	api GET "$RM/tags?projectId=$PROJECT&key=$n"
	ok "Г-17: чтение тега" "$r; отбор по ключу — HTTP $code$(why_tail), найдено: $(jq -r '.tags? | if type == "array" then length else "?" end' "$tmp/api.json" 2>/dev/null || echo '?')"
	api PATCH "$RM/tags/$tag" "$(jq -nc --arg p "$PROJECT" '{projectId: $p, description: "evo e2e probe, changed"}')"
	ok "Г-17: изменение тега" "$(brief)"
	# Диск с тегом в запросе создания. Отказ не по квоте — диск без тега:
	# шаги назначения идут на нём.
	g17_disk() { # ИМЯ [ПОЛЕ ЗНАЧЕНИЕ-JSON]
		api POST "$C1/disks" "$(jq -nc --arg p "$PROJECT" --arg n "$1" --arg z "$ZONE" --arg f "${2:-}" --argjson v "${3:-null}" \
			'{project_id: $p, name: $n, size: 10, availability_zone_name: $z, disk_type_name: "SSD", description: "evo e2e probe"} + (if $f == "" then {} else {($f): $v} end)')"
	}
	# disk_id ИМЯ — id диска из ответа создания (или по имени) в $did. Не
	# через $(…): команда маски попала бы в значение.
	disk_id() { did=$(jq -r '.id? // empty' "$tmp/api.json" 2>/dev/null) || did=; [ -n "$did" ] || did=$(lst disk "$1" | cut -f1 | head -1) || did=; mask "$did"; }
	g17_disk "$n-d" tag_ids "$(jq -nc --arg t "$tag" '[$t]')"
	case "$code" in
	2??)
		disk_id "$n-d"
		disk=$did
		r="HTTP $code"
		if [ -n "$disk" ]; then
			api GET "$C1/disks/$disk"
			r="$r; tags в карточке диска: $(case "$code" in 2??) tags_n ;; *) brief ;; esac)"
		fi
		ok "Г-17: диск с tag_ids в запросе создания" "$r"
		;;
	*)
		refusal "Г-17: диск с tag_ids в запросе создания" синхронно
		if [ "$QUOTA_HIT" = 0 ]; then
			g17_disk "$n-d"
			case "$code" in
			2??)
				disk_id "$n-d"
				disk=$did
				;;
			*) gap "Г-17: диск без тега" "$(brief)" ;;
			esac
		fi
		;;
	esac
	# ВМ с тегом в запросе создания: без публичного адреса, в подсети зоны.
	if [ "$QUOTA_HIT" = 0 ]; then
		sn=$(zone_subnet "$zid")
		mask "$sn"
		if [ -z "$sn" ]; then
			gap "Г-17: ВМ с tag_ids" "нет подсети в $ZONE"
		else
			VM_TAG_IDS=$(jq -nc --arg t "$tag" '[$t]') vm_post "$n" "$FLAVOR" "$IMAGE" "$zid" 20 SSD "$(regular_if "$sn")"
			case "$code" in
			2??)
				r="HTTP $code"
				vm=$(jq -r '(if type == "array" then .[0] else (.vms[0]? // .) end) | .id? // empty' "$tmp/api.json" 2>/dev/null) || vm=
				g17_vm() { vm=$(evo tools call vm_get --arg name="$n" --json 2>/dev/null | jq -r '.id // empty'); [ -n "$vm" ]; }
				[ -n "$vm" ] || poll 60 3 g17_vm || true
				mask "$vm"
				if [ -n "$vm" ]; then
					api GET "$C1/vms/$vm"
					r="$r; tags в карточке ВМ: $(case "$code" in 2??) tags_n ;; *) brief ;; esac)"
					api GET "$C1/vms?project_id=$PROJECT&tag_ids=$tag&limit=100"
					r="$r; отбор ВМ по tag_ids — HTTP $code$(why_tail), найдено: $(jq -r '.items? | if type == "array" then length else "?" end' "$tmp/api.json" 2>/dev/null || echo '?')"
				else
					r="$r; id ВМ не прочитан за 60 с — остаток уберёт уборка области"
				fi
				ok "Г-17: ВМ с tag_ids в запросе создания" "$r"
				;;
			*) refusal "Г-17: ВМ с tag_ids в запросе создания" синхронно ;;
			esac
		fi
	fi
	iam_fresh || true
	api GET "$RM/resources?projectIds=$PROJECT&tagIds=$tag&pageSize=100"
	ok "Г-17: ресурсы по тегу в «Менеджере»" "HTTP $code$(why_tail), найдено: $(jq -r '.resources? | if type == "array" then length else "нет поля resources" end' "$tmp/api.json" 2>/dev/null || echo '?'), виды: $(jq -c '[.resources[]?.resourceTypeName] | unique' "$tmp/api.json" 2>/dev/null || echo '?')"
	# Диск с tag_names = имени уже созданного тега: нового имени проба не
	# вводит. Принят, а тег с этим ключом в «Менеджере» по-прежнему один —
	# возможен второй тег в сервисе, которого проба не видит и не уберёт.
	if [ "${G17_TAG_NAMES:-yes}" != yes ]; then
		ok "Г-17: диск с tag_names" "не повторяется: остаток неубираем (G17_TAG_NAMES=${G17_TAG_NAMES})"
	elif [ "$QUOTA_HIT" = 0 ]; then
		g17_disk "$n-d2" tag_names "$(jq -nc --arg t "$n" '[$t]')"
		case "$code" in
		2??)
			disk_id "$n-d2"
			disk2=$did
			r="HTTP $code"
			# same — тег на диске тот же, что тег «Менеджера»: да, нет или
			# «?» (карточка не прочитана). Читается до счёта тегов: тот
			# перезаписывает последний ответ.
			same='?'
			if [ -n "$disk2" ]; then
				api GET "$C1/disks/$disk2"
				case "$code" in
				2??)
					same=$(jq -r --arg t "$tag" 'if ([.tags[]?.id] | index($t)) != null then "да" else "нет" end' "$tmp/api.json" 2>/dev/null) || same='?'
					r="$r; tags в карточке диска: $(tags_n), id тега тот же, что у тега «Менеджера»: $same"
					;;
				*) r="$r; карточка диска: $(brief)" ;;
				esac
			fi
			n1=$(mine_n)
			r="$r; тегов с ключом пробы в «Менеджере»: $n1"
			ok "Г-17: диск с tag_names" "$r"
			if [ "$n1" = 1 ] && [ "$same" = нет ]; then
				echo "::warning::Г-17: диск с tag_names принят, а тег на нём — не тег «Менеджера»: второго тега в «Менеджере» нет; есть ли он в сервисе тегов Compute, проба не видит и убрать не может — остаток неизвестной сущности с именем пробы, цена тега неизвестна. Второй прогон — с G17_TAG_NAMES=no; остаток — пункт реестра расхождений"
			fi
			;;
		*) refusal "Г-17: диск с tag_names" синхронно ;;
		esac
	fi
	# Снятие и назначение через «Менеджер» — на диске этого запуска.
	if [ -n "$disk" ]; then
		g17_assign() { api POST "$RM/tags/$tag/$1" "$(jq -nc --arg p "$PROJECT" --arg r "$disk" '{resourceId: $r, projectId: $p}')"; }
		g17_assign unassignResource
		r="снятие — $(brief)"
		api GET "$C1/disks/$disk"
		r="$r, tags диска: $(case "$code" in 2??) tags_n ;; *) brief ;; esac)"
		g17_assign assignResource
		r="$r; назначение — $(brief)"
		api GET "$C1/disks/$disk"
		ok "Г-17: снятие и назначение тега диску" "$r, tags диска: $(case "$code" in 2??) tags_n ;; *) brief ;; esac)"
	fi
	# ВМ и второй диск удаляются до удаления тега: если после него ресурс
	# не удалится, останется диск 10 ГБ, а не ВМ.
	if [ -n "$vm" ]; then
		del vm "$vm"
		[ "$rc" = 0 ] || left=1
		poll "${PROBE_WAIT:-300}" "$PROBE_PAUSE" gone vm "$n" || left=1
	fi
	iam_fresh || true
	api DELETE "$RM/tags/$tag?projectId=$PROJECT"
	r="HTTP $code$(why_tail)"
	api DELETE "$RM/tags/$tag?projectId=$PROJECT"
	r="$r, повтор — $(brief)"
	if [ -n "$disk" ]; then
		api GET "$C1/disks/$disk"
		r="$r; tags диска после удаления тега: $(case "$code" in 2??) tags_n ;; *) brief ;; esac)"
	fi
	ok "Г-17: удаление тега, назначенного диску" "$r; тегов с ключом пробы в «Менеджере»: $(mine_n)"
	for id in "$disk" "$disk2"; do
		[ -n "$id" ] || continue
		del disk "$id"
		[ "$rc" = 0 ] || { left=1; echo "::warning::Г-17: диск пробы не удалён после удаления тега ($reason) — остаток: disk $id; уберёт уборка области, в отчёт — «ресурсов пробы» не 0"; }
	done
	[ "$left" = 0 ] || gap "Г-17: уборка пробы" "ВМ или диск не удалены сразу — остаток уберёт уборка области"
}

# Проба Г-18: второй интерфейс и пересоздание ВМ. ВМ с публичным адресом и
# подсеть 10.255.252.0/24 в VPC по умолчанию создаёт evo; дальше — прямые
# запросы. Что записывается: создание интерфейса ВМ в новой подсети (код,
# поля, состояние ВМ после — перезапускается ли она), повтор с тем же
# именем, удаление своих интерфейсов (по имени пробы из списка интерфейсов
# ВМ — основной интерфейс не трогается) и повтор удаления; пересоздание из
# другого образа того же семейства, которому хватает flavor ВМ: сначала на
# запущенной, при отказе не по квоте — на остановленной; после него —
# образ (шлётся по id), остался ли публичный адрес и тот же ли загрузочный диск. Вход по
# SSH после пересоздания проба не проверяет (ключ в метаданных — разовый
# ключ пробы). ВМ и подсеть убирает уборка области.
probe_g18() {
	local zid=$1 n=$prefix-g18 vm ip0 sn='' ifc r t0 id first=1 img boot0 boot1 done_on=''
	iam_fresh || { gap "Г-18" "нет токена IAM (KEY_ID, SECRET)"; return 0; }
	[ -n "${PROJECT:-}" ] || { gap "Г-18" "нет PROJECT"; return 0; }
	tc provision --arg name="$n" --arg region="$zid" --arg image="$IMAGE" --arg flavor="$FLAVOR"
	[ "$rc" = 0 ] || { gap "Г-18: ВМ" "provision: код $rc, $reason: $why"; return 0; }
	vm=$(j -r '.vm_id // empty')
	ip0=$(j -r '.vm_ip // empty')
	mask "$vm" "$ip0"
	[ -n "$vm" ] || { gap "Г-18: ВМ" "provision не отдал vm_id"; return 0; }
	local url="$C1/vms/$vm"
	tc subnet_create --arg name="$n-sn" --arg cidr=10.255.252.0/24 --arg zone="$ZONE"
	if [ "$rc" = 0 ]; then
		sn=$(j -r '.subnet_id // .subnet.id // empty')
		mask "$sn"
	fi
	[ -n "$sn" ] || gap "Г-18: подсеть" "subnet_create: код $rc, $reason: $why — второй интерфейс не создаю"
	if [ -n "$sn" ]; then
		# Интерфейс — только в готовой подсети: запрос сразу после
		# subnet_create получает «состояние подсети не даёт создать
		# интерфейс» (прогон 201).
		local sst='' schain=''
		sn_ready() {
			local s
			s=$(evo tools call list_subnets --json 2>/dev/null | jq -r --arg id "$sn" '[.subnets[]? | select(.id == $id) | .state // "без state"][0] // "нет в списке"' 2>/dev/null) || s="список не прочитан"
			[ "$s" = "$sst" ] || schain="${schain:+$schain → }$s"
			sst=$s
			case "${s,,}" in created | active | available) return 0 ;; *) return 1 ;; esac
		}
		t0=$SECONDS
		if poll "${PROBE_WAIT:-300}" "$PROBE_PAUSE" sn_ready; then
			ok "Г-18: подсеть" "состояния: $schain, $((SECONDS - t0)) с"
		else
			gap "Г-18: подсеть" "не готова за 5 мин (состояния: $schain) — второй интерфейс не создаю"
			sn=
		fi
	fi
	if [ -n "$sn" ]; then
		if_body() { jq -nc --arg p "$PROJECT" --arg n "$n-if" --arg v "$vm" --arg s "$sn" '{project_id: $p, name: $n, vm_id: $v, subnet_id: $s, description: "evo e2e probe"}'; }
		api POST "$C1/interfaces" "$(if_body)"
		case "$code" in
		2??)
			ifc=$(jq -r '.id? // empty' "$tmp/api.json" 2>/dev/null) || ifc=
			mask "$ifc"
			r="HTTP $code, поля ответа: $(keys_of)"
			t0=$SECONDS
			ops_settle "$url" 300 || true
			ok "Г-18: второй интерфейс" "$r; состояния ВМ после: $chain, $((SECONDS - t0)) с; интерфейсов в карточке ВМ: $(jq -r '.interfaces? | length' "$tmp/api.json" 2>/dev/null || echo '?')"
			api POST "$C1/interfaces" "$(if_body)"
			mask "$(jq -r '.id? // empty' "$tmp/api.json" 2>/dev/null || true)"
			ok "Г-18: повтор создания с тем же именем" "$(brief)"
			# Свои интерфейсы — по имени пробы из списка интерфейсов этой ВМ.
			api GET "$C1/interfaces?project_id=$PROJECT&vm_id=$vm&limit=100"
			case "$code" in
			2??)
				jq -r --arg n "$n-if" '.items[]? | select((.name // "") == $n) | .id' "$tmp/api.json" >"$tmp/g18.if" 2>/dev/null || : >"$tmp/g18.if"
				r="список: HTTP $code, своих интерфейсов: $(grep -c . "$tmp/g18.if" || true)"
				;;
			*)
				# Список отказал (прогоны 204 и 207: 422 extra_forbidden на
				# запрос по спецификации): какой параметр лишний, покажет тот
				# же запрос без limit; интерфейс удаляется по id из ответа
				# создания.
				r="список: $(brief)"
				api GET "$C1/interfaces?project_id=$PROJECT&vm_id=$vm"
				r="$r; без limit: $(brief) — $([ -n "$ifc" ] && echo "удаляю по id из ответа создания" || echo "id в ответе создания нет: интерфейс уберёт уборка области вместе с ВМ")"
				printf '%s\n' "$ifc" >"$tmp/g18.if"
				;;
			esac
			while read -r id; do
				[ -n "$id" ] || continue
				mask "$id"
				# Сразу после создания интерфейс удалить нельзя (прогон 207:
				# 422 по состоянию): сначала — его устоявшееся состояние.
				t0=$SECONDS
				ops_settle "$C1/interfaces/$id" 120 || true
				r="$r; состояния интерфейса: $chain, $((SECONDS - t0)) с"
				api DELETE "$C1/interfaces/$id"
				r="$r; удаление интерфейса: HTTP $code$(why_tail)"
				if [ "$first" = 1 ]; then
					first=0
					api DELETE "$C1/interfaces/$id"
					r="$r, повтор — $(brief)"
				fi
			done <"$tmp/g18.if"
			ok "Г-18: удаление интерфейса" "$r"
			;;
		*) refusal "Г-18: второй интерфейс" синхронно ;;
		esac
	fi
	[ "$QUOTA_HIT" = 0 ] || return 0
	# Другой образ: того же семейства (имя до первого дефиса), не тот же, и
	# не требует больше vCPU, чем самый малый flavor прогона.
	# Образ шлётся по id: по имени (image_name), как допускает опубликованная
	# спецификация, платформа ответила «не UUID» (прогон 201).
	# Образ должен помещаться на загрузочный диск ВМ: пересоздание диск не
	# растит (прогон 207: 422 disk_too_small).
	local img_id boot_gb
	boot_gb=$(evo tools call disk_list --json 2>/dev/null | jq -r --arg vm "$n" '[.disks[]? | select(.bootable == true and ((.attached_to // []) | index($vm)))][0].size_gb // empty' 2>/dev/null) || boot_gb=
	case "$boot_gb" in '' | *[!0-9]*) gap "Г-18: пересоздание" "размер загрузочного диска ВМ не прочитан в disk_list — образ не выбрать"; return 0 ;; esac
	read -r img img_id < <(evo tools call image_list --arg type=public --json 2>/dev/null |
		jq -r --arg cur "$IMAGE" --argjson gb "$boot_gb" '[.images[]? | select((.name // "") != $cur and ((.name // "") | split("-")[0]) == ($cur | split("-")[0]) and (.min_disk_gb // 0) <= $gb and (.id // "") != "")] | sort_by(.name) | last // {} | "\(.name // "") \(.id // "")"' 2>/dev/null) || true
	[ -n "${img:-}" ] && [ -n "${img_id:-}" ] || { gap "Г-18: пересоздание" "в image_list нет другого образа семейства $IMAGE, который помещается на диск ВМ ($boot_gb ГБ)"; return 0; }
	mask "$img_id"
	local meta
	vm_pw
	if ! meta=$(vm_meta "$img" "$n") || ! printf '%s' "$meta" | jq -e 'type == "object"' >/dev/null 2>&1; then
		gap "Г-18: пересоздание" "image_metadata образа $img не собраны: $(cat "$tmp/vm_meta.why" 2>/dev/null || true)"
		return 0
	fi
	boot_of() { evo tools call disk_list --json 2>/dev/null | jq -r --arg vm "$n" '[.disks[]? | select(.bootable == true and ((.attached_to // []) | index($vm)))][0].id // empty' 2>/dev/null || true; }
	boot0=$(boot_of)
	mask "$boot0"
	rebuild() { api POST "$url/rebuild" "$(jq -nc --arg i "$img_id" --argjson m "$meta" '{image_id: $i, image_metadata: $m}')"; }
	api GET "$url"
	rebuild
	case "$code" in
	2??) done_on="на запущенной" ;;
	*)
		refusal "Г-18: пересоздание на запущенной ВМ" синхронно
		[ "$QUOTA_HIT" = 0 ] || return 0
		# Остановка и вторая попытка — только если отказ про состояние ВМ
		# (машинный код), а не про тело запроса.
		case "$(mcode)" in *state* | *vm_can_not_be_changed*) ;; *) return 0 ;; esac
		power power_off
		case "$code" in 2??) ;; *) refusal "Г-18: остановка перед пересозданием" синхронно; return 0 ;; esac
		ops_reach "$url" 600 stopped || true
		rebuild
		case "$code" in
		2??) done_on="на остановленной" ;;
		*) refusal "Г-18: пересоздание на остановленной ВМ" синхронно; return 0 ;;
		esac
		;;
	esac
	r="HTTP $code $done_on, поля ответа: $(keys_of)"
	t0=$SECONDS
	ops_settle "$url" 900 || true
	r="$r; состояния: $chain, $((SECONDS - t0)) с; образ $([ "$(jq -r '.image.name? // empty' "$tmp/api.json" 2>/dev/null)" = "$img" ] && echo "тот, что просили" || echo "другой или не назван"); публичный адрес тот же: $(if [ -n "$ip0" ] && grep -qF "$ip0" "$tmp/api.json" 2>/dev/null; then echo да; else echo нет; fi)"
	boot1=$(boot_of)
	mask "$boot1"
	if [ -z "$boot0" ] || [ "$boot0" = "$boot1" ]; then
		ok "Г-18: пересоздание из $img" "$r; загрузочный диск тот же: $([ -n "$boot0" ] && echo да || echo "не прочитан"); вход по SSH не проверялся"
		return 0
	fi
	# Диск сменился: старый, если он остался без ВМ, платный и может быть
	# не под именем пробы — уборка по префиксу его не найдёт. Удаляется по
	# id, только когда он есть и ни к чему не привязан.
	local old
	old=$(evo tools call disk_list --json 2>/dev/null | jq -r --arg id "$boot0" '.disks[]? | select(.id == $id) | if ((.attached_to // []) | length) == 0 then "свободен" else "привязан" end' 2>/dev/null) || old=
	case "$old" in
	свободен)
		tc disk_delete --arg disk_id="$boot0"
		if [ "$rc" = 0 ]; then ok "Г-18: пересоздание из $img" "$r; загрузочный диск тот же: нет — старый диск без ВМ удалён по id; вход по SSH не проверялся"
		else bad "Г-18: пересоздание из $img" "$r; загрузочный диск сменился, старый остался без ВМ и не удалился (код $rc, $reason) — удалите в консоли Cloud.ru диск без ВМ этого прогона"; fi
		;;
	*) ok "Г-18: пересоздание из $img" "$r; загрузочный диск тот же: нет — старого диска $([ -n "$old" ] && echo "не отвязало (привязан)" || echo "в списке дисков нет"); вход по SSH не проверялся" ;;
	esac
}

area_ops() {
	local zid
	zid=$(zone_id)
	[ -n "$zid" ] || { bad "зона $ZONE" "не найдена в list_zones"; return 1; }
	# QUOTA_HIT — на одну пробу: отказ по квоте останавливает её, а не
	# следующую (у той другие ресурсы и своя строка Г-01).
	QUOTA_HIT=0
	! probe_on g10 || probe_g10 "$zid"
	QUOTA_HIT=0
	! probe_on g11 || probe_g11 "$zid"
	QUOTA_HIT=0
	! probe_on g16 || probe_g16 "$zid"
	QUOTA_HIT=0
	! probe_on g18 || probe_g18 "$zid"
	QUOTA_HIT=0
	! probe_on g17 || probe_g17 "$zid"
}

# --- Область dbchange: изменение готового кластера DBaaS (Г-13…Г-15) ---------
# Идёт в job своего сервиса (dbaas-pg, dbaas-kafka, dbaas-redis) после
# «Кластер READY», по слову входа probes; кластер создаёт и удаляет job.
# DB_KIND — pg, kafka или redis, DB_CLUSTER_ID — id кластера. Запросы —
# прямой PATCH /v1/clusters/{id} по описи, только поля из спецификации.
# Шаги: PostgreSQL — класс на шаг вверх, возврат класса, диск больше, число
# узлов +1; Kafka — диск больше, класс вверх, возврат; Redis — класс вверх,
# возврат, диск больше, одна попытка диска меньше (ожидаемый отказ). Класс
# — следующий по CPU и памяти в том же режиме развёртывания (<вид>_specs).
# После принятого шага — ожидание исходного состояния кластера (до
# DB_WAIT секунд); состояния по порядку и время — в строке. Доступность
# кластера во время шага проба не меряет — только состояние. Отказ —
# refusal (R-77); квота по машинному коду — конец пробы, без возврата и
# следующих шагов. Ресурсов область не создаёт.
DB_WAIT=${DB_WAIT:-2400}
# DB_SETTLE — сколько секунд неизменного состояния после принятого шага
# значат «шаг прошёл без смены состояния».
DB_SETTLE=${DB_SETTLE:-120}
# DB_BUDGET — срок всей пробы от её начала, секунд: после него новый шаг не
# шлётся и ожидания кончаются; возврат класса шлётся и после срока и ждёт
# не дольше DB_RETURN_WAIT. Худший ход пробы — DB_BUDGET + DB_RETURN_WAIT:
# у job должно остаться время на удаление кластера (заключение «Риска» 144).
DB_BUDGET=${DB_BUDGET:-3000}
DB_RETURN_WAIT=${DB_RETURN_WAIT:-600}
area_dbchange() {
	local kind=${DB_KIND:-} id=${DB_CLUSTER_ID:-} g host url s0 up spec0 disk0 inst0 field step
	local stuck=0 deadline=$(($(date +%s) + DB_BUDGET))
	case "$kind" in
	pg) g=Г-13 host=postgresql ;;
	kafka) g=Г-14 host=kafka ;;
	redis) g=Г-15 host=redis ;;
	*) bad dbchange "DB_KIND «$kind» — не pg, kafka, redis"; return 1 ;;
	esac
	[ -n "$id" ] || { bad dbchange "нет DB_CLUSTER_ID"; return 1; }
	mask "$id"
	iam_fresh || { gap "$g" "нет токена IAM (KEY_ID, SECRET)"; return 0; }
	url="https://$host.api.cloud.ru/v1/clusters/$id"
	# card ВЫРАЖЕНИЕ — поле карточки кластера (ответ бывает обёрнут в cluster).
	card() { jq -r "(.cluster? // .) | $1" "$tmp/api.json" 2>/dev/null || true; }
	api GET "$url"
	case "$code" in 2??) ;; *) gap "$g" "карточка кластера не прочитана: HTTP $code: $(api_why)"; return 0 ;; esac
	s0=$(card '.status // .state // empty')
	spec0=$(card '.specification_id // .specification.id // empty')
	inst0=$(card '.instances // empty')
	case "$kind" in
	pg) field=pg_data_gb disk0=$(card '.storage.pg_data_gb // empty') ;;
	kafka) field=storage_gib disk0=$(card '.storage_gib // empty') ;;
	redis) field=$(card 'if has("storage_gb") then "storage_gb" else "storage_gib" end') disk0=$(card '.storage_gb // .storage_gib // empty') ;;
	esac
	mask "$spec0"
	[ -n "$s0" ] || { gap "$g" "в карточке кластера нет состояния: поля $(keys_of)"; return 0; }
	up=$(evo tools call "${kind}_specs" --json 2>/dev/null | jq -r --arg cur "$spec0" '(.specifications // []) as $all | ($all[] | select(.id == $cur)) as $c
		| [$all[] | select((.deployment_mode // "") == ($c.deployment_mode // "") and ([.cpu, .memory_gb] > [$c.cpu, $c.memory_gb]))] | sort_by(.cpu, .memory_gb) | .[0].id // empty' 2>/dev/null) || up=
	mask "$up"
	# db_step ПРОВЕРКА ТЕЛО [return] — PATCH и ожидание исходного состояния;
	# код 1 — шаг не послан, отказ (уже записан) или кластер не вернулся в
	# исходное состояние (stuck=1). В кластер, который не вернулся, и после
	# срока пробы уходит только возврат класса (return): он уменьшает цену;
	# любой другой PATCH поверх незавершённого повышает шанс кластера,
	# который API не удаляет.
	db_step() {
		local t0=$SECONDS until s chain='' st='' n=0 since now
		now=$(date +%s)
		if [ "${3:-}" = return ]; then
			until=$((now + DB_RETURN_WAIT))
		else
			[ "$stuck" = 0 ] || { gap "$1" "кластер не вернулся в $s0, дальше не меняю"; return 1; }
			[ "$now" -lt "$deadline" ] || { gap "$1" "срок пробы ($((DB_BUDGET / 60)) мин) вышел — шаг не шлю"; return 1; }
			until=$((now + DB_WAIT))
			[ "$until" -le "$deadline" ] || until=$deadline
		fi
		iam_fresh || true
		step=''
		api PATCH "$url" "$(jq -c --arg id "$id" '{cluster_id: $id} + .' <<<"$2")"
		case "$code" in 2??) ;; *) refusal "$1" синхронно; return 1 ;; esac
		step="HTTP $code, поля ответа: $(keys_of)"
		mask "$(jq -r '.id? // empty' "$tmp/api.json" 2>/dev/null || true)"
		since=$(date +%s)
		while :; do
			iam_fresh || true
			api GET "$url"
			case "$code" in 2??) s=$(card '.status // .state // "без состояния"') ;; *) s="HTTP $code" ;; esac
			[ "$s" = "$st" ] || { chain="${chain:+$chain → }$s"; n=$((n + 1)); since=$(date +%s); }
			st=$s
			# Исходное состояние после смены — шаг завершён; исходное без
			# смены DB_SETTLE секунд — состояние не менялось.
			if [ "$s" = "$s0" ] && { [ "$n" -gt 1 ] || [ $(($(date +%s) - since)) -ge "$DB_SETTLE" ]; }; then break; fi
			[ "$(date +%s)" -lt "$until" ] || { chain="$chain (исходного состояния не дождались)"; break; }
			sleep "$PROBE_PAUSE"
		done
		ok "$1" "$step; состояния: $chain, $((SECONDS - t0)) с"
		[ "$st" = "$s0" ] || { stuck=1; return 1; }
	}
	class_steps() {
		[ -n "$up" ] || { gap "$g: класс на шаг вверх" "в ${kind}_specs нет класса выше текущего в его режиме"; return 0; }
		local sent=0 step=""
		db_step "$g: класс на шаг вверх" "$(jq -nc --arg s "$up" '{specification_id: $s}')" || true
		# Шаг принят (и тогда, когда исходного состояния не дождались) —
		# возврат класса в любом случае.
		case "$step" in HTTP\ 2*) sent=1 ;; esac
		[ "$sent" = 0 ] || db_step "$g: возврат класса" "$(jq -nc --arg s "$spec0" '{specification_id: $s}')" return || true
	}
	disk_body() {
		case "$kind" in
		pg) jq -nc --argjson v "$1" '{storage: {pg_data_gb: $v}}' ;;
		*) jq -nc --arg f "$field" --argjson v "$1" '{($f): $v}' ;;
		esac
	}
	disk_up() {
		case "$disk0" in '' | *[!0-9]*) gap "$g: диск больше" "размер диска в карточке не прочитан"; return 0 ;; esac
		db_step "$g: диск больше" "$(disk_body $((disk0 + $1)))" || true
	}
	case "$kind" in
	pg)
		class_steps
		[ "$QUOTA_HIT" = 0 ] || return 0
		disk_up 10
		[ "$QUOTA_HIT" = 0 ] || return 0
		case "$inst0" in '' | *[!0-9]*) gap "$g: число узлов" "число узлов в карточке не прочитано" ;; *) db_step "$g: число узлов" "$(jq -nc --argjson v $((inst0 + 1)) '{instances: $v}')" || true ;; esac
		;;
	kafka)
		disk_up 10
		[ "$QUOTA_HIT" = 0 ] || return 0
		class_steps
		;;
	redis)
		class_steps
		[ "$QUOTA_HIT" = 0 ] || return 0
		disk_up 1
		[ "$QUOTA_HIT" = 0 ] || return 0
		case "$disk0" in '' | *[!0-9]*) ;; *) db_step "$g: диск меньше (ожидаемый отказ)" "$(disk_body "$disk0")" || true ;; esac
		;;
	esac
}

case "$area" in
disk | net | dns | s3 | secrets | ca | stack | mcp | pgnet | ops | dbchange)
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
	# Префикс без номера прогона — уборка всех прогонов: только завершённых.
	case "$p" in *-r[0-9]*a[0-9]*) ;; *) done_only=1 ;; esac
	# Правило и маршрут тестовой сети — без номера прогона в имени: рядом с
	# другим живым запуском (e2e_live_runs.sh others) общая уборка их не
	# трогает — их снимет область net этого запуска или следующая уборка.
	others=0
	if [ "$done_only" = 1 ]; then
		others=$("$here/e2e_live_runs.sh" others) || { others=-1; failed=1; }
	fi
	case "$p" in
	evo-e2e- | evo-e2e-net-*)
		case "$others" in
		0) delete_test_rules || failed=1 ;;
		-1) kinds=${kinds/route /} ;; # список запусков не прочитан: ошибка уже названа
		*)
			echo "::notice::идёт запуск cloud-e2e — правило и маршрут тестовой сети не трогаю"
			kinds=${kinds/route /}
			;;
		esac ;;
	*) kinds=${kinds/route /} ;;
	esac
	sweep "$p" "$kinds" || failed=1
	;;
*)
	echo "::error::неизвестная область: $area"
	exit 2
	;;
esac
