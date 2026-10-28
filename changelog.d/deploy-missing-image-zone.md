### Миграция

- **MCP `deploy` и `evo tools call deploy` нового развёртывания без образа,
  зоны, проекта или compose-файла отказывают до вопроса о согласии**
  (DECISIONS D48). Было: превью с ценой и `confirmation_required` (в
  `evo tools call deploy` без `--yes` — код 3), после согласия — ошибка
  («no boot image», «no subnet for zone  in the project», `COMPOSE_NOT_FOUND`).
  Стало: сразу ошибка, как у `evo deploy`: без образа или зоны —
  `CONFIG_INVALID`, код 2, поле `missing` (`image`, `region`); без
  compose-файла — `COMPOSE_NOT_FOUND`, код 1. Что поменять: скрипт или агент,
  который ждал `confirmation_required` на такой вызов, получает ошибку
  первым ответом — задайте `image`/`region` (аргумент, `.evo.yaml`, профиль)
  до вызова.
- **`evo deploy` без зоны** — `CONFIG_INVALID` «не задана зона доступности
  (region)», код 2, как и раньше; прежний текст «no subnet for zone  in the
  project» с пустой зоной ушёл. Зона больше не берётся молча из
  единственной подсети проекта без зоны (D15). Что поменять: задайте
  `--region`, `region` в `.evo.yaml` или `evo configure --region`.

### Исправлено

- MCP `deploy`, когда движок не собирается (ошибка конфигурации), отвечает
  этой ошибкой, а не превью без цены.
