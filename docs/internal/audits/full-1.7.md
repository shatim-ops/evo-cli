# Полный аудит 1.7 (Fable, 04.11.2026)

Чек-лист `CLAUDE.md` §5: полный аудит перед тегом minor, по diff
`v1.6.0..9786b913` (`main`, кандидат тега `v1.7.0-rc.1`: последний
продуктовый PR версии — #855) плюс проход по чек-листу всего продукта и
четырём осям §6. 140 коммитов (140 PR), 374 файла, +19 406/−4 865; код
продукта — 82 файла, +4 763/−1 331; тесты — 181 файл, +9 876/−1 477;
документы — 77 файлов, +2 529/−1 790; CI и скрипты `.github/` — 33 файла,
+2 080/−260; каталог `skills/` удалён (7 файлов, −853). Отдельной облачной
сессией по решению владельца 04.11; только чтение, в облако не ходил
(cloud-e2e и агентный прогон — отдельно, по «да» владельца); CI и ревью
Qwen по PR не сверял — сверяет «1.7 — релиз». Пять подагентов аудита шли
параллельно по пунктам чек-листа; каждую находку с тяжестью выше низкой я
перечитал в коде сам.

Продуктовые PR диапазона (38): #739, #741 (транспорт), #745 (D69, точка
бюджета), #747 (H15), #748, #752 (D69 `provision`), #759 (D69 стек),
#760 (D69 `destroy`), #767, #769 (D69 `deploy`, вариант C), #770, #771,
#774, #775, #776 (F5a), #778 (D69 k8s), #780, #781 (D69 диск), #787,
#788 (D73), #798 (D73 `destroy`), #801 (D74), #806, #811 (P5), #812,
#815, #818, #819 (P6), #821 (классы цены), #826 (M2), #828, #829, #830
(D68), #831 (D68, `skills/`), #834 (P13), #838 (P3), #846, #855 (D75).
PR «(процесс)» (97): #709–#716, #719–#738, #740, #742–#744, #746, #749–#751,
#753–#758, #761–#766, #768, #772, #773, #777, #779, #782–#785, #790,
#792, #794, #797, #799, #803–#805, #807, #808, #810, #814, #816, #817,
#820, #822–#825, #827, #832, #833, #835–#837, #839, #840, #842–#845,
#847, #850, #851, #853, #854, #856–#859; без метки в заголовке (5,
процессные по содержанию): #786, #791, #795, #796, #802.

