#!/usr/bin/env bash
# Проверка pr_title.sh на образцах заголовков: русский — зелёный, без
# кириллицы вне меток — красный.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fail=0
check() { # имя, ожидаемый код выхода, заголовок
	local got=0
	TITLE=$3 "$here/pr_title.sh" >/dev/null || got=$?
	if [ "$got" = "$2" ]; then echo "ok   $1"; else
		echo "FAIL $1: ждали $2, вышло $got — «$3»"; fail=1
	fi
}
check "русский с меткой процесса" 0 "Бриф пробы входа: после #1010 evo оставляет прежние имена действий (процесс)"
check "русский с меткой продукта" 0 "evo login: проверка прав по каталогу IAM (продукт, 1.8)"
check "имя из кода и русский" 0 "\`TestLogin_NoCreateBeforeTheGrant\`: нет записи до согласия"
check "английский с меткой процесса" 1 "Login probe brief: evo keeps its action names after #1010 (процесс)"
check "английский с меткой продукта" 1 "evo login: check the right with IAM actions (продукт, 1.8)"
check "только метка" 1 "(процесс)"
check "английский без метки" 1 "Bump actions/checkout from 7.0.1 to 7.0.2"
check "пусто" 1 ""
check "ё и Ё" 0 "Ёж (процесс)"
exit $fail
