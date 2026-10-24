# Дельта-аудит перед стабильным 1.3.0 (Fable, 24.10.2026)

Чек-лист `CLAUDE.md` §5: дельта-аудит по diff `v1.3.0-rc.1..main` (rc.1 —
`93bde8b`, rc.3 — `22a0d47`, `main` — `90648d8`; после rc.3 — только
процессный #324). Только чтение, отдельная сессия; в облако не ходил.
Прочитан продуктовый diff: гейт `internal/tools/gate.go`, `created.go`,
`argcheck.go`, все `tools_*.go`, `internal/engine/{deploy,deploy_preview,
destroy,network,create_preview}.go`, `internal/stack/store.go`,
`internal/mcp/{server,readonly}.go`, `internal/product`,
`internal/cli/{main,version}.go`. Локально зелёные: `TestConfirmedReadOnlyInTheGate`,
`TestConsent_RefusalWithoutWriteComesBeforeTheQuestion` (W8, `lateRefusals`
пуст), `TestParity_*`, `TestContract_*`, `TestReadOnly_*`,
`TestAlreadyDeleted*`, `TestCreated_*`, `TestCreateNames*`, `TestGate*`,
`TestSchemaRules*`, `TestStack_StepIDsAreReal`, `TestFault_Matrix`.

**Вердикт: стабильный тег разрешён после FB130-1.** Решение владельца 24.10:
FB130-1 исправить до тега; FB130-3 и FB130-5 — текст CHANGELOG и RELEASING
в PR версии (#295); FB130-2, -4, -6, -7 — план 1.4. Догфуд и атака Grok
(волна 3) — на стабильном 1.3.0.

## Находки

| # | Серьёзность | Суть | Решение |
|---|---|---|---|
| FB130-1 | средняя | `fip_attach` принимает адрес, который уже занят (`in_use`, привязан к другой ВМ): планировщик проверяет только `OnNATGateway()` (`internal/tools/tools_image.go`, `planFipAttach`), отказ облака — после согласия (класс K1). Что сделает платформа — отказ или перепривязка чужого адреса — не проверено. W8 слеп: в мире паритета есть только свободный адрес (`freeFIPCloud`) | до тега: занятый адрес — `USAGE` до вопроса, как у `release_floating_ip` (D31); вариант мира «адрес занят» |
| FB130-2 | средняя | среди оставшихся `inlineGates` — `image_create` с поздним отказом, который облако знает без записи: гейт до аутентификации, `disk_id` не читается до вопроса (нет диска, другая зона, не `available`) — отказ после «да». Денег не тратит | 1.4: первым из `inlineGates`, затем `s3_put` (бакета нет) |
| FB130-3 | низкая | `evo tools call destroy` без развёртывания: код 3 → 0 (W5, вариант б) не назван в «Миграции» и RELEASING | в #295 |
| FB130-4 | низкая | `open_ports`, когда все порты уже открыты: согласие спрашивается на пустое действие (`will_open: []`); зеркальный `close_ports` отвечает без вопроса | 1.4: ответ без вопроса — смена ответа MCP, нужен номер D и «Миграция» |
| FB130-5 | низкая | формат `--json` стека (W1): в `outputs` — путь/ключ/id вместо `"ok"`, в `deleted`/`failed` — ключ шага, шаг без id — `error` вместо `done`; в «Миграции» не было | в #295 |
| FB130-6 | низкая | статические правила вне схемы (D36): `disk_resize`/`disk_create size_gb`, `kafka_create brokers`, `redis_create shards`, `k8s_create master_count` — агент не видит границ в `inputSchema` | 1.4: `atLeast`/`inRange` в схеме |
| FB130-7 | низкая, гипотеза | `ca_deploy` на гейте, но планировщик не читает облако: занятое имя приложения — после согласия (`lostCreate` уже ищет по имени) | 1.4: `takenName` по `ca_list`, если платформа отвергает дубль — проверить живым прогоном |

Гипотезы без живой проверки (не для реестра расхождений без прогона):
`snat_create` — второй шлюз в зоне при свободном имени (NET-14); `s3_put` —
бакета нет; `dns_zone_create`, `cert_import`/`cert_issue_le` — занятое имя
после согласия (у сертификатов задокументировано в D28).

## Проверено, замечаний нет

1. **Согласие (D35).** `confirmed` читается только в `gate.go`;
   `inlineGates` — 31 строка, все «1.4», список только усыхает; W8:
   `lateRefusals` пуст. По каждому гейту выполнение не добавляет проверок
   сверх планировщика: удаления (`deletePlan`: GET → `already_deleted` /
   `NOT_FOUND` родителя → DELETE), `provision` (сеть зоны — `checkNetwork`),
   `deploy` (`engineGated`: CLI — `Caller.Gate`, MCP — превью с сетью),
   `disk_resize`, `open_ports` (группа, одноимённые — `USAGE`),
   `kafka/redis/pg_create` (`specStorage`), `dns_record_create`,
   `cert_private_key`, `s3_bucket_delete`, `fip_attach` (кроме FB130-1),
   `destroy` (без развёртывания — `status none`).
2. **Секреты.** Пароль `pg_user_create` и ключ `cert_private_key` не
   попадают в превью; `secret_create` вырезает payload. CI: вывод evo в
   `cloud-e2e.yml` — через `redact.sed`, проба DB-22 — `::add-mask::`,
   обёртка stderr. Реальных ID, IP и ключей в diff нет.
3. **Паритет.** `close_ports` — `CLISpec`, `descriptions_ru.go`, документы
   перегенерированы; `CheckArgs` — первый слой при любых флагах (D33),
   правила схемы (D36); `jsonAliasCompat` не менялся, новых H нет.
4. **Идемпотентность и уборка (ось 3).** `createdIDs` — один источник для
   `lostCreate`, шагов стека и `stack_destroy` (старые `"ok"` — поиск по
   имени); подтверждённое удаление — GET, затем DELETE; поправка D36 — форма
   id до облака у 14 удалений.
5. **«Миграция» ↔ DECISIONS ↔ RELEASING.** D33, D35 (W5–W7), D36 совпадают
   по трём документам; D34 и D37 не ломающие — верно. Не названное —
   FB130-3, FB130-5.
6. **Новые инструменты.** Только `close_ports`: три оси §6 выполнены.
