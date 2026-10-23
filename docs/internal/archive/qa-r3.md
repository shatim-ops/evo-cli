# QA r3: аудит архитектуры, QA и безопасности перед v1.0.0-rc.4

Дата: 2026-10-15. База: `main` @ `54a6fbe` (после PR #54–#58). Третий прогон;
исправления по r1/r2 (PR #52, #53) считаются внесёнными и перепроверены.

Метод: чтение кода, сборка `GOTOOLCHAIN=go1.26.9 go build -o /tmp/evo ./cmd/evo`,
прогон руководства `docs/guide.md` без ключей облака (`CLOUDRU_CONFIG_DIR` во
временном каталоге, `HTTPS_PROXY` на закрытый порт — ни один запрос в облако
не ушёл), локальные HTTP-серверы `evo mcp --http`, воспроизводящие тесты через
`go test -overlay` (дерево проекта не менялось). Облако не трогалось, ресурсы
не создавались; один вызов `org_quotas` с фиктивным ключом при проверке
HTTP-сервера ушёл в реальный IAM и получил 401 — других обращений не было.
Для проверки фильтра `k8s_kubectl` скачан штатный `kubectl v1.31.0` и запущен
против локального поддельного API-сервера.

Серьёзность: **critical** — потеря денег/данных без следа при обычном
использовании; **high** — утечка платного ресурса или секрета по реальному
сценарию; **medium** — неверное поведение, противоречащее документации или
решениям; **low/info** — гигиена, формулировки.

## Блокеры rc.4

Critical не найдено. High — семь:

| id | Что | Где |
|---|---|---|
| R1 | MCP `destroy vm_id=…` (и шаг `vm` в `stack_destroy`) снимает запись state **до** освобождения floating IP; при ошибке освобождения FIP тарифицируется без следа | `internal/tools/tools.go:2066-2086` |
| R2 | `deploy_apply`: id созданного FIP не записывается; повтор с тем же `plan_id` создаёт **второй** FIP; потерянный ответ на создание FIP — адрес без записи, `destroy` его не найдёт | `internal/deployplan/apply.go:265-313`, `internal/tools/tools_deploy.go:134-165` |
| R3 | `stack_destroy` на задании, у которого шаг не выполнялся (`planned`, зависимость упала), ищет ресурс по имени и **удаляет чужой** кластер/LB/VPC с тем же именем | `internal/tools/tools_stack.go:664-675` |
| M1 | HTTP-режим: `k8s_kubectl get secret x --template={{.data}}` печатает данные Secret без `--allow-secret-export` и без `confirmed` (фильтр смотрит только на `-o`) | `internal/mcp/http_kube.go:97` |
| M2 | HTTP-режим: `-v=8`/`-v=9` не запрещены; client-go пишет в stderr полные тела ответов (включая `data` Secret), stderr склеивается с stdout и уходит клиенту | `internal/mcp/http_kube.go:35-42`, `internal/kubetools/kubetools.go:73` |
| S1 | Ошибки compute API (401/403/404/429/5xx) захардкожены по-английски и печатаются в русском CLI как есть; затрагивает `vm list`, `deploy`, `destroy`, `list-zones` — основные команды руководства | `internal/provision/adapter.go:404-440` |
| S2 | `whoami`, `doctor`, `project use` игнорируют `--project-id`: показывают не тот проект/источник и печатают ложное предупреждение о подмене, хотя остальные команды с тем же флагом работают в проекте флага | `internal/cli/whoami.go:32`, `doctor.go:77`, `project.go:106` |

S1 и S2 — не утечки, но ломают два обещания руководства (CLI на русском, D6;
`whoami` показывает источник проекта, §7) на самых частых командах; для
релиза, который позиционируется как «CLI для коллег», это high.

Рекомендуемый минимум для rc.4: R1–R3, M1–M2 (плюс M3–M4 тем же патчем),
S2. S1 — большой, но механический (обернуть в `i18n.T/Tf`).

## Таблица находок

### R. Утечки платных ресурсов и гейт подтверждения

| id | серьёзность | файл:строка | путь к дефекту (сценарий → последствие) | воспроизведение | предлагаемое исправление |
|---|---|---|---|---|---|
| R1 | high | `internal/tools/tools.go:2066-2086` (`destroyTargeted`) | MCP `destroy vm_id=…` с `release_ip` (по умолчанию): `state.Update(... sgrules.Retire)` выполняется до `WaitVMGone`/`ReleaseFloatingIP`. Клиент оборвал соединение (по docs/mcp.md обрыв = отмена) или FIP ещё `attached` после 5 попыток → ответ содержит лишь `WARNING: the floating IP … was NOT released`, но записи уже нет, ВМ удалена; повторный `destroy vm_id` → «no VM matching», CLI `destroy` → «нет развёртывания». FIP тарифицируется без следа. В CLI `engine.Destroy` порядок верный (`destroy.go:148-158`) | тест T-R1 (падает: «запись о ВМ удалена из state») | Повторить порядок CLI: сначала `WaitVMGone` + `ReleaseFloatingIP`; при ошибке оставить запись со статусом `destroying` и `FloatingIPID`; `Retire` — только после успешного освобождения |
| R2 | high | `internal/deployplan/apply.go:56-79, 265-313`; `internal/tools/tools_deploy.go:134-165`; `internal/engine/destroy.go:256-271` | (а) `ensureFloatingIP` создаёт FIP `<vm>-fip`, но `ApplyResult` не несёт его id → в запись пишется только `VMIP`. Ответ на POST потерян/вызов отменён → `ip==""`, запись без адреса и id; `destroy` ищет по адресу или имени `fip-<vmID>` (не `<vm>-fip`) → «нечего освобождать», запись снимается, FIP платный. (б) Повтор с тем же `plan_id` (adopt) не читает FIP, уже привязанный к интерфейсу, и создаёт второй | тест T-R2a (падает: `CreateFloatingIP вызван 1 раз, PublicIP="203.0.113.6"; у ВМ уже есть 203.0.113.5`) | В `ensureFloatingIP` сначала брать `iface.FloatingIP` из `GetVMDetail`; возвращать id FIP в `ApplyResult` и писать `FloatingIPID`; при ошибке POST искать FIP по имени `<vm>-fip`; `releaseFloatingIP` — искать и по этому имени |
| R3 | high | `internal/tools/tools_stack.go:664-675` (`handleStackDestroy`) | Для каждого шага без `ResourceID` вызывается `findIDByName` и найденное удаляется. Job `planned` (apply не вызывался) или шаг, не дошедший до create → `stack_destroy confirmed=true` удаляет **чужой** pg/kafka/redis/k8s/LB/VPC с тем же именем. Человек подтверждал «удалить ресурсы стека», а не чужой кластер | тест T-R3 (падает: «job в статусе planned удалил кластер pg-foreign») | Искать по имени только у шагов, которые стартовали (`running`/`waiting`/`error` с `Attempts>0`/`Interrupted`); на `planned` отвечать «нечего удалять». Усыновление по имени в `advance` (`:268-273`) — хотя бы предупреждать |
| R4 | medium | `internal/tools/tools_stack.go:226-303` (`advance`), `:503` (`job.Save()` после crank) | Нет write-ahead: шаг `pending` без `Interrupted` вызывает create без предварительного поиска по имени. Жёсткий останов (kill -9, OOM, второй Ctrl-C по stdio — docs/mcp.md: «повторный Ctrl-C убивает процесс сразу») между create и `job.Save()` → следующий crank создаёт второй кластер. То же, если `findIDByName` после ошибки create проглотил ошибку list (`:84-86`) | тест T-R4 (падает: `pg_create вызван 2 раз`) | Перед create сохранять шаг как `running`/`Interrupted=true`; для `pending` с `FindTool` всегда сначала `findIDByName`; различать «не найден» и «list не удался» |
| R5 | medium | `internal/tools/tools.go:1764-1807` (`handleProvision`) | Запись в state пишется только в ветках cancel/timeout/error/active: между `CreateVM` и концом ожидания (до 10 мин) ВМ и boot-FIP в state нет. docs/mcp.md («`provision` записывает ВМ в state со статусом `creating`») верно только для мягкой отмены; жёсткий останов → ВМ и FIP без записи. CLI `deploy` пишет запись сразу (`deploy.go:559-560`) | тест T-R5 (падает: «во время ожидания ВМ vm-made не было в state») | `saveState(vm.ID, "creating", bootFIP)` сразу после `CreateVM`, как в CLI; то же для `deploy_apply` |
| R6 | medium | `internal/sgrules/sgrules.go:171-211` (`Open`) | POST правила применён, ответ потерян (POST не ретраится, `adapter.go:380`) → правило `Failed`, не записано. Повторный `deploy` видит порт в `existing` → `AlreadyOpen` → правило «чужое», `destroy` его никогда не удалит: `0.0.0.0/0` в общей группе зоны навсегда. Ветка `lost` (`:188`) работает только при `err == nil` | тест T-R6 (падает: `первый Open: Failed:[80]; второй: AlreadyOpen:[80]; rules=[]`) | При ошибке POST (не 4xx) повторно читать список и усыновлять правило с нашим `Description` («evo: port N»), которого не было в `before` |
| R7 | medium | `internal/engine/deploy.go:511-529, 831-853` (`createBootFIP`/`releaseBootFIP`) | POST boot-FIP применён, ответ потерян → `createBootFIP` даёт `nil,nil`; затем `CreateVM` отказал (квота) → FIP `fip-<vm>-<ts>` зарезервирован, нигде не записан, `releaseBootFIP(nil)` ничего не делает. Путь after-boot восстанавливает по имени (`:790`), boot-путь — нет. MCP `provision` использует тот же код (`tools.go:1720`) | тест T-R7 (падает: «FIP … создан (ответ потерян), ВМ нет — FIP не освобождён») | При ошибке `CreateFloatingIP` искать по имени (имя без timestamp или по префиксу `fip-<vmName>-`), найденное использовать/освобождать |
| R8 | medium | `internal/deployplan/apply.go:56-79`; `tools_deploy.go:154-161` | `findVMByName` — по имени в проекте без проверки зоны и статуса: старая ВМ с тем же именем (другая зона, `stopped`, `creating`) усыновляется как `running` (захардкожено `:73`), пишется в state, план удаляется; позже `destroy` её удалит. Порты на adopt-пути не открываются | тест T-R8 (падает: «stopped ВМ из другой зоны усыновлена как running») | Усыновлять только ВМ в зоне плана и не старше плана (как `FindCreatedVM`); отдавать реальный статус; дожидаться `running`, открывать порты |
| R9 | medium | `internal/deployplan/resolve.go:133-145`, `apply.go:288-297`, `engine/destroy.go:265-270` | `deploy_apply` привязывает уже существующий свободный FIP пользователя (первый `Available()`); `destroy` потом находит его по адресу и **удаляет** — адрес, зарезервированный вручную, теряется; превью показывает только `floating_ip: true` | чтение кода (гипотеза о последствиях для пользователя; код однозначен) | Хранить признак «FIP создан evo» (или id созданного); чужие только отвязывать; в превью называть переиспользуемый адрес |
| R10 | low | `internal/tools/tools.go:1992, 2035, 2049` | `destroyTargeted` ищет ВМ и список FIP в `eng.Config.ProjectID`, а `fipRec.ProjectID` берёт из записи: с org-ключом и проектом B в конфиге `destroy vm_id` ВМ из A отвечает «no VM matching» (отказ, не утечка); D12 выполнен частично | — | Брать проект из записи для всех вызовов, как `engine.Destroy` |
| R11 | low | `tools_stack.go:682`, `tools.go:2005` | «Уже удалено» распознаётся по подстрокам `404`/`not found`; `destroy vm_id` на отсутствующую ВМ отвечает «no VM matching» → `failed` → job навсегда `destroy_failed` | — | Единый маркер «gone» |
| R12 | low | `internal/provision/discovery.go:66-73` (`listAll`) | Если сервер не прислал `total` → возврат после первой страницы (100): поиск ВМ/FIP по имени промахнётся в проектах >100 объектов. Спецификация обещает `total` (api-contract §4.5) — проявится только при отклонении API. Бесконечного цикла нет (`listMaxPages=100`), пустая страница останавливает | тест T-R12 (падает: `got 100, want 130`) | Без `total` крутить до неполной страницы |
| R13 | low | `internal/state/lock_other.go` | На платформах вне списка (solaris/illumos/aix/plan9) блокировки state нет: два `deploy` в одном каталоге → две ВМ. Сборка под них не проверялась | — | `O_EXCL` lock-файл или отказ |
| R14 | low | `internal/engine/deploy.go:91-110, 175-179` | Повторный `deploy` на существующую ВМ не проходит гейт (по дизайну), но открывает **новые** порты compose-файла для `0.0.0.0/0` без вопроса; превью — только с `--dry-run` | — | Показывать превью/спрашивать при новых портах |
| R15 | low | `internal/tools/tools_deploy.go:85-98` | `deploy_plan` не gated → создаёт SSH-ключ (`EnsureSSHKey`) и пишет план на диск. Не платное | — | Ключ создавать лениво |

### M. MCP по HTTP

| id | серьёзность | файл:строка | путь к дефекту | воспроизведение | исправление |
|---|---|---|---|---|---|
| M1 | high | `internal/mcp/http_kube.go:97` | Фильтр `get secret` смотрит только на `-o/--output`. `args="get secret x --template={{.data}}"` — read-only (без `confirmed`), фильтр пропускает, kubectl печатает `data`. docs/mcp.md обещает, что `get secret … -o yaml/json/jsonpath` отключены. Строка `output` маскированием не покрывается | тест T-M1 (все пять случаев проходят фильтр); реальный kubectl против поддельного API печатает `map[password:…]` | `--template` считать эквивалентом `-o go-template`; для `secret*` запрещать любой формат вывода, кроме `name`/`wide`; надёжнее — denylist `secret` для `get`/`describe` без `--allow-secret-export` |
| M2 | high | `http_kube.go:35-42`; `internal/kubetools/kubetools.go:73` (`CombinedOutput`) | Флаги `-v`/`--v`/`--vmodule` не запрещены; при `-v>=8` client-go пишет в stderr `Response Body: {...}` с полным объектом (включая `data` Secret). stderr склеен с stdout → уходит клиенту. `get secret x -o name -v=8` — read-only. Токен в `-v=9` не печатается (client-go маскирует) | T-M1; реальный kubectl: `request.go … Response Body: {... "data": {"password": …}}` | Добавить `v`, `vmodule` в `kubectlHTTPDeniedFlags` (или `-v<=6`); не склеивать stderr |
| M3 | medium | `http_kube.go:97` (только `verb=="get"`); `tools_kube.go:90` | `label/annotate/patch/set/apply secret x … -o yaml --dry-run=server` — изменяющий verb (`confirmed=true` ставит сама модель), `--dry-run=server` ничего не меняет, но печатает объект с `data` | T-M1; реальный kubectl печатает YAML с `data.password` | Проверку `namesSecrets && output∉{name,wide}` применять ко всем verbs |
| M4 | medium | `http_kube.go:68` | `--cert`/`--key`/`--ca-cert` не покрыты denylist (`file`/`-dir` в имени нет): `create secret tls t --cert=/path/a.pem --key=/path/b.pem --dry-run=client -o yaml` читает PEM-пару с хоста сервера и печатает base64 (требует `confirmed`; работает на валидной паре — сертификаты reverse-proxy, клиентские) | T-M1 | Добавить `cert`, `key`, `ca-cert` в denylist; лучше allowlist флагов для `create secret` |
| M5 | low | `internal/engine/destroy.go:308-312` (`projectOf`); `internal/cli/mcp.go:208` | `--allow-project` проверяет проект конфигурации, а `destroy` без `vm_id` удаляет в проекте из `.evo/state.json`. Запись, сделанная ранее в проекте B, при сервере с `--allow-project A` → `destroy confirmed=true` удалит ВМ в B. Клиент сам записать state не может | чтение кода | В `handleDestroy` прогонять `deploymentProject()` через `checkAllowedProject` |
| M6 | low | `internal/mcp/http_kube.go:188-196` (`publicIP`) | Не считаются приватными `fec0::/10`, `64:ff9b::/96` (NAT64: `64:ff9b::7f00:1` = 127.0.0.1), `198.18.0.0/15`, `192.0.0.0/24`, `240/4`. Эксплуатация требует NAT64 у хоста — гипотеза | `k8s_helm repo_url=https://[64:ff9b::7f00:1]/` проходит `publicURL` | `netip` + списки RFC 6890 |
| M7 | low | `internal/mcp/http.go:510-526` (`newSession`, лимит 10000, вытеснение старейшей) | Любой клиент с токеном за секунды вытесняет сессии других: их следующий запрос → `404 unknown session`, `DELETE`/`notifications/cancelled` по вытесненной сессии не действуют. Данные не теряются (сессии stateless) | 10051 `initialize` за 2,1 с → первая сессия 404 | Лимит сессий на адрес; документировать |
| M8 | low (по дизайну) | `internal/mcp/cancel.go:632-648` | Клиенты без `Mcp-Session-Id` делят scope `""`: `notifications/cancelled requestId=1` одного отменяет вызов `id=1` другого. Docs это упоминают | `POST /mcp` без сессии с `notifications/cancelled` → 202 | Рекомендовать сессии; scope = remote addr для бессессионных |
| M9 | low | `http.go:367-370`; `tools_kube.go:102/252` | При обрыве клиента слот семафора (32) держится, пока инструмент не вернётся (`kubectl` 90 с, `helm` до 9 мин). Утечки слотов при панике нет (`defer` + `recover`) | чтение кода | Приемлемо |
| M10 | info | `internal/cli/mcp.go:197`; в `internal/mcp`, `internal/tools` нет `Debug(` | docs/mcp.md: «`--verbose` — подробнее»; на деле stderr идентичен | запуск с `--verbose` | Поправить docs или добавить debug-строки без аргументов |
| M11 | info | `internal/mcp/http_paths.go:203-257` (`redactSecrets`) | Маскируются только строковые значения по имени ключа; не маскируются объект под секретным ключом, JSON/YAML внутри строк (`output` kubectl/helm, `kubeconfig`), ключи `credentials`, `dsn`, `connection_string`, `cluster_token`. Реальные ответы API не проверялись | чтение кода | Расширить список; маскирование по regex в строках |
| M12 | info (гипотеза) | `internal/tools/tools_kube.go:182` (`helm repo add <name> <url> --force-update`) | По HTTP клиент пишет в helm-конфиг хоста: имя репо из префикса `chart`, `--force-update` перезапишет URL существующего репо оператора. helm не установлен, не воспроизводилось | — | `--repo <url>` вместо `repo add`, или отдельный `HELM_REPOSITORY_CONFIG` |

### C. CLI и QA по руководству

| id | серьёзность | файл:строка | путь к дефекту | воспроизведение | исправление |
|---|---|---|---|---|---|
| C1 (=S2) | high | `internal/cli/whoami.go:32` (`resolveConfigWithSources(printer, nil)`), `doctor.go:77-80` (без `Flags`), `project.go:106` (`resolveConfig(printer, nil)`) | `--project-id` игнорируется в `whoami`/`doctor`/`project use`: показан проект и источник из профиля/`.evo.yaml`/env, печатается лишнее предупреждение о подмене, хотя `vm list`/`deploy` с тем же флагом работают в проекте флага. Guide §7 обещает «`whoami` показывает, откуда взят проект» | `.evo.yaml` с проектом `3333…`, `evo whoami --project-id 5555…` → `Предупреждение: проект 3333… из .evo.yaml заменяет проект 1111… профиля "default"`; `evo doctor --project-id 5555…` → `project_id из project`. `project list` (через `scopeOverrides()`) флаг учитывает | Передавать `scopeOverrides()` в whoami/doctor/project use |
| C2 | medium | `internal/cli/tools.go:672-703` (`toolError`) | Коды с exit 1 (`API_UNREACHABLE`, `STATE_*`, `SSH_*`) не попадают в `switch` → `code = CodeAPIError`: `evo vm list` при недоступном IAM → exit 5, `reason: API_ERROR`, `hint: ""`; `evo whoami` в той же ситуации → exit 1, `API_UNREACHABLE`. docs/exit-codes.md и troubleshooting: `API_UNREACHABLE` — код 1. Скрипт не отличит сеть от отказа облака | `evo vm list --json` (прокси закрыт): stderr `{"error":{"code":5,"reason":"API_ERROR","message":"vm_list: API_UNREACHABLE: IAM недоступен","hint":""}}`; тест T-C2 (падает: `code=API_ERROR exit=5`) | В `toolError` сохранять любой распознанный код (`clerrors.New(prefix, …)`), а не только 2/4/5; пробрасывать hint |
| C3 | medium | `internal/cli/project.go:17-19, 48-51`; `configure.go:297-313` | `project list` с ключом уровня проекта (Organization API → 400, наблюдалось 14.10) падает с exit 5 и подсказкой «передайте --project-id», бессмысленной для list; 403 → `AUTH_FAILED`/2 (скрипт решит, что ключ плохой). `whoami`/`doctor` в той же ситуации говорят «неизвестно». Guide §7 советует `project list` именно для таких ключей | fake org 400: `evo project list` → `Ошибка: не удалось получить список проектов: статус 400 …; передайте --project-id`, exit 5 | При недоступном списке — текст как в `whoami` (`scopeText`) и активный проект, код 0; JSON `{"projects":null,"active":…,"note":…}` |
| C4 | low | `internal/cli/tools.go:644` (`callSummary`), превью инструментов | Превью подтверждения в русском CLI содержит английский текст для агента: `evo disk delete <id>` → «План: вызвать disk_delete / disk_delete permanently destroys disk … Approve, then confirmed=true.»; то же `vm delete`, `k8s kubectl`. D6: превью — русские; «описания инструментов английские» оговорено, превью — нет | `evo tools call destroy --arg vm_id=<uuid> --dry-run` | Для деревьев — русское превью из `engine/preview` или перевод `message` по коду инструмента |
| C5 | low | `internal/cli/help_ru.go:80-88`; `main.go:159-176` | `evo help nosuch` → английское cobra `Unknown help topic [nosuch]` + справка, exit 0; `evo completion nosuch` → справка, exit 0 (у `vm lst` — exit 1) | запуск | Свои `Args`/`Run` для help; `asGroup` для completion |
| C6 | low | `whoami.go:277-281`, `doctor.go:205` vs `engine/preview.go:275-288` (`originLabel`) | `whoami`/`doctor` показывают источник сырым токеном (`источник: profile`, `project_id из default`), превью — переводом («из .evo.yaml»). Без профиля `doctor` пишет «key_id из default; project_id из default» — читается как имя профиля | `evo doctor` в пустом `CLOUDRU_CONFIG_DIR` | Общая функция-метка источника; токены оставить в JSON |
| C7 | low | `internal/cli/main.go:75` (`aliasNotice`) | Alias `cloudru-vm` при `EVO_LANG=en` печатает русское уведомление | `EVO_LANG=en ./cloudru-vm version` | `i18n.T` |
| C8 | low | `help_ru.go:188-189`; `trees.go` vs cobra `ExactArgs` | Дубли/разнобой в usage-ошибках: `evo logs -n` → «флагу -n нужно значение (-n)»; `evo logs -x` → «неизвестный флаг: -x в -x»; `project use` → «нужно аргументов: 1, передано: 0», `vm get` → «evo vm get: нужно <vm-id|name>, передано аргументов: 0» | запуск | Убрать `$2`, когда равен `-$1`; единая формулировка |
| C9 | low | `tools.go:696-700` (`toolError`, ветки `CodeConfigInvalid` и `ExitAuth`) | Через деревья теряется подсказка `CONFIG_INVALID` и подменяется подсказка IAM: битый config.yaml → `evo vm list` без hint (у `whoami` — «Исправьте синтаксис YAML…»); 401 → hint «Выполните 'evo configure'» вместо «Проверьте Key ID и секрет» | запуск | Возвращать ошибку из handler структурно, не строкой |
| C10 | low | `main.go:478-483, 580-582, 614-620` (`Long: i18n.T("", …)`) | При `EVO_LANG=en` у `deploy`/`destroy`/`logs` нет Long и примеров | `EVO_LANG=en evo deploy --help` | Дать английские тексты или зафиксировать как решение |
| C11 | info | `internal/cli/trees.go` (`k8s kubectl <cluster-id> <args>`) | `evo k8s kubectl get pods` без cluster-id → «kubectl "pods" changes the cluster… requires confirmed=true» (неизвестный глагол считается изменяющим — безопасно), но сообщение сбивает | запуск | Проверять, что первый аргумент похож на UUID |
| C12 | info | `internal/cli/mcp.go` | `--auth none` на `0.0.0.0` → exit 1 (`USAGE`), нет токена → exit 2; docs называют только код 2 | запуск | Уточнить docs |

### I. Язык (D6, пересмотр)

| id | серьёзность | файл:строка | путь к дефекту | воспроизведение | исправление |
|---|---|---|---|---|---|
| I1 (=S1) | high | `internal/provision/adapter.go:404-440, 99, 209` | Все ошибки compute API по-английски (message и hint): `Cloud.ru API rejected the token (401)`, `access denied (403) for GET /api/v1/…`, `resource not found: GET …`, `API rate limit exceeded after retries`, `API returned status 503 after 3 retries`. В русском CLI печатаются как есть; их же подхватывают предупреждения `floating IP: %v` (`engine/deploy.go:655, 957, 969`). Это первые ошибки, которые увидит коллега с неверными ролями (403) | временный тест с httptest при `EVO_LANG=ru`: `HTTP 403 → AUTH_FAILED: access denied (403) for GET /api/v1/availability-zones` | Обернуть в `i18n.T/Tf` по образцу `internal/auth/iam.go:96-240` |
| I2 | medium | `internal/cli/tools.go:71`, `trees.go:242`, docs/guide.md:224 vs `invokeTool`/`toolError` | `tools call` и деревья печатают JSON-ответ инструмента на **русском** (ошибки engine/config/auth через i18n), а собственные тексты `internal/tools` — английские → смешение в одном JSON; справка обещает «ответы инструментов — на английском, как в MCP». В MCP тот же ответ английский | `evo tools call vm_list` без ключа: stdout `{"error": "AUTH_REQUIRED: не задан ключ доступа Cloud.ru", "status": "error"}`; `EVO_LANG=en` → английский; по stdio MCP при `EVO_LANG=ru` — английский | Либо на время `t.Handler` ставить EN и переводить только обёртку CLI, либо исправить справку и guide |
| I3 | low | `help_ru.go` (типы pflag), `doctor.go:72-193` (имена проверок) | В русской справке типы `string`, `int`, `stringArray`; в таблице doctor — `config dir`, `key scope`, `credentials` | `--arg stringArray` | Принять как термины или backtick-подстановки |
| I4 | low (docs) | docs/auth.md:78-86 vs `configure.go:191, 197, 271, 339, 409` | Образец интерактивного `configure` в документации — с английскими подсказками (`Access key ID:`, `Profile "default" saved to …`), код печатает русские | сравнение | Обновить образец |

### P. Проекты (D12, P1–P3)

| id | серьёзность | файл:строка | путь к дефекту | воспроизведение | исправление |
|---|---|---|---|---|---|
| P1 | low | `internal/config/config.go:203` (`from := ProjectFileName`) | С legacy `.cloudru-vm.yaml` предупреждение называет `.evo.yaml` | legacy-файл с другим проектом | Передавать фактический путь |
| P2 | low | `internal/cli/project.go:118-120, 126-129` | `project use` в `--json` не предупреждает про `CLOUDRU_PROJECT_ID`; про `.evo.yaml`, который перекроет новый проект, не предупреждает вовсе; попутное предупреждение `resolveConfig` называет старый проект профиля | `.evo.yaml` p-3 + `project use one` → «Проект профиля default: p-1 (one)» и предупреждение про p-2; следующий `whoami` → `p-3 (источник: project)` | После сохранения проверять итоговый `ProjectOrigin` и предупреждать (и в JSON) |
| P3 | low (гипотеза) | `project.go:97/115`, `configure.go:186/259`, `config/profile.go:314-343` | `LoadProfile` до блокировки, `SaveProfile` пишет снимок целиком → параллельные `configure` (ротация ключа) и `project use` теряют изменения друг друга в одном профиле; два файла пишутся последовательно — сбой между ними оставит профиль без ключа | чтение кода | Перечитывать профиль под блокировкой, мержить изменённые поля |
| P4 | гипотеза | `engine/deploy.go:312, 363, 455` (до превью `:492`) | Чужой проект в `.evo.yaml`: `deploy --dry-run` упадёт на `ListFlavors`/`resolveNetwork` (403, английский, I1) **до** превью — превью с источником проекта не покажется. Не воспроизведено (нет подмены compute URL в тестах) | — | Показывать проект/источник до первого обращения к compute или уточнять 403 («проект X из .evo.yaml») |
| P5 | low | `internal/cli/configure.go` vs guide §3 | Guide: «Сервисному аккаунту Cloud.ru не отдаёт список проектов, поэтому UUID … попросит ввести вручную — это ожидаемо»; `configure --help`: «показывает проекты, которые видит ключ, и предлагает выбрать». Оба верны для разных ключей; формулировка guide категорична | — | Уточнить guide: «ключу уровня проекта список недоступен» |

### W. Workflows и цепочка поставки

| id | серьёзность | файл:строка | путь к дефекту | воспроизведение | исправление |
|---|---|---|---|---|---|
| W1 | medium | `internal/bootstrap/install.sh:54` (встроен в бинарь `//go:embed`, запускается `sudo -n sh`) | На каждой ВМ пользователя от root выполняется `curl -fsSL https://get.docker.com \| sh` без проверки хэша/подписи и без закрепления версии; без `--proto '=https'`. Компрометация get.docker.com/CDN → произвольный код от root на ВМ клиентов | чтение кода | Docker из apt-репозитория с закреплённым GPG-ключом/версией, или скачать во временный файл, сверить sha256 закреплённой версии скрипта, затем `sh` |
| W2 | low | `install.sh:47` | Ветка wget без `--https-only`/TLS-ограничений (у curl `:43` есть `--proto '=https' --tlsv1.2`); при редиректе `https→http` архив и checksums скачаются по http; без cosign целостность держится только на TLS | чтение кода | `wget -q --https-only --secure-protocol=TLSv1_2` |
| W3 | low | `install.sh:126` | `cosign verify-blob … >/dev/null 2>&1 \|\| die` — причина отказа скрыта; пользователь не отличит атаку от сетевой ошибки | чтение кода | Показывать stderr cosign при неудаче |
| W4 | low | `install.sh:102-106` | `CLOUDRU_VM_BASE_URL` не-https допускается с предупреждением; без cosign sha256 проверяется тем же источником — ничем | чтение кода | `die` для не-https без cosign |
| W5 | low | `.github/workflows/cloud-e2e.yml:115, 131, 133, 142, 166, 174, 197` | В публичный лог попадают живые ID ВМ/FIP/SG и IP тестового проекта (`deploy` печатает `ВМ <id>, адрес <ip>`; `curl -fsS http://$ip/` при ошибке печатает IP; `tail -5 logs.txt` — access-log nginx). Ключи и `CLOUDRU_PROJECT_ID` маскируются GitHub. Ресурсы временные — разведка, не доступ | чтение | `curl … 2>/dev/null`, счётчик строк вместо `tail`, в `jq` оставить только порты |
| W6 | low | `cloud-e2e.yml:36, 146-178` | Очистка (`destroy --yes`, поиск сирот) под `if: always()`, но её бюджет (до 10 мин destroy + 6×60 с + 6×10 с) входит в общий `timeout-minutes: 60`: если deploy/logs съели час, очистку прервёт тот же таймаут; при cancel раннер даёт процессу ~10 с, `FindCreatedVM` ждёт до 30 с — state может не записаться, спасает только поиск по имени | чтение | Очистка отдельным job со своим `timeout-minutes` |
| W7 | low | `.github/workflows/api-contract.yml:44` | `pip install pyyaml` без закрепления версии/хэша — единственная незакреплённая зависимость CI; job без секретов | чтение | `--require-hashes` |
| W8 | low | `.github/workflows/qwen-review.yml:48`; `.github/scripts/qwen-review.sh:44, 88-92, 145, 156-163` | Diff и файлы PR уходят во внешний LLM-сервис (`vars.FM_BASE_URL`, по умолчанию `foundation-models.api.cloud.ru`); секрет `CLOUDRU_FM_API_KEY` уходит на адрес из repo-переменной (меняет admin). Ошибка сервиса и список моделей публикуются в комментарий PR. Триггер только `author_association == OWNER` — форк запустить не может | чтение | Allow-list хостов `*.cloud.ru`; `msg`/список моделей — только в лог job |
| W9 | info | `scripts/__pycache__/cloudru-specs.cpython-313.pyc` (в `git ls-files`) | Закоммичен .pyc; на выполнение не влияет | `git ls-files scripts/__pycache__` | `git rm --cached`, `__pycache__/` в `.gitignore` |
| W10 | info | `.github/workflows/release.yml:81-84, 145, 151` | Секреты Artifact Registry на уровне job `env` (видны всем шагам), `docker logout` без `if: always()`; образ не подписан cosign (задокументировано в RELEASING.md:55); job `image` не зависит от `goreleaser` — образ публикуется, даже если релиз бинарей упал | чтение | `AR_*` только в шаге push; `needs: [ci, goreleaser]`; подписать образ |
| W11 | info | `cloud-e2e.yml:94-98` | `zone_id`/`image_id` из JSON API подставляются в `sed -i` без валидации (входы workflow валидируются регэкспом, значения API — нет). Требует подмены ответа Cloud.ru по TLS — гипотеза | чтение | Тот же регэксп, что для входов |
| W12 | info | `.github/workflows/tag.yml:46` | `actions/checkout` без `persist-credentials: false` — токен с `contents: write` в `.git/config` на время job; чужой код не выполняется, нужен для `git push` | чтение | Оставить или `persist-credentials: false` + явный header |
| W13 | info | `install.sh:6`, docs/install.md:12 | `curl … /main/install.sh \| sh`: установщик не подписан и берётся с `main`; доверие = права записи в `main`. Частичная загрузка безопасна (весь код внутри `main "$@"`) | чтение | Публиковать `install.sh` артефактом релиза с подписью |

## Воспроизводящие тесты

Все тесты ниже **падают на текущем `main`** (проверено через
`go test -overlay`); в код не добавлены. Используют существующие фейки
пакетов (`targetCloud`/`targetRegistry`, `stackHome`/`newTestServer`/`planJob`/`fakeOK`,
`existingCloud`, `cancelCloud`/`cancelRegistry`, `newFake`, `interruptCloud`/`fastTimings`,
`setupTestServer`).

### T-R1, T-R3, T-R4 — `internal/tools/qa_r3_test.go`

```go
package tools

import (
	"context"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/stack"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// R1: destroy vm_id=… — запись state снимается ДО освобождения FIP.
type neverReleaseCloud struct{ targetCloud }

func (c *neverReleaseCloud) DeleteFloatingIP(context.Context, string, string) error {
	c.fipTries++
	return clerrors.New(clerrors.CodeAPIError, "floating ip is attached", "")
}

func TestQAr3_DestroyTargeted_FIPReleaseFailsKeepsRecord(t *testing.T) {
	cloud := &neverReleaseCloud{}
	r, dir := targetRegistry(t, cloud)
	text, _ := r.handlers["destroy"](context.Background(), map[string]any{"vm_id": "vm-1", "confirmed": true})
	if !strings.Contains(text, "NOT released") {
		t.Fatalf("answer: %s", text)
	}
	sf, _ := state.Read(state.DefaultPath(dir))
	for _, d := range sf.Deployments {
		if d.VMID == "vm-1" {
			return // запись осталась — можно повторить
		}
	}
	t.Fatalf("FIP fip-1 не освобождён, а запись о ВМ удалена из state: %+v", sf.Deployments)
}

// R3: stack_destroy на job, у которого шаг никогда не выполнялся, удаляет
// чужой ресурс с тем же именем.
func TestQAr3_StackDestroy_PlannedJobDeletesForeignByName(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"s","postgres":{"name":"shared-db"}}`)
	deleted := ""
	r.handlers["pg_list"] = fakeOK(map[string]any{"clusters": []any{map[string]any{"name": "shared-db", "id": "pg-foreign"}}})
	r.handlers["pg_delete"] = func(_ context.Context, a map[string]any) (string, bool) {
		deleted, _ = a["cluster_id"].(string)
		return toJSON(map[string]any{"status": "deleted"}), false
	}
	r.handleStackDestroy(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	if deleted != "" {
		t.Fatalf("job в статусе planned (ничего не создавал) удалил кластер %s", deleted)
	}
}

// R4: crank без write-ahead: ресурс создан, job не сохранён (kill -9) —
// следующий crank создаёт второй, не ища по имени.
func TestQAr3_StackCrank_NoSaveAfterCreate_DoubleCreate(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"c","postgres":{"name":"p"}}`)
	creates := 0
	r.handlers["pg_create"] = func(context.Context, map[string]any) (string, bool) {
		creates++
		return toJSON(map[string]any{"status": "creating", "cluster_id": "pg-1"}), false
	}
	r.handlers["pg_list"] = fakeOK(map[string]any{"clusters": []any{map[string]any{"name": "p", "id": "pg-1"}}})
	r.handlers["pg_get"] = fakeOK(map[string]any{"status": "CREATING"})
	job, _ := stack.LoadJob(j.ID)
	r.advance(context.Background(), job) // процесс умирает до job.Save()
	r.handleStackApply(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	if creates != 1 {
		t.Fatalf("pg_create вызван %d раз, want 1 (кластер p уже есть, pg_list его видит)", creates)
	}
}
```

### T-R5 — `internal/tools/qa_r3_provision_test.go`

```go
package tools

import (
	"context"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// R5: provision — между CreateVM и концом ожидания ВМ в state нет.
// (В постоянном тесте задайте Timings.VMPollInterval в фабрике, иначе опрос
// идёт раз в 5 с.)
type pollCheckCloud struct {
	cancelCloud
	dir       string
	seenState bool
	polls     int
}

func (c *pollCheckCloud) CreateVM(_ context.Context, req provision.CreateVMRequest) (*provision.VM, error) {
	c.creates++
	return &provision.VM{ID: "vm-made"}, nil
}
func (c *pollCheckCloud) GetVM(context.Context, string) (*provision.VM, error) {
	c.polls++
	if sf, err := state.Read(state.DefaultPath(c.dir)); err == nil {
		for _, d := range sf.Deployments {
			if d.VMID == "vm-made" {
				c.seenState = true
			}
		}
	}
	if c.polls >= 2 {
		return &provision.VM{ID: "vm-made", Status: "active"}, nil
	}
	return &provision.VM{ID: "vm-made", Status: "creating"}, nil
}
func (c *pollCheckCloud) GetFloatingIP(context.Context, string) (*provision.FloatingIP, error) {
	return &provision.FloatingIP{ID: "fip-boot", IPAddress: "203.0.113.9", State: "in_use"}, nil
}

func TestQAr3_Provision_VMRecordedBeforeWait(t *testing.T) {
	cloud := &pollCheckCloud{}
	r, dir := cancelRegistry(t, cloud)
	cloud.dir = dir
	_, _ = r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true})
	if !cloud.seenState {
		t.Fatalf("во время ожидания ВМ vm-made не было в state (kill -9 в этом окне = ВМ и FIP без записи)")
	}
}
```

### T-R2a, T-R8 — `internal/deployplan/qa_r3_test.go`

```go
package deployplan

import (
	"context"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
)

// R2 (б): повтор deploy_apply (adopt) не читает FIP, уже привязанный к
// интерфейсу, и создаёт второй.
type adoptFIPCloud struct {
	existingCloud
	fipCreates int
}

func (c *adoptFIPCloud) GetVMDetail(context.Context, string) (*provision.VMDetail, error) {
	return &provision.VMDetail{ID: "vm-old", Interfaces: []provision.VMInterface{{ID: "if-1", Primary: true, Type: "regular",
		FloatingIP: &provision.IfaceFloatingIP{ID: "fip-have", IPAddress: "203.0.113.5", State: "in_use"}}}}, nil
}
func (c *adoptFIPCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return []*provision.FloatingIP{{ID: "fip-have", IPAddress: "203.0.113.5", State: "in_use"}}, nil
}
func (c *adoptFIPCloud) CreateFloatingIP(context.Context, provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	c.fipCreates++
	return &provision.FloatingIP{ID: "fip-second", IPAddress: "203.0.113.6"}, nil
}

func TestQAr3_Apply_AdoptUsesAttachedFIP(t *testing.T) {
	c := &adoptFIPCloud{}
	res, err := Apply(context.Background(), c, &Plan{ProjectID: "p-1", VM: VMSpec{Name: "x"}, Network: NetworkSpec{FloatingIP: true}})
	if err != nil {
		t.Fatal(err)
	}
	if c.fipCreates != 0 || res.PublicIP != "203.0.113.5" {
		t.Fatalf("CreateFloatingIP вызван %d раз, PublicIP=%q; у ВМ уже есть 203.0.113.5", c.fipCreates, res.PublicIP)
	}
}

// R8: ВМ с тем же именем в состоянии stopped/creating усыновляется как "running".
type stoppedCloud struct{ existingCloud }

func (c *stoppedCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return []*provision.VM{{ID: "vm-old", Name: "x", Status: "stopped", Region: "zone-A"}}, nil
}
func (c *stoppedCloud) GetVMDetail(context.Context, string) (*provision.VMDetail, error) {
	return &provision.VMDetail{}, nil
}

func TestQAr3_Apply_AdoptReportsStoppedVMAsRunning(t *testing.T) {
	res, _ := Apply(context.Background(), &stoppedCloud{}, &Plan{ProjectID: "p-1", VM: VMSpec{Name: "x", Zone: RefSpec{ID: "zone-B"}}})
	if res != nil && res.Status == "running" {
		t.Fatalf("stopped ВМ из другой зоны усыновлена как running: %+v", res)
	}
}
```

### T-R6 — `internal/sgrules/qa_r3_test.go`

```go
package sgrules

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// R6: POST правила применён сервером, ответ потерян — правило есть, но
// запись его не знает; повторный deploy видит «уже открыт» и не владеет им.
type lostRuleCloud struct{ *fakeCloud }

func (c lostRuleCloud) AddSecurityGroupRule(ctx context.Context, sg, p string, r provision.SGRule) (*provision.SGRule, error) {
	c.fakeCloud.seq++
	r.ID = fmt.Sprintf("r%d", c.fakeCloud.seq)
	c.fakeCloud.rules[sg] = append(c.fakeCloud.rules[sg], r)
	return nil, errors.New("cannot reach Cloud.ru API: connection reset")
}

func TestQAr3_Open_LostAnswerAdoptsRuleByDescription(t *testing.T) {
	cloud := lostRuleCloud{newFake()}
	var rec state.DeploymentRecord
	res := Open(context.Background(), cloud, "p", "sg", []int{80}, nil, func(l string) string { return "evo: port " + l }, &rec)
	if len(rec.OpenedRules) == 1 {
		return
	}
	var rec2 state.DeploymentRecord
	res2 := Open(context.Background(), cloud, "p", "sg", []int{80}, nil, func(l string) string { return "evo: port " + l }, &rec2)
	t.Fatalf("первый Open: %+v rules=%v; второй: %+v rules=%v — правило r1 (0.0.0.0/0:80) никому не принадлежит, destroy его не закроет",
		res, rec.OpenedRules, res2, rec2.OpenedRules)
}
```

### T-R7 — `internal/engine/qa_r3_test.go`

```go
package engine

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/compose"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// R7: POST boot-FIP применён сервером, ответ потерян; CreateVM затем
// отказывает — FIP "fip-evo-…-<ts>" остаётся, нигде не записан.
type lostBootFIPCloud struct {
	interruptCloud
	fips     []*provision.FloatingIP
	released []string
}

func (c *lostBootFIPCloud) CreateFloatingIP(_ context.Context, req provision.CreateFloatingIPRequest) (*provision.FloatingIP, error) {
	c.fips = append(c.fips, &provision.FloatingIP{ID: "fip-lost", Name: req.Name, IPAddress: "203.0.113.7", State: "available"})
	return nil, errors.New("cannot reach Cloud.ru API: connection reset")
}
func (c *lostBootFIPCloud) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return c.fips, nil
}
func (c *lostBootFIPCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) { return nil, nil }
func (c *lostBootFIPCloud) CreateVM(context.Context, provision.CreateVMRequest) (*provision.VM, error) {
	return nil, errors.New("API returned status 422: quota")
}
func (c *lostBootFIPCloud) DeleteFloatingIP(_ context.Context, id, _ string) error {
	c.released = append(c.released, id)
	return nil
}

func TestQAr3_Deploy_LostBootFIPAnswer(t *testing.T) {
	cloud := &lostBootFIPCloud{}
	dir := t.TempDir()
	var out bytes.Buffer
	e := &Engine{Config: config.Config{ProjectID: "p", ImageID: "img"}, Cloud: cloud,
		Printer: output.NewWithWriter(&out, output.ModePlain, false), StateDir: dir, Timings: fastTimings}
	_, err := e.provisionVM(context.Background(), DeployOptions{NonInteractive: true, Yes: true}, &state.File{Version: 1},
		state.DefaultPath(dir), &compose.Project{Hash: "h"}, "flavor", "az")
	if err == nil || !strings.Contains(err.Error(), "422") {
		t.Fatalf("provisionVM = %v", err)
	}
	if len(cloud.released) == 0 {
		t.Fatalf("FIP %q создан (ответ потерян), ВМ нет — FIP не освобождён и не записан; вывод:\n%s", cloud.fips[0].Name, out.String())
	}
}
```

### T-R12 — `internal/provision/qa_r3_test.go`

```go
package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
)

// R12: сервер без поля total — читается только первая страница.
func TestQAr3_ListAll_NoTotalField(t *testing.T) {
	const total = 130
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		var items []map[string]any
		for i := offset; i < total && i < offset+100; i++ {
			items = append(items, map[string]any{"id": fmt.Sprintf("x%d", i)})
		}
		if items == nil {
			items = []map[string]any{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	})
	defer srv.Close()
	l, err := a.ListFloatingIPs(context.Background(), "p")
	if err != nil || len(l) != total {
		t.Fatalf("got %d, %v; want %d", len(l), err, total)
	}
}
```

### T-M1 — `internal/mcp/qa_r3_kube_test.go` (M1–M4)

```go
package mcp

import (
	"strings"
	"testing"
)

// M1–M4: политика k8s_kubectl по HTTP пропускает аргументы, которые
// печатают данные Secret или читают файлы хоста без --allow-secret-export.
func TestQAr3_KubectlHTTPCheckSecretBypasses(t *testing.T) {
	cases := []string{
		"get secret x --template={{.data}}",                       // M1
		"get secret x -o name -v=8",                               // M2
		"describe secret x -v=9",                                  // M2
		"label secret x a=b -o yaml --dry-run=server",             // M3
		"create secret tls t --cert=/etc/ssl/host.pem --key=/etc/ssl/host.key --dry-run=client -o yaml", // M4
	}
	for _, c := range cases {
		if err := kubectlHTTPCheck(strings.Fields(c), false); err == nil {
			t.Errorf("%q: allowed over HTTP, must be refused", c)
		}
	}
}
```

Результат на `main`: все пять случаев «allowed over HTTP».

### T-C2 — `internal/cli/qa_r3_test.go`

```go
package cli

import (
	"errors"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// C2: ответ инструмента с кодом, у которого exit 1, сохраняет код и reason.
func TestQAr3_ToolErrorKeepsAPIUnreachable(t *testing.T) {
	err := toolError("vm_list", `{"error":"API_UNREACHABLE: IAM unreachable","status":"error"}`)
	var ce *clerrors.CLIError
	if !errors.As(err, &ce) {
		t.Fatalf("not a CLIError: %v", err)
	}
	if ce.Code != clerrors.CodeAPIUnreachable || clerrors.ExitCode(err) != 1 {
		t.Fatalf("code=%s exit=%d, want API_UNREACHABLE/1 (docs/exit-codes.md)", ce.Code, clerrors.ExitCode(err))
	}
}
```

Результат на `main`: `code=API_ERROR exit=5`.

### T-C1 (S2) — `internal/cli/qa_r3_project_test.go` (набросок, не прогонялся как файл; поведение подтверждено запуском бинаря)

```go
// Профиль с проектом p-1, .evo.yaml с p-2, `evo whoami --project-id p-9 --json`
// (IAM подменён как в TestWhoami*): ожидается "project_id":"p-9",
// "project_from":"flag" и отсутствие строки {"warning":"проект p-2 из .evo.yaml …"}
// в stderr. Сейчас: project_id p-2, project_from "project", предупреждение есть.
```

## Проверено и в порядке

**Сборка и статика.** `go build`, `go vet ./...`, `gofmt -l .` — чисто.
`GORACE=atexit_sleep_ms=0 go test -race ./...` — 28 пакетов `ok`.
`govulncheck ./...` — 0 уязвимостей в вызываемом коде; GO-2026-6599/6600
(`html/template`, stdlib go1.26.0) не вызываются, CI и релиз собираются
go1.26.9 (`ci.yml:40`, `release.yml:46`). `TestCommandsDoc` проходит —
`docs/commands.md` актуален.

**Соответствие DECISIONS.** D4 (файлы 0600/0700, Windows ACL по коду
`private_windows.go`: protected DACL для пользователя/SYSTEM/Administrators,
как у OpenSSH; на Windows не запускалось), D7 (147 инструментов, 64
изменяющих; деревья `vm disk vpc k8s pg s3 secret` + `project`), D9
(`isError:true` у `confirmation_required`), D10 (поле `approval` во всех
превью), D11 (`status`/`destroy` без развёртывания — 0; `logs`/`verify` — 4;
аргументы, отвергнутые инструментом — `USAGE`/1; S3 без ключей — 2), D12
(проект в записи state и в задании стека; `destroy` CLI работает в проекте
записи; `deploy` отказывает при расхождении; превью `deploy`/`destroy`/`provision`
называют проект и источник; `--allow-project` применяется на каждом
построении engine; без флага — предупреждение при старте HTTP), D6 пересмотр
(справка, ошибки, подсказки, превью CLI — русские; MCP — английский при любом
`EVO_LANG`; `reason` в каждой JSON-ошибке; коды/флаги/JSON-ключи английские).
Отклонения — I1, I2, C4, C7, C10.

**Гейт подтверждения.** CLI: `--dry-run` раньше `--yes` (`--yes --dry-run` →
dry-run, код 0, ничего не создаётся, `.evo/` не появляется); без терминала и
без `--yes` — код 3, с `--json` в stdout `{"status":"confirmation_required",…}`
и в stderr `{"error":{"code":3,"reason":"CONFIRMATION_REQUIRED"…}}`;
`echo y |` — не терминал; Enter/EOF — нет; `--arg confirmed=true` отвергается
(«у destroy нет аргумента confirmed»). MCP: механизм в каждом из 64
изменяющих инструментов (`args["confirmed"].(bool)`), строка `"true"` и `1`
не проходят; `mutating` вычисляется из схемы и сверен со списком; помеченных
без гейта и изменяющих без пометки нет; батч «превью + confirmed» в одном
POST выполняется последовательно (граница — на вызов, docs это оговаривают);
`stack_apply`/`stack_destroy` подтверждают дочерние шаги одним согласием
(задокументировано). Паника инструмента → ошибка инструмента, сервер живёт.

**CLI deploy/destroy (engine).** Порядок: согласие → SSH-ключ → boot-FIP →
`CreateVM` → запись `creating` с `FloatingIPID`/`SecurityGroupID` → сигналы →
ожидание → FIP → порты с записью id правил. До согласия — только чтение.
Потерянный ответ `CreateVM` → `FindCreatedVM` по имени и времени, запись
есть, FIP не освобождается. Ошибка записи state называет `vm_id`. Ctrl-C и
`ctx.Done` → один путь: без TTY ВМ остаётся с записью `creating` и кодом 130;
с «y» — ожидание удаляемости, освобождение FIP с повторами, запись снимается
только после. `destroy`: блокировка state, превью и гейт, статус `destroying`,
ожидание 409/422 до 10 мин, 404 = «уже удалена» с освобождением FIP, FIP по
id → адресу → имени `fip-<vmID>` с 5 попытками, при неудаче запись остаётся;
правила SG после ВМ, чужие и нужные другому развёртыванию не трогаются;
идемпотентно. FIP ищется и удаляется в проекте записи (D12).
Хост-ключ SSH: TOFU при первом подключении, отпечаток пишется в state до
дальнейших шагов, несовпадение → `SSH_HOST_KEY_CHANGED` (принятый риск
первого подключения).

**state.** Запись tmp + fsync + rename; `flock`/`LockFileEx` на `state.lock`,
снимается ОС при падении; `deploy‖deploy`, `deploy‖destroy` в одном каталоге
→ `STATE_LOCKED`; MCP пишет через `state.Update` под `LockWait` (2 мин);
каталог 0700; `project_id` пишется, старые записи без него берут проект
конфигурации; версия файла проверяется.

**stack.** `~/.evo/stack-jobs` 0700/0600, секреты вычищаются после шага;
`job_id` валидируется regex; mutex процесса + файловая блокировка (Windows
через `LockFileEx`); два HTTP-клиента или stdio+HTTP создают ресурс один раз
(`TestQA_StackConcurrentCranksCreateOnce`); проект в задании и
`checkJobProject` для apply/status/destroy (P2/P3); мягкая отмена во время
create не тратит попытку; порядок destroy dns→lb→tg→k8s→kafka→redis→snat→registry→pg→vm→secrets→s3→vpc.

**provision.** Пагинация `limit=100`/`offset` до `total`, предел 100 страниц,
пустая страница останавливает, bare-array поддержан; вложенные поля через
указатели, nil-safe, неожиданный тип → ошибка decode, не паника; POST не
ретраится, GET/DELETE — да.

**MCP HTTP.** Bearer: `subtle.ConstantTimeCompare`, токен только из env, в
лог не пишется, санитайзер маскирует `MCP_BEARER_TOKEN=`; без заголовка /
неверный → 401 с `WWW-Authenticate`. `--auth none`: отказ на `0.0.0.0`,
`[::]`, `:PORT`, внешних адресах; `Host` при `--auth none`: `evil.com`,
`127.0.0.1.nip.io`, `localhost.`, `0.0.0.0`, `127.1`, `0x7f000001`,
отсутствие Host → 403; `localhost`, `[::1]`, `127.0.0.2`, `::ffff:127.0.0.1`
→ 200. `Origin`: пустой — ок, `null`/чужой → 403 в обоих режимах,
loopback-Origin любой порт — ок. `/healthz` — только `{"status":"ok"}`.
Лимиты: `MaxBytesReader` 16 МиБ до парсинга (17 МБ → 413), 60 с на тело
через `SetReadDeadline` (→ 408), `ReadHeaderTimeout` 10 с, `IdleTimeout`
120 с; батч 101 → 413, `initialize` в батче → 400; семафор 32 неблокирующий,
освобождение в `defer`. Сессии: 16 байт `crypto/rand`, TTL 24 ч,
неизвестная → 404, `DELETE` отменяет вызовы своей сессии. `--allow-project`:
`checkAllowedProject` на каждом построении engine; `deploy`/`provision`
передают `project_id` как override → проверяется; `iam_service_accounts`/
`org_quotas` — документированное исключение; `.evo.yaml`/`CLOUDRU_PROJECT_ID`
меняют проект сервера, но он тоже проверяется; `plan_id`/`job_id`
валидируются, traversal невозможен; `stack_apply` проверяет проект задания.
Маскирование: `secret_value`, `cert_private_key`, `k8s_kubeconfig`
(в том числе `embed_token:"false"` строкой) → отказ без
`--allow-secret-export`; `k8s_kubectl config …` → отказ; `deploy_apply.password`,
`*_get` password маскируются; `pg_user_create` не эхо-ит пароль; обёртка
применяется ко всем инструментам, включая шаги `stack_*`. Хост:
`compose_file` — `EvalSymlinks` cwd и ближайшего существующего предка (`../x`,
`/etc/passwd`, симлинки, `sub/root/etc/passwd` → отказ); `k8s_kubectl` —
allowlist verbs, verb = первое не-флаговое слово, denylist флагов с разбором
`--k=v`/`-fFILE`/`-ojson`, `--raw` → отказ, `childEnv` вычищает `CLOUDRU_*`
и `MCP_BEARER_TOKEN`; `k8s_helm` — только `https://`/`oci://`, резолв всех
A/AAAA, loopback/private/link-local/CGNAT → отказ, локальный chart → отказ,
структурные аргументы (`--set-file`, `--post-renderer`, `--kubeconfig`
недоступны); `k8s_apply` — манифест через stdin. Логи: только `tool`,
`status`, `duration_ms`; аргументов и результатов нет.

**MCP stdio.** `confirmation_required` с `approval`; SIGINT/SIGTERM → код
130; `EVO_LANG=ru evo mcp` — ответы английские; предупреждение о `.evo.yaml`
— в stderr по-английски.

**Ключи и файлы.** `credentials.yaml`/`config.yaml`: `WriteFileAtomic`
(temp 0600 + rename), каталог `MkdirPrivate`, блокировка `.lock` O_EXCL с
ожиданием 10 с; права 0644 → предупреждение в каждой команде и «ошибка» в
`doctor`; битый YAML → `CONFIG_INVALID`/2 с номером строки; `--profile nope`
→ 2; нет secret → `AUTH_REQUIRED`/2. Кэш токена: `token-<profile>.json`
через `WriteFilePrivate`, ключ кэша = key_id + отпечаток секрета (смена
ключа = промах). SSH-ключ: каталог 0700 (симлинк отвергается), ключ через
temp 0600 + rename, `.gitignore` создаётся; `CLOUDRU_*` не передаются
дочерним `kubectl`/`helm`. `CLOUDRU_API_KEY=id:secret` работает с
предупреждением об устаревании.

**Проекты.** Приоритет флаг > `CLOUDRU_PROJECT_ID` > `.evo.yaml` (или legacy
`.cloudru-vm.yaml`) > профиль — по коду и запуском во всех комбинациях;
предупреждение о подмене — в stderr, с `--json` строкой `{"warning":…}`,
stdout не ломается. `project use`: неоднозначное имя → 2 с перечнем UUID;
несуществующий UUID → 2, профиль не меняется; без списка UUID сохраняется с
предупреждением; права 0600 сохраняются.

**Workflows и поставка.** `${{ inputs.* }}` и `github.event.*` в `run:` не
подставляются (через `env` + регэксп); `pull_request_target` нет;
`permissions` минимальны на уровне workflow/job; `persist-credentials: false`
везде, кроме `tag.yml` (W12); все `uses:` закреплены по SHA (checkout v7.0.1,
setup-go v7.0.0, goreleaser-action v7.3.0, cosign-installer v4.1.2);
hadolint, govulncheck, staticcheck закреплены; Dependabot для actions/gomod/docker.
Релиз: `checksums.txt` + cosign `sign-blob --bundle` keyless по OIDC
(`id-token: write` только у job `goreleaser`), `cache: false`,
`CGO_ENABLED=0 -trimpath`; `install.sh`: `set -eu`, mktemp + trap,
`--certificate-identity` с точным тегом и `--certificate-oidc-issuer`,
sha256 архива из подписанного `checksums.txt`, извлечение только именованных
членов, атомарная замена; identity согласована с docs/install.md и
docs/RELEASING.md. `api-probe.yml`: печатаются только коды HTTP, имена полей
и количества; тело запроса токена через stdin; `add-mask` на токен и проект.
`cloud-e2e.yml`: секреты через `env`, `--secret-stdin`, `cancel-in-progress: false`,
очистка под `if: always()`, поиск сирот по имени. Dockerfile: оба образа по
digest, `USER nonroot`, `HEALTHCHECK` без shell, `.dockerignore` исключает
секреты. `qwen-review`: триггер только `author_association == OWNER`, код PR
не исполняется.

**Руководство (команды и примеры).** Все команды §3–§8 и шпаргалки
существуют и дают ожидаемые коды: `version`/`--json`, `--help` на русском,
`EVO_LANG=en` — английская справка и ошибки, `EVO_LANG=xx` — предупреждение и
русский; `whoami`/`init` без ключа — 2 `AUTH_REQUIRED`; `configure` без
терминала — 2 с подсказкой про `--secret-stdin`; `status`/`destroy` без
развёртывания — 0, `verify`/`logs` — 4; `deploy --dry-run` без compose-файла —
1 `COMPOSE_NOT_FOUND`; `tools list/describe/call`, неизвестный инструмент —
`USAGE`/1, `--arg` с неверным bool — ошибка; `s3 buckets` без ключей — 2;
`completion bash` работает; пример `jq '.vms[]'` соответствует ключу ответа
`vm_list`; `vpc fip-release`, `project list/use`, `--project-id` на одну
команду — есть. SIGINT/SIGTERM при зависшей сети: `vm list`, `whoami --json`
(`reason: INTERRUPTED`), `deploy --dry-run` — код 130. `mcp --http` без
токена — 2; короткий токен — WARN; без `--allow-project` — WARN.

**Не проверено (нет облака/окружения).** Реальные ответы Cloud.ru на предмет
полей с секретами (M11); наличие `total` в ответах (R12); поведение API при
втором FIP на интерфейсе (R2); уникальность имён кластеров (R3/R4); helm
(M12); Windows ACL и `LockFileEx` — только по коду; сборка под
solaris/aix (R13); чужой проект в `.evo.yaml` против реального compute (P4).

## Руководство для коллег: где расходится с реальным поведением

| Место | Руководство | Реальность | Находка |
|---|---|---|---|
| §7 | «`evo whoami` показывает, откуда взят проект»; «другой проект на одну команду — `--project-id`» | `whoami`/`doctor` игнорируют `--project-id` и показывают проект из профиля/`.evo.yaml`/env с ложным предупреждением | C1 |
| §7, шпаргалка | «`evo project list` — проекты ключа» для ключа, который видит несколько проектов | С ключом уровня проекта (рекомендованным в §1) `project list` падает с кодом 5 и подсказкой «передайте --project-id» | C3 |
| §6 | коды выхода: «`1` — ошибка использования … `5` — ошибка API» и exit-codes.md: `API_UNREACHABLE` — 1 | `vm list`, `disk list`, `tools call` при недоступной сети — 5 `API_ERROR`; `whoami`/`deploy` — 1 | C2 |
| §9 «Язык», README «Язык» | «CLI говорит по-русски: … ошибки и подсказки» | Ошибки compute (401/403/404/429/5xx) — английские; превью `disk delete`/`vm delete`/`k8s kubectl` — английский текст для агента | I1, C4 |
| §4, README | «Описания инструментов в `tools describe` … английские» (справка `tools`: «и ответы инструментов — на английском») | Ответ `tools call` — смесь: ошибки engine/config на русском внутри JSON | I2 |
| §3 | «Сервисному аккаунту Cloud.ru не отдаёт список проектов, поэтому UUID … попросит ввести вручную — это ожидаемо» | `configure` показывает список, если ключ его видит (org-ключ); категоричная формулировка вводит в заблуждение владельцев org-ключей | P5 |
| §8 (docs/mcp.md) | «`k8s_kubectl` не выполняет … `get secret … -o yaml\|json\|jsonpath…` (данные Secret)» | `--template`, `-v=8`, `label … -o yaml --dry-run=server` отдают данные Secret без `--allow-secret-export` | M1–M3 |
| docs/mcp.md «Отмена вызовов» | «`provision` … записывают ВМ в `.evo/state.json` (статус `creating`)» | Только при мягкой отмене; между `CreateVM` и концом ожидания записи нет | R5 |
| docs/mcp.md | «`--verbose` — подробнее» | stderr идентичен | M10 |
| docs/mcp.md | «Без `MCP_BEARER_TOKEN` сервер не стартует (код `2`)» | верно; `--auth none` на внешнем адресе — код 1 (не описано) | C12 |
| docs/auth.md §5 | образец `configure` с английскими подсказками | подсказки русские | I4 |
| docs/troubleshooting.md | «Прерванный `deploy` … команда спросит, удалить ли её» — соответствует; все 12 цитируемых сообщений совпадают с кодом | — | ок |

## Что добавить в BACKLOG (вне rc.4)

R9 (переиспользование чужого FIP), R12–R15, M5–M9, M11–M12, C5–C12, I3–I4,
P1–P5, W2–W13.
