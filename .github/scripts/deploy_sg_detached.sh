#!/usr/bin/env bash
# Проверяем точную группу из плана. При наличии списка штатный CLI может
# выбрать другую уже готовую группу той же зоны через .evo.yaml. Токен в stdin.
set -euo pipefail
sg=${1:?нужен ID группы из превью}
catalog=${2:-}
[[ "$sg" =~ ^[A-Za-z0-9_-]{1,128}$ ]] || { echo "::error::некорректный ID группы из превью" >&2; exit 1; }
token=$(cat)
[ -n "$token" ] || { echo "::error::не получен токен для чтения группы" >&2; exit 1; }
body=$(mktemp)
trap 'rm -f "$body"' EXIT
attached() {
	local code
	code=$(curl -sS --connect-timeout 10 --max-time 60 -o "$body" -w '%{http_code}' -H "Authorization: Bearer $token" "https://compute.api.cloud.ru/api/v1/security-groups/$1") || return 1
	[ "$code" = 200 ] && jq -r --arg id "$1" 'select(.id == $id) | .has_interfaces | booleans' "$body"
}
case "$(attached "$sg")" in
false) [ -z "$catalog" ] || printf '%s\n' "$sg"; exit 0 ;;
true) ;;
*) echo "::error::GET выбранной группы не подтвердил состав интерфейсов" >&2; exit 1 ;;
esac
if [ -n "$catalog" ]; then
	# Для запасной группы выбираем консервативное подмножество правил:
	# активный входящий IPv4 TCP/22 с интернета и свободный исходящий IPv4-трафик.
	zone_id=$(jq -r --arg id "$sg" '.security_groups[]? | select(.id == $id) | .availability_zone_id | strings | select(length > 0)' "$catalog" | head -n 1)
	if [ -z "$zone_id" ]; then
		echo "::error::не найдена свободная группа для deploy (у выбранной группы нет availability_zone_id в каталоге), состав групп проекта сохранён" >&2
		exit 1
	fi
	in_zone=$(jq -r --arg id "$sg" --arg z "$zone_id" '
		[.security_groups[]? | select(.availability_zone_id == $z and .id != $id and ((.id // "") | test("^[A-Za-z0-9_-]{1,128}$")))] | length' "$catalog")
	candidates=$(jq -r --arg id "$sg" --arg z "$zone_id" '
		def active: ((.state // "created") | ascii_downcase | test("error|deleting") | not);
		def ipv4: ((.ether_type // "IPv4") | ascii_downcase) == "ipv4";
		def internet: active and ipv4 and .remote_security_group == null and ((.remote_security_group_id // "") == "") and
			((.remote_ip_prefix // "") == "" or .remote_ip_prefix == "0.0.0.0/0");
		def proto: ((.ip_protocol // "") | ascii_downcase);
		def anyproto: proto == "" or proto == "any";
		def allports: ((.port_range // "") | ascii_downcase) as $r | ["","any","0:65535","1:65535","0-65535","1-65535"] | index($r) != null;
		def covers22: allports or (
			((.port_range // "") | capture("^(?<lo>[0-9]+)(?:[:-](?<hi>[0-9]+))?$") // null) as $m
			| $m != null and (($m.lo | tonumber) <= 22) and ((($m.hi // $m.lo) | tonumber) >= 22));
		[.security_groups[]? | select(.availability_zone_id == $z and .id != $id and ((.id // "") | test("^[A-Za-z0-9_-]{1,128}$")))
			| select(any((.rules // [])[]; internet and ((.direction // "") | ascii_downcase) == "ingress" and
				(anyproto or proto == "tcp") and covers22))
			| select(any((.rules // [])[]; internet and ((.direction // "") | ascii_downcase) == "egress" and anyproto and allports))
			| .id] | join("\n")' "$catalog")
	matched=0 busy=0 unreadable=0
	while IFS= read -r candidate; do
		[ -n "$candidate" ] || continue
		matched=$((matched + 1))
		case "$(attached "$candidate")" in
		false) printf '%s\n' "$candidate"; exit 0 ;;
		true) busy=$((busy + 1)) ;;
		*) unreadable=$((unreadable + 1)) ;;
		esac
	done <<<"$candidates"
	echo "::error::не найдена свободная группа для deploy (основная занята; других в зоне: $in_zone, прошли правила TCP/22+IPv4 egress: $matched, заняты интерфейсами: $busy, не подтверждены GET: $unreadable), состав групп проекта сохранён" >&2
	exit 1
fi
echo "::error::не найдена свободная группа для deploy (основная занята; каталог запасных групп не передан), состав групп проекта сохранён" >&2
exit 1
