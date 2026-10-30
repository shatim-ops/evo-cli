### Добавлено

- **Превью ВМ говорит, проверена ли квота проекта** (DECISIONS D60): `evo vm create`, `evo deploy` и MCP `provision`, `deploy`. С ключом проекта, который квоты не читает, — строка «квота: не проверена (ключ проекта)» и поле `quota: {checked: false, reason: "project_key"}`; сбой чтения — `not_read`; квоты прочитаны, но evo ещё не сверяет их типы — `not_mapped`. Отказа по квоте до вопроса пока нет.
- **`evo org quotas` и MCP `org_quotas` называют тип квоты:** `name`, `unit`, `product` из справочника типов квот рядом с кодом `resource`.
