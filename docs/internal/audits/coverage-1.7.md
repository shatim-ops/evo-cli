# Сверка охвата тестов с поверхностью 1.7 (кандидат 9786b913, 04.11.2026)

«Процесс · 1.7» по `CLAUDE.md` §0, правило 3, и шагу 0
`docs/RELEASING.md`: каждая новая поверхность 1.7 держится инвариантом на
свой риск — тестом, который поймает следующий такой же случай, а не только
этот. Кандидат — `main` на `9786b913` («1.7 — релиз», 04.11); diff
`v1.6.0..9786b913`, продуктовые PR с меткой «(продукт, 1.7)» — 38.

Колонка «класс» — да, если тест ловит новый инструмент, команду или случай
того же рода без правки теста (обход реестра, общий мир паритета, закрытый
перечень, одна точка решения с тестом на неё); «точка» — тест держит
названный случай.

## D69: предел вызова MCP, этапы на ВМ, отмена

| Поверхность | PR | Инвариант | Класс |
|---|---|---|---|
| Долгий вызов MCP отвечает не позже ~45 с состоянием и следующим шагом | #745, #752, #759, #760, #769, #778, #781 | `TestMCP_NoCallOutlivesTheBudget` (обход реестра на мире медленного облака), `TestWaits_ReachedToolsHoldTheBudget`, `TestMCP_BudgetCoversTheWaits` (#737); точки по инструментам — `TestD69_*` (14), `TestHelm_*`, `TestKubectl_StoppedAtTheLimitSaysWhatHolds`, `TestDiskCreate_AttachAtTheLimit…`, `TestDiskAttach_UnsettledAtTheLimit…` | да |
| Этапы `deploy` на ВМ (вариант C): этап переживает сессию, повтор продолжает | #769, #777, #762 | `TestDeploySSH_EachStepUnderTheBudget`, `TestDeploySSH_DockerNeverReadyUnderTheBudget`, `TestDeploySSH_ForceStartsComposeUpOnceAcrossRepeats` (#777), `TestDeployStage_OnceAndNotCutShort` (#762), `internal/vmstage` (`shell_test.go` — скрипт этапа в оболочке); вживую — шаг `e2e` «deploy с обрывом посреди этапа» (#845, на rc.1) | да по миру; что образ Cloud.ru не убивает процессы сессии (`KillUserProcesses`) — только вживую, пробел П1 |
| Отмена — `INTERRUPTED`, в CLI код 130 | #745 | `TestD69_CancelIsInterrupted`, `TestToolError_InterruptedIs130`, `TestD69_ProvisionCancelledIsInterrupted`, `TestD69_DeployApplyCancelledNamesVMPlanAndPassword` | да: одна точка `errAnswer` с тестом на неё |
| Пароль ВМ — в ответе один раз, и когда ВМ ещё создаётся | #745, #759 | `TestD69_DeployApplyCancelledNamesVMPlanAndPassword`, `TestHTTPRedact_StackPasswords` | точка (`deploy_apply`, `stack_apply`, `provision`) |
| `k8s_helm` по MCP не ждёт подов: `deploying`, затем `status` | #778 | `TestHelm_InstallOverMCPDoesNotWaitAndSaysDeploying`, `TestHelm_AFailingInstallOverMCPIsAnError`; вживую — `k8s_helm_mcp.sh` в `k8s-smoke` (#845, на rc.1) | точка (один инструмент) |

## D73, D74: присвоение и итог удаления

| Поверхность | PR | Инвариант | Класс |
|---|---|---|---|
| D73: присваивать только своё — `deploy_plan`, `provision` с именем чужой ВМ отказывают до вопроса | #788, #798 | `TestApply_AdoptsOnlyItsOwnVM`, `TestDeployApply_AVMOfAnotherFolderIsNotAdopted`, `TestDeployPlan_ANameTakenBySomeoneElseIsRefused`, `TestProvision_ANameTakenBySomeoneElseIsRefusedBeforeTheQuestion`, `TestD69_ProvisionAdoptsByTheRecordedIDNotTheName`, `TestDestroy_ARecordOfAnAdoptedVMIsNotDeleted`; W8 `TestConsent_RefusalWithoutWriteComesBeforeTheQuestion` | **точка** по четырём путям присвоения; пробел П2 |
| D74: асинхронное удаление ждёт итог своей операции; `--wait=false` — сразу | #801 | `TestDeleteOutcome_EveryAsyncDeleteReadsItsOperation` (#805: обход `deleteTargets`, без операции — закрытый `noOperationOutcome` с причиной), `TestDeleteOutcome_OperationErrorIsARefusal`, `TestDeleteOutcome_WaitFalseDoesNotWait`, `TestDeleteWaitFalse_TreeEqualsToolsCall` | да |
| `destroy` называет цель; ВМ, пропавшая к удалению, — `already_deleted` | #747 | `TestDestroy_ConfirmedWithoutTargetOverMCPIsUsage`, `TestDestroy_TargetGoneAtTheDeleteIsAlreadyDeleted`; D26 `TestAlreadyDeleted_EveryDeleteDeclared` | точка (`destroy`) поверх класса D26 |
| P3: превью удаления называет, создан ли ресурс этим evo (`provenance`) | #838 | `TestProvenanceOf_TheRule` (одна функция правила), `TestContract_DeletePreviewNamesProvenance` (перечень случаев: `destroy`, `release_floating_ip`, `deletePlan`), `TestProvenance_*` | **точка по перечню**: правило в одной функции, но зовут её три места (`delete_preview.go`, `destroy`, `release_floating_ip`); обхода «каждое превью удаления несёт `provenance`» нет — пробел П3 |
| Правило порта помечено id ВМ; удаление ВМ закрывает его из любого каталога | #819 | `TestRuleLabel_EveryVMDeleteClosesItsRulesFromAnotherFolder` (#825: обход удалений ВМ из `deleteTargets`), `TestDestroy_ClosesTheVMsLabelledRules`, `TestFindLabeled_*` | да |

