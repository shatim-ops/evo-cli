#!/usr/bin/env bash
# Стенд 1.9 (план, Г-50…Г-54): ресурсы, которые живут дольше запусков
# cloud-e2e, — на них до тега пробуют операции второго дня. Здесь —
# единственное место, где назван их префикс. Он не evo-e2e-*: уборка
# запусков (cleanup_only, уборка областей, dbaas-delete.sh) отбирает по
# evo-e2e- и стенд не видит; исключение — именем, а не пропуском вида
# ресурса. Закреплено TestWorkflows_CleanupLeavesTheStand и случаями
# «стенд» в cloud_live_ops_test.sh и dbaas_delete_test.sh.
#
# Подключение: . e2e_stand.sh — STAND_PREFIX и stand_names.
# Запуск: e2e_stand.sh prefix | names.
STAND_PREFIX=evo-stand-19

# stand_names — «вид<TAB>имя» ресурсов, которые создаёт стенд (Г-51). Имена
# производных ресурсов evo строит сам от имени ВМ: адрес fip-<ВМ>-…,
# загрузочный диск <ВМ>-boot — их находит префикс.
stand_names() {
	printf '%s\t%s\n' \
		vm "$STAND_PREFIX-vm" \
		k8s "$STAND_PREFIX-k8s" \
		pg "$STAND_PREFIX-pg" \
		backup "$STAND_PREFIX-bk"
}

# Запущен, а не подключён.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	case "${1:-}" in
	prefix) printf '%s\n' "$STAND_PREFIX" ;;
	names) stand_names ;;
	*)
		echo "::error::e2e_stand.sh: prefix или names" >&2
		exit 2
		;;
	esac
fi
