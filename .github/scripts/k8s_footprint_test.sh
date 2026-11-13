#!/usr/bin/env bash
# Проверка k8s-footprint.sh на поддельном evo: след кластера — ресурсы
# проекта, которых не было до создания и которые остались после удаления;
# ресурсы других job запуска (evo-e2e-*, кроме evo-e2e-k8s-*) не считаются;
# id — только под маской (заключение «Риска» 189, У3).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# shellcheck source-path=SCRIPTDIR source=fake_evo.sh
. "$here/fake_evo.sh"
fake_evo_install "$work/bin"
export PATH="$work/bin:$PATH" FAKE_DIR="$work/f"
mkdir -p "$FAKE_DIR"
fail=0
none() {
	fake_evo_answer disk_list "$1" 0 '{"disks":[]}'
	fake_evo_answer list_floating_ips "$1" 0 '{"floating_ips":[]}'
	fake_evo_answer list_security_groups "$1" 0 '{"security_groups":[{"id":"sg0","name":"default"}]}'
	fake_evo_answer lb_list "$1" 0 '{"load_balancers":[]}'
}
# До: одна чужая ВМ. После: та же, ВМ другого job (evo-e2e-net-…), узел
# кластера без имени e2e, его диск и ещё одна группа безопасности.
fake_evo_answer vm_list 0 0 '{"vms":[{"id":"vm0","name":"web"}]}'
none 0
fake_evo_answer vm_list 1 0 '{"vms":[{"id":"vm0","name":"web"},{"id":"vm9","name":"evo-e2e-net-r7a1-a"},{"id":"vmk","name":"mk8s-node-1"}]}'
fake_evo_answer disk_list 1 0 '{"disks":[{"id":"dk","name":"mk8s-node-1-boot"}]}'
fake_evo_answer list_floating_ips 1 0 '{"floating_ips":[]}'
fake_evo_answer list_security_groups 1 0 '{"security_groups":[{"id":"sg0","name":"default"},{"id":"sgk","name":"evo-e2e-k8s-r7a1"}]}'
fake_evo_answer lb_list 1 0 '{"load_balancers":[]}'
"$here/k8s-footprint.sh" save "$work/snap" >"$work/out" 2>&1
grep -q 'прочитано видов: 5 из 5' "$work/out" || { echo "FAIL: снимок до создания"; sed 's/^/  | /' "$work/out"; fail=1; }
"$here/k8s-footprint.sh" check "$work/snap" >"$work/out" 2>&1
grep -q '::warning::след кластера в проекте после его удаления: disk_list 1, list_security_groups 1, vm_list 1' "$work/out" || { echo "FAIL: след не назван по видам"; sed 's/^/  | /' "$work/out"; fail=1; }
if grep -v '::add-mask::' "$work/out" | grep -qw 'vmk\|dk\|sgk'; then echo "FAIL: id в строке лога"; fail=1; fi
grep -q '::add-mask::vm9' "$work/out" && { echo "FAIL: ВМ другого job посчитана следом"; fail=1; }
# Ничего нового — «след: 0».
rm -f "$FAKE_DIR"/.n.*
fake_evo_answer vm_list 1 0 '{"vms":[{"id":"vm0","name":"web"}]}'
fake_evo_answer disk_list 1 0 '{"disks":[]}'
fake_evo_answer list_security_groups 1 0 '{"security_groups":[{"id":"sg0","name":"default"}]}'
"$here/k8s-footprint.sh" save "$work/snap" >/dev/null 2>&1
"$here/k8s-footprint.sh" check "$work/snap" >"$work/out" 2>&1
grep -q 'след кластера в проекте: 0' "$work/out" || { echo "FAIL: нет «след: 0»"; sed 's/^/  | /' "$work/out"; fail=1; }
# Список не прочитан — код 2, не «0».
rm -f "$FAKE_DIR"/.n.* "$FAKE_DIR"/lb_list.*
rc=0
"$here/k8s-footprint.sh" check "$work/snap" >"$work/out" 2>&1 || rc=$?
[ "$rc" = 2 ] || { echo "FAIL: непрочитанный список: код $rc, ждём 2"; fail=1; }
grep -q 'след кластера в проекте: 0' "$work/out" && { echo "FAIL: непрочитанный список дал «след: 0»"; fail=1; }
[ "$fail" = 0 ] && echo "k8s-footprint: ok"
exit "$fail"
