# План внедрения по этапам: что делает владелец, что делает сессия

Источники: `process-1.2.1.md` (аудит), `process-1.2.1-brief.md` (ТЗ на
внедрение, порядок PR, правило делегирования, как устроены новые тесты),
`delta-1.2.2.md` (FB122-1). Решение владельца 22.10: внедрение ведёт **та же
сессия, что делала аудит**, после переключения модели на Opus 5.5; роль
сессии меняется с «только чтение» на «разработка» (ветки и PR, в `main` не
сливает). Этап закрыт, когда выполнен его критерий.

## Этап 0. Закрыть 1.2.2 (релизная сессия, владелец)

| # | Действие | Кто | Где |
|---|---|---|---|
| 0.1 | cloud-e2e на `5d2d054` без `os_smoke` (FB122-1), затем `cleanup_only` | владелец запускает и одобряет; релизная сессия читает итог | Actions → cloud-e2e → Run workflow (ветка `main`) → Review deployments → Approve |
| 0.2 | Тег `v1.2.2` на `5d2d054`, одобрение релиза | владелец | Actions → tag → Run workflow (`v1.2.2`, полный SHA) → затем Actions → release → Review deployments → Approve |
| 0.3 | Слить PR #187 (аудиты) и #188 (D17) | релизная сессия или владелец | GitHub → PR → Squash and merge |
| 0.4 | Handoff 1.2.2 с четырьмя метриками (бриф, «Метрики») | релизная сессия | `docs/internal/handoff/1.2.2.md` |

Критерий: тег стоит, релиз опубликован, `docs/internal/audits/*` и D17 в `main`.

## Этап 1. Настройки GitHub (владелец, 20–30 минут, один раз, до первого PR)

Все пункты — Settings репозитория `shatim-ops/evo-cli`.

| # | Действие | Где |
|---|---|---|
| 1.1 | Secret scanning → Enable; Push protection → Enable | Settings → Code security → Secret scanning |
| 1.2 | Ruleset на `main`: Require a pull request before merging (Required approvals: 0); Require status checks to pass — добавить `test (ubuntu-latest)`, `test (macos-latest)`, `test (windows-latest)`, `docker`; «Require branches to be up to date before merging» — выключено; Require linear history; Block force pushes; Restrict deletions. Enforcement: Active | Settings → Rules → Rulesets → New ruleset → New branch ruleset; Target branches → Add target → Include default branch |
| 1.3 | Окружение `cloud-readonly`: Deployment branches and tags → Selected branches and tags → Add deployment branch or tag rule → `main`, затем ещё одно правило `claude/**` | Settings → Environments → cloud-readonly |
| 1.4 | Сверить `cloud-e2e` и `release`: Required reviewers — владелец; у `release` Deployment branches and tags — только теги `v*`; у `cloud-e2e` ограничения ветки нет | Settings → Environments |
| 1.5 | Не включать пока: CodeQL (В7 не решён) | — |

Проверка 1.2: в следующем PR внизу видны четыре обязательных check'а, кнопка слияния недоступна при красном. Проверка 1.3: Actions → cloud-e2e → Run workflow с ветки `claude/*` → job `readonly` запускается, а не пропускается.

Критерий: 1.1–1.3 включены.

## Этап 2. Переключить эту сессию в разработку (владелец, 5 минут)

