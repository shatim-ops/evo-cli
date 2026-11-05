#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
# SPDX-FileContributor: Тимур Шакирбеков
# SPDX-License-Identifier: Apache-2.0
#
# Итог проверки readonly «удаление несуществующего ресурса» (D26) по ответу
# evo: одна строка «ok|warn|FAIL подробности».
#
#   delete_absent_verdict.sh КОД REASON ALREADY_DELETED
#
# ALREADY_DELETED — «already_deleted», если ответ — already_deleted с
# existed false, иначе пусто. FAIL — только ответ evo, который выглядит
# успехом без already_deleted, вопрос согласия (код 3) или ошибка не
# API_ERROR: это дефект evo. Отказ платформы (API_ERROR: 5xx, 403 AR-2 и
# CRT-2, другой 4xx) — warn «не покрыто»: evo честно передал отказ, а
# проверка D26 на этом сервисе не выполнена; 5xx — строка реестра
# расхождений (план 1.8, «сценарии cloud-e2e», п. 4; 3 наблюдения 04.11).
rc=${1:?код}
reason=${2:-}
gone=${3:-}
case "$rc:${reason%%,*}" in
0:-)
	if [ "$gone" = already_deleted ]; then echo "ok код 0, existed false"
	else echo "FAIL код 0 без already_deleted и existed false: что-то «удалилось»?"; fi ;;
3:*) echo "FAIL код 3: гейт спросил согласие на удаление того, чего нет" ;;
*:API_ERROR)
	if [[ $reason =~ HTTP\ 5[0-9][0-9] ]]; then
		echo "warn не покрыто: платформа ответила 5xx на удаление несуществующего ($reason) — строка реестра расхождений"
	else
		echo "warn не покрыто: отказ платформы ($reason)"
	fi ;;
*) echo "FAIL код $rc, reason $reason" ;;
esac
