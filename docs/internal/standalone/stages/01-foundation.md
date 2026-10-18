# Этап 1 — Фундамент CLI: ошибки, коды возврата, таймауты, контекст

Контекст: см. `docs/internal/standalone/00-audit.md`, раздел 2, пункты 3, 4, 10.

## Scope

Только `cmd/cloudru-vm/main.go`, `internal/errors/`, `internal/output/`,
`internal/auth/envvar.go`, `internal/provision/adapter.go`, `internal/engine/destroy.go`.
Новых команд не добавлять. Поведение успешных сценариев не менять.

## Задачи

1. **Ошибки видны.** Убрать `os.Exit(1)` без вывода: при ошибке cobra (unknown
   command/flag) печатать сообщение и подсказку `--help` в stderr. Оставить
   `SilenceUsage`, но `SilenceErrors` компенсировать собственным выводом.
2. **`handleError` через `errors.As`**, а не приведение типа: подсказки из
   `clerrors` должны переживать обёртку `fmt.Errorf("...: %w")`.
3. **Коды возврата** — ввести в `internal/errors` и использовать в `main`:
   - `0` успех · `1` общая ошибка · `2` аутентификация/конфигурация
   - `3` требуется подтверждение (не дано / нет TTY) · `4` объект не найден
   - `5` ошибка API Cloud.ru (4xx/5xx кроме auth) · `130` прерывание пользователем
   Задокументировать в `--help` корневой команды.
4. **`destroy` без TTY**: если stdin не терминал и нет `--force` → ошибка, код 3.
   Никогда не возвращать 0, если ничего не удалено.
5. **Таймауты.** Один `*http.Client` с `Timeout: 60s` для IAM и compute
   (`main.go` где создаются клиенты; `envvar.go:35`; `adapter.go:43`).
6. **Контекст.** `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`
   в `main`, прокидывать во все команды; по отмене — код 130. На Windows
   `SIGTERM` не приходит — это нормально, `os.Interrupt` достаточно.
7. **`--json`:** ошибки в JSON-режиме печатать как `{"error": {"code": N, "message": "...", "hint": "..."}}`
   в stderr. Приглашение `destroy` «Type 'yes'» — в stderr, не в stdout.

## Не делать

- Не трогать `internal/mcp/*` (гейты MCP — этап 3).
- Не вводить `--verbose`/логирование (этап отдельный, P2).
- Не переименовывать команды и флаги.

## Критерии приёмки

```
cloudru-vm nosuchcmd; echo $?            # сообщение в stderr, код 1
cloudru-vm deploy --nosuchflag; echo $?  # сообщение в stderr, код 1
CLOUDRU_KEY_ID= CLOUDRU_SECRET= cloudru-vm list-zones; echo $?   # код 2, подсказка про configure/env
echo | cloudru-vm destroy; echo $?       # код 3, ВМ не тронута
cloudru-vm list-zones --json 2>&1 >/dev/null | jq .error.code   # число
```

Юнит-тесты: таблица «ошибка → код» в `internal/errors`, тест `handleError`
на обёрнутую ошибку. `go test -race ./...` зелёный.
