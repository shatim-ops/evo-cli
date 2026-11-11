#!/usr/bin/env bash
# Проверка ssh_diag.sh без облака: evo — общая заглушка fake_evo.sh (ответы
# vm_get и list_security_groups), порт — локальный: слушающий (открыт) и
# закрытый (отказ). Случаи: адрес тот же, floating IP на интерфейсе, правило
# 22 из 0.0.0.0/0 в группе state; группа без правила на 22; другой адрес; vm_get
# не прочитан. В выводе нет адресов и id (redact.sed).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fail=0
work=$(mktemp -d)
listener=
trap '[ -z "$listener" ] || kill "$listener" 2>/dev/null; rm -rf "$work"' EXIT
# shellcheck source-path=SCRIPTDIR source=fake_evo.sh
. "$here/fake_evo.sh"
fake_evo_install "$work/bin"
vm=00000000-0000-4000-8000-0000000000d1
sg=00000000-0000-4000-8000-0000000000d2
groups='{"security_groups":[{"id":"'$sg'","name":"evo-app-sg","rules":[{"direction":"ingress","protocol":"tcp","port_range":"22:22","remote_ip_prefix":"0.0.0.0/0"},{"direction":"ingress","protocol":"tcp","port_range":"80:80","remote_ip_prefix":"0.0.0.0/0"}]},{"id":"x","name":"SSH-access_ru.AZ-9","rules":[{"direction":"ingress","protocol":"tcp","port_range":"80:80","remote_ip_prefix":"0.0.0.0/0"}]}]}'
# Свободный порт: слушатель на нём — «открыт»; после него — «отказ».
port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])')
python3 -c 'import socket,sys,time; s=socket.socket(); s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1); s.bind(("127.0.0.1",int(sys.argv[1]))); s.listen(5); time.sleep(60)' "$port" &
listener=$!
sleep 0.5

fips='{"floating_ips":[{"id":"f1","ip_address":"127.0.0.1","state":"active"}]}'

# run ИМЯ VM_GET_JSON [SG_ID] — вывод в $work/ИМЯ.out
run() {
	local d="$work/$1"
	mkdir -p "$d"
	export FAKE_DIR="$d"
	if [ -n "$2" ]; then fake_evo_answer vm_get 0 0 "$2"; else fake_evo_answer vm_get 0 1 '{"error":{"code":4,"reason":"NOT_FOUND","message":"no"}}'; fi
	fake_evo_answer list_security_groups 0 0 "$groups"
	fake_evo_answer list_floating_ips 0 0 "${FIPS:-$fips}"
	PATH="$work/bin:$PATH" SSH_PORT="${PORT:-$port}" TCP_WAIT=2 "$here/ssh_diag.sh" "$vm" 127.0.0.1 ${3:+"$3"} >"$d/out" 2>&1 || { echo "FAIL $1: код не 0"; fail=1; }
}
want() { # ИМЯ ПОДСТРОКА
	grep -qF -- "$2" "$work/$1/out" || { printf 'FAIL %s: нет «%s» в\n  %s\n' "$1" "$2" "$(cat "$work/$1/out")"; fail=1; }
}
run same '{"id":"'$vm'","state":"running","public_ip":"127.0.0.1","interfaces":[{"type":"regular","floating_ip":"127.0.0.1","state":"active"}]}' "$sg"
want same "ВМ running"
want same "адрес тот же"
want same "regular+fip(active)"
want same "evo-app-sg: 22 из любого адреса"
want same "TCP/22 с раннера — открыт"
want same "floating IP в active"
run nosg '{"id":"'$vm'","state":"running","public_ip":"","interfaces":[{"type":"regular"}]}'
want nosg "публичного адреса нет"
want nosg "по имени SSH-access_*, не группы ВМ — SSH-access_ru.AZ-9: правила на 22 нет"
kill "$listener" 2>/dev/null; wait "$listener" 2>/dev/null || true
listener=
run closed '{"id":"'$vm'","state":"running","public_ip":"127.0.0.2","interfaces":[]}' "$sg"
want closed "адрес другой"
want closed "TCP/22 с раннера — отказ: Connection refused"
FIPS='{"floating_ips":[{"id":"f1","ip_address":"127.0.0.1","state":"down"}]}' run fipdown '{"id":"'$vm'","state":"running","public_ip":"127.0.0.1","interfaces":[]}' "$sg"
want fipdown "floating IP в down"
FIPS='{"floating_ips":[]}' run nofip '{"id":"'$vm'","state":"running","public_ip":"127.0.0.1","interfaces":[]}' "$sg"
want nofip "floating IP с этим адресом не найден"
# Порт приложения (область mcp): правило и метка — по RULE_PORT.
RULE_PORT=80 run app '{"id":"'$vm'","state":"running","public_ip":"127.0.0.1","interfaces":[{"type":"regular","floating_ip":"127.0.0.1","state":"active"}]}' "$sg"
want app "evo-app-sg: 80 из любого адреса"
want app "TCP/80 с раннера — "
RULE_PORT=8086 run noapp '{"id":"'$vm'","state":"running","public_ip":"127.0.0.1","interfaces":[]}' "$sg"
want noapp "evo-app-sg: правила на 8086 нет"
run novm "" "$sg"
want novm "ВМ vm_get не прочитан"
! grep -q '0000-4000' "$work"/*/out || { echo "FAIL: id в выводе"; fail=1; }
[ "$fail" = 0 ] && echo "ssh_diag: ok"
exit "$fail"
