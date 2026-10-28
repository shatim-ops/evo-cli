# Полный аудит 1.5 (Fable, 28.10.2026)

Чек-лист `CLAUDE.md` §5: полный аудит перед тегом minor, по diff
`v1.4.1..c60092a` (`main`, кандидат тега `v1.5.0-rc.1`: последний
продуктовый PR версии — #517) плюс проход по чек-листу всего продукта,
включая новый пункт «полнота параметров платформы» (§6, ось 4). 49 коммитов,
160 файлов, +7 415/−426; код продукта — 40 файлов, +1 278/−264; тесты —
≈70 файлов; остальное — документы, скиллы, CI. Поверх кандидата в `main`
только PR «(процесс)» #518 (сверка охвата, `coverage-1.5.md`). Только
чтение; в облако не ходил (cloud-e2e — отдельно, по «да» владельца).

Продуктовые PR диапазона: #465, #466, #469, #472 (D49), #474 (D50), #475,
#476, #477, #478, #482, #483, #500, #501, #504 (D52), #506, #510, #511,
#512, #514, #517. PR «(процесс)»: #463, #467, #468, #473, #479, #480, #481,
#484, #486, #487, #489, #490, #491, #492, #493, #494, #495, #497, #498,
#499, #502, #503, #505, #507, #508, #509, #513, #516, #518; живой прогон —
#460 (проба Redis `STANDALONE`).

**Вердикт: блокеров тега нет — тег `v1.5.0-rc.1` на коммите слияния PR
версии (поверх `8316c58`) ставить можно.** Блокер — дефект кода, который
запрещает тег; таких нет. Три ломающих изменения версии (D49, D50, D52)
оформлены по правилу `docs/RELEASING.md`: у каждого пункт «Миграции» во
фрагменте `changelog.d`, решение владельца с номером D, условия 1–5
выполнены; строк 1.5 в таблице `RELEASING.md` ещё нет — это шаг PR версии,
не дефект (FB150-6). Необъявленных ломающих изменений в diff не нашёл:
все новые поля ответов — добавления, `reason` и коды выхода, кроме
объявленных, не меняются. Блок С1–С9 и С4а закрыт по существу: в
`bodyUnnamed` нет строк «доступ» и «цена», у четырёх строк «работа» есть
причина; доля vCPU берётся только из поля `oversubscription`;
`sgrules.Coverage` — единственная точка «уже открыто»; описания и скиллы
говорят то, что делает код, и каждое обещание держит тест. Дефектов
согласия, секретов, паритета и идемпотентности нет. Шесть находок низкой
серьёзности — после тега; два невыполненных шага порядка выпуска — до тега.

Находок: блокирует тег — 0; до тега (шаги порядка, не дефекты) — 2
(FB150-6, FB150-7); после тега — 6 (FB150-1…5, FB150-8); гипотез — 3.

## Проверки

| Проверка (коммит `c60092a`, `main`) | Результат |
|---|---|
| CI на `c60092a` | `ci` run 40763632066 — success с первой попытки (push в `main`: test ×3 ОС, docker, classify, скрипты); на `8316c58` (#518) — run 40764848242, success |
| Локально на `c60092a` | gofmt чисто; `go vet ./...` чисто (linux и `GOOS=windows`); `go test -count=1 ./...` — 45 пакетов ok; `go test -race` — `internal/tools`, `internal/cli`, `internal/engine`, `internal/mcp`, `internal/sgrules`, `internal/deployplan` ok; `python3 scripts/changelog.py check` — ok (16 фрагментов, три с «Миграцией») |
| Утечки в diff (добавленные строки) | UUID — только нулевые и `1111…` фейки тестов; IP — `0.0.0.0/0`, `10.0.0.0/8`, `127.0.0.x`, документационные `203.0.113.x`, `192.0.2.x`; хосты — `api.cloud.ru`, `github.com`, `example.com`, домен владельца в примерах (как и раньше); ключей, почты, внутренних имён нет; `hunter2-very-secret` — строка теста маскирования. Сведения о платформе — только ссылками на пункты реестра (DB-3, DB-17, NET-7 и т. д.), без сценариев |
| Тест, падающий без правки (проверено по тестам в дереве) | D49 — `TestCreate_NeverTakesWhatWasThereBefore` (`takenAdopts` пуст), `TestNameFree`, `TestImageCreate_NameTakenByOwnImageOnly`, `TestSecretCreate_TakenPathIsAnErrorNotCreating`, мир W8 «secret path taken»; D50 — `TestProvision_UnknownFlavorRefusedBeforePreview`, `TestDeploy_UnknownFlavorRefusedBeforePreview`, мир W8 «flavor not in the catalogue» (`refusedBy: provision, deploy`); D52 — `TestCoverage`, `TestOpenPorts_AlreadyOpenCountsTheSource`; #469 — `TestProvision_AllRefusalsInOneAnswer`, `TestVMCreate_AllRefusalsShown`; #475 — `TestDelete_RefusedInStateSaysSoAndTheWayOut` (у каждого delete — отказные состояния или причина `none`); #476 — `TestRedisCreate_StandaloneSendsWhatTheAPICreates`; #477 — `TestWaits_SayWhatTheyWaitFor`, `TestWaitForSSHPort_SaysThePortState`; #478/#483 — `TestCompletion_EveryReferenceIsCompletedOrSaysWhy` (пробелов 0), `TestComplete_WritesNoJournal`, `TestComplete_SilentWithoutAnswerOrKey`; #500 — `TestRedact_EnvPairNamesAndURLs`; #501 — `TestDescriptions_SayWhatIsBuilt`; #510 — `TestFlavorText_ShareGPUSpotFromTheCatalogue`, `TestWorldCatalogue_AutomaticPicksStay`; #506/#511/#512/#517 — `TestPreview_NamesWhatTheWriteSends` (С1), `TestPreview_HumanShowsEveryValue` (С2); #514 — `TestSkills_SayWhatTheCodeDoes` (`skillProofGaps = 0`), `TestSkills_RouteGroupsHoldTheirTools`, `TestCleanup_InventoryReadsEveryCreatedList` |
| Ревью Qwen | есть у всех 20 продуктовых PR; 72 находки без «Гипотез», на каждую — ответ автора (исправлено с коммитом или опровергнуто); критические и high (#475, #514) опровергнуты по коду; все 4 находки о секретах (#500) исправлены в `b52bcf8` или опровергнуты по D24; workflows с секретом не затронуты. Два ответа ни «исправлено», ни «опровергнуто»: #466 №3 (строка теста «добавлю при следующей правке») и #483 №4 (regexp `refArg` — «передам Процессу», в реестре таких аргументов нет). Оба — тесты, не продукт; не блокируют |
| Инварианты «Процесса» в дельте | `TestConsent_RefusalWithoutWriteComesBeforeTheQuestion` (`lateRefusals` пуст), `TestConsent_EveryHandled4xxIsARuleOfTheWorld`, `TestCreate_NeverTakesWhatWasThereBefore`, `TestDelete_RefusedInStateSaysSoAndTheWayOut`, `TestWaits_SayWhatTheyWaitFor` (`silentWaits` — только блокировки, повторы, циклы сервера, короткий `findBootFIP`), `TestPreview_NamesWhatTheWriteSends`, `TestPreview_HumanShowsEveryValue`, `TestCompletion_*`, `TestOutbound_OnlyCloudRuOrDeclaredLinks` (S6), `TestToolsList_SizeBudget`, `TestContractMap_IsTheDeclarations`, `TestWorkflows_NoRedByDesign`, job `handoff` (шаг 6 RELEASING) |
| Инварианты всего продукта на кандидате (полный проход) | зелёные: `TestConfirmedReadOnlyInTheGate` (гейт — одна функция, D35; `inlineGates` не вырос), `TestAlreadyDeleted_EveryDeleteDeclared` (D26), `TestErrAnswer_CLIErrorDetailsNotCopied`, `TestReadOnly_*`, `TestParity_CommandsAndTools`, `TestParity_JSONEqualsTool`, `TestParityInventory`, `TestCommandsDoc`, `TestToolsDoc`, `TestWallClockReadsDeclared` (новый `readCompletionCache` объявлен: TTL кэша — решение по стенке), `TestDocs_DecisionsCitedAreWritten`, `TestJournal_*` (D46), `TestRedact_*`, `TestAuthorship` |

## Чек-лист

### 1. Согласие (гейт D35, ось 1 §6)

- **D50 (#474).** Одна точка отказа — `engine.flavorNotFound`
  (`internal/engine/create_preview.go:462`): её дают превью
  (`completePreview` → `PickVMFlavor`) и сами вызовы (`provisionVM` →
  `PickVMFlavor`; `deploy` → `resolveFlavor` → `flavorAPIName`,
  `internal/engine/deploy.go:449`, первым шагом — до образа и сети, как
  превью). `NOT_FOUND`, код 4, подсказка `evo vm flavors`; UUID не из
  каталога проходит без проверки, как раньше (сказано в D50). Единственный
  другой вызов `flavorAPIName` — `deploy.go:569`, тот же путь `deploy`.
- **Все отказы одним ответом (#469).** `clierrors.Refusals`
  (`internal/errors/refusals.go`): `Unwrap() []error`, `errors.As` находит
  первый — его `reason`, код и подсказка остаются, как когда он был один;
  `errAnswer` кладёт каждый в `refusals` (`tools.go:3317`). Порядок —
  flavor, образ, сеть (`completePreview`, `create_preview.go:349`);
  `provisionInputError` — образ, затем зона, `missing` называет оба. Код
  выхода первого не меняется — не ломающее, как и задумано.
- **D49 (#472).** `nameFree` → `takenName` → `nameTakenResult`
  (`internal/tools/namecheck.go:59`) у `disk_create`, `image_create`
  (только `type=private`), `dns_zone_create` (та же видимость, имя по
  умолчанию — домен), `secret_create` (путь) — до `confirmed`, в
  `inlineGates` этих обработчиков; проверка одна, как у D20–D28. Превью
  `image_create`, `dns_zone_create`, `secret_create` теперь требует ключ и
  проект (код 2) — объявлено в D49 и «Миграции».
- **Зона в терминале (#466).** `askMissingZone` (`internal/cli/zone_ask.go`)
  спрашивает только при `missing: region`, TTY на stdin и без `--yes`;
  `evo tools call` не спрашивает (`TestToolsCall_NoZoneNotAskedEvenAtTerminal`),
  MCP — прежний отказ. Выбранная зона пишется в профиль только по «да» и
  тем же `SaveProfile`, что `evo configure` (оба файла, под блокировкой);
  молча зона не выбирается (D15, D48).
- **Отказ в удалении (#475).** `stateWayOut` и `withWayOut`
  (`internal/tools/delete_preview.go:64, :83`) — одно место для
  `deletePlan` и `release_floating_ip`: превью предупреждает, отказ после
  согласия несёт `resource_status` и подсказку с выходом, `reason` —
  платформы (вариант (б) владельца, не ломающее). Подсказка заменяет любую
  ошибку выполнения, не только отказ платформы — FB150-2.
- **Гейт.** `TestConfirmedReadOnlyInTheGate` зелёный; `inlineGates` — те же
  обработчики, что в 1.4.1, с меткой «1.5»; перевод на `gated` (K1) в
  состав «а» не вошёл — метки устарели (FB150-3).

### 2. Секреты (вывод, ошибки, журнал, превью, ответы HTTP MCP)

- **H7, `env` (#500).** `pairSecret` (`internal/mcp/http_redact.go:248`):
  значение пары маскируется по корню ключа, по последнему слову имени
  (`token(s)`, `pass(es)`, `pwd(s)`, `credential(s)`, `key(s)`, кроме
  `…PUBLIC_KEY`) и по ссылке с паролем в значении при любом имени;
  ссылка проверяется до исключений `HTTPNonSecretFields`, так что
  `…_URL` её не открывает. `docs/mcp.md` H7 обновлён. Превью `ca_deploy`
  отдаёт `env` именами без значений (`tools_ca.go:144`) — значений там нет
  ни по stdio, ни по HTTP. Пробел П3 «Процесса» (мир JSON-паритета без
  `env` с секретным именем) — бэклог 1.6, не дефект.
- **Tab (#478).** `Complete` зовёт `unchecked[list]` только для инструментов
  без записи, без `confirmed` и не `sensitiveTools` (`listable`,
  `completion.go:109`): `secret_value` и `cert_private_key` не дополняют;
  в кэш идут только `id`, `name`, `path` (`completionItems`), каталог
  `0700`, файл `0600`, имя — 12 байт SHA-256 от списка, проекта и key id
  (не обратимо). Пути секретов в кэше — по строке плана, значений нет.
  Журнал не пишется (`TestComplete_WritesNoJournal`).
- **Зона в профиль (#466).** `offerZoneToProfile` сохраняет профиль с теми
  же ключами, что прочитал, в те же файлы; в вывод ключи не попадают.
- **Отказы.** `flavorNotFound` перечисляет имена и id flavor каталога —
  не секрет; `nameFree` при нечитаемом списке заворачивает ошибку списка —
  как у других ошибок облака, через `errReason`, без ключей
  (`TestErrAnswer_CLIErrorDetailsNotCopied`).
- **Ход ожиданий (#477).** Строки хода называют id ВМ, адрес floating IP и
  состояние (`deploy.go:1089, :1223, :1828`; `apply.go:367`; `userkeys.go:164`) —
  без паролей и ключей; по MCP идут через `notifications/progress`
  (`logAndProgress`, `caller.go:146`), строки продуктовые, маскирование
  прогресса H7 — прежняя строка плана FB13-4.

### 3. Паритет CLI ↔ MCP (D13, D14) и ось 4 («человек видит каждое значение»)

- `list_flavors` ↔ `evo vm flavors`: одни данные, колонки `name, vcpus,
  cpu_share, ram_gb, gpu, spot, est_monthly_rub, free_tier`; `zones` —
  только в `--json` (гипотеза 3). Фильтры `cpu, ram, share, zone, gpu,
  spot` — аргументами инструмента, флаги из `CLISpec`.
- Превью: `ports_source` — параметр превью `provision`/`deploy`/
  `deploy_plan`, человеческая строка портов берёт источник из него
  (`ShowCreatePreview`, `create_preview.go:555`); `ParamLabel` — одна
  функция подписей для агента и человека; объект-параметр (`health_check`)
  человек видит по полям (`paramValue`, `:927`). С2 держит
  `TestPreview_HumanShowsEveryValue`: в `humanUnnamed` остались только id
  того, что названо по имени, умолчания «окружение/профиль/автоподбор»,
  почасовая цена (человеку — месячная) и 15 строк «работа» с причиной
  (`project_from` ×8, `deploy update`, `image_create has_user_data_template`
  и `zone`, `k8s_add_addon version_from`, `pg_create zone`). Строка
  `k8s_add_addon warnings` ушла (#517): предупреждения плана — в
  `CreatePreview.Warnings`, откуда их показывает каждое превью; в ответе MCP
  они теперь и в `warnings`, и в `preview.warnings` — дубль, не смена.
  Две строки «работа» стоит поднять раньше линии CLI 1.6: `pg_create zone`
  (человек видит подсеть, а не зону кластера) и `deploy update` — П5
  сверки охвата, бэклог 1.6.
- Ошибка CLI без префикса инструмента (#465): `toolHints.prefix`
  (`internal/cli/tools.go:818`) — одна точка; `evo tools call` оставляет префикс. Текст
  ошибки — не контракт, `reason` и код не меняются.
- Расхождений `--json` ≠ ответ инструмента не нашёл; `TestParity_JSONEqualsTool`
  зелёный, `jsonAliasCompat` не рос.

### 4. Идемпотентность и уборка (ось 3 §6, D26)

- **D49** закрывает «ресурс был до нас»: имя свободно до вызова, поэтому
  `lostCreate` по имени находит только ресурс этого вызова; `takenAdopts`
  пуст. Мир не доводит до записи `dns_record_create`, `k8s_add_nodepool`,
  `lb_create` (`takenNotReached`, П7 «Процесса») — у них проверка имени
  есть с D28, пробел в мире, не в продукте.
- **D52**: `Open` и `open_ports` помечают «открыто шире» как не своё
  (`PortsAlreadyOpen`, `sgrules.go:202`), `destroy` такие правила не
  закрывает — чужое не удаляется. Повтор `open_ports` с теми же портами и
  источником — `already_open` без правила; `openedHere` не даёт открыть
  порт дважды за один вызов.
- **#475**: удаление в `DEPLOYING`/`CREATING`/`FAILED`/`ERROR`/`error_deleting`
  не выглядит успехом — `status: error`, `resource_status`, путь
  (подождать и повторить; консоль или поддержка с id); каждое delete
  объявлено в `deleteRefusals` (состояния или `none` с причиной).
- **Redis `STANDALONE` (#476)**: `redisAPIReplicas` (`tools_redis.go:219`) —
  одно место; превью называет один узел в шарде, тело шлёт `replicas: 1`;
  С1 держит соответствие.

### 5. «Миграция» ↔ `DECISIONS.md` ↔ таблица `RELEASING.md`

| Решение | Фрагмент `changelog.d` | Строка `RELEASING.md` | Условия 1–5 | Смена `reason`/кода объявлена |
|---|---|---|---|---|
| D49 (#472) | `taken-before.md`, «Миграция» + «Исправлено» | нет — шаг PR версии | 1 — второй платный ресурс и чужой ресурс как свой (неверный результат с кодом 0); 2 — повтор после обрыва у любого скрипта; 3 — только четыре create и превью трёх; 4 — другое имя или `existing_id`, код 3 → 2 у превью без ключа; 5 — «Миграция», D49 с решением владельца (#464) | да: код 0/5 → 1; без `--yes` 3 → 1; превью без ключа 3 → 2 |
| D50 (#474) | `unknown-flavor-before.md` | нет — шаг PR версии | 1 — превью с ценой у заведомо невыполнимого (дыра в согласии, D35); 2 — догфуд 28.10; 3 — одна функция отказа; 4 — ждать код 4; 5 — D50 (#470) | да: без `--yes` 3 → 4; `deploy` после согласия 2 → 4 |
| D52 (#504) | `open-ports-source.md` | нет — шаг PR версии | 1 — `status: ok` при недоступном сервисе (неверный результат с кодом 0); 2 — любая группа с узким правилом; 3 — одна функция `Coverage`, «нет поля — как раньше»; 4 — проверить правила группы; 5 — D52 (#496) | `reason` и коды не меняются; меняется, какие правила создаются — объявлено |

Необъявленных ломающих изменений нет. Проверено по diff: смена
`flavorAPIName` `CONFIG_INVALID` → `NOT_FOUND` покрыта D50 («`deploy`
после согласия код 2 → 4»); превью `image_create`/`dns_zone_create`/
`secret_create` без ключа код 3 → 2 — D49; `missing` остаётся списком;
`refusals`, `already_open_wider`, `resource_status`, `cpu_share`/`gpu`/
`spot`/`zones`, `ports_source`, `will_create` у `dns_record_create`,
`ui_enabled`/`cruise_control_*`, `backup_schedule`/`backup_retention_days`,
`health_check`, `protocol`, `count`/`disk_gb`, `kube_api_internet` —
добавления полей; текст `note` у `list_flavors` — свободный текст.
D51 — только план 1.6, кода нет, не ломающее.

### 6. Полнота параметров платформы (ось 4 §6; блок С1–С9, С4а)

| Пункт | Проверено | Итог |
|---|---|---|
| С1 | `bodyUnnamed` (`internal/cli/preview_body_test.go:37`): классов «доступ» и «цена» нет; «работа» — 4 строки с причиной (`deploy ip_protocol` — только TCP, `k8s_add_addon resolveMode`, `k8s_add_nodepool updateConfiguration.strategy`, `lb_create rules.listeners.target_port`); «шум» — id, формы API, имена, которые evo даёт сам; «секрет» — 2 (`payload`); `projectid` — одной строкой «работа» (П6, «разработке») | закрыт по существу |
| С2 | `humanUnnamed` — раздел 3; `k8s_add_addon warnings` ушла | закрыт; П5 — 1.6 |
| С4 | превью называет `kube_api_internet`, `ui_enabled`, `public`/`external` подписями, `count`/`disk_gb` узлов, расписание и срок копий PG, Cruise Control, `health_check`, `protocol`, TTL записи; `ports_source` | закрыт в объёме строки плана; тариф AR и `kms_key_id` секрета — FB150-5 |
| С4а | `cp.Warnings = append(cp.Warnings, p.warnings...)` (`tools_k8s_addons.go:478`) | закрыт |
| С5 | `provision.Flavor.Share` ← `oversubscription`, `GPU`, `Type.IsSpot`, `Zones` (`provision/types.go:266`); `flavorText` — только из `FlavorSpec` (`SpecOf`, одна функция); `list_flavors` — ключ только если каталог прислал; имя flavor в продуктовом коде — лишь в примерах справки и тарифной карте стека (`stack/store.go:829`, прежнее, цена, не доля); `TestFlavorText_ShareGPUSpotFromTheCatalogue`; автоподбор не меняется (`TestWorldCatalogue_AutomaticPicksStay`) | закрыт |
| С6 | раздел 2 | закрыт |
| С7 / D52 | `sgrules.Coverage` — единственная точка (`Open` и `planOpenPorts`, превью и выполнение); `existing`-карты удалены; ложного «уже открыто» по узкому источнику нет (`src.Bits() > want.Bits()` → не считается); правило без источника — как до D52, с причиной в D52 и коде | закрыт; живой формат — проба 1.6 |
| С8 | `TestDescriptions_SayWhatIsBuilt`: `cert_issue_le`, `cert_list`, `lb_create` — L4 TCP, TLS на ВМ; `vpc_create` — подсеть отдельно; `redis_create` — три платных узла; `ca_deploy min_instances` — оплачивается; по каждому языку отдельно | закрыт |
| С9 | `evo-deploy`/`evo-vm`: HTTPS на ВМ, Caddy в compose, 80/443 из `0.0.0.0/0`, A-запись через `evo dns record-create <zone-id> <name> --record-type a --value … --dry-run` — флаги и аргументы существуют (`docs/commands.md`); `evo-scenarios`: маршруты Kafka/Redis/k8s к существующим инструментам, «топики и пользователи — в консоли» держит `TestKafka_NoTopicOrUserTools`; группы `--toolsets` строк держит `TestSkills_RouteGroupsHoldTheirTools`; обещания — `TestSkills_SayWhatTheCodeDoes`, `skillProofGaps = 0`; таблиц размеров нет (решение владельца 28.10) | закрыт |

Что из FB15-1…49 осталось на 1.6 и записано ли в плане
(`release-plan.md`, «Первым блоком 1.6»): явно — FB15-2, 5, 6, 17, 18, 21,
38, 40, 42 (пакет отказов D50), FB15-1, 3, 5, 15 (единый автоподбор, `cpu_share`
аргументом), FB15-49, 13, 17, 18, 19, 39 (поля 11 списков), FB15-44
(`secret_env`), FB15-8 (квоты), FB15-14 (таблица аудита, 1.6); остальные —
одной строкой «по таблицам аудита при начале работы». Из «остальных» три
стоит назвать строкой до передачи 1.5, иначе они теряются: FB15-47
(умолчание `nlb.external` стека и отказ `stack_plan` на `dns.target=lb` без
внешнего адреса — корзина (Б) аудита, ломающее, нужны D и «Миграция»),
FB15-46 (тариф AR: evo не шлёт `tariff`, платформа берёт BASIC, превью об
этом молчит — «параметр, который evo не трогает», без объявления причины) и
FB15-9 (исходящий интернет приватной ВМ) — FB150-5.

### 7. Сверка ответов на ревью Qwen — по коду

- #500 №1–4 (маскирование): `b52bcf8` — проверено, что ссылка с паролем
  маскируется до исключений и `TOKEN_URL`/`ACCESS_KEY_ID` остаются видны
  (`TestRedact_EnvPairNamesAndURLs`); №2 (безопасная сторона для
  `…PUBLIC_API_KEY`) — по D24, согласен.
- #504 №1 (пустой `RemoteIPPrefix` = «откуда угодно»): опровергнуто как
  дефект с явной причиной в D52 и коде; проба — строка 1.6. Согласен:
  лишнее правило на каждый порт каждого `deploy` дороже пробела.
- #476 №1 (превью `replicas: 0`, в API 1): превью называет один узел в шарде,
  `hosts_per_shard`; С1 держит тело — согласен.
- #474 №3 (код `deploy` 2 → 4): ломающее, объявлено — раздел 5.
- #478 №3 (key id в имени кэша): 12 байт хэша, не обратимо — согласен.
- #483 №4 (regexp `refArg`): в реестре нет аргументов вида `…_id_…`, тест
  «Процесса» — не продукт; не блокирует.
- #475 critical («синтаксическая ошибка»): пакет собирается, CI зелёный —
  ложная находка.

## Находки

### FB150-1 (после тега; код `internal/tools/completion.go:51`) — Tab у `evo ca deploy --image` предлагает образы ВМ

`valueRefs["image"]` дополняет аргумент `image` у каждого инструмента, где
он есть; у `ca_deploy` это URI образа контейнера из Artifact Registry, а
список — `image_list` (образы ВМ). Проверено: `completionOf("ca_deploy",
"image")` → `image_list`. Инвариант `TestCompletion_EveryReferenceIsCompletedOrSaysWhy`
считает `image` ссылкой (`refArg`) и доволен тем, что дополнение есть, —
«того же сервиса» он не проверяет. Записи в облако нет, ошибка видна
человеку сразу (имена вроде `Ubuntu-24.04` вместо `<реестр>.cr.cloud.ru/…`),
поэтому серьёзность низкая. Предложение: строка `noCompletion["ca_deploy"]["image"]`
с причиной («образ контейнера, не образ ВМ; список — `ar_*`, когда будет
список образов реестра») — «разработка»; инвариант «список дополнения — того
же сервиса, что инструмент аргумента» (как уже сделано для `version_id`) —
«Процесс».

### FB150-2 (после тега; код `internal/tools/delete_preview.go:83–98`) — подсказка состояния заменяет подсказку любой ошибки после согласия

`withWayOut` подменяет `hint` у **любой** ошибки выполнения удаления, если
ресурс был прочитан в отказном состоянии: 401 «expired», 403 «нет роли»,
429, обрыв сети на DELETE ресурса в `FAILED` получают «удалите в консоли или
обратитесь в поддержку с id», в `CREATING` — «дождитесь готовности и
повторите». `reason` остаётся верным (код по статусу), поэтому скрипт не
обманут; человек и агент получают не ту подсказку. Для `FAILED`/`ERROR` так
было с 1.4.x; #475 распространил это на `DEPLOYING`/`CREATING`/`ERROR_DELETING`
и `release_floating_ip`. Предложение: применять подсказку состояния только
к ответу платформы с кодом отказа (4xx/5xx тела, не `AUTH_REQUIRED`,
`API_RATE_LIMITED`, обрыв), иначе оставлять подсказку по `reason`; одна
точка та же — «разработка». Инвариант: вариант мира «отказ удаления» с 401
или 429 на DELETE — подсказка `reason`, не состояния — «Процесс».

### FB150-3 (после тега; тесты `internal/testrules/gate_test.go:27`, план) — метки «1.5» в `inlineGates` устарели

30 обработчиков по-прежнему спрашивают согласие вне `gated` и помечены
«1.5»; перевод на планировщики (K1, первая строка 1.5 плана) в состав «а»
не вошёл. D49 добавил проверку имени в четыре из них до `confirmed` — одной
функцией `nameFree`, §2 соблюдён. Дефекта нет; при передаче 1.5 метки и
строку плана перенести на 1.6, чтобы `inlineGates` не выглядел невыполненным
обещанием версии. «Процесс» (метки), «релиз» (план).

### FB150-4 (после тега; документы `docs/internal/state/release.md`) — срез «релиза» ждёт решения, которое принято

«Ждёт решения: #455 — отказ в удалении: новый `reason` (D) или подсказки» —
решено 28.10 (вариант (б), #475, фрагмент `delete-refused-way-out.md`).
Строку снять; там же «установка автодополнения через `install.sh`» из
`handoff/1.4.1.md` — сделано (#482). «Релиз», при PR версии.

### FB150-5 (после тега; план `release-plan.md`, 1.6) — три находки слепых зон без строки плана; `kms_key_id` не в превью

- `secret_create` с заданным `kms_key_id` шлёт `kmsKeyId`, а превью —
  `willPreview(ResourceSecret, path, "", nil, nil)` без параметров
  (`tools_secret.go:127`): ключ KMS, то есть то, чем шифруется секрет,
  человек и агент до вопроса не видят. С1 этого не ловит: мир паритета
  зовёт `secret_create` только с `path` и `value`
  (`parity_fixtures_test.go:161`) — необязательные аргументы в теле
  записи инвариант не видит. Предложение: `will_create: {path, kms_key_id}`
  у превью — «разработка»; второй набор аргументов мира С1 «со всеми
  необязательными» для create с необязательными полями тела — «Процесс».
- FB15-47, FB15-46, FB15-9 — строками раздела 1.6 (раздел 6). «Релиз».

### FB150-6 (до тега; шаги PR версии, условие 5 RELEASING, D38) — не дефекты

Не выполнено на `8316c58`, выполняется PR версии: свёртка `changelog.d`
(16 фрагментов, `python3 scripts/changelog.py fold`; «[Не выпущено]» пуст);
три строки 1.5 в таблице `RELEASING.md` (D49, D50, D52 — текст раздела 5
годится как основа); README «Что сейчас можно (v1.5.0, …)» (сейчас v1.4.1);
`metadata.evo-version` пяти скиллов и `skillVersionWant` в
`internal/cli/skills_test.go` → «1.5» (сейчас «1.4»; тест упадёт, пока не
поменяны оба). Без них тег rc ставить нельзя по порядку, не по коду.

### FB150-7 (до тега; шаг порядка) — cloud-e2e кандидата и Windows

cloud-e2e на кандидате (ru.AZ-2, `cleanup_only` без остатков) — по «да»
владельца, ещё не прогнан. Windows (#400, #493): известное красное
рантайма Go, один перезапуск job на PR; CI на `c60092a` и `8316c58`
зелёный с первой попытки, проба `split` «Процесса» идёт — не блокер.

### FB150-8 (после тега; документ D49) — D49 не говорит, что нечитаемый список отказывает create

`nameFree` при ошибке списка отказывает вызов («проверка, что имя
свободно: …», `namecheck.go:62`): 5xx или 429 на `disk_list` теперь
отказывает `disk_create`, которого раньше это не касалось. Решение верное
(иначе `lostCreate` не отличит своё от чужого), сказано в комментарии кода,
но не в D49 и не в «Миграции». Дописать одной фразой в D49 при PR версии —
«релиз».

## Гипотезы (не проверял вживую)

1. `Coverage` считает правило точным по строке `port_range == "80:80"`
   (`sgrules.go:136`). Если живой API отдаёт один порт как `"80"`, точное
   правило читается как «шире» — `already_open_wider` у точного правила.
   Дубль не создаётся, только слово. Проверяется той же пробой 1.6, что
   `remote_ip_prefix`.
2. `dns_zone_create`: D49 проверяет имя зоны (по умолчанию домен) среди зон
   той же видимости. Зона с другим `name`, но тем же `domain` проверку
   проходит; что ответит платформа — не проверено (пункт закрытому
   реестру).
3. `evo vm flavors` не показывает `zones` в таблице (только `--json`): для
   списка это не нарушение оси 4 (она о превью), но при выборе flavor в
   терминале человек не видит, где его можно создать. Колонка «зоны» или
   строка линии CLI 1.6.

## Что делать

- **До тега rc.1 («релиз»):** PR версии — FB150-6; cloud-e2e кандидата по
  «да» владельца — FB150-7; в том же PR версии снять устаревшие строки среза
  (FB150-4), дописать D49 (FB150-8), добавить строки 1.6 для FB15-47, 46, 9
  (FB150-5).
- **После тега («разработка», 1.6):** FB150-1 (`noCompletion`), FB150-2
  (подсказка состояния только у отказа платформы), `kms_key_id` в превью
  `secret_create` (FB150-5).
- **После тега («Процесс», 1.6):** инварианты к FB150-1 (список того же
  сервиса), FB150-2 (мир «401/429 на DELETE в отказном состоянии»),
  FB150-5 (С1 с необязательными аргументами); метки `inlineGates` → 1.6
  (FB150-3).
