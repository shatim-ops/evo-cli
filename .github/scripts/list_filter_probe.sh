#!/usr/bin/env bash
# jq-выражения в одинарных кавычках — намеренно: $v — переменная jq.
# shellcheck disable=SC2016
# Проба семантики отбора в облаке для фильтров списков 1.8.2 (ТЗ
# docs/internal/briefs/list-filters.md, «Тесты и проверки»; «да» владельца
# 11.11 — строка плана 1.8.2 «Фильтры списков», два прогона). Только GET,
# ключ наблюдателя. Для каждого параметра Compute из таблицы ТЗ: полный
# список проекта (без параметра), выборка по правилу evo из него и выборка
# облака с параметром — совпадают ли наборы id. В лог — имя проверки,
# номер значения (не само значение: id не печатаются), коды HTTP и счётчики;
# зоны и состояния — по имени, это не id.
#
# Итог строки: same — облако отбирает так же, как evo (параметр можно
# передавать в облако); diff — нет (параметр остаётся у evo, пункт в реестр
# расхождений evo-internal); ignored — облако вернуло весь список (параметр
# не работает); error — запрос с параметром не прошёл.
#
# Нужны KEY_ID, SECRET, PROJECT. Выход 1 — без секретов или без токена (как у
# шага «Проверки» api-probe); любая проверка, упавшая в облаке, печатает
# строку и выход не меняет.
#
# STRICT=1 (job readonly cloud-e2e на каждом кандидате; снижение R-76,
# заключение «Риска» 106): параметр, который evo уже передаёт облаку
# (IN_CLOUD), с итогом не same — выход 1 и строка ::error::. Такой
# параметр выходит из отбора в облаке (inCloud в internal/tools/list_filters.go),
# пункт — в реестр расхождений evo-internal.
set -euo pipefail
if [ -z "${KEY_ID:-}" ] || [ -z "${SECRET:-}" ] || [ -z "${PROJECT:-}" ]; then
	echo "::error::нет KEY_ID / SECRET / PROJECT"
	exit 1
fi
echo "::add-mask::$PROJECT"
redact() { sed -E -f "${GITHUB_WORKSPACE:-.}/.github/scripts/redact.sed"; }

TOKEN=$(jq -n --arg k "$KEY_ID" --arg s "$SECRET" '{keyId:$k, secret:$s}' |
	curl -sS -X POST https://iam.api.cloud.ru/api/v1/auth/token \
		-H 'Content-Type: application/json' --data-binary @- |
	jq -r '.access_token // .accessToken // empty')
[ -n "$TOKEN" ] || { echo "::error::нет токена"; exit 1; }
echo "::add-mask::$TOKEN"

C=https://compute.api.cloud.ru/api/v1

# IN_CLOUD — параметры, которые evo передаёт облаку (путь:параметр), как
# их объявляет inCloud в internal/tools/list_filters.go; список держит
# TestListFilters_CloudParamsAreProbed.
IN_CLOUD="vms:availability_zone_ids vms:statuses disks:availability_zone_id floating-ips:availability_zone_id subnets:availability_zone_ids security-groups:availability_zone_id"
broken=""
# strict PATH PARAM VERDICT — запоминает параметр облака с итогом не same.
strict() {
	[ "${STRICT:-}" = 1 ] || return 0
	case " $IN_CLOUD " in *" $1:$2 "*) ;; *) return 0 ;; esac
	[ "$3" = same ] && return 0
	case " $broken " in *" $1:$2 "*) ;; *) broken="$broken $1:$2" ;; esac
}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# get URL FILE — тело в FILE, код HTTP на stdout.
get() { curl -sS -H "Authorization: Bearer $TOKEN" -o "$2" -w '%{http_code}' "$1" || echo 000; }
# items FILE — элементы списка (поле-массив ответа или сам массив).
items() { jq -c 'if type == "array" then . else ([.[] | arrays] | first // []) end' "$1" 2>/dev/null || echo '[]'; }

