#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
# SPDX-FileContributor: Тимур Шакирбеков
# SPDX-License-Identifier: Apache-2.0
#
# Почему не поднялся SSH к ВМ прогона (cloud-e2e: job e2e и область disk
# cloud-live.sh): одна строка фактов вместо «не поднялся за N мин» (rc.2 1.7,
# run 41831665204: disk 2 из 2 «Connection timed out», класс не установлен).
#
#   ssh_diag.sh VM_ID IP [SG_ID]
#
# Факты, только чтение и без секретов:
#   ВМ — state из vm_get;
#   адрес — совпадает ли public_ip vm_get с адресом, к которому шёл SSH, и
#     есть ли floating IP на интерфейсе (тип интерфейса);
#   группа — правило ingress на TCP/22 и откуда («любого адреса» — 0.0.0.0/0;
#     узкий префикс redact.sed маскирует): в группе SG_ID (deploy
#     пишет её в state), иначе в группах SSH-access_* (provision без своей
#     группы ставит ВМ туда);
#   порт — TCP/22 с раннера: открыт, отказ (RST) или нет ответа.
# Вывод — через redact.sed (ID и адреса маскируются), код выхода — 0:
# диагностика не меняет итог шага.
here=$(cd "$(dirname "$0")" && pwd)
redact() { sed -E -f "$here/redact.sed"; }
vm=${1:?id ВМ}
ip=${2:?адрес ВМ}
sg=${3:-}
TCP_WAIT=${TCP_WAIT:-5}
# Порт — 22; другой только у проверки скрипта (ssh_diag_test.sh).
SSH_PORT=${SSH_PORT:-22}

state='?' addr='?' iface='?' rule='?' port='?'
if v=$(evo tools call vm_get --arg vm_id="$vm" --json 2>/dev/null); then
	state=$(jq -r '.state // "?"' <<<"$v")
	pub=$(jq -r '.public_ip // empty' <<<"$v")
	if [ -z "$pub" ]; then addr="публичного адреса нет"
	elif [ "$pub" = "$ip" ]; then addr="тот же"
	else addr="другой (vm_get: $pub)"; fi
	iface=$(jq -r '[.interfaces[]? | "\(.type // "?")\(if .floating_ip then "+fip" else "" end)\(if .state then "(\(.state))" else "" end)"] | join(",") | if . == "" then "нет" else . end' <<<"$v")
else
	state="vm_get не прочитан"
fi

# Правило TCP/22 ingress: port_range «22:22», диапазон с 22 или любой порт.
if g=$(evo tools call list_security_groups --arg rules=true --json 2>/dev/null); then
	rule=$(jq -r --arg sg "$sg" '
		[.security_groups[]? | select(if $sg != "" then .id == $sg else ((.name // "") | startswith("SSH-access_")) end)] as $gs
		| if ($gs | length) == 0 then "группа не найдена"
		  else [$gs[] | . as $g | [.rules[]? | select(.direction == "ingress" and ((.protocol // "tcp") | ascii_downcase | test("tcp|any|all|^$")))
		      | select((.port_range // "") as $p | $p == "" or $p == "any" or ($p | split(":") | map(tonumber? // 0) | (.[0] <= 22 and (.[1] // .[0]) >= 22)))
		      | (.remote_ip_prefix // "") | if . == "" or . == "0.0.0.0/0" or . == "::/0" then "любого адреса" else . end] | unique
		      | "\($g.name // "?"): \(if length == 0 then "правила на 22 нет" else "22 из " + join(" ") end)"] | join("; ")
		  end' <<<"$g" 2>/dev/null) || rule="правила не разобраны"
else
	rule="list_security_groups не прочитан"
fi

timeout "$TCP_WAIT" bash -c "</dev/tcp/$ip/$SSH_PORT" 2>/dev/null
case $? in
0) port="открыт" ;;
124) port="нет ответа за $TCP_WAIT с" ;;
*) port="отказ (соединение сброшено)" ;;
esac

printf 'SSH-диагностика: ВМ %s; адрес %s; интерфейсы %s; группа — %s; TCP/22 с раннера — %s\n' \
	"$state" "$addr" "$iface" "$rule" "$port" | redact
exit 0