1. Дождаться этапа 0 (тег стоит, #187 слит): менять `ci.yml` между слиянием релизного PR и тегом не нужно — `release.yml` вызывает `ci.yml` на теге.
2. В этой сессии переключить модель на Opus 5.5 (переключатель модели в интерфейсе Claude Code; контекст сессии сохраняется).
3. Переименовать сессию: «CLI+MCP · Процесс — разработка» (правило §0 `CLAUDE.md`: роль — по названию).
4. Отправить сообщение:

   ```
   Роль — разработка: ветки и PR, в main не сливаешь. Работаем по
   docs/internal/audits/process-1.2.1-brief.md и process-1.2.1-stages.md.
   PR 0 (FB122-1) делает релизная сессия (#191). Начни с PR 1. Перед задачей, которую бриф
   отмечает как передаваемую, спрашивай: делаю сама или отдаём по брифу.
   ```

5. Сессия перечитывает `CLAUDE.md` §0, `release-plan.md`, последний handoff, бриф — и открывает первую ветку.

Критерий: открыт PR 1.

## Этап 3. Первая волна, быстрые PR (1–2 дня)

Одна ветка на PR от `origin/main`, имя `claude/proc-<номер>-<тема>`. Каждый PR: тест, падающий без правки → локальные проверки → PR → Qwen → разбор находок → зелёный CI → слияние.

| PR | Ветка | Что делает сессия | Что делает владелец |
|---|---|---|---|
| 1 — тестовые `Timings` | `claude/proc-1-cli-test-timings` | `runCLI` подставляет `engine.Timings` в фабрику; `TestEngineCommands_RepeatedDeployGate` с 450 с до секунд; `go test ./internal/cli` < 1 мин | `/qwen review`; слияние; увидеть CI ≈ 2–3 мин |
| 2 — автозапуск Qwen | `claude/proc-2-qwen-auto` | `qwen-review.yml`: `pull_request` `opened`/`ready_for_review` для PR из этого репозитория; команда `/qwen review` остаётся | последний ручной `/qwen review`; слияние |
| 3 — триггеры CI | `claude/proc-3-ci-triggers` | `push: branches: [main]`, `pull_request`, `workflow_call`; `concurrency` с отменой на не-`main`; `-count=1` только `main`/тег | слияние; убедиться, что required checks из 1.2 зелёные |

Критерий: CI на PR 2–3 мин; Qwen приходит сам; один прогон на push.

## Этап 4. Первая волна, остальное (неделя в фоне)

| PR | Ветка | Суть | Действие владельца |
|---|---|---|---|
| 4 | `claude/proc-4-wallclock` | два класса часов стенки: пороги времени и значения времени в сравниваемых ответах (находка #190), `-shuffle=on` на `main` | слияние |
| 5a | `claude/proc-5a-decision-refs` | тест ссылок `D\d+` ↔ `DECISIONS.md` | слияние |
| 5b | `claude/proc-5b-tools-list-budget` | бюджет `tools/list` в байтах | слияние |
| 5c | `claude/proc-5c-preview-price` | цена или `tariff_url` у каждого `Mutating` превью; закрытый список исключений | если тест упадёт на существующем инструменте — решение: `tariff_url` или исключение с причиной; слияние |
| 6 | `claude/proc-6-process-docs` | `SECURITY.md`; §4 `CLAUDE.md` (ритм аудитов, Qwen авто); `RELEASING.md` (откат без удаления тега, `release-1.x`, rc для minor с новой поверхностью агента); шаблон метрик в `handoff/README.md`; `standalone/{00-audit,FINAL_AUDIT,KICKOFF,stages,01-plan}` → `archive/` | ответ «сама или Cursor»; прочитать `SECURITY.md`; слияние |
| 7 | `claude/proc-7-actionlint-deadcode` | два шага на Linux | ответ «сама или Cursor»; слияние |
| 8 | `claude/proc-8-classify` | job `classify`: классы по путям → outputs, метки PR, чек-лист в summary; `contract` по классу | слияние; дальше на PR видны метки класса |
| 9 | `claude/proc-9-e2e-schedule` | расписание cloud-e2e: понедельник дешёвый набор из `main`, суббота `cleanup_only`; `live-plan` берёт области из путей при пустых входах | слияние; с этого момента каждый понедельник один клик Approve в cloud-e2e (с телефона) |
| 10 | `claude/proc-10-weekly-race` | еженедельный `-race -count=3 -shuffle=on` на трёх ОС; красное — issue | ответ «сама или Cursor»; слияние |

После слияния: запись в `release-plan.md` «внедрена первая волна аудита процессов» (через релизную сессию — один писатель); при желании брифы Cursor «документация против бинаря» и Grok «атака на `cloud-readonly` с ветки» — сессия пишет бриф, владелец отправляет.

Критерий: PR 1–10 слиты; required checks и метки работают; первый понедельничный прогон прошёл.

## Этап 5. Проверка первой волны (отдельная сессия Fable, только чтение)

Эта сессия теперь разработчик, поэтому проверку волны делает новая аудиторская сессия «CLI+MCP · Fable: проверка первой волны (только чтение)»: что внедрено против брифа, что отклонилось, что осталось; дельта-аудит ближайшего patch по чек-листу В9.

Действие владельца: открыть сессию, сообщение «Проверь первую волну по docs/internal/audits/process-1.2.1-brief.md». Одна страница в `docs/internal/audits/`.

## Этап 6. Вторая волна в 1.3 (эта сессия, отдельная ветка, 4–6 дней)

| # | Ветка | Что | Действие владельца |
|---|---|---|---|
| 6.1 | `claude/proc-11-fault-matrix` | расширение `faultCloud` (кластеры, приложения, NLB, DNS), `faultSlow`, матрица «сбой на шаге k» из реестра; затем один транспорт повторов | разбор найденных дефектов: каждый — PR, возможно «Миграция»; неделя на разбор |
| 6.2 | `claude/proc-12-untrusted-names` | инвариант имён из облака в `message`/`next`; поле `_untrusted` в `logs`, `ca_logs`, `k8s_kubectl` | слияние |
| 6.3 | `claude/proc-13-fuzz-weekly` | еженедельный fuzz по 5 мин на цель; цели `internal/stack`, `deployplan`, ответы API | слияние |
| 6.4 | `claude/proc-14-supply-chain` | `sboms` в goreleaser, `attest-build-provenance`, cosign для образа; раздел проверки в `RELEASING.md` | решение В8; проверяется на `1.3.0-rc.1` — одобрение rc в окружении `release` |
| 6.5 | `claude/proc-15-classify-registry` | классы по diff `evo tools list --json`; `live-plan` по классу | слияние |
| 6.6 | `claude/proc-16-http-body-limit` | лимит тела `tools/call` 1 МБ по HTTP | решение В13; слияние |
| 6.7 | бриф Grok: наборы инструментов и режим без удалений — `docs/internal/briefs/grok-attack-1.3.md` (цели A–G, вместе с `cloud-readonly` из ветки) | перед тегом 1.3, на `1.3.0-rc.1` | отправить бриф; выдать ключ наблюдателя в тестовом проекте и ресурсы с «вредными» именами |

Критерий: полный аудит Fable 1.3 перед тегом (отдельная сессия, В9).

## Этап 7. 1.4 и 2.0 (по мере работы над ними)

- 1.4: нагрузочный Go-тест 32 × 100 на фейке; бенчмарки перед minor; группы `wf` и `repo` — одной декларацией; потолок стоимости как инвариант одной точки учёта цены.
- 2.0: реестр по группам одной декларацией (П14); `release-1.x` от последнего тега 1.x при первой ломающей правке; удаление алиасов с тестами.

## Открытые решения по ходу

| Вопрос | Когда понадобится | Рекомендация |
|---|---|---|
| В4 набор еженедельного cloud-e2e | PR 9 | e2e + dns + secrets + s3 |
| В5 порог стоимости живой проверки до слияния | PR 8 | дешёвые области до, k8s и DBaaS после |
| В7 CodeQL | этап 1 или позже | включить, не блокирует PR |
| В8 SBOM и подпись образа | 6.4 | 1.3 |
| В10 rc для 1.3 | перед тегом 1.3 | `1.3.0-rc.1` на волну догфуда |
| В11 `release-1.x` при старте 2.0 | первая ломающая правка 2.0 | да |
| В12 один писатель `release-plan.md` | PR 6 | релизная сессия |
| В13 лимит тела `tools/call` | 6.6 | 1 МБ по HTTP |
