#!/usr/bin/env bash
# Проверка pg-repeat-probe.sh (job ci) на заглушке curl: повтор создания
# пользователя и базы шлётся только когда оба уже есть; ответ платформы —
# отказ или «принят» — записан кодом, машинным кодом и счётом по списку;
# принятый повтор — предупреждение; шаг не падает; ID кластера, токен и
# пароль в журнал не идут. Облака нет.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fail=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
# Заглушка curl: токен IAM; иначе «МЕТОД путь» — в $FAKE_DIR/calls, тело — в
# $FAKE_DIR/bodies, ответ — первая строка $FAKE_DIR/http «МЕТОД ОБРАЗЕЦ КОД
# [ТЕЛО]» (МЕТОД! — разовая).
cat >"$work/bin/curl" <<'FAKE'
#!/usr/bin/env bash
out=/dev/stdout url='' method=GET body=0
while [ $# -gt 0 ]; do
	case $1 in
	-o) out=$2; shift ;; -X) method=$2; shift ;; -w | -H) shift ;;
	--data-binary) body=1; shift ;;
	https://*) url=$1 ;;
	esac
	shift
done
case "$url" in */auth/token) cat >/dev/null; printf '{"access_token":"tok-secret"}'; exit 0 ;; esac
[ "$body" = 0 ] || { cat; echo; } >>"$FAKE_DIR/bodies"
p=${url#https://postgresql.api.cloud.ru/v1/clusters/}
echo "$method $p" >>"$FAKE_DIR/calls"
code=404 b='{}' i=0
while read -r m pat c rest; do
	i=$((i + 1))
	once=0
	case "$m" in *!) once=1 m=${m%!} ;; esac
	[ "$once" = 0 ] || ! grep -qx "$i" "$FAKE_DIR/used" 2>/dev/null || continue
	# shellcheck disable=SC2254 # образец — glob
	case "$method $p" in "$m "$pat)
		code=$c b=${rest:-{\}}
		[ "$once" = 0 ] || echo "$i" >>"$FAKE_DIR/used"
		break
		;;
	esac
done <"$FAKE_DIR/http"
[ "$code" != 000 ] || { printf '000'; exit 56; }
printf '%s' "$b" >"$out"
printf '%s' "$code"
FAKE
chmod +x "$work/bin/curl"
n=0
world() {
	n=$((n + 1))
	export FAKE_DIR="$work/w$n"
	mkdir "$FAKE_DIR"
	: >"$FAKE_DIR/calls"
}
run() { rc=0; PATH="$work/bin:$PATH" CLUSTER_ID=cl-1 KEY_ID=k SECRET=s PG_REPEAT_PAUSE=0 RUNNER_TEMP="$FAKE_DIR" "$here/pg-repeat-probe.sh" >"$FAKE_DIR/out" 2>&1 || rc=$?; }
want() { if [ "$2" != "$3" ]; then printf 'FAIL %s\n  got:  %s\n  want: %s\n' "$1" "$2" "$3"; fail=1; fi; }
has() { grep -q -- "$2" "$FAKE_DIR/out" || { echo "FAIL $1: нет строки «$2»"; sed 's/^/  | /' "$FAKE_DIR/out"; fail=1; }; }
calls() { tr '\n' ';' <"$FAKE_DIR/calls"; }
U='{"users":[{"name":"evo_e2e_user"},{"name":"dbadmin"}]}'
D='{"databases":[{"name":"evo_e2e_db"},{"name":"postgres"}]}'

