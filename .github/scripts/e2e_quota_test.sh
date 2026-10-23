#!/usr/bin/env bash
# Проверка e2e_quota.sh (job ci): какой отказ image_create — квота
# организации на пользовательские образы (CMP-16, пропуск шага), а какой —
# ошибка, которая роняет шаг.
set -euo pipefail
# shellcheck source-path=SCRIPTDIR source=e2e_quota.sh
. "$(dirname "$0")/e2e_quota.sh"
fail=0
check() { # текст ошибки | ждём: skip или fail
	local got=fail
	if image_quota_exceeded "$1"; then got=skip; fi
	if [ "$got" != "$2" ]; then
		printf 'FAIL\n  in:   %s\n  got:  %s\n  want: %s\n' "$1" "$got" "$2"
		fail=1
	fi
}
# Живой ответ (CMP-16, 18.10, ru.AZ-2), с опечаткой платформы «custome».
check "create image: API_ERROR: HTTP 422: organization_quota_exceeded: Organization quota for compute.image.custome exceeded" skip
check "Organization quota for compute.image.custom exceeded" skip
# Квота другого ресурса — не пропуск: шаг падает, как раньше.
check "create VM: HTTP 422: organization_quota_exceeded: Organization quota for compute.vm exceeded" fail
check "create disk: quota exceeded for compute.disk.ssd" fail
# Не квота.
check "image_create: API_ERROR: HTTP 500: internal error" fail
check "" fail
[ "$fail" = 0 ] && echo "e2e_quota: ok"
exit "$fail"
