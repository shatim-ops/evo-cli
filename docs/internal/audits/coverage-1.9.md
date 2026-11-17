# Сверка охвата тестов с поверхностью 1.9 (кандидат a3652873, 14.11.2026)

«Процесс · 1.9» по `CLAUDE.md` §0, правило 3, и шагу 0
`docs/RELEASING.md`: каждая новая поверхность 1.9 держится инвариантом на
свой риск — тестом, который поймает следующий такой же случай, а не только
этот. Кандидат — `origin/main` на `a3652873` (#1970, 14.11.2026); diff
`v1.8.2..a3652873` — 493 коммита, продуктовых PR с меткой «(продукт, 1.9)» —
135, фрагментов `changelog.d/` — 120. Инструментов в реестре прибавилось 16:
15 из сверки объёма `audits/rc1-scope-1.9.md` (срез `5ceeede`) и `vm_resize`
(#1686), слитый после неё.

Метод — только чтение: `git log`, `changelog.d/`, реестр `internal/tools`
(`decl.go`, `tools.go`), имена и тела тестов. Тесты не запускались целиком;
точечно (`go test -run`) на снимке кандидата зелёные:
`TestStates_EveryChangeHasAWorldForEveryStateOfTheSpec`,
`TestPreviewProject_NamesWhereTheWriteLands`,
`TestHeld_AnotherNameOfTheSameTargetDoesNotCloseTheRecord`,
`TestDeleteLostAnswer_TheRepeatConverges`,
`TestIntent_ACallThatSentNoWriteLeavesNoRecord`,
`TestAgentSkill_EveryNameInTheCardIsServedOrCovered`,
`TestPowerStates_EveryStateOffRunningIsCoveredByLogsAndVerify`,
`TestParityWorld_YesAfterACutCallSendsWhatMCPSends`,
`TestConsent_EveryChangeDeclaresItsRepeat`,
`TestProgress_ChangeGoingOnNamesTheNextStep`,
`TestContract_EveryPreviewPricedOrListed`,
`TestSecrets_VMConsoleFieldsLeaveNoSurface`. `evo-internal` и живые прогоны
не читались: всё о платформе ниже — по тестам и мирам, не по факту вызова.

Колонка «класс» — да, если тест ловит новый инструмент, команду или случай
того же рода без правки теста (обход реестра, общий мир паритета, закрытый
перечень, одна точка решения с тестом на неё); «точка» — тест держит
названный случай.

Общий фон для всех 16 новых инструментов: каждый стоит в общем мире
паритета (`internal/cli/parity_fixtures_test.go`) — его держат
`TestParity_*`, `TestContract_*`, `TestReadOnly_*`; изменяющие — в описи
состояний `specStatesOf` и вне долга `stateWorldsPendingShipped`
(`TestStates_EveryChangeHasAWorldForEveryStateOfTheSpec`, #1552); в убывающих
списках `repeatUndeclared`, `noNextStep`, `listLagsComputePending`,
`untrustedFieldPending`, `overWhileGoingOn` новых инструментов нет (списки
пусты). В `noPriceInPreview` — `backup_delete`, `k8s_nodepool_delete`,
`sg_rule_delete`, `vm_reboot` с причиной `priceNoNewResource`.

## ВМ: питание и конфигурация (D99, D104)

| Поверхность | PR | Инвариант | Класс |
|---|---|---|---|
| `vm_start`, `vm_stop`, `vm_reboot` — правила волны П1–П10 | #1422, #1461, #1491, #1484 | `TestVMPower_EveryStateOfTheTableHasAWorld` (#1462, таблица «состояние → ответ» на перечень `compute:VmState`), `TestVMPower_EveryToolTakesANameStrictly` (каждое действие `vmPowerActions` — в `strictVMRef`), `TestVMPower_EverySendIsAChangeIsDeclaredToTheIntentLayer`; мир сбоев — `TestFaultWorld_Power*`; чек-лист операции — `TestAddOperation_NamesExistingTests` (#1428); точки — `TestVMStart_*` (37), `TestVMStop_*`, `TestVMReboot_*` | да |
| Повтор `vm_start`/`vm_stop`/`vm_reboot` после обрыва смотрит на состояние ВМ | #1652, #1631 | `TestVMPower_ACutCallIsToldByTheStateOfItsVM`, `TestHeld_ACutSendOnAHeldOperationIsAskedAbout` (FB19I-2, У13), `TestVMReboot_RepeatAfterACutCallAsksAndSendsNothing` | да по таблице `vmPowerActions` |
| `vm_resize`: превью обеих конфигураций, повтор, предел вызова | #1686 | `TestVMResize_EveryStateOfTheTableHasAWorld`, `TestVMResize_*` (19); в `mcpWaits` и `previewCalls` | да по описи состояний; остальное — точка |
| D104: `deploy`, `verify`, `logs` на остановленной или поднимающейся ВМ | #1471, #1599, #1728 | `TestPowerStates_EveryStateOffRunningIsCoveredByLogsAndVerify` (каждое состояние действий питания, кроме `running`, покрыто `engine.VMStateCovered`), `TestStoppedVM_*`, `TestDeploy_AStoppedVMIsRefused*` | да |
| `vm_get`: образ, группы и подсеть интерфейсов, разрешённые пары адресов, `--diagnose` | #1496, #1731, #1759 | `TestVMGet_NamesTheImage`, `TestVMGet_EachInterfaceNamesItsGroupsAndSubnet`, `TestVMGet_NamesTheAllowedAddressPairs`, `TestVMGet_Diagnose*` | точка (один инструмент) |
| `vm_list --images`: отбор по образу в пределе вызова MCP; исчерпание — `USAGE` | #1496, #1825, #1847 | `TestVMList_SelectsByImages`, `TestVMList_CardsWithinTheCallBudget`; общее — `TestWaits_ReachedToolsHoldTheBudget` | точка (фильтр по карточкам есть только у `vm_list`) |

## Узлы Managed Kubernetes

| Поверхность | PR | Инвариант | Класс |
|---|---|---|---|
| `k8s_nodepool_scale`, `k8s_nodepool_delete`: таблица состояний группы узлов | #1450, #1465 | `TestK8sNodePool_EveryStatusOfTheTableHasAWorld` (#1563, буквой спецификации), `TestDelete_EveryStateOfTheSpecHasAWorld` (журнал, строка 1); точки — `TestK8sNodePoolScale_*` (17), `TestK8sNodePoolDelete_*` (9) | да |
| `k8s_nodepool_delete`: удаление по D26, D74, повтор после потерянного ответа | #1465 | `TestAlreadyDeleted_EveryDeleteDeclared`, `TestDeleteOutcome_EveryAsyncDeleteReadsItsOperation`, `TestDeleteLostAnswer_TheRepeatConverges` (#1619, обход `allDeletes()`) | да |
| Превью удаления: другие группы кластера не работают; имя группы — `_untrusted` | #1626, #1624 | `TestK8sNodePoolDelete_WhatTheOtherPoolsAreIn` (строка на каждый статус таблицы), `TestK8sNodePoolDelete_CloudNamesAreMarked`; класс пометки — `TestUntrusted_EveryFieldWithTextOfTheCloudIsNamed` | да для пометки; предупреждение о других группах — точка |
| `k8s_nodepool_get`, `k8s_operation` (ось 2) | #1399, #1391 | `TestOperation_EveryOperationDeclared` (обход `*_operation`), `TestOperation_EveryOneHasAWorldOfAFailedOperation` (#1527), `TestK8sOperation_AnswersTheOperationAndItsResource`, `TestK8sNodePoolGet_AnswersThePool` | да для `k8s_operation`; `k8s_nodepool_get` — точка |
| Превью `k8s_add_nodepool` и `k8s_add_addon`: стратегия обновления узлов, происхождение версии, слова каталога в кавычках | #1725, #1957 | `TestK8sAddAddon_WarningsQuoteTheCataloguesVersions`, `TestK8sAddAddon_QuestionSaysWhatHappensToTheInstalled`; кавычки — скан `TestCloudWords_QuotedInEvoText` | точка; кавычки — да |

## Правила групп безопасности (D105)

| Поверхность | PR | Инвариант | Класс |
|---|---|---|---|
| `sg_rule_create`, `sg_rule_delete`: таблица состояний правила, вид источника | #1508, #1490 | `TestSGRules_EveryStateOfTheTableHasAWorld` (#1557, журнал, строка 4), `TestSGRules_EveryKindOfSourceHasAWorld` (#1561, строка 2), `TestSGRuleCreate_*` (11), `TestSGRuleDelete_*` (8) | да |
| `sg_rule_create`: потерянный ответ при отстающем списке не даёт второго правила | #1602 | `TestFault_Matrix_ListBehindALostCreate_Compute` (`listLagsComputePending` пуст), `TestSGRuleCreate_ALostAnswerAndAListBehindMakeOneRule` | да |
| `sg_rule_delete`: D26, отказ платформы, последнее правило SSH развёртывания | #1490, #1497 | `TestAlreadyDeleted_EveryDeleteDeclared`, `TestDeleteLostAnswer_TheRepeatConverges`, `delete_refused_test.go` (закрытый перечень с причиной), `TestSGRuleDelete_TheLastSSHRuleOfADeploymentIsRefused`, `TestClosePorts_TheLastSSHRuleOfADeploymentIsRefused` | да для удаления; правило SSH — точка (два инструмента) |
| D105: порт всему интернету на общей группе — только со словом человека | #1497, #1508 | `TestOpenPorts_TheWholeInternetOnASharedGroupIsRefusedWithoutTheWord`, `TestSGRuleCreate_TheInternetOnASharedGroupNeedsTheWord`, `TestInternet_IsAThresholdNotTwoLiterals` (`internal/sgrules`), `TestD105_*` | **точка** по двум инструментам; пробел П3 |
| Порт, который пускает только другая группа, не назван открытым (`open_ports`, `deploy`, MCP) | #1551, #1612 | `TestSGRules_EveryKindOfSourceHasAWorld` (общее решение `sgrules.Open`), `TestOpenPorts_APortOnlyAGroupLetsInIsNotCalledOpen`, `TestDeploy_AnswerNamesThePortsOnlyGroupsLetIn` | да |
| `destroy`, `vm_delete`: превью называет правила, открытые в интернет | #1512 | `TestDestroyPreview_SaysWhatStaysOpenToTheInternet`, `TestDestroyTargeted_*`, `TestOpenLeft_*` | точка (`destroy`, удаление ВМ) |
| Имена ВМ под группой (`vms`) — `_untrusted` | #1605 | `TestSGTools_TheNamesOfTheVMsUnderAGroupAreMarkedUntrusted`; класс — `TestUntrusted_EveryFieldWithTextOfTheCloudIsNamed` | да |

## Резервные копии (D106)

| Поверхность | PR | Инвариант | Класс |
|---|---|---|---|
| `backup_list`, `backup_get` | #1533 | общий мир паритета, `TestUntrusted_EveryFieldWithTextOfTheCloudIsNamed`, `TestBackupGet_*`, `TestBackupList_RowsWithTheirSource` | да для пометки и паритета; поля — точка |
| `backup_create`: занятое имя по состоянию, повтор, превью без цены | #1544 | опись `compute:BackupState`, `TestBackupCreate_TheNameTakenByState` (журнал, строка 5), `TestPaidCreate_EveryCreateIsDeclaredOrNamedWithAReason`, `TestFault_Matrix_ListBehindALostCreate_Compute`, `TestContract_EveryPreviewPricedOrListed` | да |
| `backup_delete`: состояния, отказ «не готова», проект копии в превью | #1558, #1648, #1615 | `TestBackupDelete_EveryStateOfTheSpec`, `TestDeleteLostAnswer_TheRepeatConverges`, `TestPreviewProject_NamesWhereTheWriteLands` (обход инструментов с `target`), `TestConsentWord_EveryDeleteNamesItsClassOrWhyNot` | да |
| `backup_restore`: платное создание диска, принятое и не видимое в списке восстановление | #1580, #1610, #1633 | `TestBackupRestore_EveryStateOfTheSpec`, `TestBackupRestore_TakenAndNotListedSendsNoSecond`, `TestBackupRestore_HeldIsReadWhereTheRecordsAre`, `TestFault_Matrix_ListBehindALostCreate_Compute` | да по описи; «принят, ресурса ещё нет» — точка (журнал, «Найдено до слияния») |
| Живая проверка `backup_*` в cloud-e2e | — | `TestClassify_EveryToolFileHasALiveArea`: `tools_backup.go` в храповике «нет живой области» | нет живой области — пробел П5 |

## Слой намерений D92 и повтор оборванного вызова

| Поверхность | PR | Инвариант | Класс |
|---|---|---|---|
| Вызов с согласием спрашивает заново после своего оборванного вызова | #1485, #1473 | `TestIntent_ConsentedCallAsksAgainAfterItsCutCall`, `TestEverySendIsAChange_ToolNamesItsResource` (каждая строка `everySendIsAChange` называет ресурс), `TestIntent_HeldWhileTheOperationGoesOn` | да |
| Слой читает причину каждого ответа реестра; 408 и отказ платформы | #1553, #1636 | `TestIntent_ReadsTheReasonOfEveryAnswerTheRegistryGives` (журнал, строка 6), `TestIntent_RefusalOfThePlatformClosesTheRecord`, `TestIntent_A408KeepsTheRecordOfTheCall` | да |
| Оборванный вызов без отправленной записи не оставляет записи; отметка «отправлено» | #1675 | `TestIntent_ACallThatSentNoWriteLeavesNoRecord` (обход инструментов с согласием), `TestSendPoints_EveryWriteIsCounted` (скан: каждая отправка и каждый HTTP-клиент считаются) | да |
| Один каталог записей, объявленный при запуске | #1578 | `TestStateDir_UndeclaredRefusesEveryToolThatTakesConsent` (обход), `TestStateDir_*`, `TestTools_ReadIntentRecordsInTheDeclaredDirectory`, `TestIntent_ACallsOwnRecordIsNotInItsOwnAnswer` (#1575; журнал, строка 9) | да |
| `--yes` после оборванного вызова не отправляет: отвечает только человек | #1603 | `TestParityWorld_YesAfterACutCallSendsWhatMCPSends` (каждый инструмент с согласием мира паритета: CLI с `--yes` и MCP шлют одно число записей; журнал, строка 11), `TestCutCall_OnlyAPersonAnswersTheQuestion` | да |
| Запись закрывает только тот же вызов: другой ключ, другая группа, переход имени | #1468, #1620, #1792, #1856, #1781 | `TestSettle_*` (`internal/intent`, одна функция), `TestIntent_RepeatDoesNotCloseARecordOfAnotherKey`, `TestIntent_AMovedNameKeepsTheCutCall`, `TestSGRuleCreate_ACallToAnotherGroupOfTheNameKeepsTheCutCall` | да по одной точке `settle` |
| Удержанную запись не закрывает ответ о другом создаваемом с той же целью | #1603 | `TestHeld_AnotherNameOfTheSameTargetDoesNotCloseTheRecord` (мир `backup_restore`; журнал, строка 12) | **точка**; пробел П2 |
| Намерение по id называет проект ресурса; `doctor` сверяет по id | #1447, #1615 | `TestDoctor_CutDeleteByIDIsCheckedByID`, `TestIntent_ACutCallIsOfTheProjectOfTheCall` | точка по видам из перечня случаев |
| Длинный список прерванных вызовов — пять новейших и «и ещё K» | #1681 | `TestPendingNotes_ManyRecordsFoldAndDoctorShowsEvery` | точка (одна функция вывода) |

## Повтор созданий и удалений

| Поверхность | PR | Инвариант | Класс |
|---|---|---|---|
| D107: повтор платного создания после потерянного ответа спрашивает заново | #1640, #1685 | `TestPaidCreate_EveryCreateIsDeclaredOrNamedWithAReason` (обход `allCreated()`: `everySendIsAChange` или `repeatNotAsked` с причиной), `TestPaidCreate_TheRepeatAfterALostAnswerAsks` (по `faultMatrixCases`) | да; `dns_record_create`, `snat_create`, `vpc_route_create` — объявленное исключение («Известные ограничения» `paid-create-repeat.md`) |
| Повтор удаления после потерянного ответа сходится | #1619 | `TestDeleteLostAnswer_TheRepeatConverges` (обход `allDeletes()`, `lostDeleteUnreached` с причиной) | да |
| D103: `k8s_delete`, `lb_delete`, `tg_delete` — удаляемое без вопроса и без DELETE | #1477 | `TestDelete_EveryStateOfTheSpecHasAWorld` (#1563), `TestDelete_PrefixedState*` | да для трёх; прочие удаления — долг `stateWorldsPendingShipped` до 1.10 (журнал, «План», шаг 7) |
| D109: `pg_db_create`, `pg_user_create` — занятое имя до вопроса, без второго запроса | #1427, #1414, #1421, #1662, #1690 | `TestPgCreate_TakenNameRefusedBeforeTheQuestion`, `TestPgCreate_RepeatAfterALostAnswerMakesNoSecond`; W8 `TestConsent_RefusalWithoutWriteComesBeforeTheQuestion` | точка (два инструмента одного механизма); W8 — да |
| D101: свой существующий бакет — `already_exists` | #1453, #1676 | `TestMakeBucket_ExistedOnlyWhenTheBucketIsTheCallers`, `TestS3BucketCreate_*` | точка |
| D102: шаг продолжения не создаёт вторую ВМ; внешний адрес не резервируется дважды | #1448, #1708 | `TestNextStep_VMGone*`, `TestNextArgs_ConsentIsWrittenOnlyByTheClosedList` (скан), `TestCreateFloatingIP_OnlyThroughReserve` (скан: создание адреса только через `Reserve`), `TestReserve_SendsOnce` | да |

## Превью и согласие

| Поверхность | PR | Инвариант | Класс |
|---|---|---|---|
| Обработчики на общем гейте согласия | #1401, #1403, #1424, #1430, #1434, #1440, #1459 | `inlineGatesLeft = 0` (`internal/testrules/gate_test.go`), `TestGate*_RefusalWithoutConsentIsTheRefusalWithIt` (по группам), `TestGate_PreviewOfIsBuiltForTheQuestionOnly` | да |
| Превью называет проект записи у каждого изменения по id | #1387, #1615 | `TestPreviewProject_TargetDeclared` (обход реестра: изменение по id без `target` роняет тест), `TestPreviewProject_NamesWhereTheWriteLands` (каждый инструмент с `target` доходит до превью), `previewNoProject` пуст | да; строка 8 журнала (FB19I-20: `backup_delete`, `k8s_nodepool_delete`) на кандидате держится этим обходом — статус строки сверить «релизу» |
| Значение вызывающего, разделители строк, байты не в UTF-8 в превью — экранированы | #1858, #1882, #1970 | `TestPreviewText_SeparatorsAndInvalidUTF8`, `TestPreviewText_WholeValue` (одна функция `engine`), `TestPreview_CallerValueStaysOnItsLine`, `TestPreview_InvalidUTF8ShownReplaced` | да по одной точке |
| Слово согласия у удалений «Deletes data.»; одна формулировка правила согласия | #1670, #1505 | `TestConsentWord_EveryDeleteNamesItsClassOrWhyNot` (обход `allDeletes()`), `TestConsentNote_OneWordingAtEveryToolThatAsks` | да |
| Н1–Н3: повтор каждого изменения объявлен; поля тела в описи; ход называет следующий шаг | #1396, #1400, #1407 (процесс) | `TestConsent_EveryChangeDeclaresItsRepeat` (`repeatUndeclared` пуст), `TestContract_WorldBodiesInTheInventory`, `TestProgress_ChangeGoingOnNamesTheNextStep` (`noNextStep` пуст) | да |
| Вопрос `stack_apply` называет шаги, их цену, уже созданное и выполненное задание | #1729, #1760, #1474 | `TestStackApply_TheQuestionNamesEveryStep`, `TestStackApply_TheQuestionTellsAMadeResourceFromACreate`, `TestStackApply_JobDoneAsksAndSaysItCreatesNothing` | точка (один инструмент) |
| Вопрос `ca_start`, `ca_stop`, `k8s_add_addon` называет, что уже есть | #1478 | `TestCaStartStop_QuestionNamesTheStateOfTheApp`, `TestK8sAddAddon_QuestionSaysWhatHappensToTheInstalled` | точка |

## Честный ход операций и итоги (ось 2, D111)

| Поверхность | PR | Инвариант | Класс |
|---|---|---|---|
| D111: «готово» только у завершённого (`ca_start`, `ca_stop`, `disk_create`, `subnet_create`, `snat_create`) | #1828, #1865, #1872, #1936 | `TestProgress_AnOverAnswerIsOver` (#1828: обход мира паритета, ответ «завершено» на незавершённую операцию роняет тест; `overWhileGoingOn` пуст), `TestMadeAnswer_*`, `TestDiskCreate_TheOutcomeIsTheWaits`, `TestCaStartStop_TheAnswerIsTheOperations` | да |
| Создания баз и Kubernetes называют следующий шаг и состояние ресурса; состояние — `_untrusted` | #1903, #1940 | `TestProgress_ChangeGoingOnNamesTheNextStep`, `TestGoingOn_*` (одна функция `goingOn`), `TestUntrusted_EveryFieldWithTextOfTheCloudIsNamed` | да |
| Отказ по квоте: `details.cause: "quota"` у каждого инструмента и в операции | #1488, #1522, #1663, #1667 | `TestErrAnswer_QuotaRefusalNamesItsCause` (одна точка `errAnswer`), `TestOperation_EveryOneHasAWorldOfAFailedOperation` (журнал, строка 3), `TestQuotaCause_InJSONAtThePathTheDocsName`, `TestQuotaFailure_FailedOfAnotherService` | да |
| Шаг стека ждёт свой ресурс по id: чужой того же имени не подхватывается, пропавший — ошибка | #1801, #1844, #1893 | `TestStackStep_AnOperationsEndChecksTheStepsResourceIsThere` (`vpc_create`, `tg_create`), `TestStackStep_TheWaitedResourceNotFoundEndsTheStep` (PG), `TestStackVMStep_*` (ВМ) | **точка** по перечню видов шага; пробел П4 |

## Тексты для модели и недоверенный текст

| Поверхность | PR | Инвариант | Класс |
|---|---|---|---|
| `agent_skill`: карточки групп по MCP; пустая группа — `USAGE`; сервер одной группы и `--allow-project` | #1520, #1671, #1674, #1683 | `TestGroupCards_CoverEveryToolFromTheDeclarations`, `TestAgentSkill_EveryNameInTheCardIsServedOrCovered` (обход `Groups()`), `TestAgentSkill_CardOverMCPCarriesNothingLocal`, `TestAgentSkill_CardOfEveryGroupWithNoTerminalInIt`, `TestAgentSkill_TheCardSaysWhatARepeatOfADeleteAnswersOnThisServer`, `TestReadOnly_*`, `TestParity_NewCommandsJSONEqualsToolsCall` | да |
| Короче описания в `tools/list` (пять волн) | #1505, #1517, #1531, #1543 | `TestToolsList_EachToolWithinItsBudget`, `TestToolsList_SizeBudget`; замер — `audits/tools-list-budget-1.9.md` | да |
| «Billable» у каждого платного создания; «повтор — новое изменение» | #1513 (процесс), #1479 | `TestDescription_SaysWhatThePreviewBills`, `TestConsent_EveryChangeDeclaresItsRepeat` | да |
| Описания удалений называют `NOT_FOUND` на сервере с `--allow-project` | #1684 | `TestAlreadyDeleted_DescriptionNamesTheBoundAnswer` (обход удалений, оба языка) | да |
| Каталог: `stack_status`, `k8s_kubeconfig` — «побочно»; тексты `close_ports`, `deploy_apply` | #1782, #1767 | `TestToolsDoc_NoMeansARead` (одна точка `tools.EffectOf`), `TestCatalog_CloseAndApplyTextsSayWhatTheyDo` | да для «побочно»; тексты двух инструментов — точка |
| Текст облака — `_untrusted` (ошибки, kubeconfig, ключи S3, кластеры, итог стека) | #1706, #1850, #1940 | `TestUntrusted_EveryFieldWithTextOfTheCloudIsNamed` (#1600, обход реестра на враждебном теле; `untrustedFieldPending` пуст), `TestUntrusted_CloudTextMarkedOverMCP` (четыре случая через сервер), `TestUntrustedNames_*` (долг `namesUnmarked`, 36) | да; долг имён — пробел П6 |
| Слово облака в тексте evo — в кавычках одной строкой | #1863, #1642, #1957 | `TestCloudWords_QuotedInEvoText` (скан форматов `internal/`; долг `unquotedCloudWords`, 31), `TestCloudWords_QuotedInEvoErrors` | да; долг — пробел П6 |

## Язык CLI (D6)

| Поверхность | PR | Инвариант | Класс |
|---|---|---|---|
| Ответы, заметки, следующие шаги, превью на русском: зоны A (ВМ, диски, сеть, deploy, CA) и B (базы, Kubernetes, стеки); `doctor`, `org_quotas`, источник цены | #1868, #1883, #1886, #1888, #1889, #1917 | ошибки и вопрос согласия — обход `TestQA11_ToolErrorsSpeakCLILanguage` (`walkLang`: только `confirmation_required`, `USAGE`, `CONFIG_INVALID`, `NOT_FOUND`, `API_ERROR`); успешные ответы и «идёт» — `TestLangA_*` (3), `TestRussian_AnswersOfTheDatabasesKubernetesAndStacks` (перечень `want`), `TestDoctor_CheckNamesSpeakCLILanguage`, `TestRenderAnswer_ThePriceSourceInRussian`, `TestSourceRU_EverySourceOfThePackage`, `TestCLICommand_NamesTheCommandOfTheTool` | **точка** для успешных ответов; пробел П1 |

## Секреты и транспорт

| Поверхность | PR | Инвариант | Класс |
|---|---|---|---|
| Адрес консоли ВМ не выходит в ответ, ошибку, журнал ответов (D110, ТЗ §5.1) | #1919, #1947 | `TestSecrets_VMConsoleFieldsLeaveNoSurface` (поля консоли из `provision`, мир паритета, все поверхности), `TestConsoleFieldsLeaveNoErrorText`, `TestHideConsole_FailsClosed` | да |
| `X-Request-Id` в каждом запросе и в ошибке; id ответа с эхом секрета не называется | #1703, #1951 | `TestRequestID_EveryRequestCarriesOne` (оба транспорта), `TestTransport_OneBase` (1.7), `TestRequestID_ErrorOfAnAnswerNamesIt`, `TestFailedRequest_*` (7, одна функция) | да |
| TLS не ниже 1.2 | #1388 | `TestBaseTransport_DeclaresTLS12`, `TestBaseTransport_RefusesTLS11`, `TestTransport_OneBase` | да |
| `EVO_RECORD_ANSWERS`: формы ответов без значений, права 0600 | #1714, #1765 | `TestRecordAnswers_*` (5), точка записи — в базовом транспорте | да по одной точке |
| Ответ 404 службы с оборванным телом — ошибка обрыва | #1780 | `TestRouteNotFound_ReadErrorReachesTheCaller`, `TestPeek_TheOnlyReadOfABodyGivenOn` | точка |

## CLI и аргументы

| Поверхность | PR | Инвариант | Класс |
|---|---|---|---|
| Списки отбирают по нескольким `ids`, `states`, `statuses`; пустой фильтр — `USAGE` | #1494 | `TestListFilters_EveryListSelectsBySeveralValues` (обход `listSpecs`), `TestListFilters_ListFlagsRefuseAnEmptyValue`, `TestListFilters_HelpShowsFiltersAsABlock` | да |
| D100: пустая строка в аргументах области — `USAGE` | #1398 | `TestScopeFlags_EveryOneRefusesAnEmptyValue`, `TestScopeArgs_*` (`scopeFlagUnmarked` пуст) | да |
| Целый аргумент вне 32 бит — отказ до обработчика | #1566 | `TestIntegerArgs_OutOf32BitsRefusedBeforeTheHandler` (одна проверка `CheckArgs`), `TestIntegerFlag_OutOfRangeRefusedAsTheToolDoes` | да |
| D108: `stack_plan` отказывает неизвестному полю манифеста | #1673 | `TestStackPlan_FreePartsOfTheManifestAreDeclared` (обход типа манифеста), `TestStackPlan_UnknownFieldsSameInCLIAndMCP`, `TestStackPlan_UnknownFieldsRefusedBeforeTheCloud` | да |
| Реестр строится один раз за запуск | #1559 | `TestCLI_BuildsTheRegistryInOnePlace` (скан `internal/cli`), `TestRegistry_BuiltOnceARun` | да |
| `evo doctor`: запись evo в Claude Desktop; проверка копии и группы узлов по id | #1709, #1615 | `TestDoctor_ChecksTheEntryOfAClientOfFullPath`, `TestDoctor_ClientsOfFullPathAreRead` | точка |
| `deploy apply`: заметка о недоступном SSH — факт, проверка порта без сети | #1720, #1454 | `TestSSHUnreachedNote_SaysTheFactNotACause`, `TestCLIEngine_ChecksPort22WithoutAConnect`, `TestDeployApply_RepeatAfterAppliedSaysSo` | точка |

## Ускользнувшие дефекты 1.9 — сверка со строками журнала

Разбор — в `docs/internal/handoff/1.9-journal.md`, «Ускользнувшие дефекты»;
здесь только состояние инварианта на кандидате.

| Строка журнала | Инвариант на кандидате | Замечание к статусу |
|---|---|---|
| 1–6 | `TestDelete_EveryStateOfTheSpecHasAWorld` (#1563), `TestSGRules_EveryKindOfSourceHasAWorld` (#1561), `TestOperation_EveryOneHasAWorldOfAFailedOperation` (#1527), `TestSGRules_EveryStateOfTheTableHasAWorld` (#1557), `TestBackupCreate_TheNameTakenByState` (#1544), `TestIntent_ReadsTheReasonOfEveryAnswerTheRegistryGives` (#1553) | закрыты, совпадает |
| 7 | не продукт (CI): #1541; М5 — #1572 | вне сверки |
| 8 | `TestPreviewProject_TargetDeclared` + `TestPreviewProject_NamesWhereTheWriteLands`: `backup_delete`, `k8s_nodepool_delete` объявляют `target` и доходят до превью | в журнале «открыта заново» — на кандидате держится обходом; сверить «релизу» |
| 9 | `TestIntent_ACallsOwnRecordIsNotInItsOwnAnswer` (#1575), `TestStateDir_RecordsAreOfTheDeclaredDirectoryNotOfTheProcess`, `TestIntent_OnlyTestsReplaceTheReadingOfTheRecords` (#1578) | #1578 слит — строку можно закрывать |
| 10 | `TestIntent_ACallReadsTheRecordsOnceUntilItWritesThere` (#1578) — точка на число чтений | рост цены с числом незакрытых записей — не установлено |
| 11 | `TestParityWorld_YesAfterACutCallSendsWhatMCPSends` (#1603) — обход инструментов с согласием | в журнале «открыта» — инвариант слит; сверить «релизу» |
| 12 | `TestHeld_AnotherNameOfTheSameTargetDoesNotCloseTheRecord` (#1603) — точка `backup_restore` | открыта по классу — пробел П2 |

## Вне сверки

- `vm_console` (D110) в реестре кандидата нет; в 1.9 есть только защита
  полей консоли в ответах и ошибках (строки «Секреты и транспорт»).
- Сторож публичного следа (строка плана «Сторож публичного следа в CI») —
  процесс, не поверхность продукта.
- Убывающие списки со сроком 1.10 (`stateWorldsPendingShipped` — 44
  выпущенных инструмента) — долг выпущенного, не поверхность 1.9.

## Пробелы

Ни один не роняет класс, на котором держатся 16 новых инструментов. Решение
«1.9 — релиза (4)» 14.11: П1, П2, П6 — до rc.1 (строки плана со сроком
rc.1); П3, П4 — бэклог `process-handoff.md`, кандидат 1.10; П5 — вопрос
владельцу о живом запуске задаёт «релиз».

| № | Пробел | Риск | Что закрывает | Хозяин |
|---|---|---|---|---|
| П1 | Язык CLI: обход `walkLang` смотрит отказы и вопрос согласия, но не `note`, `next`, `warning`, `resource_status_note` успешного ответа и ответа «идёт»; русский там держат точки по зонам A и B (перечень `want`) | новая заметка в успешном ответе выйдет по-английски в русском CLI, и ни один тест не упадёт | строка плана «Язык CLI в успешных ответах…», п. (1): `walkLang` проверяет и ответы со `status` кроме ошибок — поля заметок на `englishPhrase` при `ru`; нынешние английские строки — убывающий список со сроком | «Процесс» |
| П2 | Удержанная запись: «другое создаваемое с той же целью не закрывает запись» — мир одного инструмента (`backup_restore`); код общий (`settleHeld`, `named`), обхода нет | инструмент, у которого цель и создаваемое различаются (например, создание внутри родителя по `target`), закроет чужую удержанную запись и отправит второй запрос | строка плана «Удержанная запись закрывается чужим ответом…»: обход реестра — каждый инструмент с `target` в `IDArgs` и `CreateName`, два вызова с одной целью и разными именами на мире удержанной записи | «Процесс» |
| П3 | D105 «весь интернет на общей группе» — тесты двух инструментов (`open_ports`, `sg_rule_create`) и порога в `sgrules`; нет проверки, что каждый путь, добавляющий правило в существующую группу, проходит эту проверку | новый инструмент правил (или правка группы стеком) откроет порт всему интернету без слова человека | скан по образцу `TestCreateFloatingIP_OnlyThroughReserve`: создание правила группы вне создания своего ресурса (`deploy`, `provision`) — только через функцию D105; пути перечислены в тесте | «Процесс» |
| П4 | Шаг стека сверяет свой ресурс по id — перечень видов (`vpc_create`, `tg_create`, PG, ВМ); нового вида шага тест не увидит | новый вид шага подхватит чужой ресурс того же имени или закончится «done» без своего ресурса | обход видов шагов манифеста (тот же источник, что `TestStackPlan_FreePartsOfTheManifestAreDeclared`): у каждого вида — мир «ресурс шага пропал» и «чужой того же имени», шаг кончается ошибкой и чужое не берёт | «Процесс» |
| П5 | `backup_*`: поведение держится мирами; живой области cloud-e2e нет (`tools_backup.go` в храповике `TestClassify_EveryToolFileHasALiveArea`), состояния «создаётся» и «ошибка» при удалении объявлены непроверенными вживую (`creatingUnseen`, `failedUnseen`) | таблица состояний верна по спецификации, но не по платформе — тот же класс, что строки 3–4 журнала | строка плана «живая область `backup` в cloud-e2e» (до rc.1 по `rc1-scope-1.9.md`, C-81); запуск — по «да» владельца | «Процесс» (пробы) |
| П6 | Класс-тесты есть, но убывающие списки со сроком «1.9, до rc.1» не пусты: `namesUnmarked` — 36, `unquotedCloudWords` — 31, `bodyUnnamed` — 38 и `humanUnnamed` — 12 (с причинами; принятие «Риском» каждой строки — не установлено) | имя ресурса из облака без пометки `_untrusted`, слово облака без кавычек в тексте evo — у выпущенных инструментов | строка плана «Просроченные убывающие списки» (решение владельца 13.11, #1590, вопрос 19): правки — «разработка» A, B, C по спискам; остаток — исключение с причиной, принятой «Риском» | «разработка» (A, B, C); сверка — «Процесс» |

## Итог

- Строк-поверхностей — 69 по 12 темам; класс «да» — 51 (из них 3 — «да
  по одной точке», 13 — «да» с точечной частью строки или объявленным
  исключением); «точка» — 17 (4 из них — пробелы П1–П4); без инварианта —
  1 (живая область `backup_*`, П5).
- Все 16 новых инструментов стоят в общем мире паритета и в описи
  состояний; изменяющие — вне долга `stateWorldsPendingShipped`.
- Строки журнала 8, 9 и 11 на кандидате держатся слитыми инвариантами —
  статусы сверить «1.9 — релизу»; строка 12 — П2.
- Пробелы (6):
  - П1 — язык CLI в успешных ответах и «идёт»: расширить `walkLang` на поля
    заметок успешных ответов — «Процесс».
  - П2 — удержанная запись, одна цель и разные создаваемые: обход реестра по
    `target` и `CreateName` — «Процесс».
  - П3 — D105 у каждого пути, добавляющего правило в существующую группу:
    скан единственной точки — «Процесс».
  - П4 — шаг стека сверяет свой ресурс по id: обход видов шагов манифеста —
    «Процесс».
  - П5 — живая область `backup_*` в cloud-e2e — «Процесс» (пробы), по «да»
    владельца.
  - П6 — убывающие списки со сроком 1.9 (`namesUnmarked`,
    `unquotedCloudWords`, `bodyUnnamed`, `humanUnnamed`) — «разработка» A, B,
    C; сверка — «Процесс».

## Второй проход, 17.11: пути, которых инвариант не проходит

Поручение владельца 17.11 через «1.9 — релиз (4)» (строка #2171). Повод: за
17.11 четыре дефекта прошли мимо существующих инвариантов:

- **#2177** — слово облака в ошибке оборванного вложенного чтения;
- **#2190** — тест workflow проверял подстроки, а не поля;
- **RequiredOneOf** — мир враждебных имён не доходил до 14 инструментов;
- **#2166** — шаги `stack_apply` не попадают в журнал изменений D43.

Первый проход (выше) отвечал на вопрос «есть ли у поверхности инвариант
класса». Этот отвечает на другой: «какие пути инвариант класса не обходит».

**Метод — только чтение и мутации на копии:**
- находки Астры с меткой `внешняя-проверка` за 12–17.11 — 99 issue, из них
  14 — аудиты без находок и описи. Классы ниже — по заголовкам, тела сверены
  выборочно;
- мутации, которыми 17.11 проверялись #2183, #2187, #2163 и ветка
  RequiredOneOf (счёт — в описаниях PR);
- отчёт Астры по той же задаче входит сюда по получении (раздел
  «Вход Астры»).

### Инварианты класса × пути вне обхода

| Инвариант (обход) | Чем обходит | Путь вне обхода | Как вскрылось | Состояние на 17.11 |
|---|---|---|---|---|
| `TestUntrustedNames_*` (мир враждебных имён) | каждый инструмент реестра, обязательные аргументы | аргументы `RequiredOneOf` (14 объявлений): отказ USAGE до облака | «Риск», строка #2171 | закрыто: `requiredCalls` и `TestRequiredCalls_PassTheArgumentCheck` (в #2177 A(7)); нашлись 3 дефекта |
| то же | — | значения, которые не проходят форму (enum, CIDR) | то же | закрыто там же |
| то же | — | необязательные флаги, ведущие к вложенному чтению (`rules=true`); обрыв второго чтения | Астра #2177 | ветка `…-proc-cloud-words-in-errors`: обрыв со 2-го по 6-й запрос × флаги; красный ровно на #2177 |
| то же | — | значение, которого в облаке нет: подсказка перечисляет, что есть (`vm_resize`) | «Риск», #2171 | точка `hostileCalls`; общего хода нет — П7 |
| то же | — | статус облака, перенесённый в поле evo (`resource_status`, «unknown» как своё слово) | Астра #1925, #1952, #2153, #2175, #2176 | точки по инструментам; класс — П7 |
| `TestUntrustedNames_OneLineInText` (текст evo) | поля `message`, `next`, `hint`, `error` JSON | человеческий вывод CLI (ANSI, U+2028), русский перевод | Астра #1848, #1950, #1942, #2024 | точки; общего хода по выводу CLI и `ru` нет — П7 |
| Мир паритета (`parity_fixtures_test.go`) | каждый инструмент: CLI = MCP | форма ответа по спецификации, а не по живому ответу | ретро 1.9, строки 3–4 журнала; каталог форм рос только из api-probe | ветка `…-proc-live-forms` (импорт форм прогона, проверка «только формы»); PR — после #2145 |
| Инвариант передачи тела (#2183) | `peek`, запись форм, 404 службы, `Transport` | `UserAgent` без записи; ошибка `Close`; замена HTML | Астра #2197 | закрыто в #2183 (`5593654`) |
| Тесты workflow (`internal/workflows`) | разбор YAML | условие `if`, права, `with`, секреты в `env` workflow — проверки подстрокой (`strings.Contains`): 60 мест в `workflows_test.go` | Астра #2190 (4 из 14 мутаций) | #2163: точное сравнение для yield; остальные 59 мест — П8 |
| `TestStackApply_ConsentCreatesWhatTheQuestionNamed` (#2187) | 13 видов шагов × 8 состояний × 2 мира | второй и следующие прогоны (`stack_status`), журнал изменений шагов | Астра #2166 | журнал D43 у шагов — «разработка»; ход по прогонам — П9 |
| Слой намерений и повтор (матрица сбоев) | каждый create и delete реестра | повтор CLI в цикле с `--yes` после ответа без квитанции | Астра #2113, #2148 | «разработка» (D112); миграция расходится с поведением — класс К2 |

### Ускользнувшие 1.9 по классам (Астра, 12–17.11)

Ускользнувший — по CLAUDE.md §5: найден не тестами. Строки журнала 1–12
(`handoff/1.9-journal.md`) сюда не повторяются.

| Класс | Число | Примеры | Почему тесты не поймали | Недостающая механика |
|---|---|---|---|---|
| К1. Текст облака без `_untrusted` или кавычек | 18 | #2177, #2175, #1925, #1802, #1942, #1848 | мир враждебных имён обходит только обязательные аргументы, первое чтение, JSON и английский | враждебные данные на всех путях — М2 |
| К2. «Миграция», CHANGELOG и docs расходятся с поведением | 10 | #2148, #2118, #2075, #1750, #2078 | тест проверяет, что номер D есть в «Миграции», а не изменившиеся случаи | строки «состояние → старый и новый ответ» из миров (#1769) — М4 |
| К3. Ход и итог создания, повтор после обрыва | 17 | #2113, #2112, #1963, #1924, #1921, #1830, #1766, #1748 | миры по спецификации: живых форм состояний нет; одна точка — один инструмент | миры по живой форме — М3; обходы состояний есть, но не по всем путям |
| К4. Журнал D43 и слой намерений | 7 | #2165–#2168, #1817–#1819 | у журнала нет обхода «каждая запись реестра, в том числе шаг стека»; хвост JSONL — точки | обход записей журнала по реестру — П9 |
| К5. Секреты и маскирование | 10 | #1945, #1946, #1806, #1751, #1933 | маскирование проверяется на перечне форм, а не на порождённых | порождение форм (экранирование, регистр, кодировки) — М2 |
| К6. Корреляция `X-Request-Id` | 4 | #1752, #1753, #1754, #1934 | журнал попыток без логической операции (#1769) | закрыто в 1.9 частично; остаток — «разработка» |
| К7. Транспорт и файлы | 3 | #1746, #1747, #2197 | таблица слоёв неполная | закрыто #2183 |
| К8. Тесты и CI самого процесса | 9 | #2188–#2190, #1756, #1755, #1841, #1968, #1962, #1741 | тесты процесса не проверялись мутациями | мутационная проверка инвариантов — М1 |
| К9. Язык D6 | 1 | #1943 | `walkLang` не смотрит JSON `ru` | П1 первого прохода |
| К10. Каталог агента, установка, документация | 6 | #1743–#1745, #2097, #2103, #2104 | у текстов для агента нет мира выполнения | вне механик; точки |

Итого 85 находок, из них 32 закрыты (по статусу issue на 17.11, около 16:50 UTC).
Классы К1, К3 и К8 дают 44 из 85. Их общий корень: инвариант обходит
реестр по одному пути — обязательным аргументам, первому чтению, форме по
спецификации, подстроке. Пути в стороне от него проверяет только внешний
взгляд.

### Недостающие механики

| № | Механика | Что даёт | Цена | Порядок |
|---|---|---|---|---|
| М1 | **Мутационная проверка инвариантов.** Каждый инвариант класса объявляет каталог мутантов продукта: правок кода, на которых он обязан падать. Ночной job накладывает их по одному и требует FAIL. Мутант, который выжил, — красный | инвариант не стареет молча: 17.11 мутанты нашли пробелы в 4 из 4 проверенных инвариантов (#2183, #2187, #2163, мир имён) | каталог — по 3–6 мутантов на инвариант; job — минуты на мутант (пакет, не весь `./...`) | 1.10, первым |
| М2 | **Враждебные данные облака на всех путях.** Генератор вызовов по реестру: обязательные аргументы × `RequiredOneOf` × флаги × «нет такого значения». Мир: враждебное имя, статус, текст отказа во всех полях. Обрыв k-го запроса. Проверка JSON, человеческого вывода CLI и `ru` | закрывает К1 и часть К5 одним ходом вместо точек | генератор есть (`requiredCalls`, `withFlags`, `brokenAfterAPI`) — сводится в один мир; выводы CLI — через `runCLI` | 1.10 |
| М3 | **Миры по живой форме.** Каталог `platform-answers.json` пополняется из каждого живого прогона (импорт), миры паритета сверяют свою форму с каталогом, а не со спецификацией | К3: форма состояния, которой нет в спецификации, ломает мир до релиза | импорт и проверка готовы в ветке; заполнение — по прогонам | rc.1 → 1.10 |
| М4 | **Миграция из миров.** Для изменённого решения D строки «состояние → старый и новый `reason`, код, вопрос → действие» порождаются мирами; тест требует ссылку из «Миграции» на каждую изменившуюся строку | К2 | 1–2 единицы на механизм (#1769) | 1.10 |

### Пробелы второго прохода (строки бэклога)

| № | Строка бэклога | Хозяин | Версия-кандидат |
|---|---|---|---|
| П7 | Мир враждебных данных: каждое значение, которого нет в облаке (подсказки «есть только …»), перенесённый статус облака, человеческий вывод CLI и `ru` — один обход реестра (М2) | «Процесс» | 1.10 |
| П8 | Тесты workflow: 59 проверок подстрокой в `workflows_test.go` → сравнение полей YAML; каталог мутантов workflow (права, `if`, секреты, checkout) | «Процесс» | 1.10 |
| П9 | Стек: ход по прогонам (`stack_status`) — повтор согласия на каждом прогоне до `done`; журнал D43 — каждая запись через реестр, в том числе шаг стека, видна в журнале (по #2166; «гипотеза» до разбора «разработки») | «Процесс» (мир), «разработка» (журнал) | 1.10 |
| П10 | М1: каталог мутантов инвариантов классов и ночной job; первая волна — мир имён, паритет, передача тела, стек, yield | «Процесс» | 1.10 |
| П11 | М4: миграция из миров для изменённых D (#1769, класс #1750) | «Процесс» | 1.10 |

### Падения CI не от diff, 1.9

(по получении сводки прогонов; раздел дополняется до тега)

### Вход Астры

(по получении независимого отчёта Астры; раздел дополняется до тега)
