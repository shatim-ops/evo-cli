# Сверка охвата тестов с поверхностью 1.5 (28.10.2026)

«Процесс · 1.4.x и 1.5» по `CLAUDE.md` §0, правило 3, и шагу 0
`docs/RELEASING.md`: каждая новая поверхность 1.5 держится инвариантом на
свой риск — тестом, который поймает следующий такой же случай, а не только
этот. Diff `v1.4.1..ddcbfee` (кандидат «релиза»), продуктовые PR и PR
«разработки» без метки.

Колонка «класс» — да, если тест ловит новый инструмент или новую команду
того же рода без правки теста (закрытый перечень, обход реестра, общий мир);
«точка» — тест держит названный случай.

## Поверхности

| Поверхность | PR | Инвариант | Класс |
|---|---|---|---|
| Tab: дополнение ссылок во всей CLI | #478, #483 | `TestCompletion_EveryReferenceIsCompletedOrSaysWhy` (пробелов `refGap` — 0), `TestCompletion_DeclarationsHold`, `TestTab_CompletesReferencesEverywhere`, `TestComplete_NarrowsByPrefixAndCaches`, `TestComplete_SilentWithoutAnswerOrKey`, `TestComplete_ToolRefFromItsList`, `TestComplete_WritesNoJournal` | да |
| install.sh: автодополнение и `--uninstall` | #482 | `TestInstallSh_CompletionFilesAndUninstall` (поддельный выпуск) | точка: установка в домашний каталог (Linux и macOS; на Windows — пропуск, скрипт не для неё); путь root (`/usr/local/bin`) не проверен |
| Зона не задана — вопрос в терминале | #466 | `TestVMCreate_NoZoneAskedAtTerminal`, `TestDeploy_NoZoneAskedAtTerminal`, `TestToolsCall_NoZoneNotAskedEvenAtTerminal`; вопрос — по `missing: region` ответа (`askMissingZone`, одна точка) | да по механизму: любой инструмент с `missing: region` |
| Ошибка команды без имени инструмента MCP | #465 | одна точка (`toolHints.prefix`); точечные ожидания в `zone_onboarding_test`, `tool_errcontract_test` | **точка** — пробел П1 |
| Все отказы без записи — одним ответом | #469 | `TestProvision_AllRefusalsInOneAnswer`, `TestVMCreate_AllRefusalsShown` | **точка** (`provision`, `vm create`) — пробел П2 |
| Ход ожиданий называет состояние | #477 | `TestWaits_SayWhatTheyWaitFor` (в `silentWaits` — только объявленные: блокировки, повторы, циклы сервера, короткое `findBootFIP`), `TestWaits_SayTheStateOfTheResource`, `TestWaitForSSHPort_SaysThePortState` | да |
| D49: «ресурс был до нас» | #472 | `TestCreate_NeverTakesWhatWasThereBefore` (`takenAdopts` пуст), `TestNameFree`, `TestImageCreate_NameTakenByOwnImageOnly` | да; мир не доводит до записи 3 create (`takenNotReached`: `dns_record_create`, `k8s_add_nodepool`, `lb_create`) |
| D50: неизвестный flavor до вопроса | #474 | `TestConsent_RefusalWithoutWriteComesBeforeTheQuestion` (вариант W8), `TestProvision_UnknownFlavorRefusedBeforePreview`, `TestDeploy_UnknownFlavorRefusedBeforePreview` | да |
| Отказ удаления в состоянии — путь | #475 | `TestDelete_RefusedInStateSaysSoAndTheWayOut` | да |
| Redis STANDALONE создаётся | #476 | `TestRedisCreate_StandaloneSendsWhatTheAPICreates`, `TestRedisCreate_StandalonePreviewShowsOneHost`; тело записи — С1 | да (тело — С1) |
| H7: маскирование `env` | #500 | `TestRedact_EnvPairNamesAndURLs` (функция маскирования) | **точка** — пробел П3 |
| Честные описания | #501 | `TestDescriptions_SayWhatIsBuilt`; имена — `TestDescriptions_NamesExist` | имена — да; смысл — точка |
| D52: `open_ports` с учётом источника | #504 | `TestOpenPorts_AlreadyOpenCountsTheSource`; порт — `0.0.0.0/0` в данной группе: `TestOpen_PortOpensForAnyAddressInTheGivenGroup` | да по одной точке (`sgrules`); живой формат `remote_ip_prefix` — проба 1.6 |
| Превью умолчаний (С4: доступ, цена, работа, источник портов) | #506, #511, #512 | С1 `TestPreview_NamesWhatTheWriteSends` — в `bodyUnnamed` доступа и цены 0, работы 4, шума 34, секрета 2, `projectid` одной строкой; С2 `TestPreview_HumanShowsEveryValue` | да |
| Доля vCPU, GPU, spot, зоны flavor; фильтры | #510 | `TestFlavor_ReadsShareGPUSpotZones`, `TestFlavorText_ShareGPUSpotFromTheCatalogue` (только по полю API), `TestListFlavors_ShareGPUSpotZonesAndFilters`, С3 `TestWorldCatalogue_AutomaticPicksStay` | да |
| Скиллы: HTTPS, маршруты Kafka/Redis/k8s | #514 | `TestSkills_*`, `TestCleanup_InventoryReadsEveryCreatedList`, `TestKafka_NoTopicOrUserTools`; обещания — строки `TestSkills_SayWhatTheCodeDoes` с доказательством (этот PR: HTTPS — `TestLBCreate_TakesNoCertificate`); группы маршрутов — `TestSkills_RouteGroupsHoldTheirTools` (этот PR) | да |

