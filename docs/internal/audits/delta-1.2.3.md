# Дельта-аудит 1.2.3 (Fable, 22.10.2026)

Чек-лист В9: аудит по diff `v1.2.2..origin/claude/release-1.2.3` (релизный
PR #215, открыт, в `main` не слит). Задание называло head `adbeae05`; к
моменту аудита ветка получила ещё один коммит `136305f` (README: ссылка на
SECURITY.md в описании версии, по ревью Qwen) — он просмотрен, вывод тот
же. 25 коммитов, 89 файлов, +3 290/−99; код продукта — 12 файлов,
+128/−16. Только чтение; в облако не ходил (страницы тарифов — публичный
сайт).

**Вердикт: тег `v1.2.3` на коммите слияния #215 можно ставить.** Дефектов
согласия, секретов, паритета, идемпотентности и совместимости в дельте нет.
Две находки — документация и форма превью, тег не блокируют (FB123-1,
FB123-2).

## Проверки

| Проверка | Результат |
|---|---|
| CI на `136305f` (head #215) | зелёный: прогон 39821860789 — test ×3 ОС, docker, classify |
| Локально на `adbeae05` | gofmt чисто; `go vet ./...` чисто (linux и `GOOS=windows`); `go test -count=1 ./...` — 42 пакета, 0 падений (`internal/tools` 30 с, `internal/cli` 15 с — ожидание гейта `deploy` больше не ждёт продовых интервалов, #193) |
| Утечки в diff | UUID — только нулевые фейков; IP — RFC 5737 (`203.0.113.7/9`), `192.168.0.1` как пример; ключей, адресов, внутренних хостов, почты — нет |
| Страницы тарифов | шесть адресов — HTTP 200, заголовок страницы называет сервис (см. «Ссылки на тарифы») |
| Тест, падающий без правки | #209 — `TestPreview_TariffAndWarningsInCLI`, `TestTariffs_EveryResource`, `TestPrice_UnpricedLinksTariff`, `TestContract_EveryPreviewPricedOrListed`; #213 — `TestApply_FIPCreateWithoutIDFoundByName`, `TestApply_LostFIPUnlistedRetryNotConsole`; CI — `internal/workflows` (12 инвариантов), `TestNoUnguardedWallClockBounds`, `TestDocs_DecisionsCitedAreWritten` |
| Ревью Qwen | #209 — 4 находки, на каждую ответ (1 опровергнута, 3 оставлены с причиной); #213 — 5 находок, на каждую ответ (4 опровергнуты, 1 оставлена). Возражений к ответам нет (см. «Идемпотентность») |

## Чек-лист

### 1. Согласие
- Превью шести инструментов строится внутри ветки `!confirmed` и до любого
  вызова облака на запись: `tools_ar.go:75–83`, `tools_ca.go:184–201`,
  `tools_kafka.go:347–356`, `tools_redis.go:186–194`, `tools_nlb.go:368–378`,
  `tools_snat.go:89–101`. Единственная новая функция — `willPreview`
  (`tools.go:3350`): собирает `CreatePreview` из уже вычисленного
  `will_create`, не ходит в сеть, не меняет гейт. Отказы до согласия
  (`ValidName`, зона, подсеть, спецификация) стоят там же, где были.
- Новых путей в обход гейта нет: `confirmed` читается после превью, вызов
  с `confirmed=true` идёт прежним кодом.
- Паритет превью по поверхностям: команда дерева показывает `preview`
  словами deploy (`internal/cli/tools.go:731 createPreview` →
  `engine.ShowCreatePreview`), `evo tools call` — поля ответа
  (`toolPreview`), MCP stdio/HTTP — объект как есть. Это допущение D13,
  закреплённое `TestContract_PreviewParity` (details равны на четырёх
  поверхностях) и `TestPreview_TariffAndWarningsInCLI` (CLI называет тариф
  и не теряет предупреждения).
- `sameConsent` (`parity_newcmds_test.go:168–186`) убирает из записи
  согласия на stderr только `preview.summary`, и только когда у ответа
  есть структурное `preview`; `action` и `details` (весь ответ инструмента)
  сравниваются дальше, ключи нормализованы `json.Marshal`. `Lines` в запись
  не входят (`confirm.Preview.Lines json:"-"`). Ослабление ровно на то, что
  различается по D13; не шире.

### 2. Секреты
- В `preview.params` попадают только перечисленные ключи `will_create`:
  тип реестра, образ, порт, cpu/memory, число инстансов, `runs_as`, версии,
  брокеры, шарды, `subnet_id`, `nat_type`, зона. `env` у `ca_deploy` — имена
  переменных без значений (`tools_ca.go:164–170`, комментарий «a value may
  be a secret»). У `kafka_create`/`redis_create` паролей в аргументах нет.
- `preview.warnings` — фиксированные тексты (root, один брокер, хосты в
  одной зоне) плюс имя зоны. Ошибки и логи дельта не меняет.
- H7 по HTTP: `redactSecrets` (`internal/mcp/http_redact.go:252`) обходит
  весь JSON ответа, `preview` включая; `echoes` маскирует повтор значения.
  Структурное замечание (не дефект): пары `PreviewParam{key,value}`
  маскируются по имени поля `value`, а не по `key`, поэтому секрет,
  положенный в `params` под ключом `password`, H7 по имени не поймал бы —
  только по эху с одноимённым полем ответа. Сейчас ни один инструмент
  такого не делает; правило «в `params` — только несекретные ключи» стоит
  записать в комментарии `willPreview` при следующей правке.
- #213: заметки `ensureFloatingIP` несут только текст ошибки API и имя
  адреса. FB122-1 закрыт #191: `redact.sed` маскирует `"…password":"…"` и
  строку «Пароль ВМ: …», `redact_test.sh` — 9 образцов.

### 3. Паритет CLI ↔ MCP (D13/D14)
- Все изменения — в обработчиках реестра; `CLISpec` и `localCommands` не
  трогались, `docs/tools.md`/`docs/commands.md` без diff (описания не
  менялись). `--json` команды = JSON инструмента: `createPreview` кладёт в
  `details["preview"]` тот же объект (`tools.go:751`),
  `TestParity_JSONEqualsTool` зелёный; `sameVolatile` получил маску минуты
  имени плана (`-<minute>`), чтобы два вызова не расходились на границе
  минуты (#190; `internal/clock` — одна точка времени для значений в
  ответе).
- `evo tools call` печатает `preview` как поля — по дизайну D13
  (комментарий `TestContract_PreviewParity`).

### 4. Идемпотентность и деньги
- #213 (`apply.go:330–389`): порядок — адрес на ВМ (`AttachedFloatingIP`)
  → адрес из старого плана → создание → при `e == nil && ID == ""` или
  ошибке — поиск по имени `FitName(<ВМ>, "-fip")` → заметка «повторите
  `deploy_apply`». Повтор `deploy_apply`: существующая ВМ усыновляется
  (`apply.go:86–89 finishApply`) и `ensureFloatingIP` первым шагом берёт
  адрес, уже привязанный к ВМ, — второй адрес не создаётся. Тесты:
  `TestApply_FIPCreateWithoutIDFoundByName` (один create, id из списка),
  `TestQAr3_Apply_LostFIPCreateFoundByName` (чужой свободный адрес не
  берётся), `TestApply_LostFIPUnlistedRetryNotConsole`.
- Ответы релизной сессии на medium-находки Qwen по #213 верны: фильтр
  `in_use` вернул бы сам дефект; повтор сначала читает адрес на ВМ.
- #209 платных путей не меняет: превью без побочных действий.

### 5. Совместимость
- Ломающих изменений нет: `message`, `will_create`, `warning`
  (kafka/redis), `warnings` (lb) сохранены, `preview` добавлен рядом;
  `ar_create` и раньше `will_create` не имел. Заметка `deploy_apply` при
  несостоявшемся адресе — текст поля `note`, не код и не `reason`.
- CHANGELOG 1.2.3 без «Миграции», таблица ломающих в `RELEASING.md` без
  новой строки, README «Ломающих правок нет» — согласованно. DECISIONS:
  записан D17 (хвост аудита 1.2.2 закрыт), новых решений дельта не требует.
  `TestDocs_DecisionsCitedAreWritten` держит ссылки.

### 6. Ссылки на тарифы
GET каждой страницы 22.10 (публичный сайт, не облако):

| Ресурс | Страница | Код | Заголовок страницы |
|---|---|---|---|
| `ar_registry` | `…/evolution/artifact-registry` | 200 | Тарифы «Evolution Artifact Registry», прил. 7.EVO.6 |
| `ca_app` | `…/evolution/container-apps` | 200 | Тарифы «Evolution Container Apps», прил. 7.EVO.10 |
| `kafka_cluster` | `…/evolution/managed-kafka` | 200 | Тарифы «Evolution Managed Kafka», прил. 7.EVO.30 |
| `load_balancer` | `…/evolution/load-balancer` | 200 | Тарифы «Evolution Load Balancer», прил. 7.EVO.19 |
| `redis_cluster` | `…/evolution/managed-redis` | 200 | Тарифы «Evolution Managed Redis», прил. 7.EVO.16 |
| `snat_gateway` | `…/evolution/evolution-compute` | 200 | Тарифы «Evolution Compute», прил. 7.EVO.1 — строка «Аренда публичного sNAT шлюза» |

Таблица `tariffs` (`create_preview.go:140–156`) закреплена
`TestTariffs_EveryResource`; страницы — `tariffPages` в
`preview_tariff_test.go`. Постоянная живая проверка (HEAD в cloud-e2e
readonly) — в плане 1.3, как и было.

### 7. CI и workflow (#193–#207)
- Код PR не получает секретов: `qwen-review.yml` — `pull_request_target`
  и `issue_comment` на ветке по умолчанию, checkout без PR-кода, PR
  читается `git diff` как данные, команда только от OWNER; `classify` —
  job без секретов и без прав записи (summary); `cloud-e2e-weekly` —
  только с `main`, `actions: write` для `gh workflow run`, job'ы cloud-e2e
  ждут одобрения окружения как прежде; `weekly-race`/`weekly-fuzz` —
  `contents: read`, `issues: write` только в вызываемом `weekly-issue.yml`
  с литеральными `title`/`what`.
- Текст события — через `env`: скан всех `run:` на `${{ github.event.* }}`
  — ни одного (единственное `github.event_name` в `api-contract.yml` —
  имя события, не текст). `BASE` sha, `PR`, `COMMENT_ID` — через `env`.
  Инварианты `TestWorkflows_PRCodeNeverRunsWithSecrets`,
  `TestWorkflows_EventTextNeverInRunScripts` зелёные.
- `ci.yml`: один прогон на изменение, отмена по новому push только для PR,
  `-shuffle=on` на `main`, actionlint со shellcheck, deadcode.

### 8. Утечки внутренних сведений
По маскам (см. «Проверки») — чисто. `SECURITY.md` без имён, адресов и
внутренних процессов; просит не вставлять ключи и ID.

## Находки

### FB123-1 (low, документация) — `docs/mcp.md` «Цена» не знает шести новых превью и `preview.warnings`
- **Путь:** `docs/mcp.md:136` перечисляет ресурсы с `tariff_url` как
  «кластеры Managed Kubernetes и PostgreSQL, группы узлов, бакеты,
  расширение диска»; с #209 их двенадцать (`create_preview.go:140–156`).
  Поле `preview.warnings` (`create_preview.go:85`) в `docs/mcp.md` не
  описано — агент по HTTP не знает, что предупреждения инструмента
  дублируются туда.
- **Исправление:** в абзаце «Цена» — «любой платный ресурс, цену которого
  evo не рассчитывает (список — `tariffs` в `internal/engine`)», и одна
  фраза о `preview.warnings`. Только документ; в релизный PR или первым
  PR после тега.

### FB123-2 (low, форма превью) — у `snat_create` зона в превью только как UUID
- **Путь:** `tools_snat.go:98` передаёт в `willPreview` зону `""`, а
  `availability_zone_id` (UUID из `resolveSnatZone`) — параметром. Kafka,
  Redis и LB передают имя зоны, и сводка превью говорит «в зоне ru.AZ-1».
  Человек в CLI соглашается на платный шлюз, видя идентификатор вместо
  имени зоны. Поведение `will_create` такое же с 1.0 — не регрессия.
- **Исправление:** `resolveSnatZone` возвращает и имя зоны, когда она
  задана по имени или найдена в списке; `willPreview(…, zoneName, …)`.
  1.3, вместе с «имена из облака в `message`/`next`/`hint`» (пункт 6.2).

## Гипотезы (не проверял вживую)
- Поиск адреса по имени (`apply.go:376–381`) не сверяет привязку к этой ВМ:
  если от прежней ВМ с тем же именем остался адрес `<имя>-fip`, удалённый
  вне evo, повтор `deploy_apply` запишет его как адрес ВМ. Для денег это
  скорее плюс (`destroy` освободит сироту), но `public_ip` в ответе будет
  не адресом ВМ. Поведение существовало до 1.2.3 (путь потерянного
  ответа); при наличии в ответе списка поля привязки (`FloatingIP.Interface`
  / `VM`, `types.go:455–456`) можно предпочесть адрес с привязкой к
  `vmID`. Не блокирует.
- Структурное замечание о H7 и `PreviewParam` (раздел «Секреты») — правило
  для будущих инструментов, не дефект дельты.

## Что делать
1. Тег `v1.2.3` на коммите слияния #215 после зелёного cloud-e2e (шаг 0
   `RELEASING.md`) и «да» владельца.
2. FB123-1 — абзац в `docs/mcp.md`: в релизный PR или первым PR после
   тега.
3. FB123-2 и гипотеза о привязке адреса — в «Осталось и найдено» 1.3.