## Цена и превью

| Поверхность | PR | Инвариант | Класс |
|---|---|---|---|
| Класс цены у каждого ресурса превью (`price_classes`) | #821 | `TestPriceClasses_EveryKindDeclared` (каждый вид `allResources` объявлен), `TestContract_EveryPreviewPricedOrListed` (#823: обход изменяющих на мире паритета: `priced` — своя доля `cost` > 0, `zero_at_create` — нота и `tariff_url`, `paid_unpriced` — нота) | да для превью изменяющих; превью без записи (`stack_plan`) — пробел П4, строка плана в работе |
| Цена стека в `stack_plan` из превью его ресурсов; без цены — без суммы (D75) | #855 | `TestStackPlan_CostNamesNoSumWithoutAPrice`, `TestStackPlan_EveryStepHasAKind` | точка — пробел П4 |
| Карта flavor по доле vCPU; цена диска из API; оценка ВМ с FIP | #846, #812, #815 | `TestEstimateFor_PricesTheVCPUShare`, `TestEstimateFor_ShareRatesOverride`, `TestDiskPreview_PriceFromAPI`, `TestEstimateVM_FloatingIPWithPublicAddress`, `TestFlavors_ListPriceIsThePreviewPrice` (#751); живая сверка — P49 2 из 2 после #846 (≈1 у трёх долей, evo-internal `runs/`) | точка по ставкам; живая сверка — api-probe P49, не CI |
| Описание в `tools/list` называет платность и публичный адрес | #834 (регрессия), #847 | `TestDescription_SaysWhatThePreviewBills` (обход превью изменяющих; 11 промахов в `descMoneyMisses`, Closes by 1.8) | да |
| Превью называет всё, что evo пишет рядом (state, правила, ключи) | #749 | `TestPreview_NamesEverySideRecord` (обход изменяющих JSON-мира) | да |
| Порты: группа и источник в превью `provision`, `deploy`, `deploy_apply` | #811 | `TestPortsPreview_NamesGroupSourceAndWarns`, `TestApply_OpensToThePlansSource`, `TestWideOpen_WarnsOfPortsBesidesTheWeb` | точка (три инструмента одного механизма) |
| M2: превью `deploy` называет загрузку по SSH; обновление проверяет TCP/22 до вопроса | #826 | `TestDeploy_PreviewNamesTheSSHUploadAndProbesPort22`, `TestRedeploy_PreviewProbesPort22` | точка |
| Подсеть без DNS-серверов — до вопроса | #818 | `TestSubnetCreate_PreviewWarnsOfNoDNS`, `TestNoSubnetHint_NamesSubnetCreate` | точка |
| Фильтр flavor по зоне: имя в любом регистре и id; `stack_plan` не пропускает проверку | #828 | `TestStackPlan_FlavorZoneCheckIsNotSkippedByASecondRead` | точка |

## Тексты для модели, скиллы, согласие

