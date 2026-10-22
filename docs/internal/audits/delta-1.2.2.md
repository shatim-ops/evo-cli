# Дельта-аудит 1.2.2 (Fable, 22.10.2026)

Первый прогон чек-листа В9: аудит по diff `v1.2.1..5d2d054` (релизный PR
#186 слит, тег ещё не поставлен). 14 коммитов, 62 файла, +2 000/−299.
Только чтение; в облако не ходил.

**Вердикт: тег `v1.2.2` на `5d2d054` можно ставить.** Одна находка — по
гигиене публичных логов CI, не по продукту (FB122-1); закрывается одним
правилом в `redact.sed` первым PR после тега или до ближайшего прогона
`os_smoke`.

## Проверки

| Проверка | Результат |
|---|---|
| CI на `5d2d054` | зелёный: прогон 39761389754 (test ×3 ОС, docker) |
| Локально на `5d2d054` | gofmt чисто, `go vet` чисто, `go test -count=1 ./...` — 40 пакетов, 0 падений (8 мин 04 с, из них 450 с — известный `TestEngineCommands_RepeatedDeployGate`) |
| Утечки в diff | по маске UUID/IP/ключей — только примеры RFC 5737 (`203.0.113.9`), `192.168.0.1` как пример отказа и нулевые UUID фейков |
| Тест, падающий без правки | у каждого изменения: `TestBilling_ProjectBoundNoAgreementBalance`, `TestBilling_SumsWithVAT`, `TestHandleError_JSONDetails`, `TestHandleError_TextPassword`, `TestS3_BucketNameRefusedInCLILanguage`, `TestStackPlan_S3BucketNameCheckedAtPlan`, `TestCUR122_*` (6), `TestResolveZoneRef`, `TestDeploy_ZoneByName*`, `TestResolveNetwork_ZoneByName`, `TestK8s_NotesInCLIWords`, `billing_views_test.go` (201 строка) |

## Чек-лист

### Согласие
- D19: имя бакета, которое S3 не примет, отказывает до превью и до гейта. Одна точка — `s3.CheckBucketName` (`internal/s3/client.go:180`), вызывается в `s3_ls/get/put/rm/bucket_delete` (`tools_s3.go`) и в `normalizeManifest` для `s3[]` стека (`tools_stack.go:732`); для создания бакета `bucketName` (`argrules.go:263`) добавляет то же правило к шаблону схемы (CUR122-2). Новых путей в обход гейта нет: проверка стоит **до** чтения `confirmed`.
- Зона по имени в `deploy`: неизвестное имя — `USAGE` на превью, до согласия (`deploy_preview.go:72–80`, `ResolveZoneRef` в `network.go:44`); имя при пустом списке — отказ, а не уход в API (CUR122-4). Ок.

### Секреты
- D18: баланс договора на сервере с `--allow-project` не отдаётся. Одна точка `balanceRefused()` (`tools_billing.go:94`) на флаге `projectBound` — действует и по stdio (H9 `StdioToo`). `billing_finops` не вызывает `cli.Balance` при отказе (`tools_billing.go:334–337`). Тест проверяет и `billing_balance`, и `billing_finops`, и что баланс не читается. `docs/mcp.md` H9 обновлён, `TestHTTPPolicy_ClosedList` зелёный.
- `details` в конверте ошибки CLI (#178): `answerDetails` (`internal/cli/tools.go:896`) переносит поля ответа инструмента, кроме `status/message/reason/hint`. Пароль ВМ попадает в `--json` stderr и строкой «Пароль ВМ: …» в текстовом режиме — по решению FB121-1, паритет с MCP (по HTTP его маскирует H7, CLI локальный). Код 3 идёт отдельным путём (`invokeTool`, `tools.go:671`), в `details` не попадает.
- api-probe: новый секрет `CLOUDRU_AGREEMENT_ID` маскируется `::add-mask::` до использования (`api-probe.yml:468`). Ок.
- **FB122-1 (CI-логи).** Шаг `os-smoke` печатает stderr `evo --json --yes vm create` в публичный лог через `redact.sed` (`cloud-e2e.yml:581`). С 1.2.2 конверт ошибки на этом stderr несёт `details.password` (таймаут или обрыв после создания ВМ), а в `redact.sed` правила для пароля нет (`redact_test.sh` его тоже не проверяет). В 1.2.1 ошибка пароль не несла, а успешный `vm.json` фильтруется `jq` по полям. `cloud-live.sh` безопасен: `tc()` печатает только `.error.message`. Риск низкий: `os_smoke` по умолчанию выключен, ВМ удаляется шагом «всегда», путь — только отказ после создания. Предложение: два правила в `redact.sed` — `"password": "…"` → `"password":"<password>"` и `Пароль ВМ: …` → `Пароль ВМ: <password>` — и образец в `redact_test.sh`; это один PR без изменения бинаря. Тег не блокирует.

### Паритет CLI ↔ MCP
- Все изменения — в обработчиках реестра; команды CLI те же (`CLISpec`). Текстовые виды `billing` — через закрытый набор `treeRenders` (`trees.go:72–74`), `--json` и ответ MCP не изменились (`TestParity_JSONEqualsTool` зелёный).
- Примечания k8s по-русски: `evo mcp` принудительно ставит английский (`internal/cli/mcp.go:202`), ответы MCP прежние; CLI с `EVO_LANG=en` печатает английский текст агента. Ок.
- `details` в ошибке CLI = поля ответа MCP: паритет закрыт.

### Идемпотентность
- Затронутые пути (billing, s3-имена, зона, k8s-примечания) повторов не меняют. Зона: state получает UUID (`deploy.go:546`), повторный `deploy` по имени зоны сходится к той же ВМ. `ZoneRefError` удалён вместе с тестом — мёртвого кода нет.

### «Миграция» ↔ DECISIONS ↔ RELEASING
- CHANGELOG 1.2.2, раздел «Миграция»: два пункта, каждый — было → стало → что поменять, со ссылкой на D18 и D19.
- `DECISIONS.md`: D18 и D19 записаны с решением владельца 22.10.
- `RELEASING.md`, таблица ломающих вне major: две строки 1.2.2.
- README «Что сейчас можно (v1.2.2)» называет оба изменения.
- Условия 1–5 выполнены: D18 — раскрытие денег организации сверх проекта (условие 1, первая часть); D19 — согласие на вызов, который не мог пройти (как D15); переход — одна правка (код 3 → 1, другой источник баланса).
- Хвост с 1.2.1: **D17** (цена по API калькулятора) по-прежнему только упомянут (`release-plan.md:85`, внутри строки D18), своей строки в `DECISIONS.md` нет. Закрывается тестом ссылок из брифа (PR 5).

### Зависимости и CI
- minio-go 7.3.0 → 7.4.1 (#166); govulncheck в CI зелёный. `spf13/pflag` стал прямой зависимостью в `go.mod` — его импортируют только тесты `internal/cli`; это корректно для `go mod tidy`, на бинарь не влияет.
- Workflow: только `api-probe.yml` (проба BIL-6 с маской) и шаблон issue (версия). Ок.

### Документация
- `docs/commands.md`, `docs/tools.md` перегенерированы (`TestCommandsDoc`, `TestToolsDoc` в CI); `docs/exit-codes.md` описывает `details`; `docs/auth.md` и `docs/guide.md` — зона по имени для `deploy`; `docs/mcp.md` — H9 и раздел `--allow-project`. `TestDocsExamples_*` зелёные.

## Гипотезы (не проверял вживую)
- `deploy --dry-run` с именем зоны делает один дополнительный запрос списка зон в пределах `PreviewLookupTimeout` — на время превью не повлияет заметно.
- `answerDetails` переносит любые поля ошибочного ответа: если в будущем ошибка инструмента понесёт большой объект, он окажется в stderr. Сейчас таких ответов нет.

## Что делать
1. Тег `v1.2.2` на `5d2d054` после зелёного cloud-e2e (шаг 0 `RELEASING.md`); `os_smoke` в этом прогоне можно не включать.
2. Первым PR после тега: правила пароля в `redact.sed` + образец в `redact_test.sh` (FB122-1). Подходит для брифа Cursor.
3. D17 — записать при первом PR 1.3 по цене или по тесту ссылок (бриф, PR 5).
