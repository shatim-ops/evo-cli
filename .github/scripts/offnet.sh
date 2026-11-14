#!/usr/bin/env bash
# offnet.sh ЖУРНАЛ — сторож «тест не открывает соединений вне loopback»
# (строка плана 1.9 о TestDeployApply_DeployKeySSHUnchanged: тест ждал
# «порт не ответил» от 203.0.113.12:22 и падал в сети, где этот адрес
# принимает TCP; в CI был зелёным, потому что адрес не маршрутизируется).
# ЖУРНАЛ — вывод strace -f --seccomp-bpf --decode-pids=comm -e trace=connect
# над go test ./...: каждый connect к адресу вне loopback из тестового
# бинарника (*.test) — строка «пакет → адрес:порт (N раз)», код 1.
# Loopback (127.0.0.0/8, ::1), сокеты unix и connect самого go (загрузка
# модулей) не считаются. Закрытый список — OFFNET_ALLOW: строки
# «бинарник адрес:порт причина». Пустой или непрочитанный журнал — код 2:
# сторож, который ничего не видел, не говорит «чисто».
set -euo pipefail
trace=${1:?usage: offnet.sh STRACE_LOG}
[ -e "$trace" ] || { echo "offnet: журнала strace нет ($trace) — strace не запустился, проверка не состоялась" >&2; exit 2; }
[ -s "$trace" ] || { echo "offnet: журнал strace пуст ($trace) — проверка не состоялась" >&2; exit 2; }

found=$(awk '
	# pid<comm> connect(fd, {sa_family=AF_INET...}...)
	match($0, /^[0-9]+<[^>]*>/) {
		comm = substr($0, RSTART, RLENGTH); sub(/^[0-9]+</, "", comm); sub(/>$/, "", comm)
		if (comm !~ /\.test$/ || $0 !~ / connect\(/) next
		port = ""; addr = ""
		if (match($0, /port=htons\([0-9]+\)/)) { port = substr($0, RSTART + 11, RLENGTH - 12) }
		if ($0 ~ /sa_family=AF_INET6/ && match($0, /AF_INET6, "[^"]+"/)) {
			addr = substr($0, RSTART + 11, RLENGTH - 12)
			if (addr == "::1" || addr ~ /^::ffff:127\./) next
			addr = "[" addr "]"
		} else if ($0 ~ /sa_family=AF_INET,/ && match($0, /inet_addr\("[^"]+"\)/)) {
			addr = substr($0, RSTART + 11, RLENGTH - 13)
			if (addr ~ /^127\./) next
		} else next
		print comm " " addr ":" port
	}' "$trace" | sort | uniq -c | awk '{ print $2, $3, $1 }')

allow=${OFFNET_ALLOW:-}
bad=0
while read -r comm where n; do
	[ -n "$comm" ] || continue
	# Exact first two fields: a substring would let «ax.test …» cover «x.test …».
	if [ -n "$allow" ] && printf '%s\n' "$allow" | awk -v c="$comm" -v w="$where" '$1 == c && $2 == w { f = 1 } END { exit !f }'; then continue; fi
	[ "$n" = 1 ] && times="1 раз" || times="$n раза"
	echo "::error::offnet: $comm → $where ($times) — тест открыл соединение вне loopback: подмените проверку сети (как deployplan.DialSSH) или объявите место в OFFNET_ALLOW с причиной"
	bad=1
done <<<"$found"
[ "$bad" = 0 ] || exit 1
echo "offnet: соединений вне loopback нет"
