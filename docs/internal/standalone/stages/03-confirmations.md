# Этап 3 — Подтверждения: `--yes`, `--dry-run`, превью с ценой, гейты в MCP

Контекст: `00-audit.md` разд. 2 п. 8; README «Подтверждение изменяющих
операций». Зависит от этапа 1 (код 3).

## Принцип

Одна модель согласия для человека и агента. В MCP она уже есть:
`status: confirmation_required` → повторный вызов с `confirmed=true`. CLI
обязан отображать её, а не обходить.

## Задачи

1. **Общий примитив** `internal/confirm`: `Ask(ctx, preview Preview, opts) (bool, error)`.
   - есть TTY и нет `--yes` → печать превью в stderr, вопрос `[y/N]`;
   - нет TTY и нет `--yes` → ошибка `ErrConfirmationRequired`, код 3, превью в stderr
     (в `--json` — объект `{"status":"confirmation_required","preview":{…}}` в stdout, код 3);
   - `--yes` → без вопроса, превью всё равно печатается в stderr одной строкой.
   Глобальные флаги: `--yes/-y`, `--dry-run` (печатает превью и выходит с 0).
2. **`deploy`:** перед созданием ВМ — превью: flavor, зона, образ, диск,
   оценка `internal/pricing` (с пометкой «оценка, не счёт»). Переиспользовать
   `deploy_plan` из `internal/deployplan`, не дублировать.
3. **`destroy`:** заменить `fmt.Scanln` на `confirm.Ask`; превью — список
   ресурсов к удалению, включая floating IP (сам релиз FIP — этап 5).
4. **Гейты в MCP** для операций, которых сейчас нет под подтверждением:
   `deploy`, `provision` (платная ВМ), `ca_start`/`ca_stop`,
   `cert_issue_le`/`cert_import`/`cert_renew`, `s3_put`. Паттерн — как у
   существующих 19 групп в `internal/mcp/tools.go`.
5. **`confirmation_required` в MCP** возвращается с `isError=true`
   (`tools.go:~1814`). Обсудить: по спецификации MCP это не ошибка. Поменять
   на `isError=false` **только** если внутренний MCP-клиент сборочной системы
   (код возврата 3) различает по `status`, а не по `isError` — проверить
   текст README и, если сомнение, оставить и записать в BACKLOG.

## Не делать

- Не добавлять новые команды CLI (этап 4).
- Не менять текст превью существующих MCP-инструментов.

## Критерии приёмки

```
cloudru-vm deploy --dry-run; echo $?                 # превью с ценой, 0, ВМ нет
echo | cloudru-vm deploy; echo $?                    # код 3, ВМ нет
cloudru-vm deploy --yes                              # создаёт, превью в stderr
cloudru-vm destroy --json </dev/null; echo $?        # JSON confirmation_required, код 3
```

Тесты: `confirm.Ask` для четырёх комбинаций TTY×yes; MCP-тесты на новые гейты
(по образцу `tools_image_test.go`: первый вызов — превью, второй с
`confirmed=true` — действие).
