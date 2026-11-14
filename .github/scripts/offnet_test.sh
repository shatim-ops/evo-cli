#!/usr/bin/env bash
# Проверка offnet.sh (сторож «тест не открывает соединений вне loopback»,
# строка плана 1.9 «TestDeployApply_DeployKeySSHUnchanged ходит в настоящую
# сеть»): разбор журнала strace — connect к адресу вне loopback из тестового
# бинарника — красный с пакетом, адресом и портом; loopback, сокеты unix,
# connect самого go и строки закрытого списка — нет.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

cat >"$work/trace" <<'T'
101<tools.test> connect(8, {sa_family=AF_INET, sin_port=htons(22), sin_addr=inet_addr("203.0.113.12")}, 16) = -1 EINPROGRESS (Operation now in progress)
101<tools.test> connect(9, {sa_family=AF_INET, sin_port=htons(41234), sin_addr=inet_addr("127.0.0.1")}, 16) = 0
102<engine.test> connect(3, {sa_family=AF_INET6, sin6_port=htons(443), sin6_flowinfo=htonl(0), inet_pton(AF_INET6, "2001:db8::1", &sin6_addr), sin6_scope_id=0}, 28) = -1 ENETUNREACH (Network is unreachable)
102<engine.test> connect(4, {sa_family=AF_INET6, sin6_port=htons(8080), sin6_flowinfo=htonl(0), inet_pton(AF_INET6, "::1", &sin6_addr), sin6_scope_id=0}, 28) = 0
103<cli.test> connect(5, {sa_family=AF_UNIX, sun_path="/run/x.sock"}, 110) = 0
104<go> connect(3, {sa_family=AF_INET, sin_port=htons(443), sin_addr=inet_addr("192.0.2.9")}, 16) = 0
105<verify.test> connect(6, {sa_family=AF_INET, sin_port=htons(53), sin_addr=inet_addr("127.0.0.53")}, 16) = 0
106<tools.test> connect(8, {sa_family=AF_INET, sin_port=htons(22), sin_addr=inet_addr("203.0.113.12")}, 16) = -1 EINPROGRESS (Operation now in progress)
107<tools.test> <... connect resumed>) = 0
T

rc=0
out=$("$here/offnet.sh" "$work/trace" 2>&1) || rc=$?
[ "$rc" = 1 ] || { echo "FAIL: соединения вне loopback — код $rc, ждали 1"; fail=1; }
for want in 'tools.test → 203.0.113.12:22 (2 раза)' 'engine.test → \[2001:db8::1\]:443 (1 раз)'; do
	grep -q "$want" <<<"$out" || { echo "FAIL: нет строки «$want»"; printf '%s\n' "$out" | sed 's/^/  | /'; fail=1; }
done
for not in '127.0.0.1' ' \[::1\]' 'x.sock' '192.0.2.9' '127.0.0.53'; do
	if grep -q "$not" <<<"$out"; then echo "FAIL: в выводе лишнее «$not»"; printf '%s\n' "$out" | sed 's/^/  | /'; fail=1; fi
done

# Только loopback, unix и go — код 0, строка-итог.
grep -v '203.0.113.12\|2001:db8' "$work/trace" >"$work/clean"
rc=0
out=$("$here/offnet.sh" "$work/clean" 2>&1) || rc=$?
[ "$rc" = 0 ] || { echo "FAIL: чистый журнал — код $rc"; printf '%s\n' "$out"; fail=1; }
grep -q 'соединений вне loopback нет' <<<"$out" || { echo "FAIL: чистый журнал без итога"; fail=1; }

# Строка закрытого списка снимает своё место, и только его.
rc=0
out=$(OFFNET_ALLOW=$'engine.test [2001:db8::1]:443 a documented unreachable address\n' "$here/offnet.sh" "$work/trace" 2>&1) || rc=$?
[ "$rc" = 1 ] || { echo "FAIL: список снял не только своё — код $rc"; fail=1; }
if grep -q '2001:db8' <<<"$out"; then echo "FAIL: строка списка не сняла своё место"; fail=1; fi

# Строка списка другого бинарника, чьё имя кончается так же («xtools.test»),
# и другого порта (:222) не снимает место.
rc=0
out=$(OFFNET_ALLOW=$'xtools.test 203.0.113.12:22 other binary\ntools.test 203.0.113.12:222 other port\n' "$here/offnet.sh" "$work/trace" 2>&1) || rc=$?
grep -q 'tools.test → 203.0.113.12:22 (2 раза)' <<<"$out" || { echo "FAIL: чужая строка списка сняла место"; printf '%s\n' "$out" | sed 's/^/  | /'; fail=1; }

# Пустой или непрочитанный журнал — красный: сторож, который ничего не
# видел, не говорит «чисто».
rc=0
: >"$work/empty"
"$here/offnet.sh" "$work/empty" >/dev/null 2>&1 || rc=$?
[ "$rc" = 2 ] || { echo "FAIL: пустой журнал — код $rc, ждали 2"; fail=1; }
rc=0
"$here/offnet.sh" "$work/nope" >/dev/null 2>&1 || rc=$?
[ "$rc" = 2 ] || { echo "FAIL: нет журнала — код $rc, ждали 2"; fail=1; }

[ "$fail" = 0 ] || exit 1
echo "offnet: ok"
