# Полный аудит 1.6 (Fable, 30.10.2026)

Чек-лист `CLAUDE.md` §5: полный аудит перед тегом minor, по diff
`v1.5.0..ee1287d` (`main`, кандидат тега `v1.6.0-rc.1`: последний
продуктовый PR версии — #664) плюс проход по чек-листу всего продукта и
четырём осям §6. 86 коммитов, 307 файлов, +16 291/−1 377; код продукта —
89 файлов, +6 729/−639 (из них опись `scripts/api-contract-fields.json` —
3 009 строк); тесты — 112 файлов, +6 941/−305; остальное — документы,
скиллы, CI. По решению владельца аудит шёл подагентом сессии релиза, не
отдельной сессией; только чтение, в облако не ходил (cloud-e2e — отдельно,
по «да» владельца); сеть не использовал — CI и ревью Qwen по PR не
сверял, сверяет «1.6 — релиз».

Продуктовые PR диапазона (38): #537, #538, #539, #544 (D53), #546, #551,
#553, #554 (D54), #556, #557, #565, #566, #567, #583, #584 (D58), #601
(D59), #602, #613 (D57), #616, #618, #619, #630 (D60), #631, #633 (D61),
#634 (D62), #637 (D64), #638, #639 (D63), #641, #643, #648, #650, #653,
#654, #655, #660 (D65), #662, #664. PR «(процесс)» (47): #561, #587–#596,
#599, #600, #603–#612, #614, #615, #617, #620, #625–#629, #632, #640, #642,
#644–#647, #649, #651, #652, #656–#659, #661; без метки — #635 (план,
итог api-probe).

**Вердикт: блокеров тега нет — тег `v1.6.0-rc.1` на коммите слияния PR
версии (поверх `ee1287d`) ставить можно.** Блокер — дефект кода, который
запрещает тег; таких нет. Десять ломающих изменений версии (D53, D54, D57,
D58, D59, D61, D62, D63, D64, D65) оформлены по правилу `docs/RELEASING.md`:
у каждого пункт «Миграции» во фрагменте `changelog.d`, строка 1.6.0 в
таблице `RELEASING.md` (в отличие от 1.5 — уже на кандидате), решение
владельца с номером D, условия 1–5 выполнены (раздел 5). D60 — не
ломающее: новые поля, `reason` и коды не меняются. Необъявленных ломающих
изменений в diff не нашёл: удалённые строки с кодами причин — только смена
сигнатур (`k8sVIPSubnet`, `k8sNodesSubnet`, `lbZoneAndSubnet` стали
отдавать имя подсети), `lb_create` на планировщике сохраняет `status:
creating`, `operation_id`, `created`, `lb_id`; `destroy` по `vm_id` держит
`status: not_deletable_yet`, меняется только текст `message`. Дефектов
согласия, секретов, паритета и идемпотентности нет. Одна находка средней
тяжести — пробел «одной точки» D63 на пути повтора после отвергнутого
токена (FB160-1): редкое сочетание двух условий, после тега. Остальные —
низкие и шаги порядка выпуска.

Находок: блокирует тег — 0; высокая — 0; средняя — 1 (FB160-1); низкая —
5 (FB160-2…5, FB160-8); до тега (шаги порядка, не дефекты) — 2 (FB160-6,
FB160-7); гипотез — 4.

## Проверки

| Проверка (коммит `ee1287d`, `main`) | Результат |
|---|---|
| Локально на `ee1287d` | `gofmt -l` пусто; `go vet ./...` чисто (linux и `GOOS=windows`); `go test -count=1 ./...` — 47 пакетов ok; `go test -race` — `internal/httpretry`, `internal/auth`, `internal/engine`, `internal/tools`, `internal/cli`, `internal/mcp` — см. строку ниже; `python3 scripts/changelog.py check` — ok (40 фрагментов, 10 с «Миграцией»); `evo tools list --json` — 157 инструментов, как говорит `docs/mcp.md` |
| `go test -race` | `internal/httpretry`, `internal/auth`, `internal/engine`, `internal/tools`, `internal/cli`, `internal/mcp` — ok (гонок нет); `staticcheck` в окружении аудита не установлен — за ним CI |
| CI на `ee1287d`, ревью Qwen | не сверял: аудит шёл без сети; сверяет «1.6 — релиз» по правилу §4 (ревью Qwen у продуктовых PR, ответ на каждую находку) |
| Утечки в diff (добавленные строки) | UUID — только фейки тестов нулевой формы (`00000000-0000-4000-8000-…`, `0000a1a1-…`); IP — `10.x`, `172.16.x`, `192.168.x`, `0.0.0.0`, `127.0.0.1`, документационные `203.0.113.x`, `192.0.2.x`; `8.8.8.0/24` и `10.10.0.5/24` — входы теста `subnet_create`; хосты — `*.api.cloud.ru`, `cloud.ru`, `github.com`, `example.com`, `example.ru`, `kind.ru` (строка теста); почты и ключей нет. Сведения о платформе — только ссылками на пункты реестра (CMP-25, DB-5, DB-13, DB-17, DB-19, DB-21, NET-11, IAM-6, K8S-16, SEC-1), без сценариев |
| Тест, падающий без правки (проверено по тестам в дереве) | D53 — `TestDiskChange_*` (4); D54 — `TestNotInCatalogue`, `TestSnatCreate_SecondGatewayInTheZoneRefusedBeforeTheQuestion`; D57 — `TestRefByName`, `TestRefArgs_DeclarationsHold`, `TestInputByName_SubnetAndSecurityGroup`; D58 — `TestCreate_ConfiguredZoneByName`; D59 — `TestDiskDelete_AttachedRefusedBeforeTheQuestion`; D60 — `TestPreview_SaysTheQuotaWasNotChecked`; D61 — `TestDeletePlan_PassingStateRefusedBeforeTheQuestion`, `TestDeletePlan_DeletingStateAnsweredWithoutTheQuestion`; D62 — `TestDeploy_AnotherProjectsComposeIsRefused` (три подтеста: чужой файл, запись до 1.6, ВМ `provision`); D63 — `TestRouteNotFoundAsBadGateway`, `TestAlreadyDeleted_Bare404IsNotGone`; D64 — `TestStackPlan_DNSOnAnInternalBalancerRefused`; D65 — `TestDNSZoneCreate_PrivateZoneLinksAVPC`; #641 — `TestOperation_EveryOperationDeclared`, `TestOperation_ElapsedAndResourceStatus`; #650 — `TestEstimatePG_ByTheTariffSnapshot`, `TestPGCreatePreview_PricedByTheTariff`; #648 — `TestPG_AccessIsNamed`; #655 — `TestFlavor_ZonesFromTheLiveAnswer`; #631/#646 — `TestRefusedTokenGoesOnceMoreWithTheRenewedOne`, `TestCreate_OneRefusedTokenMakesOneResource`; линия CLI — `TestRenderAnswer_ReferenceByNameAndID`, `TestNamedParams_HumanOnly`, `TestZoneNameReadWithItsID`, перечень `bareRefs` пуст (#664) |
| Инварианты «Процесса» на кандидате (полный проход) | зелёные: `TestConfirmedReadOnlyInTheGate` (гейт — одна функция, D35; `inlineGates` 30 → 28: `disk_attach`, `disk_detach` на `gated`), `TestConsent_RefusalWithoutWriteComesBeforeTheQuestion` (`lateRefusals` пуст), `TestAlreadyDeleted_EveryDeleteDeclared` (D26; `subnet_delete` в `deleteTargets`), `TestParity_CommandsAndTools`, `TestParity_JSONEqualsTool` (`jsonAliasCompat` не рос), `TestCommandsDoc`, `TestToolsDoc`, `TestPreview_NamesWhatTheWriteSends` (С1), `TestPreview_HumanShowsEveryValue` (С2), `TestWallClockReadsDeclared`, `TestRedact_*`, `TestMisses_HaveADeadline` — 27 перечней, 264 строки, просроченных 0; пусты `lateRefusals`, `bareRefs`, `localJSONMisses`, `optionalUnnamed`, `previewNoProject`, `accessHintMisses`, `toolHintFlagMisses`, `takenAdopts`, `takenNotReached`, `worldDrift`, `liveWorldDrift`; `bare404Gone` — 1 (S3) |

