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
# Команда, кроме tools call (evo deploy, evo destroy…), — исполняемый
# $FAKE_DIR/<команда>.run с аргументами вызова (fake_evo_run): долгий
# процесс с обработчиком SIGINT, файлы проекта. Сценария нет — USAGE.

fake_evo_install() {
	mkdir -p "$1"
	cat >"$1/evo" <<'EOF'
#!/usr/bin/env bash
d=${FAKE_DIR:?FAKE_DIR is not set}
for a in "$@"; do
	case $a in
	-*) ;;
	tools) break ;;
	*)
		echo "$a" >>"$d/evo-calls"
		[ -x "$d/$a.run" ] && exec "$d/$a.run" "$@"
		printf '{"error":{"code":2,"reason":"USAGE","message":"fake evo: no run for %s"}}\n' "$a" >&2
		exit 2
		;;
	esac
done
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

# fake_evo_run КОМАНДА — сценарий команды КОМАНДА со stdin: evo КОМАНДА …
# исполняет его с аргументами вызова. Заглушке, которая ловит SIGINT, нужен
# не bash: фоновый запуск bash игнорирует SIGINT, и trap на него в
# дочернем bash не встаёт (настоящий evo ставит обработчик сам,
# signal.NotifyContext).
fake_evo_run() {
	cat >"$FAKE_DIR/$1.run"
	chmod +x "$FAKE_DIR/$1.run"
}
