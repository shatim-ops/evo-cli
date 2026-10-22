# Бриф на внедрение после 1.2.2 (по аудиту процессов 1.2.1)

Источник: `docs/internal/audits/process-1.2.1.md`. Нумерация П/В — оттуда.
Сессия внедрения — разработка (ветки и PR), не релиз. Каждый пункт — отдельный
PR с тестом, который падает без правки (§4 `CLAUDE.md`). Ни один PR не меняет
поведение продукта; «Миграция» не нужна.

## Решения владельца 22.10.2026 (опрос после аудита)

| Вопрос | Решение |
|---|---|
| В1 `-race` | оставить на трёх ОС; вернуться после PR 1 и PR 4, если Windows продолжит краснеть |
| В2 триггеры CI | только `main` и PR (push в ветки не запускает CI) |
| В3 `cloud-readonly` | открыть `main` и `claude/*`; без одобрения (настройка окружения — владелец) |
| В6 GitHub | secret scanning + push protection; required checks `test` ×3 ОС и `docker`; linear history, запрет force-push (настройки — владелец, до PR 2) |
| В9 аудиты Fable | полный на minor/major, дельта по чек-листу на patch |
| Сессия внедрения | новая сессия разработки «CLI+MCP · Процесс — разработка» по этому брифу, после тега 1.2.2; в `main` не сливает |
| Публикация | отчёт и бриф — в `docs/internal/audits/` |

Открытые вопросы аудита (§11 отчёта) без решения: В4, В5, В7 (CodeQL), В8, В10–В13 — решать по ходу второй волны.

## Решения владельца до старта (справочно, все приняты)

| Вопрос | Нужен для | Рекомендация |
|---|---|---|
| В1: `-race` на трёх ОС или Linux + еженедельно | PR 7 | после PR 1 можно отложить: CI и так станет 2–3 мин |
| В2: CI только `main` + PR | PR 2 | да |
| В3: `cloud-readonly` открыть `claude/*` | PR 9 | да (настройка окружения, делает владелец) |
| В6: secret scanning, push protection, required checks | вне кода | включить до PR 2, чтобы required checks совпали с именами job |
| В9: Fable полный на minor/major, дельта на patch | PR 6 | да |

## Порядок PR (первая волна, 1.2.2 → до 1.3)

| # | PR | Что | Тест-ограничитель | Труд |
|---|---|---|---|---|
| 1 | `ci: тестовые Timings в фабрике CLI-тестов` | `runCLI` получает `engine.Timings` (как `fastTimings` в `engine_test.go:393`); `TestEngineCommands_RepeatedDeployGate` с 450 с до секунд | прогон `go test ./internal/cli` < 60 с локально (проверка руками, не тестом) | 1 ч |
| 2 | `ci: триггеры и concurrency` | `push: branches: [main]`, `pull_request`, `workflow_call`; `concurrency` с отменой на не-`main`; `-count=1` только на `main`/теге | — | 1 ч |
| 3 | `ci: actionlint и deadcode` | два шага на Linux; deadcode — отчёт, не провал, пока не очищено | — | 0,5 ч |
| 4 | `test: мета-тест порогов времени и -shuffle` | `TestNoWallClockAssertions`; `-shuffle=on` на `main` | сам тест | 2 ч |
| 5 | `test: инварианты` (3 PR) | ссылки `D\d+` ↔ `DECISIONS.md` (и записать D17); бюджет `tools/list` в байтах; цена или `tariff_url` у каждого `Mutating` превью | сами тесты; могут упасть на текущем коде — тогда находка в план | 5–7 ч |
| 6 | `docs: процесс` | `SECURITY.md`; ритм аудитов и автозапуск Qwen в `CLAUDE.md` §4 и `review-guide`; `RELEASING.md`: откат без удаления тега, `release-1.x` при первой ломающей правке 2.0; шаблон 4 метрик в `handoff/README.md`; `standalone/{00-audit,FINAL_AUDIT,KICKOFF,stages,01-plan}` → `archive/` | `TestDocsExamples_Links` | 3 ч |
| 7 | `ci: qwen-review на opened/ready_for_review` | только PR из этого репозитория; `/qwen review` остаётся для повтора | — | 0,5 ч |
| 8 | `ci: classify` | job по путям → классы в outputs, метки PR, чек-лист в summary; `contract` и fuzz-набор по классу | — | 1 день |
| 9 | `ci: расписание cloud-e2e` | понедельник: e2e + dns + secrets + s3 из `main` с одобрением; суббота: `cleanup_only`; `live-plan` берёт области из путей, если входы пусты | — | 2 ч |
| 10 | `ci: еженедельный -race -count=3 -shuffle на трёх ОС` | детектор flaky, результат — issue | — | 0,5 ч |

После PR 1–3 — короткая запись в `release-plan.md` (раздел 1.2.2 или 1.3), чтобы
релизная сессия видела, что внедрено.

## Вторая волна (1.3, отдельная ветка, 4–6 дней)

1. П4: расширение `faultCloud` (кластеры, приложения, NLB, DNS), `faultSlow`,
   матрица «сбой на шаге k» из реестра; затем один транспорт повторов.
2. П9: инвариант «имя из облака только в полях данных»; еженедельный fuzz-job;
   цели: `internal/stack`, `deployplan`, ответы API.
3. П8: `sboms` в goreleaser, `attest-build-provenance`, cosign для образа —
   проверять на `1.3.0-rc.1` (В10).
4. П5 часть 2: классы по diff `evo tools list --json`.
5. Лимит тела `tools/call` 1 МБ по HTTP (В13).

## Что не делать (зафиксировано в аудите, §9)

Мутационное тестирование, gosec/semgrep/gitleaks в CI, merge queue, rc на
patch, ночной платный e2e, временные бюджеты как блокеры CI, `t.Parallel()`
по всему `internal/cli`.
