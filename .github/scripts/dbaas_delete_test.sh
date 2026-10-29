#!/usr/bin/env bash
# Проверка dbaas-delete.sh (job ci) на поддельном evo: ERASED и пропавший из
# списка кластер — удалён, повтор удаления без согласия — already_deleted
# (D44, D26); FAILED — сразу ошибка, без повторов 120 мин (D44). Облака нет.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fail=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# Поддельный evo — общий (fake_evo.sh): kafka_list — следующий ответ
# списка (последний повторяется); kafka_delete с --yes — kafka_delete.yes.N,
# без него — превью kafka_delete.0.
# shellcheck source-path=SCRIPTDIR source=fake_evo.sh
. "$here/fake_evo.sh"
fake_evo_install "$work/bin"

# run ИМЯ КОД_ВЫХОДА ЧИСЛО_ВЫЗОВОВ_С_YES СПИСОК.0 [СПИСОК.1 …] — ответы
# kafka_delete уже положены в $FAKE_DIR (fresh).
run() {
	local name=$1 want=$2 yes=$3 i=0 rc got
	shift 3
	for l in "$@"; do fake_evo_answer kafka_list "$i" 0 "$l"; i=$((i + 1)); done
	set +e
	PATH="$work/bin:$PATH" DBAAS_POLL=0 RUNNER_TEMP="$FAKE_DIR" "$here/dbaas-delete.sh" kafka evo-e2e-kafka >"$FAKE_DIR/out" 2>&1
	rc=$?
	set -e
	got=$(grep -c 'yes=1' "$FAKE_DIR/evo-calls" 2>/dev/null || true)
	if [ "$rc" != "$want" ] || [ "${got:-0}" != "$yes" ]; then
		printf 'FAIL %s: код %s (ждём %s), удалений с --yes %s (ждём %s)\n' "$name" "$rc" "$want" "${got:-0}" "$yes"
		sed 's/^/  | /' "$FAKE_DIR/out"
		fail=1
	fi
}
fresh() {
	export FAKE_DIR="$work/$1"
	mkdir -p "$FAKE_DIR"
	fake_evo_answer kafka_delete.yes 0 0 '{"status":"deleting","id":"a"}'
	fake_evo_answer kafka_delete 0 0 '{"status":"already_deleted","existed":false,"cluster_id":"a"}'
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
fake_evo_answer kafka_delete 0 3 '{"error":{"code":3,"reason":"CONFIRMATION_REQUIRED","message":"confirmation required"}}'
run asks 3 1 "$running" "$erased"

# FAILED: отказ с путём — код 1 сразу, одно удаление, без повторов.
fresh failed
fake_evo_answer kafka_delete.yes 0 5 '{"error":{"code":5,"reason":"API_ERROR","message":"delete cluster: dbaas API 500","hint":"Cloud.ru does not delete a FAILED cluster: delete it in the Cloud.ru console or contact support"}}'
run failed 1 1 "$failed"
grep -q 'путь — в ответе evo' "$FAKE_DIR/out" || { echo "FAIL failed: нет строки о пути в ответе"; fail=1; }

# FAILED без пути в ответе — тоже код 1, и ошибка говорит, что пути нет.
fresh failed-nopath
fake_evo_answer kafka_delete.yes 0 5 '{"error":{"code":5,"reason":"API_ERROR","message":"delete cluster: dbaas API 500"}}'
run failed-nopath 1 1 "$failed"
grep -q 'не называет путь' "$FAKE_DIR/out" || { echo "FAIL failed-nopath: нет строки «не называет путь»"; fail=1; }

# Кластер ещё создаётся (DEPLOYING): Cloud.ru отказывает FailedPrecondition
# (ошибка — в stderr), скрипт повторяет; второе удаление принято, кластер
# ERASED — код 0, два удаления с --yes (#525: ветка не срабатывала ни разу).
deploying='{"clusters":[{"id":"a","name":"evo-e2e-kafka","status":"DEPLOYING"}]}'
fresh creating
fake_evo_answer kafka_delete.yes 0 5 '{"error":{"code":5,"reason":"API_ERROR","message":"delete cluster: dbaas API 400: FailedPrecondition: cluster is DEPLOYING"}}'
fake_evo_answer kafka_delete.yes 1 0 '{"status":"deleting","id":"a"}'
run creating 0 2 "$deploying" "$deploying" "$erased"
grep -q 'ещё создаётся' "$FAKE_DIR/out" || { echo "FAIL creating: нет строки «ещё создаётся»"; fail=1; }

# Уже нет (NOT_FOUND в stderr): принято без повторов.
fresh notfound
fake_evo_answer kafka_delete.yes 0 4 '{"error":{"code":4,"reason":"NOT_FOUND","message":"cluster not found"}}'
run notfound 0 1 "$running" "$none"
grep -q 'кластера уже нет' "$FAKE_DIR/out" || { echo "FAIL notfound: нет строки «кластера уже нет»"; fail=1; }

# Кластеров нет с самого начала: ни удаления, ни повтора.
fresh empty
run empty 0 0 "$none"
if grep -q '_delete' "$FAKE_DIR/evo-calls"; then echo "FAIL empty: был вызов удаления"; fail=1; fi

[ "$fail" = 0 ] && echo "dbaas-delete: ok"
exit "$fail"
