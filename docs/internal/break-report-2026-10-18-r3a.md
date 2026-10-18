Cursor: break-report r3a, main=315c1cb0f0457660727e674b1129e2ff6e743ac1

Третий прогон, часть A: обрыв на пути `engine.Deploy` / `engine.Destroy`, MCP `provision`, `deployplan.ApplyWithTimeout` (`deploy_apply`) и `tools.Registry.advance` (шаг `provision`). Облако не вызывалось. Сеть наружу не нужна: мир ведёт фейк `provision.CloudAPI`, разбор ответа — `httptest` под `provision.CloudRuAdapter`. Код продукта не менялся. Пагинация (issue #98), R2-1…R2-5 и открытые пункты `docs/internal/standalone/BACKLOG.md` не повторялись. Поведение из `docs/internal/cloudru-discrepancies.md` дефектом не ставилось.

`t.Parallel` нет: изоляция делает `t.Setenv`. `t.Skip` на Windows нет: FIFO и символических ссылок в этих тестах нет. Синхронизация — каналы, счётчики вызовов и `Timings` в миллисекундах, без `time.Sleep`. `cli.run` на `httptest` не нацелить без правки продукта; код выхода и `reason` проверены через `clerrors.ExitCode` и `clerrors.Reason` — то, что `run()` возвращает после `signal.NotifyContext`. Реальные SIGINT и SIGTERM в `go test` не посылались.

| ID | Серьёзность | Область | Вход → неверный результат | Ожидалось (со ссылкой на документ/код) | Тест | Предложение |
|---|---|---|---|---|---|---|
| R3A-1 | blocker | deploy, provision, разбор `POST /vms` | `CreateVM` отвечает успехом с пустым `id` после того, как ВМ уже в мире (`200` и тело `[{}]`). `engine.Deploy` пишет state `creating` с пустым `VMID` и boot FIP `00000000-0000-4000-8000-000000000021`, опрашивает `GetVM("")` и выходит `VM_PROVISION_FAILED: VM  did not become active within 40ms`. ВМ `…0012` нет ни в state, ни в тексте ошибки. `Destroy` видит пустой `VMID`, печатает «В state нет ID ВМ» и ВМ не удаляет, FIP из записи отпускает, запись снимает. MCP `provision` отвечает `"vm_id": ""`, в state `VMID` пустой, повторный `provision` заводит вторую ВМ. Цена: оплачиваемая ВМ, которую `destroy` не находит. | Ветка объекта в `CloudRuAdapter.CreateVM` пустой `id` отвергает (`vm.ID != ""` → `API_ERROR`, `internal/provision/adapter.go`). Потерянный ответ (`err != nil`) идёт в `provision.FindCreatedVM` и пишет реальный id (`engine/deploy.go`, `handleProvision` в `internal/tools/tools.go`). `docs/exit-codes.md`: прерванный и неудачный `deploy` после создания ВМ называет её. `Destroy` при пустом `VMID` специально не делает `DELETE /vms/` (`internal/engine/destroy.go`). | `TestBreak_HTTP_CreateVMArrayWithoutID`, `TestBreak_Deploy_EmptyVMIDOrphansVM`, `TestBreak_Provision_EmptyVMIDOrphansVM` | В `CloudRuAdapter.CreateVM` (`adapter.go`, ветка массива) не возвращать элемент с пустым `ID`: та же ошибка, что у объекта `{}` (`TestFault_HTTP_CreateVMObjectWithoutID` уже зелёный). В `engine.provisionVM` и в `handleProvision`, если `err == nil`, а `vm == nil` или `vm.ID == ""`, не писать state с пустым id: вызвать `FindCreatedVM` и записать найденный id, иначе отпустить boot FIP и вернуть `API_ERROR`. |
| R3A-2 | blocker | deploy, `releaseBootFIP` | `CreateVM` отвечает 409 (ВМ в мир не записана), `DeleteFloatingIP` boot-адреса `…0021` (`203.0.113.11`) отвечает 500. Предупреждение называет адрес и id. Возврат: `CONFIG_INVALID: a VM named evo-004 already exists … nothing was created`. Записи в state нет. Повторный `deploy` с тем же входом успешен и заводит второй адрес `203.0.113.12`; первый остаётся в мире. Цена: два floating IP, один без записи. MCP `provision` этот путь не повторяет: ответ содержит id (`TestFault_Provision_Release500NamesFIP`). | `docs/exit-codes.md`: повтор `deploy` на 409 — код 2 `CONFIG_INVALID`, «чужая ВМ не подхватывается, floating IP повтора освобождается». `releaseBootFIP` уже печатает «floating IP %s (%s) was not released» и возвращает ошибку (`deploy.go`). `provisionVM` эту ошибку отбрасывает (`_ = e.releaseBootFIP`) и пишет «nothing was created». | `TestBreak_Deploy_BootFIPRelease500OrphansOnRetry` | В `provisionVM` (`deploy.go`, ветка `CreateRejected` и ветка «ВМ не нашлась») не отбрасывать ошибку `releaseBootFIP`. Если отдача не удалась, вернуть её: в тексте адрес и id, без «nothing was created». В `createBootFIP` до `CreateFloatingIP` забрать уже существующий boot FIP этой ВМ (`provision.IsBootFIPOf`), чтобы повтор не выпускал второй адрес. |
| R3A-3 | blocker | `deploy_apply` | Два `ApplyWithTimeout` одного плана. Первый `GetVMDetail` уже ответил «floating IP нет» и удерживается каналом; второй создаёт и привязывает адрес; первый по старому снимку создаёт второй. Оба вызова без ошибки. В мире одна ВМ и два floating IP. Последовательный повтор тот же FIP переиспользует (`TestFault_DeployApply_RetryReusesFIP`): дело в снимке, не в модели привязки фейка. Гонки данных нет: `-count=20 -race` падает одинаково, пауза — канал. | Комментарий `ensureFloatingIP` (`internal/deployplan/apply.go`): «one already on the VM … a second FIP used to be created». Поиск по имени есть только после ошибки создания. В спецификации compute нет 409 «floating IP с таким именем уже есть», поэтому два `POST` с одним именем оба могут пройти. | `TestBreak_DeployApply_OverlapAdoptsAndBillsSecondFIP` | В `ensureFloatingIP` до `CreateFloatingIP` найти `provision.FitName(plan.VM.Name, "-fip")` через `ListFloatingIPs` и взять его. После создания перечитать ВМ и список: если на ВМ или под этим именем уже есть другой адрес, удалить тот, который создал этот вызов. Снимок `GetVMDetail` не считать доказательством, что адреса нет. |
| R3A-4 | high | deploy, опрос ВМ | Токен истёк на каждом `GetVM` после создания (401, ВМ уже в мире и в state). Предупреждения: `AUTH_EXPIRED: token expired`. Итог: выход 1, `VM_PROVISION_FAILED: VM …0012 did not become active within 40ms`. Id в ошибке и в state есть, сироты нет. | `docs/exit-codes.md`: «401/403 от compute API (ВМ, диски, deploy, destroy) дают 2 (`AUTH_EXPIRED` / `AUTH_FAILED`)». 401 на самом `CreateVM` до записи в мир уже даёт 2 и отпускает boot FIP (`TestFault_Deploy_AuthOnCreateExitsAuth`). | `TestBreak_Deploy_AuthExpiredDuringPoll` | В цикле опроса `provisionVM` (`deploy.go`, `GetVM`): если `clerrors.Reason(err)` — `AUTH_EXPIRED` или `AUTH_FAILED`, вернуть эту ошибку сразу. State уже содержит ВМ. Разовый транспортный сбой по-прежнему предупреждение и следующая попытка. |

## Гипотеза, не находка

Потерянный ответ `CreateFloatingIP`, в мире адрес без имени. В спецификации compute `PublicFloatingIpCreateRequest.name` и `PublicFloatingIpResponse.name` обязательны, `minLength: 1` (кэш `python3 scripts/cloudru-specs.py`). В `docs/internal/cloudru-discrepancies.md` пустого имени нет. При такой инъекции `deploy` не находил адрес по `BootFIPName` и выпускал второй, именованный. Это не форма ответа из спецификации и не живое расхождение — в таблицу находок не ставилось, теста `TestBreak_*` нет.

## Проверено, не сломалось

1. **Счастливый путь и коды.** `TestFault_Deploy_HappyPath`: deploy до `running`, destroy опустошает ВМ, FIP и правила. 409 на `CreateVM` — выход 2 `CONFIG_INVALID`, boot FIP отпущен, state пуст (`TestFault_Deploy_ExitCodeMatchesReason`). Транспорт и HTTP 500 на `CreateVM` до записи в мир — выход 1 `API_UNREACHABLE` и 5 `API_ERROR`, мир пуст, записи нет. 401 на `CreateVM` до записи — выход 2 `AUTH_EXPIRED`, мир пуст. `--json` содержит id созданной ВМ (`TestFault_Deploy_JSONNamesCreatedVM`).

2. **Потерянный ответ и повтор.** Потерянный `CreateVM` усыновляется `FindCreatedVM`, id в ошибке и в state, повтор не создаёт вторую ВМ и второй FIP, destroy опустошает мир. Потерянный boot FIP с именем, которое отправили, — один адрес. Пустой id в ответе при сохранённом имени — тоже один, id в state. Транспорт на boot FIP до ВМ не оставляет оплаченный адрес: один FIP создаётся уже после загрузки. Пустой `status` на каждом `GetVM` — выход 1, id назван, повтор ту же ВМ доводит до `running`.

3. **Дальше по шагам deploy.** FIP навсегда `DOWN`: адрес записан, deploy доходит до `running`, destroy его удаляет. Второе из трёх правил SG потеряно после записи: в `opened_rules` три id, destroy снимает все три. Обрыв upload, `compose up` и SSH connect оставляет ВМ и FIP в записи, повтор не плодит вторую ВМ. Нездоровый verify deploy не валит — предупреждение, статус `running` (так и задумано в коде). Отмена контекста на ожидании ВМ — выход 130, id в подсказке и в предупреждении (`Error()` его не содержит: формат `CODE: message`), destroy опустошает мир. Отмена на `CreateVM` до записи отпускает boot FIP и state не пишет.

4. **Destroy.** `DELETE /vms` → 404, затем FIP отпускается, мир пуст. FIP → 409 «in use»: ошибка называет id, запись остаётся `destroying`, повтор после снятия 409 опустошает мир. `DELETE rule` → 404 считается закрытым. Отмена между удалением ВМ и FIP — выход 130, ВМ нет, FIP в записи `destroying`, повтор его отпускает. `DELETE /vms` → 409: запись и мир на месте, ошибка называет ВМ.

5. **HTTP-разбор.** Объект `{}` без id, HTML и `204` на `GetVM` — ошибка, не ВМ. `429` с `Retry-After: 3600` у compute-адаптера не ждёт этот срок: адаптер берёт `Backoff` (в тесте 1 мс) и выходит `API_RATE_LIMITED`. `httpretry.Transport` с `Backoff: time.Hour` и тем же `Retry-After` обрывается отменой контекста за 40 мс, без сна на 3600 с. Числовой потолок `maxRetryAfter` 30 с уже в `internal/httpretry/httpretry_test.go`; здесь он не перепроверялся сном.

6. **MCP.** `provision` счастливый путь и `destroy` по `vm_id`. Потерянный `CreateVM` записан; повтор получает 409 и не оставляет вторую живую ВМ и второй FIP. 409 отпускает boot FIP. 500 на отдаче boot FIP кладёт id в ответ (`fipLeftAnswer`) — в отличие от R3A-2. План, удалённый между `deploy_plan` и `deploy_apply`, не создаёт ресурсов. Последовательный повтор `deploy_apply` держит одну ВМ и один FIP. `advance` после `partial` (FIP не привязался) вторую ВМ не создаёт. `takenByName` считает занятыми `ERROR`, `DELETING` и `error_creating` и пропускает `DELETED` и `ERASED`. Несколько активных записей и `destroy` без `vm_id` — исправление R2-2, заново не заводилось.

## Таблица покрытия

Шаг × вид обрыва. `ok` — зелёный `TestFault_*`. Имя теста — падает на `main`.

| Шаг | отмена контекста | транспорт | ответ потерян после записи | пустые id или status | 401 | HTTP: без id, HTML, 204, 429 |
|---|---|---|---|---|---|---|
| IAM, зоны, флейворы, образы, подсеть и SG до создания | не покрыто | не покрыто | не покрыто | не покрыто | не покрыто | не покрыто |
| `CreateFloatingIP` (boot) | не покрыто | ok, затем один FIP после загрузки | ok, усыновление по имени | ok, пустой id при сохранённом имени | не покрыто | не покрыто |
| `CreateVM` | ok, boot FIP отпущен, state пуст | ok, мир пуст, выход 1 | ok, id записан, повтор без дубля | `TestBreak_Deploy_EmptyVMIDOrphansVM` | ok, выход 2, мир пуст | `[{}]` — `TestBreak_HTTP_CreateVMArrayWithoutID`; `{}`, HTML — ok; 429 — ok, без сна |
| опрос `GET /vms` | ok, выход 130, id назван, destroy пуст | не покрыто | не покрыто | ok, таймаут называет id, повтор без дубля | `TestBreak_Deploy_AuthExpiredDuringPoll` | `204` на `GetVM` — ok (адаптер) |
| ожидание FIP | не покрыто | не покрыто | не покрыто | не покрыто | не покрыто | не покрыто |
| FIP навсегда `DOWN` | — | — | — | ok, адрес в state, destroy его снимает | — | — |
| правила SG, 2-е из 3 | не покрыто | не покрыто | ok, три `opened_rules`, destroy снимает все | не покрыто | не покрыто | не покрыто |
| SSH wait | не покрыто | ok, connect, ВМ в записи, повтор без дубля | не покрыто | не покрыто | не покрыто | не покрыто |
| upload / `compose up` / verify | не покрыто | ok, ВМ остаётся; verify не валит deploy | не покрыто | не покрыто | не покрыто | не покрыто |
| `Destroy`: VM, затем FIP, затем правила | ok между ВМ и FIP, повтор дочищает | не покрыто | — | — | не покрыто | `DELETE /vms` 404 — ok; FIP 409 — ok, повтор дочищает; rule 404 — ok; VM 409 — ok, запись на месте |
| `releaseBootFIP` сам получил 500 | — | — | — | — | — | `TestBreak_Deploy_BootFIPRelease500OrphansOnRetry` (после 409 на `CreateVM`) |
| MCP `provision` | не покрыто | не покрыто | ok, повтор без второй живой ВМ | `TestBreak_Provision_EmptyVMIDOrphansVM` | не покрыто | не покрыто |
| `provision`: 409 и отдача FIP | — | — | — | — | — | 409 отпускает FIP — ok; 500 на отдаче называет id — ok |
| `deploy_apply` | не покрыто | не покрыто | не покрыто | не покрыто | не покрыто | план удалён — ok, ничего не создано; последовательный повтор — один FIP; два сразу — `TestBreak_DeployApply_OverlapAdoptsAndBillsSecondFIP` |
| `advance` после `partial` | — | — | — | — | — | ok, вторая ВМ не создаётся |
| `takenByName` на `ERROR` / `DELETING` | — | — | — | — | — | ok, имя занято; `DELETED` / `ERASED` — нет |
| TTY и вопрос y/N в `handleInterrupt` | нужна живая проверка | — | — | — | — | — |

Разница TTY и без TTY: при `!nonInteractive && StdinIsTTY()` `handleInterrupt` спрашивает, удалять ли ВМ. Тест гоняет только отмену контекста в неинтерактивном режиме (ВМ остаётся, выход 130, id назван). Живой вопрос с клавиатуры не поднимался.

## Итог

`TestFault_*`: 37, все зелёные на `main` `315c1cb0f0457660727e674b1129e2ff6e743ac1`. `TestBreak_*`: 6, на этом `main` падают, каждый из 20 прогонов с `-race` (pass=0).

Время на этой машине (Linux):

- `go test ./internal/engine/ -count=1 -timeout 90s -skip TestBreak_` — 1.909s
- `go test ./internal/tools/ -count=1 -timeout 120s -skip TestBreak_` — 7.338s
- `go test ./internal/engine/ -count=20 -race -timeout 180s -run TestFault_` — 7.534s
- `go test ./internal/tools/ -count=20 -race -timeout 180s -run TestFault_` — 0.725s

Оба пакета с новыми тестами короче 30 с. `gofmt -l` по новым файлам пуст, `go vet ./internal/engine/ ./internal/tools/` чистый.

Ветка `cursor/break-2026-10-18-r3a`. Коммит harness — `6af4e9b`, коммит падающих кейсов — `0eec252`. Полный SHA коммита с этим отчётом в файл не вписан: он появляется после коммита и приводится в ответе (`git rev-parse HEAD`).

## Таблица для issue

Заголовок: `[break] break-report 2026-10-18 r3a`. Метка: `отзыв`.

| ID | серьёзность | область | суть |
|---|---|---|---|
| R3A-1 | blocker | deploy, provision, разбор POST /vms | Успех CreateVM с пустым id (ответ `[{}]`) не записывает ВМ: state с пустым VMID, destroy её не удаляет |
| R3A-2 | blocker | deploy, releaseBootFIP | 500 на отдаче boot FIP после 409: ошибка говорит «ничего не создано», повторный deploy оставляет первый адрес и заводит второй |
| R3A-3 | blocker | deploy_apply | Два одновременных apply одного плана: оба успех, одна ВМ, два floating IP |
| R3A-4 | high | deploy, опрос ВМ | 401 на каждом GetVM до таймаута — выход 1 VM_PROVISION_FAILED, не 2 AUTH_EXPIRED |
