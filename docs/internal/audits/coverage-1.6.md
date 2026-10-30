# Сверка охвата тестов с поверхностью 1.6 (черновик, 30.10.2026)

«Процесс · 1.6» по `CLAUDE.md` §0, правило 3, и шагу 0
`docs/RELEASING.md`: каждая новая поверхность 1.6 держится инвариантом на
свой риск — тестом, который поймает следующий такой же случай, а не только
этот. **Черновик по `main` 591a758** (после #651); на кандидате rc.1
обновляется по diff `v1.5.0..<кандидат>`. Продуктовые PR — с меткой
«(продукт, 1.6)», 35 штук.

Колонка «класс» — да, если тест ловит новый инструмент, команду или случай
того же рода без правки теста (закрытый перечень, обход реестра, общий
мир, одна точка решения с тестом на неё); «точка» — тест держит названный
случай.

## Поверхности, названные «релизом»

| Поверхность | PR | Инвариант | Класс |
|---|---|---|---|
| `*_operation`: `elapsed_sec` и `resource_status` (ось 2) | #641 | `TestOperation_EveryOperationDeclared` (каждый `*_operation` реестра отвечает через `operation`, чтения ресурса объявлены в `operationReads` и есть в реестре), `TestOperation_ElapsedAndResourceStatus` | да |
| Цена PostgreSQL до согласия по тарифу | #650, #644 | `TestPgCreate_PreviewGivesAnEstimateByTheTariff` (обещание скилла), `TestPGCreatePreview_PricedByTheTariff`, `TestEstimatePG_ByTheTariffSnapshot`; снимок тарифа против публичной страницы — `TestLive_TariffPageMatchesSnapshot` (job `contract`, раз в неделю и вручную перед rc) | точка: тарифная карта есть только у PostgreSQL; Kafka и Redis — «evo цены не даёт» (`tariff_url`), пробел П1 |
| Доступ PostgreSQL: откуда доступен кластер (ось 4) | #648 | `TestPG_AccessIsNamed` (превью `pg_create` и ответ `pg_get`) | **точка** — пробел П2; текст держится на документации, которая противоречит себе (DB-5 в реестре, `evo-internal`) |
| Зоны flavor по живому ответу (CMP-25) | #655, #649, #651 | `TestFlavor_ZonesFromTheLiveAnswer`, `TestFlavor_ZoneNotCreatedIsNoZone`; класс — миры против каталога живых ответов: `TestPlatformAnswers_GoWorldsKeepTheLiveForm` (`cli`), `…ToolsWorlds…`, `…EngineWorlds…`, перечни расхождений пусты | да: поле, которого платформа не шлёт, в мире — красный |
| Линия CLI: «имя (id)», зона подсетей и групп | #653, #654 | `TestRenderAnswer_ReferenceByNameAndID`, `TestNamedParams_HumanOnly`, `TestZoneNameReadWithItsID`; «в человеческом выводе нет голого UUID ссылки» — перечень `bareRefs` (36 строк, «Closes by 1.8» — этот PR); С2 `TestPreview_HumanShowsEveryValue` (`humanUnnamed` 31) | да, по перечню; шаги линии — «разработке», строка плана |
| D60: квота в превью ВМ «не проверена» | #630 | `TestPreview_SaysTheQuotaWasNotChecked`, `TestOrgQuotas_NameAndUnitOfEachQuota` | точка по смыслу (только ВМ: `engine.QuotaCheck` зовут `completePreview` и `deploy`); других create с квотой в D60 нет |
| D61: удаление ресурса, который ещё создаётся, — отказ до вопроса | #633 | `TestDeletePlan_PassingStateRefusedBeforeTheQuestion`, `TestDeletePlan_DeletingStateAnsweredWithoutTheQuestion`, `TestDelete_RefusedInStateSaysSoAndTheWayOut` (все удаления `deletePlan`, «passing» — `USAGE` без записи) | да |
| D62: `deploy` чужого compose-файла | #634 | `TestDeploy_AnotherProjectsComposeIsRefused` | точка — один инструмент, одна проверка `composeRefusal`; класса нет |
| D63: 404 без ответа сервиса — `API_ERROR` | #639 | `TestRouteNotFoundAsBadGateway` (транспорт), `TestAlreadyDeleted_Bare404IsNotGone` (каждое удаление `deleteTargets` в мире каждого вида голого 404; `bare404Gone` 1 строка — S3) | да |
| D64: DNS стека на внутренний балансировщик | #637 | `TestStackPlan_DNSOnAnInternalBalancerRefused` | точка — один манифест, одна проверка |
| D65 | — | номера нет ни в `DECISIONS.md`, ни резервом | — (на кандидате сверить снова) |

## Остальная поверхность 1.6

| Поверхность | PR | Инвариант | Класс |
|---|---|---|---|
| `subnet_create`, `subnet_delete` (новые create и delete) | #567 | `TestSubnetCreate_PreviewNamesWhatThePostSends`, `TestSubnetCreate_RefusedBeforeTheQuestion`, `TestSubnetDelete`; общие инварианты четырёх осей по реестру (гейт согласия, С1/С2, `deleteTargets`, D26) — без правки теста | да |
| D57: подсеть и группа безопасности по имени | #613 | `TestRefArgs_DeclarationsHold` (одна декларация `refArgs`: аргумент есть, описание на двух языках, Tab из того же списка), `TestRefByName`, `TestInputByName_SubnetAndSecurityGroup` | да |
| D58: зона из настроек по имени | #584 | `TestCreate_ConfiguredZoneByName` (`disk_create`, `snat_create`, `image_create`) | **точка** — пробел П3 |
| D59: `disk_delete` подключённого диска | #601 | `TestDiskDelete_AttachedRefusedBeforeTheQuestion` | точка (один инструмент) |
| D53: диск в переходном состоянии | #544 | `TestDiskChange_*` (4) | да для трёх инструментов диска, `TestDiskChange_SettleWaitIsTheDecided` держит срок |
| D54: значение не из каталога, второй SNAT | #554, #553, #551 | `TestNotInCatalogue`, `TestDbaasCreate_VersionAndSubnetNotInTheListRefusedBeforeTheQuestion`, `TestK8sCreate_…`, `TestSnatCreate_…`; W8 `TestConsent_RefusalWithoutWriteComesBeforeTheQuestion` | да по механизму `NotInCatalogue` |
| Превью: заданные необязательные, сводки, проект, KMS, `lb`/`tg` | #618, #619, #546, #537, #583 | `TestEchoedArgs_DeclarationsHold`, `TestSummarizedArgs`, `optionalUnnamed` (0), `previewNoProject` (0), С1 `bodyUnnamed` (40), `TestSecretCreate_PreviewNamesTheKMSKey`, `TestLbTgCreate_PreviewNamesWhatTheyServe` | да |
| Сон машины: отвергнутый токен | #631, #646 | `TestRefusedTokenGoesOnceMoreWithTheRenewedOne`, `TestCreate_OneRefusedTokenMakesOneResource`; провайдер токена в мирах сна — #646 | да для пути `TokenProvider`; ожидание `*_operation` — тот же провайдер |
| `destroy` и отказ платформы удалить ВМ | #565 | `TestDestroy_RefusedDeleteNamesTheStateAndTheWayOut`, `…FallsBackToTheStateTheWaitRead`, `TestDestroyTargeted_…` | точка (`destroy`); класс удалений — D61 и #475 |
| Отказ `docker compose up`, `_untrusted` | #566 | `TestEngineError_CarriesTheAnswersUntrustedFields`, `TestDeploy_Compose*`, `TestTailWriter_UnfinishedLineStaysBounded` | да для полей `_untrusted` движка |
| Три формы: 403 S3, подсказки MCP без флагов, JSON локальных команд | #638 | `accessHintMisses` (0), `toolHintFlagMisses` (0), `localJSONMisses` (0) | да |
| Подсказка тайм-аута запроса | #602 | `TestNetworkHint_ClientTimeout` | точка |
| Подсказка состояния только у отказа платформы | #539 | `TestDeletePlan_WayOutOnlyForTheRefusal`, `TestNotTheState_EveryKeyAndConnectionReason` | да |
| Карточки групп, скиллы | #556, #557 | `TestGroupCards_CoverEveryToolFromTheDeclarations`, `TestGroupCards_SayWhereAnIDComesFrom`, `TestGroupCardsDoc`, `TestSkills_SayWhatTheCodeDoes` | да |
| Tab `ca deploy --image` | #538 | `TestCompletion_ContainerImageIsNotAVMImage`; «список дополнения — того же сервиса» — строка плана | точка |
| `vm flavors`: доля vCPU в оценке | #643 | `TestListFlavors_EstimateSaysItIgnoresTheShare` | точка |

## Пробелы

Ни один не блокирует кандидат: поверхность держится точечным тестом;
класс — строкой бэклога (`process-handoff.md`) с версией-кандидатом.

| № | Пробел | Риск | Что закрывает |
|---|---|---|---|
| П1 | Цена до согласия: у PostgreSQL — тарифная карта, у Kafka и Redis — нет, и нет инварианта «create DBaaS с ценой — со снимком своего тарифа в job `contract`» | новая карта без снимка устареет молча | инвариант: каждая тарифная карта `pricing` — со снимком `testdata/tariff-*.json` и сверкой `TestLive_*`; 1.7 («деньги») |
| П2 | Доступ: «откуда доступен» назван у PostgreSQL; у Kafka и Redis превью о доступе молчит (гипотеза: они так же живут в подсети — не проверено) | агент открывает не тот путь к кластеру | после DB-5 (живой ответ, план cloud-e2e rc.1) — то же поле у `kafka_create`, `redis_create`, инвариант по группе DBaaS |
| П3 | D58: зона по имени проверена у трёх названных create; обхода реестра нет — новый create, который берёт зону из настроек, тест не увидит | имя зоны уйдёт в поле id у нового create (класс D15) | обход реестра: каждый create, который берёт зону из настроек, в мире `CLOUDRU_REGION=<имя>` шлёт UUID |
| П4 | `TestMisses_HaveADeadline` (#645) не видел перечень, у которого «only shrinks» перенесено на новую строку: `bareRefs` (36 строк) шёл без срока | перечень пропусков живёт вечно | **закрыт этим PR**: `isMissList` по нормализованному тексту, проверка в `TestMisses_DeadlineParts`; `bareRefs` — «Closes by 1.8» |

## Перечни пропусков на 30.10 (`TestMisses_HaveADeadline`)

27 перечней, 281 строка, просроченных 0 (выпущено 1.5). Наибольшие:
`idWithoutFormat` 82, `namesUnmarked` 49, `bodyUnnamed` 40, `bareRefs` 36,
`humanUnnamed` 31, `inlineGates` 28, `refUncompleted` 25. Пусты:
`lateRefusals`, `optionalUnnamed`, `previewNoProject`, `accessHintMisses`,
`toolHintFlagMisses`, `localJSONMisses`, `takenAdopts`, `takenNotReached`,
`liveWorldDrift` (оба), `worldDrift`.

## На кандидате

- diff `v1.5.0..<кандидат>`: новые продуктовые PR после 591a758 — строкой
  в таблицу;
- D65 и следующие номера — по `DECISIONS.md`;
- счёт перечней — заново, прогоном `TestMisses_HaveADeadline -v`.
