#!/usr/bin/env bash
# Отказ image_create по квоте организации на пользовательские образы
# (реестр CMP-16: «2 шт.» на организацию, 422 organization_quota_exceeded,
# «quota for compute.image.custome»). Квота — платформы и тестовой
# организации, не evo: шаг cloud-e2e disk помечается «не покрыто», а не
# FAIL. Квота другого ресурса — по-прежнему FAIL. Проверка —
# e2e_quota_test.sh (job ci). Источник: cloud-live.sh.
# image_quota_exceeded ТЕКСТ_ОШИБКИ — 0, если это квота образов.
image_quota_exceeded() {
	printf '%s' "$1" | grep -qi 'quota' && printf '%s' "$1" | grep -qi 'compute\.image'
}
