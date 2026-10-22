# Этап 4 — Покрытие: `tools list|describe|call` + деревья команд

Контекст: `00-audit.md` разд. 1 («Покрытие CLI ↔ MCP»). Зависит от этапов 2
(креды) и 3 (`confirm`). Самый объёмный этап — делить на два PR.

## PR 4a — универсальный доступ ко всем 146 инструментам

1. **Реестр как данные.** Обработчики в `internal/mcp/tools.go` принимают
   `map[string]any` и возвращают JSON-строку + `isError`. Вынести реестр
   (имя, описание, JSON-schema аргументов, обработчик, флаг «изменяющая») в
   пакет, не зависящий от транспорта: `internal/tools`. MCP-сервер и CLI
   становятся двумя клиентами одного реестра. Поведение MCP не меняется
   (`server_test.go` зелёный без правок).
2. **Команды:**
   - `tools list [--group vm] [--json]` — таблица: имя, группа, изменяющая?, описание.
   - `tools describe <name>` — схема аргументов, человекочитаемо и `--json`.
   - `tools call <name> [--arg k=v]... [--args-json '{…}'] [--args-file f.json]`
     — вызывает обработчик. Значения `--arg` приводятся по схеме (string/number/bool/array через запятую).
     Ответ `confirmation_required` → через `confirm.Ask` (этап 3): при согласии —
     повторный вызов с `confirmed=true`. Вывод — JSON как есть; `isError` → код 5
     (или 4, если в ответе `not found`).
3. **`*_operation` ожидания** (`opwait.go`) — флаг `--wait` для `tools call`,
   использующий ту же логику capped backoff.

## PR 4b — первоклассные деревья (v1.0)

Группы: `vm`, `disk`, `vpc` (сети/подсети/SG/FIP), `k8s`, `pg`, `s3`,
`secret`. Каждая команда — тонкая обёртка над обработчиком реестра: разбор
флагов → `map[string]any` → вызов → форматирование (таблица по умолчанию,
`--json` как есть). Никакой бизнес-логики в cobra-слое.

Соглашения:
- глагол второго уровня: `list|get|create|delete|…`, имена из MCP без префикса группы;
- `--project-id`, `--region`, `--profile` наследуются от корня;
- изменяющие операции идут через `confirm.Ask`;
- id ресурса — позиционный аргумент.

Остальные 16 групп остаются через `tools call` (BACKLOG по мере спроса).

## Не делать

- Не переписывать обработчики; не менять их входы/выходы.
- Не добавлять интерактивные мастера.
- `deploy/status/logs/destroy/verify/init` не трогать (этап 5).

## Критерии приёмки

```
cloudru-vm tools list --json | jq length                    # 146
cloudru-vm tools describe vm_get | head                     # схема
cloudru-vm tools call list_zones                            # тот же JSON, что MCP
cloudru-vm tools call disk_delete --arg id=X </dev/null; echo $?   # код 3, ничего не удалено
cloudru-vm vm list; cloudru-vm vm get <id> --json
cloudru-vm k8s list; cloudru-vm pg list; cloudru-vm s3 ls <bucket>
```

Тест-инвариант: для каждого имени в реестре существует ровно один обработчик,
и для каждой команды дерева 4b есть соответствующий инструмент (table-driven
тест, чтобы деревья не расходились с реестром).
