#!/usr/bin/env bash
# Проверка k8s-delete.sh на поддельном evo, без облака: удаление принято —
# ждём, пока имя пропадёт из списка; отказ (кластер в обновлении, группа в
# переходе) — повтор удаления, когда состояние кластера сменилось, до
# предела шага; остаток — ::error:: с именем, состоянием и числом групп;
# отмена прогона — одна попытка (заключение «Риска» 189, У2: удаление,
# отвергнутое один раз, оставляло платный кластер без повтора).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# shellcheck source-path=SCRIPTDIR source=fake_evo.sh
. "$here/fake_evo.sh"
fake_evo_install "$work/bin"
export PATH="$work/bin:$PATH" K8S_DEL_POLL=0 RUNNER_TEMP="$work"
fail=0
n=0
N=evo-e2e-k8s-r7a1
fresh() {
	n=$((n + 1))
	export FAKE_DIR="$work/w$n"
	mkdir -p "$FAKE_DIR"
	fake_evo_answer k8s_nodepools 0 0 '{"node_pools":[{"id":"np1","status":"OBJECT_STATUS_RUNNING"},{"id":"np2","status":"OBJECT_STATUS_DELETING"}]}'
}
cl() { printf '{"clusters":[{"id":"c1","name":"%s","status":"%s"}]}' "$N" "$1"; }
none='{"clusters":[]}'
want() { if [ "$2" != "$3" ]; then printf 'FAIL %s\n  got:  %s\n  want: %s\n' "$1" "$2" "$3"; fail=1; fi; }
dels() { grep -c '^k8s_delete yes=1' "$FAKE_DIR/evo-calls" || true; }

# Удаление принято, кластер пропал — код 0, одна попытка.
fresh
fake_evo_answer k8s_list 0 0 "$(cl OBJECT_STATUS_RUNNING)"
fake_evo_answer k8s_list 1 0 "$(cl OBJECT_STATUS_DELETING)"
fake_evo_answer k8s_list 2 0 "$none"
fake_evo_answer k8s_delete 0 0 '{"status":"deleting"}'
rc=0
"$here/k8s-delete.sh" "$N" >"$work/out" 2>&1 || rc=$?
want "принято: код" "$rc" 0
want "принято: попыток" "$(dels)" 1
grep -q 'кластер удалён' "$work/out" || { echo "FAIL: принято: нет «кластер удалён»"; sed 's/^/  | /' "$work/out"; fail=1; }

# Отказ в обновлении; состояние сменилось — повтор, он принят, кластер пропал.
fresh
fake_evo_answer k8s_list 0 0 "$(cl OBJECT_STATUS_UPGRADING)"
fake_evo_answer k8s_list 1 0 "$(cl OBJECT_STATUS_UPGRADING)"
fake_evo_answer k8s_list 2 0 "$(cl OBJECT_STATUS_RUNNING)"
fake_evo_answer k8s_list 3 0 "$(cl OBJECT_STATUS_DELETING)"
fake_evo_answer k8s_list 4 0 "$none"
fake_evo_answer k8s_delete 0 1 '{"error":{"code":5,"reason":"API_ERROR","message":"cluster is upgrading"}}'
fake_evo_answer k8s_delete 1 0 '{"status":"deleting"}'
rc=0
"$here/k8s-delete.sh" "$N" >"$work/out" 2>&1 || rc=$?
want "отказ, затем смена состояния: код" "$rc" 0
want "отказ, затем смена состояния: попыток" "$(dels)" 2
grep -q 'удаление отвергнуто.*API_ERROR' "$work/out" || { echo "FAIL: отказ не назван"; sed 's/^/  | /' "$work/out"; fail=1; }

# Отказ, состояние не меняется — без повтора; срок вышел — ::error:: с
# именем, состоянием и числом групп, код 1.
fresh
fake_evo_answer k8s_list 0 0 "$(cl OBJECT_STATUS_UPGRADING)"
fake_evo_answer k8s_delete 0 1 '{"error":{"code":5,"reason":"API_ERROR","message":"cluster is upgrading"}}'
rc=0
K8S_DEL_WAIT=0 "$here/k8s-delete.sh" "$N" >"$work/out" 2>&1 || rc=$?
want "остаток: код" "$rc" 1
want "остаток: попыток" "$(dels)" 1
grep -q "::error::.*$N.*OBJECT_STATUS_UPGRADING.*групп узлов: 2" "$work/out" || { echo "FAIL: остаток не назван поимённо"; sed 's/^/  | /' "$work/out"; fail=1; }
if grep -v '::add-mask::' "$work/out" | grep -qw 'c1\|np1'; then echo "FAIL: id в строке лога"; fail=1; fi

# Отмена прогона — одна попытка, без ожидания.
fresh
fake_evo_answer k8s_list 0 0 "$(cl OBJECT_STATUS_RUNNING)"
fake_evo_answer k8s_delete 0 0 '{"status":"deleting"}'
rc=0
K8S_DEL_ONCE=true "$here/k8s-delete.sh" "$N" >"$work/out" 2>&1 || rc=$?
want "отмена: код" "$rc" 0
want "отмена: чтений списка" "$(grep -c '^k8s_list' "$FAKE_DIR/evo-calls")" 1

# Кластера нет — код 0, удалений нет; чужое имя — отказ.
fresh
fake_evo_answer k8s_list 0 0 "$none"
rc=0
"$here/k8s-delete.sh" "$N" >"$work/out" 2>&1 || rc=$?
want "кластера нет: код" "$rc" 0
want "кластера нет: попыток" "$(dels)" 0
rc=0
"$here/k8s-delete.sh" prod-cluster >"$work/out" 2>&1 || rc=$?
want "чужое имя: код" "$rc" 2

[ "$fail" = 0 ] && echo "k8s-delete: ok"
exit "$fail"
