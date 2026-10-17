# MCP-сервер

`evo mcp` — сервер [Model Context Protocol](https://modelcontextprotocol.io):
AI-агент (Claude Code, Cursor и любой другой MCP-клиент) получает ~150
инструментов Cloud.ru — ВМ, диски, сети, DNS, балансировщики, Kubernetes,
PostgreSQL, Kafka, Redis, S3, секреты, сертификаты, Artifact Registry,
мониторинг, биллинг, `deploy` docker-compose на ВМ. Каталог всех
инструментов по группам — [tools.md](tools.md); описания и схемы аргументов
— `evo tools list` и `evo tools describe <имя>`; любой инструмент можно
вызвать и без агента: `evo tools call`.

Два транспорта:

| | stdio | Streamable HTTP |
|---|---|---|
| Запуск | клиент сам запускает `evo mcp` | `evo mcp --http :8080` или Docker-образ |
| Для чего | агент на вашей машине | общий сервер, контейнер |
| Аутентификация клиента | не нужна (локальный процесс) | `Authorization: Bearer $MCP_BEARER_TOKEN` |
| Креды Cloud.ru | ваш профиль / переменные окружения | профиль / переменные окружения **сервера** |

Версии протокола: `2025-06-18`, `2025-03-26`, `2024-11-05` (сервер отвечает
той, что запросил клиент, если знает её).

Креды Cloud.ru всегда принадлежат серверу и находятся так же, как для
обычных команд ([auth.md](auth.md#6-откуда-берутся-настройки)): переменные
окружения, профиль из `evo configure`, `./.evo.yaml` в рабочем
каталоге. Клиент их не передаёт и не видит.

## Подтверждения

Изменяющий инструмент (создаёт платный ресурс или удаляет данные) без
`confirmed=true` ничего не делает и возвращает превью:

```json
{"status": "confirmation_required", "message": "deploy creates a billable VM … call again with confirmed=true.", "will_deploy": {…}}
```

Поле `status` одинаково у всех инструментов; остальные поля превью (что
будет создано или удалено) у каждого свои. Сервер добавляет к каждому такому
ответу поле `approval` — одну и ту же для всех инструментов инструкцию
агенту: показать превью человеку и ставить `confirmed=true` только после его
согласия.

Агент должен показать превью человеку и, получив согласие, вызвать тот же
инструмент с теми же аргументами и `"confirmed": true`. Граница проходит в
сервере, до обращения к облаку: агент, который «сам решил» удалить кластер,
упирается в неё. Но это граница для вызова, а не для агента: модель может
повторить вызов с `confirmed=true`, не спросив человека. Поэтому для
изменяющих инструментов включайте в MCP-клиенте подтверждение каждого вызова
человеком, а по HTTP отдавайте только те инструменты, которые агенту можно
вызывать без присмотра. Поле `mutating` в `evo tools list --json` показывает,
какие инструменты так устроены. В `tools/list` то же видно по стандартным
аннотациям MCP: `readOnlyHint: true` у чтений, `destructiveHint: true` у
удалений (`*_delete`, `destroy`, `stack_destroy`, `s3_rm`,
`release_floating_ip`) и у инструментов, которые могут удалить или
перезаписать данные (`k8s_kubectl`, `k8s_apply`, `k8s_helm`, `s3_put`,
`k8s_add_addon` — с `resolve_mode=OVERWRITE` он заменяет объекты в кластере) —
клиент может по ним одобрять чтения без вопроса и всегда спрашивать перед
разрушительным вызовом. Аннотации — подсказка, гейт `confirmed=true` от них
не зависит. CLI ведёт себя так же (`--yes`, `--dry-run`, код выхода `3`, см.
[README](../README.md#подтверждения)).

`k8s_kubectl` без подтверждения выполняет только чтения (`get`, `describe`,
`logs`, …). В любом транспорте он отклоняет (`USAGE`) флаги, которые
подменяют кластер или учётные данные kubeconfig, написанного evo:
`--server`/`-s`, `--kubeconfig`, `--token`, `--username`, `--password`,
`--client-certificate`, `--client-key`, `--certificate-authority`,
`--insecure-skip-tls-verify`, `--tls-server-name`. С ними даже `get` отправил
бы токен кластера на другой сервер. Кластер выбирает `cluster_id`. Отклоняется
и `--kuberc`: evo запускает kubectl с `KUBERC=off`, чтобы файл настроек
kubectl (`~/.kube/kuberc`: алиасы, значения флагов по умолчанию) не менял его
команды.

Сейчас ответ `confirmation_required` приходит с `isError: true` — клиенту
стоит различать исход по полю `status`, а не по `isError`. Без исключений:
`s3_bucket_create` тоже ждёт `confirmed=true`.

## Ошибки

Ошибка инструмента — `{"status":"error","reason":"…","error":"…","hint":"…"}`
с `isError: true`, например
`{"status":"error","reason":"USAGE","error":"vm_get requires vm_id or name"}`:

- `reason` есть всегда — символьный код из [таблицы](exit-codes.md):
  `USAGE` (инструмент отверг аргументы, в облако ничего не ушло),
  `NOT_FOUND`, `AUTH_FAILED`, `API_UNREACHABLE`, `API_ERROR` и т. д. Ошибка
  без своего кода получает причину по происхождению: ответ API — по
  HTTP-статусу (`404` — `NOT_FOUND`, `429` — `API_RATE_LIMITED`, прочие —
  `API_ERROR`), как и сбой операции облака и ошибка kubectl/helm от
  кластера; нет связи или истёк срок — `API_UNREACHABLE`; нет `kubectl`/`helm`
  — `CONFIG_INVALID`; ошибка о ВМ, которую создал `provision`, —
  `VM_PROVISION_FAILED`; остальное — локальная ошибка, в облако ничего не
  ушло: `NOT_FOUND`, если в ответе сказано «not found», иначе `USAGE`. Так же
  её понимает CLI (`evo tools call`, код выхода).
- `error` — только фраза, без префикса `КОД: `. Код остаётся внутри текста
  лишь после добавленного контекста (`create VM: API_ERROR: …`), как в
  сообщениях CLI. Решайте по `reason`, а не по тексту.
- `hint` — что сделать дальше, когда это известно; иначе поля нет.

Аргументы сверяются со схемой инструмента до обработчика: аргумент, которого
нет в `inputSchema`, — `USAGE` со списком аргументов инструмента в `hint`
(`confirmed` принимает любой инструмент); значение поля с `enum`
сравнивается без учёта регистра (`Private` → `private`), чужое — `USAGE`.
Скаляр не того типа, что в схеме (`"confirmed": "true"`, `"size_gb": "20"`,
дробное число в целом поле), — тоже `USAGE` с именем поля: раньше такое
значение молча считалось отсутствующим. Массивы и объекты по типу не
сверяются (`ports` можно передать и строкой `"80,443"`).

У части ошибок есть свои поля: `vm_id` у `provision`, когда ВМ уже создана
(её надо удалить); `output` и `stage` у `k8s_kubectl`, `k8s_apply`,
`k8s_helm`. `provision`, не дождавшийся ВМ, отвечает `"status":"timeout"`
с теми же `reason` и `error`. Отказ политики HTTP (`secret_value`,
`cert_private_key`, `k8s_kubeconfig` с токеном, путь вне рабочего каталога)
— `USAGE`; нет `kubectl`/`helm` на сервере — `CONFIG_INVALID` с подсказкой;
сервер занят (уже идут 32 вызова инструментов) — `SERVER_BUSY`: это лимит
самого `evo mcp --http`, в Cloud.ru запрос не уходил, повторите позже. Ответ
`confirmation_required` — не ошибка, поля `reason` у него нет.

Неизвестный инструмент (и инструмент вне `--toolsets`) — ошибка протокола
JSON-RPC `-32602` с сообщением `Unknown tool: <имя>`, как требует MCP
2025-06-18, а не результат с `isError`.

## Цена

Цену ВМ возвращает `deploy_plan` в поле `cost`. Она берётся из API расчёта
цены Cloud.ru (`POST /api/v1/projects/{project_id}/price-calculation`: только
расчёт, ничего не создаёт) с тайм-аутом 5 с; если API не ответил, вернул
ошибку или отказал (два образа Astra Linux отвечают `422 sku_identifier
cannot be blank`), — оценка по встроенной таблице тарифов, как раньше.
Превью `provision` и `deploy` без `confirmed` в облако не ходят: в
`preview.cost` `provision` — по-прежнему оценка (или её нет, если размер
flavor неизвестен), за ценой из API — `deploy_plan`.

Поля `cost` (суммы в рублях):

| Поле | Из API | Оценка |
|---|---|---|
| `source` | `price-calculation API (Cloud.ru); image license not included` | `estimate: rate-card (vCPU+RAM); not a billed price` или `… (flavor) …` |
| `total_monthly_rub` | `total_price_month`: ВМ, диск и floating IP | ВМ и диск |
| `compute_monthly_rub`, `disk_monthly_rub` | `flavor` и `disks` за месяц | по таблице |
| `hourly_rub` | flavor за час | вычисления за час |
| `total_hourly_rub` | `total_price_hour` | — |
| `floating_ip_monthly_rub` | `floating_ips` за месяц | — |
| `breakdown` | ответ API как есть: `flavor`, `disks`, `floating_ips`, `image`, `mkp_image`, `custom_image_size`, `nat_gateways` и `period` | — |
| `price_api_error` | — | почему цены из API нет: `HTTP 422: …`, `no answer within 5s`, … |
| `license_note` | у образа маркетплейса: лицензии в цене нет | то же |

Прежние поля (`compute_monthly_rub`, `disk_monthly_rub`, `total_monthly_rub`,
`hourly_rub`, `currency`, `source`) остались с теми же именами; новые
добавлены рядом. `total_monthly_rub` из API включает floating IP, а оценка —
нет: не складывайте его с `disk_monthly_rub`. Период составляющих API не
называет: `breakdown.period` — `month` или `hour`, смотря с каким итогом они
сходятся; `unknown` — не сошлись, тогда известны только итоги, а
`compute_monthly_rub`, `disk_monthly_rub` и `floating_ip_monthly_rub` равны 0.

**Лицензия образа в цену не входит** ни в одном источнике: API отвечает
`image` = 0 и `mkp_image` = 0 за любой образ, в том числе Astra Linux KES
(проба 16.10.2026). Как оплачивается лицензия продукта маркетплейса —
открытый вопрос к Cloud.ru; покажите человеку `license_note` и условия из
карточки продукта.

## Отмена вызовов

Общего таймаута на вызов нет: `deploy` или создание кластера идут столько,
сколько нужно. Вызов отменяется, когда его больше никто не ждёт:

- уведомление `notifications/cancelled` с `requestId` вызова (MCP
  2025-06-18): вызов прерывается, ответа на него нет. По HTTP уведомление
  действует внутри своей сессии (`Mcp-Session-Id`); без заголовка — среди
  вызовов без сессии. Ответ в виде JSON (не SSE) на отменённый вызов всё же
  приходит — у POST должно быть тело, клиент его игнорирует;
- **stdio:** процесс получил SIGINT/SIGTERM. После сигнала сервер ждёт
  завершения прерванного вызова до 10 с; повторный Ctrl-C убивает процесс
  сразу. Закрытый stdin вызовы **не** отменяет: строки, прочитанные до конца
  stdin, выполняются до конца и получают ответ, потом сервер завершается —
  `printf … | evo mcp` с долгим `deploy` дождётся результата.
  Недоведённый до конца `deploy` дороже законченного, ответ на который никто
  не прочитал;
- **HTTP:** клиент закрыл соединение запроса (возобновления SSE-потока по
  `Last-Event-ID` нет — ответ всё равно было бы некуда доставить), удалил
  сессию (`DELETE /mcp` отменяет все её вызовы) или сервер останавливается.
  Спецификация советует не считать обрыв отменой; здесь обрыв отменяет вызов
  намеренно — иначе ушедший клиент оставлял работать опрос `provision`
  (до 5 мин) и `k8s_helm` (до 9 мин). В логе вызова — `status=cancelled: …`
  с причиной.

Отмена после того, как запрос создания ушёл в облако, не теряет ресурс:
`provision` и `deploy_apply` записывают ВМ в `.evo/state.json`
(статус `creating`) и называют её `vm_id` в ошибке — `status`/`destroy` её
видят; план `deploy_apply` остаётся, повтор с тем же `plan_id` подхватывает
ВМ по имени, а не создаёт вторую. `deploy` пишет запись до ожидания ВМ, как в
CLI. Шаг `stack_apply`, прерванный на создании, не тратит попытку: следующий
`stack_apply`/`stack_status` сначала ищет ресурс (по id из ответа или по
имени) и создаёт его заново, только если не нашёл. Если отмена пришла, пока
сам POST создания ещё в пути, облако могло успеть создать ресурс, не
вернув id: у `deploy_apply` и шагов стека с поиском по имени повтор его
найдёт, у `provision` проверьте `vm_list`.

План `deploy_apply` удаляется, только когда ВМ работает. Если ВМ не
поднялась к сроку (`timeout`) или подхваченная ВМ ещё создаётся, ответ —
ошибка `VM_PROVISION_FAILED` с `vm_id`, `vm_status` и `plan_id`. Повтор с тем
же `plan_id` дожидается ВМ, привязывает floating IP и открывает порты плана.
ВМ в состоянии ошибки — ошибка с подсказкой удалить её через `destroy`, план
удаляется.

## Состояние развёртывания

`status` читает запись развёртывания из `.evo/state.json` и по умолчанию
спрашивает облако о её ВМ: один `GET /vms/{id}`, не дольше 8 с вместе с
получением токена. К записи добавляются поля:

- `cloud_checked` — облако ответило (ВМ есть или 404);
- `cloud_state` — живое состояние ВМ (`running`, `stopped`, …);
- `not_found_in_cloud: true` — ВМ удалили вне evo (в консоли); запись
  уберёт `destroy` (без аргументов или с `vm_id`): освободит её floating IP
  и закроет правила портов; с `vm_id` ответ — `status: "already_deleted"`;
- `cloud_error` — почему облако не проверено (нет ключа, сеть, ошибка API),
  фразой без кода; `cloud_reason` — код этой ошибки (`API_UNREACHABLE`,
  `AUTH_REQUIRED`, …), как `reason` у ошибки инструмента; запись всё равно
  возвращается, вызов не считается ошибкой.

`refresh: false` — только state, без запроса (`cloud_checked: false`).
Инструмент только читает: state он не меняет. `list` по-прежнему читает
только state.

## Только нужные группы инструментов

По умолчанию сервер предлагает агенту все 152 инструмента. Это около 100 КБ
`tools/list`, и агент читает их в каждой сессии. Если нужен только deploy и
работа с ВМ и сетью, оставьте эти группы:

```sh
evo mcp --toolsets deploy,vm,vpc     # 26 инструментов, ~18 КБ
```

Группы перечислены в столбце ГРУППА вывода `evo tools list`: deploy, vm,
disk, image, vpc, k8s, pg, s3, secret, stack и другие. `--toolsets all` или
отсутствие флага включает все группы, неизвестная группа — ошибка с кодом 1.
Флаг работает и для stdio, и для `--http`.

Флаг экономит контекст агента, но прав не ограничивает: если в наборе есть
группа `stack`, `stack_apply` сам вызывает инструменты других групп (без
`stack` в `--toolsets` он агенту недоступен). Права ограничивают
`--allow-project`, ограничения HTTP-режима и роли сервисного аккаунта.

## Образы ВМ

Агент находит образ по имени, а не по UUID:

- `image_list` (`query`, `type` = `public` | `marketplace` | `private`,
  `zone`, `all`) — поиск по подстроке имени или отображаемого имени без учёта
  регистра; компактные строки `{id, name, display_name, type, zones_ready,
  min_disk_gb}` без иконки, шаблона cloud-init и объектов зон, плюс `shown`,
  `total`, `hidden`. Образы, не готовые ни в одной зоне, и образы общего
  каталога, недоступные проекту, скрыты без `all=true`; вторые считает
  `hidden_not_in_project`, а с `all=true` их строки несут
  `available_in_project: false`, и в ответе есть `note` с объяснением.
  `list_images` остаётся как был: без аргументов, полные объекты образов
  каталога и проекта (без проверки доступности); для поиска лучше
  `image_list`.
- `image_get` (`image` — имя, отображаемое имя или UUID; `image_id` — как
  раньше; `full`) — карточка: `ready`, `zones_ready`, `fields[]` с
  `filled_by` (`evo`, `default`, `user`, `none`, `status`), `fields_required`
  и `create{cli, tool, args}`. Иконка, `user_data_template` и исходные
  `image_metadata` — только с `full=true`. Несколько совпадений — ошибка `USAGE` с `candidates[]`, ни
  одного — `NOT_FOUND`. Образ, недоступный проекту, `image_get` всё равно
  отдаёт — с `available_in_project: false`, `note` и без `create`.

Доступность проекту. Общий каталог (`GET /images` без `project_id`) шире
того, что видит проект (`project_id=<проект>`): 16.10.2026 в общем каталоге
было 86 образов, проекту из них были видны 63 (плюс 20 своих). Невидимые
проекту — все Astra Linux, Альт, РЕД ОС и SberLinux, Windows 11 VDI, Ubuntu
24.04 Desktop, NGFW — API в ВМ не превращает: 422
`[{"message":"Image with this id is not found","code":"wrong_image"}]`.
evo сверяет каждый поиск с каталогом проекта (тот же запрос с
`project_id`), а образ по UUID — списком проекта с `name=<имя образа>`.
Если каталог проекта не прочитан, ничего не помечается, в ответе
`image_list` — `warning`. Почему образ не виден проекту, не проверено:
вероятно, продукт маркетплейса нужно сначала подключить к проекту.

Образ передаётся в `provision` и `deploy` как `image` — имя, отображаемое
имя или UUID (разрешается так же, как в `image_get`: неоднозначно — `USAGE`
с кандидатами в сообщении, не найдено — `NOT_FOUND`), или по-старому как
`image_id` (UUID); оба сразу — `USAGE`.

`provision` дополнительно принимает:

- `image_metadata` — объект `{поле: значение}`: значения полей образа, в
  первую очередь обязательных, которые evo сам не заполнит (`image_get`:
  `fields_required`). Если такое поле не задано, вызов отклоняется (`USAGE`)
  **до** резервирования floating IP и создания ВМ; поле, которого у образа
  нет, — тоже `USAGE` со списком полей. В превью без `confirmed` —
  только имена полей (`will_create.image_metadata_fields`), не значения.
- `ports` — `["80", "8080/tcp", "53/udp"]` (можно числами): после загрузки
  ВМ evo открывает их для `0.0.0.0/0` в её группе безопасности (группа
  общая для всех ВМ в ней), в ответе — `ports{opened, already_open, failed}`;
  `destroy` закрывает созданные правила.

`provision` создаёт ВМ из любого доступного проекту образа, включая Astra
Linux, Альт и РЕД ОС. Образ, недоступный проекту, `provision`, `deploy` и
`deploy_plan` отклоняют с `NOT_FOUND` («… is in the Cloud.ru catalogue but
not available to the project») до резервирования floating IP и создания ВМ;
`provision` и `deploy` проверяют это в вызове с `confirmed=true` (превью без
подтверждения в облако не ходит), `deploy_plan` — при построении плана.
`deploy` на Astra Linux, Альт и РЕД ОС отказывает до создания ВМ и тогда,
когда образ доступен: Docker ставится через get.docker.com, который их не
поддерживает; `deploy_plan` с `compose_file` на таком образе плана не
строит (`USAGE`), без `compose_file` — строит обычную ВМ. `deploy_plan` тоже
принимает `image_metadata` и без обязательных полей не строит план; значения хранятся
в локальном файле плана (`.evo/plans`, права `0600`) до `deploy_apply`.

## Плагины Kubernetes

Плагины (addons) Managed Kubernetes — в группе `k8s` (`--toolsets k8s`):

- `k8s_addons` (`query`, `category`, `billable`) — поиск по каталогу Cloud.ru:
  подстрока имени, отображаемого имени или описания без учёта регистра,
  категория (слова консоли по-русски: «Сети», «Безопасность», «Мониторинг»…;
  все категории — в поле `categories` ответа), платный или нет. Строки
  `{name, display_name, categories, billable, deploy_with}`, где
  `deploy_with` = `{tool: "k8s_add_addon", args: {name}}`, плюс `shown` и
  `total`. Только чтение.
- `k8s_addon_versions` (`name`, `full`) — версии плагина, новые первыми:
  `version` (её принимает `k8s_add_addon`), `addon_version`, `mode`
  (`ACTIVE`, `EXPERIMENTAL`, `END_OF_SUPPORT`, `END_OF_LIFE`, `INACTIVE`),
  `kube_versions` (диапазон версий Kubernetes кластера), `ready_workers`,
  `namespace`, `has_schema`, `schema_bytes`, `latest`. Схема настроек
  (`configuration_schema`, до ~94 КБ) — только с `full=true`. Только чтение.
- `k8s_add_addon` (`cluster_id`, `name`, `version`, `config`, `resolve_mode`)
  — установка, с гейтом `confirmed=true`. Без него инструмент только читает
  и возвращает превью: какая версия выбрана (`version_from`: `latest` —
  новейшая `ACTIVE`, `requested`), namespace (если API дал годный),
  требования версии, версия Kubernetes кластера, узлы его групп, установлен
  ли плагин уже. Что
  определить не удалось, перечислено в `not_checked`; требование, которое
  точно не выполнено, — отказ `USAGE` до подтверждения. `config` — объект
  JSON по схеме версии (строка с объектом JSON тоже принимается; Cloud.ru
  подставляет `{{clusterID}}`, `{{clusterName}}`, `{{productInstanceID}}`,
  `{{nodeCIDR}}`, `{{serviceCIDR}}`; без `config` API берёт последнюю
  конфигурацию плагина в кластере). Ответ —
  `status: "installing"`, `operation_id` и/или `cluster_addon_id`;
  установка асинхронная, `note` называет следующий шаг: `k8s_kubectl`
  `get pods -n <namespace>` (или `-A`, если API не дал годного namespace)
  до `Ready=True` у подов плагина (поды Job, например
  `cert-manager-startupapicheck`, заканчиваются `Completed`). Если поды
  остаются в `ImagePullBackOff`, у кластера, вероятно, нет выхода в
  интернет: по документации Cloud.ru нужен публичный SNAT-шлюз в зоне
  узлов.

Неизвестный плагин или версия — `NOT_FOUND` с подсказкой и похожими
именами в `hint`.

## Витрина

`marketplace_list` (`query`, `kind` = `vm-os` | `vm-app` | `k8s-addon`,
`all`) — в группе `image`: образы общего каталога (ОС и продукты
маркетплейса, без образов проекта) и плагины Managed Kubernetes одним
списком. Только чтение; ВМ создаёт `provision`, плагин — `k8s_add_addon`,
единого инструмента установки нет.

- `query` — подстрока имени или отображаемого имени без учёта регистра (у
  плагинов — и описания). `kind` читает только нужный каталог: `vm-*` не
  ходит в Managed Kubernetes, `k8s-addon` — в каталог образов.
- Строки `{kind, name, display_name, id, billable, available_in_project,
  ready, deploy{cli, tool, args}}`, по виду, затем по отображаемому имени;
  `counts` — показанные строки каждого вида, `total` — совпадения до
  скрытия, `hidden`, `hidden_not_in_project`, `hidden_not_ready`.
- `kind` образа — эвристика: `public` — `vm-os`; образ маркетплейса —
  `vm-os`, если имя или отображаемое имя начинается со слова семейства ОС
  (Ubuntu, Debian, CentOS, Rocky, Alma, Fedora, openSUSE, Astra, Alt/Альт,
  RED OS/РЕД ОС, SberLinux, ROSA/РОСА, Windows), иначе `vm-app`
  (`nginx-ubuntu-22.04` — `vm-app`).
- `billable` — только у плагинов (флаг каталога). У образа его нет: расчёт
  цены API отдаёт 0 за любой образ, лицензия в нём не отражена.
- `available_in_project` — только у образов: `true`/`false`, если каталог
  сверен с тем, каким его видит проект; без проекта или при ошибке такой
  сверки поля нет (и есть `warnings`). Недоступные проекту и не готовые ни в
  одной зоне скрыты без `all=true`; с ним первые идут с
  `available_in_project: false` и без `deploy`, вторые — с `ready: false`.
- `deploy` — `{tool: "provision", args: {image}}` (имя образа, а если имя в
  каталоге повторяется — UUID); у плагина — `{tool: "k8s_add_addon",
  args: {name}}` (плюс `cluster_id`; версии — `k8s_addon_versions`). `cli` —
  та же команда для человека.
- Если один каталог не прочитан, ответ — второй с `warnings[]` и без
  ошибки; ошибка — только когда не прочитан ни один.

## stdio

```sh
evo configure      # один раз: ключ и проект в профиль
```

### Claude Code

```sh
claude mcp add --transport stdio --scope project evo -- evo mcp
```

Сервер, добавленный под старым именем (`… cloudru-vm -- cloudru-vm mcp`), до
v1.1 работает через alias, но лучше заменить: `claude mcp remove cloudru-vm`
и команда выше. Имена инструментов не менялись.

или файл `.mcp.json` в корне проекта:

```json
{
  "mcpServers": {
    "evo": {
      "command": "evo",
      "args": ["mcp"]
    }
  }
}
```

Другой профиль — `"args": ["--profile", "staging", "mcp"]` или
`"env": {"CLOUDRU_PROFILE": "staging"}`. Ключ можно передать и через `env`
(`CLOUDRU_KEY_ID`, `CLOUDRU_SECRET`, `CLOUDRU_PROJECT_ID`), но тогда он лежит в
файле проекта открытым текстом — профиль надёжнее.

### Cursor

`~/.cursor/mcp.json` (для всех проектов) или `.cursor/mcp.json` в проекте:

```json
{
  "mcpServers": {
    "evo": {
      "command": "evo",
      "args": ["mcp"]
    }
  }
}
```

Если клиент не находит бинарь (у GUI-приложений на macOS свой `PATH`), укажите
полный путь: `"command": "/Users/me/.local/bin/evo"`.

Рабочий каталог сервера — тот, в котором клиент его запускает (обычно корень
проекта). Там же `deploy` и другие инструменты ведут `./.evo/` —
состояние и SSH-ключ к созданным ВМ ([auth.md](auth.md#что-хранится-на-машине)).
По stdio stdout занят протоколом; человекочитаемого вывода нет.

## Streamable HTTP

```sh
export MCP_BEARER_TOKEN="$(openssl rand -hex 32)"
evo mcp --http :8080
```

- `POST /mcp` — MCP Streamable HTTP; без верного `Authorization: Bearer …` —
  `401`.
- `GET /healthz` — проверка живости, без аутентификации.
- Без `MCP_BEARER_TOKEN` сервер не стартует (код `2`). Токен короче 16
  символов — предупреждение в лог.
- `--auth none` — без аутентификации, **только** на loopback-адресе
  (`--http 127.0.0.1:8080`); на `:8080` или внешнем адресе сервер откажется
  стартовать. В этом режиме заголовок `Host` тоже должен быть loopback
  (`localhost`, `127.0.0.1`, `[::1]`), иначе `403`: так страница в браузере
  не доберётся до сервера через DNS rebinding. Запросы с чужим `Origin` —
  `403` в любом режиме.
- `POST /mcp` принимает только `Content-Type: application/json` (иначе
  `415`): `text/plain` страница в браузере может отправить без
  предварительного запроса CORS, а `application/json` — нет, и CORS сервер не
  разрешает.
- Тело запроса — до 16 МБ (больше — `413`), и прийти оно должно за 60 с
  (иначе `408`).
- Принимаются батчи JSON-RPC (массив сообщений, по HTTP — не больше 100,
  иначе `413`); на уведомления (без `id`) сервер не отвечает. То же по stdio:
  одна строка — одно сообщение или батч, строка длиннее 16 МБ получает
  ошибку, а сервер продолжает работу.
- Одновременно выполняется не больше 32 вызовов инструментов; следующий
  сразу получает ошибку инструмента «server busy» — повторите позже.
- В stderr — одна строка на вызов инструмента: имя, статус, длительность;
  аргументы не пишутся. `--verbose` — подробнее.

Сервер однопользовательский: все клиенты с токеном работают от имени одного
ключа Cloud.ru, в одном рабочем каталоге и подтверждают операции сами за себя
(`confirmed=true`). Выдавайте токен только тем, кому доверили бы и сам ключ;
наружу публикуйте только за TLS (обратный прокси).

**Проекты.** `deploy` и `provision` принимают аргумент `project_id`. С ключом,
который видит несколько проектов (например, ключ с ролью «Администратор
организации»), любой клиент сервера может создать ВМ в любом из них.
Ограничьте сервер списком проектов:

```sh
evo mcp --http :8080 --allow-project <UUID-1> --allow-project <UUID-2>
```

Вызов в проекте не из списка, включая проект самого сервера, если его в
списке нет, получает ошибку `CONFIG_INVALID`. Исключение — чтения
`org_quotas` и `iam_service_accounts` с аргументом `project_id`: при
разрешённом проекте сервера они читают и другие проекты организации. Без флага сервер работает как раньше,
но при старте в HTTP-режиме пишет предупреждение. Для общего сервера надёжнее
ключ уровня проекта: тогда ограничение даёт сам Cloud.ru. Превью
подтверждения `deploy`, `provision` и `destroy` называет проект
(`project_id`), в котором будет выполнен вызов.

### Claude Code

```sh
claude mcp add --transport http evo http://localhost:8080/mcp \
  --header "Authorization: Bearer $MCP_BEARER_TOKEN"
```

или в `.mcp.json` (Claude Code подставляет `${…}` из окружения):

```json
{
  "mcpServers": {
    "evo": {
      "type": "http",
      "url": "http://localhost:8080/mcp",
      "headers": {"Authorization": "Bearer ${MCP_BEARER_TOKEN}"}
    }
  }
}
```

### Cursor

```json
{
  "mcpServers": {
    "evo": {
      "url": "http://localhost:8080/mcp",
      "headers": {"Authorization": "Bearer <токен>"}
    }
  }
}
```

### Ограничения HTTP-режима

Набор инструментов тот же, но часть вызовов отвечает ошибкой. По HTTP
вызывающий — удалённый клиент (возможно, агент, которому подсунули чужие
инструкции), поэтому сервер не отдаёт ему секреты и не пускает за пределы
облака: к файлам, сети и программам своего хоста.

**Секреты** — пока сервер не запущен с `--allow-secret-export`:

- `k8s_kubeconfig` со встроенным токеном (`embed_token`, по умолчанию),
  `cert_private_key` и `secret_value` отключены: секрет ушёл бы с хоста, где
  лежит ключ Cloud.ru. Вместо `k8s_kubeconfig` используйте `embed_token=false`
  — kubeconfig с exec-плагином `cloudlogin`, без кредов внутри.
- `k8s_kubectl` не выполняет `config …` (`config view --raw` печатает токен),
  `get --raw`, `create token` и `set env … --resolve` (печатает значения из
  Secret). Если команда называет Secret (`secret`, `secrets`, `secret/x`,
  `secrets/x`, список вроде `secret,configmap`), то при любом verb — `get`,
  `label`, `annotate`, `patch`, `set`, `apply`, `create`, `wait`, … —
  разрешён только вывод таблицей (без `-o`, `-o wide`) и `-o name`. Отклоняются
  `-o yaml|json|jsonpath…|go-template…|custom-columns…` в любом написании
  (`-oyaml`, `-o=yaml`, `--output yaml`, `-Aoyaml`), `--template` (без `-o`
  это `go-template`), `--list` (`annotate --list` показывает
  `last-applied-configuration` с данными) и `apply view-last-applied`. Так
  закрыто и `label secret x a=b -o yaml --dry-run=server`: объект не меняется,
  но печатается целиком. `get secrets`, `get secret x -o name`,
  `describe secret` (без значений), `create secret generic … --from-literal`
  работают.
- В ответах остальных инструментов значения полей с паролями, ключами и
  токенами (`password`, `secret_key`, `private_key`, `token`, …) заменяются на
  `[redacted over HTTP: …]`. Так, `deploy_apply` не возвращает
  сгенерированный пароль ВМ (вход — по SSH-ключу сервера), а `pg_get`,
  `redis_get`, `kafka_get` и другие `*_get` — паролей, если облако их пришлёт.

С `--allow-secret-export` всё это отдаётся, как по stdio.

**Хост сервера** — всегда, с флагом и без:

- `deploy`: `compose_file` — только внутри рабочего каталога сервера
  (с учётом символических ссылок).
- `k8s_kubectl`: только встроенные команды kubectl, первым словом
  (`get`, `describe`, `logs`, `apply`, `exec`, `scale`, …). Не выполняются
  плагины `kubectl-*` из `PATH` сервера, `cp`, `port-forward`, `proxy`, `edit`,
  `kustomize`; флаги, читающие или пишущие файлы хоста (`-f`, `-k`,
  `--kubeconfig`, `--from-file`, `--from-env-file`, `-o …-file=…`, `--cert`,
  `--key`, `--ca-cert` у `create secret tls`, `--custom` у `debug`,
  `--log-file`, `--output-directory`, `--kuberc`, …), и флаги, отправляющие токен кластера
  на другой сервер (`--server`, `--token`, `--insecure-skip-tls-verify`, …),
  в том числе через `_` вместо `-` (`--client_key`). Флаги подробного журнала
  `-v`, `--v`, `-v=N`, `-vN`, `--vmodule` тоже отклоняются: с `-v=8` kubectl
  пишет в вывод тела ответов API, включая данные Secret. Манифест
  применяйте через `k8s_apply`. Внутри кластера `k8s_kubectl` по-прежнему
  может всё, что разрешает роль сервисного аккаунта, — например, `exec` в под
  и прочитать его переменные; изменяющие команды требуют `confirmed=true`.
- `k8s_helm`: chart — из репозитория (`repo/name` с `repo_url`) или
  `oci://` / `https://`; локальный путь на сервере не принимается. Адреса
  `repo_url` и chart — только `https://` / `oci://` на публичных IP: не
  loopback, не частные сети, не `169.254.0.0/16` (сервис метаданных) и не
  `100.64.0.0/10`. Адрес проверяется при вызове; helm резолвит имя ещё раз
  сам.
- `k8s_kubectl`, `k8s_apply`, `k8s_helm` — если на сервере нет `kubectl` /
  `helm` (в Docker-образе их нет): «not available in this mode». Запускайте
  `kubectl` у себя или используйте stdio-сервер на машине с `kubectl`.

По stdio сервер — процесс самого пользователя, и этих ограничений нет.

## Docker-образ

Образ `evo` — distroless, без shell, пользователь `nonroot`, слушает
`:8080`, `HEALTHCHECK` встроен (`evo mcp healthcheck`). Релизные образы
публикуются в Artifact Registry Cloud.ru (`<реестр>.cr.cloud.ru/evo:<версия>`;
публичный адрес реестра появится вместе с первым опубликованным образом);
собрать самому:

```sh
docker build -t evo:local .
```

Запуск — креды Cloud.ru и токен передаются переменными окружения (в образе
нет профиля):

```sh
export MCP_BEARER_TOKEN="$(openssl rand -hex 32)"
docker run -d --name evo -p 127.0.0.1:8080:8080 \
  -e MCP_BEARER_TOKEN \
  -e CLOUDRU_KEY_ID -e CLOUDRU_SECRET -e CLOUDRU_PROJECT_ID \
  -e CLOUDRU_REGION \
  evo:local

curl -s localhost:8080/healthz
docker logs -f evo
```

`-e NAME` без значения берёт его из вашего окружения — секрет не попадает в
историю оболочки. Лучше — `--env-file` с правами `0600`. Все переменные — в
[auth.md](auth.md#7-переменные-окружения); для S3 добавьте `CLOUDRU_S3_*`.

Состояние (`.evo/` с SSH-ключом к созданным `deploy` ВМ, кэш токена)
живёт в `/home/nonroot` контейнера и пропадает вместе с ним. Если сервер будет
создавать ВМ, подключите том: `-v evo-data:/home/nonroot` — иначе после
пересоздания контейнера к этим ВМ не будет SSH-доступа. Том от образа
`cloudru-vm` подходит как есть: старый `.cloudru-vm/` в нём читается до v1.1.

Без `MCP_BEARER_TOKEN` контейнер завершается с кодом `2`.
