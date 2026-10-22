# Документация против бинаря, 21.10.2026

База: `2ab7c7965a33457d7108b28f6745f678f6f120b4` (`main`, выпуск 1.2.0 и протокол догфуда). Облако не вызывалось: превью шло в поддельный каталог.

Проверены `README.md`, `docs/guide.md`, `docs/install.md`, `docs/auth.md`, `docs/mcp.md`, `docs/troubleshooting.md`, `docs/exit-codes.md`, `docs/internal/dogfood-protocol.md`. Сгенерированные `docs/commands.md` и `docs/tools.md` не разбирались: их держат `TestCommandsDoc` и `TestToolsDoc`.

Ограничитель — `TestDocsExamples_*` в `internal/cli`. `go test ./...` падает только на `TestDocsExamples_ImageNotInProjectPreview`.

## Расхождения

| ID | Где | Что написано | Что делает бинарь | Как исправить |
|---|---|---|---|---|
| DOC-1 | `docs/exit-codes.md:44` | MCP `provision` проверяет образ, которого нет в каталоге проекта, при вызове с `confirmed=true`. Превью в облако не ходит. | Вызов без `confirmed` уже читает каталог и отвечает `NOT_FOUND`, код выхода 4. Превью `confirmation_required` не отдаётся, ВМ и floating IP не создаются. То же у `deploy`. | Было: `MCP provision проверяет это при вызове с confirmed=true: превью в облако не ходит.` Стало: `MCP provision и deploy проверяют это уже на превью, без confirmed=true: превью читает каталог и при недоступном образе отвечает NOT_FOUND, ничего не создавая.` |
| DOC-2 | `docs/mcp.md:482` | `provision` и `deploy` проверяют недоступный проекту образ в вызове с `confirmed=true`. Превью без подтверждения в облако не ходит. | То же, что DOC-1: отказ приходит на превью, до `confirmed=true`, после чтения каталога. Фраза про `deploy_plan` (проверка при построении плана) с бинарём сходится и в исправленном предложении остаётся. | Было: `provision и deploy проверяют это в вызове с confirmed=true (превью без подтверждения в облако не ходит), deploy_plan — при построении плана.` Стало: `provision и deploy проверяют это уже на превью, без confirmed=true (превью читает каталог и отвечает NOT_FOUND, ничего не создавая), deploy_plan — при построении плана.` |

Остальной абзац в `docs/exit-codes.md:37-43` с бинарём сходится: `evo vm create`, `evo deploy`, `evo init --image`, `provision`, `deploy` и `deploy_plan` отдают код 4 до вопроса и до создания; `evo image get` такого образа завершается с кодом 0 и пометкой.

## Сошлось с бинарём

- Пути команд, флаги и число позиционных в примерах восьми документов, включая алиасы и скрытые команды.
- 154 инструмента, 24 группы `--toolsets`, 89 инструментов под `--read-only`, 26 в `deploy,vm,vpc`, 17 инструментов только на чтение в `vm,vpc,disk,image`.
- Коды выхода 0, 1, 2, 3, 4, 5, 130 и `reason` в таблице `docs/exit-codes.md` (`USAGE`, `AUTH_FAILED`, `NOT_FOUND`, `API_ERROR` и остальные). Тот же ряд чисел печатает `evo --help`. Образец JSON с `reason: AUTH_FAILED` несёт `"code": 2`.
- JSON MCP для Cursor и Claude Code (`mcpServers.evo`): валидный JSON, `command` — `evo`, аргументы (`mcp`, `--read-only`, `--toolsets`, `--profile`) есть у команды.
- Относительные ссылки и якоря заголовков.
- Без облака (`HTTPS_PROXY=http://127.0.0.1:9`, пустой каталог настроек, фиктивные `CLOUDRU_KEY_ID`, `CLOUDRU_SECRET`, `CLOUDRU_PROJECT_ID`): `version` и `completion bash` — код 0, `mcp --help` называет `--read-only` и `--toolsets`, неизвестная группа `--toolsets`, пара `--read-only` с `--allow-secret-export` и неизвестная команда — код 1.

Имена переменных в `docs/auth.md` по-прежнему закрывает `TestAuthDoc_ListsEveryEnvVar`.

## Как устроен ограничитель

`TestDocsExamples_CommandLines` разбирает строки `evo …` в блоках кода и в `` `…` ``. Плейсхолдер (`<id>`, `<ID ВМ>`) — одно значение. Строка из одного `…` — образец, арность не фиксирована.

Закрытый список исключений (`docExampleSkip`):

| Документ | Строка | Почему это не расхождение |
|---|---|---|
| `README.md`, `docs/guide.md` | `evo <команда> --help` | На месте команды плейсхолдер, не имя. Флаги такой строки проверяются как устойчивые флаги корня. |

Отдельные правила разбора, чтобы каталог команд не выглядел как ложный запуск: упоминание команды без позиционных аргументов не заявляет арность; `list\|describe\|call`, ` / ` и `→` — перечень команд; `evo` в `go build -o bin/evo`, `rm ~/.local/bin/evo` и `docker run --name evo` — имя файла.