## Чек-лист

### 1. Согласие (гейт D35, ось 1 §6: «отказ без записи = отказ до вопроса»)

- **Гейт — одна функция.** `TestConfirmedReadOnlyInTheGate` зелёный,
  `inlineGatesLeft = 28` (`internal/testrules/gate_test.go:62`); D53 перевёл
  `disk_attach` и `disk_detach` на планировщики, `lb_create` — на
  `planLbCreate`. Перечень со сроком «Closes by 1.8» (#645).
- **Каждый новый отказ 1.6 стоит до чтения `confirmed`:**
  - D53 — `settledDisk` (`internal/tools/tools_disk.go:311`): переходное
    состояние ждёт `diskSettleTimeout` = 30 с тем же `waitDisk`, говорит
    состояние, затем `USAGE`; константа одна.
  - D54 — `notInCatalogue` (`catalogue_refusal.go`): отказ только когда
    список прочитан и не пуст; `gatewayInZone` в `handleSnatCreate`
    (`tools_snat.go:118`) — до `confirmed` (`:125`), `existing_id`.
  - D57 — `resolveRefs` — обёртка реестра (`registry.go:129`), внутри
    `requireProject`, снаружи `boundToProjects`: имя заменяется на id до
    обработчика на обеих поверхностях; дубль — `USAGE` с id; ни одного,
    сбой списка — значение уходит обработчику как раньше (`ref_by_name.go`).
  - D58 — `resolveVMZone` у `disk_create` (`tools_disk.go:137`),
    `snat_create` (`tools_snat.go:44`), `image_create` (`tools_image.go:140`);
    одна точка — `engine.ResolveZoneRef`.
  - D59 — `diskAttachedError` в `planDiskDelete` (`tools_disk.go:486`) по
    уже прочитанному `GetDisk`; подсказка по каждой ВМ — `destroy` для
    загрузочного, `disk_detach` для обычного; `attached_vms` в ответе.
  - D61 — `passingRefusal` и `alreadyDeleting` в `deletePlan`
    (`delete_preview.go:48–53`) — десять `*_delete` (`ar`, `redis`, `pg`,
    `vpc`, `kafka`, `snat`, `subnet`, `tg`, `lb`, `k8s`) и
    `release_floating_ip` (`tools_deploy.go:953`); состояния — одна точка
    `engine.PassingDeleteState`/`DeletingState` (`delete_wayout.go`),
    сравнение без учёта регистра. ВМ в `creating` — как раньше, ждёт
    (объявлено в «Миграции»).
  - D62 — `composeRefusal` в `engine.Deploy` (`deploy.go:78`) стоит до
    `redeployGate` (`:137`) и гейта новой ВМ (`:620`) — CLI отказывает до
    вопроса; MCP — `DeployComposeCheck` в `deployAnswer` (`tools.go:1637`),
    до превью. Отбор записи — `ActiveComposeDeployment` (`state.go:227`),
    записи `provision` не обновляются.
  - D64 — проверка в `handleStackPlan` (`tools_stack.go:592`) до чтений
    облака; умолчание `nlb.external` не меняется.
  - D65 — `projectVPC` (`tools_subnet.go:58`, общая с `subnet_create`)
    зовётся в `handleDnsZoneCreate` (`tools_dns.go:219`) до `confirmed`
    (`:230`); два VPC «по умолчанию» — `USAGE`, нет VPC — `USAGE` с
    подсказкой `vpc_create`, `vpc_id` не из проекта — `NOT_FOUND`,
    `vpc_id` у публичной зоны — `USAGE`; `vpc_id` объявлен `kindVPC`
    для `--allow-project` (`project_bound.go:164`).
- **D60** — отказа по квоте нет, как решено: `quotaCheck` отдаёт только
  `reason` (`project_key` по 403 `ErrQuotasForbidden`, `not_read`,
  `not_mapped`), строка превью «квота: не проверена (…)».

### 2. Секреты (вывод, ошибки, журнал, превью, state, ответы MCP)

- **Повтор после отвергнутого токена (#631, #646).** `IAMProvider.issued`
  (`auth/iam.go`) — набор выданных токенов в памяти процесса, не больше
  16, не пишется и не логируется; `renewRefused` отдаёт новый токен только
  провайдеру, который выдал отвергнутый; `renewedToken` (`httpretry.go`)
  меняет только заголовок `Authorization` клона запроса. Провайдеры CLI и
  `evo mcp` держит `providers` (`cli/credentials.go:68`) весь процесс —
  слабые ссылки `live` их не теряют; провайдеры `configure` и
  `tools_admin` короткоживущие, им повтор не нужен.
- **Новые тексты.** `S3AccessHint` называет имена переменных
  (`CLOUDRU_S3_ACCESS_KEY`, `…_SECRET_KEY`), не значения; `composeRefusal`
  — пути файлов; `diskAttachedError` — id и имена ВМ (`_untrusted`);
  `notInCatalogue` — значения каталога. Ключей и токенов в них нет.
- **`output` отказа `docker compose up` (#566).** Последние 20 строк
  вывода нагрузки — поле `details.output`/`output`, помечено `_untrusted`
  как `log`; маскирование H7 (`redactSecrets`) идёт по ключам JSON, а не
  по свободному тексту — тот же класс, что у `logs` и `log` (строка плана
  FB13-4), новой поверхности утечки нет.
- **Превью PostgreSQL с ценой** — числа тарифа; **превью `secret_create`**
  называет `kms_key_id` и `description`, значение секрета в превью не
  попадает (#537, `TestSecretCreate_PreviewNamesTheKMSKey`).
- **Tab** не менялся, кроме `noCompletion` у `ca_deploy image` (#538).

### 3. Паритет CLI ↔ MCP (D13, D14) и «Линия CLI»

- **«Имя (id)» — только человеку.** Единственные точки — `cli.refValue` и
  `namedRef` (`cli/trees.go:905, :921`) в таблицах полей и списков и
  `engine.namedParams` (`create_preview.go:965`) в строках превью; обе
  берут имя одним правилом `engine.RefName` (`:954`): `<x>_name`, иначе
  строковый `<x>` рядом с `<x>_id`. В JSON-ответ и `preview.params` поля
  кладутся обработчиком как есть (`subnet_name`, `flavor_name`,
  `region_name`, `availability_zone_name`, `interface_name`,
  `defaults.zone_name`, `network.subnet_name`); `TestParity_JSONEqualsTool`
  зелёный, `jsonAliasCompat` не рос. `bareRefs` пуст (#664, срок «1.8»
  снят досрочно).
- **`*_operation`** — одна точка `Registry.operation` (`opanswer.go`): все
  восемь обработчиков зовут её (`TestOperation_EveryOperationDeclared`);
  без `wait_seconds` ответ — тоже `opToMap` структуры (ключи те же, что у
  структуры; раньше без ожидания отдавалась структура, с ожиданием — map),
  плюс `elapsed_sec`, `resource_status`, `resource_status_error`,
  `resource_status_note`. Чтение ресурса — `callTool` по `operationReads`,
  то есть через обёрнутый обработчик реестра (`boundToProjects`,
  `markUntrusted`), а не в обход.
- **Три формы (#638)** — `localJSONMisses`, `toolHintFlagMisses`,
  `accessHintMisses` пусты: `evo init --json`, `crew --json`, `mcp
  healthcheck --json` отвечают JSON; подсказки MCP без флагов CLI.
- **D57** — одна обёртка на обеих поверхностях (`TestInputByName_SubnetAndSecurityGroup`:
  ответ по имени = ответ по id).

### 4. Идемпотентность и уборка (ось 3 §6, D26)

- **Повтор запроса после 401** (`httpretry.RoundTrip`): отвергнутый 401
  запрос не исполнен, тело повторяется из `GetBody` (без `GetBody` —
  возврат 401), повтор ровно один — `t.roundTrip(again)` не проверяется
  снова; `TestCreate_OneRefusedTokenMakesOneResource`. Второй ресурс не
  создаётся.
- **D61** — ресурс в `DELETING`/`ERASING` — `status: deleting`,
  `already_deleting: true`, код 0, без DELETE: повтор удаления безопасен,
  как D26.
- **D63** — голый 404 у удалений — `API_ERROR` с `RouteNotFoundHint`
  (`tools.go:3382`, одна точка в `errAnswer`); `bare404Gone` — 1 строка
  (`s3_bucket_delete`, клиент minio, объявлен). `stack_destroy` читает
  «уже нет» только по коду причины, текстом — лишь без кода
  (`tools_stack.go:1221`). Пробел на пути повтора после 401 — FB160-1.
- **`subnet_delete` (#567)** — новый delete: `deletePlan` (D26, D61),
  `can_delete: false` — `USAGE` до вопроса (`tools_subnet.go:260`), в
  `deleteTargets` (`delete_gone.go:54`). `subnet_create` — занятое имя,
  пересечение сетей в VPC (`subnetOverlap`), неоднозначный VPC — до
  вопроса.
- **D59** — диск у ВМ не удаляется: ничего не уходит, подсказка называет
  порядок уборки; **D62** — чужой compose не уходит, ВМ `provision` не
  трогается; **destroy (#565)** — отказ платформы называет прочитанное
  состояние ВМ и её floating IP с путём `release_floating_ip`.

### 5. «Миграция» ↔ `DECISIONS.md` ↔ таблица `RELEASING.md` (D53, D54, D57–D65)

| Решение (PR) | Фрагмент `changelog.d` | Строка 1.6.0 `RELEASING.md` | Условия 1–5 | Смена `reason`/кода объявлена |
|---|---|---|---|---|
| D53 (#544) | `disk-transient-state.md`, «Миграция» | строка 83 — есть | 1 — согласие на вызов, который не пройдёт (дыра в согласии, D35; run 142); 2 — любой `disk_resize`/`detach` сразу после изменения; 3 — одна функция `settledDisk`, три инструмента; 4 — ждать `USAGE` вместо 5; 5 — «Миграция», D53 (#542), README — PR версии | да: после «да» 5 → 1 до вопроса; превью `disk_attach`/`detach` без ключа 3 → 2, нет ВМ или диска — 4 |
| D54 (#554) | `d54-refusals.md`, «Миграция» | строка 84 — есть | 1 — согласие на заведомо невыполнимое (FB15-17, 38, 42; второй SNAT — 422, 2 из 2); 2 — любой скрипт с версией не из списка; 3 — `notInCatalogue` + `gatewayInZone`, «нет списка — как раньше»; 4 — ждать `NOT_FOUND`/`USAGE` на превью; 5 — «Миграция», D54 (#543) | да: 3/5 → 4; SNAT 3/5 → 1 |
| D57 (#613) | `input-by-name.md`, «Миграция» + «Добавлено» | строка 87 — есть | 1 — имя двух объектов: отказ обработчика или вопрос и отказ платформы (неверный путь); 2 — платформа уникальности имён не обещает (P-N2); 3 — одна декларация `refArgs`, одна обёртка; 4 — передать id из ошибки; 5 — «Миграция», D57 (#597) | да: `CONFIG_INVALID`/`NOT_FOUND`/3 → 1 только у дубля имени |
| D58 (#584) | `region-by-name-creates.md`, «Миграция» + «Исправлено» | строка 85 — есть | 1 — согласие на вызов, который не пройдёт (P-Z1, с 1.0); 2 — любой `CLOUDRU_REGION=<имя>` у трёх create; 3 — одна точка `resolveVMZone`; 4 — ждать `USAGE`; 5 — «Миграция», D58 (#585) | да: 3/5 → 1; `will_create.zone` у `image_create` — зона вместо текста правила (объявлено в «Исправлено»; прежнее значение — фраза, не для скрипта) |
| D59 (#601) | `disk-delete-attached.md`, «Миграция» | строка 86 — есть | 1 — согласие на вызов, который не пройдёт (422 после «да», финал Grok rc.1); 2 — любой `disk_delete` подключённого диска; 3 — одна проверка по уже прочитанному `GetDisk`; 4 — ждать `USAGE`; 5 — «Миграция», D59 (#587) | да: 3/5 → 1 |
| D60 (#630) | `preview-quota.md`, «Добавлено» | не нужна — не ломающее | новые поля `quota`, `name`/`unit`/`product`; `reason` и коды не меняются | — |
| D61 (#633) | `delete-passing-state.md`, «Миграция» | строка 88 — есть | 1 — согласие на вызов, который не пройдёт (DB-17, DB-19, NET-11; G130-19); 2 — любое удаление сразу после создания; 3 — одна точка `passingRefusal`/`alreadyDeleting`, конечные состояния и ВМ не трогаются; 4 — ждать 1 и повторить, `DELETING` — код 0 `status: deleting`; 5 — «Миграция», D61 (#621) | да: 3/5 → 1; `DELETING`/`ERASING` 3 → 0 |
| D62 (#634) | `deploy-compose-path.md`, «Миграция» | строка 89 — есть | 1 — выкат чужого проекта на работающую ВМ после «да» (неверный результат, Ц-27, Ц-30); 2 — любой `evo mcp` из другого каталога; 3 — одно поле state и одна проверка `composeRefusal`, старые записи — предупреждение; 4 — запустить из каталога записи или `compose_file`; 5 — «Миграция», D62 (#622) | да: вопрос → `USAGE` 1; `deploy` рядом с ВМ `provision` создаёт свою |
| D63 (#639) | `bare-404-route.md`, «Миграция» | строка 91 — есть | 1 — ложное «удалено» при живом оплачиваемом ресурсе (E1, FB13-3; ось 3); 2 — переезд API или неверный базовый адрес у любого скрипта; 3 — одна точка в транспорте, `Keep404` только у Organization API, HEAD и minio не тронуты; 4 — код 5 с подсказкой адреса — не повторять, проверить версию; 5 — «Миграция», D63 (#623, проба #629) | да: удаление 0 → 5, чтение 4 → 5 — только у 404 без тела сервиса |
| D64 (#637) | `stack-dns-internal-lb.md`, «Миграция» | строка 90 — есть | 1 — ложный успех: стек «готов», сервис снаружи не открывается (FB15-47); 2 — любой манифест с `dns` и `nlb` без `external`; 3 — одна проверка в `handleStackPlan`, умолчание не меняется; 4 — `nlb.external: true`; 5 — «Миграция», D64 (#624) | да: 0 → `USAGE` 1 на `stack_plan` |
| D65 (#660) | `dns-private-zone-vpc.md`, «Миграция» | строка 92 — есть | 1 — нерабочий ресурс с кодом 0 (ось 4); 2 — любая приватная зона в проекте без VPC по умолчанию; 3 — один аргумент, одна точка `projectVPC`, публичные зоны не меняются; 4 — `--vpc-id`; 5 — «Миграция», D65 (#657) | да: 0 → `USAGE` 1 / `NOT_FOUND` 4 |

Условие 5, третий пункт («упоминание в README „Что сейчас можно“ и в
анонсе») — шаг PR версии, как в 1.5 (FB160-6).

Необъявленных ломающих изменений нет. Проверено по удалённым строкам diff
с кодами причин (`clierrors.Code*`, `usageT`, `"status":`): все — смена
сигнатур при чтении имени подсети (`k8sVIPSubnet`, `k8sNodesSubnet`,
`lbZoneAndSubnet`), перенос `lb_create`, `disk_attach`, `disk_detach` на
планировщики с теми же ответами, `destroy` с тем же `status` и
`RefusedVMDelete`, подсказки (`ProjectRequired`, `hintUnexpectedFormatVerbose`
— текст, не контракт). Новые поля ответов — добавления: `elapsed_sec`,
`resource_status*`, `access`, `cost`/`cost_note` у `pg_create`, `est_note`,
`tariff_url` у `list_flavors`, `quota`, `project_id` превью, `attached_vms`,
`already_deleting`, `output`, `subnet_name`, `flavor_name`, `region_name`,
`availability_zone_name`, `interface_name`, `zone_name`, `vpc_id`/`vpc_name`
у зоны DNS, `min_size_gb`/`max_size_gb`/`zones`/`speed_limits`, `purpose`,
`zone` у адреса, `flavor_zones`, `image_min_*`, `interface_types`,
`will_create.subnet_name`, `flavor` у `k8s_add_nodepool`. `evo init --json`
был пустым stdout с кодом 0 — теперь JSON (исправление). Значения прежних
полей не меняются, кроме объявленного `will_create.zone` (D58).

### 6. Четыре оси §6 на новых поверхностях 1.6

| Поверхность | Ось | Проверено | Итог |
|---|---|---|---|
| `*_operation` (#641) | 2 | `elapsed_sec` — от `created_at`/`createdAt` до `updated_at`/`updatedAt` (завершена) или до `clock.Now()` (идёт); времена читают структуры семи клиентов (`dbaas`, `postgresql`, `nlb` — `created_at`; `vpc`, `dns`, `artifact` — `createdAt`), `certmanager` отдаёт map как есть; `resource_status` — `status`/`state`/`phase` чтения; `NOT_FOUND` первого чтения — следующее (`lb_get` → `tg_get`, `dns_zone_get` → `dns_record_get`), все `NOT_FOUND` — `not_found` с пояснением, иной отказ — `resource_status_error` с причиной | закрыт; FB160-4, FB160-5 |
| Цена PostgreSQL (#650, #644) | 4 | `pricing.EstimatePG` — vCPU и RAM каждого узла, SSD каждого узла, `n = max(instances, 1)`; ставки = снимок `testdata/tariff-managed-postgresql.json` (версия 260918, с НДС: 1,49145 / 0,4026 / 0,016836); `Source` называет допущение «storage counted on each node»; `cost_note` — дату тарифа, `tariff_url` — страницу; человек видит узлы, хранилище, час и месяц одной строкой (`create_preview.go:827`); `pgInt` читает `float64`/`int`/строку — размеры спецификации и `instances` доходят | закрыт; сверка со страницей — `TestLive_TariffPageMatchesSnapshot` в job `contract` (живой, здесь не гонялся) |
| Доступ PostgreSQL (#648) | 4 | `pgAccess` — одна константа для превью и `pg_get`: «размещён в подсетях `subnet_ids`; API PostgreSQL не принимает параметра публичного доступа; evo его не настраивает» — только факты вызова и спецификации, слов «private»/«приватный» нет; откуда доступен на деле — проба DB-5 | закрыт по существу; FB160-2 (язык) |
| Зоны flavor (#655) | 1, 4 | `Flavor.ZoneNames`: `enabled && (available == nil \|\| *available) && (state == "" \|\| state == "created")` (`provision/types.go:357`); `available` — `*bool`, nil не «недоступна» (CMP-25); фильтр `zone` и `flavor_zones` превью — от неё | закрыт |
| Линия CLI (#653, #654, #662, #664) | 4 (С2) | раздел 3; `humanUnnamed` 31 строка со причинами, `bareRefs` 0 | закрыт |
| D63 (#639) | 3 | `RouteNotFoundAsBadGateway` в `Transport.RoundTrip` для всех клиентов на `httpretry.Transport` (compute через адаптер, VPC, NLB, DNS, mk8s, AR, Container Apps, Secret Manager, Certificate Manager, DBaaS, мониторинг, аудит, биллинг); `serviceNotFound` — JSON-массив, или объект с `code`/`message`, кроме `{"error_msg"}` без них и `code: 5` + ровно `Not Found`; `Keep404` — `tokenGET` Organization API; HEAD не трогается; `orgClient` `configure` — FB160-3 | закрыт; пробел — FB160-1 |
| D61 (#633) | 1, 3 | раздел 1 и 4 | закрыт |
| D65 (#660) | 4 | `linkedVpcIds: [id]` в теле, в описи `api-contract.json:119` поле в `body` и `impact` («работа»); превью называет `vpc_id`, `vpc_name` | закрыт |
| Продление токена после сна (#631, #646) | 2, 3 | раздел 2 и 4; `refusals.keyRefused` (`engine/refused_token.go`) — два 401 подряд = ключ, у опроса ВМ `deploy` и отката boot FIP | закрыт |
| Квота в превью (D60) | 1 | строка «не проверена (…)» и поле `quota`; отказа нет, как решено; один GET в пределах превью | закрыт в объёме D60 |
| Превью: проект, необязательные аргументы, сводки, KMS, `lb`/`tg` (#546, #618, #619, #537, #583) | 4 | `previewNoProject` 0, `optionalUnnamed` 0, `bodyUnnamed` 40 (с причинами) | закрыт |
| `subnet_create`/`subnet_delete` (#567) | 1–4 | раздел 4; превью называет сеть, маску, VPC, зону, маршрутизацию; «Известные ограничения» — `subnet_address` с маской по спецификации, не вживую | закрыт; живая проверка — cloud-e2e rc.1 (#661) |

## Находки

### FB160-1 (средняя; после тега; код `internal/httpretry/httpretry.go:289–312`) — голый 404 после повтора с новым токеном остаётся 404

`Transport.RoundTrip` применяет `RouteNotFoundAsBadGateway` только к
первому ответу (`:292`); когда первый ответ — 401 и запрос уходит ещё раз
с новым токеном, результат `t.roundTrip(again)` (`:312`) возвращается как
есть. Голый 404 шлюза на этом втором ответе доходит до клиента как 404:
удаление отвечает `already_deleted`, код 0, чтение — `NOT_FOUND` — ровно
класс E1, который закрывает D63 («одна точка — транспорт»). Проверено
пробой в копии дерева (сервер: 401 на старый токен, затем `text/plain` 404;
`OnTokenRenewed` отдаёт новый): два вызова, статус 404, заголовка
`Evo-Replaced-Answer` нет. Нужны оба условия — сон машины (или истёкший
токен у клиента сервиса) и переехавший адрес API, — поэтому не блокер.
Предложение («разработка»): один выход из `RoundTrip` — применять правило
404 к ответу повтора так же, как к первому (например, вынести
`RouteNotFoundAsBadGateway` в `roundTrip` после `answer`, или обернуть оба
возврата). Инвариант («Процесс»): мир «401, затем голый 404» в
`TestRefusedTokenGoesOnceMoreWithTheRenewedOne` или вид в
`TestAlreadyDeleted_Bare404IsNotGone`. Ускользнул бы: тестами не пойман,
инварианта на сочетание двух правил транспорта нет.

### FB160-2 (низкая; после тега; код `internal/tools/tools_pg.go:170`) — текст доступа PostgreSQL не локализован

`pgAccess` — английская константа без `i18n.T`; она идёт в `preview.params`
(`:736`) и в `pg_get` (`:162`). У `access` нет подписи в `paramLabels`
(`ParamLabel` отдаёт ключ), поэтому в русском превью `evo pg create` и в
таблице `evo pg get` человек читает «access: placed in the subnets of
subnet_ids; the PostgreSQL API takes no public-access parameter; evo sets up
none» — против D6 (язык CLI). `englishWraps` держит только обёртки ошибок,
константу значения превью инвариант не видит. Факт верный и без слова
«private» — замечание только о языке. Предложение: `i18n.T` у константы и
подпись `access` в `paramLabels` («доступ») — «разработка»; инвариант
«строковое значение `PreviewParam`, которое задаёт evo, проходит через
`i18n`» — «Процесс» (как сделать проверяемым — гипотеза 4).

### FB160-3 (низкая; после тега; код `internal/cli/configure.go:42, :368`) — два клиента Organization API с разной политикой 404

D63 держит 404 Organization API как есть только в `tokenGET`
(`tools_admin.go:49`, `Keep404: true`): «404 значит „ключ не видит список
проектов“, форма не проверена». `configure.go` читает тот же список
проектов своим `orgClient = httpretry.NewClient(…)` без `Keep404`, поэтому
404 там превращается в 502 с текстом «answered 404 without the service's
answer… the API may have moved». Код причины и подсказка не меняются
(`listProjectsPage`: любой не-200, кроме 401/403, — `API_ERROR` с «передайте
--project-id»), меняется только сообщение — поэтому низкая, не ломающее.
Две точки на одно правило — §2. Предложение: `orgClient` на транспорт с
`Keep404`, или один клиент Organization API для `configure` и
`tools_admin` — «разработка».

### FB160-4 (низкая; после тега; код `internal/tools/opanswer.go:74–90`) — `elapsed_sec` завершённой операции без `updated_at` растёт

`operationElapsed` у `done: true` берёт `updated_at`/`updatedAt`, а без них
— `clock.Now()`: завершённая операция, у которой платформа не прислала
время обновления, с каждым опросом «идёт дольше». Строка плана обещала
«`elapsed` (если операция несёт время создания, иначе объявлено)»; без
`created_at` поле просто отсутствует, пояснения нет. Присылает ли
платформа `updated_at` у каждого из восьми сервисов — гипотеза 2 (здесь
не проверялось; `certmanager` отдаёт map как есть). Предложение: у
`done: true` без времени обновления `elapsed_sec` не ставить (или ставить
`elapsed_note`), без `created_at` — `elapsed_note` «операция не несёт
времени создания» — «разработка».

### FB160-5 (низкая; после тега; тест `internal/tools/opanswer_test.go:59–64`) — тест времени операции не различает написание ключей

Тело мира несёт сразу `created_at` и `createdAt`, `updated_at` и
`updatedAt`, поэтому клиент, чья структура читала бы время не под тем
именем (`created_at` вместо `createdAt` у сервиса с camelCase), тест
прошёл бы — `opToMap` сохранил бы второе написание. Сверил по коду: семь
структур читают своё имя верно (раздел 6). Предложение («Процесс»): мир
на каждый `*_operation` — только написание его сервиса (из каталога
живых ответов #617/#651), чужое написание в теле — красный.

### FB160-6 (до тега; шаги PR версии, условие 5 RELEASING) — не дефекты

Не выполнено на `ee1287d`, выполняется PR версии: свёртка `changelog.d`
(40 фрагментов, `python3 scripts/changelog.py fold`; «[Не выпущено]» пуст);
README «Что сейчас можно (v1.6.0, …)» (сейчас v1.5.0); `metadata.evo-version`
пяти скиллов и `skillVersionWant` (`internal/cli/skills_test.go:38`) →
«1.6» (сейчас «1.5»; тест упадёт, пока не поменяны оба); пересчёт
`docs/tools.md` и `docs/commands.md` не нужен — `TestCommandsDoc`,
`TestToolsDoc` зелёные. Строки 1.6.0 в `RELEASING.md` уже есть (10) —
лучше, чем в 1.5 (FB150-6).

### FB160-7 (до тега; шаги порядка) — сверка охвата на кандидате, cloud-e2e, CI и Qwen

- `coverage-1.6.md` — черновик по `591a758`; на кандидате нужны: строка
  D65 (есть, `TestDNSZoneCreate_PrivateZoneLinksAVPC`, #660), PR после
  черновика — #653, #654, #655 (в таблице), #660, #662, #664 (нет),
  счёт перечней — 27 перечней, 264 строки (было 281), `bareRefs` 0 (было
  36). Итог — строкой ниже; правка файла — «Процесс».
- cloud-e2e на кандидате (все области, `*_smoke`, `cleanup_only` без
  остатков; группа 1 rc.1 подготовлена в #661) — по «да» владельца.
- CI на `ee1287d` и ответы на ревью Qwen у 38 продуктовых PR — сверяет
  «1.6 — релиз»: аудит шёл без сети.

### FB160-8 (низкая, гипотеза; после тега; код `internal/engine/compose_path.go:16–37`) — сравнение путей compose буквально

`composeRefusal` сравнивает `filepath.Abs` обоих путей без `EvalSymlinks` и
без учёта регистра: тот же файл через символическую ссылку или другим
регистром на macOS/Windows — ложный `USAGE`. Сторона безопасная (отказ без
записи, подсказка называет путь и `compose_file`), вживую не проверял —
гипотеза 1. Предложение: сравнивать после `EvalSymlinks`, на
регистронезависимых ФС — без учёта регистра; записать путь после
`EvalSymlinks` — «разработка».

## Гипотезы (не проверял вживую)

1. FB160-8 — ложный отказ D62 через символическую ссылку или регистр пути.
2. FB160-4 — присылает ли платформа `updated_at`/`updatedAt` у завершённой
   операции каждого из восьми сервисов; спецификации — да, живой ответ —
   не проверен.
3. `serviceNotFound` (`httpretry.go:242`) считает ответом сервиса любой
   JSON-объект, кроме `{"error_msg"}` без `code`/`message` и `code: 5` +
   ровно `Not Found`: шлюз, который ответил бы, например, `{"detail":
   "not found"}`, прошёл бы как «удалено». По пробе #629 (2 из 2) таких
   форм у шлюзов Cloud.ru нет; смену формы ловит храповик спецификаций.
4. Инвариант к FB160-2: все строковые константы, которые кладутся в
   `PreviewParam.Value` и в ответы чтения, — через `i18n`; проверяемо
   через `i18n.T` в вызовах `p.Add` с литералом или через перечень
   исключений («Процесс»).

## Сверка охвата (итог)

Сверка охвата «Процесса» (`coverage-1.6.md`, черновик по `591a758`) на
кандидате `ee1287d` держится: каждая новая поверхность 1.6 — `*_operation`,
цена и доступ PostgreSQL, зоны flavor, линия CLI, D53, D54, D57–D65,
`subnet_create`/`subnet_delete`, сон машины, три формы, карточки групп —
имеет инвариант или точечный тест в дереве (таблица «Проверки»); D65
получил номер и тест (#657, #660); `bareRefs` закрыт досрочно (#664);
перечни пропусков — 27, 264 строки, просроченных 0; пробелы П1–П3
(тарифные карты Kafka/Redis, доступ Kafka/Redis, обход реестра для D58)
— бэклог «Процесса» с версией-кандидатом, кандидат не блокируют. Новый
пробел — FB160-1 (сочетание правил транспорта) и FB160-5 (мир времени
операции).

## Что делать

- **До тега rc.1 («релиз»):** PR версии — FB160-6; cloud-e2e кандидата по
  «да» владельца, CI и Qwen — FB160-7; обновить `coverage-1.6.md` на
  кандидате («Процесс», FB160-7).
- **После тега («разработка», 1.6.x или 1.7):** FB160-1 (один выход
  `RoundTrip`), FB160-2 (`i18n` и подпись `access`), FB160-3 (один клиент
  Organization API), FB160-4 (`elapsed_sec` без времени обновления),
  FB160-8 (пути compose).
- **После тега («Процесс»):** инварианты к FB160-1 (мир «401, затем голый
  404»), FB160-5 (написание времени — по сервису), FB160-2 (константы
  превью через `i18n`, гипотеза 4).

## Дополнение: #671 (FB160-1)

**Вердикт: блокеров нет; FB160-1 закрыта.** Diff `ee1287d..84ec2f6` — PR #671: `internal/httpretry/httpretry.go`, `internal/httpretry/route404_test.go`, `changelog.d/bare-404-route.md`. Кандидат тега `v1.6.0-rc.1` теперь `84ec2f6`. Режим — только чтение, без сети; тесты гонялись в отдельном worktree.

**Код.** Повтор с обновлённым токеном вынесен в `renewing`, у `RoundTrip` теперь один выход. `RouteNotFoundAsBadGateway` применяется к ответу, который уходит клиенту, каким бы путём он ни пришёл. Проверено по путям:
- первый ответ — как прежде;
- повтор после 401 — голый 404 теперь 502 с `Evo-Replaced-Answer`;
- ошибка и `nil` — сработает охранник `err == nil`;
- `Keep404` действует на обоих путях.

Побочных эффектов нет:
- 404 и 401 не пересекаются, двойной замены нет: `HTMLAsBadGateway` трогает только 2xx, D63 — только 404;
- тело 401 закрывается в `renewing`, тело 404 читается один раз;
- HEAD исключён;
- без `OnTokenRenewed` путь не меняется;
- для ответов не-404 порядок проверок эквивалентен прежнему.

**Тест.** `TestRouteNotFoundAfterTheRenewedToken`: 401 на старый токен, `text/plain` 404 на новый, ожидается 502 с заголовком. С `httpretry.go` из `main` тест падает, с правкой проходит. Зелёные: `go test -race ./internal/httpretry`, `go test ./...`, gofmt, `go vet` (linux, windows), `changelog.py check`.

**Фрагмент.** Добавлено «в том числе после повтора с обновлённым токеном». Код 5, подсказка и «Что поменять» прежние. Это закрытие пробела в собственном утверждении D63 («одна точка — транспорт»), а не новое ломающее изменение. Строка 1.6.0 в `RELEASING.md` верна без правки.

Секреты и согласие правка не затрагивает. Новых находок нет.

## Дополнение: rc.2 (v1.6.0-rc.1..68759235)

**Вердикт: блокеров тега нет.** Пять находок: FB160-9 — средняя, остальные
низкие, FB160-12 — гипотеза. По решению владельца FB160-9 и FB160-11 правятся
до тега rc.2 (#597, п. 31), поэтому кандидат rc.2 сменится и по diff правки
будет ещё одно дополнение. Режим — только чтение, без сети. Тесты гонялись в
отдельном worktree на `68759235`.

**Diff.** Кандидат — `68759235` (#688). От `v1.6.0-rc.1`: 13 коммитов, 40
файлов, +602/−98.
- Продуктовые PR (5): #676 (D65), #681 (`subnet_create` без `prefix_length`),
  #684 (D66), #687 (справка, без Telegram), #688 (группы справки — продукты).
- «(процесс)» (8): #677, #678, #679, #680, #682, #683, #685, #686 —
  продуктового кода не трогают. #677 правит ожидание cloud-e2e по D57 под код
  `resolveRefs`. #678 и #685 — пробы; #685 под словом `dnslink` делает прямые
  DELETE и PATCH по решению владельца (#597, п. 27).

**Проверки на `68759235`.**
- `gofmt -l` — пусто; `go vet ./...` (linux, `GOOS=windows`) — чисто.
- `go test ./internal/...` — 45 пакетов ok; `-race` на `internal/tools`,
  `internal/dns`, `internal/cli` — ok; `changelog.py check` — ok.
- Утечек в добавленных строках нет: UUID нулевой формы, адреса тестовых миров,
  имена секретов workflow без значений.
- У каждой продуктовой правки есть тест, падающий без неё.

| Пункт | Итог | Доказательство |
|---|---|---|
| Согласие, ось 1 | да | D66: отказ — ошибка планировщика `planDnsZoneDelete` (`tools_dns.go`), гейт `gated` отвечает ею до `confirmed`; тест — с `confirmed` и без, DELETE не уходит. D65: `dnsServerIn` до `confirmed` — один из объявленных inline-гейтов (`testrules/gate_test.go`), не новый |
| Секреты | да | в новых текстах — имена и id VPC, домен, текст ошибки операции, `operation_id`; имена VPC без кавычек — FB160-9 |
| Паритет | да | один обработчик реестра; `TestParity_*`, `TestParity_JSONEqualsTool` зелёные; мир паритета дополнен `GET /v1/dnsServers` |
| Уборка, ось 3 | да, с оговорками | повтор — `already_deleted` (D26); `deleted` — только `done` без `error`; `error` — `API_ERROR`; таймаут, 429/5xx — `deleting` с `operation_id`; Ctrl-C — FB160-11 |
| Честный ход, ось 2 | да | общий `waitForOpJSON`: «ожидание операции… N с (состояние)» одинаково в CLI и MCP |
| Полнота, ось 4 | да | у `subnet_create` нет аргумента `prefix_length`, маска — в `cidr`; опись `api-contract.json` объявляет пропуск с причиной; D65: превью называет `vpc_id`, `vpc_name` |
| Миграция ↔ DECISIONS ↔ RELEASING | да | D65 и D66 — строки 1.6.0 `RELEASING.md`, `DECISIONS.md`, «Было/Стало/Что поменять»; #681, #687, #688 — не ломающие (`evo version --json` не менялся); неточный текст — FB160-10 |
| Wall-clock | да | ожидание — `waitForOpJSON`, объявлен в `testrules`; значений времени в ответе D66 нет |

**Сверка охвата («Процесс · 1.6»):** у каждого продуктового изменения — тест,
падающий без правки (`dns_linked_vpc_test`, `dns_zone_links_test`,
`subnet_test` и мир паритета, отвергающий оба поля, `help_groups`,
`help_products`, `i18n_help`). Вживую: D65 и D66 — run 154, `subnet_create` —
run 153; справка облака не касается. Пробелы, не блокирующие, — в бэклоге
процесса: отказы D65/D66 в мирах паритета не проверены (гейт общий, риск
низкий), `productNames` — префикс (FB160-13), итог операции в асинхронных
удалениях вне dns — строка 1.7.

### Находки

- **FB160-9 (средняя; до тега rc.2, #597 п. 31).** `dnsServerIn` и
  `dnsZoneLinked` (`internal/tools/tools_dns.go`) подставляют имя VPC из
  облака в `message` без `quoteCloud`. Правило `untrusted.go` требует одну
  строку в кавычках, соседний код его соблюдает (`tools_subnet.go`,
  `tools_disk.go`, `delete_preview.go`, `ref_by_name.go`). Инвариант
  `TestUntrustedNames_OneLineInText` не поймал: его мир отдаёт зону без
  `linkedVpcIds` и список без `vpcs`, до этих текстов не доходит. Код выхода и
  `reason` не меняются. Ускользнувший класс «имя из облака в новом тексте»:
  инвариант есть, но его мир не дошёл до нового кода.
- **FB160-10 (низкая; в той же правке).** Поле `feedback` и бывшая «Обратная
  связь» живут в скрытой `evo crew`, а фрагмент `root-help-welcome.md`,
  комментарий `product.go`, тест и строка плана #686 называют `evo version`.
- **FB160-11 (низкая; до тега rc.2, #597 п. 31).** Отмена (Ctrl-C) в ожидании
  операции D66 сводится к ответу `deleting` с кодом 0, а не к «прервано», как у
  `*_operation wait_seconds`. Ложного успеха нет, но скрипт не увидит код
  прерывания.
- **FB160-12 (низкая, гипотеза).** Две вещи видны только в облаке:
  положительный путь D65 (список DNS-серверов не пуст) и «`deleted` = зоны
  нет». **Закрыто run 154** (проба п. 27, отчёт — в закрытом репозитории):
  зона создана, итог операции DELETE со связями — ошибка (evo ответил бы
  `API_ERROR`), после снятия связи `deleted` совпало с отсутствием зоны (1 из
  1).
- **FB160-13 (низкая; бэклог процесса).** `productNames` в
  `TestHelp_RussianEverywhere` — префикс «Evolution » плюс два имени, а не
  закрытый список. Существующие заголовки держит
  `TestRootHelp_GroupsAreTheProducts`; дыра — новая английская группа на
  «Evolution ».

**Решение владельца по оси 3 (#597, п. 32).** evo сам привязывает приватную
зону к VPC (D65), а удалить её со связью не может (D66 — отказ с путём).
Снятие связи самим `dns_zone_delete` — в 1.7, после второго наблюдения.
