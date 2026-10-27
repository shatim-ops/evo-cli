#!/usr/bin/env bash
# Текст отказа Foundation Models для комментария ревью Qwen
# (qwen-review.sh). Комментарий публичный: на 401, 403 и 404 — одна строка
# диагностики без тела ответа (E7, 1.4.0): тело шлюза может назвать его
# внутренние адреса, а причина и так ясна по коду. Иначе — сообщение API,
# обрезанное до 500 символов. Обратные кавычки — markdown комментария.
# shellcheck disable=SC2016

# fm_error КОД ФАЙЛ_ОТВЕТА — текст отказа.
fm_error() {
  local code=$1 resp=$2 msg
  case "$code" in
    401 | 403)
      printf 'Foundation Models ответил %s: ключ `CLOUDRU_FM_API_KEY` не принят этим сервисом — ключ не для Foundation Models, отозван или у сервисного аккаунта нет роли.' "$code"
      return
      ;;
    404)
      printf 'Foundation Models ответил 404: адрес `FM_BASE_URL` или модель `QWEN_MODEL` не найдены — эндпоинт сменился или модели нет.'
      return
      ;;
  esac
  msg=$(jq -r '.error.message // .error // .detail // .message // "без описания"' "$resp" 2>/dev/null | head -c 500 || true)
  printf 'Foundation Models ответил %s: %s.' "$code" "${msg:-без описания}"
}