# Платформа отвергает оба повтора: код, машинный код и текст записаны, в
# списках по-прежнему по одному, предупреждения нет.
world
cat >"$FAKE_DIR/http" <<H
GET cl-1/users 200 $U
GET cl-1/databases 200 $D
POST cl-1/users 409 {"code":6,"message":"user evo_e2e_user already exists","details":[]}
POST cl-1/databases 409 {"code":6,"message":"database evo_e2e_db already exists","details":[]}
H
run
want "отказ: код" "$rc" 0
want "отказ: запросы по порядку" "$(calls)" "GET cl-1/users;GET cl-1/databases;POST cl-1/users;POST cl-1/databases;GET cl-1/users;GET cl-1/databases;"
has отказ 'повтор создания пользователя: отвергнут: HTTP 409, поля ответа: \["code","details","message"\], машинный код: 6, текст: user evo_e2e_user already exists; пользователей с этим именем: было 1, через 0 с — 1'
has отказ 'повтор создания базы: отвергнут: HTTP 409.*машинный код: 6.*баз с этим именем: было 1, через 0 с — 1'
if grep -q '::warning::' "$FAKE_DIR/out"; then echo "FAIL отказ: предупреждение при отказе платформы"; fail=1; fi
# Тела повторов: то же имя; у базы — владелец; пароль — не в журнале.
want "отказ: имя пользователя в теле" "$(sed -n 1p "$FAKE_DIR/bodies" | jq -r '[.cluster_id, .name, (.password | length > 12)] | join(" ")')" "cl-1 evo_e2e_user true"
want "отказ: тело базы" "$(sed -n 2p "$FAKE_DIR/bodies" | jq -c .)" '{"cluster_id":"cl-1","name":"evo_e2e_db","owner":"evo_e2e_user"}'
pw=$(sed -n 1p "$FAKE_DIR/bodies" | jq -r .password)
if grep -v '::add-mask::' "$FAKE_DIR/out" | grep -qF -e "$pw" -e cl-1 -e tok-secret; then echo "FAIL отказ: пароль, ID кластера или токен в строке журнала"; fail=1; fi
[ "$(grep -c '::add-mask::' "$FAKE_DIR/out")" = 3 ] || { echo "FAIL отказ: масок не три (кластер, токен, пароль)"; fail=1; }

# Платформа принимает повтор: «принят» с полями ответа и предупреждение;
# счёт по списку показывает, появился ли второй.
world
cat >"$FAKE_DIR/http" <<H
GET! cl-1/users 200 $U
GET! cl-1/databases 200 $D
POST cl-1/users 200 {"name":"evo_e2e_user","granted_roles":[]}
POST cl-1/databases 409 {"code":6,"message":"exists"}
GET cl-1/users 200 {"users":[{"name":"evo_e2e_user"},{"name":"evo_e2e_user"}]}
GET cl-1/databases 200 $D
H
run
want "принят: код" "$rc" 0
has принят 'повтор создания пользователя: принят: HTTP 200, поля ответа: \["granted_roles","name"\]; пользователей с этим именем: было 1, через 0 с — 2'
has принят '::warning::повтор создания PostgreSQL принят платформой (пользователь: HTTP 200, база: HTTP 409)'

# Пользователя или базы до запроса нет (предыдущий шаг не дошёл): повтор
# не шлётся — иначе проба сама создала бы первого.
world
cat >"$FAKE_DIR/http" <<H
GET cl-1/users 200 {"users":[{"name":"dbadmin"}]}
GET cl-1/databases 200 $D
H
run
want "нет пользователя: код" "$rc" 0
want "нет пользователя: записи нет" "$(grep -c '^POST' "$FAKE_DIR/calls" || true)" 0
has "нет пользователя" 'не проведена — до запроса пользователей с именем пробы: 0, баз: 1'

# Список не прочитан — тоже без записи; ответа на повтор нет (обрыв) —
# «исход неизвестен», шаг не падает.
world
echo 'GET cl-1/* 500 {"message":"boom"}' >"$FAKE_DIR/http"
run
want "список не прочитан: записи нет" "$rc $(grep -c '^POST' "$FAKE_DIR/calls" || true)" "0 0"
world
cat >"$FAKE_DIR/http" <<H
GET cl-1/users 200 $U
GET cl-1/databases 200 $D
POST cl-1/users 000
POST cl-1/databases 000
H
run
want "обрыв: код" "$rc" 0
has обрыв 'повтор создания пользователя: ответа нет — исход запроса неизвестен'

# Нет ключа — не проведена, без единого запроса.
world
rc=0
PATH="$work/bin:$PATH" CLUSTER_ID=cl-1 RUNNER_TEMP="$FAKE_DIR" "$here/pg-repeat-probe.sh" >"$FAKE_DIR/out" 2>&1 || rc=$?
want "нет ключа: код и запросы" "$rc $(wc -l <"$FAKE_DIR/calls" | tr -d ' ')" "0 0"

[ "$fail" = 0 ] && echo "pg-repeat-probe: ok"
exit "$fail"
