# Полный аудит 1.3 (Fable, 23.10.2026)

Чек-лист `CLAUDE.md` §5: полный аудит перед тегом minor, по diff
`v1.2.3..main` (head `eef78db`, с #265). 45 коммитов, из них 17
продуктовых; 252 файла, +79 289/−1 009, из них код продукта — 56 файлов,
+4 531/−588 (остальное — тесты, документы и 28 файлов спецификаций в
каталоге `--help/`, см. FB13-1). Пока отчёт писался, `main` ушёл на
`622070d` (#268): только удаление `--help/`, кода не касается — вывод тот
же. Только чтение; в облако не ходил
(страницы тарифов — публичный сайт). Пункты rc.1 A1–A12 — по
`docs/internal/release-plan.md`, решения D22–D31 — по
`docs/internal/standalone/DECISIONS.md`.

**Вердикт: тег `1.3.0-rc.1` на `622070d` или позднее ставить можно.**
Дефектов согласия, секретов, паритета, идемпотентности и незаписанных
ломающих изменений в дельте нет. Условия: (1) каталог `--help/` (FB13-1:
28 спецификаций Cloud.ru, 63 тыс. строк, попали в #265 случайно;
содержимое публичное) из `main` убран #268 — выполнено; защита от
повторения — за «Процессом · 1.3» до rc.1 (решение владельца 23.10);
(2) до `1.3.0` — две неточности в текстах «Миграции» (FB13-2);
(3) гипотезы D28 и D26 — живым cloud-e2e версии, как в плане. Остальные
находки — 1.4.

Находок: блокирует rc.1 — 0; до 1.3.0 — 2 (FB13-1, FB13-2); 1.4 — 5
(FB13-3…FB13-7).

## Проверки

| Проверка (коммит `eef78db`, `main` до #268; `622070d` меняет только `--help/**`) | Результат |
|---|---|
| CI на `eef78db` (head `main`) | `ci` run 39977745011 — success: test ×3 ОС (`go test -race -count=1 -shuffle=on`), docker, на ubuntu — gofmt, shellcheck, фрагменты CHANGELOG, `redact.sed`, обёртка evo, classify.sh, govulncheck, staticcheck, actionlint, deadcode; `classify` на push не нужен (skipped). `api-contract` run 39977744822 — success |
| Локально на `eef78db` | gofmt чисто; `go vet ./...` чисто (linux и `GOOS=windows`); staticcheck чисто; `go test -race -count=1 ./...` — 43 пакета, 0 падений (`internal/tools` 51 с, `internal/cli` 49 с, `internal/mcp` 8 с); `python3 scripts/changelog.py check` — ok |
| Утечки в diff (добавленные строки, 13 161) | UUID — только фейков (`0000a1a1-0000-4000-8000-…`, `00000000-0000-4000-8000-…`, `0b5e9a6c-1111-2222-3333-…`); IP — RFC 5737 (`203.0.113.*`, `198.51.100.*`, `192.0.2.*`), `127.0.0.1`, `192.168.10.0`; хосты — публичные `*.api.cloud.ru`, `s3.cloud.ru`, `console.cloud.ru`; PEM — фейковый ключ мира паритета (`ZmFrZS1rZXk=` — «fake-key»); почты, ключей, внутренних хостов нет. Отдельно — спецификации `--help/` (FB13-1): публичные OpenAPI Cloud.ru, внутренних сведений нет |
| Страницы тарифов (A5), HEAD 23.10 | `evolution-dns`, `object-storage`, `managed-postgresql`, `load-balancer`, `index`, `evolution-compute` — все 200 без редиректа (`tariffsBase` — `create_preview.go:146`) |
| Тест, падающий без правки (по описаниям PR, все есть в дереве) | #254 `TestProgress_Stdio`, `TestProgress_HTTP`, `TestVMCreate_MCPProgressIsTheStderrStages`, `TestApply_ShowsItsStages`; #258 `TestK8sCreate_PreviewResolvesThePlan`, `TestK8s_ExplicitFlavorRefusedForWhatItIs`, `TestPickK8sFlavor`, `TestArCreate_TakenNameBeforeConsent`; #261 `TestCreateNames_SpacesRefusedBeforeTheCloud`; #264 `TestProjectBound_VMGetByNameInVMID`, `TestProjectBound_FactoryErrorIsItsOwn`, `TestDeploy_DryRunWithoutKey`, `TestCertImport_RawPEM`, `TestReleaseFloatingIP_RefusedBeforeConsent`; #265 `TestApply_LostFIPOrphanByNameNotTaken`, `TestApply_LostFIPOwnTakenInEveryForm`, `TestSnatCreate_PreviewNamesTheZone`, `TestStackPlan_ConsentInNext`; #244 `TestAlreadyDeleted_*`; #236 `TestDeterminism_ListOrderDoesNotChangeTheAnswer` |

## Чек-лист

### 1. Согласие
- **A2 + A12, `k8s_create` и `k8s_add_nodepool`** (`tools_k8s.go:301–347`,
  `:487–590`). Порядок у `k8s_create`: `k8sArgsError` → список кластеров и
  `takenName` → `resolveK8sPlan` → превью → `confirmed`. План разрешается до
  гейта, превью несёт `resolved()` (flavor с vCPU и RAM, версия, зона,
  подсеть VIP, `master_count`), `k8sPreview` подставляет разрешённые
  значения вместо аргументов того же имени (`:862–885`). У
  `k8s_add_nodepool`: список групп узлов и `takenName` → каталог →
  `pickK8sFlavor` → зона кластера (`clusterZoneID`) → подсеть узлов →
  группа безопасности → превью (`:575`) → создание. Отказы разрешения —
  `clierrors` с `USAGE` (`k8sZone`: зона не найдена, не предлагается для
  mk8s) или `CONFIG_INVALID` (подсеть другой зоны, нет SG в зоне, кластер
  без зоны) — до вопроса. Один выбор flavor для мастеров и узлов —
  `pickK8sFlavor` (`:153–184`): явное имя как есть, GPU и «нет в каталоге»
  — свои `USAGE`, из равных — `provision.CompareByNameID` (D27).
- **A2, `ar_create`** (`tools_ar.go:75–98`): клиент → `ListRegistries` →
  `takenName` → `nameTakenResult` — до чтения `confirmed`. Сбой списка —
  отказ, не создание «на авось» (как D20, D21, D23).
- **A6, `release_floating_ip`** (`tools_deploy.go:648–740`): адрес
  читается до гейта (`GetFloatingIP` по id или список по `ip_address`),
  отказ для адреса шлюза SNAT (`:708`) и для `in_use` (`:716`) — `USAGE`,
  `status: refused`, до вопроса о согласии (`:723`). Адрес, которого нет, —
  `already_deleted` (D26) на превью и после согласия.
- **D26, превью delete читает облако.** `deletePreview`
  (`delete_preview.go:21–25`) — один GET, 404 → `deleteFailed` →
  `already_deleted` без вопроса; у инструментов без `deletePreview`
  добавлен GET или список перед вопросом: `disk_delete`
  (`tools_disk.go:380–384`), `image_delete`, `secret_delete`,
  `cert_delete`, `s3_bucket_delete`, `s3_rm` (объект ищется до и после
  согласия, `tools_s3.go:156–162`), `vpc_route_delete`, `pg_db_delete`,
  `pg_user_delete` (родитель — до цели, `tools_pg.go:588`, `:663`),
  `dns_zone_delete` и `dns_record_delete` с `type`. На превью без ключа —
  `AUTH_REQUIRED`, без проекта — `CONFIG_INVALID`: записано в «Миграции»
  D26.
- **D29–D31.** D29 — выше. D30 (`create_names.go:73–88`): `plainName` в
  обёртке реестра до обработчика, с `confirmed` и без; порядок обёрток
  `checkedArgs → checkedDelete → checkedCreateName → markUntrusted →
  requireProject → обработчик` (`registry.go:129`) — отказ имени раньше
  любого запроса. D31: `evo deploy --dry-run` без ключа —
  `engine/deploy.go:152` до обмена ключа на токен, превью с `missing`.
- Шаги стека вызывают те же обёрнутые обработчики (`tools_stack.go:35–40`,
  `s.handlers[name]`): `plainName`, `checkedDelete` и `already_deleted`
  действуют и внутри `stack_apply`/`stack_destroy`.
- Пометки для агента (A9): `sideEffectTools` (`registry.go:440`) →
  `annotations` (`mcp/server.go:101–114`): `stack_status` и
  `k8s_kubeconfig` без `readOnlyHint` и `idempotentHint`,
  `destructiveHint: false`; `stack_plan` — `next`, `message` — копия до
  2.0 (`tools_stack.go:584–591`).

### 2. Секреты
- **A1, `notifications/progress`** (#254). Строки прогресса — те же, что
  CLI печатает в stderr: `progressOf` отдаёт `output.Printer` поверх
  `lineWriter` (`caller.go:109–150`), строки `provision` (`tools.go:1785,
  2078, 2132, 2156, 2233`) и `deploy_apply` (`deployplan/apply.go`:
  «Creating VM», «Request accepted», «VM is running», «Public IP»,
  «Waiting for SSH», порты) не несут пароля: `vmReadyText`
  (`tools.go:2275`) — имя, зона, IP, команда SSH; `passwordOnce` — только в
  ответе. Инвариант: `mcp/progress_test.go:48–66` — ни одно уведомление
  не содержит сгенерированный пароль, по stdio и по HTTP. Шаги стека —
  `stepOf` (`caller.go:123`): прогресса нет ни в CLI, ни по MCP. После
  отмены уведомлений нет (`server.go:627`).
- **`_untrusted`** (`untrusted.go:26–55`): список имён полей, добавляется к
  ответу после обработчика; значений не несёт. Описания `logs`, `ca_logs`,
  `s3_get`, `k8s_kubectl`, `k8s_helm` получают `untrustedOutputNote` из
  одной точки (`registry.go:113`). `quoteCloud` — имя из облака в одну
  строку в кавычках в превью удаления (`delete_preview.go:33`),
  `TestUntrustedNames_OneLineInText`.
- **H7.** Маска ответа по HTTP не менялась. Структурное замечание —
  FB13-4: уведомления прогресса по HTTP уходят мимо H7 (`http.go:384–392`
  маршалит `v` как есть). Сейчас строки — собственный текст evo без
  секретов (инвариант выше), дефекта нет.
- **Превью.** `TestContract_EveryPreviewPricedOrListed`
  (`cli/preview_price_test.go:45–80`): превью не несёт значения аргумента
  из `CLISpec.SecretFiles`; `secret_create` — только путь
  (`tools_secret.go:109–110`); `s3_put` — размер в байтах; `cert_import` —
  имя сертификата (`tools_certmanager.go:112–116`), PEM и ключ в превью не
  попадают, `pemArg` (`:146`) только кодирует.
- **Workflows (#253).** stderr evo в cloud-e2e и api-probe — через
  `build_evo.sh` (обёртка с `redact.sed`), `TestWorkflows_CloudE2EStderrRedacted`
  (`workflows_test.go:690`) роняет сборку на `go build ./cmd/evo` без
  обёртки; `evo_wrapper_test.sh` в CI.
- **`evo agent setup`** (#217, D22): запись клиента секретов не содержит
  (`agent_clients.go:240–262`): ключ Cloud.ru остаётся в профиле, токен HTTP
  — имя переменной (`${CLOUDRU_MCP_TOKEN}`, `bearer_token_env_var`), файл
  MCP — 0600. Замечание — FB13-6.
- **Права на файлы, которые пишет код 1.3** (ревью Qwen #267). Новые
  писатели дельты — только `agent setup`/`agent skill`: конфигурация
  клиента MCP — 0600 (`cli/agent.go:508`, «пользователь может вписать в неё
  токен», `:315–317`), правила агента (SKILL.md, `AGENTS.md`) — 0644
  (`:530`, секретов не содержат); существующий файл сохраняет свой режим
  (`:537–542`); запись — `config.WriteFileAtomic` (`:626`), как у профиля.
  Других `os.WriteFile`/`os.OpenFile` в изменённых продуктовых файлах нет
  (`git diff --name-only | xargs grep`), кроме `.gitignore` 0644 в
  `cli/main.go:533` без секретов. Писатели секретов и state дельта не
  трогает, их режимы прежние: профиль — 0600 (`config/profile.go:374, 536`),
  каталог — 0700 (`config.MkdirPrivate`), план `deploy_plan` — 0600
  (`deployplan/store.go:158`), state и его lock — `internal/state` вне
  diff. Нарушений нет.
- **FB121-7** (пароль ВМ шага `provision` в `stack_status`): `internal/stack`
  в diff не входит, шаги не шлют прогресс (`stepOf`), H7 без изменений —
  хуже не стало; перенос в 1.4 подтверждаю.

### 3. Паритет CLI ↔ MCP (D13/D14)
- Все правки — в обработчиках реестра и его обёртках; `CLISpec` получил
  только новые команды `agent setup`/`agent skill` (`localCommands` с
  причиной, #217). `--json` команды = JSON инструмента: `markUntrusted`,
  `checkedCreateName`, `checkedDelete` стоят внутри `r.handlers`, которыми
  пользуются обе поверхности; `TestParity_*`, `TestContract_*`,
  `TestReadOnly_*` зелёные (CI и локально).
- Различия поверхностей — закрытый перечень `HTTPRestrictions`
  (`mcp/http_policy.go:52–94`): новые H12 (`httpMaxBody`, `http.go:65`) и
  H13 (`withNotifier`, прогресс только в SSE). Строки H12 и H13 в
  `docs/mcp.md:941–942`; `http_policy_closed_test.go:204–209` роняет
  сборку на расхождение документа и перечня в обе стороны. Раздел
  «Прогресс долгих вызовов» `docs/mcp.md:255–277` — текст совпадает с
  кодом (поток stdio под мьютексом, SSE, без токена — ничего, после
  отмены — ничего, шаги стека — ничего).
- `evo vm delete <ref>`: позиционный `vm_id|name` делится по UUID
  (`cli/trees.go:505, 525–530`) — не-UUID уходит в `name`, `checkedDelete`
  (`uuid: true` у `destroy`) его не касается; фраза «Миграции» D26 «у
  `evo vm delete` не-UUID по-прежнему ищется как имя» верна.
- `access_denied` (A10): `errAnswer` (`tools.go:3253–3259`) — поля при 403
  любого сервиса, `reason` прежний; CLI — в `details`
  (`engine_commands.go:124–140`); инвариант в `TestContract_APIStatusReason`
  и `TestParity_ErrorReasonEqualsTool`.

### 4. Идемпотентность и деньги
- **D26, 404 на DELETE = `already_deleted`** (`delete_gone.go:69–105`).
  Классификация — та же `errReason`, что у ответа об ошибке: `NOT_FOUND`
  и `VM_NOT_FOUND`; 404 любого сервиса → `NOT_FOUND`
  (`errreason.go:45–50`: `apiStatus` или текст «not found»). Повторный
  compute DELETE после обрыва — через `httpretry` с `computeRetry`
  (`adapter.go:399`), 404 на повторе читается как «уже удалено» — тот же
  ответ. Форма id: `checkedDelete` (`:127–140`) — `USAGE` до облака у
  `destroy`, `disk_delete`, `image_delete`, `release_floating_ip`,
  `snat_delete`. Родитель — до цели (PG, VPC, бакет). Связанный сервер —
  `notInProject` у каждого delete, `TestAlreadyDeleted_BoundServerKeepsNotFound`.
  Остаточный риск — FB13-3.
- **A3, свой floating IP после потерянного ответа**
  (`deployplan/apply.go:398–452`). Порядок: адрес на ВМ
  (`AttachedFloatingIP`) → адрес из старого плана → создание → при ошибке
  или ответе без id — поиск по имени `FitName(<ВМ>, "-fip")`, берётся
  только адрес с `BoundTo(vmID) || BoundTo(ifaceID) || Pending()`
  (`:446`). `Pending` (`types.go:479–481`): `vm` и `interface` пусты и
  `state` ∈ {`creating`, `attaching`}. Повтор `deploy_apply` усыновляет ВМ
  и первым шагом берёт адрес, уже привязанный к ней, — второй адрес не
  создаётся, пока первый привязан. Тесты:
  `TestApply_LostFIPOwnTakenInEveryForm` (по vm, по interface, creating,
  attaching), `TestApply_LostFIPOrphanByNameNotTaken`. Оставшийся случай —
  FB13-5.
- **D25 `lostCreate`** (`namecheck.go:75–100`): ответ без id → `noID` →
  список → ресурс по имени — код 0 с id и `warning`, нет — `API_ERROR`;
  обрыв — прежняя ошибка. У `dns_record_create` ключ — `name + " " +
  тип` в нижнем регистре (`tools_dns.go:386–397`, `record_type` приводится
  к нижнему регистру на `:358`), у `vpc_route_create` — подсеть назначения
  (`tools_vpc.go:256`), у `secret_create` — путь, у `cert_issue_le` — id из
  списка.
- **D27** (`provision/order.go`): `CompareByNameID`, `CompareFlavorSize`,
  `PickDiskType` — одна точка для ВМ, `disk_create`, `deploy`, k8s;
  `TestDeterminism_ListOrderDoesNotChangeTheAnswer`.
- Повтор `k8s_create`/`k8s_add_nodepool`/`ar_create`/`vpc_create` с
  занятым именем — `USAGE` с `existing_id` до запроса на создание
  (`name_dup_test.go`, `k8s_consent_test.go:205`).

### 5. «Миграция» ↔ DECISIONS ↔ RELEASING
Ломающих изменений в дельте восемь, каждое записано трижды. Три фрагмента
`changelog.d/*` сверены с кодом построчно (ревью Qwen #267):
- `plain-names.md` (D30): 13 инструментов фрагмента — ровно те записи
  `createNames` без `rule` (`create_names.go:33–55`; у десяти остальных —
  своё правило сервиса, они верно названы «не изменилось»); код — `USAGE`
  из `plainName` (`argrules.go:218–236`: пустое после обрезки, пробел по
  краям, управляющий символ, больше 255 символов); «до вопроса о согласии
  и до запроса» — обёртка стоит до обработчика (`registry.go:129`), тест
  `TestCreateNames_SpacesRefusedBeforeTheCloud` считает запросы; у
  `dns_zone_create` — `name` и `domain` (`:40`). Верно.
- `honest-errors.md` (D31): (1) `evo deploy --dry-run` без ключа — код 0,
  `status: dry_run` с `missing`, без `--dry-run` — код 2
  (`TestDeploy_DryRunWithoutKey`, `engine/deploy.go:152`); (2) `vm_get` с
  именем в `vm_id` на сервере `--allow-project` находит ВМ, ошибка фабрики
  — как есть, пример `CONFIG_INVALID` (`TestProjectBound_VMGetByNameInVMID`,
  `TestProjectBound_FactoryErrorIsItsOwn`), чужая — `NOT_FOUND`
  (`TestProjectBound_ForeignResourceRefused`); (3) `release_floating_ip` —
  `USAGE`, `status: refused` до согласия для адреса SNAT и `in_use`
  (`TestReleaseFloatingIP_RefusedBeforeConsent`, `tools_deploy.go:708–723`).
  «Добавлено»: `access_denied: true`, `http_status: 403` у каждого
  инструмента при 403 (`contract_test.go:491`), в `details` CLI
  (`parity_json_test.go:530`); PEM `-----BEGIN` кодируется, Base64 как
  дано (`TestCertImport_RawPEM`). Верно.
- `k8s-ar-consent.md` (D29): верно, кроме кода у «кластер не найден» —
  FB13-2.
- `mcp-progress.md`, `preview-meta.md` — не «Миграция»; поведение сверено в
  разделах 1, 2 и 4.

Таблица решений:

| Решение | Фрагмент или раздел «Миграция» | Таблица 1.3.0 `RELEASING.md` |
|---|---|---|
| D23 `vpc_create` занятое имя | `CHANGELOG.md` [Не выпущено] | `:55` |
| D24 H12, 4 МБ по HTTP | там же | `:56` |
| D25 потерянный ответ, ответ без id | там же | `:57` |
| D28 вложенные объекты, занятое имя | там же | `:58` |
| D26 повторный delete | там же | `:59` |
| D29 `ar_create`, `k8s_create`, `k8s_add_nodepool` до согласия | `changelog.d/k8s-ar-consent.md` | `:60` |
| D30 `plainName` | `changelog.d/plain-names.md` | `:61` |
| D31 честный код | `changelog.d/honest-errors.md` | `:62` |

Строки D22–D31 в `DECISIONS.md` записаны, резервов нет;
`TestDocs_DecisionsCitedAreWritten` держит ссылки. Изменения кодов и
`reason`, которых нет в записях, не нашёл: `release_floating_ip` по
`ip_address` (было `USAGE`, см. FB13-2), остальные — в перечнях D26, D29,
D31. Не ломающее и записано как «Изменено»/«Добавлено»: аннотации A9,
один транспорт повторов (#224: 60 с на запрос со всеми повторами),
`_untrusted`, `preview` у девяти созданий, `next` у `stack_plan`,
`access_denied`, PEM у `cert_import`, прогресс. README «Что сейчас можно
(v1.2.3)» и строку ломающих обновляет PR версии (`RELEASING.md`, шаг 1).

### 6. Тесты-инварианты
- `createNames` (D30): `TestCreateNames_EveryCreateDeclared`
  (`create_names_test.go:29`) — каждый `createLike` инструмент в
  `createNames` или `namelessCreates` с причиной, аргументы — из схемы.
- `deleteTargets` (D26): `TestAlreadyDeleted_EveryDeleteDeclared`
  (`already_deleted_test.go:101`) — `deleteLike` (`*_delete`, `*_rm`,
  `destroy`, `release_floating_ip`) без объявления роняет сборку.
- `HTTPRestrictions` H13: `http_policy_closed_test.go:75–209` — перечень ↔
  `docs/mcp.md` ↔ вызовы `restrict` в коде.
- Мир паритета (#255): `TestParityWorld_UnknownRequestIsNamed`,
  `TestParityWorld_EmptyListsAreLists` (`parity_world_guard_test.go`);
  запрос без фикстуры роняет `TestContract_*`
  (`contract_preview_test.go:572`).
- `whyListed` (#256): `project_bound_test.go:541–563` — у каждого аргумента
  `whyListed` есть случай с чужим id в `listedCases`, и наоборот.
- Workflows: #253 `TestWorkflows_CloudE2EStderrRedacted` (`:690`), #257
  `TestWorkflows_ReadonlyChecksTariffPages` (`:337`), #259
  `TestWorkflows_CIChecksImageQuotaSkip` (`:367`), #260
  `TestWorkflows_CleanupKeepsRulesOfGroupsInUse` (`:388`), #266
  `TestWorkflows_RaceKeepsAsyncPreemption` (`:199`).
- Прогресс: `TestProgress_*` (пароль), `TestVMCreate_MCPProgressIsTheStderrStages`
  (строки MCP = stderr CLI), `TestProgress_StepShowsNone` (шаги стека).

## Гипотезы из задания — что проверено

- **A3, состояния FIP.** Факт: в спецификации Compute
  (`compute/openapi-v3.yaml`, схема `FloatingIpState`) перечень —
  `available`, `in_use`, `error`, `error_creating`, `error_deleting`,
  `creating`, `deleting`, `updating`, `attaching`, `detaching`; схема
  `PublicFloatingIpResponse` несёт `state`, `interface`, `vm`,
  `nat_gateway`. Значения `Pending` — из спецификации; `BoundTo` читает
  `vm` и `interface`. Регрессии относительно 1.2.3 нет: свой адрес
  узнаётся во всех формах, которые отдаёт API; перестал браться только
  свободный одноимённый — это и есть A3. Что в состоянии `creating` API
  уже называет `interface`, спецификация не говорит; код не зависит от
  этого (достаточно любого из трёх условий). Остаток — FB13-5.
- **D26, пути DELETE.** Факт: все HTTP-пути 23 целей удаления есть в
  `scripts/api-contract.json` (compute — 6, VPC — 2, DNS — 4, NLB — 2,
  mk8s — 1, PostgreSQL — 3, Kafka/Redis — 1, AR — 1, Container Apps — 1,
  Secret Manager — 1, Certificate Manager — 1; S3 — протокол minio, вне
  описи). Сверка идёт на каждом PR (жёстко) и на push в `main` (мягко,
  #248). Гипотеза подтверждается как остаточный риск: 404 шлюза или
  региона без маршрута прочитается как `already_deleted` — FB13-3.
- **D28, `dns_record_create` и FQDN с точкой.** Факт: спецификация DNS
  (`v1CreatePublicRecordSoleRequest.name` — «Название записи публичной
  зоны») формата имени не задаёт; тип приводится к нижнему регистру
  (`tools_dns.go:358`), имя сравнивается как дано. Гипотеза остаётся
  гипотезой — только живой cloud-e2e (уже в плане 1.3). Тот же класс у
  `vpc_route_create`: подсеть назначения сравнивается строкой
  (`10.0.0.0/24` против нормализованной формы API) — FB13-7.
- **A8 / FB121-2, путь к чужой ВМ.** Не нашёл. Проверка
  (`project_bound.go:378`) и обработчик (`tools_vm.go:58`) делят ветки
  одной функцией `looksUUID` (`tools_disk.go:23`: 36 символов и 4 дефиса):
  всё, что обработчик пошлёт в `GET /vms/{id}`, проверка сверила со
  списком ВМ проекта точным сравнением строк; строка из 36 символов, не
  UUID, проверку проходит как «UUID», в списке её нет — `NOT_FOUND`
  (закрыто в безопасную сторону); имя ищется `resolveVM` только в списке
  проекта сервера. `destroy` разрешает `vm_id` через список
  (`tools.go:2473–2486`, `whyListed`), случай с чужим id — в
  `listedCases`. Ошибка фабрики помечена `ErrProjectNotAllowed`
  (`project_bound.go:300, 310–327`): только она — `NOT_FOUND`.
- **FB121-7** — не стало хуже (раздел 2).

## Находки

### FB13-1 (до rc.1; процесс, гигиена репозитория) — каталог `--help/` с 28 спецификациями Cloud.ru попал в #265; удалён #268, остался инвариант
- **Путь:** `scripts/cloudru-specs.py` берёт каталог вывода из
  `sys.argv[1]` без разбора флагов (`cloudru-specs.py:195`): вызов
  `python3 scripts/cloudru-specs.py --help` создал каталог `--help/` с
  манифестом и 27 OpenAPI-файлами (63 тыс. строк), и #265 закоммитил его
  вместе с правкой (`git show eef78db --stat`: 54 файла, +63 704, из них
  код — 26 файлов). Содержимое — публичные спецификации, внутренних
  сведений нет; но в дереве rc.1 это мусор, он мешает `grep` и
  `api-contract` (у которого свой кэш `.cache/cloudru-specs`, он в
  `.gitignore`).
- **Содержимое проверено:** `manifest.json` называет источник
  `https://cloud.ru/docs` и URL каждого файла; в файлах — публичные хосты
  `*.api.cloud.ru`, примеры спецификаций (нулевые UUID, частные IP,
  `support@cloud.ru`, `user_email@test.com`); ключей, токенов, внутренних
  хостов и почты нет.
- **Сделано:** удаление — #268 (`622070d`, только `--help/**`); #269 —
  дубль той же ветки, закрыт. Ускользнул: «1.3 — разработка», по счётчику
  additions у #265 после слияния; инварианта нет.
- **Осталось (владелец: «Процесс · 1.3», до rc.1):** в `cloudru-specs.py`
  — `-h`/`--help` печатает Usage и выходит, каталог, начинающийся с `-`,
  отвергается. **Тест:** в `cloudru-specs-test.py` — вызов с `--help` не
  создаёт каталога; инвариант репозитория (`internal/workflows` или
  `internal/testrules`) — файлов `openapi*.yaml`/`swagger*.yaml` вне
  `scripts/` и `.cache/` нет.

### FB13-2 (до 1.3.0; документ) — две неточности в текстах «Миграции»
- **D29, `changelog.d/k8s-ar-consent.md`:** «кластер не найден … Стало: эти
  отказы — до вопроса, `USAGE` (код 1) или `CONFIG_INVALID` (код 2)».
  Факт: кластера нет → `ListNodePools` отвечает 404 (`mk8s API 404: …`,
  `mk8s/client.go:114`) → `uncodedReason` → `NOT_FOUND`, код 4
  (`errreason.go:50`); так же у `clusterZoneID`. До вопроса — верно, код —
  4, не 1 и не 2. Поправить фразу: «кластер не найден — `NOT_FOUND`, код 4».
- **D26, «Миграция» в `CHANGELOG.md`:** «Было: `NOT_FOUND` или
  `VM_NOT_FOUND`, код 4». У `release_floating_ip` по `ip_address` в 1.2.3
  было `errResult(fmt.Errorf("no floating IP with address …"))` без кода —
  `uncodedReason` давал `USAGE`, код 1 (в тексте нет «not found»); стало
  0. Добавить в пункт: «у `evo vpc fip-release --ip-address` — `USAGE`,
  код 1».
- **Тест:** не нужен — тексты; сверка с кодом — этот отчёт.

### FB13-3 (1.4; идемпотентность, гипотеза) — 404 не того маршрута читается как `already_deleted`
- **Путь:** `isGone` (`delete_gone.go:69`) берёт `NOT_FOUND` из той же
  `errReason`, что и ответ об ошибке; у сервисов кроме compute 404
  любого происхождения — `NOT_FOUND` (`errreason.go:50`). Маршрут, снятый
  или не развёрнутый в регионе, шлюз, отвечающий 404 на неизвестный путь,
  — код 0 «удаление уже выполнено раньше». Для `destroy`/`disk_delete`
  compute различает VM и прочее (`notFoundCode(path)`), но тоже по пути.
  Защита сейчас — опись `api-contract.json` против спецификации (жёстко на
  PR, мягко на `main`); живое API может расходиться со спецификацией
  (реестр расхождений).
- **Исправление:** одна точка в `deleteFailed`: когда в этом же вызове
  ресурс только что читался (превью или GET родителя) и ответил 200, 404 на
  DELETE перепроверяется GET; ресурс на месте — `API_ERROR` с текстом «API
  ответил 404 на удаление ресурса, который есть», а не `already_deleted`.
  Процесс: сверка `api-contract` на push в `main` — жёсткая для записей с
  `DELETE`. **Тест:** в `already_deleted_test.go` — фейк, у которого GET
  200, DELETE 404: ответ `API_ERROR`, не 0.

### FB13-4 (1.4; секреты, структура) — прогресс по HTTP идёт мимо H7
- **Путь:** `postSSE` (`mcp/http.go:384–392`) маршалит уведомление как
  есть; `redactSecrets` оборачивает только ответ инструмента
  (`restrictForHTTP`). Сегодня строки прогресса — собственный текст evo без
  секретов (`TestProgress_HTTP` проверяет пароль), дефекта нет. Но правило
  «секрет не уходит по HTTP» держится на том, что ни одна будущая строка
  этапа не возьмёт секрет, а не на одной точке решения (§2 CLAUDE.md).
- **Исправление:** `notify` по HTTP — через ту же маску, что ответ
  (`redactSecrets` на `params.message`), одной обёрткой. **Тест:**
  строка прогресса с эхом секретного значения по HTTP приходит замаскированной.
- **Почему не до rc.1** (ревью Qwen #267 предлагает временный инвариант по
  образцам «пароль, PEM, ключ» в `progress_test.go`): единственный секрет,
  который рождается в `provision` и `deploy_apply`, — пароль ВМ, и его
  инвариант уже есть (`TestProgress_Stdio`, `TestProgress_HTTP` на обоих
  транспортах); PEM и ключи в эти обработчики не входят, а `*_operation`
  печатает только статус операции (`opwait.go:89`). Проверка по образцам
  ловила бы только то, что заранее назвали, и давала бы ложное
  спокойствие; правильная защита — одна точка маски, это правка кода
  сервера MCP, а не теста — 1.4.

### FB13-5 (1.4; деньги, гипотеза) — свободный одноимённый адрес после сбоя привязки остаётся безымянным
- **Путь:** создание FIP с `interface_id` прошло, привязка не удалась —
  адрес `<ВМ>-fip` в `available`, без `vm`/`interface`: не `Pending`, не
  `BoundTo` — не берётся (`apply.go:446`), по замыслу A3. Ответ просит
  повторить `deploy_apply`; повтор создаёт второй адрес, первый
  оплачивается, `destroy` освобождает только привязанный. В 1.2.3 адрес
  брался (с неверным `public_ip`); обе стороны выбора записаны в
  `changelog.d/preview-meta.md`.
- **Исправление:** в `note` ответа — id и адрес найденного свободного
  одноимённого FIP и подсказка `release_floating_ip`/`fip_attach`; или
  привязка свободного одноимённого адреса к этой ВМ вместо нового (деньги:
  сирота дешевле второго адреса; решение владельца). **Тест:** фейк, где
  после потерянного ответа список отдаёт `available` `<ВМ>-fip`: ответ
  называет его id.

### FB13-6 (1.4; секреты, документ) — `agent setup` просит вписать токен в файл проекта
- **Путь:** у клиента без ссылки на переменную окружения запись получает
  заполнитель `<CLOUDRU_MCP_TOKEN>` (`agent_clients.go:258`), подсказка
  велит заменить его токеном (`agent.go:666`). Файл — проектный
  (`.mcp.json`, `.cursor/mcp.json`, D22) и уходит в git вместе с проектом.
  Подсказки про `.gitignore` или `--scope user` нет.
- **Исправление:** в подсказку — «файл с токеном не коммитьте: добавьте в
  `.gitignore` или используйте `--scope user`»; в `docs/agent-setup.md` —
  то же. Вариант сильнее (ревью Qwen #267): для клиента с заполнителем —
  `--scope user` по умолчанию, чтобы файл с токеном не попадал в проект
  вовсе; это правка умолчаний D22, решение владельца. Это единственное
  место, где человек вписывает секрет в файл сам: профиль и план evo пишет
  сам с 0600. **Тест:** текст подсказки содержит `.gitignore` при
  заполнителе.

### FB13-7 (1.4; идемпотентность, гипотеза для cloud-e2e) — сравнение «имени» вложенного объекта строкой
- **Путь:** `dns_record_create` — `name` как дано против `name` из списка
  (`tools_dns.go:386–397`), `vpc_route_create` — `subnet` как дано против
  `subnet` маршрута (`tools_vpc.go:256`). Если API нормализует (FQDN с
  точкой, CIDR с нулями в хосте), повтор создаст второй объект, а
  `lostCreate` назовёт не тот — класс D28. Спецификации формат не задают.
- **Исправление:** после живой пробы — нормализация в одной функции
  (`strings.TrimSuffix(name, ".")`, `netip.ParsePrefix(...).Masked()`) с
  обеих сторон сравнения. **Тест:** фейк с нормализованным именем в
  списке — повтор отказывает с `existing_id`. Живая проверка — в cloud-e2e
  1.3, как записано в плане.

## Гипотезы (не проверял вживую)
- FB13-3, FB13-5, FB13-7 — выше; все три — поведение живого API, которого
  спецификация не описывает.
- `k8s_add_nodepool`: `ListNodePools` отдаёт `name`/`status` в тех ключах,
  что читает `takenName`; `vpc_routes` — `subnet` строкой. Фикстуры
  тестов так и устроены; живой ответ — cloud-e2e.
- `pg_delete` кластера в `DEPLOYING` по-прежнему особый случай до
  `deleteFailed` (`tools_pg.go:514–526`) — верно, не 404.

## Что делать
1. FB13-1: удаление сделано (#268); правка `cloudru-specs.py` и инвариант
   — «Процесс · 1.3» до тега rc.1. FB13-2 — поправка двух фраз «Миграции»
   до `1.3.0` (можно в PR версии).
2. Тег `1.3.0-rc.1` (на `622070d` или позднее) после зелёного cloud-e2e на коммите тега, подписи
   образа (#228) и «да» владельца (`RELEASING.md`, шаг 0).
3. FB13-3…FB13-7 — строки 1.4 в `release-plan.md`; FB13-7 и гипотеза D28 —
   в живые проверки cloud-e2e 1.3, как уже записано.
