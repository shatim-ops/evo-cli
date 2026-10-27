#!/usr/bin/env bash
# Проверка dbaas-delete.sh (job ci) на поддельном evo: ERASED и пропавший из
# списка кластер — удалён, повтор удаления без согласия — already_deleted
# (D44, D26); FAILED — сразу ошибка, без повторов 120 мин (D44). Облака нет.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fail=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# Поддельный evo: tools call <kind>_list — следующий файл list.N (последний
# повторяется); <kind>_delete с --yes — del-yes.json и код del-yes.rc, без
# него — del-preview.json и del-preview.rc. Каждый вызов — строка в calls.
mkdir -p "$work/bin"
cat >"$work/bin/evo" <<'EOF'
#!/usr/bin/env bash
d=$FAKE_DIR
yes=0
[ "$1" = --yes ] && { yes=1; shift; }
tool=$3
echo "$tool yes=$yes" >>"$d/calls"
case "$tool" in
*_list)
	n=$(cat "$d/n" 2>/dev/null || echo 0)
	f="$d/list.$n"
	[ -f "$f" ] && echo $((n + 1)) >"$d/n" || f="$d/list.$((n - 1))"
	cat "$f"
	;;
*_delete)
	if [ "$yes" = 1 ]; then cat "$d/del-yes.json"; exit "$(cat "$d/del-yes.rc")"; fi
	cat "$d/del-preview.json"
	exit "$(cat "$d/del-preview.rc")"
	;;
esac
EOF
chmod +x "$work/bin/evo"

# case ИМЯ КОД_ВЫХОДА ЧИСЛО_ВЫЗОВОВ_С_YES list.0 [list.1 …] — del-* уже
# положены в $FAKE_DIR.
run() {
	local name=$1 want=$2 yes=$3 i=0 rc got
	shift 3
	for l in "$@"; do printf '%s\n' "$l" >"$FAKE_DIR/list.$i"; i=$((i + 1)); done
	set +e
	PATH="$work/bin:$PATH" DBAAS_POLL=0 RUNNER_TEMP="$FAKE_DIR" "$here/dbaas-delete.sh" kafka evo-e2e-kafka >"$FAKE_DIR/out" 2>&1
	rc=$?
	set -e
	got=$(grep -c 'yes=1' "$FAKE_DIR/calls" 2>/dev/null || true)
	if [ "$rc" != "$want" ] || [ "${got:-0}" != "$yes" ]; then
		printf 'FAIL %s: код %s (ждём %s), удалений с --yes %s (ждём %s)\n' "$name" "$rc" "$want" "${got:-0}" "$yes"
		sed 's/^/  | /' "$FAKE_DIR/out"
		fail=1
	fi
}
fresh() {
	export FAKE_DIR="$work/$1"
	mkdir -p "$FAKE_DIR"
	printf '{"status":"deleting","id":"a"}' >"$FAKE_DIR/del-yes.json"
	echo 0 >"$FAKE_DIR/del-yes.rc"
	printf '{"status":"already_deleted","existed":false,"cluster_id":"a"}' >"$FAKE_DIR/del-preview.json"
	echo 0 >"$FAKE_DIR/del-preview.rc"
}
running='{"clusters":[{"id":"a","name":"evo-e2e-kafka","status":"RUNNING"}]}'
erased='{"clusters":[{"id":"a","name":"evo-e2e-kafka","status":"ERASED"}]}'
failed='{"clusters":[{"id":"a","name":"evo-e2e-kafka","status":"FAILED"}]}'
none='{"clusters":[]}'

# Удаление принято, кластер остался в списке ERASED: удалён, повтор —
# already_deleted (живой прогон 27.10: ERASED+HEALTHY).
fresh erased
run erased 0 1 "$running" "$erased"

# Кластер пропал из списка: повтор по его id — already_deleted (404, D26).
fresh vanished
run vanished 0 1 "$running" "$none"

# Повтор ERASED ответил превью с вопросом (1.3.0: confirmation_required) —
# код 3, а кластер не удаляется второй раз.
fresh asks
printf '{"status":"confirmation_required"}' >"$FAKE_DIR/del-preview.json"
echo 3 >"$FAKE_DIR/del-preview.rc"
run asks 3 1 "$running" "$erased"

# FAILED: отказ с путём — код 1 сразу, одно удаление, без повторов.
fresh failed
printf '{"status":"error","reason":"API_ERROR","error":"delete cluster: dbaas API 500","hint":"Cloud.ru does not delete a FAILED cluster: delete it in the Cloud.ru console or contact support"}' >"$FAKE_DIR/del-yes.json"
echo 5 >"$FAKE_DIR/del-yes.rc"
run failed 1 1 "$failed"
grep -q 'путь — в ответе evo' "$FAKE_DIR/out" || { echo "FAIL failed: нет строки о пути в ответе"; fail=1; }

# FAILED без пути в ответе — тоже код 1, и ошибка говорит, что пути нет.
fresh failed-nopath
printf '{"status":"error","reason":"API_ERROR","error":"delete cluster: dbaas API 500"}' >"$FAKE_DIR/del-yes.json"
echo 5 >"$FAKE_DIR/del-yes.rc"
run failed-nopath 1 1 "$failed"
grep -q 'не называет путь' "$FAKE_DIR/out" || { echo "FAIL failed-nopath: нет строки «не называет путь»"; fail=1; }

# Кластеров нет с самого начала: ни удаления, ни повтора.
fresh empty
run empty 0 0 "$none"
if grep -q '_delete' "$FAKE_DIR/calls"; then echo "FAIL empty: был вызов удаления"; fail=1; fi

[ "$fail" = 0 ] && echo "dbaas-delete: ok"
exit "$fail"
