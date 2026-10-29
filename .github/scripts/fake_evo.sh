#!/usr/bin/env bash
# Поддельный evo для проверок скриптов (.github/scripts/*_test.sh): один на
# все, в формате настоящего evo с --json (QA-12) — код 0: ответ в stdout;
# иначе ошибка {"error":…} в stderr и пустой stdout. Своя копия у каждой
# проверки разошлась с настоящим: dbaas_delete_test.sh писал ошибку в stdout,
# скрипт читал её оттуда, ветки FAILED и «ещё создаётся» не срабатывали ни
# разу (run 144, #525, #526).
#
# fake_evo_install КАТАЛОГ — кладёт КАТАЛОГ/evo. Ответы — в $FAKE_DIR:
#   <инструмент>.<N>.json и <инструмент>.<N>.rc (кода нет — 0) — N-й вызов
#   инструмента, с нуля; вызов после последнего файла повторяет последний;
#   <инструмент>.yes.<N>.* — вызов с --yes, когда такие файлы есть.
# Каждый вызов — строка «<инструмент> yes=0|1» в $FAKE_DIR/evo-calls. Вызов
# без файла ответа — ошибка USAGE (код 2): проверка не задала ответ. Вызовы
# одного инструмента — по очереди: счётчик ответов без блокировки, как и
# скрипты, которые проверки запускают (параллельно — разные инструменты).
# Ответ не проверяется на JSON: мусор вместо ответа — тоже случай проверки.

fake_evo_install() {
	mkdir -p "$1"
	cat >"$1/evo" <<'EOF'
#!/usr/bin/env bash
d=${FAKE_DIR:?FAKE_DIR is not set}
yes=0 tool=
while [ $# -gt 0 ]; do
	case $1 in
	--yes) yes=1 ;;
	call) tool=$2; shift ;;
	esac
	shift
done
echo "$tool yes=$yes" >>"$d/evo-calls"
key=$tool
if [ "$yes" = 1 ] && [ -f "$d/$tool.yes.0.json" ]; then key=$tool.yes; fi
n=$(cat "$d/.n.$key" 2>/dev/null || echo 0)
if [ -f "$d/$key.$n.json" ]; then
	echo $((n + 1)) >"$d/.n.$key"
elif [ "$n" -gt 0 ]; then
	n=$((n - 1))
else
	printf '{"error":{"code":2,"reason":"USAGE","message":"fake evo: no answer for %s call %s"}}\n' "$key" "$n" >&2
	exit 2
fi
rc=$(cat "$d/$key.$n.rc" 2>/dev/null || echo 0)
if [ "$rc" = 0 ]; then cat "$d/$key.$n.json"; else cat "$d/$key.$n.json" >&2; fi
exit "$rc"
EOF
	chmod +x "$1/evo"
}

# fake_evo_answer ИНСТРУМЕНТ N КОД JSON — ответ N-го вызова.
fake_evo_answer() {
	case $2 in
	'' | *[!0-9]* | 0?*)
		echo "fake_evo_answer: N is 0, 1, 2… — got '$2'" >&2
		return 1
		;;
	esac
	printf '%s\n' "$4" >"$FAKE_DIR/$1.$2.json"
	echo "$3" >"$FAKE_DIR/$1.$2.rc"
}