**Вердикт: блокеров тега нет — тег `v1.7.0-rc.1` на коммите слияния PR
версии (поверх `9786b913`) ставить можно.** Блокер — дефект кода, который
запрещает тег; таких нет. Четырнадцать строк ломающих изменений 1.7.0 в
таблице `docs/RELEASING.md` (D68, D69 × 6, D73 × 2, D74, D75 и три без
номера D — #747, #776, #806) оформлены по правилу: у каждой пункт
«Миграции» во фрагменте `changelog.d` (13 фрагментов с «Миграцией»),
условия 1–5 выполнены (раздел 5), решения D68, D69, D73, D74, D75 — с
текстом, не резервы. Необъявленных ломающих изменений уровня контракта
(код выхода, `reason`, ключ `--json`) не нашёл; две мелочи оформления —
FB170-5. Дефектов согласия, секретов и паритета нет; повтор create,
delete и apply безопасен (раздел 4). Две находки средней тяжести, обе —
после тега rc.1, рекомендую до стабильного `v1.7.0`: FB170-1 — запись
`deploy_apply` датируется концом ожидания, и при ВМ, которая шла к SSH
дольше пяти минут, `destroy` отказывает своей же ВМ по правилу D73 с
ложной причиной; FB170-2 — `deploy` с именем чужой ВМ спрашивает
согласие, получает 409 и подсказывает удалить ту ВМ — класс P1, который
#829 закрыл у `provision`, остался у `deploy`. Остальное — низкие,
гипотезы и шаги порядка выпуска.

Находок: блокирует тег — 0; высокая — 0; средняя — 2 (FB170-1, FB170-2);
низкая — 5 (FB170-3, FB170-4, FB170-5, FB170-7, FB170-9); до тега (шаги
порядка, не дефекты) — 1 (FB170-6); гипотез — 3 (FB170-8, FB170-9 и
гипотезы 1–4 ниже).

## Проверки

| Проверка (коммит `9786b913`, `main`) | Результат |
|---|---|
| Локально на `9786b913` | `gofmt -l` пусто; `go vet ./...` чисто (linux и `GOOS=windows`); `staticcheck ./...` чисто (установлен в сессии аудита, `honnef.co/go/tools` последний); `go test -count=1 ./...` — 46 пакетов ok, 5 без тестов, падений нет; `python3 scripts/changelog.py check` — ok (37 фрагментов, 13 с «Миграцией»); инструментов 157 — как на `v1.6.0` и как говорит `docs/mcp.md`; `docs/tools.md` и `docs/commands.md` совпадают с генерацией от кандидата (дерево после `TestToolsDoc`/`TestCommandsDoc` чистое) |
| `go test -race` | `internal/httpretry`, `internal/auth`, `internal/engine`, `internal/tools`, `internal/cli`, `internal/mcp`, `internal/vmstage`, `internal/deployplan`, `internal/testrules` — ok, гонок нет (`internal/cli` 408 с, `internal/tools` 182 с под детектором) |
| CI на `9786b913`, ревью Qwen | не сверял: аудит шёл без обращения к CI; сверяет «1.7 — релиз» по правилу §4 |
| Утечки в diff (добавленные строки, 19 406) | UUID — только фейки тестов нулевой формы и `11111111-…` (тест `configure`); IP — `0.0.0.0`, `10.x`, `127.0.0.1`, документационные `203.0.113.x`, `198.51.100.x`, `192.0.2.x`, `1.2.3.4` (вход теста Tab); хосты — `*.api.cloud.ru`, `cloud.ru`, `github.com`, `example.com`, документация клиентов MCP; почт и ключей нет. Сведения о платформе — только ссылками на пункты реестра (OPS-18, CMP-25, DB-5, DB-17, DB-21, NET-11, K8S-16) и строки «гипотеза», без сценариев |
| Тест, падающий без правки (по тестам в дереве) | D69 — `TestMCP_NoCallOutlivesTheBudget`, `TestD69_*` (14), `TestHelm_InstallOverMCPDoesNotWaitAndSaysDeploying`, `TestDiskCreate_AttachAtTheLimit…`; вариант C — `TestDeployStage_OnceAndNotCutShort`, `TestDeploySSH_*`, `vmstage/shell_test.go` (13, в CI стресс замка с `-race`); #747 — `TestDestroy_ConfirmedWithoutTargetOverMCPIsUsage`; D73 — `TestApply_AdoptsOnlyItsOwnVM`, `TestProvision_ANameTakenBySomeoneElseIsRefusedBeforeTheQuestion`, `TestDeployPlan_ANameTakenBySomeoneElseIsRefused`, `TestDestroy_ARecordOfAnAdoptedVMIsNotDeleted`; D74 — `TestDeleteOutcome_*`, `TestDeleteWaitFalse_TreeEqualsToolsCall`; #776 — `TestNewGates_S3PutWithoutSettingsRefuses`; #767 — `TestUntrusted_NoteIffMarker`, `TestUntrusted_S3ObjectIsMarked`; P5 — `TestPortsPreview_NamesGroupSourceAndWarns`; P6 — `TestRuleLabel_EveryVMDeleteClosesItsRulesFromAnotherFolder`; #821/#823 — `TestPriceClasses_EveryKindDeclared`, `TestContract_EveryPreviewPricedOrListed`; D75 — `TestStackPlan_CostNamesNoSumWithoutAPrice`, `TestStackPlan_EveryStepHasAKind`; #846 — `TestEstimateFor_PricesTheVCPUShare`; #812 — `TestDiskPreview_PriceFromAPI`; #815 — `TestEstimateVM_FloatingIPWithPublicAddress`; #838 — `TestProvenanceOf_TheRule`, `TestProvenance_*`; #826 — `TestDeploy_PreviewNamesTheSSHUploadAndProbesPort22`; #829 — `TestHints_DeleteIsThePersonsDecision`; D68 — `TestSkills_KnowledgeHasItsPlace`, `TestInitialize_InstructionsNameNoScenarioSkills`; #834 — `TestToolsList_EachToolWithinItsBudget`; #741 — `TestTransport_OneBase`; #748 — `TestPrinters_ABufferIsNoTerminal`; #771/#773 — `TestExec_NoForeignFileFromPATH`, `TestDoctor_OtherEvoIsNotRun`; Tab — `TestTab_*` (11); #806 — `TestOperation_ElapsedOnlyWhereTheTimesAre` |
| Инварианты «Процесса» на кандидате (полный проход `internal/testrules` и перечни) | зелёные: `TestConfirmedReadOnlyInTheGate` (гейт — одна функция `gated`/`engineGated`, `internal/tools/gate.go:48–83`; `inlineGates` 28 → 26: `handleS3Put`, `handleS3BucketCreate` ушли на `gated`, #776), `TestConsent_RefusalWithoutWriteComesBeforeTheQuestion` (`lateRefusals` пуст), `TestAlreadyDeleted_EveryDeleteDeclared`, `TestParity_CommandsAndTools`, `TestParity_JSONEqualsTool` (`jsonAliasCompat` не рос), `TestParity_YesEqualsConfirmed` (`yesDiffers` пуст — исключение для H15 не понадобилось: превью без цели отвечает `confirmation_required`), `TestPreview_NamesWhatTheWriteSends`, `TestPreview_HumanShowsEveryValue`, `TestPreview_NamesEverySideRecord`, `TestWallClockReadsDeclared` (+6 деклараций D69), `TestMisses_HaveADeadline` — 40 перечней, 270 строк, просроченных 0; пусты `lateRefusals`, `mcpOverBudget`, `deployStagePending`, `deleteHintMisses`, `untrustedNoteOnly`, `tabFileDefault`, `tabRequiredMisses`, `tabShellPending`, `textPrinterMisses`, `takenAdopts`, `takenNotReached`, `yesDiffers`, `bareRefs`, `worldDrift`, `liveWorldDrift`, `localJSONMisses`, `optionalUnnamed`, `previewNoProject`; `humanUnnamed` 31 → 22, `bodyUnnamed` 40 → 40, `toolsOverBudget` 3 (с потолками, «Closes by 1.8»), `sideRecordMisses` 1 («Closes by 1.7» — FB170-6), `bare404Gone` 1 (S3) |

## Чек-лист

### 1. Согласие (гейт D35, ось 1 §6: «отказ без записи = отказ до вопроса»)

- **Гейт — одна функция.** `gated` и `engineGated` (`internal/tools/gate.go:48–66`, `:72–83`) — единственные чтения `confirmed` в продуктовом коде, кроме перечня `inlineGates` (26, срок «Closes by 1.8»). Бюджет D69 — тоже одна точка: `Registry.SetCallBudget` (`internal/tools/registry.go:121`), ставит только `internal/mcp/server.go:158`; в `internal/cli` вызовов нет, CLI ждёт как раньше (H14).
- **Каждый новый отказ 1.7 стоит до чтения `confirmed` и без записи:**
  - D73 `provision` — `vmNameFree` внутри `planProvision` (`internal/tools/tools.go:1893–1900`, `own_vm.go:34–59`): чужое имя — `USAGE` с `existing_id`; непрочитанный список — тоже отказ, ошибка ключа пропускается (отказ ключом на исполнении). Своя запись `creating` — `adopts` в превью, второй ВМ нет.
  - D73 `deploy_plan` — `vmNameFree(…, ownIn(folderVMs))` до плана (`tools_deploy.go:157`).
  - D73 `deploy_apply` — `ForeignVMError` из `ApplyWithTimeout` (`internal/deployplan/apply.go:107–118`) приходит после `confirmed` (`tools_deploy.go:312`), но до любой записи: превью `deploy_apply` по замыслу локальное (B12-8); окно «ВМ человека между plan и apply» — принятый остаток, назван в D73. Отклонение от буквы «до вопроса» — факт, не находка.
  - D73 `destroy` — `checkedRecord` в `DestroyPreview` и в `Destroy` до `destroyGate` (`internal/engine/destroy.go:62`, `:109`, гейт `:121`); по `vm_id`/`name` — `engine.AdoptedVM` в `planDestroyTargeted` (`tools.go:2809–2813`); непрочитанная ВМ (кроме 404 и ключа) — отказ до вопроса, `INTERRUPTED` пробрасывается (`own_record.go:67–73`). См. FB170-1.
  - #776 — `planS3Put`/`planS3BucketCreate` строят клиент до `Plan` (`tools_s3.go:123–126`, `:189–192`): без ключей — отказ планировщика до вопроса, `--dry-run` тоже.
  - #747 — `recordedTarget` в ветке исполнения `engineGated` (`tools.go:2592–2599`): `USAGE` с `vm_id` записи, ничего не удалено; превью без цели прежнее. Структурно — отказ после чтения `confirmed` без записи, по замыслу H15.
  - D74 — `deleteOutcome` (`delete_outcome.go:54–75`) работает только после DELETE в `Execute`; `wait=false` — одно чтение `waitOff` (`registry.go:153–158`).
  - Проба TCP/22 (#826) — `ReachSSH` до `redeployGate` (`engine/deploy.go:142–144`): один TCP-connect без данных, 5 с, кэш на адрес; предупреждение, не отказ.
- **Ответы «не дождался» D69 — после согласия, и `next_args` с `confirmed: true` не расширяют согласие:** `provision` — `cutShort` только в цикле ожидания после `CreateVM` (`tools.go:2338–2394`), `next_args` — тот же вызов без `wait` и `image_metadata` (`provision_adopt.go:72–75`); `deploy_apply` — `until` после `confirmed` (`tools_deploy.go:338–342`), `next_args` — только `plan_id`; `disk_create` → `disk_attach` с тем же `disk_id`/`vm` — та привязка, на которую согласие дано (`tools_disk.go:216–224`); `destroy` — после DELETE (`tools.go:2940–2952`).
- **Превью у платного и разрушительного** есть у всех изменённых: `provision`, `deploy`, `deploy_apply`, `disk_create`, `s3_bucket_create`, `destroy` (с `provenance`), удаления D74.

### 2. Секреты (вывод, ошибки, журнал, превью, state, ответы MCP)

- **Пароль ВМ — один раз.** `provision`: `password` заполняется только при `GenPassword` в вызове, который создаёт ВМ (`tools.go:2010–2016`), при подхвате — пусто (`:2031–2035`); `creating`, `INTERRUPTED`, таймаут и `ok` первого вызова несут его (`leftFields`, `cutShort`), повтор — нет; state пароля не хранит. `deploy_apply`: `ApplyResult.Password` не сохраняется (`deployplan/apply.go:44`), выдаётся в `creating`/отмене с `note` «только в этом ответе» (`tools_deploy.go:360–365`). Стек: `Job.passwords` — неэкспортированное поле (`internal/stack/store.go:212`), `Save` его не пишет; `stack_status` собирает пароли только кранков этого вызова (`tools_stack.go:1181–1196`). Тесты `TestProvision_PasswordInLeftAnswers`, `TestD69_ProvisionRepeatAdoptsItsVM` (у повтора нет `password`), `TestHTTPRedact_StackPasswords`.
- **Маскирование по HTTP** — ключи `password`/`passwords`, `secret`, `*_token` (`internal/mcp/http_redact.go:34–59`); новые поля `output`, `log`, `phase_state` — текст, помечены `_untrusted` (`untrusted.go:49`), класс тот же, что у `logs` (FB13-4).
- **`evo configure s3`** (`internal/cli/configure_s3.go:46–84`): в argv только `--tenant-id`, секрет — из профиля, вывод — `tenant:<MaskKeyID>`.
- **`evo doctor`** — версия чужого evo через `debug/buildinfo.ReadFile`, без запуска (`doctor_path.go:95–111`); `TestExec_NoForeignFileFromPATH` — AST-скан, исполнение только в `kubetools`.
- **Этапы на ВМ** (`internal/vmstage`, `engine/stage.go`): в скрипте этапа — `docker compose up` и установка Docker, в `input` — sha256 compose и env (`stage.go:92–95`), сырых значений нет; `umask 077`; хвост лога — `output` с `_untrusted`.
- **`evo mcp` без `--toolsets`** — в stderr число инструментов и КБ (`cli/mcp.go:262–266`). **`install.sh`** — блок между метками, только `fpath` и `compinit`; `EVO_ZSHRC=0`, `--uninstall`.
- **Журнал D46** — имена аргументов и размер ответа, без значений.

### 3. Паритет CLI ↔ MCP (D13, D14)

- **Один обработчик, ветвление по бюджету, не по имени поверхности.** Предел живёт в контексте (`withCallStart`/`budgetLeft`/`callBound`, `registry.go:131–181`): шаги стека тратят бюджет внешнего вызова; helm — `--wait --timeout 8m` без бюджета, `--timeout` в пределах бюджета с ним (`tools_kube.go:381–384`), `stopped_at_limit` только при `capped` (`:405–408`); диск — `callBound` в `waitDisk` (`tools_disk.go:283`); операции и удаления D74 — `waitForOpJSON` режет `deadline` бюджетом (`opwait.go:100–104`, `delete_outcome.go:59`, 11 вызовов с `dns_zone_delete`). `evo deploy` в CLI — тот же обработчик (`engine_commands.go:248–250`), этапы на ВМ общие (`engine/deploy.go:369`), `compose-up` без предела, как в 1.6.
- **`wait` — тот же аргумент схемы:** `boolProp(waitDesc)` у `provision` (`tools.go:83`), `deploy_apply` (`:236`), `deleteWaitDesc` у 10 удалений D74 и `dns_zone_delete`; CLI-флаг генерируется из схемы (`trees.go:386–394`). Исключение — `evo tools call`: FB170-4.
- **`INTERRUPTED` — одна точка** `errReason` (`tools.go:3719–3722`): `context.Canceled` → `ReasonInterrupted` без подсказки о сети на обеих поверхностях.
- **H15** — `liveCaller(ctx)` (`caller.go:195`) в `handleDestroy` (`tools.go:2563`, `:2592–2599`); различие объявлено строкой H15 `docs/mcp.md:1004`; `jsonAliasCompat` `evo destroy` — 8 записей, в 1.7 не росло.
- **Человек видит каждое значение превью** — `TestPreview_HumanShowsEveryValue` зелёный, `humanUnnamed` 31 → 22: `ssh_reachable` — строка `ssh` (`engine/preview.go:297–305`), `provenance` — `details` удалений и вывод `evo tools call` (`cli/tools.go:735`), `ports_group`/`ports_source` (`create_preview.go:685–692`), классы цены — строка `cost` (`:916–921`), стек — таблица `cost` (`by_component` с `class`, без `monthly_rub` у неоценённых).
- **Tab** — обязательные флаги из `InputSchema.required` и `RequiredOneOf` реестра (`completion.go:95–102`); **`localCommands`** — `evo configure s3` с причиной (`local_commands.go:21`: ключи пишутся на диск машины evo, клиент MCP ключей не передаёт).
- `docs/mcp.md` H14 — перечень «пока предел держат» без удалений D74 и `ca_delete`, D74 в `docs/mcp.md` не упомянут — FB170-5.

### 4. Идемпотентность и уборка (ось 3 §6, D26)

- **`provision`**: запись `creating` — сразу после принятого POST (`tools.go:2233`); повтор подхватывает только запись своего имени, проекта и зоны (`provision_adopt.go:35–38`); ВМ без записи — `vmNameFree` → `USAGE` с `existing_id`; boot FIP записи переиспользуется (`:2110–2113`); обрыв бюджета после `active` — `cutShort` с `fipID` в записи. `TestD69_ProvisionRepeatAdoptsItsVM`, `TestCancel_ProvisionRepeatAfterCancelMakesNoSecondVM`, `TestAudit_Provision_NameTaken409DoesNotAdopt`. Подхват без сверки flavor — гипотеза FB170-9.
- **`deploy_apply`**: `plan.Owned = folderVMs` (`tools_deploy.go:345`), `ownVM` = запись папки или `MadeSince(plan.CreatedAt)` (`apply.go:520`); чужая — `ForeignVMError` до записи; `creating` при обрыве бюджета хранит план и запись (`:357`). Запись на пути с ожиданием пишется после возврата `ApplyWithTimeout` — FB170-1.
- **`deploy` (вариант C)**: этап не запускается, пока `running` (`engine/stage.go:120`); готовый для того же `input` не повторяется; `force` снимается из `next_args` после старта `compose-up` (`tools.go:1638–1643`); `lost` перезапускается и назван ошибкой (`stage.go:194–209`); замок — `flock` или каталог с владельцем (`stage.sh:53–56`, `:95–97`). `TestDeploy_ForceRunsComposeUpOnceAcrossRepeats`, `TestDeployStage_OnceAndNotCutShort`, `TestStage_ManyAtOnceNoneLostNoneUnread`.
- **FIP перед обрывом**: `cut()` ищет адрес по имени под своим контекстом (`engine/deploy.go:1083–1093`), id — в state; `TestAssignFloatingIP_CutAfterTheCreateNamesTheFIP`, `TestDeploy_BootFIPFoundAfterCancel`.
- **`destroy`**: ВМ в `deleting` — второй DELETE не шлётся (`tools.go:2907–2910`), запись `destroying` с `way_out` (`:3177–3209`); бюджет режет только ожидание; ВМ, пропавшая к удалению, — `already_deleted` с уборкой FIP, правил и записи (#747); правила по метке — 404 закрыто (`sgrules/labels.go:107–110`); старые правила без метки идут прежним путём через state (`engine/destroy.go:237–253`).
- **D74**: одна точка `deleteOutcome`; `done` — `deleted`, `error` — `API_ERROR` с текстом платформы, не дождались — `deleting` и `next_tool`; повтор после `deleting` — D61 `already_deleting`; `ca_delete` — `goneWatch` по `ca_get` (`tools_ca.go:245`); класс — `TestDeleteOutcome_EveryAsyncDeleteReadsItsOperation` (#805).
- **Стек**: шаг `provision` с `creating` остаётся `pending` с `ResourceID`, попытка не тратится, следующий crank подхватывает (`tools_stack.go:514–523`); `stack_destroy` видит `st.ResourceID` (`:1287`); `TestFault_Stack_PartialDoesNotCreateSecondVM`.
- Случаев «повтор создаёт второй платный ресурс или теряет созданный» в diff нет.

### 5. Полнота параметров платформы (§6, ось 4) и цена в превью

- **Классы цены — одна точка** `priceClasses` (`internal/engine/price_class.go:70–135`), ровно одно из `priceable` / `zero` / `why` на вид; необъявленный вид — `paid_unpriced` без ноты, что роняет `TestPriceClasses_EveryKindDeclared` и инвариант #823. `setPriceClasses` — из `Price()` (`create_preview.go:233`) и `priceFromAPI` (`:297`): превью без классов не бывает. `priced` — только своя доля > 0 (`share()`, `:180`).
- **«0 ₽ только по тарифу».** `zero_at_create` объявлен у одного вида — `s3_bucket` (`price_class.go:79`, `tariff_url` Object Storage); подсеть, VPC, маршрут, образ, секрет, бэкап PG, целевая группа — `paid_unpriced` «в тарифах строки для него нет» со ссылкой на индекс (`:116–129`) — решение владельца 04.11; k8s, узлы, Kafka, Redis — «нет до калькулятора (D17)» (калькулятор — 1.8, P50: 404, 36 из 36).
- **Карта** (`internal/pricing/pricing.go`): `ShareHourly` 1:1 / 1:3 / 1:10 с датой 04.11.2026 по пробе P49 (два прогона, `:169–177`); доля flavor — поле каталога `Flavor.Share`; доля без ставок — `est_note` только у `Source == SourceVCPURAM` (`tools_deploy.go:520`); SSD 11,42 и FIP 149,33 — по ответам API; `pricing.json` переопределяет `share_hourly` только при обеих ставках > 0. Живой сверки карты в job `contract` нет — строка плана открыта (FB170-7).
- **Диск по API** (`deployplan/price.go:71`): нулевой ответ API — ошибка, не цена (`:90`); без ответа — карта `SourceDiskCard` и «цена API: не получена (причина)» (`create_preview.go:904`).
- **Стек D75** (`tools_stack.go:738` `stackCost`): планировщики `planProvision`/`planPgCreate` только читают (`Plan.Execute` не зовётся, `:752`); `stackKinds` на 13 инструментов шага с инвариантом; у `priced` — `monthly_rub` и `source`, у прочих — `note` и `tariff_url` (`:781`); `total_monthly_rub` — сумма `priced` (`:802`), нота «total of the priced resources only» при неоценённых; `s3_put` → `s3_object` `paid_unpriced`.
- **Полнота**: `AddPorts`/`PortsGroup` — один помощник на три превью (`engine/ports_preview.go:21`, `:41`), `warnings` при порте вне 80/443 из `0.0.0.0/0`; `ssh_reachable` и класс ошибки (`ssh_reach.go:21`, `:74`); `dns_servers: []` с предупреждением (`tools_subnet.go:213–217`); `quota.note` (`quota.go:49`); заполнителя `default (env/config or auto)` в коде нет. `bodyUnnamed` 40 → 40.
- **`provenance`** (`engine/provenance.go:62`): «yes» — запись с id и ВМ не старше записи; «no» — только `madeBeforeRecord` (`:105`: обе отметки есть и ВМ старше записи больше чем на 5 мин); «unknown» — вид не записывается (все `*_delete` через `deletePlan`), state или время ВМ не прочитаны. Ложное «no» у своей ВМ — FB170-1.

### 6. «Миграция» ↔ `DECISIONS.md` ↔ таблица `RELEASING.md` (1.7.0)

| Решение (PR) | Фрагмент `changelog.d`, «Миграция» | Строка 1.7.0 `RELEASING.md` | Условия 1–5 | Смена `reason`/кода объявлена |
|---|---|---|---|---|
| D69 (#745, точка бюджета) | `d69-call-budget.md` | 95 — есть | 1 — клиент рвал вызов через 60 с, агент терял ВМ и пароль (неверный результат, которого скрипт не видит; M2/M0 rc.3), отмена читалась как сбой сети (FB160-11); 2 — любой `deploy_apply` по MCP; 3 — одна точка `SetCallBudget`, CLI не меняется (H14); 4 — `status: creating` и `next_tool`, `wait: true` для прежнего ожидания; 5 — «Миграция», D69 с решением | да: ошибка/обрыв → 0 `creating`; `wait_seconds` > 45 → `USAGE` 1; `stack_status` > 20 → `USAGE`; `API_UNREACHABLE`/`VM_PROVISION_FAILED` при отмене → `INTERRUPTED`, код 130 |
| D69 (#752, `provision`) | `d69-provision.md` | 97 — есть | 1 — то же; повтор после обрыва — 409 → `USAGE`, ВМ без FIP и портов; 2 — любой `provision` по MCP; 3 — подхват только записи `creating` этой папки; 4 — `next_args`; 5 — да | да: `USAGE` 1 → `ok` 0 только при своей записи; чужое имя — `USAGE` как раньше (`tools.go:2208`) |
| D69 (#760, `destroy`) | `d69-destroy.md` | 98 — есть | 1 — обрыв клиентом, `partial` 5 после 2 мин, второй DELETE → 409 (OPS-18); 2 — любой `destroy` по MCP; 3 — CLI `partial` остаётся, меняется только ВМ в `deleting`; 4 — `next_tool: destroy`; 5 — да | да: MCP 5 → 0 `deleting`; CLI `evo vm delete` ВМ в `deleting` 5 → 0 |
| D69 (#769, `deploy`, вариант C) | `d69-deploy.md` | 99 — есть (слова «DECISIONS» в основании нет, «вариант C D69» — формально) | 1 — обрыв сессии рвал `apt`/`compose up`; 2 — любой `deploy` по MCP; 3 — этап на ВМ, коды CLI прежние; 4 — `next_tool: deploy`; 5 — да, «Известные ограничения» — `KillUserProcesses` | да: ошибка/обрыв → 0 `deploying`; CLI `SSH_TIMEOUT` на потерянном этапе — новый случай прежнего кода |
| D69 (#778, k8s) | `d69-k8s.md` | 100 — есть | 1 — обрыв через 60 с, неизвестно, что ушло в кластер; 2 — любой `k8s_helm install` по MCP; 3 — только MCP, CLI `--wait --timeout 8m` остаётся; 4 — `deploying`/`stopped_at_limit`, `--timeout` < 40 с; 5 — да | да: `API_UNREACHABLE` → 0 `stopped_at_limit` |
| D69 (#781, диск) | `d69-disk.md` | 101 — есть | 1 — обрыв через 60 с, диск без привязки; 2 — любой `disk_create attach_to` по MCP; 3 — `callBound` одна точка; 4 — `next_tool: disk_attach`; 5 — да | да: `USAGE` 1 через 30 с (D53) → 0 с состоянием по MCP |
| D69 (#759, стек) | `d69-stack.md` — только «Изменено»/«Исправлено» | строки нет | D69 называет `stack_apply` в перечне PR; `stack_apply` по MCP возвращает шаг ВМ `pending`, где в 1.6 ждал её (`tools_stack.go:507–517`) — тот же класс, что строки 95–101 | не объявлено пунктом «Миграции» — FB170-5 |
| #747 (`destroy` без цели), без D | `destroy-names-target.md` | 96 — есть | 1 — агент удалял развёртывание, которого не видел (дыра в согласии, G2-3); 2 — любой `evo mcp` из папки с записью; 3 — только «подтверждённый вызов без терминала», превью и `evo destroy` прежние (H15); 4 — `vm_id` из превью; 5 — «Миграция» есть, записи D нет (решение владельца 31.10 в плане) | да: удаление 0 → `USAGE` 1; ответ по `vm_id` — `status: ok`, `deleted_vm` вместо `log` |
| D73 (#788) | `d73-own-vm.md` | 102 — есть | 1 — присвоение чужой ВМ, затем `destroy` (G2-1); 2 — любое совпадение имени в проекте; 3 — одна точка `provision.MadeSince`, 5 мин запаса; 4 — другое `name`; 5 — да, D73 с решением | да: `deploy_apply` 0 → `USAGE` 1; `provision`/`deploy_plan` 3 → 1 |
| D73 (#798, `destroy` старой записи) | тот же фрагмент, 4-й пункт | 103 — есть | 1 — удаление ВМ человека, присвоенной до 1.7; 2 — старые state; 3 — отказ только при ВМ старше записи или непрочитанной; 4 — удалить самому, запись уйдёт `already_deleted`; 5 — да | да: удаление 0 → `USAGE` 1; нечитаемая ВМ — код ошибки чтения до вопроса |
| #776 (s3 на `gated`), без D | `s3-gated.md` | 104 — есть | 1 — согласие на заведомо невыполнимый вызов (класс D35, F5a); 2 — любой `s3_put` без ключей; 3 — две команды на общий гейт; 4 — задать ключи; 5 — «Миграция» есть, своей записи D нет (класс D35, решение владельца 31.10, #717) | да: 3 → `CONFIG_INVALID` 2; `--dry-run` 0 → 2 |
| D74 (#801) | `delete-outcome.md` | 105 — есть | 1 — ошибка операции выглядела успехом, ресурс оплачивался (ось 3, run 151); 2 — 10 удалений у любого скрипта; 3 — одна точка `deleteOutcome`, `ca_delete` по исчезновению; 4 — `--wait=false`, ждать `deleted`; 5 — да, D74 с решением | да: `deleting` 0 → `deleted` 0 / `API_ERROR` 5; ключ `note` у `deleting` ушёл — во фрагменте не назван (FB170-5) |
| #806 (`elapsed_sec`), без D | `fable-1.6-lows.md` | 106 — есть | 1 — выдуманное значение (условие 1, второе; FB160-4); 2 — любой опрос завершённой операции без `updated_at`; 3 — одно поле, `elapsed_note`; 4 — поле необязательное, завершённость по `done`; 5 — «Миграция» есть, записи D нет (строка плана «находки Fable 1.6») | да: поле исчезает, коды и `reason` прежние |
| D68 (#831) | `d68-remove-skills.md` | 107 — есть | 1 — граница продукта по решениям владельца 30.10 и 03.11, не дефект согласия (спорно по букве условия 1; принято владельцем как решение D68); 2 — скопированные скиллы расходятся с 1.7 (`wait_seconds`, `next_tool`); 3 — удаление каталога и одной фразы `instructions`; 4 — `evo agent setup --skill`; 5 — да, D68 с решением | да: `instructions` при `initialize` без фразы о `skills/`; кодов и `reason` нет |
| D75 (#855) | `stack-plan-price.md` | 108 — есть | 1 — ложная цена, которой скрипт не видит (условие 1); 2 — любой `stack_plan`; 3 — один `stackCost`, коды и `reason` прежние; 4 — проверять наличие `monthly_rub`; 5 — да, D75 с решением | да: `monthly_rub` у строки без цены исчезает; `total_monthly_rub` — сумма `priced` |

Условие 5, третий пункт («упоминание в README „Что сейчас можно“ и в
анонсе») — шаг PR версии, как в 1.5 и 1.6 (FB170-6): README сейчас
«v1.6.0, 31.10.2026».

Необъявленных ломающих изменений уровня контракта нет. Проверено по
удалённым строкам diff с кодами причин (`Code*`, `usageT`, `errAnswer`,
`"status":` — 36 мест с контекстом) и по сгенерированным `docs/tools.md`,
`docs/commands.md`, `docs/mcp.md` обеих ревизий: инструментов 157 = 157,
имена те же; команд +1 (`evo configure s3`); удалённых флагов и аргументов
нет, добавлены `--cidr`, `--wait`, `--skill-only`, `--tenant-id`; 409 у
`provision` остаётся `USAGE` (`tools.go:2208`); метка правил `evo: port N
(vm <id>)` — старые правила без метки идут прежним путём через state,
объявлено в «Известных ограничениях» #819; Tab и ActiveHelp кодов не
меняют. Новые поля ответов — добавления: `next`, `next_tool`, `next_args`,
`vm_status`, `phase`, `phase_state`, `disk_state`, `way_out`,
`stopped_at_limit`, `passwords`, `price_classes`, `provenance`,
`ports_group`, `ports_source`, `ssh_reachable`, `ssh_probe_error`,
`quota.note`, `elapsed_note`, `floating_ip_monthly_rub`, `cost.source`,
`security_group`/`security_group_shared`/`ports_source` в `network`
`deploy_plan`, `labelled_rules_unchecked`, `_untrusted` у `s3_get`,
`k8s_helm`, `k8s_apply`. Значения прежних полей меняются только там, где
объявлено: `est_monthly_rub` (#846, #815, #812 — «Исправлено»),
`security_group_rules.closed` — `8080 (vm <id>)` (#819, «Изменено»),
`will_create` без заполнителя (#780), зона по умолчанию `evo init` (#780).
Мелочи оформления — FB170-5.

### 7. Четыре оси §6 на новых create и delete версии

**Новых инструментов create и delete в 1.7 нет**: реестр — 157
инструментов на `v1.6.0` и на кандидате, имена совпадают; новая команда
одна — локальная `evo configure s3` (в `localCommands` с причиной, тот же
класс, что `evo configure`). Оси проверены на изменённых:

| Поверхность | Ось | Проверено | Итог |
|---|---|---|---|
| `provision` под пределом (#752, #759) | 1, 2, 3 | D73 до вопроса (раздел 1); `creating` с `vm_status`, `next_tool`, пароль один раз; запись `creating` сразу после POST, повтор подхватывает только её, второй ВМ и второго FIP нет (раздел 4) | закрыт |
| `deploy`, вариант C (#769, #777) | 1, 2, 3 | проба TCP/22 до вопроса (#826); `deploying` с `phase`, `phase_state`, `output`; этап не запускается дважды, `lost` назван; CLI — строки лога этапа | закрыт по миру; живой образ (`KillUserProcesses`) — cloud-e2e `deploy` rc.1, «Известные ограничения» |
| `deploy_apply` (#745, #788) | 1, 3 | чужая ВМ — `ForeignVMError` до записи; `creating` хранит план и пароль; повтор по `plan_id` | закрыт; FB170-1 — запись датируется концом ожидания |
| `disk_create` с `attach_to`, `disk_attach` (#781) | 1, 2, 3 | `creating` с `disk_id`, `next_tool: disk_attach` и `confirmed` на ту же привязку; переходное состояние — состояние и тот же вызов; диск перечитывается после привязки (Ц-14) | закрыт |
| `destroy` (#747, #760, #798, #819, #838) | 1, 3, 4 | цель обязательна без терминала (H15); ВМ в `deleting` — без второго DELETE, `way_out`; пропавшая — `already_deleted` с уборкой; правила по метке из любой папки; `provenance` в превью | закрыт; FB170-1 — ложное «no» у своей ВМ |
| Удаления D74 (#801, #805) | 3 | «удалено» значит `done` операции; `error` — отказ с текстом платформы и `operation_id`; `ca_delete` ждёт исчезновения; `stack_destroy` — `failed`, а не `deleting` | закрыт |
| `s3_put`, `s3_bucket_create` (#776) | 1 | без ключей — отказ до превью; с ключами — превью, класс `zero_at_create` у бакета по тарифу | закрыт |
| `k8s_helm`, `k8s_kubectl` (#778) | 2 | `deploying` → `action: status`; `stopped_at_limit` с выводом и следующим шагом; чтения повторяются, изменения — нет | закрыт по миру; живой helm без `--wait` — cloud-e2e `k8s-smoke` rc.1 |
| `stack_plan` (#855, D75) | 4 | цена из превью ресурсов, без «0 ₽» без тарифа; планировщики не пишут | закрыт; класс-инвариант на мире — строка плана «Процессу» (П4) |
| `subnet_create` (#818), `provision`/`deploy_plan`/`deploy` (#811) | 4 | `dns_servers: []` с предупреждением; группа, «общая группа зоны», источник, `cidr`, `warnings` | закрыт |

### 8. Сверка охвата тестов с поверхностью продукта

Сверка «Процесса · 1.7» — PR #860, `docs/internal/audits/coverage-1.7.md`
(кандидат `9786b913`): пробелы П1–П4, блокеров нет, П2 и П3 — бэклог 1.8,
П4 — строка плана. Оценил независимо, своим обходом поверхностей (таблица
«Проверки», строка «Инварианты»): выводы #860 держатся — каждая новая
поверхность 1.7 имеет инвариант класса (бюджет D69 — обход реестра на
мире медленного облака; D74 и P6 — обход `deleteTargets`; `_untrusted` —
«предупреждение ⇔ маркер»; подсказки — AST-скан; `tools/list` —
бюджет на инструмент; транспорт, принтеры, `exec` — сканы кода; Tab —
обход дерева команд; классы цены — обход изменяющих на мире паритета)
или точечный тест с открытой строкой плана или бэклога. Согласен с П1
(живой образ — только cloud-e2e), П2 (четыре пути присвоения без общего
обхода; добавлю: `deploy_apply` вне класса `TestCreate_NeverTakesWhatWasThereBefore`,
его держит только `own_test.go`), П3 (`provenance` — три места вызова),
П4 (`stack_plan` вне `TestContract_EveryPreviewPricedOrListed`). Дополняю
два пробела, которых в #860 нет: инвариант подсказок удаления не читает
тела `switch` и склейки строк (FB170-3) и строка `sideRecordMisses` со
сроком «Closes by 1.7» при зачёркнутой строке плана P5 (FB170-6).
Перечни пропусков — 40, 270 строк, просроченных 0.

## Находки

### FB170-1 (средняя; после тега rc.1, рекомендую до стабильного тега; код `internal/tools/tools_deploy.go:248–268`, `internal/engine/own_record.go:93–102`, `internal/engine/provenance.go:105–107`, `internal/provision/findvm.go:23, :50`) — запись `deploy_apply` датируется концом ожидания, и D73 отказывает своей ВМ

`recordDeployment` пишет запись с `CreatedAt: now` (`tools_deploy.go:262`)
только после возврата `ApplyWithTimeout`, то есть после ожидания
`running` (до 25 мин, `DefaultTimeout`), floating IP и порта 22 (до 120 с,
`SSHPortWait`); ранней записи `creating` на этом пути нет — она есть
только в ветке ошибки или обрыва (`:357`). Правило D73 для `destroy`
сравнивает `created_time` ВМ с `CreatedAt` записи с запасом 5 мин
(`madeBeforeRecord`, `MadeSince`): ВМ, которая шла от POST до SSH дольше
пяти минут, старше своей записи, и `destroy` (без цели, по `vm_id`/`name`,
`evo destroy`, шаг `stack_destroy`) отказывает `USAGE` с текстом «evo до
1.7 взял её по имени, а не создал», а превью удаления говорит «создан evo:
нет». Сценарий: `evo deploy apply` или MCP `deploy_apply` с `wait: true`,
облако поднимает ВМ 6 мин → `destroy` своей ВМ — отказ с ложной
причиной; агент без присмотра не убирает своё (ось 3), человек читает
неверный факт. По MCP с пределом 45 с путь другой: запись `creating`
ставится в ветке обрыва (`:357`), и повтор сохраняет её `CreatedAt`
(`:268`) — там дефекта нет. Деньги и чужие ресурсы не теряются — сторона
осторожная. Как часто ВМ Cloud.ru идёт к SSH дольше 5 мин — гипотеза,
двумя прогонами не проверена; cloud-e2e rc.1 покажет только при
медленной ВМ. Предложение («разработка»): писать `CreatedAt` записи из
`plan.CreatedAt` (та же точка, по которой `ownVM` принял ВМ, `apply.go:520`)
или ставить запись `creating` сразу после `CreateVM`, как `provision`
(`tools.go:2233`) и `deploy` (`engine/deploy.go:776–794`). Инвариант
(«Процесс»): мир `deploy_apply` → `destroy`, где фейковое облако отдаёт
`created_time` на 6 мин раньше момента записи (через `internal/clock`),
ожидание — удаление и `provenance: yes`. Ускользнул бы: тесты D73 подают
ВМ с временем около записи. Смежное (из того же пути, не новое в 1.7):
жёсткий останов процесса между POST и возвратом оставляет ВМ без записи;
повтор по `plan_id` подхватывает её через `MadeSince`, второй ВМ нет.

### FB170-2 (средняя; после тега rc.1, рекомендую до стабильного тега; код `internal/engine/deploy.go:736–756`) — `deploy` с именем чужой ВМ спрашивает согласие, получает 409 и подсказывает удалить ту ВМ

`deploy` называет ВМ `evo-<project_name>`; её имя занято ВМ не из state
папки — превью этого не проверяет (`vmNameFree` зовут только `provision`,
`tools.go:1895`, и `deploy_plan`, `tools_deploy.go:157`), вопрос о
согласии задаётся, после «да» Cloud.ru отвечает 409, и ответ —
`CONFIG_INVALID` с подсказкой «Check it with 'evo vm get'. If it is not
this directory's deployment, delete it ('evo vm delete <имя>') or deploy
from a directory with another name» (`:753–754`, по-русски «удалите её»).
Это ровно случай, который план 1.7 (блок D, #829) назвал худшим у
`provision`: подсказка удалить чужую ВМ, — и ось 1 (согласие на вызов,
который не пройдёт), закрытая для `provision` и `deploy_plan` в D73, но
не для `deploy`. Подсказка одинакова в CLI и в ответе MCP `deploy`
(`clerrors.Wrap` с подсказкой → `errAnswer`). Текст не из 1.7 (в дереве
с 1.4.0, #427); инвариант #829 его не видит, потому что ищет имена инструментов
реестра (`destroy`, `*_delete`), а не команды CLI (`evo vm delete`).
Сценарий: агент без присмотра по `deploy` в папке проекта, чьё имя
совпадает с ВМ человека → согласие, 409, подсказка удалить ВМ человека;
модель с терминалом тем же ключом её выполнит. Предложение
(«разработка»): `vmNameFree` в превью `deploy` для новой ВМ (отказ до
вопроса, `USAGE` с `existing_id`, как D73 — ломающее того же класса, что
строка 102 RELEASING, D73 дополнить) и текст подсказки «другое имя
(`project_name` в `.evo.yaml`)»; решение владельца — нужен ли отказ до
вопроса у `deploy` или только текст. Инвариант («Процесс»): вариант мира
согласия «ВМ с именем `evo-<project>` не из state» для `deploy`, как у
`provision`; скан #829 — ещё и по именам команд CLI удаления.

### FB170-3 (низкая; после тега; процесс; тест `internal/testrules/deletehint_test.go:118`; тексты `internal/tools/tools.go:2224–2225, :2256–2257`, `internal/tools/tools_deploy.go:375–377, :423`, `internal/deployplan/apply.go:152, :232–234, :258–260`) — инвариант #829 пропускает тела `switch` и склейки строк

`TestHints_DeleteIsThePersonsDecision` обходит AST и на `*ast.CaseClause`,
`*ast.BinaryExpr`, `*ast.IndexExpr` возвращает `false` — чтобы не читать
`case "destroy":` и `x == "destroy"`, — но тем самым не заходит ни в одно
тело `case`/`default` и ни в одну склейку `"…" + "…"`. Семь текстов, которые
предлагают удаление следующим шагом без слов «ask the person», проходят
мимо: `provision` после сбоя или отмены вызова («check it with vm_get,
delete it with destroy (vm_id …)», `tools.go:2225`, `:2257` — склейки),
`deploy_apply` при остановленной ВМ («…or delete it with destroy (vm_id
…)», `tools_deploy.go:423` — ветка `default`), `deploy_apply` при
несохранённом state («or delete the VM with destroy», `:375–377`),
`deployplan/apply.go:152` (`Note` присвоенной остановленной ВМ), `:234`,
`:260` (обрыв и отмена). Все семь — о ВМ, которую создал этот же вызов
или план, то есть ближе к «откат своей сорвавшейся операции», чем к P1;
продуктовая сторона низкая. Сценарий: обрыв `provision` после POST → агент
читает «delete it with destroy» как следующий шаг и удаляет ВМ, за
которую заплатил, вместо повтора. Предложение («разработка», тексты):
формулировка «решение человека», как у соседей (`tools.go:2348`,
`:2458`); («Процесс»): в скане не выходить из `CaseClause` и
`BinaryExpr`, а пропускать только `CaseClause.List` и операнды сравнений —
так `deleteHintMisses` сначала вырастет на семь строк, затем закроется.
Ускользнул бы: инвариант есть, но не дошёл по форме AST.

### FB170-4 (низкая; после тега; код `internal/cli/tools.go:348, :632–641`) — `evo tools call --wait=false` отвергается у инструментов со своим `wait`

Флаг `--wait` у `evo tools call` — свой, булев: «ждать `wait_seconds`»;
`applyWait` отказывает `USAGE` «<tool> does not support --wait» всякому
инструменту без `wait_seconds` в схеме. `provision`, `deploy_apply` и 11
удалений D74 принимают аргумент схемы `wait`, и `evo vm create
--wait=false` работает, а `evo tools call provision --wait=false` — нет;
обход — `--arg wait=false`, в `docs/commands.md` не назван. Паритет
`evo tools call` ↔ инструмент (D14) здесь расходится на имени флага.
Предложение («разработка»): `--wait` у `tools call` передаёт `wait` схеме,
когда она его объявляет, и `wait_seconds` — когда объявляет его; или
подсказка в отказе называет `--arg wait=false`. Инвариант («Процесс»):
«каждый аргумент схемы доступен `evo tools call` без столкновения с его
флагами».

### FB170-5 (низкая; до тега — оформление, PR версии; `docs/RELEASING.md:96, :104, :106`, `changelog.d/d69-stack.md`, `changelog.d/delete-outcome.md`, `docs/mcp.md:1003`) — мелочи оформления ломающих изменений

- Три строки 1.7.0 без записи D — #747 (96), #776 (104), #806 (106) — при
  условии 5 «запись в `DECISIONS.md` с решением владельца». Решения
  владельца у всех трёх есть (план 1.7: 31.10, #717; класс D35; FB160-4),
  прецеденты без D — строки 1.1.0 и 1.3.0; в 1.6 все десять имели D. Риск
  — только сверка «Миграция ↔ D» будущими аудитами. Решает «1.7 — релиз»
  (номера D — правило 6): резерв D или оговорка в строке «класс D35, без
  своего D».
- `stack_apply`/`stack_status` по MCP под пределом (#759): шаг ВМ остаётся
  `pending` там, где в 1.6 `stack_apply` ждал ВМ в вызове
  (`tools_stack.go:507–517`) — тот же класс, что строки 95–101, но во
  фрагменте это «Изменено», и строки в таблице RELEASING нет; D69 называет
  `stack_apply` в перечне, условие 5 по D выполнено. Пункт «Миграции» и
  строка таблицы — при свёртке.
- D74: ключ `note` («Async — poll pg_operation…») у ответа `deleting` ушёл
  (удалённая строка `tools_pg.go`; `deleting()` в
  `delete_outcome.go:76–82` отдаёт `status`, `operation_id`, `next`,
  `next_tool`, `next_args`); `delete-outcome.md` перечисляет новые поля, но
  не говорит, что `note` исчез. Текст, не значение для скрипта — слово во
  фрагмент.
- `docs/mcp.md` H14: перечень «пока предел держат» без десяти удалений D74
  и `ca_delete`; D74 в `docs/mcp.md` не упомянут. Строка 99 RELEASING —
  «вариант C D69» без слова DECISIONS (как в других строках).

### FB170-6 (до тега; шаги PR версии и стабильного тега, не дефекты)

- Свёртка 37 фрагментов (`python3 scripts/changelog.py fold`, 13 с
  «Миграцией»); README «Что сейчас можно» — сейчас «v1.6.0, 31.10.2026»;
  анонс (условие 5).
- Резервы **D55 «(1.7)»** и **D56 «(1.7)»** в `DECISIONS.md`:
  `TestDocs_DecisionReserves` (`internal/cli/docs_decisions_test.go:92–112`)
  роняет сборку, когда в `CHANGELOG.md` появляется раздел `## [1.7.x]`;
  rc.1 не задевает (при rc раздел версии не создаётся), PR версии перед
  стабильным `v1.7.0` — задевает: перенести на 1.8 или снять до него
  (хозяин — «1.7 — релиз»). D72 — «(1.8)», в порядке.
- `sideRecordMisses` (`internal/cli/preview_side_test.go:61–65`) — строка
  `provision: a rule of security group` со сроком «Closes by 1.7», при
  зачёркнутой строке плана P5 (#811): превью `provision` называет группу
  именем (`ports_group`, `engine/ports_preview.go:48`), инвариант ждёт
  `security_group.id` (`:33–36`). После выпуска 1.7.0 строгий
  `weekly-misses.yml` (`EVO_MISS_DEADLINE=1`) краснеет на ней. Закрыть
  правкой превью (`security_group` с `id`, как у `deploy_plan`) или
  перенести срок с причиной — «разработка»/«Процесс».
- Бэклог процесса `process-handoff.md`: строка 555 «инвариант D69»
  закрыта по факту (`TestMCP_NoCallOutlivesTheBudget`), не помечена
  «сделано»; 577 «Release immutability и goreleaser — до rc.1» — открыта,
  ждёт владельца (`state/release.md`).
- cloud-e2e кандидата (`deploy` с обрывом посреди этапа — условие к PR 4
  D69; `k8s-smoke` с `k8s_helm` по MCP — условие к PR 5), CI и Qwen — по
  правилу, «1.7 — релиз».

### FB170-7 (низкая; после тега; код `internal/pricing/pricing.go:37, :180–189`; `.github/workflows/ci.yml:334`, `api-probe.yml:1042`) — месяц карты и живая сверка

Compute считается по 730 ч, а ставки SSD (11,42 ₽/ГБ) и FIP (149,33 ₽) взяты
из ответов API с месяцем 732 ч: в одной оценке два месяца, расхождение
< 0,3 % — косметика. Живой сверки карты ВМ и диска (#846, #815, #812) в job
`contract` нет: `pricecard` — только `go vet`/`go test` в CI и ручная
проба P49; строки плана (#812: «живая сверка — Процессу») и бэклога (551,
565, 566) открыты — карта с датой 04.11 устареет молча. По правилу §0 п. 3
охват есть (строка плана); дефекта нет.

### FB170-8 (низкая, гипотеза; после тега; код `internal/engine/price_class.go:180–195`, `internal/tools/tools_stack.go:781`) — ответ API с нераспознанной разбивкой

Если `price-calculation` отвечает с `Breakdown.Period == "unknown"`,
`share()` отдаёт ВМ весь `total`, диску и FIP — 0: их строки — `paid_unpriced`
«в цене нет его доли», хотя итог ВМ их включает; в стеке они идут в
`unpriced`, и нота говорит «итог только по ресурсам с ценой», хотя итог
полный; инвариант `costPart` при этом ждёт `compute_monthly_rub > 0`.
Вживую такой ответ не наблюдался (пробы P49 — 2 из 2 с разбивкой); мир
паритета его не подаёт.

### FB170-9 (низкая, гипотеза; после тега; код `internal/tools/provision_adopt.go:35–38`) — повтор `provision` с другим flavor подхватит запись

Запись `creating` подбирается по имени, проекту и зоне, без сверки flavor и
образа: `provision` с тем же именем, но другим `flavor`, после обрыва
молча подхватит ВМ прежнего размера. `next_args` повторяют исходные
аргументы, так что по `next_tool` агент сюда не попадёт; руками —
возможно. Инвариант: мир «повтор с другим flavor» → отказ или явная
строка `adopts` с flavor записи.

## Гипотезы (не проверял вживую)

1. FB170-1 — как часто ВМ Cloud.ru идёт от POST до порта 22 дольше 5 мин;
   cloud-e2e rc.1 покажет только при медленной ВМ.
2. Вариант C — `KillUserProcesses` в образах Cloud.ru (гипотеза
   «Известных ограничений» `d69-deploy.md`); проверяет шаг cloud-e2e
   `deploy` с обрывом посреди этапа.
3. `k8s_helm` без `--wait`: что `helm status` даёт честное состояние и свой
   `--timeout` укладывается в предел — cloud-e2e `k8s-smoke` rc.1.
4. Паритет `INTERRUPTED` держится одной точкой `errReason` и именованными
   тестами; обхода «каждый ждущий инструмент при отмене на обеих
   поверхностях» нет (бэклог процесса 563); обработчик, который завернёт
   `ctx.Err()` через `%v` мимо `errAnswer`, как `provision` до #745,
   инвариантом не ловится.

## Сверка охвата (итог)

Сверка «Процесса» (#860, `coverage-1.7.md`) на кандидате `9786b913`
держится: каждая новая поверхность 1.7 — предел D69 и этапы на ВМ,
`INTERRUPTED`, H15, D73, D74, P3, P5, P6, классы цены и D75, карта цен,
тексты для модели, D68 и `tools/list`, `_untrusted`, транспорт, принтеры,
`exec`, Tab и `install.sh`, `configure s3`, `doctor`, `init`, `elapsed_sec`
— имеет инвариант класса или точечный тест с открытой строкой плана или
бэклога (раздел 8); перечни пропусков — 40, 270 строк, просроченных 0;
пробелы П1–П4 — бэклог с версией-кандидатом, кандидат не блокируют.
Новые пробелы — FB170-3 (форма AST в скане подсказок), FB170-2 (скан не
читает команды CLI удаления; мир «чужое имя» у `deploy`), FB170-1 (мир
времени записи), FB170-4 (аргументы схемы в `evo tools call`),
FB170-6 (`sideRecordMisses`).

## Что делать

- **До тега rc.1 («релиз»):** PR версии — FB170-6 (свёртка, README, анонс)
  и оформление FB170-5 (пункт «Миграции» и строка для `stack_apply`,
  слово о `note`, H14); cloud-e2e кандидата по «да» владельца, CI и Qwen.
- **До стабильного `v1.7.0`:** резервы D55 и D56 (FB170-6); FB170-1 и
  FB170-2 — рекомендация, решает владелец (обе — агент без присмотра:
  своя ВМ не убирается, чужую предлагают удалить); `sideRecordMisses`.
- **После тега («разработка», 1.7.x или 1.8):** FB170-3 (семь текстов),
  FB170-4 (`--wait` у `tools call`), FB170-7 (месяц карты), FB170-9 (flavor
  при подхвате).
- **После тега («Процесс»):** инварианты к FB170-1 (мир времени записи),
  FB170-2 (мир «чужое имя» у `deploy`, скан по командам CLI), FB170-3
  (форма AST), FB170-4 (аргументы схемы), FB170-8 (мир ответа без
  разбивки); живая сверка карты в `contract` (FB170-7); бэклог 555 —
  пометить.