# check NAME PATH PARAM VALUES_JQ RULE_JQ [QUERY_PREFIX]
#   VALUES_JQ — уникальные значения параметра по полному списку;
#   RULE_JQ   — правило evo: select(...) над элементом с $v.
check() {
	local name=$1 param=$3 values=$4 rule=$5 base="$C/$2?limit=100" i=0
	[ "${6:-project}" = project ] && base="$base&project_id=$PROJECT"
	local code
	code=$(get "$base" "$tmp/all")
	if [ "$code" != 200 ]; then
		printf '%-34s полный список http=%s\n' "$name" "$code" | redact
		return
	fi
	items "$tmp/all" >"$tmp/all.items"
	local total
	total=$(jq length "$tmp/all.items")
	# Не больше 4 значений на параметр: зон 2–3, состояний 2–4 — все; у
	# flavor, cpu и ram — первые 4: семантика параметра одна на все значения,
	# а каждая проверка — запрос к API.
	mapfile -t vals < <(jq -r "$values | .[]? | tostring" "$tmp/all.items" | head -n 4)
	if [ "${#vals[@]}" = 0 ]; then
		printf '%-34s всего=%s, значений нет — не проверено\n' "$name" "$total"
		return
	fi
	for v in "${vals[@]}"; do
		i=$((i + 1))
		jq -r --arg v "$v" "[.[] | $rule | .id] | unique | .[]" "$tmp/all.items" >"$tmp/evo"
		code=$(get "$base&$param=$(jq -rn --arg v "$v" '$v | @uri')" "$tmp/cloud")
		if [ "$code" != 200 ]; then
			printf '%-34s значение #%d: http=%s — error\n' "$name" "$i" "$code" | redact
			strict "$2" "$param" error
			continue
		fi
		# unique: повтор id в ответе облака — не повод для diff.
		items "$tmp/cloud" | jq -r '[.[] | .id] | unique | .[]' >"$tmp/cloudids"
		local nc ne verdict
		nc=$(wc -l <"$tmp/cloudids"); ne=$(wc -l <"$tmp/evo")
		if cmp -s "$tmp/cloudids" "$tmp/evo"; then verdict=same
		elif [ "$nc" = "$total" ] && [ "$ne" != "$total" ]; then verdict=ignored
		else verdict="diff"
		fi
		printf '%-34s значение #%d: всего=%s облако=%s evo=%s — %s\n' "$name" "$i" "$total" "$nc" "$ne" "$verdict"
		strict "$2" "$param" "$verdict"
	done
}

echo "== фильтры списков 1.8.2: облако против правила evo (только GET)"
check vms_zone          vms              availability_zone_ids '[.[] | .availability_zone.id] | unique' 'select(.availability_zone.id == $v)'
check vms_state         vms              statuses              '[.[] | (.state // .status)] | unique'  'select(((.state // .status) | ascii_downcase) == ($v | ascii_downcase))'
check vms_flavor        vms              flavor_ids            '[.[] | .flavor.id] | unique'           'select(.flavor.id == $v)'
check vms_empty_fip     vms              empty_fip             '["true", "false"]'                     'select(([.interfaces[]? | .floating_ip | objects] | length == 0) == ($v == "true"))'
check disks_zone        disks            availability_zone_id  '[.[] | .availability_zone.id] | unique' 'select(.availability_zone.id == $v)'
check fips_zone         floating-ips     availability_zone_id  '[.[] | .availability_zone.id // .availability_zone_id] | unique' 'select((.availability_zone.id // .availability_zone_id) == $v)'
check fips_free         floating-ips     free                  '["true", "false"]'                     'select(((.vm == null) and (.interface == null) and (.nat_gateway == null)) == ($v == "true"))'
check subnets_zone      subnets          availability_zone_ids '[.[] | .availability_zone.id] | unique' 'select(.availability_zone.id == $v)'
check subnets_vpc       subnets          vpc_id                '[.[] | .vpc_id] | unique'              'select(.vpc_id == $v)'
check sgs_zone          security-groups  availability_zone_id  '[.[] | .availability_zone.id] | unique' 'select(.availability_zone.id == $v)'
check snat_zone         nat-gateways     availability_zone_ids '[.[] | .availability_zone_id] | unique' 'select(.availability_zone_id == $v)'
check flavors_zone      flavors          availability_zone_id  '[.[] | .availability_zones[]? | .availability_zone_id] | unique' 'select([.availability_zones[]? | .availability_zone_id] | index($v))' global
check flavors_cpu       flavors          cpu                   '[.[] | .cpu] | unique'                 'select((.cpu | tostring) == $v)' global
check flavors_ram       flavors          ram                   '[.[] | .ram] | unique'                 'select((.ram | tostring) == $v)' global
if [ -n "$broken" ]; then
	echo "::error::облако отбирает не так, как evo, по параметрам, которые evo ему передаёт:$broken — убрать из inCloud, пункт в реестр расхождений"
	exit 1
fi
exit 0
