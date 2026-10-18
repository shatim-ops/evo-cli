Cursor: break-report r3b, main=951c4dc96686ce97b5cb7b955fd90291134531ff

Контракт инструментов и паритет CLI ↔ MCP. Облако не вызывалось: транспорт подменён. Пагинация (#98), R2-1…R2-5, находки r3-A и открытые пункты `docs/internal/standalone/BACKLOG.md` не повторялись. Поведение из `docs/internal/cloudru-discrepancies.md` дефектом не считалось. Код продукта не менялся.

`TestContract_*` зелёные, исключения — таблица в коде. Каждое исключение либо падающий `TestBreak_*`, либо строка «так задумано» со ссылкой. `TestBreak_*` на этом `main` падают.

## Расхождение брифа и документа

Бриф считает разные значения одного ключа `preview.details` находкой high. `docs/internal/standalone/BACKLOG.md` [03] фиксирует обратное для цены: превью MCP `provision`/`deploy` без `confirmed` в облако не ходит, размер flavor неизвестен, оценки нет; цена есть у `deploy_plan` и у `evo vm create` (CLI), который превью достраивает. То же в комментарии `invokeTool`: дерево показывает дополненное превью, `tools call` — ответ инструмента как есть. В прогоне ключи `preview` у `vm create` и `k8s_add_addon` на дереве отличаются от `tools call` и обоих MCP (у ВМ — цена и имена flavor/образа/зоны, у аддона — `project_id`). `tools call`, stdio и HTTP между собой совпали. Это не дефект.

## Находки

| ID | Серьёзность | Область | Вход → неверный результат | Ожидалось (со ссылкой на документ/код) | Тест | Предложение |
|---|---|---|---|---|---|---|
| R3B-1 | high | delete/create, разбор HTTP 200 | Изменяющий инструмент с `confirmed=true` и телом ответа HTML (`<`) или пустым телом на HTTP 200 отвечает успехом. `image_delete`: `{"status":"deleted"}`, `isError=false`, после `DELETE compute.api.cloud.ru/api/v1/images/…`. `pg_db_create`: `{"database":null,"status":"created"}`. `ca_start`: `{"status":"ok"}`. Тот же класс (таблица в `contractAPIBreak`, отдельные вызовы в тесте — представители): `disk_delete`, `k8s_delete`, `ca_delete`, `ca_stop`, `snat_delete`, `pg_db_delete`, `pg_user_create`, `pg_user_delete`, `secret_create`, `secret_add_version`, `secret_delete`, `cert_delete`, `s3_put`, `s3_bucket_create` (HTML и пустое тело); `s3_rm`, `s3_bucket_delete`, `s3_buckets`, `s3_ls` (HTML). Повторит агент или скрипт: шлюз отдал страницу вместо JSON, команда напечатала «удалено» или «создано». Цена: объект, который не удалился, считается удалённым; созданная база в ответе `null`. Пустой 200 на DELETE слабее (многие API так и отвечают); пустой 200 на create с `database: null` и любой HTML — нет. | `docs/exit-codes.md`: ответ API — `API_ERROR`, код 5. `errreason.go` уже считает HTML-страницу шлюза ответом API, если клиент её разбирает. `provision.DeleteImage` закрывает тело и возвращает nil на любом 2xx (`internal/provision/image.go`). `handlePgDBCreate` кладёт разобранное значение в `database` и ставит `created` (`internal/tools/tools_pg.go`). `handleCAAction` ставит `ok` после 2xx (`internal/tools/tools_ca.go`). | `TestBreak_API_HTML200ReportedSuccess` | В клиентах, которые на 2xx не читают тело (`DeleteImage` и такие же delete/start/stop), непустое тело, которое не JSON, возвращать `API_ERROR`, не nil. В `handlePgDBCreate` / `handlePgUserCreate` / `secret_*` / `s3_put` / `s3_bucket_create`: пустой или не-JSON ответ create — `API_ERROR`, не `created` с `null`. Пустой DELETE оставить успехом только там, где это записано в клиенте явно. |
| R3B-2 | high | reason после не-JSON тела | Облако ответило 200, тело не JSON. `disk_resize` (PUT): `resize disk: invalid character '<'` и на пустом теле `resize disk: EOF`, reason `USAGE`. `snat_create` (POST): `create SNAT gateway: invalid character '<'`, `USAGE`. `tg_list`: `parse target_groups: invalid character '<'`, `USAGE` — `parseAnswerRE` (`\bparse [a-z ]+: `) не видит подчёркивание. Тот же класс: `s3_get` HTML и пустое, `s3_ls` и `s3_buckets` пустое. Повторит скрипт: шлюз отдал HTML на resize или создание SNAT, процесс вышел 1, повтор шлёт второй PUT/POST. | `docs/exit-codes.md`: `USAGE` — локальный отказ, в облако ничего не ушло; ответ API с телом — `API_ERROR`, код 5. Комментарий `errreason.go`: `parse <what>: invalid character '<'` — это `API_ERROR`. | `TestBreak_API_UnparsedBodyIsUsage` | В `uncodedReason`: синтаксическая ошибка JSON (`invalid character`, `unexpected end of JSON`, `EOF`) после вызова — `API_ERROR`, даже если в тексте нет `parse `. В `parseAnswerRE` разрешить `_` (`parse target_groups:`). Не ставить `USAGE` разбору тела HTTP. |
| R3B-3 | medium | `--json`, `k8s kubectl` | `evo k8s kubectl <id> get pods --json`. stderr — две строки текста: `Error: evo k8s kubectl: --json after the arguments would go to the tool` и подсказка про порядок флагов. Не объект `{"error":…}`. `--json` в argv есть: `jsonRequested` его видит, но `run` подставляет его в `flagJSON` только пока `commandStarted` ложь, а `PersistentPreRun` уже его поднял. Повторит человек или скрипт, поставивший глобальный флаг в конце, как у обычных команд. Отказ сам по себе верный. | `docs/exit-codes.md`: с `--json` stderr — один объект JSON, чтобы строка разбиралась `jq`. `docs/install.md`: флаги evo у `k8s kubectl` стоят до id кластера, флаг после — `USAGE`. Оба верны сразу: отказ `USAGE`, и этот отказ печатается JSON. | `TestBreak_JSON_KubectlFlagAfterArgs` | В `run` перед `handleError` ставить `flagJSON = flagJSON || jsonRequested(args)` и когда команда уже началась. Подсказка порядка флагов остаётся в `hint`. |
| R3B-4 | medium | stdio, UTF-8 в аргументе | В `tools/call` `image_list` байт `0xFF` внутри JSON-строки `query`. `encoding/json` считает документ годным и подставляет U+FFFD. Уходят `GET /api/v1/images?name=web%EF%BF%BD` и такой же `display_name`. Ответ успеха: `isError=false`, пустой список. Повторит клиент, отдав обрезанную или не-UTF-8 строку: поиск идёт по другой строке и выглядит успешным. | JSON-RPC и MCP требуют UTF-8. `docs/exit-codes.md`: неверные аргументы — `USAGE` до облака. Подмена байта другим символом и запрос с ним — не отказ. | `TestBreak_UTF8_ReplacedQueryHitsCloud` | В `decodeMessages` отклонять строку, где `utf8.Valid` ложь, тем же parse error, что и не-JSON (`-32700`), до обработчика. Либо в `checkedArgs` не пускать строку с U+FFFD, которая появилась из невалидного UTF-8, — но на этом слое исходный байт уже потерян, поэтому отказ нужен в разборе строки. |

## Проверено, не сломалось

1. **Пустые аргументы.** Все 152 инструмента реестра. У 96 с `required` и `{}`, и `{"confirmed":true}` дают `USAGE`, запросов транспорта 0, паники нет, `AUTH_*` нет. Инструменты без `required` в это утверждение не входят: отказывать им не за что.

2. **Гейт.** 65 изменяющих. `confirmed` из `"true"`, `1`, `"yes"`, `[]` — `USAGE`, 0 запросов (как `TestRegistry_ConfirmedAsStringRefused`, `docs/mcp.md` «Ошибки»). Без `confirmed` и с `null` записей (не GET/HEAD) нет. Не `confirmation_required` при этом бывает: `USAGE` на имени или enum, `NOT_FOUND`/`VM_NOT_FOUND` уже на превью (`docs/exit-codes.md`), `need_specification` у pg/kafka — меню из GET. Так и задумано, в духе `TestQA_MutatingToolsWriteNothingWithoutConfirmed`. CLI: `--arg confirmed=false`, `confirmed=true`, `CONFIRMED=true`, `confirmed` без `=` — код 1, `confirmed` не аргумент (`docs/mcp.md` «Подтверждения»). `--yes --dry-run` на `disk delete` — `dry_run`, код 0 (`confirm.Ask` смотрит dry-run раньше `--yes`).

3. **Коды по статусам.** 1425 пар инструмент×статус, где транспорт реально вызван, сошлись с `docs/exit-codes.md`: 404 — `NOT_FOUND` или `VM_NOT_FOUND`; 429 — `API_RATE_LIMITED`; 401 только compute — `AUTH_EXPIRED`, 403 только compute — `AUTH_FAILED`; 403 не-compute — `API_ERROR` (раздел «Особенности»); 400/409/422/500/502 — `API_ERROR`; обрыв — `API_UNREACHABLE`. `reason: ERROR` при наличии статуса не встретился. `Retry-After: 0` и backoff адаптера 1 мс, без сна. Исключения «так задумано»: `org_projects` с текстом «Not an error» — BACKLOG [02]; `org_quotas` 403 про квоты — ответ без `isError`; `metrics_scrape` HTML и пустое тело — это текст Prometheus, не JSON API. `k8s_kubectl`, `k8s_apply`, `k8s_helm` в матрицу не входят: они запускают процесс, не этот транспорт. `s3_*` на 429/500/502 не покрыто: minio повторяет по своим часам, контекст теста кончается раньше, reason получается `API_UNREACHABLE` из-за бюджета, не из-за продукта.

4. **`--json`.** 71 команда деревьев vm, image, disk, vpc, k8s, pg, s3, secret на пути ошибки (ключа нет, транспорт сразу 401 JSON): stdout пустой или один JSON-документ, stderr только JSON-строки. Успех: `vm list`, `vm zones`, `disk list`, `disk types`, `vpc subnets`, `image list` — код 0 и один JSON-документ. `--json` стоит первым аргументом. Исключение — R3B-3.

5. **Паритет превью.** `disk delete`, `destroy`, `pg_create`: ключи и значения `preview.details` совпали у дерева `--dry-run --json`, `tools call --dry-run --json`, stdio MCP без `confirmed` и HTTP MCP без `confirmed`. `open_ports` дерева нет, три оставшиеся поверхности совпали. `vm create` и `k8s_add_addon`: три агентские поверхности совпали; дерево достраивает превью — строка «расхождение брифа» выше, не находка. Поле `approval` есть только у MCP (`docs/mcp.md` «Подтверждения») и в сравнение не входило.

6. **Схемы.** `required` ⊂ `properties`, у свойств типы string/integer/number/boolean/array/object, `confirmed` есть у всех изменяющих и ещё только у `cert_private_key` (`mutatingOverride` в `internal/tools/registry.go`: согласие из-за секрета, не из-за записи). Аннотации `readOnlyHint` / `destructiveHint` уже проверяет `TestToolsList_Annotations`; новый тест их не дублирует.

7. **stdio.** `tools/call` до `initialize` и второй `initialize` принимаются: `docs/mcp.md` этого не запрещает, версию из списка сервер повторяет. Число и пустая `protocolVersion` — версия по умолчанию (`docs/mcp.md`: отвечаем той, что знаем, иначе новейшей). Два объекта в одной строке — parse error, в облако ничего не уходит. `\r\n` разбирается. `resources/list` — `-32601`. Граница `readLine` — `maxMessageSize` (`16<<20`): ровно столько байт плюс перевод строки принимается, на байт больше — отказ, следующая строка жива. Закрытый stdin долгий `tools/call` не отменяет: `Run` возвращается после ответа, `.evo` не появляется (`docs/mcp.md` «Отмена»). Комментарий в `internal/mcp/cancel.go` про отмену по stdin старее этого текста; поведение совпало с `docs/mcp.md` и с `RunContext`, отдельной находкой не ставился.

8. **`--arg`.** `ports=80,443`, `ports=80`, `ports=[80,443]` — массив целых. `ports=80a` и `size_gb=10GB` — `USAGE`, не число 10 и не 80. `name=` — пустое обязательное, `USAGE`. `values=a=b` остаётся `a=b`. Повтор `--arg` — побеждает последний. Без `=` — `USAGE`. `--args-json` массивом или `null` — `USAGE`. `--args-json` вместе с `--arg` — побеждает флаг. Молчаливого приведения к другому значению нет.

## Покрытие

| Sweep | Прогнано | ok | так задумано | TestBreak_* | не покрыто |
|---|---|---|---|---|---|
| 1. `{}` → USAGE до облака | 152 инструмента, утверждение на 96 с `required` (192 вызова) | 96 | — | — | 56 без `required`: отказывать не за что |
| 2. Гейт | 65 изменяющих; CLI `confirmed` и `--yes --dry-run` | 65 + 4 формы CLI | превью не всегда `confirmation_required` (USAGE, NOT_FOUND, need_specification); `cert_private_key` не Mutating | — | — |
| 3. Ответы API | 12 статусов × инструменты, дошедшие до транспорта; 1425 пар суждены | 1425 | org_projects BACKLOG [02]; org_quotas 403; metrics_scrape HTML/пустое | R3B-1, R3B-2 (класс из 48 пар в `contractAPIBreak`, в тесте — представители) | `s3_*` на 429/500/502; `k8s_kubectl`/`k8s_apply`/`k8s_helm`; пары с 0 запросов |
| 4. `--json` | 71 команда (ошибка) + 6 списков (успех) | 71 + 6 | — | R3B-3 | успех остальных команд деревьев (не списки из этой шестёрки) |
| 5. Паритет превью | 6 команд × до 4 поверхностей | disk delete, destroy, pg_create, open_ports (без дерева) | vm create и k8s_add_addon: дерево достраивает превью, BACKLOG [03] | — | — |
| 6. Схемы | 152 | 152 | `cert_private_key`; аннотации — уже `TestToolsList_Annotations` | — | — |
| 7. stdio | перечисленные в разделе 7 | все, кроме UTF-8 | число/пустая версия, вызов до initialize, второй initialize | R3B-4 | — |
| 8. `--arg` | формы из брифа на `open_ports`, `disk_create`, `k8s_helm` | все | — | — | не каждый аргумент каждого инструмента, только эти формы |

## Итог

`TestContract_*`: 17, все зелёные (`-count=20 -race`, pass).

- tools: `TestContract_EmptyRequiredIsUsage`, `TestContract_GateNoWrite`, `TestContract_GatePreviewDoesNotWrite`, `TestContract_Schema`, `TestContract_APIStatusReason`
- cli: `TestContract_ArgForms`, `TestContract_ConfirmedIsNotAnArg`, `TestContract_YesDryRunIsDryRun`, `TestContract_TreeJSONPurity`, `TestContract_TreeJSONSuccess`, `TestContract_PreviewParity` (`contract_preview_test.go`)
- mcp: `TestContract_ProtocolVersionFallsBack`, `TestContract_InitializeIsOptional`, `TestContract_StdioFraming`, `TestContract_ResourcesListIsMethodNotFound`, `TestContract_LineLimitBoundary`, `TestContract_StdinCloseDoesNotCancel`

`TestBreak_*`: 4, на `main` падают все 20 повторов, ни одного pass.

- `TestBreak_API_HTML200ReportedSuccess`, `TestBreak_API_UnparsedBodyIsUsage`
- `TestBreak_JSON_KubectlFlagAfterArgs`
- `TestBreak_UTF8_ReplacedQueryHitsCloud`

Пакет целиком, один проход (внутри падают `TestBreak_*`, это время пакета, не зелёный прогон): tools 19.483 с, cli 2.395 с, mcp 1.399 с. Все три меньше 30 с. `-count=20 -race` только `TestContract_*`: tools 246.660 с, cli 5.288 с, mcp 13.952 с.

Ветка `cursor/break-2026-10-18-r3b`. PR не открывался. Issue из этой среды не создаётся (`gh` только на чтение). Полный SHA последнего коммита — в сообщении после пуша: этот файл его не содержит.

## Таблица для issue

Заголовок: `[break] break-report 2026-10-18 r3b`. Метка: `отзыв`.

| ID | серьёзность | область | суть |
|---|---|---|---|
| R3B-1 | high | delete/create, HTTP 200 | HTML или пустое тело на 200 сообщается как deleted/created/ok (`image_delete`, `pg_db_create`, `ca_start` и тот же класс) |
| R3B-2 | high | reason после не-JSON | `disk_resize`, `snat_create`, `tg_list`: облако ответило, reason USAGE (код 1), не API_ERROR; у `tg_list` regex `parse` не видит `_` |
| R3B-3 | medium | `--json`, kubectl | `--json` после аргументов `k8s kubectl` печатает текст `Error:`, не JSON, хотя отказ USAGE верный |
| R3B-4 | medium | stdio UTF-8 | Байт 0xFF в `query` заменяется на U+FFFD и уходит в `GET /images`; ответ — успех пустого списка |