| Поверхность | PR | Инвариант | Класс |
|---|---|---|---|
| `confirmed` — сначала превью, `--yes` — согласие человека; `evo mcp` без `--toolsets` | #739 | `TestConfirmedDescription_PreviewFirst`, `TestYesFlag_IsThePersonsConsent`, `TestMCP_WarnsWithoutToolsets` | точка по текстам |
| Подсказки не предлагают удаление следующим шагом | #829 | `TestHints_DeleteIsThePersonsDecision` (#738: скан всех подсказок) | да |
| D68: знание скиллов — в описаниях, карточках, встроенном скилле; `skills/` удалены | #830, #831 | `TestSkills_KnowledgeHasItsPlace` (#755), `TestGroupCards_PrintTheirNotes`, `TestLayout_NoScenarioSkills`, `TestInitialize_InstructionsNameNoScenarioSkills` | да |
| Короткие описания: в `tools/list` только то, без чего вызов неверен | #834 | `TestToolsList_EachToolWithinItsBudget` (#754), `TestToolsList_SizeBudget`; рост — job `growth` (#837); платность и адрес — `TestDescription_SaysWhatThePreviewBills` (#847) | да |
| Недоверенный текст из облака (`_untrusted`) | #767 | `TestUntrusted_NoteIffMarker` (#756: обход ответов), `TestUntrusted_S3ObjectIsMarked`, `TestUntrusted_S3BinaryObjectIsMarked` | да |
| `s3 put`, `s3 bucket-create` без ключей S3 — отказ до вопроса | #776 | `TestNewGates_S3PutWithoutSettingsRefuses`; W8 `TestConsent_RefusalWithoutWriteComesBeforeTheQuestion` | да по W8 |

## Транспорт, CLI, установка

| Поверхность | PR | Инвариант | Класс |
|---|---|---|---|
| Один базовый транспорт с пингом HTTP/2 | #741 | `TestTransport_OneBase` (#743: скан кода), `TestBaseTransport_ChecksHTTP2Connections` | да |
| Журнал в ответах MCP без символов терминала | #748 | `TestPrinters_ABufferIsNoTerminal` (#742: скан кода) | да |
| evo не исполняет чужих файлов из `PATH` (kubectl, helm) | #778 | `TestExec_NoForeignFileFromPATH` (#773: скан AST) | да |
| Tab: обязательные флаги, файлы только у путей, меню zsh, `doctor` о втором evo | #770, #771, #774, #775 | `TestTab_EveryCommandCompletesOrSaysNone` (#757: обход дерева), `TestTab_RequiredFlagsFirst`, `TestTab_DirectivesThatOfferFiles` (#758), `TestCLISpec_FilesAndPathsApart`, `TestDoctor_*`; `install.sh` и `~/.zshrc` — `install_zsh_test.sh` | да для Tab; `install.sh` — точка |
| `evo configure s3`, `agent setup --skill-only` | #787 | `TestConfigureS3_FromTheProfilesKey`, `TestConfigureS3_Refusals`, `TestAgentSetup_SkillOnlyLeavesTheClientEntry` | точка |
| `init`: `project_name`, зона по умолчанию платформы | #780 | `TestInit_WritesProjectName`, `TestInit_NonInteractiveTakesDefaultZoneAndImage`, `TestDefaultZoneFirst` | точка |
| Доступ PG языком CLI, один 404 Organization API, `elapsed_sec` только где есть время | #806 | `TestPG_AccessFollowsTheLanguage`, `TestListProjects_404IsKept`, `TestOperation_ElapsedOnlyWhereTheTimesAre` (поверх `TestOperation_EveryOperationDeclared`, 1.6) | да для `elapsed_sec`; остальное — точка |

## Пробелы

Ни один не блокирует кандидат: поверхность держится точечным тестом или
живым шагом rc.1; класс — строкой бэклога (`process-handoff.md`) с
версией-кандидатом.

| № | Пробел | Риск | Что закрывает |
|---|---|---|---|
| П1 | Этап на ВМ переживает конец сессии SSH на образе Cloud.ru — только вживую | `KillUserProcesses=yes` в образе убьёт этап с сессией: `deploy` по MCP начнёт этап заново на каждом вызове | шаг `e2e` cloud-e2e rc.1 (#845); `lost` — пункт реестра и пересмотр отката (`systemd-run`), строка плана |
| П2 | D73: «присваивать только своё» — четыре пути присвоения, каждый своим тестом; общего обхода «каждый путь, который берёт существующую ВМ, сверяет её с записью» нет | новый путь присвоения (стек, будущий `vm adopt`) возьмёт чужую ВМ молча | бэклог 1.8: инвариант по путям `adopt` (одна функция проверки и обход её вызовов) |
| П3 | P3: `provenance` в превью удаления — правило в одной функции, но три места вызова и перечень случаев в тесте | новое удаление со своим превью (не `deletePlan`) выйдет без `provenance`, и агент не увидит, что удаляет чужое | бэклог 1.8: обход превью всех удалений реестра на мире паритета — у каждого `provenance.created_by_evo` из `yes`/`no`/`unknown` |
| П4 | Цена в превью без записи (`stack_plan`): `by_component` без класса — мимо `TestContract_EveryPreviewPricedOrListed` | агрегат цены с «0» у платной строки | строка плана 1.7 «Класс-инвариант цены превью» (хозяин — Процесс), в работе; rc.1 не блокирует |
