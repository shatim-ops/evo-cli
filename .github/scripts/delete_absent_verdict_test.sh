#!/usr/bin/env bash
# Проверка delete_absent_verdict.sh на образцах ответов evo: FAIL — только
# дефект evo, отказ платформы — warn «не покрыто».
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fail=0
check() { # имя, ожидаемое начало, код, reason, already_deleted
	local got
	got=$("$here/delete_absent_verdict.sh" "$3" "$4" "$5")
	case "$got" in "$2"*) echo "ok   $1" ;; *) echo "FAIL $1: ждали «$2…», вышло «$got»"; fail=1 ;; esac
}
check "already_deleted" "ok" 0 "-" already_deleted
check "успех без already_deleted" "FAIL код 0" 0 "-" ""
check "вопрос согласия" "FAIL код 3" 3 "CONFIRMATION_REQUIRED" ""
check "5xx платформы" "warn не покрыто: платформа ответила 5xx" 1 "API_ERROR, HTTP 502" ""
check "503 платформы" "warn не покрыто: платформа ответила 5xx" 1 "API_ERROR, HTTP 503: upstream" ""
check "403 AR-2" "warn не покрыто: отказ платформы" 1 "API_ERROR, HTTP 403" ""
check "400 платформы" "warn не покрыто: отказ платформы" 1 "API_ERROR, HTTP 400" ""
check "ошибка не API_ERROR" "FAIL код 4" 4 "NOT_FOUND" ""
check "USAGE" "FAIL код 2" 2 "USAGE" ""
exit $fail
