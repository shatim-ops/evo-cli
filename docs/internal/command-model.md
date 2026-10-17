# Модель «команда ↔ ресурс»

Дата: 16.10.2026. Код: `main` на `7d5346e`.

Документ для того, кто впервые меняет команду или инструмент evo: что каждая
команда создаёт, читает и удаляет в облаке, что она пишет на диск, кто потом
убирает созданное и где три пути создания ВМ ведут себя по-разному. Всё
выведено из кода; ссылки `файл:строка` — на этот коммит. Живых запросов к
Cloud.ru при подготовке не было: где поведение облака выведено только из кода
или комментария, это сказано. «Не проверено» — значит, не удалось подтвердить
даже по коду.

Пути без префикса — от `internal/`.

Содержание:

1. [Источники правды и общие механизмы](#1-источники-правды-и-общие-механизмы)
2. [Команды верхнего уровня CLI](#2-команды-верхнего-уровня-cli)
3. [Деревья команд](#3-деревья-команд)
4. [Инструменты MCP](#4-инструменты-mcp)
5. [Три пути создания ВМ](#5-три-пути-создания-вм)
6. [Кто за кем убирает](#6-кто-за-кем-убирает)
7. [Грамматика и дубли имён](#7-грамматика-и-дубли-имён)
8. [Кандидаты в инварианты для теста](#8-кандидаты-в-инварианты-для-теста)

## 1. Источники правды и общие механизмы

### 1.1. `.evo/state.json`

Файл лежит в текущем каталоге процесса: `StateDir` — это `os.Getwd()`
(`cli/main.go:263`, `:274`). Для CLI это каталог проекта, для MCP-сервера —
каталог, из которого его запустили (фабрика движка та же, `cli/mcp.go:211`).
Если есть только старый `.cloudru-vm/`, используется он (`config/names.go:97`).

Структура (`state/types.go`): `File{version, project, deployments[]}`. Поле
`File.Project` объявлено (`state/types.go:50`), но нигде не пишется.

Кто заполняет поля `DeploymentRecord`:

| поле | `engine.Deploy` | MCP `provision` | MCP `deploy_apply` |
|---|---|---|---|
| `id`, `vm_id` | ID ВМ (`engine/deploy.go:539`) | ID ВМ (`tools/tools.go:1741`) | ID ВМ (`tools/tools_deploy.go:138`) |
| `vm_ip` | адрес boot FIP или FIP после загрузки (`engine/deploy.go:557`, `:656`) | пусто, пока не `running`; адрес boot FIP при `error` (`tools/tools.go:1843`, `:1886`) | адрес FIP (`tools/tools_deploy.go:140`) |
| `floating_ip_id` | boot FIP сразу, FIP после загрузки при `provisioned` (`engine/deploy.go:556`, `:657`) | только boot FIP; FIP, привязанный после загрузки, не записывается (`tools/tools.go:1729-1732`, `:1836`) | ID FIP (`tools/tools_deploy.go:141`) |
| `project_id` | проект конфига (`engine/deploy.go:542`) | проект конфига или аргумента `project_id` | проект плана |
| `vm_flavor` | имя или ID: при `auto` — ID из подбора (`engine/deploy.go:333`) | UUID flavor (`tools/tools.go:1745`) | имя flavor (`tools/tools_deploy.go:143`) |
| `region` | UUID зоны | UUID зоны | ID зоны плана |
| `status` | см. 1.2 | `creating` / `running` / `error` | `creating` / статус результата |
| `compose_hash`, `exposed_ports` | да (`engine/deploy.go:547-548`, `:162-163`) | нет | нет |
| `ssh_user` | из метаданных образа, по умолчанию `ubuntu` (`engine/deploy.go:402-406`) | по умолчанию `user1` (`tools/tools.go:1583`, `:1610`) | логин плана, по умолчанию `user1` (`deployplan/resolve.go:308`) |
| `ssh_host_key_fingerprint` | при первом SSH (`engine/deploy.go:216-217`) | нет | нет (пишется `.evo/known_hosts`, см. 5) |
| `ssh_key_fingerprint` | нигде не пишется | — | — |
| `security_group_id` | сразу при создании записи (`engine/deploy.go:550`) | нет | из `sgrules.Open` (`tools/tools_deploy.go:147`) |
| `security_group_ports`, `opened_rules` | `sgrules.Open` (`sgrules/sgrules.go:112`, `:139`) | нет | `sgrules.Open`, сливаются со старой записью (`tools/tools_deploy.go:155-162`) |
| `created_at` | при создании | перезаписывается при каждом сохранении (`tools/tools.go:1750`) | сохраняется от старой записи (`tools/tools_deploy.go:156`) |

Ещё два писателя: `destroy` по `vm_id` (`tools/tools.go:2091-2121`) и
`engine.Destroy` (`engine/destroy.go`). Других мест, где пишется state, нет:
вызовы `state.Read`/`Write`/`Update` в `tools/` есть только в `tools.go` и
`tools_deploy.go`.

«Активная» запись — первая со статусом не `destroyed`
(`state/state.go:154-162`). Её берут `deploy`, `destroy`, `status`, `logs`,
`verify`, MCP `status` и `deploy`. Записи от `provision`, `deploy_apply` и
`evo vm create` лежат в том же файле и тоже становятся активными (см. 5).

**Блокировка.** `state.Lock` — файловая блокировка `.evo/state.lock`
(flock), снимается вместе с процессом (`state/lock.go:16-47`). Без ожидания:
занято — ошибка `STATE_LOCKED`. `engine.Deploy` и `engine.Destroy` держат её
всю работу, кроме `--dry-run` (`engine/deploy.go:55-61`,
`engine/destroy.go:28-34`). MCP `provision`, `deploy_apply` и `destroy` по
`vm_id` пишут через `state.Update` — ожидание до 2 минут
(`tools/tools_deploy.go:170`), контекст вызова при этом не учитывается
(`state/lock.go:56-61`). Запись — через временный файл и rename
(`state/state.go:76-142`).

### 1.2. Статусы записи и переходы

Список в комментарии (`state/types.go:17`): `creating, provisioned, deploying,
running, ssh_timeout, docker_failed, destroying, destroyed`. В коде пишутся ещё
`error`, `timeout` и любой статус ВМ из облака (усыновление в `deploy_apply`).

| из | чем | в | где |
|---|---|---|---|
| — | `deploy`: ВМ создана (или найдена после потерянного ответа) | `creating` | `engine/deploy.go:538-560` |
| `creating` | `deploy`: ВМ `active`/`running` | `provisioned` | `engine/deploy.go:656-661` |
| `creating` | `deploy`: ВМ `error`/`error_*` или таймаут ожидания | `creating` (не меняется) | `engine/deploy.go:625-629`, `:678-682` |
| `creating` | `deploy`: Ctrl-C, ВМ удалена, FIP освобождён | запись удалена | `engine/deploy.go:612-616` |
| `creating` | `deploy`: Ctrl-C, ВМ удалена, но FIP не освобождён или удаление не подтверждено | `destroying` | `engine/deploy.go:598-610` |
| `creating` | `deploy`: Ctrl-C без терминала или ответ «нет» | `creating` | `engine/deploy.go:618`, `:1206-1213` |
| любой активный, кроме `destroying` | `deploy`: SSH не поднялся | `ssh_timeout` | `engine/deploy.go:203-208` |
| то же | `deploy`: SSH подключён | `deploying` | `engine/deploy.go:219-222` |
| `deploying` | `deploy`: Docker не готов (не отмена) | `docker_failed` | `engine/deploy.go:225-231` |
| `deploying` | `deploy`: ошибка загрузки файла или `compose up` | `deploying` (не меняется) | `engine/deploy.go:248-263` |
| `deploying` | `deploy`: `compose up` прошёл | `running` | `engine/deploy.go:267-272` |
| `running` с тем же хешем | `deploy` без `--force` | без изменений | `engine/deploy.go:91-96` |
| `destroying` | `deploy` | отказ | `engine/deploy.go:74-78` |
| любой активный | `destroy` | `destroying` | `engine/destroy.go:81-85` |
| `destroying` | `destroy`: DeleteVM — ошибка не 404 | прежний статус | `engine/destroy.go:95-99` |
| `destroying` | `destroy`: FIP не освобождён | `destroying` | `engine/destroy.go:155-157` |
| `destroying` | `destroy`: всё удалено | запись удалена; `destroyed`, если правило SG не удалилось | `engine/destroy.go:162-164`, `sgrules/sgrules.go:348-358` |
| `destroyed` с правилами | `destroy` | запись удалена (или остаётся) | `engine/destroy.go:44-46`, `:208-229` |
| — | `provision`: ВМ создана / найдена после потерянного ответа | `creating` | `tools/tools.go:1784`, `:1770` |
| `creating` | `provision`: ВМ активна | `running` (Docker и compose не ставились) | `tools/tools.go:1843` |
| `creating` | `provision`: ВМ `error`/`error_*` | `error` | `tools/tools.go:1886` |
| `creating` | `provision`: таймаут или отмена | `creating` | `tools/tools.go:1790`, `:1817` |
| — / любой | `deploy_apply`: успех | `running` | `deployplan/apply.go:242-251`, `tools/tools_deploy.go:210` |
| — / любой | `deploy_apply`: таймаут | `timeout` | `deployplan/apply.go:189-194` |
| — / любой | `deploy_apply`: ВМ в ошибке (новая или найденная по имени) | `error` | `deployplan/apply.go:66-72`, `:205-210` |
| — / любой | `deploy_apply`: усыновлена ВМ с тем же именем | статус ВМ в облаке (`running`, если `active`) | `deployplan/apply.go:78-81` |
| — / любой | `deploy_apply`: отмена или потерянный ответ на создание | `creating` | `tools/tools_deploy.go:198-201` |
| любой | `destroy vm_id`: FIP не освобождён | `destroying` | `tools/tools.go:2101-2115` |
| любой | `destroy vm_id`: удалено | запись удалена или `destroyed` | `tools/tools.go:2117-2119` |

Кто читает статус: `ActiveDeployment` (всё, кроме `destroyed`), `deploy`
(`destroying` и `running`), `sgrules.neededBy` (пропускает `destroyed` и
`destroying`, `sgrules/sgrules.go:226`), `sgrules.Pending` (`destroyed` с
правилами, `:335-343`). Статусы `error` и `timeout` ни одна команда отдельно не
обрабатывает: для `deploy` это просто активная запись.

### 1.3. Задания стека

Файлы `~/.evo/stack-jobs/<job_id>.json` — в домашнем каталоге, а не в
проекте (`stack/store.go:205-208`); id `stk-<10 hex>` (`:210-214`, `:331`).
Права 0700/0600, секреты и тела объектов S3 выкидываются после выполнения шага
(`stack/store.go:263-327`). Блокировка: мьютекс на job в процессе плюс
файловая `<job_id>.lock` (`tools/tools_stack.go:517-538`, `stack/store.go:337`).

Статусы job: `planned` (`stack/store.go:370`), `running`, `done`, `error`
(`tools/tools_stack.go:309-323`), `destroyed`, `destroy_failed`
(`tools/tools_stack.go:756-759`). `destroying` упомянут в комментарии
(`stack/store.go:193`), но не ставится. Статусы шага: `pending`, `waiting`,
`done`, `error`; `running` в комментарии (`stack/store.go:178`) не ставится.

Двигает job только `stack_apply` и `stack_status` для job в `running`
(`tools/tools_stack.go:567-587`). Шаг ВМ стека — это вызов `provision`
(`stack/store.go:556-567`), поэтому ВМ стека попадает и в job, и в
`state.json` того каталога, где запущен MCP-сервер.

### 1.4. Профиль и `.evo.yaml`

Порядок слоёв (`config/config.go:155-224`), каждый следующий важнее:

1. умолчания (`flavor: auto`, `disk_size: 10`, `timeout: 600`,
   `docker-compose.yml`; `config/config.go:99-106`);
2. профиль: `config.yaml` и `credentials.yaml` в `config.Dir()` (или
   `$CLOUDRU_CONFIG_DIR`); имя — `--profile`, `$CLOUDRU_PROFILE`, `default`
   (`config/config.go:126-134`, `config/profile.go:22-32`, `:61-66`);
3. `./.evo.yaml` (или старый `.cloudru-vm.yaml`) в текущем каталоге; ключи и
   секреты оттуда не читаются (`config/config.go:348-398`);
4. переменные `CLOUDRU_*` (`config/config.go:137-153`, `:183-197`);
5. флаги: глобальные `--project-id`, `--region` (`cli/tools.go:725-741`) и
   локальные флаги `deploy` с теми же именами (`cli/main.go:533-534`).

Если `.evo.yaml` или `CLOUDRU_PROJECT_ID` заменяют проект профиля без флага,
печатается предупреждение (`config/config.go:202-213`).

Для существующей записи проект берётся из записи: `destroy` работает в
`project_id` записи, а при её отсутствии — в проекте конфига
(`engine/destroy.go:306-313`); `deploy` отказывает, если проект конфига не
совпадает с проектом записи (`engine/deploy.go:81-87`). Исключение — `destroy`
по `vm_id` (см. 6).

### 1.5. Имена, по которым evo находит «своё»

| ресурс | имя | кто создаёт | кто ищет по имени |
|---|---|---|---|
| ВМ (deploy) | `evo-<имя проекта или каталога>` (`engine/deploy.go:356-360`, `:939-962`) | `engine.Deploy` | `FindCreatedVM` после ошибки создания (`engine/deploy.go:525`) |
| ВМ (provision) | аргумент `name` или `evo-<unix-время>` (`tools/tools.go:1476-1479`) | `provision` | `FindCreatedVM` (`tools/tools.go:1769`) |
| ВМ (deploy_apply) | `name` плана или `<product\|vm>-ГГГГММДД-ЧЧММ` (`deployplan/resolve.go:141-145`) | `deploy_apply` | `findVMByName` до создания (усыновление) и `FindCreatedVM` после ошибки (`deployplan/apply.go:59`, `:154`) |
| загрузочный диск | `<vm>-boot` (deploy, provision), `<vm>-disk` (deploy_apply) | вместе с ВМ | никто |
| boot FIP | `fip-<vm>-<unix-время в base36>` (`engine/deploy.go:852`) | `deploy`, `provision` | `createBootFIP` после ошибки (`engine/deploy.go:862-868`); `destroy vm_id` по префиксу `fip-<vm>-` (`tools/tools.go:2040-2051`) |
| FIP после загрузки | `fip-<vm_id>` (`engine/deploy.go:966`) | `deploy`, `provision` | `assignFloatingIP` после ошибки (`engine/deploy.go:810`); `evo destroy` без `floating_ip_id` (`engine/destroy.go:256-271`) |
| FIP deploy_apply | `<vm>-fip` (`deployplan/apply.go:311`) | `deploy_apply` | только `ensureFloatingIP` после ошибки (`deployplan/apply.go:326-332`) |
| FIP при `fip_attach` | прежнее имя FIP или `fip-<имя ВМ>` (`tools/tools_image.go:213-216`) | переименовывает чужой FIP | никто |
| правило SG (deploy) | описание `evo: port <порт>` (`engine/network.go:107`) | `deploy` | только после ответа без ID (`sgrules/sgrules.go:146-178`) |
| правило SG (deploy_apply) | `auto: <product> port <порт>` (`deployplan/apply.go:240`) | `deploy_apply` | так же |
| правило SG (open_ports) | `opened via open_ports` (`tools/tools_deploy.go:434`) | `open_ports` | никто |
| ресурсы стека | имена из манифеста (`FindTool`/`FindName`, `stack/store.go`) | `stack_apply` | `findIDByName` при сбое создания, после отмены и в `stack_destroy` (`tools/tools_stack.go:229-247`, `:268-274`, `:676-690`) |

Удаляет `destroy` правила SG только по ID из state, не по описанию
(`sgrules/sgrules.go:296-331`).

## 2. Команды верхнего уровня CLI

Список — `newRootCmd` (`cli/main.go:226-243`). Команды `list` в CLI нет:
`list` — только инструмент MCP. Гейт: «да» — `confirm.Ask`; без терминала и
без `--yes` выход с кодом 3 (`confirm/confirm.go:134-150`).

| команда | облако: создаёт / читает / удаляет | state | гейт | проект | код |
|---|---|---|---|---|---|
| `configure` (+ `list`, `delete`) | читает: токен IAM, список проектов | нет; пишет профиль | нет | вводится | cli (`cli/configure.go:79`, `:241`, `:259`) |
| `whoami` | читает: токен IAM | нет | нет | конфиг | cli (`cli/whoami.go:14`) |
| `doctor` | читает: доступность IAM/Compute, токен, видимость проектов | нет | нет | конфиг | cli (`cli/doctor.go:46-129`) |
| `project list` | читает: проекты организации | нет | нет | конфиг | cli (`cli/project.go:32`) |
| `project use` | читает: проекты (для проверки) | нет; пишет профиль | нет | аргумент | cli (`cli/project.go:90-150`) |
| `init` | читает: зоны, образы | нет; пишет `.evo.yaml`, если его нет | нет | конфиг | engine (`engine/init.go:30-95`) |
| `deploy` (новая ВМ) | создаёт: ВМ, boot FIP (или FIP после загрузки), правила SG; читает: образ, flavor, типы дисков, подсети, SG, зоны | пишет | да | конфиг | engine (`engine/deploy.go:44`) |
| `deploy` (есть запись) | создаёт: недостающие правила SG, FIP, если в записи нет адреса; SSH, compose | пишет | нет (`engine/deploy.go:97-102`) | запись; отказ при несовпадении | engine |
| `deploy --dry-run` | только чтения (новая ВМ); для существующей — ничего | нет, блокировку не берёт | — | как выше | engine (`engine/deploy.go:55`, `:97-102`, `:492`) |
| `verify` | HTTP/TCP-проверка портов из записи | читает | нет | — | engine (`engine/verify.go:17`) |
| `logs` | SSH на адрес из записи | читает; может создать SSH-ключ (`engine/logs.go:44`) | нет | — | engine |
| `status` | ничего | читает | нет | — | engine (`engine/status.go:12`) |
| `destroy` | удаляет: ВМ записи, её FIP, правила SG записи | пишет | да (`--yes` или `--force`) | запись, иначе конфиг | engine (`engine/destroy.go:26`) |
| `list-zones` | читает: зоны | нет | нет | — | cli (`cli/main.go:718-762`) |
| `list-images` | читает: общий каталог образов | нет | нет | — | engine (`engine/list_images.go:10-17`) |
| `tools list` / `describe` | ничего | нет | нет | — | cli (`cli/tools.go:88`, `:153`) |
| `tools call` | как вызываемый инструмент | как инструмент | да, если `mutating` | как инструмент | tools через `invokeTool` (`cli/tools.go:587-616`) |
| `mcp` (и скрытый `mcp healthcheck`) | как вызываемые инструменты | как инструменты | `confirmed` в аргументах | конфиг сервера; `--allow-project` ограничивает (`cli/mcp.go:203-240`) | tools |
| деревья (`vm`, `disk`, …) | см. 3 | см. 3 | да, если `mutating` | конфиг, `--project-id` | tools |
| `version`, `completion`, `help` | ничего | нет | нет | — | cli, cobra |

Гейт деревьев и `tools call`: если инструмент `mutating` и не берёт
`confirmed`, превью строит сам CLI; если берёт — CLI вызывает обработчик, на
ответ `confirmation_required` спрашивает и вызывает ещё раз с
`confirmed=true` (`cli/tools.go:587-616`). Сейчас все `mutating` берут
`confirmed` (так определён `Mutating`, `tools/registry.go:210-218`), первая
ветка не срабатывает.

## 3. Деревья команд

Источник — `treeGroups` (`cli/trees.go:48-130`). Пишут state только те, что
вызывают `provision` или `destroy`.

| команда | инструмент | mutating | state |
|---|---|---|---|
| `vm list` | `vm_list` | нет | нет |
| `vm get` | `vm_get` | нет | нет |
| `vm create` | `provision` | да | пишет (запись в cwd) |
| `vm delete` | `destroy` (по `vm_id`/`name`) | да | пишет (удаляет запись этой ВМ) |
| `vm zones` | `list_zones` | нет | нет |
| `vm flavors` | `list_flavors` | нет | нет |
| `marketplace list` | `marketplace_list` (каталог образов и каталог плагинов mk8s; образ ставит `vm create --image`, плагин — `k8s add-addon`) | нет | нет |
| `disk list`, `disk types` | `disk_list`, `list_disk_types` | нет | нет |
| `disk create`, `attach`, `detach`, `resize`, `delete` | `disk_create`, `disk_attach`, `disk_detach`, `disk_resize`, `disk_delete` | да | нет |
| `vpc list`, `get`, `operation`, `routes`, `subnets`, `security-groups`, `fips` | `vpc_list`, `vpc_get`, `vpc_operation`, `vpc_routes`, `list_subnets`, `list_security_groups`, `list_floating_ips` | нет | нет |
| `vpc create`, `delete`, `route-create`, `route-delete` | `vpc_create`, `vpc_delete`, `vpc_route_create`, `vpc_route_delete` | да | нет |
| `vpc fip-attach`, `fip-release` | `fip_attach`, `release_floating_ip` | да | нет |
| `k8s list`, `get`, `options`, `plan`, `kubeconfig`, `nodepools`, `addons`, `addon-versions` | `k8s_list`, `k8s_get`, `k8s_options`, `k8s_plan`, `k8s_kubeconfig`, `k8s_nodepools`, `k8s_addons`, `k8s_addon_versions` | нет | нет |
| `k8s create`, `delete`, `add-nodepool`, `add-addon`, `kubectl`, `apply`, `helm` | `k8s_create`, `k8s_delete`, `k8s_add_nodepool`, `k8s_add_addon`, `k8s_kubectl`, `k8s_apply`, `k8s_helm` | да | нет |
| `pg list`, `get`, `versions`, `specs`, `operation`, `databases`, `users`, `backups` | `pg_list`, `pg_get`, `pg_versions`, `pg_specs`, `pg_operation`, `pg_databases`, `pg_users`, `pg_backups` | нет | нет |
| `pg create`, `delete`, `db-create`, `db-delete`, `user-create`, `user-delete`, `backup-create` | `pg_create`, `pg_delete`, `pg_db_create`, `pg_db_delete`, `pg_user_create`, `pg_user_delete`, `pg_backup_create` | да | нет |
| `s3 buckets`, `ls`, `get` | `s3_buckets`, `s3_ls`, `s3_get` | нет | нет |
| `s3 put`, `rm`, `bucket-create`, `bucket-delete` | `s3_put`, `s3_rm`, `s3_bucket_create`, `s3_bucket_delete` | да | нет |
| `secret list`, `get`, `value` | `secret_list`, `secret_get`, `secret_value` | нет | нет |
| `secret create`, `add-version`, `delete` | `secret_create`, `secret_add_version`, `secret_delete` | да | нет |

Всего 72 команды дерева. Из 152 инструментов 80 доступны в CLI только через
`evo tools call`: `deploy_plan`, `deploy_apply`, `open_ports`, `image_*`,
`list_images`, `snat_*`, `kafka_*`, `redis_*`, `lb_*`, `tg_*`, `dns_*`, `ar_*`,
`ca_*`, `cert_*`, `billing_*`, `metrics_*`, `audit_events`,
`iam_service_accounts`, `org_*`, `stack_*`, а также `deploy`, `status`, `list`,
`logs` (у четырёх последних есть свои команды верхнего уровня, кроме `list`).

## 4. Инструменты MCP

152 инструмента, 65 изменяющих (`evo tools list --json`; каталог — `docs/tools.md`). Группа
назначается по имени (`tools/registry.go:133-201`), `mutating` — по наличию
`confirmed` в схеме, кроме `cert_private_key` (`tools/registry.go:203-218`).
Обработчики — `tools/tools.go:1214-1362`.

### 4.1. Изменяющие

«Своё по имени» — ищет ли сам инструмент созданный ресурс после потерянного
ответа. «Убирает» — кто удаляет созданное. Ни один инструмент вне строк
deploy/vm/stack не пишет ни state, ни job.

| группа | инструмент | создаёт / удаляет | state / job | своё по имени | убирает |
|---|---|---|---|---|---|
| deploy | `deploy` | ВМ, FIP, правила SG (как `evo deploy`) | state | ВМ, boot FIP, FIP после загрузки | `destroy` |
| deploy | `deploy_apply` | ВМ, FIP, правила SG (tcp) | state после ответа Apply; удаляет план | ВМ (до и после создания), FIP `<vm>-fip` | `destroy` |
| deploy | `destroy` | удаляет ВМ записи или ВМ по `vm_id`/`name`, её FIP, правила SG из state | state | FIP по префиксу `fip-<vm>-` | — |
| deploy | `open_ports` | правила SG (`opened via open_ports`) | нет | нет | никто: удалить правило инструментом нельзя |
| vm | `provision` | ВМ, boot FIP (или FIP после загрузки) | state | ВМ, boot FIP | `destroy` |
| image | `image_create` / `image_delete` | образ из диска / удаляет образ | нет | нет | `image_delete` |
| vpc | `fip_attach` | привязывает существующий FIP, может задать ему имя | нет | нет | отвязки нет; `release_floating_ip` отказывает для `in_use` (`tools/tools_deploy.go:527-533`) |
| vpc | `release_floating_ip` | удаляет свободный FIP | нет | — | — |
| vpc | `vpc_create`/`vpc_delete`, `vpc_route_create`/`vpc_route_delete` | VPC, маршрут | нет (VPC — job стека) | только в стеке | `*_delete`, `stack_destroy` |
| snat | `snat_create`/`snat_delete` | шлюз SNAT | нет (job стека) | нет | `snat_delete`, `stack_destroy` |
| disk | `disk_create` (с `attach_to`), `disk_attach`, `disk_detach`, `disk_resize`, `disk_delete` | диск, привязка | нет | нет | `disk_delete`; `destroy` диски, кроме загрузочного, не трогает |
| k8s | `k8s_create`/`k8s_delete`, `k8s_add_nodepool` | кластер, группа узлов | нет (кластер — job стека) | только в стеке | `k8s_delete`, `stack_destroy`; удаления группы узлов нет |
| k8s | `k8s_add_addon` | плагин кластера (`POST /v3/clusterAddons`; вживую — cert-manager, 16.10.2026) | нет | нет | удаления в evo нет (в спецификации — `DELETE /v3/clusterAddons/{id}`): в консоли; кластер с плагином удаляется вживую (k8s-smoke 16.10.2026) — плагин уходит с ним |
| k8s | `k8s_kubectl`, `k8s_apply`, `k8s_helm` | объекты внутри кластера | нет | — | пользователь (kubectl/helm) |
| pg | `pg_create`/`pg_delete` | кластер PostgreSQL | нет (job стека) | только в стеке | `pg_delete`, `stack_destroy` |
| pg | `pg_db_*`, `pg_user_*`, `pg_backup_create` | база, пользователь, резервная копия | нет | нет | `pg_db_delete`, `pg_user_delete`; удаления копии нет |
| kafka, redis | `*_create`/`*_delete` | кластер | нет (job стека) | только в стеке | `*_delete`, `stack_destroy` |
| nlb | `tg_create`/`tg_delete`, `lb_create`/`lb_delete` | целевая группа, балансировщик (с внешним адресом при `external`) | нет (job стека) | только в стеке | `*_delete`, `stack_destroy`; судьба внешнего адреса при `lb_delete` не проверена |
| dns | `dns_zone_*`, `dns_record_*` | зона, запись | нет (запись — job стека) | только в стеке | `*_delete`, `stack_destroy` (только запись) |
| secret | `secret_create`, `secret_add_version`, `secret_delete` | секрет, версия | нет (job стека) | нет | `secret_delete`, `stack_destroy` |
| ar | `ar_create`/`ar_delete` | реестр | нет (job стека) | только в стеке | `ar_delete`, `stack_destroy` |
| ca | `ca_deploy`, `ca_start`, `ca_stop`, `ca_delete` | Container App | нет | нет | `ca_delete` |
| cert | `cert_issue_le`, `cert_import`, `cert_renew`, `cert_delete` | сертификат | нет | нет | `cert_delete` |
| s3 | `s3_bucket_create`/`s3_bucket_delete`, `s3_put`/`s3_rm` | бакет, объект | нет (объект — job стека) | нет | `s3_bucket_delete`, `s3_rm`, `stack_destroy` (только объекты) |
| stack | `stack_apply` / `stack_destroy` | всё из манифеста через инструменты выше / удаляет в обратном порядке | job (+ state для ВМ через `provision`) | `findIDByName` для всех шагов, кроме ВМ, секретов, S3, SNAT | `stack_destroy` |

### 4.2. Только чтение (сводно)

- deploy: `status`, `list` (state), `logs` (SSH), `deploy_plan` — облако только
  читает, но пишет на диск план `.evo/plans/<id>.json` и SSH-ключ, если нет
  `ssh_key` (`tools/tools_deploy.go:85-98`);
- vm, image, vpc, disk: списки и `*_get`, `list_*`;
- k8s: `k8s_list/get/options/plan/kubeconfig/nodepools`;
- pg, kafka, redis, nlb, dns, ar, ca, cert, secret: `*_list`, `*_get`,
  `*_versions`, `*_specs`, `*_operation` (опрос операции), `secret_value`,
  `cert_private_key` (раскрывает ключ, поэтому под `confirmed`, но не
  `mutating`), `ca_logs`;
- billing, monitoring, audit, iam, org, s3 (`s3_buckets/ls/get`): чтения;
- stack: `stack_plan` пишет job в `planned`; `stack_status` двигает job,
  только если он уже `running` (`tools/tools_stack.go:567-587`).

## 5. Три пути создания ВМ

`engine.Deploy` — это `evo deploy` и MCP `deploy` (`tools/tools.go:1367-1422`,
с `NonInteractive: true, Yes: true`). `provision` — MCP `provision`,
`evo vm create` и шаг `vm` стека. `deploy_plan`/`deploy_apply` — только MCP
или `evo tools call`.

| аспект | `engine.Deploy` | `provision` | `deploy_plan` → `deploy_apply` |
|---|---|---|---|
| подтверждение | превью с ценой, `--yes`/вопрос; обновление существующей ВМ без гейта (`engine/deploy.go:97-102`, `:492`); MCP `deploy` требует `confirmed` всегда (`tools/tools.go:1368-1375`) | `confirmed` (`tools/tools.go:1425-1435`) | `confirmed` на `deploy_apply` (`tools/tools_deploy.go:178-184`) |
| имя ВМ | `evo-<проект>` | `name` или `evo-<unix>` | `name` или `<product>-<дата-время>` |
| flavor по умолчанию | подбор по compose (`sizer`, `engine/deploy.go:299-334`) | самый маленький lowcost, не free (`tools/tools.go:1488-1523`) | по cpu/ram или имени (`deployplan/resolve.go`) |
| тип диска | ищется `ssd` в `ListDiskTypes`, нет — ошибка (`engine/deploy.go:450`, `:1139-1154`) | всегда `SSD` (`tools/tools.go:1696`) | `ListDiskTypes`, по умолчанию `SSD` (`deployplan/resolve.go:94-105`) |
| подсеть и SG | по ID зоны, по имени с зоной, единственная; иначе ошибка (`engine/network.go:31-90`) | первая, затем по имени с зоной; ошибка списка SG — без SG (`tools/tools.go:1641-1665`) | подсеть по зоне, иначе ошибка; SG по зоне, иначе «default», иначе без SG с предупреждением (`deployplan/resolve.go:108-125`) |
| boot FIP: создание | до ВМ, имя `fip-<vm>-<ts>`; при квоте — отказ до создания ВМ (`engine/deploy.go:511-517`, `:851-886`) | тот же код, кроме `no_public_ip` (`tools/tools.go:1719-1728`) | нет: FIP создаётся после `running` (`deployplan/apply.go:216`, `:282-334`) |
| FIP: поиск при потерянном ответе | boot FIP по точному имени; FIP после загрузки — `fip-<vm_id>` (`engine/deploy.go:810`) | boot FIP — да; FIP после загрузки — да, но его ID не возвращается (`engine/deploy.go:703-706`) | `<vm>-fip` по имени (`deployplan/apply.go:324-332`); FIP на интерфейсе усыновляется (`:286-293`) |
| FIP: освобождение при ошибке | boot FIP — если ВМ не создана (`engine/deploy.go:527`); при Ctrl-C с удалением ВМ — `releaseFloatingIP` (`:597`) | boot FIP — если ВМ не создана (`tools/tools.go:1778`) | не нужно: до ВМ FIP нет |
| FIP: квота | ошибка до создания ВМ | то же | ВМ остаётся без адреса, примечание (`deployplan/apply.go:321-323`) |
| запись в state: когда | сразу после CreateVM, под блокировкой на весь deploy (`engine/deploy.go:559-568`) | сразу после CreateVM (`tools/tools.go:1784`, ошибка записи игнорируется), затем при каждом исходе | только после возврата Apply — после ожидания ВМ (до 10 мин) и SSH (до 120 с) (`tools/tools_deploy.go:194-215`) |
| запись в state: поля | полная запись (см. 1.1) | нет `security_group_*`, `compose_hash`, `exposed_ports`, `ssh_host_key_fingerprint`; `floating_ip_id` только boot FIP | нет `compose_hash`, `exposed_ports`, `ssh_host_key_fingerprint` |
| ожидание active | `CLOUDRU_TIMEOUT`/`timeout` (по умолчанию 600 с), опрос 5 с (`cli/main.go:277`, `engine/timings.go:7-8`) | то же значение, опрос 5 с (`tools/tools.go:1801-1810`) | то же значение (`tools/tools_deploy.go:194`), опрос 10 с (`deployplan/apply.go:23`) |
| таймаут ожидания | ошибка, запись `creating` | ответ `timeout`, запись `creating` | ответ `timeout`, запись `timeout`, план удаляется |
| `error`/`error_*` | ошибка, запись `creating`, ВМ и FIP остаются (`engine/deploy.go:678-682`) | запись `error` с FIP, ответ называет `destroy` (`tools/tools.go:1879-1897`) | запись `error`, план удаляется (`deployplan/apply.go:205-210`, `tools/tools_deploy.go:210-216`) |
| усыновление | любой активной записи state, без проверки ВМ в облаке и её происхождения (`engine/deploy.go:69`, `:159-181`) | нет; повторный вызов создаёт ещё одну ВМ | ВМ с тем же именем в проекте, если не в другой зоне (`deployplan/apply.go:58-92`) |
| порты / правила SG | tcp и udp из compose, `evo: port N`, ID в state; повтор открывает недостающие (`engine/deploy.go:172-179`, `:673`) | не открывает | только tcp из плана, `auto: <product> port N`, ID в state (`deployplan/apply.go:238-240`); для усыновлённой ВМ не открывает ничего, правила старой записи сохраняются (`deployplan/apply.go:82-91`, `tools/tools_deploy.go:153-162`) |
| SSH-ключ | `.evo/id_ed25519`, создаётся после согласия (`engine/deploy.go:499`) | создаётся после `confirmed` (`tools/tools.go:1552`) | создаётся уже на `deploy_plan` (`tools/tools_deploy.go:85-90`) |
| known_hosts | отпечаток в `ssh_host_key_fingerprint` при первом SSH (`engine/deploy.go:216-217`) | ничего | `.evo/known_hosts` для адреса ВМ (`tools/tools_deploy.go:226-248`) |
| Docker/compose | ставит Docker по SSH, `compose up` (`engine/deploy.go:224-264`) | нет | нет (только ждёт порт 22, `deployplan/apply.go:227-234`) |
| Ctrl-C / отмена | на терминале спрашивает, удалить ли ВМ; без терминала оставляет (`engine/deploy.go:587-619`, `:1205-1241`); после `active` — выход 130 с подсказкой (`:1171-1191`) | ВМ остаётся, запись `creating`, ответ с `vm_id` (`tools/tools.go:1789-1797`, `:1899`) | ВМ остаётся, запись `creating`, план сохраняется для повтора (`deployplan/apply.go:165-188`, `tools/tools_deploy.go:196-203`) |
| проект | конфиг; для записи — проект записи | конфиг или `project_id` | проект на момент `deploy_plan` |
| блокировка state | `state.Lock` без ожидания на весь deploy | `state.Update`, ожидание до 2 мин | `state.Update`, ожидание до 2 мин |

Расхождения (главное в этом разделе):

1. **Запись в state у `deploy_apply` поздняя.** `deploy` и `provision` пишут
   запись сразу после CreateVM; `deploy_apply` — только когда Apply вернулся,
   то есть после ожидания ВМ и порта SSH (`tools/tools_deploy.go:194-215`).
   Процесс, убитый в это время, оставляет ВМ без записи. Повтор с тем же
   `plan_id` её усыновит, но план живёт 30 минут (`deployplan/store.go:27`).
2. **Boot FIP есть только у `deploy` и `provision`.** `deploy_apply` создаёт
   FIP после `running` (`deployplan/apply.go:216`); комментарий в
   `engine/deploy.go:506-510` объясняет, почему без адреса при загрузке первая
   загрузка затягивается.
3. **Три схемы имени FIP** (`fip-<vm>-<ts>`, `fip-<vm_id>`, `<vm>-fip`), и
   поиск по имени у каждой команды свой (1.5). `evo destroy` без
   `floating_ip_id` ищет по адресу и по `fip-<vm_id>`
   (`engine/destroy.go:256-271`), то есть не находит ни boot FIP, ни FIP
   `deploy_apply` по имени.
4. **`provision` теряет ID FIP, привязанного после загрузки.**
   `engine.AssignFloatingIP` отбрасывает ID (`engine/deploy.go:703-706`),
   `provision` пишет только адрес (`tools/tools.go:1835-1838`); `destroy`
   найдёт FIP по адресу, если адрес в записи есть.
5. **Ошибка ожидания boot FIP в `provision` игнорируется**
   (`tools/tools.go:1833`), в `deploy` — предупреждение (`engine/deploy.go:665-668`).
6. **Статус ВМ в ошибке пишется по-разному:** `creating` у `deploy`, `error`
   у `provision` и `deploy_apply`; при таймауте — `creating`, `creating`,
   `timeout`. `error` и `timeout` нет в списке статусов (`state/types.go:17`).
7. **`evo deploy` усыновляет чужие записи.** Активная запись от `provision`,
   `evo vm create`, `deploy_apply` или шага стека в том же каталоге берётся
   как развёртывание: `compose_hash` пустой, поэтому deploy идёт по ветке
   повторного запуска и ставит Docker на эту ВМ (`engine/deploy.go:69`,
   `:159-181`). Записи от `provision` без `security_group_id`, поэтому порты
   не открываются (`engine/deploy.go:175`). Вживую не проверено.
8. **Усыновление по имени есть только у `deploy_apply`**, и оно берёт любую
   ВМ проекта с этим именем, не только созданную evo (проверяется лишь зона,
   `deployplan/apply.go:73-77`). `deploy` при потерянном state создаст вторую
   ВМ с тем же именем; `provision` создаёт новую ВМ при каждом вызове.
9. **Порты:** udp открывает только `deploy`; `provision` не открывает ничего;
   `open_ports` открывает, но не записывает, и сравнивает существующие
   правила без учёта протокола (`tools/tools_deploy.go:412-418`), в отличие от
   `sgrules.Open` (`sgrules/sgrules.go:80-97`).
10. **Выбор сети:** `deploy` отказывает, если нет подсети или SG в зоне;
    `provision` берёт первую подсеть и первую SG (комментарий там же говорит,
    что SG другой зоны приводит к `error_creating`, `tools/tools.go:1618-1624`);
    `deploy_apply` создаёт ВМ без SG с предупреждением в плане.
11. **Тип диска:** `provision` шлёт `SSD` без проверки
    (`tools/tools.go:1696`), остальные ищут тип в `ListDiskTypes`.
12. **Логин по умолчанию:** `ubuntu` у `deploy`, `user1` у `provision` и
    `deploy_apply` — когда у поля метаданных образа нет значения по
    умолчанию. `image_metadata` все три пути заполняют одной функцией
    `images.FillMetadata` (по `field_type`); расходится только этот
    запасной логин, его передаёт каждый путь.
13. **Отмена:** только `deploy` на терминале предлагает удалить ВМ; MCP-путь
    `deploy` (`NonInteractive: true`) и два других оставляют ВМ.
14. **Гейт обновления:** `evo deploy` для существующей ВМ не спрашивает, MCP
    `deploy` спрашивает всегда.
15. **Блокировка:** `deploy` и `destroy` падают сразу, если state занят;
    `provision`, `deploy_apply` и `destroy vm_id` ждут до 2 минут, а затем
    возвращают ответ с предупреждением «не в state» (`tools/tools.go:1757-1762`).
16. **Ожидание порта 22** есть только у `deploy_apply` (120 с); `deploy`
    ждёт SSH-подключения (60 попыток по 5 с, `engine/deploy.go:31`, `:193-196`);
    `provision` не ждёт.

## 6. Кто за кем убирает

| ресурс | создаёт | удаляет | если удалить в консоли мимо evo |
|---|---|---|---|
| ВМ из state | `deploy`, `provision` (`vm create`), `deploy_apply`, шаг `vm` стека | `evo destroy`/MCP `destroy` (активная запись), `destroy vm_id`/`vm delete`, `stack_destroy` (через `destroy vm_id`) | `status`, MCP `status`/`list` показывают запись как была — они читают только state (`engine/status.go:12-54`, `tools/tools.go:1905-1931`). `logs` падает на SSH, `verify` — на проверке портов (код 1). `deploy` снова берёт запись, ждёт SSH и пишет `ssh_timeout`, новую ВМ не создаёт. `evo destroy` получает 404, пишет «ВМ уже удалена» и доочищает FIP и правила (`engine/destroy.go:90-94`; 404 → `VM_NOT_FOUND` в `provision/adapter.go:443-445`). `destroy vm_id` ищет ВМ в списке проекта, не находит и отвечает ошибкой; запись остаётся (`tools/tools.go:1999-2012`). |
| ВМ стека | `stack_apply` → `provision` | `stack_destroy` | `stack_destroy` получает от `destroy vm_id` «no VM matching …»; эта строка не содержит `404`/`NOT_FOUND`/`not found` (`tools/tools_stack.go:699-701`), поэтому job переходит в `destroy_failed` и останется в нём при повторах (проверено временным тестом с фейковым облаком). |
| FIP | `deploy`, `provision`, `deploy_apply` | `destroy` (по `floating_ip_id`, иначе по адресу/имени), `release_floating_ip` (только свободный) | `destroy`: 404 при удалении FIP считается «уже освобождён» (`engine/destroy.go:284-288`). |
| FIP после ошибок | boot FIP при неудачном CreateVM | освобождается сразу (`engine/deploy.go:527`, `tools/tools.go:1778`); при неудаче освобождения — предупреждение «удалите в консоли» (`engine/deploy.go:895-898`) | — |
| правило SG (deploy, deploy_apply) | `sgrules.Open` | `destroy` после удаления ВМ; правило, нужное другой записи этого state, переходит к ней (`sgrules/sgrules.go:296-331`); неудачное удаление — запись `destroyed`, повтор `destroy` | 404 считается удалённым (`sgrules/sgrules.go:318-323`). |
| правило SG без ID в ответе | `sgrules.Open` (`Untracked`) | никто; deploy предупреждает (`engine/network.go:115-119`) | — |
| правило SG от `open_ports` | `open_ports` | никто | — |
| SSH-ключ `.evo/id_ed25519` | `deploy` (после согласия), `logs`, `provision`, `deploy_plan`, `deploy_apply` (`cli/main.go:297-335`) | никто; `destroy` не трогает | — |
| `.evo/known_hosts` | `deploy_apply` | никто | — |
| план `.evo/plans/<id>.json` | `deploy_plan` | `deploy_apply` при любом ответе Apply без ошибки, в том числе `error` и `timeout` (`tools/tools_deploy.go:216`); при ошибке остаётся для повтора. Просроченный план не удаляется (`deployplan/store.go:150-152`) | — |
| job стека `~/.evo/stack-jobs/` | `stack_plan` | никто; `stack_destroy` ставит `destroyed`/`destroy_failed`, файл остаётся | — |
| ресурсы стека (VPC, PG, Kafka, Redis, k8s, реестр, SNAT, TG, LB, запись DNS, секреты, объекты S3) | `stack_apply` | `stack_destroy` в обратном порядке (`tools/tools_stack.go:709-752`) | 404 и «not found» в ошибке считаются удалёнными (`tools/tools_stack.go:699-701`). Перед удалением ID берутся из шагов, а для начатых шагов — поиском по имени (`:676-690`). |
| ВМ из неудачных попыток шага `vm` | `provision` при ответе-ошибке с `vm_id` (`error`, `timeout`) | `stack_destroy` их не видит: при неотменённой ошибке `ResourceID` не сохраняется, у шага нет `FindTool`, шаг повторяется до 4 раз (`tools/tools_stack.go:261-283`, `stack/store.go:565-567`). Каждая такая ВМ записана `provision` в `state.json`; убирает `evo destroy` по одной (первая активная) или `destroy vm_id`. Подтверждено временным тестом на поддельном `provision` (4 вызова, шаг `error`, `ResourceID` пуст). | — |
| k8s, pg, kafka, redis, LB, TG, VPC, DNS, реестр, CA, сертификаты, диски, образы, бакеты, секреты вне стека | соответствующие `*_create` | только соответствующие `*_delete`; учёта в evo нет | `*_delete` возвращает ошибку 404 от API; evo ничего не помнит, кроме job стека. |
| группа узлов и плагин k8s, резервная копия pg, версия секрета, привязка FIP (`fip_attach`) | `k8s_add_nodepool`, `k8s_add_addon`, `pg_backup_create`, `secret_add_version`, `fip_attach` | парного инструмента нет; группа узлов, по-видимому, уходит с кластером (не проверено) | — |
| загрузочный диск ВМ | вместе с ВМ | вместе с ВМ (по API; не проверено); дополнительные диски мешают удалению ВМ (HTTP 405, `engine/destroy.go:350-363`) | — |

Ещё два факта про `destroy` по `vm_id`:

- ВМ ищется и удаляется в проекте конфига, а не в `project_id` записи
  (`tools/tools.go:1998-2012`, `:2055`); FIP освобождается в проекте записи,
  если он там есть (`:2026`, `engine/destroy.go:252`).
- С `release_ip=false` FIP остаётся, а запись удаляется
  (`tools/tools.go:2036-2039`, `:2117-2119`): FIP больше нигде не учтён.

`evo destroy` удаляет только первую активную запись; при нескольких записях
в одном state (например, после двух `vm create`) нужно несколько запусков.

## 7. Грамматика и дубли имён

Дублирующие команды:

- `evo list-zones` (свой код, `cli/main.go:718-762`) и `evo vm zones`
  (инструмент `list_zones`, `tools/tools.go:2146-2162`).
- `evo list-images` — только общий каталог (`engine/list_images.go:17`);
  инструмент `list_images` добавляет образы проекта (`tools/tools.go:2186-2195`).
  Команды дерева для образов нет.
- `evo destroy` (запись state) и `evo vm delete <id|имя>` (инструмент
  `destroy` по `vm_id`/`name`): один инструмент, разное поведение (6).
- `evo deploy` и `evo vm create` (`provision`): обе создают ВМ и пишут в
  один state (5).
- `evo status` и MCP `status`; MCP `list` в CLI не имеет команды.
- `evo vm list` (облако) и MCP `list` (state) — одно слово, разные источники.

Глаголы удаления: `destroy` (верхний уровень), `delete` (деревья), `rm`
(`s3`), `fip-release` (`vpc`), `bucket-delete`, `db-delete`, `user-delete`,
`route-delete`.

Глаголы списков: `list`, `buckets`, `ls`, `fips`, `subnets`,
`security-groups`, `types`, `routes`, `databases`, `users`, `backups`,
`nodepools`, `zones`, `flavors`, `versions`, `specs`, `options`.

Флаги (флаг = имя аргумента инструмента, `_` → `-`, `cli/trees.go:182-188`):

- `--force` значит разное: у `deploy` — развернуть заново без изменений
  (`cli/main.go:537`), у `destroy` — то же, что `--yes` (`cli/main.go:628`,
  `:636`), у `secret delete` — удалить используемый секрет.
- `--non-interactive` есть у `deploy` и `init`, в деревьях его нет (там без
  терминала — код 3).
- `deploy` объявляет свои `--project-id` и `--region` поверх глобальных
  (`cli/main.go:533-534`).
- память: `--ram-gb` (`k8s plan/create/add-nodepool`), `--ram` (`pg create`);
- диск: `--disk-size` (`vm create`), `--size-gb` (`disk create/resize`),
  `--disk-gb` (`k8s add-nodepool`), `--storage-gb` (`pg create`);
- подсеть: `--subnet-id` (`vm create`, `pg create`, `k8s plan/create`),
  `--nodes-subnet-id` (`k8s add-nodepool`), `--subnet` (`vpc route-create`);
- ВМ: позиционный `<vm-id|name>` (`vm get/delete`), `--vm` (`disk attach/
  detach`, `vpc fip-attach`), `--attach-to` (`disk create`);
- зона: глобальный `--region` (UUID) и `--zone` (имя, `disk create`, `k8s`);
- регион S3: `--s3-region` (`s3 bucket-create`, переименован из-за глобального
  `--region`, `cli/trees.go:118-119`);
- `pg get` и `pg delete` принимают и позиционный `<cluster-id>`, и флаг
  `--id` (в схеме оба аргумента);
- позиционные ID: `<id>` (`vpc get/delete`), `<vpc-id>`, `<disk-id>`,
  `<cluster-id>`, `<operation-id>`, `<path>`, `<bucket>`;
- `vpc fip-release` — позиционный `[id]` или `--ip-address`;
- у `provision` есть аргумент `dry_run`, не объявленный в схеме
  (`tools/tools.go:1709-1713`); в CLI это глобальный `--dry-run` с другим
  смыслом (превью гейта).

## 8. Кандидаты в инварианты для теста

| # | правило | как проверить | сейчас |
|---|---|---|---|
| 1 | Изменяющий инструмент без `confirmed=true` не шлёт в облако ничего, кроме чтений | уже есть: `TestQA_MutatingToolsWriteNothingWithoutConfirmed` (`tools/qa_tools_test.go:65`) | проходит |
| 2 | Каждая команда дерева ссылается на существующий инструмент, у каждого инструмента есть обработчик и группа | уже есть: `TestTrees_MatchRegistry` (`cli/trees_test.go:20`), `TestRegistry_OneHandlerPerTool`, `TestRegistry_EveryToolHasAKnownGroup` (`tools/registry_test.go:24`, `:46`) | проходит |
| 3 | У каждого изменяющего инструмента, который что-то создаёт, есть парный удаляющий | список пар в тесте (`*_create` → `*_delete`, `provision`/`deploy`/`deploy_apply` → `destroy`, `s3_put` → `s3_rm`, …), сверка с реестром | не проходит: нет пары у `open_ports`, `fip_attach`, `k8s_add_nodepool`, `pg_backup_create`, `secret_add_version` (временный тест) |
| 4 | Каждый статус, который пишется в `DeploymentRecord.Status`, есть в объявленном списке | AST-обход: строковые литералы в `Status =`, `Status:` у `DeploymentRecord`, аргументы `saveState`/`recordDeployment`, плюс запрет нелитеральных значений | не проходит: `error`, `timeout` и статус из облака в `deploy_apply` (`deployplan/apply.go:78-81`) |
| 5 | Каждый инструмент, создающий платный ресурс, либо пишет state/job, либо явно помечен «без учёта» | поле или список в реестре + проверка, что обработчик из списка «с учётом» вызывает `state.Update`/`Save` | не проходит: пометки нет; учёт ведут только `deploy`, `provision`, `deploy_apply`, `stack_apply` |
| 6 | Каждое имя FIP, которое даёт evo, находит `destroy` без `floating_ip_id` | таблица имён (`fip-<vm>-<ts>`, `fip-<vm_id>`, `<vm>-fip`) и вызов `releaseFloatingIP` с поддельным облаком, где у FIP есть только имя | не проходит (по коду): `engine/destroy.go:266` знает только `fip-<vm_id>` и адрес; тест не писался |

Проверки 3 и «ВМ стека, удалённая в консоли» (6) сделаны временным тестом в
`internal/tools`, в коммит он не вошёл.

## Что не проверено

- Поведение Cloud.ru вживую: всё выше выведено из кода и комментариев.
- Удаляется ли загрузочный диск вместе с ВМ и группа узлов вместе с
  кластером k8s.
- Что происходит с внешним адресом балансировщика (`lb_create
  external=true`) при `lb_delete`.
- Даёт ли API создать две ВМ с одинаковым именем (важно для `deploy` при
  потерянном state и для повторного `provision`).
- Общие таймауты MCP-вызова на стороне клиента и сервера: в этом документе
  учтены только ожидания внутри evo.
