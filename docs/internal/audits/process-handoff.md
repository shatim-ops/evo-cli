# Состояние роли «Процесс и тестирование»

Файл состояния роли (`CLAUDE.md` §0, правило 5). Новая сессия роли читает его
после `release-plan.md`, последнего `handoff/<версия>.md`, `DECISIONS.md` и
`RELEASING.md`. Обновляет сессия роли при каждом заметном изменении и перед
закрытием. Одна страница, без внутренних сведений Cloud.ru.

Роль: как мы проверяем, что evo не сломан. Тестовая инфраструктура, CI и
workflows, аудиты процессов, брифы Grok (только в закрытом `shatim-ops/evo-internal`: решение владельца 27.10, #411, `CLAUDE.md` §0), проверка цепочки поставки
на rc, падения CI не от diff. Продуктовый код не меняет: дефект продукта —
падающий тест и диагноз для «N — разработки».

Роль сквозная, сессия — на версию («Процесс · N», `CLAUDE.md` §0, правило
5): перед закрытием сессия обновляет этот файл, следующая начинает с него и
берёт задачи из бэклога, а не по памяти.

Правила ролей и процесса здесь не повторяются — только ссылки на
`CLAUDE.md`, `DECISIONS.md` и `RELEASING.md`: копия расходится с источником.

Обновлено: 29.10.2026, «Процесс · 1.5 (2)», передача «Процессу · 1.6» (раздел «Итог «Процесс · 1.5 (2)»»). Раньше в тот же день — «Процесс · 1.4.x и 1.5», передача (раздел «Итог 1.5»); раньше в тот же день, после тега v1.5.0-rc.1: строки 1.5 — с номерами PR (#508, #518, #522, #526), ретро 1.5 (`handoff/1.5-journal.md`), бэклог 1.6. Раньше — 27.10.2026, «Процесс · 1.4.0», передача дел после тега v1.4.0 (раздел «Итог 1.4.0»). Раньше — 25.10.2026, «Процесс · 1.3.1 и 1.4», стоп по решению владельца (дальше — догфуд и сбор обратной связи): матрица и A/B пробы GC, мир 4xx (#350), язык обёрток — инварианты (#351, #353, #358), часть 3 (#361), исправление #354; шаблон Jira (#355). Раньше — 24.10, «Процесс · 1.3 (2)».

## Сделано

Аудит процессов 1.2.1 (`process-1.2.1.md`, бриф и этапы рядом):

| Пункт | Что | PR |
|---|---|---|
| Первая волна (этапы 3–4) | тестовые тайминги, автозапуск Qwen, триггеры CI, часы стенки, ссылки на D, бюджет `tools/list`, цена в превью, `SECURITY.md`, actionlint, классы PR, расписание cloud-e2e, еженедельный `-race` | слиты до 1.2.3 |
| 6.1 матрица сбоев | `TestFault_Matrix` по реестру (`TestFault_MatrixCoversTheRegistry`, закрытый список `faultMatrixOut`), миры `faultCloud` и `faultAPI`, `faultSlow`; 24 инструмента, 514 точек; находки — D25, D23, D28 | #211, #212, #218, #222, #230, #239, #240; таймауты #223, один транспорт повторов #224 |
| 6.2 имена из облака | `_untrusted`, имена в кавычках | #216 |
| 6.3 fuzz | еженедельный fuzz всех целей | #207 |
| 6.4 цепочка поставки | SBOM архивов, attestation происхождения, cosign образа | #228; на `1.3.0-rc.1` (23.10) пройдено: 6 архивов и 6 SBOM (SPDX-2.3) в `checksums.txt`, `cosign verify-blob` его подписи, `sha256sum -c`, `gh attestation verify` (`release.yml@refs/tags/v1.3.0-rc.1`); образа нет по D32 — его подпись не проверяется |
| 6.5 классы PR по реестру | `classify_registry.sh` | #208 |
| 6.6 лимит тела HTTP | 4 МБ по HTTP, 16 МБ stdio (D24) | #226 |
| 6.7 бриф Grok | роль и бриф атаки 1.3 | #229 |
| Номера D | резерв строкой в `DECISIONS.md` при выдаче (правило 4 §0, решение владельца 23.10); `TestDocs_DecisionReserves`: резерв без версии, рядом со строкой решения или после выпуска версии роняет тест | #243 |
| Состав репозитория (FB13-1 аудита Fable) | `internal/repolayout`: каталог или файл в корне вне `rootDirs`/`rootFiles` и OpenAPI/Swagger-файл (по имени или ключу верхнего уровня) вне `apiSpecs` роняют тест; файлы — как их увидит `git add -A`; `cloudru-specs.py` не принимает опцию за OUT_DIR | этот PR; каталог `--help/` удаляет `claude/rm-stray-specs-1.3` |
| Детерминизм | `TestDeterminism_ListOrderDoesNotChangeTheAnswer` (мир двойников) | тест в #236 у «1.3 — разработки» (D27) |
| W8, ось 1 контракта (K1, G13-3) | `TestConfirmedReadOnlyInTheGate` (`internal/testrules`, `inlineGates`), `TestConsent_RefusalWithoutWriteComesBeforeTheQuestion` (`internal/cli`, `lateRefusals`); оба перечня только усыхают, их вычёркивают PR W4–W6 | #300 |
| Мир HTTP-сбоев удаляет (P0 к A1) | DELETE элемента коллекции по самой длинной коллекции-префиксу, DELETE объекта S3, листинг по prefix; `TestFaultAPI_DeletesWhatItKeeps` | #301 |
| Проба копий PG после удаления (DB-22, G13-27) | шаг `dbaas-pg`, `pg-backup-probe.sh`, `TestWorkflows_PGBackupProbeAfterDeleteChangesNothing`; живой итог — прогон cloud-e2e на теге rc.2 | #303 |
| Ось 3 контракта: объявление созданного (K2, G13-14) | `TestCreated_EveryCreateDeclared` (create ⇔ `createdIDs`, одна форма, `lostField()` — поле верхнего уровня, delete объявлен, каждый шаг стека находится по имени и удаляется), инвариант (c) матрицы сбоев `checkCreatedID`, `TestCreatedFakes_AnswerTheDeclaredID` (фейк create в тестах отвечает объявленным полем id; нашёл `TestQA_StackConcurrentCranksCreateOnce` с `id` вместо `cluster_id`) | #305 |
| `lostCreate`: инструмент назван литералом и объявляет `lostField()`, `list`, `field` | `TestCreated_LostCreateCallersDeclareListAndField` (AST, без ручного списка; просьба «разработки» из ревью #306) | #308 |
| Мир HTTP-сбоев отвечает на GET элемента (W4: удаление читает ресурс до DELETE) | `item`/`writeItem` — один просмотр под `a.mu`; `TestFaultAPI_DeletesWhatItKeeps` | #309 |
| Миры под W5 и W9 (в PR «разработки», без своего PR) | `open_ports` и `close_ports` в паритете — группа зоны `SSH-access_ru.AZ-1` (есть в обоих мирах); `paritySetup["close_ports"]` с правилом порта 80; фикстуры превью в `internal/tools`; D26 для удаления по списку портов (`goneArgs`, `goneWorld`, `namesTarget`), `faultMatrixOut`, `listedCases` | коммиты `d1b96ee`, `bfe9250`, `11b345e` |
| Перед тегом и после 1.3.0 (сессия «Процесс · 1.3 (2)») | `close_ports` в живом прогоне и `RULE_SEEN` (#318); Linux CI, release и cloud-e2e на `ubuntu-24.04`, проба `wrapper-ubuntu-latest` (#321); проба DB-22 с причиной и маской до обрезки (#323); причина отказа evo в cloud-e2e — из stderr через `evo_err.sh` (#327); инвариант сценарных скиллов `TestSkills_*`, `skills/` в `repolayout`, поле «Сценарные скиллы» в чек-ине (в #328 «разработки»); «статус раньше текста» `TestStatusBeforeText_*` (в #330, D26 для DB-13 закреплён впервые); брифы Grok `grok-live-1.3.0.md` и волны 3 (#331); бэклог (#335); проба GC на Windows (#337) | см. PR |
| Паузы повторов `minio-go` в мирах S3 (1.3.1, бэклог «Процесса») | клиент S3 повторял 429, 5xx и HTML-ответ до десяти раз с паузами 200 мс … 1 с на своих часах, и ещё одна пауза шла после последней попытки; в `TestContract_APIStatusReason` каждый S3-случай на 429/500/502/html жил до конца контекста (28 × 400 мс = 11,2 с из 15,9 с теста), а его `reason` не проверялся («Не покрыто»). `s3_retry_test.go`: `init` тестового пакета `internal/tools` — `minio.MaxRetry = 1`, `minio.DefaultRetryUnit = 0` (прод не трогается); пропуск S3-случаев снят — `reason` на 429/500/502 теперь сверяется; инвариант `TestS3World_OneRequestPerFailure` — ни один S3-инструмент не шлёт один и тот же запрос дважды (второй запрос другого вида — своё чтение инструмента, как `HEAD` бакета после неудачного `s3_bucket_create`). Тест стал 4,4 с вместо 15,9 с; гипотеза подтверждена, 28 из 28 | #348 |
| Атака Grok 1.3, волна 1 (цели A–I, `grok-attack-1.3.md`) | отчёт 23.10: «критично» и «высокая» нет; «средние» G13-1 (проверка аргументов после запрета режима сервера) и G13-2 (инструкция сервера о данных ресурсов) исправлены «1.3 — разработкой», D33; инварианты `TestArgs_CheckedFirstInEveryMode`, `TestInitialize_InstructionsSayCloudFieldsAreData` | #288 |

## Открыто

- **Живой прогон Grok на 1.3.0 (волна 3).** Брифы в `main` (#331):
  `docs/internal/briefs/grok-live-1.3.0.md` — порядок работы (чистая
  установка, снимок проекта и правило 0 «удаляешь только своё», матрица
  покрытия, L1–L10 и S1–S15, свои сценарии агента, скиллы с ними и без,
  реестр расхождений `G130-API-<N>`, треки и координатор) и
  `grok-attack-1.3-wave3.md` — цели A–G. Отправляет и запускает владелец;
  деньги не ограничены, проект — тот же тестовый (решение владельца 24.10,
  предложения релиза о потолке расхода и пустом проекте не приняты). Ключ
  оператора владелец передаёт Grok отдельно и отзывает после. Отчёт —
  владельцу; «Процесс» по нему: ускользнувшие находки → инварианты (§5),
  итог скиллов — к порогу §10 спецификации вместе с догфудом.
- **Догфуд 1.3.0** — ждём обратной связи от агентов и людей (решение
  владельца 24.10, вечер); поле «Сценарные скиллы» в `checkin.yml`.

- **Атака Grok, волна 2** — отчёт разобран 24.10: решение владельца —
  `1.3.0-rc.2`, дельта-аудит Fable, повторный короткий прогон Grok, затем
  стабильный тег (план W1…W8 — `release-plan.md`, «1.3»; ход —
  `handoff/1.3.md`). Часть процесса — W8 (этот PR): инвариант гейта
  `TestConfirmedReadOnlyInTheGate` (`internal/testrules`, перечень
  `inlineGates` — 67 обработчиков на старте, 44 после W4 (#312), 40 на ветке
  W5; только усыхает, `inlineGatesLeft`) и
  `TestConsent_RefusalWithoutWriteComesBeforeTheQuestion` (`internal/cli`,
  мир JSON отвергает каждую запись; 295 случаев по 67 инструментам;
  `lateRefusals` — 5 случаев: два у W5, три у W6 (`cert_private_key`,
  `dns_record_create`, `fip_attach` на несуществующем id; назначил
  релиз). PR W4–W6 «разработки» вычёркивают строки в обоих перечнях.
  Видит только смоделированное миром; правила 4xx платформы — мир 4xx в
  1.4. Инварианты K2 после A1 — «Сделано».
- **Проба копий PG после удаления кластера** (DB-22, G13-27; решение
  владельца 24.10 — в прогоне cloud-e2e на коммите тега rc.2): шаг job
  `dbaas-pg` после удаления, `.github/scripts/pg-backup-probe.sh` —
  `pg_backups` evo и два GET API по ID удалённого кластера; строка «DB-22
  проба: …» уходит «1.3 — релизу» в реестр как «N из M». По спецификации у
  копии нет DELETE и списка по проекту; восстановление по `backup_id` в
  новый кластер не проверяется (платно).
- **Правило «бюджет ожидания — в попытках»** (решение владельца 23.10,
  ускользнувшее из #277, исправлено в #284): тест, который ждёт успеха
  после N опросов фейка, задаёт бюджет попыток, а не миллисекунды Timings.
  Правило — у «1.3 — разработки»; детектор — `weekly-race` (#286), миры
  engine и tools ×20 на Windows без `-race`. Кандидаты от «разработки», не
  проверены: `waitVMGone` (`DestroyPollTimeout`), ожидание удаляемой ВМ
  (`DeletableWaitTimeout`), Docker (`DockerWaitTimeout`),
  `deployplan/apply.go`, `tools_disk.go`, `opwait.go`. Пол в 200 мс для
  `…Timeout` в тестах проверен и отклонён: на `main` 37 срабатываний, почти
  все — миры сбоев, где тайм-аут обязан сработать; по тексту теста их не
  отличить от случая #277.
- **Qwen не ревьюит новые пуши в открытый PR** (24.10, `439ea2e` в #294
  слит без ревью). Не сбой доставки: `qwen-review.yml` срабатывает на
  `opened` и `ready_for_review`, повтор — командой `/qwen review` от
  владельца. Был вопрос: сессия пишет `/qwen review` после
  каждого пуша в свой PR, или workflow добавляет `synchronize`. Решение
  владельца 24.10: сессия пишет `/qwen review` после каждого пуша в свой
  PR, workflow не меняется. События ревью к тому же доходят не всегда (23.10 —
  #275, #276, #278): после пуша — ручная проверка комментариев через
  15–20 мин, без ответа на находку PR не сливается.
- **Этап 5** — проверка первой волны отдельной сессией Fable (открывает
  владелец). Решение владельца 23.10: до `1.3.0-rc.1`, а не вместе с полным
  аудитом 1.3.
- **Этап 7 (1.4)** — нагрузочный тест на фейке, бенчмарки перед minor,
  потолок стоимости как инвариант (`process-1.2.1-stages.md`).
- **CI не от diff: падение рантайма Go на Windows под `-race`.**
  `0xc0000005` в `adjustdefers` при `shrinkstack` во время сборки мусора, в
  `internal/tools`: #226 (22.10) и #219 (23.10, PR без `.go`-файлов); оба
  раза другие ОС зелёные. Гипотеза: [golang/go#79249](https://github.com/golang/go/issues/79249) (вытеснение потока
  Windows внутри обработки исключения, исправление — Go 1.28). Обход
  `GODEBUG=asyncpreemptoff=1` на Windows (#245) снят решением владельца
  23.10: эксперимент #262 показал, что флаг растягивает хвост задержек в
  10–20 раз (`state.Update` p90 142 мс против 10 мс), отсюда
  нестабильный `TestFault_Matrix/provision` (2,6–4,9 с, #255).
  `TestWorkflows_RaceKeepsAsyncPreemption` не даёт вернуть флаг. Третий
  случай падения — разбор (стек, версия Go, совпадение с #79249), а не
  настройка CI; переход на Go 1.28 — проверить, ушло ли.
  Третий случай — #267 (23.10, Go 1.26.10, job 120509398814), после снятия
  флага: nil-разыменование в `httpproxy.(*config).useProxy` (стандартная
  библиотека Go, `vendor/golang.org/x/net/http/httpproxy/proxy.go:200`,
  цикл по `domainMatchers`), конфиг которого собирается один раз и не
  меняется. Это порча памяти, а не nil в данных; гипотеза — та же порча
  стека при его сжатии, что в `adjustdefers`/`shrinkstack`.
  Попутно: раз дошло до `useProxy`, на Windows-раннере задан HTTPS_PROXY, а
  миры сбоев строили подсказку по `ProxyFromEnvironment` — ответ зависел от
  раннера. Закрыто: `init` в `fault_fake_test.go` (обе копии) задаёт
  `ProxyFunc` без прокси, `TestFaultWorld_IgnoresRunnerProxy`.
  Замер #272 (закрыт, runs 39982774356 и 39983732660):
  `GODEBUG=gcshrinkstackoff=1` дёшев (`state.Update` p90 9–13 мс против
  8–10), но падения не предотвращает: `go test -race ./internal/tools` на
  Windows — 10 из 11 с флагом, 11 из 11 без. Гипотеза «только `shrinkstack`»
  не подтверждена, флага в CI нет. Частота — порядка 1 на 10–30 прогонов
  пакета. Решение владельца 23.10: до 1.4 наблюдаем (один перезапуск у
  «N — релиза», случай — в «Метрики версии»), в 1.4 — переход на Go 1.27.x
  и замер. Следующий случай — сохранить полный стек из лога job.
  Четвёртый случай — #278 (23.10, job 120602434713): `fatal error: found
  pointer to free object` (`reportZombies` ← `sweep` ← `gcMarkTermination`)
  в `internal/tools`.
  Пятый случай — #293 (23.10, Go 1.26.10, job 120686261372), PR без
  `.go`-файлов: `0xc0000005` в `adjustdefers` ← `copystack` ← `shrinkstack`
  ← `scanstack` фонового сборщика, как в первом; сжимался стек goroutine 1
  (`testing.(*T).Run`, ждёт канал), в работе — `TestUncodedReason`
  (`internal/tools`). Пять случаев за два дня,
  общее — порча памяти рантайма на Windows под `-race`.
  Шестой — #331 (24.10, job 120989449061): `found pointer to free object`
  (`reportZombies` ← `bgsweep`) в `internal/tools`, перезапуск зелёный.
  Чтение `internal/config/private_windows.go` против `x/sys` v0.49.0
  дефекта не показало: дескрипторы копируются в Go-кучу
  (`copySelfRelativeSecurityDescriptor`), `dacl`/`ace`/`sid` — внутренние
  указатели в живой буфер. Проба — `weekly-race` с `windows_gc_probe`
  (#337); срок — 1.3.1 (решение владельца 24.10). Первый запуск (run
  40135541445, с ветки #337): `internal/config` ×200 под `-race` и
  `GODEBUG=invalidptr=1,gccheckmark=1` — ok, 200 из 200 (202 с): гипотеза
  `unsafe` в ACL не подтверждается. `internal/tools` ×20 с `-race` и без —
  ответа нет: оба упёрлись в `-timeout 10m` по умолчанию (дефект пробы,
  исправлен в #337 — `-timeout=150m` у tools, `60m` у config, job 390 мин), падения рантайма за
  эти 10 мин не было. Повтор — новой сессии «Процесса» (решение владельца
  24.10: новых запусков в этой сессии нет).
  **Второй запуск (25.10, run 40163378381, с `main` 80e5ef8, «го»
  владельца):** 5 минут вместо 6,5 часов — оба шага `tools` упали на
  первом из 20 прогонов.
  - `internal/config` ×200 под `-race` и `GODEBUG=invalidptr=1,gccheckmark=1`
    — ok, 200 из 200 (186 с), второй раз. Гипотеза «`unsafe` в ACL
    `private_windows.go`» снята.
  - `internal/tools` ×20 под `-race` — через 20 с `fatal error: checkmark
    found unmarked object` (`gcMarkTermination → runCheckmark → gcDrain →
    scanObject` в `mgcmark_greenteagc.go` → `greyobject → setCheckmark`):
    объект 16 байт (spanclass 4) не помечен, ссылка на него — из объекта
    1280 байт по смещению 0x2f8. Рабочая горутина (runnable) —
    `TestErrResult_HintOfAnUncodedError` (`errresult_test.go:77`) →
    `errResult → errAnswer → uncodedHint → apiStatus`
    (`errreason.go:155`, `errors.As`); табличный тест без `unsafe`, cgo и
    горутин.
  - `internal/tools` ×20 без `-race` — через 27 с `Exception 0xc0000005`,
    `PC=0x80006`: переход по мусорному адресу, у горутины `unknown pc`,
    стек не читается; на стеке следы `encoding/json.typeEncoder`,
    `internal/sync.HashTrieMap.Load`, `runtime.typehash`,
    `runtime.mallocgcSmallScanNoHeader`. Порча памяти **без детектора
    гонок**.
  - То же на Linux (контейнер сессии, amd64, 4 CPU): `go test ./internal/tools`
    с `GODEBUG=invalidptr=1,gccheckmark=1` ×2 — ok; с настройками
    репродуктора [golang/go#82394](https://github.com/golang/go/issues/82394) (`GOMAXPROCS=4 GOGC=20
    GODEBUG=clobberfree=1,gccheckmark=1,invalidptr=1`) ×2 без `-race` и ×2
    с `-race` — ok, на go1.26.0 и на go1.26.10 (версия CI). Падение
    воспроизводится только на Windows; `gccheckmark=1` делает его
    детерминированным за 20–30 с — это репродуктор.
  - Upstream-кандидаты (гипотезы, не подтверждены нашим прогоном):
    golang/go#82394 (21.10.2026, открыт, без CL) — `_panic.fp` при
    `Goexit`/panic держит мёртвый объект стека, GC помечает освобождённый
    объект; go1.22…1.27.3 (в том числе 1.26.9, 1.26.10), amd64 linux и
    darwin, не arm64; триггер — `Goexit` с defer и объектом на стеке
    (`t.Fatal`/`t.Skip`); `GOEXPERIMENT=nogreenteagc` не помогает. Против:
    у них воспроизводится на Linux, у нас — нет. golang/go#79249 (закрыт,
    Go1.28) — асинхронное вытеснение потока Windows внутри обработки
    исключения, порча состояния, редкие `0xc0000005`; обход
    `asyncpreemptoff=1` снижал, но не убирал падения (у нас снят, #262:
    хвост задержек ×10–20). [golang/go#80799](https://github.com/golang/go/issues/80799) (FixPending, Go1.28) — под
    Green Tea отчёт `reportZombies` печатает «unmarked» для всех слотов
    (как в #278 и #331) и `gccheckmark=1` пропускает часть плохих
    указателей для size class 2–26 — это об отчёте, не о причине.
  - Вывод для критерия владельца: падение без `-race` есть, но стеков в
    продуктовом коде нет (ни `unsafe`, ни cgo на пути; в продуктовом
    бинаре `runtime.Goexit` нет, `recover` — один, `callHandler` MCP).
    Это рантайм Go на windows/amd64. «Go 1.27.x в 1.4» — не исправление:
    1.27.3 в #82394 поражён; у #79249 и #80799 исправления — в Go 1.28.
    Затронут ли бинарь пользователя на Windows — неизвестно: та же среда
    выполнения, но без нагрузки тестов и `gccheckmark`; шесть падений 1.3
    случались без `gccheckmark`, в обычном `go test -race`.
  - Предложение владельцу (решение открыто): третий запуск — матрица на
    Windows по `-count=3`: {Go 1.26.10, 1.27.x, tip} × {`-race`, без} ×
    {`GODEBUG` пробы, без} × {`GOEXPERIMENT=nogreenteagc`, без} ×
    {`asyncpreemptoff=1`, без} — какие оси убирают падение, та и обход для
    Windows-job CI до исправления в Go; затем issue в golang/go с
    репродуктором (репозиторий публичный, 20 с на `windows-latest`), по
    «да» владельца. Статус Windows-job в `ci.yml` до обхода — решение
    владельца: как есть с одним перезапуском или `continue-on-error` с
    еженедельной пробой (теряется защита кода `*_windows.go`).
- **CI не от diff: `startup_failure` у повторного запуска** (#219, run
  39917297878: «re-run failed jobs» — ни одного job, логов нет; затем
  полный повтор того же run у «1.3 — релиза» — тоже `startup_failure`).
  «Re-run all jobs» зелёного run 39920181369 (#241) стартовал со всеми
  пятью job. Вывод «разовый сбой» снят. Гипотеза, не проверена: ломается
  повтор run, в котором уже упал job. Проверка — на следующем красном PR:
  сначала «re-run failed jobs», при сбое — «re-run all jobs», затем новый
  run от push; запись — сюда. Пока обход: новый run (push автора или
  вливание `main`), а не повтор.

- **CI не от diff: `contract` отменялся по тайм-ауту** (#245, #217,
  23.10). Полный проход загрузки спецификаций — около 4,5 мин; каждый
  проход падал на одном случайном файле из 27 (каждый раз другом), повтор
  в workflow качал всё заново, три прохода не укладывались в 15 мин, и
  job отменялся, не дойдя до «сверка пропущена». Кэш сохраняется только
  после полной загрузки, поэтому промахивался каждый PR за день. После #219
  (6 сервисов DP) проходов стало больше. Правка (PR «(процесс)», ветка
  `claude/proc-contract-specs-download`, «да» владельца 23.10): скрипт
  повторяет только недостающие сервисы, 4 потока, срок загрузки 10 мин
  (`TestWorkflows_ContractDownloadEndsBeforeTheJob`), офлайн-тесты
  `scripts/cloudru-specs-test.py`. Живой прогон из сессии — 72 с, один
  повтор. Владелец просит следить за дальнейшим поведением: время шага и
  долю пропусков сверки на следующих PR записывать сюда. После слияния
  сверка идёт и на push в `main` (те же пути, мягкое правило PR, кэш `main`
  читают все PR; «да» владельца 23.10,
  `TestWorkflows_ContractOnMainSoftAndWarmsTheCache`): пропуск на PR больше
  не ждёт понедельника.
  Наблюдения 23.10: PR #245 и #217 до правки — отмена через 15 мин; PR #247
  — 1 мин 33 с; первый push в `main` после #248 (run 39950146523) — 2 мин,
  загрузка 1 мин 44 с, кэш сохранён; следующий PR #244 (run 39950399584)
  взял кэш `main` (`Cache restored`) — job 15 с. Дальше: время шага и
  пропуски сверки — сюда при отклонении.

- **stderr evo в публичных логах cloud-e2e и api-probe** (задача процесса
  1.3, `release-plan.md`: ошибка облака из `k8s create` в `k8s-smoke` ушла в
  лог без `redact.sed`, 22.10). Одна точка: `build_evo.sh` кладёт в PATH
  обёртку (`evo_wrapper.sh`): stderr в лог или канал — через `redact.sed`,
  stderr в файл — как есть и синхронно (шаги читают его сразу; в лог файл
  выводится через `redact.sed`) (`TestWorkflows_CloudE2EStderrRedacted`,
  `evo_wrapper_test.sh` в ci).
  Не покрыт `cloud-readonly-os.yml` (Windows и macOS, только чтение) — в
  бэклог. Проверить на первом cloud-e2e после слияния: шаги `logs -f` (код
  130), повторные `*_create` (ID в отказе), `bound-check`.

## Состояние на стоп 25.10 (продолжать отсюда)

- **Язык обёрток ошибок (1.3.1).** Cursor больше не используется
  (решение владельца 25.10); брифы `cursor-error-wraps-*` удалены в #358.
  Инварианты в `main`: `TestErrorWrapsSpeakCLILanguage` (`englishWraps`,
  `copiedWraps`), `TestErrorWrapsKeepTheirEnglish` (английский текст
  заморожен, `internal/testrules/testdata/english-wraps.txt`), ветвь
  `API_ERROR` в `TestQA11_ToolErrorsSpeakCLILanguage`
  (`apiErrorEnglishLeft`). В #358: `TestReason_SameInEveryLanguage` и
  `TestReason_ClassifiersReadBothLanguages` — `reason` одинаков на обоих
  языках; ускользнуло с части 2 (#354, `parseAnswerRE` читал только
  «parse»), нашла «разработка» на ревью.
  Порядок слияния: #358 → #360 (часть 2 — «разработка»; #354 Cursor
  закрыт решением владельца) → #361 (часть 3 — «Процесс» разово по «да»
  владельца; ревью по файлам — «разработка», после паузы). Продуктовый код
  «Процесс» больше не пишет (решение владельца 25.10). Остаются: `internal/mcp` — английский навсегда (D6),
  `internal/stack` — у D41 «разработки».
- **Проба GC на Windows.** Матрица (run 40166544650, `main` `277dfac`):
  0 падений из 144 (Go 1.26.10/1.27.3/tip × greenteagc/nogreenteagc ×
  `-race` × `gccheckmark` × `asyncpreemptoff`). Второй запуск (`80e5ef8`)
  падал на первом прогоне. Между ними — #348 (тесты не ходят в повторы
  `minio-go`: `for range c.newRetryTimer` — итератор range-over-func с
  `time.After`). A/B — run 40170649316 (`main` с revert #348, 1.26.10
  greenteagc; ветка удалена): **0 падений из 24** — гипотеза «путь
  повторов minio-go» не подтверждена. Итог трёх запусков: 2 из 2
  мгновенных падений во втором и 0 из 168 после при тех же настройках;
  ось не найдена, причина не локализована, репродуктора для golang/go нет
  — issue не заводится. Падения рантайма на Windows — редкие, класс «CI не
  от diff»: Windows-job — как есть, один перезапуск у «релиза», случай со
  стеком — сюда; следующий запуск пробы (`windows_gc_probe`) — при новом
  случае. **Седьмой случай — сразу после (25.10, CI #359, только
  документы, run 40173620536, job 121109552888, `main` `3c57784`):**
  `fatal error: found pointer to free object` (`reportZombies` ← `bgsweep`)
  без `gccheckmark`, в обычном CI; рабочая горутина — `TestUncodedReason →
  errAnswer → uncodedReason → regexp.MatchString` (`errreason.go`).
  Значит, 0 из 168 у пробы — не «ушло»: на обычном CI падения остаются,
  частота — порядка прежней. **Восьмой случай (25.10, CI #363, только
  документы, run 40173887443, попытка 1, job 121110341963, коммит
  `73e1490`):** снова `found pointer to free object` (`reportZombies` ←
  `bgsweep`), без `gccheckmark`; рабочая горутина — `TestFault_Matrix →
  faultToolsIsolate → t.TempDir → os.MkdirTemp`, в системном вызове
  `CreateDirectory`. Попытка 2 — зелёная. Продуктового кода на стеке
  снова нет; пакет тот же (`internal/tools`), тесты другие — ось по
  тесту не видна. Перезапуск job — один, у «релиза». Строка плана «Go 1.27.x в 1.4»: данных за или против нет
  (1.26.10 и 1.27.3 одинаково чистые в матрице).
- **Мир 4xx (#350, в `main`).** Правила D39 и D42; `lateRefusals` — две
  строки D39, вычеркнет #345. Дальше — D40, D41 с PR «разработки».
- **Догфуд 1.3.** Шаблон заявки Jira SM — #355 (`dogfood-jira-template.md`).
  Замер сценариев (поля С0–С13, способ, вызовы, ошибки маршрута, согласия)
  и скрипт сводки Г2–Г7 — не начат, ждёт решения владельца.
- **Брошено/не начато:** сценарий cloud-e2e для G130-08, бриф волны 3
  (оговорки прогона 25.10), пробы NET-16/NET-17, бэклог G130-04/-12.

## Состояние на 27.10 (стоп снят)

План «Процесса» на 1.3.1 и 1.4 (P1–P8) — сообщение «релизу» 27.10, «го»
владельца 27.10; поправки «релиза»: P4 — без проверки версии UUID, P5 — только
храповик.

- **P4** — #379: `TestArgs_IDsDeclareTheirFormat`, список `idWithoutFormat`
  (70 «UUID в описании, не в схеме», 7 с непроверенной формой, 5 своих id
  evo); сокращает «разработка» через `uuidProp`.
- **P1** — падающий тест передан «разработке» через «релиз»: ветка
  `claude/proc-erased-world`, `TestAlreadyDeleted_ErasedClusterIsDeleted`
  (pg, kafka, redis на ERASED: превью с вопросом, запись, у pg подсказка
  «ещё создаётся»). Ускользнул: живой прогон 27.10; инвариант D26 был, но
  «нет» в нём — только 404.
- **P5** — #382: `TestUntrustedNames_Marked`, список `namesUnmarked`
  (52 инструмента); пометку имён в 1.4 делает «разработка».
- **P6** — бриф `grok-live-1.3.0.md`, раздел 6, пункт 2: незаконченное
  удаление повторяется тем же evo, оба ответа — в журнал.
- **P2, P3, P7** — 1.4, строки бэклога ниже (каскад #386: теперь 1.5).
- **P1 закрыт:** N1/D44 слит в 1.3.1, тест `erased_test.go` в `main`
  (ветка `claude/proc-erased-world` больше не нужна).

Срез «сейчас» — `docs/internal/state/process.md` (1.4.0); этот файл —
история и бэклог.

## Итог 1.4.0 (27.10, передача)

| Что | PR |
|---|---|
| Инвариант «CLI `--yes` = MCP `confirmed=true`» `TestParity_YesEqualsConfirmed`; свои id evo в W8 (нашёл `stack_destroy` и `stack_apply`, D47) | #405 |
| Инварианты журнала D46: ответ одинаков с журналом и без, в журнале ни одного значения, выключенный — без следа | #407 |
| `diag-check.py --public`, журнал в cloud-e2e, `TestJournal_PassesDiagCheck` (формат evo ↔ скрипт) | #408 |
| E4: 404 без тела сервиса — храповик `bare404Gone` (18 удалений, E1 в 1.5) | #410 |
| S2: смоук с Origin, 413, `--read-only`; ACL Windows; `TestHTTPRestrictions_EachHasATest` | #412 |
| E7: отказ FM без тела; голова PR по sha при отсутствии `refs/pull/N/head` | #413 |
| E5: форма ответа monitoring и audit, токен в stdin `curl` | #414 |
| `time.Until` в `TestWallClockReadsDeclared`; TTL в ответе — храповик (`Token.TTL` исправлен в #419) | #416 |
| Раньше в версии: `TestArgs_IDsDeclareTheirFormat`, `TestUntrustedNames_Marked`, `TestWallClockReadsDeclared`, догфуд-форма, `TestRoleStateFiles`, смоук-токен | #379, #382, #385, #395, #399, #402 |

CI `main` за 1.4.0 (27.10, `ci` на push): красный один раз — 4019267
(слияние #414), `TestJournal_AnswerSameOnOffAndBroken` на Windows: TTL
токена по часам стенки (3600 и 3599 при тике 15,6 мс); инвариант был (#385),
но не видел `time.Until` — #416, продукт — #419. Канарейка
`wrapper-ubuntu-latest` красная по замыслу. Рантайм Go на Windows — 11-й
случай на PR #408 (не `main`), «как есть до 1.5» (#400). Шесть отменённых
прогонов — вытесненные пуши (concurrency), не красные.

## Итог «Процесс · 1.5 (2)» (29.10, передача «Процессу · 1.6»)

Сессия закрывается вместе с ролями 1.5 (решение владельца 29.10). Срез на
1.6 — `state/process.md`.

- **Сон машины посреди ожидания** (строка бэклога закрыта): миры
  `sleptCloud`, остановленный процесс `evo mcp`, один отвергнутый токен на
  чтении после записи; три дефекта — строками 1.6 «разработке» (#545, #550).
- **Стратегия тестирования** — аудит Fable `audits/process-test-strategy-1.6.md`
  (#578), опрос владельца, правила в `CLAUDE.md` (#579, #586: Qwen само —
  только секреты, согласие и workflows; Windows на PR — только своё; ночная
  полная матрица; «да» в issue или плане; мета-работа раз в версию; окно
  передачи — 24 часа после тега, job `handoff`). Строки 1.6 — #581.
- **api-probe первого блока 1.6** (#588): шаг написан и слит, run 28
  запущен — разбор итога первым делом «Процесса · 1.6».
- **Финал Grok rc.1** — мнение релизу: тег не опирается на Grok; поправки
  реестра учтены (evo-internal #23); бриф 1.6 — с LLM-драйвером.
- **Уроки сессии** (ретро 1.5 у предела слов, счёт в `handoff/1.5.md` сведён —
  сюда): коммит запушен в ветку после слияния PR (#579 → #586) — перед пушем
  проверять, что PR открыт; `pip download` в каталоге репозитория — wheel в
  #588 (строка бэклога ниже); директива shellcheck в начале `run:` действует
  на одну команду — ставить у строки (#588).

## Итог 1.5 (29.10, передача)

Сессия «Процесс · 1.4.x и 1.5» закрывается после тега v1.5.0-rc.1 (решение
владельца 29.10). Сделанное — таблица ниже, ретро — `handoff/1.5-journal.md`.

- **Волна rc.1** — только Grok, бриф `evo-internal/briefs/grok-rc-1.5.0.md`
  (#13, #15). Полная проверка поверхности и цели 1.5 A–K, параллельно без
  лимитов, сон среды — пульс и сверка. Каждая находка — падающий тест
  «разработке» на rc.2; ускользнувшая от тестов — инвариант «Процесса».
- **Ветки 1.6 для «разработки»** (тест с объявленным пропуском, правка
  удаляет строки, устаревшая строка роняет тест):
  #533 — `disk_attach`, `disk_detach`, `disk_resize` в переходном состоянии
  диска отказывают до вопроса и называют состояние (`lateRefusals`); #534 —
  превью 20 из 28 create называет проект (`previewNoProject`).
- **Release:** `cosign initialize` до goreleaser (#530); job `image` — одна
  подпись, без гонки.
- **Windows CI** (#529): обход и повтор по сигнатуре; первый недельный счёт
  `runtime-crashes` — воскресенье.
- **`dbaas-pg`:** `evo-e2e-pg` в FAILED и после удаления в консоли; имя
  кластера — `CLUSTER` job `dbaas-pg` в `cloud-e2e.yml` и списки
  `cleanup_only`, смена — по слову владельца.

## 1.4.x (сессия «Процесс · 1.4.x и 1.5»)

| Что | PR |
|---|---|
| 1.4.1, п. 0: красные тесты до продуктовых PR — W8 «значения по умолчанию из конфигурации нет» и «нет пустой подстановки», ответ на запрос с `id`, `ExpiresAt` без монотонной части, `error_reasons` шага стека, мир «401 Jwt is expired» в долгой сессии | ветка `process/1.4.1-invariants` → «разработка» |
| Проба IAM: `expires_in` против `exp`/`iat`, сдвиг часов раннера — совпадают, 0 с (run 40669740261; реестр, «Снятые вопросы») | #436 |
| 1.5: канарейка `wrapper-ubuntu-latest` — `::notice` вместо красного; `TestWorkflows_NoRedByDesign` — ни один job с `continue-on-error` | #454 |
| 1.5, ось 2 как класс: `TestWaits_SayWhatTheyWaitFor` — каждый цикл ожидания продукта сообщает ход или объявлен в `silentWaits` с причиной; сообщение — внутри цикла, который ждёт; пробелы — «разработке»: `Engine.waitVMGone` (до 2 мин, `destroy vm_id` по MCP), `waitForSSHPort` (до 2 мин, `deploy_apply`), `Engine.settledBootFIP` и `Engine.waitBootFIP` (до 1 мин каждый, `deploy`), `appendUserKeys` (до 30 с: 4 попытки через 10 с, `deploy_apply` с ключами человека); строка плана 1.5 «Ожидания без хода (ось 2)» — #457. Мир «диск создаётся долго» уже есть (`TestDiskWait_*`, N3 1.4.0) | #456 |
| 1.5, миры 4xx (ось 1): `TestConsent_EveryHandled4xxIsARuleOfTheWorld` — каждый 4xx записи из `scripts/api-contract.json` («statuses») либо вариант `consentWorlds`, либо причина (ключ, частота, отсутствующий id, вызов ждёт, чтение); пробелы оси 1 — 10 пар (ВМ в `creating`, имя образа и диска занято, FIP в `error_deleting`, диск не привязан, 422 спецификации без правила) — «разработке» | #458 |
| 1.5, «ресурс был до нас»: `TestCreate_NeverTakesWhatWasThereBefore` — каждый create с поиском по имени (`createdIDs.list`, `lostCreate`), имя занято давно, ответ записи потерян: не называет чужой ресурс своим; присваивают сейчас `disk_create`, `dns_zone_create`, `image_create`, `secret_create` (`takenAdopts`) — «разработке»; мир не доводит до записи `dns_record_create`, `k8s_add_nodepool`, `lb_create` (`takenNotReached`) | #459 |
| 1.4.1, п. 11: инвариант уборки `TestSkills_InventoryIsOnlyTheirs` — скилл, который удаляет найденное, берёт в инвентарь только своё (префикс; ВМ развёртывания с `vm_name` `evo-<prefix>`), источники только читают; ошибочный текст #445 (1a8a308) роняет тест | этот PR |
| Ретро R1–R6: `TestRetro_Rules`, `TestRetro_OneTemplate`; исторические нарушения 1.3.1 и 1.4.0 — в `retroKnown` (сокращается) | этот PR |
| Гипотеза аудита: занятая `journal.lock` задерживает ответ на 2 с — подтверждена тестом | ветка `process/journal-hypotheses` → «разработка» |
| Цель J по `grok-diag-*.jsonl` (счёт без строк журналов) | `evo-internal`, `runs/grok-live-1.4.0/journal-j.md` |
| FB140-6: мир паритета CLI на остановленных часах (`stoppedClock` вместо `jumpingClock`, как в `internal/tools`), `previewToken` от `clock.Now`; маски TTL (`tokenTTL`, удаление `token_expires_at` и `token_ttl_seconds` в `TestEngineCommands_WhoamiDoctorOverHTTP`) убраны; храповик `TestParityWorld_TokenTTLIsExact` — ровно 3600 по stdio и HTTP | этот PR |
| 1.5, Tab (новая поверхность, §0 правило 3): `TestCompletion_EveryReferenceIsCompletedOrSaysWhy` — каждый аргумент-ссылка (`*_id`, `zone`, `flavor`, `bucket`, `key`…) дополняется (`valueRefs`, `idArgs`) или объявлен в `noCompletion` / `refUncompleted` с причиной (вернул вызов, нужен родитель, нет списка, пробел); 10 пробелов «перечислено, не объявлено» — «разработке» | #480 |
| 1.5, мир пустого облака на остановленных часах (`withEmptyCloud`): TTL токена одинаков в двух процессах — `whoami` 3600, `doctor` «valid for 1h0m0s» (`TestParityWorld_EmptyCloudTokenTTLIsExact`) | #492 |
| 1.5, С9 — тесты (#496, FB15-48, 20): `TestDescriptions_NamesExist` — snake_case-слово описаний реестра — инструмент, аргумент или поле кода, иначе строка `descriptionWords` с причиной (13); `TestHints_CallsNameTheToolsArguments` — подсказка `tool(arg)` называет аргумент инструмента; у каждой строки `TestSkills_SayWhatTheCodeDoes` — тест-доказательство или причина, пробелы только сокращаются | #505 |
| 1.5, С9 — доказательства обещаний скиллов: `evo-deploy` «нет образа по умолчанию» (мир `config without CLOUDRU_IMAGE_ID` в `TestConsent_RefusalWithoutWriteComesBeforeTheQuestion`) и «порты — 0.0.0.0/0 в данной группе» (`TestOpen_PortOpensForAnyAddressInTheGivenGroup`); `evo-postgres` «основная подсеть зоны профиля» (`TestDbaasSubnet_MainSubnetOfTheProfilesZone`: без зоны профиля и с первой неосновной подсетью — красный) и «оценка по тарифу Managed PostgreSQL, `tariff_url`» (`TestPgCreate_PreviewGivesAnEstimateByTheTariff`; до #650 — «tariff_url без цены», `TestPgCreate_PreviewGivesTheTariffNotAPrice`); `skillProofGaps` 2 → 0 | #508 |
| 1.5, С2 (#496, п. 7): `TestPreview_HumanShowsEveryValue` — каждое значение `preview.details` изменяющего инструмента (строка, число; `true`/`false` — по подписи поля; запись `params` — значение своего ключа) есть в человеческом превью `--dry-run` команды при `COLUMNS=40` и `200`, иначе строка `humanUnnamed` с классом; служебные ключи (`message`, `status`, `resource`, `key`, `cost_note`, `source`) — форма; без терминала — ни одного ANSI; в терминале превью целиком раньше вопроса, всё в stderr, stdout пуст (кроме шагов команд движка, как с v1.0). Стартовый перечень — 50 строк (работа 17, цена 2, шум 31): человек не видит предупреждение `k8s_add_addon` о неготовых узлах, зону и включённые копии `pg_create`, `update` у `deploy`, источник проекта (`project_from`) у 8 инструментов, почасовую цену. Ширины терминала у evo пока нет — проверка держит будущий рендер (линия CLI 1.6) | #502 |
| 1.5, `dbaas-delete.sh` читает ошибку evo из stderr (#525): `{"error":{reason,message,hint}}` с `--json` — в stderr, stdout пуст (QA-12); скрипт сбрасывал stderr, и ветки «ещё создаётся» и FAILED не срабатывали ни разу (cloud-e2e кандидата 1.5, run 144: кластер PG 2,5 ч в DEPLOYING → FAILED, ложное «ответ evo не называет путь»). Поддельный evo `dbaas_delete_test.sh` писал ошибку в stdout — тест держал неверную модель; теперь как настоящий, плюс случаи «ещё создаётся → повтор → принято» и NOT_FOUND. Класс: `TestWorkflows_EvoErrorIsReadFromStderr` — вызов `evo … --json` с `2>/dev/null` не читает `.reason`/`.error`/`.hint` (четвёртый случай класса: pg_create run 40118457142, DB-22 run 40093421741) | #526 |
| 1.5, `cloud-live.sh`: `disk_detach` — после того, как диск после `disk_resize` устоялся (опрос `settled`), а не через ~1 с (cloud-e2e кандидата, run 142, 422 `disk_can_not_be_detached_from_current_state`; #521, п. 1); `TestWorkflows_LiveDiskChangesFromASettledState` — каждое изменение диска данных в живом прогоне идёт из устоявшегося состояния. Продуктовая сторона (отказ до вопроса) — строка бэклога 1.6 | #522 |
| 1.5, сверка охвата перед кандидатом (§0, правило 3; шаг 0 `RELEASING`): `coverage-1.5.md` — 16 поверхностей `v1.4.1..ddcbfee`, классом держатся 12, точкой — #465, #469, #500, `install.sh`; пробелы П1–П7 не блокируют; `TestSkills_RouteGroupsHoldTheirTools` (группы маршрутов `evo-scenarios` держат инструменты своих скиллов), доказательства обещаний #514 (`TestLBCreate_TakesNoCertificate` и ещё три) | #518 |
| 1.5, С1 (аудит слепых зон §9, #496): `TestPreview_NamesWhatTheWriteSends` — каждое значение тела подтверждённой записи изменяющего инструмента (66, мир JSON-паритета) названо в превью тем же значением (ключ любой глубины, snake_case и camelCase как одно) или аргументом вызова, иначе строка `bodyUnnamed` с классом (доступ / цена / работа / шум / секрет) и причиной; значение секрета, наоборот, в превью не попадает (и в base64, и раскодированным); устаревшая строка роняет тест. Стартовый перечень — 76 строк у 28 инструментов и `projectid` одной строкой на все; доступ — 8 (`ca_deploy` `publiclyAccessible`, kube-API, `ar_create` `isPublic`, `lb_create` публичный адрес, источник `0.0.0.0/0` у `open_ports` и `deploy`, группа узлов k8s, UI Kafka); перечень сокращает С4 «разработки» | #499 |
| 1.5, рантайм Go на Windows (#493): история CI 22–28.10 — 14 сбоев на 828 попыток `test (windows-latest)` (1,7 %), все в `internal/tools`, две сигнатуры (`found pointer to free object` ← `bgsweep`; `adjustdefers` ← `copystack` ← `shrinkstack`), на macOS и Linux — 0; `gc-probe` (tools один) — 0 из 192: сбой — под нагрузкой параллельных пакетов. Классификатор `go_runtime_crash.sh` (на 77 красных логах 22–28.10 без ошибки), проба `ci-probe` в `weekly-race.yml` (`together`, `split`, `norace`, 6 × 50 прогонов) | #494 |
| 1.5, рантайм Go на Windows, шаг 2 (#493, #370 вариант 1): `go_test_ci.sh` — на Windows `internal/tools` один после остальных пакетов (`split`, проба #494: 0 из 300); сбой рантайма без падения теста — один шаг повтора упавших пакетов, второй — красный; стек через `internal/config` — не повтор. Счёт — шаги повтора всех попыток `ci.yml` за 7 дней, `weekly-race` job `runtime-crashes`, 3 и больше — красный (`go_runtime_crash_week.sh`); `TestWorkflows_RuntimeCrashRetriedOnceAndCounted`. Причина в рантайме не найдена: `split` — обход | #529 |
| 1.6, ось 1 (#521, п. 2): вариант `consentWorlds` «disk in a transient state» — чтение диска отвечает `extending`; `disk_attach`, `disk_detach`, `disk_resize` в `refusedBy` и отказ называет состояние (поле варианта `names`); правило 4xx `detach 422` — этот вариант, а не `ruleGap`; три случая объявлены в `lateRefusals` до правки «разработки» (строка плана 1.6 «Изменение диска в переходном состоянии») | #533 |
| 1.6, П6: `TestPreview_CreateNamesTheProject` — превью каждого create (`contract-map.json`) называет проект конфигурации; 8 из 28 называют (`engine.CreatePreview`), 20 — в `previewNoProject` до правки «разработки» (строка плана 1.6 «Превью create называет проект»); запись П6 в `coverage-1.5.md` уточнена | #534 |
| 1.5, карта контракта §6 «инструмент × ось» из объявлений реестра — `docs/internal/contract-map.json`, `TestContractMap_IsTheDeclarations` | #491 |
| Сверка охвата с поверхностью 1.5 (§0, правило 3; до кандидата rc.1): Tab — объявления (`TestCompletion_DeclarationsHold`), каждая ссылка (`TestCompletion_EveryReferenceIsCompletedOrSaysWhy`), кэш 0600 и тайм-аут (`TestComplete_*`), без журнала (`TestComplete_WritesNoJournal`, #480); вопрос зоны в терминале — `vm create` спрашивает, без терминала и с `--yes` отказ (`TestVMCreate_NoZoneAskedAtTerminal`), `tools call` и в терминале отвечает как MCP (`TestToolsCall_NoZoneNotAskedEvenAtTerminal`, #480), `deploy` в терминале спрашивает и повторяет вызов с выбранной зоной (`TestDeploy_NoZoneAskedAtTerminal`, #480); отказы — все сразу (`TestProvision_AllRefusalsInOneAnswer`, `TestVMCreate_AllRefusalsShown`), flavor (мир W8), имя занято (`TestCreate_NeverTakesWhatWasThereBefore`), удаление отвергнуто (`TestDelete_RefusedInStateSaysSoAndTheWayOut`), ожидания (`TestWaits_SayWhatTheyWaitFor`), STANDALONE (`TestRedisCreate_StandaloneSendsWhatTheAPICreates`) | #480 |
| 1.5, шаг 6 `RELEASING` (ретро 1.4.1): job `handoff` в ci — PR без нового раздела выпуска в CHANGELOG красный, пока у последней версии нет `handoff/<версия>.md` (`handoff_check.sh`, `handoff_check_test.sh`, `TestWorkflows_HandoffBeforeNextMerge`) | #484 |
| 1.5, P7 шаг 1: храповик размера `tools/list` (`TestToolsList_SizeBudget`, база `toolsListBytes`, рост и уменьшение — правкой базы в PR) | #486 |
| 1.5, #485 П1 и П2 (внедряет #372): ревью Qwen само — кроме PR только с тестами и документами без `.github/` и `internal/workflows/`; до пяти находок, critical/high/секреты/согласие/workflows — сверх; «Гипотезы» без ответа; CLAUDE.md §0 правила 2 и 6, §4 (`qwen_review_scope_test.sh`, `TestWorkflows_QwenReviewScope`) | #487 |
| 1.5, S6 «нет телеметрии»: `TestOutbound_OnlyCloudRuOrDeclaredLinks` — адрес вне `*.cloud.ru` и loopback в коде продукта роняет сборку, ссылки, которые evo только печатает, — `linkOnly` с причиной; раздел `SECURITY.md` | #490 |

Отклонение от аудита: он не учёл, что мир держал не стенку, а прыгающие
часы `jumpingClock` (#190) — они роняли неприкрытое маской время всегда.
Остановленные часы так же детерминированы: значение времени одинаково в
обоих вызовах, маска не нужна. Значение по стенке по-прежнему не ловит ни
один из двух вариантов; продуктовые чтения стенки держит
`TestWallClockReadsDeclared`.

## Ретро перед закрытием сессии

Решение владельца 27.10: перед файлом состояния сессия роли пишет ретро —
до 20 минут, до 150 слов, раздел «Ретро» в `handoff/N-journal.md`
(первый — `handoff/1.3.1-journal.md`; ведёт «N — релиз»); в `handoff/N.md` —
одна строка счёта. Пункт без результата не пишется; результат — правило (строка
`CLAUDE.md` или `RELEASING.md` по «да» владельца), проверка (инвариант,
job, трещотка) или строка бэклога с версией; удачный приём без проверки —
«пример, до <версия+1>».

```
### Ретро «<роль> · <версия>» (<дата>)
- Сработало: <что>, потому что <почему> — <ссылка>. Закрепить: <правило | проверка | пример, до <версия>>.
- Споткнулись: <событие> — <причина> — <ссылка>. Результат: <правило | проверка | бэклог <версия>>.
- Дороже всего (токены, время владельца): <что> — <число>. Результат: <…>.
- Повтор из прошлого ретро: <нет | пункт> → решение владельца.
```

## Бэклог процесса

| Приоритет | Что усилить | Причина |
|---|---|---|
| 1 | Тесты по находкам атаки Grok 1.3, волна 2 | `CLAUDE.md` §5: проверенное атакой закрепляется тестом; волна 1 закреплена в #288 |
| 1 | cloud-e2e `1.3.0-rc.1`: сверить состояния floating IP `creating`/`attaching` с живым API | A3, #265 (просьба «1.3 — релиза»): состояния живьём не проверены |
| 2 | Учёт ускользнувших дефектов 1.3 → инварианты | `CLAUDE.md` §5; первый пример — K8S-12 → мир двойников |
| 1 (1.3.1) | Проба GC на Windows, третий запуск — матрица осей (Go 1.26.10/1.27.x/tip × `-race` × `GODEBUG` × `nogreenteagc` × `asyncpreemptoff`, `-count=3`) в `weekly-race.yml` по `windows_gc_probe`; по итогу — обход для Windows-job CI и issue в golang/go с репродуктором (по «да» владельца) | второй запуск (run 40163378381): `tools` падает на первом прогоне с `gccheckmark=1`, с `-race` и без; `config` ×200 ok; Linux чист при тех же настройках на 1.26.0 и 1.26.10 — «Открыто», пункт «CI не от diff: падение рантайма Go на Windows под `-race`», абзац «Второй запуск» |
| 2 | Ожидания `cloud-readonly.sh` (удаление несуществующего, stdio) — на фейке в CI на PR, а не только в живом прогоне | ускользнул: job `readonly` cloud-e2e упал на D26 (#244) — скрипт ждал NOT_FOUND и `confirmation_required`, исправлено в #276 |
| 1 (1.4) | Мир с правилами 4xx платформы (`disk_not_reduce`, `storage < min_storage_gigs`, группа безопасности не найдена, бакет не пуст) и инвариант «после согласия нет 4xx» | K1 (G13-3): W8 видит только отказы evo без записи; правила брать из спецификаций и реестра расхождений |
| 2 (1.4) | Чек-лист полного аудита: «для каждого гейта — что выполнение отвергает до первой записи и отвергает ли это превью» | K1 прошёл мимо полного аудита Fable 1.3; в дельта-аудит перед 1.3.0 пункт задаёт «1.3 — релиз» |
| 2 (1.4) | Job `api-contract`: `format: uuid` в схеме инструмента против спецификации — CI падает, если у параметра пути появится `format` или `pattern`, противоречащий UUID, или пример id не в форме UUID | W3 (D36): решение владельца 24.10 — `format: uuid` у удалений vpc, dns, nlb, k8s, pg, kafka, redis, cert по живым данным, спецификации UUID у них не подтверждают |
| 2 (1.4) | Матрица сбоев: случай манифеста с несуществующим бакетом | K3 (G13-13): бакет в матрице всегда создан заранее (`before: true`) |
| 2 (1.4) | Проба api-probe: формат `cpu`/`memory` у `ca_deploy` — что Container Apps делает с `abc`, `0.25`, `500m`, `1Gi`, `512M`; по итогу «разработка» ставит проверку до вопроса | решение владельца 24.10 (W6 → 1.4, `release-plan.md` #313): правило формата не выводится из спецификации |
| 2 (1.4) | Язык CLI в ошибках `API_ERROR`: приставка evo к ошибке облака (`fmt.Errorf("get vm detail: %w")`, `"create record: %w"`) в русском CLI — английская. Правило: в мире «500» текст до первого «: » перед телом ответа облака — на языке CLI, или AST — литерал `fmt.Errorf` с `%w` в `internal/tools` через `i18n.T` | ускользнул с ревью Qwen на #319 (D6), от «1.3 — разработки»; `TestQA11_ToolErrorsSpeakCLILanguage` (`walkLang`) видит только `USAGE`/`CONFIG_INVALID`/`NOT_FOUND` и превью; две в `fip_attach` исправлены в W6 (`TestFipAttach_ErrorsSpeakCLILanguage`) |
| 3 | Правило запуска волны «агент-оператор» в брифе Grok (1.4): на rc.1 или на rc.2 по итогам волны на чтение | в 1.3 решено владельцем разово (волна 2 на rc.1), в бриф не записано |
| 3 | Локальный shellcheck — той же версии, что на раннере (0.10) | #303: локальный 0.11 не пометил SC2015, CI упал; версию держать в одном месте с CI |
| 3 | `startup_failure` у повтора run с упавшим job | гипотеза; проверка на следующем красном PR (см. «Открыто») |
| 3 | Этап 7 (1.4): нагрузочный тест, бенчмарки, потолок стоимости | `process-1.2.1-stages.md` |
| 3 | Маска stderr evo в `cloud-readonly-os.yml` (Windows и macOS) | `build_evo.sh` — только Linux-job cloud-e2e и api-probe |
| 3 | Весь вывод run-шагов cloud-e2e — через свой shell с `redact.sed` (1.4) | `TestWorkflows_CloudLogsGetNoRawEvoOutput` (#278) видит только команды evo и чтение `.evo*`, не печать переменных |
| 3 | Когда образ вернут (отмена D32): стабильный тег без секретов `CLOUDRU_AR_*` роняет job `image` | сейчас job проходит с notice, и без образа это не видно |
| 3 (не делается, решение «Процесса» 29.10) | Ни один тестовый бинарь не читает прокси раннера: общий инвариант вместо закрепления по пакетам | #271 закрыл миры сбоев; `internal/cli` подменяет `ProxyFunc` по тестам Ответ зависит от `HTTPS_PROXY` раннера, только когда тест сверяет текст сетевой подсказки — это миры сбоев, закреплены (#271, `TestFaultWorld_IgnoresRunnerProxy`); общий инвариант по всем пакетам — много кода на редкий случай |
| 3 (сделано — #562) | `namesTarget` (`already_deleted_test.go`) сверяет цель-массив подстрокой (теперь по элементам, `TestNamesTarget_ElementsAreWhole`): порт «80» найдётся и в «8080» — сравнивать по элементам | находка Qwen на #315 от «1.3 — разработки»; сейчас в мире один порт 80, ложного прохода нет |
| 2 (1.4) | Образ ubuntu-26.04 (`timeout` из uutils 0.10.0): проба с `timeout` на переднем плане, как шаг cloud-e2e `logs -f`; по итогу — переход ci, release и cloud-e2e с `ubuntu-24.04` или правка обёртки | #321: проба в фоне — у evo `SigIgn 0x1006` (INT, QUIT, PIPE), Ctrl-C не дошёл; GNU `timeout` ребёнку INT возвращает, uutils — нет; на переднем плане не проверено |
| 2 (1.4) | `cleanup_only` не стартует, пока идёт другой прогон cloud-e2e (concurrency-группа или проверка в job) | 24.10: субботний `cloud-e2e-weekly` (run 40082315632) ждал одобрения во время живого прогона rc.2; одобренный, он удалил бы ВМ и кластеры `evo-e2e-*` идущего прогона — защита есть только у правил портов |
| 3 (сделано — #562) | `TestRun_GlobalFlagsDoNotOutliveTheRun` — по списку persistent-флагов корня cobra, а не руками (обход `PersistentFlags` корня; мутация «`--region` не сброшен» — красный) | #322: `--project-id` и `--region` не сбрасывались, ручной список теста их не знал; main красный с #319 при `-shuffle` |
| 3 | os-smoke: при таймауте SSH печатать состояние ВМ (`vm_get`: статус, адрес есть) | OPS-1b на rc.2 (nginx-ubuntu-22.04, run 40093421741): лог не отличает «ВМ не загрузилась» от «SSH не поднялся» |
| 2 (1.3.1) | Инвариант скиллов — и на `skills/*/references/*.md`: имена (§8.2), запрещённое (§8.6); согласие — ссылкой на шаг сценария или «after the person's yes» | дельта-аудит Fable скиллов 1.3.0 (через «1.3 — релиз», 24.10): `references/delete-map.md` называет `--yes`/`confirmed: true` без маркера, тест читает только `SKILL.md` |
| 2 (1.4) | Мир согласия: варианты «FIP на NAT-шлюзе» и «у ВМ уже есть FIP» (`refusedBy`) | от «1.3 — разработки» при #326: отказ до вопроса у `fip_attach` в этих состояниях в мире не проверен |
| 3 (вопрос «разработке») | Повторы S3 в проде: `minio-go` сам повторяет 429, 5xx и HTML-ответ до 10 раз с паузами 200 мс … 1 с (и спит после последней попытки) — отказ S3 виден через 5–9 с, тогда как остальные клиенты повторяют по правилам `httpretry` (один транспорт повторов, #224). Одно правило повторов на все клиенты или задокументированное исключение; `minio.Options.MaxRetries` есть | найдено при разборе пауз в мирах S3 (этот PR); не дефект, вопрос паритета правил повторов |
| 1 (1.4) | P2: мир «платформа отвергает удаление в состоянии X» (kafka_delete на FAILED — сырой 500; класс DB-17, DB-21) и инвариант оси 3: такой delete не отвечает сырым `API_ERROR` и не выглядит успехом, ответ называет путь; новый delete без объявленных отказных состояний роняет сборку | `CLAUDE.md` §6, ось 3; живой прогон 27.10; список состояний — из реестра, храповиком; перед началом сверить живой ответ `kafka_delete` на `FAILED` |
| 1 (1.4) | P3: мир «диск создаётся долго» — `openstack_id` появляется через N опросов, по `internal/clock`; `disk_attach` после create ждёт состояние, а не время | живой прогон 27.10: 149 с → 422 «no openstack_id» при ожидании 2 мин; ось 2; лечение — «разработка» |
| 2 (1.6) | P7, шаг 2: храповик ответов списков — вместе с решением D о функции ответа списка (`release-plan.md`, «Функция ответа списка»); шаг 1 — `tools/list` (#486) | 28.10: в мире JSON-паритета строки есть у 32 списков, у 25 фикстура отвечает пустым массивом (все `*_list` баз данных, `k8s_list`, `vpc_list`, `secret_list`, `metrics_history`…), `metrics_scrape` в мир не входит — байты строки мерили бы фикстуры; строки evo — своя проекция, рост держит набор полей строки, а налог контекста — число строк без сужающего аргумента (`image_list` без `query`, `metrics_scrape` без `match`), его закрывает D |
| 3 (1.6) | Рантайм Go на Windows — причина (обход `split` и один повтор по сигнатуре — #529; «до» — 14 из 828 попыток 22–28.10): 12 случаев (десятый — `main` `9b2664a`, run 40476120416, `shrinkstack` ← `scanstack`; 11-й — PR #408, `TestFault_Matrix/deploy_apply`; 12-й — PR #492, `internal/tools`, `adjustdefers` ← `copystack` ← `shrinkstack`); «как есть до 1.5» (#400); авто-перезапуск по сигнатуре — #370 | список сигнатур #370 берёт все |
| 2 (1.4.1) | FB140-1: инвариант — согласие команд движка (`Caller.Gate`, `engineGated`) пишется в журнал как `confirmed: true` | аудит Fable 1.4.0 (#417); лечение — «разработка» |
| 2 (1.4.1) | Гипотезы аудита 1.4.0 — тестом до правки: ответ MCP ждёт до 2 с при занятой `journal.lock`; `journal_full` на сотни байт раньше 16 МБ; файл журнала без версии формата | аудит Fable 1.4.0 (#417) |
| 2 (1.4.1) | Входы прогона cloud-e2e в `display_title` (еженедельный против ручного); один прогон со всеми наборами на кандидате тега | ретро «релиз · 1.3.1»: еженедельный прогон принят за `cleanup_only` |
| 2 (1.4.0) | cloud-e2e в ru.AZ-2: тайм-аут SSH 2 из 2 (27.10, OPS-1b) — отчёт печатает состояние ВМ перед тайм-аутом | строка бэклога «os-smoke: при таймауте SSH печатать состояние ВМ» — та же причина, теперь и в `e2e` |
| 3 (нужна правка продукта) | Мир стека без стенки: дедлайн `stack_status` на фейковых часах или `stackCrankInterval` в тестах; новый тест стека не ждёт бюджет `wait_seconds` (минимум 1 с) | 28.10: `TestQA_StackStatusHonoursShortBudget` и `TestStackVMStep_ForeignVMWithSameNameIsNotAdopted` ждут по 1,01 с; `stackCrankInterval` создана для тестов, но ни один её не укорачивает Бюджет `wait_seconds` (минимум 1 с) — `time.Now` в `handleStackStatus`; без шва в продукте тест его не сократит, `stackCrankInterval` — только пауза между прокрутками. Цена — два теста по 1,01 с; правка «разработке» не окупается |
| 2 (1.6) | Вариант мира W8 задаёт значение по умолчанию явно: пустое поле — не «нет значения», а «выбрать автоматически» (пустой flavor = auto), вариант без явного значения проверяет не тот путь | 1.5, D50: мир неизвестного flavor сначала проверял автоподбор; «разработка» исправила явным `CLOUDRU_FLAVOR` |
| 2 (1.6, сделано — #555) | Каждый мир паритета `internal/cli` строится от одной базы с остановленными часами — инвариант, а не перечень миров (`TestWorlds_RunOnTheStoppedClock`: функция теста, которая ставит `toolEngine`, останавливает часы сама, через вызываемую или через всех вызывающих; семь миров на движущихся часах переведены; решение владельца 29.10) | FB140-6 остановил часы в `withParityWorld`, `withEmptyCloud` строился отдельно и остался на движущихся: TTL 3599/3600 на Windows, #490 красный дважды (`whoami`, затем `doctor`) — #492. Тот же класс — ci run 2053 (Windows, `TestParity_NewCommandsJSONEqualsToolsCall`, `token_expires…`; `handoff/1.5.md`): прогон `main` после слияния #472, до #555; класс закрыт #555 |
| 1 (1.6) | Проба только на чтение рядом с P34–P46 (начало 1.6, «да» владельца — #496, п. 4): что живой API отдаёт в `remote_ip_prefix` правила «откуда угодно» — пусто, `null` или `0.0.0.0/0` (`list_security_groups` с правилами) | от «разработки» через «релиз» (#504, D52): до пробы #504 держит «нет поля — как раньше» |
| 1 (1.6) | П6 сверки 1.5: ни одно превью create не называет проект (`projectid` — одна строка `bodyUnnamedEverywhere` С1); неверный проект — самая дорогая ошибка create. Инвариант готов: строка уходит, когда превью назовёт проект («разработка»), — С1 тогда красный на устаревшей строке | `coverage-1.5.md`; приоритет «релиза» 28.10 |
| 2 (1.6) | П5 сверки 1.5: человек не видит 17 значений превью (С2 `humanUnnamed`: работа 15, цена 2) — сокращает линия CLI 1.6 (строки из `Details` одной функцией, D) | `coverage-1.5.md` |
| 2 (1.6) | П1 сверки 1.5: инвариант «ошибка команды дерева не начинается с имени инструмента, у `tools call` — начинается» обходом команд мира паритета (#465 держится точечно; `sameRefusal` снимает префикс у обеих) | `docs/internal/audits/coverage-1.5.md` |
| 2 (1.6) | П2 сверки 1.5: вариант `consentWorlds` с двумя пропусками сразу и инвариант «ответ называет оба отказа» у каждого create, который читает оба (#469 держится у `provision` и `vm create`) | `coverage-1.5.md` |
| 2 (1.6) | П3 сверки 1.5: фикстура `ca_get` с `env` секретного имени и сверка «по HTTP значение замаскировано» (#500 держится на функции маскирования) | `coverage-1.5.md` |
| 3 (1.6, сделано — #562) | П7 сверки 1.5: фикстуры мира, которые доводят до записи `dns_record_create`, `k8s_add_nodepool`, `lb_create` (`takenNotReached`) — пуст: объект мира — зона, зона Managed Kubernetes и flavor; ни один из трёх не берёт чужое | `coverage-1.5.md` |
| 1 (1.6, тест — #533) | Инвариант оси 1 «переходное состояние ресурса — отказ до вопроса»: мир с диском в `extending`/`attaching`/`detaching`, и каждый изменяющий инструмент диска (`disk_detach`, `disk_resize`, `disk_attach`) отказывает до согласия с состоянием в ответе, а не 422 после «да»; лечение — «разработка» 1.6 | #521, п. 2: cloud-e2e кандидата 1.5 (run 142) — `disk_detach` через ~1 с после `disk_resize`, 422 `disk_can_not_be_detached_from_current_state` |
| 1 (1.6, шаги «а» и «б» — #545, «в» — #550) | Процесс evo приостановлен посреди долгого ожидания: шаг «а» (решение владельца 29.10) — мир `sleptCloud` у инструментов с `wait_seconds`: после первого опроса сервис отвергает токен 401 «Jwt is expired» (часы приостановленной ВМ отстают, G140-3), затем операция готова, упала или исчезла. Найдено: у всех восьми `*_operation` ожидание кончается 401 одного опроса — перечень `sleepBreaksWait`, правка «разработки» 1.6 (строка `release-plan.md`, 1.6). Состояние после сна (`FAILED` с ошибкой платформы, «not found») и обрыв соединения (`httpretry`) — уже честно. Шаг «б» (решение владельца 29.10) — `TestStdio_CallSurvivesTheProcessStopped` (`internal/mcp`, unix): дочерний процесс `evo mcp` (тестовый бинарь, вход через IAM) останавливается SIGSTOP на 3 с посреди `pg_operation` с `wait_seconds` — между опросами, с опросом в полёте, с токеном, отвергнутым после остановки; `tools/call` отвечает после SIGCONT концом операции (в третьем — честный 401 с выходом, правка — `sleepBreaksWait`). Зелёный: таймер и соединение до остановки процесс не вешают; мутация «опрос без ответа» — красный. Токен на 2 с не делается: evo обновляет токен за 5 мин до конца и не берёт токен короче 10 мин (`refreshTTL`, `minAcceptableTTL`), срок по стенке в тесте недостижим, его заменяет отказ сервиса (G140-3). Шаг «в» (решение владельца 29.10): один отвергнутый токен на чтении после записи — `TestSleep_OneRefusedTokenAfterTheWriteDoesNotEndTheCommand` (engine: `deploy`, `destroy`), `TestFault_OneRefusedTokenAfterTheWrite` (матрица сбоев `tools`), `TestDiskWait_GoesOnAfterOneRefusedToken`, `TestStackStatus_GoesOnAfterOneRefusedToken`; найдено: `evo deploy` обрывается на ожидании ВМ (`oneRefusalEnds`, строка `release-plan.md` 1.6), остальное идёт дальше. Храповик `TestWaits_SleepThroughIsTested` (`internal/testrules`): каждое ожидание в цикле — с тестом сна или с причиной. Пробелы храповика закрыты (решение владельца 29.10): `TestSleep_OneRefusedTokenOnThePathsOfFailure` (`assignFloatingIP`, `waitVMGone`, `findBootFIP`, `releaseBootFIP`), `TestApply_VMWaitGoesOnAfterOneRefusedToken` (`finishApply`); найдено: откат boot FIP оставляет адрес на одном 401 (`oneRefusalEndsPath`, строка `release-plan.md` 1.6). Строка закрыта: каждое ожидание в цикле — с тестом сна или без токена. Гипотеза: опрос без ответа держит `tools/call` до тайм-аута клиента сервиса (~60 с), а не до `wait_seconds` — мутация шага «б». Шаг cloud-e2e с `kill -STOP` не нужен | наблюдение владельца 29.10: среда Grok засыпала посреди Kafka (~40 мин), k8s, PG; бриф rc.1, 1.2 |
| 2 (1.6, сделано — #552) | Поддельный evo скриптов `.github/scripts/*_test.sh` — одним помощником (`fake_evo.sh`: `fake_evo_install`, `fake_evo_answer`; инварианты `TestWorkflows_ScriptTestsShareOneFakeEvo`, `TestWorkflows_FakeEvoAnswersLikeEvo`; решение владельца 29.10) с форматом настоящего: код 0 — ответ в stdout, иначе `{"error":…}` в stderr и пустой stdout (QA-12); сейчас у `dbaas_delete_test.sh` и `e2e_rules_sweep_test.sh` свои копии | #526: поддельный evo писал ошибку в stdout, тест держал неверную модель, ветки FAILED и «ещё создаётся» не срабатывали (run 144) |
| 2 (1.6) | Инвариант «нет бинарных файлов в дереве»: `git ls-files` без `*.whl`, `*.zip`, `*.tar*` и файлов больше 1 МБ вне объявленного перечня | #588: `pip download` в каталоге репозитория, `git add -A` унёс wheel 3,8 МБ в PR; поймал Qwen (critical), не тест |
| 2 (1.6) | Мир «ВМ в `error_deleting` с floating IP» для обоих путей `destroy` (по записи развёртывания и по `vm_id`): после отказа удаления ответ называет ресурсы, которые остаются привязанными и оплачиваемыми (адрес, диски), а не только путь — `deleteRefusals` и `TestParityWorld_RefusedVMDeleteNamesTheState` (#594) проверяют подсказку пути, но не остаток; по `vm_id` адрес в мире #594 не моделируется | #565 («Ускользнул: финальный прогон Grok rc.1 (E11)»), предложение «разработки», передал «1.6 — релиз» 29.10 |
| 2 (1.6, сделано — вариант (б), решение владельца #597, п. 12; `tools-list.yml`) | Храповик `tools/list` без точного числа в коде: `toolsListBytes` в `internal/mcp/limits_test.go` — точная константа, и каждый следующий PR очереди, который трогает описания инструментов, конфликтует с предыдущим (#566 → #567 → #583 → #584): слияние = merge `main` + CI у следующего, очередь строго последовательна. Варианты: (а) как есть; (б) в коде — только потолок `maxToolsListBytes`, рост — job CI сравнивает размер `tools/list` головы PR с базой и требует строку «tools/list: +N байт, потому что …» в описании PR (числа в дереве нет — нет конфликта); (в) запас роста в `toolsListSlack` (+2 КБ без строки) — храповик слабеет. Мнение «Процесса»: (б) — сохраняет «не растёт попутно» (P7) без конфликтов. Решать — владельцу | наблюдение «1.6 — релиза» в ночь на 30.10 по очереди «разработки» |
| 2 (1.7) | П1 сверки 1.6: каждая тарифная карта `pricing` — со снимком `testdata/tariff-*.json` и сверкой `TestLive_*` в job `contract` (сейчас — только PostgreSQL, #644) | `audits/coverage-1.6.md`: новая карта без снимка устареет молча |
| 2 (1.7) | П2 сверки 1.6: «откуда доступен кластер» — инвариант по группе DBaaS (`kafka_create`, `redis_create`), после живого ответа DB-5 | `audits/coverage-1.6.md`; #648 — только PostgreSQL |
| 3 (1.7) | П3 сверки 1.6: обход реестра «create с зоной из настроек шлёт UUID» в мире `CLOUDRU_REGION=<имя>` (D58 держат три названных create) | `audits/coverage-1.6.md`; класс D15 |
| 2 (1.7) | Инвариант «каждый create и delete реестра проходит живой сценарий `cloud-live.sh` или объявлен с причиной» (обход реестра против вызовов скрипта) | ретро «Процесс · 1.6»: `subnet_create`/`subnet_delete` 1.6 и отказы D57–D59, D64 вживую не проверялись до #661 |
| 2 (1.7) | П5 сверки 1.6: матрица мира транспорта — каждый путь `httpretry.Transport.RoundTrip` (первый ответ, повтор 5xx/429, повтор после 401) × каждая замена ответа (`HTMLAsBadGateway`, `RouteNotFoundAsBadGateway`) с одним итогом | FB160-1 (аудит Fable 1.6, #670, #671): замена D63 не применялась к ответу на повтор после 401 |
| 3 (1.7, решение владельца 29.10) | Общий фейк `faultCloud` вместо двух копий (`internal/engine`, `internal/tools`, по 852 строки) | от «1.3 — разработки»; сейчас копии сверяет `fault_copies_test.go`, правка одной без другой роняет тест — вынести в тестовый пакет. Отложено: ~940 обращений к неэкспортированным именам в ~40 файлах `engine` и `tools` — конфликты с открытыми PR «разработки» 1.6; расхождение копий уже держит `TestFaultFakeCopiesMatch`. Один механический PR в начале 1.7, при пустой очереди «разработки» |

## Инструменты

- **Миры** (`internal/cli`): `withParityWorld` (превью, считает записи),
  `withJSONWorld` (ответы, S3, kubectl/helm), мир двойников (#236);
  `TestParityWorlds_NoProductionWaits` держит все ожидания мира в
  миллисекундах. Запрос без фикстуры роняет тест и называет себя (метод, хост,
  путь с `{id}`): ответ — в `parityAnswers`, пустой список нарочно — строкой
  с причиной в `parityEmptyLists` (только GET списков,
  `TestParityWorld_EmptyListsAreLists`; дельта-аудит 1.2.3).
- **Матрица сбоев** (`internal/tools/fault_*_test.go`; замысел и история —
  `process-1.2.1-stages.md`, этап 6, п. 6.1): новый изменяющий
  инструмент — случай `apiCase`/`computeCase` или строка `faultMatrixOut` с
  причиной, иначе падает `TestFault_MatrixCoversTheRegistry`. Фейк отказов —
  один файл в `engine` и `tools` (`TestFaultFakeCopiesMatch`).
- **Правила тестов:** `internal/testrules` (часы стенки), `internal/clock`,
  `internal/workflows` (правила workflow).
- **CHANGELOG — фрагментами** (с 23.10, задача процесса 1.3, «да»
  владельца): продуктовый PR пишет `changelog.d/<тема>.md`
  (`changelog.d/README.md`), `CHANGELOG.md` не трогает; перед тегом rc и
  стабильным релиз вливает их `python3 scripts/changelog.py fold`. CI:
  `scripts/changelog-test.py`, `changelog.py check`, «Миграция» во фрагменте —
  класс «ломающее» (`TestWorkflows_CIChecksChangelogFragments`). Ручное
  слияние раздела `[Не выпущено]` при вливании `main` осталось только для
  правок уже влитых пунктов: раздел — из `main` плюс пункты ветки к общей
  базе (`git merge-base`).

## Взаимодействие

Роли сессий, кто сливает, кто обновляет ветку, номера D и передача
находок — `CLAUDE.md` §0; ускользнувшие дефекты — `CLAUDE.md` §5.

Постоянное разрешение владельца (24.10.2026): правки тестовых миров и
фейков, которых требует чужой PR, «Процесс» делает без согласования и
сообщает после — владельцу, «N — релизу» и автору PR. Когда без
инструмента из чужой ветки правка не соберётся, она идёт коммитом поверх
этой ветки, и автор вливает его в свой PR. Слияние своих PR «Процесса» —
по-прежнему по «да» владельца на каждый. Продуктовый код, CI-workflow и
живое облако — только по «да».