## Пробелы

Ни один не блокирует кандидат: поверхность работает и держится точечным
тестом; класс — строкой бэклога 1.6 (`process-handoff.md`).

| № | Пробел | Риск | Что закрывает |
|---|---|---|---|
| П1 | #465: нет инварианта «ошибка команды дерева не начинается с имени инструмента» — паритет отказов (`sameRefusal`) снимает префикс у обеих поверхностей | префикс вернётся правкой `toolError` у одной команды незамеченным | обход команд мира паритета: отказ команды без `<tool>:` в начале, отказ `tools call` — с ним |
| П2 | #469: «все отказы сразу» проверены у `provision` и `vm create`; у остальных create с двумя одновременными отказами (зона и образ, сеть и спецификация) — нет | агент чинит по одному отказу за вызов | вариант `consentWorlds` с двумя пропусками сразу и инвариант «ответ называет оба» для каждого инструмента, который читает оба |
| П3 | #500: маскирование `env[].value` проверено на функции, но мир JSON-паритета не отдаёт `env` с секретным именем — проверка HTTP против stdio его не видит | правка ответа `ca_get` в обход маскирования | фикстура `ca_get` с `DB_PASSWORD` и сверка «по HTTP замаскировано» |
| П4 | Строка таблицы `evo-scenarios` для `evo-vm` без «a domain: `dns`», а шаг HTTPS `evo-vm` зовёт `dns_record_create` | агент, запущенный по таблице, не видит инструмента | **закрыт в #517 (7f58bd8)**: строка `evo-vm` даёт `dns`; `TestSkills_RouteGroupsHoldTheirTools` держит |
| П5 | С2: человек не видит 17 значений превью (работа 15, цена 2: предупреждение `k8s_add_addon`, зона и копии `pg_create`, `update` у `deploy`, источник проекта, почасовая цена) | согласие человека уже, чем агента | линия CLI 1.6: строки из `Details` одной функцией (D) |
| П6 | С1: превью не называет проект (`projectid`) ни у одного create | создание не в том проекте | строка общего заголовка превью — «разработке» |
| П7 | `takenNotReached`: мир не доводит до записи `dns_record_create`, `k8s_add_nodepool`, `lb_create` | «ресурс был до нас» у них не проверен | фикстуры мира: зона DNS в списке, каталог flavor mk8s |

## Что сделано этим PR

- `TestSkills_RouteGroupsHoldTheirTools` — группы `--toolsets` строки
  маршрута существуют, и каждый инструмент, который называет скилл строки
  (или маршрут `tools \`kafka_*\``), входит в них; исключения — с причиной
  (`lb_create` назван, чтобы сказать «TLS не здесь»); П4 этот тест нашёл, #517 закрыл.
- Строки `TestSkills_SayWhatTheCodeDoes` для обещаний #514 с
  доказательствами: «балансировщик L4, сертификата не берёт» —
  `TestLBCreate_TakesNoCertificate` (новый: у `lb_create` нет аргумента
  сертификата или TLS); «порты прокси — `0.0.0.0/0`» —
  `TestOpen_PortOpensForAnyAddressInTheGivenGroup`; «у Kafka нет
  инструментов топиков и пользователей» — `TestKafka_NoTopicOrUserTools`;
  «Redis STANDALONE — один узел» — `TestRedisCreate_StandalonePreviewShowsOneHost`.
