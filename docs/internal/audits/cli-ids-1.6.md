# Линия CLI: где человек видит UUID вместо имени (Fable, 29.10.2026)

Только чтение. Код — `main` на `f0f76da`. Факты — файл:строка кода `main` или
реальный вывод команд на фейковом мире паритета (`withJSONWorld`, тот же, что у
`TestPreview_HumanShowsEveryValue`; снят overlay-тестом из scratchpad, в
репозиторий ничего не писалось: `go test -overlay … -run TestFableDump`).
Гипотезы — поведение живого API, которого фейк не моделирует; у каждой —
проба. Мнения — тяжесть, приоритет, версия; решает владелец.

Содержание: 1 резюме · 2 как устроен вывод сейчас · 3 инвентарь (таблицы) ·
4 входы, где обязателен UUID · 5 ограничения и позиция по паритету ·
6 механизм одной точки · 7 инварианты для «Процесса» · 8 ломающее/нет ·
9 топ-10 · 10 где я против · 11 пробы.

## 1. Резюме

Зона — уже решена почти везде: `vm list`, `disk list`, `snat list`, все превью
create (`completePreview` дочитывает имя по id, `refText` печатает
«ru.AZ-1 (uuid)»), `status`/`destroy` (`withNames`). Остались три дыры у зоны:
`evo vpc subnets` и `evo vpc security-groups` печатают колонку
`AVAILABILITY_ZONE_ID` голым UUID (а имя подсети API отдаёт тем же вызовом и
`Subnet.UnmarshalJSON` его выбрасывает — цена 0); `evo k8s options` —
`defaults.zone` UUID; и на входе: `disk create`, `snat create`, `image create`
берут `CLOUDRU_REGION`/`--region` как UUID без разрешения имени
(`tools_disk.go:135`, `tools_snat.go:42`, `tools_image.go:152`), хотя
`docs/auth.md:234` и справка корневого `--region` («UUID зоны») это и обещают —
но README:44 обещает «`--region` понимает имя». Это класс D15, проба P-Z1.

Главная находка не про зону. **Подсеть** — самый частый UUID по линии CLI:
её вводят семь create (`vm create --subnet-id`, `pg/kafka/redis create
--subnet-id`, `k8s create/add-nodepool`, `lb create`), и восемь превью
печатают `subnet: <UUID>` (`pg_create`, `kafka_create`, `redis_create`,
`k8s_create`, `k8s_add_nodepool`, `lb_create`, `deploy_apply`, `provision
--subnet-id`) — при том, что каждый из этих обработчиков уже прочитал
`list_subnets` до вопроса (имя бесплатно). Так же бесплатны имена
кластера (`k8s_add_nodepool`, `pg_db_create`, `pg_user_create`,
`pg_backup_create`), целевой группы и VPC (`lb_create`: `GetTargetGroup`
уже вызван, а превью не называет TG вообще — строка `humanUnnamed`
«id того, что превью называет по имени» для `lb_create.target_group_id` и
`tg_create.targets.id` неверна: имени в превью нет), ВМ в `tg_create`
(`GetVMDetail` вызван на каждую цель), адреса в `release_floating_ip`
(`GetFloatingIP` вызван), сервисного аккаунта в `k8s_create` (1 вызов
`iam_service_accounts`).

Механизм: одна декларация в реестре «поле/аргумент → список → поле имени» —
это уже существующая таблица автодополнения `valueRefs`/`toolRefs`
(`completion.go:46–76`), её и расширить; из неё три потребителя: Tab,
разрешение имени на входе (отказ при двух, класс D35) и имя в ответе
(`<поле>_name` рядом с id — паритет сохраняется, `--json` = ответ
инструмента). Человеку — одна функция `refText`-класса «имя (id)» в превью
и имя в колонке списка. Не ломающее по `docs/RELEASING.md`: поля
добавляются, ключи не переименовываются, коды и `reason` не меняются.
Позиция по паритету: имя только в человеческом выводе без поля в ответе —
нарушение §1 и §6 ось 4 (агент видит меньше человека), не «подача».

Топ по пользе: (1) `vpc subnets`/`security-groups` — имя зоны, 0 вызовов;
(2) `--subnet-id` и `subnet:` в восьми превью — имя, 0 вызовов; (3) входы по
имени для подсети, группы безопасности, VPC, целевой группы, кластеров;
(4) зона по имени в `disk/snat/image create`; (5) проект в `whoami`,
`doctor`, превью — имя из `org_projects` с кэшем, молча пропускать, когда
ключ списка не видит.

## 2. Как устроен вывод сейчас (точки рендера)

| Точка | Где | Что делает с id |
|---|---|---|
| Таблица списка | `internal/cli/trees.go:941–972` `renderList`, колонки из `CLISpec.Columns` (`cli_spec.go:71–284`) или `autoColumns` (`preferredColumns: id, name, …` :937) | печатает значение как есть; вложенный объект с `name` → имя (`cell`, :1019–1021, только при `max>0`, т. е. в списках) |
| Карточка get | `renderAnswer` :855–896: FIELD/VALUE, вложенные объекты — JSON | `k8s_get`, `pg_get`, `lb_get`, `tg_get`, `kafka_get`, `redis_get`, `vpc_get`, `cert_get`, `dns_zone_get`, `ar_get` отдают сырую карту API (`toJSON(m)`) |
| Превью create | `engine.ShowCreatePreview` (`create_preview.go:457–560`): `refText` «name (id)» (`preview.go:446`), `flavorText`, `projectText` «uuid (из профиля)», `paramLine` по `paramLabels` (:794–846) | зона: `completePreview` (:303–317) дочитывает имя по id из `list_zones` (`PreviewLookupTimeout` 20 с); flavor и образ — по каталогу; подсеть/SG — только у `deploy` (`DeployPreview`, `SubnetName`, `SecurityGroup.Name`) |
| Превью без `preview` | `cli/tools.go:723–735` `toolPreview` → `argLines` «key: json» | всё как в ответе инструмента, объекты JSON-ом (`destroy`: `target: {"id":…,"name":"web",…}`) |
| Превью delete | `delete_preview.go:24–57` `deletePlan`: один GET → «"name" (id)», `target{id,name,status}` | одна точка на 12 delete; не через неё: `disk_delete`, `image_delete`, `release_floating_ip`, `vpc_route_delete`, `dns_zone_delete`, `dns_record_delete`, `cert_delete`, `cert_renew`, `secret_delete`, `s3_*` |
| status/destroy | `engine/names.go:21–78` `withNames`: имя ВМ, зоны, flavor из GET ВМ + `list_zones` + `list_flavors`, `StatusRefreshTimeout`, best effort | прецедент «UUID → имя» с v1.0.1; результат — поля `vm_name`, `zone_name`, `flavor_name` в записи state |
| Человеческие вьюхи | `treeRenders` (`trees.go:62–75`): image, marketplace, addons, project, billing, vm-created | `renderImageList` прячет UUID, подсказывает «UUID — в --json или evo image get» (`images.go:124`) |
| Подсказки/ошибки | `project_bound.go:336–341` «id — в выводе vm_list»; `namecheck.go:73–81` «(id …, status …)»; `create_preview.go:467` `flavorNotFound` «name (uuid), …» | id нужен для копирования; у `flavorNotFound` UUID — шум (20 flavor × 36 символов) |

Что уже решено правильно (не трогать): `vm list` zone по имени
(`tools_vm.go:126–130`); `disk list` zone, disk_type, attached_to по именам
(`tools_disk.go:60–74`); `snat list` оба поля (`tools_snat.go:72–76`);
`k8s nodepools` flavor по имени (:472–476); `deploy` «network: subnet
Default_ru.AZ-1, security group SSH-access_ru.AZ-1»; `deletePlan`; `open/close
ports` по имени группы (`securityGroupByRef`, `tools_deploy.go:705–735`, с
отказом при двух одинаковых — образец для входов); `vm get`/`vm delete`/`fip
attach`/`disk create --attach-to` по имени ВМ (`resolveVM`).

## 3. Инвентарь: где человек видит UUID

Легенда. **(а)** — собственный id объекта, нужен для следующей команды.
**(б)** — ссылка на другой объект, у которого есть имя. Источник имени:
**0** — уже в ответе API или прочитано тем же вызовом; **1** — один
дополнительный список (кэшируемый); **—** — имени нет. Тяжесть: В/С/Н.
Частота: `deploy`, `vm`, `pg` — частые. Вывод — с фейкового мира (`ru.AZ-1`
= `…b2`, подсеть `…d1`, SG `…c1`, проект `…a1`).

### 3.1 Списки и get

| Команда | Что видит человек (факт, мир) | Случай | Источник имени | Тяж. | Частота |
|---|---|---|---|---|---|
| `vpc subnets` | `AVAILABILITY_ZONE_ID 00000000-…-b2` (`tools_deploy.go:543`) | б | **0**: `availability_zone{id,name}` в ответе, `Subnet.UnmarshalJSON` берёт только id (`provision/types.go:173–187`; комментарий: «documented nested availability_zone{id,name}») | **В** | высокая — это список, откуда берут `--subnet-id` |
| `vpc security-groups` | то же, `tools_deploy.go:565` | б | 0, если API шлёт `availability_zone.name` (гипотеза, P-N1; `SecurityGroup.UnmarshalJSON` :203–217 читает `ZoneRef{ID,Name}`); иначе 1 (`list_zones`) | В | средняя |
| `k8s options` | `defaults {"zone":"…b2",…}` | б | 0: `pc.Zones[].Name` в том же ответе (`tools_k8s.go:117–124`) | С | низкая |
| `k8s options` | `FLAVOR_ID …d4` колонкой | а? | `--flavor` принимает имя; id человеку не нужен — шум | Н | низкая |
| `lb list`, `tg list` | `vpc_id` UUID (`tools_nlb.go:72,138`) | б | 1: `vpc_list` | С | низкая |
| `k8s get` | сырая карта: `controlPlane.zones[]` UUID, `networkConfiguration.privateVipSubnetId`, `sizingConfiguration.flavorId`, `identityConfiguration.clusterSaId` (гипотеза по телу create, `api-contract.json:157`; P-K1) | б | 1–3 (`list_zones`, `list_subnets`, `k8s_options`, `iam_service_accounts`) | С | средняя |
| `pg get`, `kafka get`, `redis get` | сырая карта: `specification_id`, `subnet_ids`/`subnet_id`, `version_id` (гипотеза по телам create, `api-contract.json:195,220,237`; P-D1) | б | 0–1 (`*_specs` по версии, `list_subnets`, `*_versions`) | С | средняя (`pg`) |
| `lb get`, `tg get` | сырая карта: `vpc_id`, `availability_zone_specs[]`, `target_vms[].zone_id` (+`zone_name` — манифест читает оба, `api-contract.json:141`) | б | 0–1 | Н | низкая |
| `cert list` | `project <UUID>` строкой | б | 1 (`org_projects`, может быть недоступен) | Н | низкая |
| `whoami` | `Project ID: <UUID> (from env)` (`whoami.go:85`) | б | 1: `org_projects` — ключ сервисного аккаунта списка не видит (`tools_admin.go:142–152`, 400); тогда имени нет честно | С | высокая (первая команда новичка) |
| `doctor` | `project_id from env` без UUID и без имени (`tools_auth.go:156`) | — | как у whoami | Н | средняя |
| `status` | `Region ru.AZ-1 (…b2)`, `VM ID …` | а/ok | `withNames` | — | — |
| `vm get` | `interfaces [{"id":"…f1",…}]` JSON; зоны, flavor, образа нет совсем (`tools_vm.go:91–98`), хотя `VM.UnmarshalJSON` читает `flavor{id}`, `image{id}`, `availability_zone` | а (id интерфейса нужен `tg create --interface-id`, `vpc route-create`) | — ; отсутствие зоны/flavor/образа — ось 4, не этот аудит | Н | высокая |
| `kafka specs`, `redis specs` | `resolved_version_id v1` | б | 0: `ListVersions` уже вызван (`tools_kafka.go:303–306`), у версии есть `name` (мир: `{"id":"v1","name":"1"}`) | Н | низкая |
| `pg specs`, `kafka/redis specs` | `ID std`, `s-1` первой колонкой | а (`--specification-id`), есть альтернатива `--cpu --ram` | — | Н | низкая |
| `vm zones`, `disk types`, `org projects`, `vm list`, `disk list`, `dns zones`, `vpc fips`, `snat list` | id + name | а | — | — | — |
| `audit events` | сырые события (`tools_audit.go:48–53`): id субъектов и ресурсов (гипотеза) | б | — (событие и так несёт имена? P-A1) | Н | низкая |

### 3.2 Превью (`--dry-run` и вопрос о согласии) — то же, что видит агент в `preview`

| Инструмент (команда) | Строка превью (факт, мир) | Случай | Источник имени | Тяж. | Частота |
|---|---|---|---|---|---|
| `pg_create` | `subnet: …d1` | б | 0: `dbaasSubnet` прочитал `list_subnets` (`tools_pg.go:400`) | В | высокая |
| `kafka_create`, `redis_create` | `subnet: …d1`; `version-id: v1` (ключ не в `paramLabels`) | б | 0 (подсеть; версия — `ListVersions`) | С | средняя |
| `k8s_create` | `subnet: …d1`; `account: …0001` | б | 0 подсеть (`k8sVIPSubnet` :766); SA — 1 (`iam_service_accounts`, у SA есть `name`, `email`) | В | средняя |
| `k8s_add_nodepool` | `cluster: …e1`, `subnet: …d1`, `SG: …c1` | б | 0: `clusterZoneID` → `GetCluster` (имя кластера), `k8sNodesSubnet` → `ListSubnets`, `ListSecurityGroups` при автоподборе (`tools_k8s.go:527–570`) | В | средняя |
| `k8s_add_addon` | `cluster: …e1` и `cluster name: k8s-dev` | ok (обе) | — | — | — |
| `lb_create` | `subnet: …d1`; целевая группа и VPC не названы вовсе (ни именем, ни id) | б | 0: `GetTargetGroup` вызван (`tools_nlb.go:329`), VPC — 1 (`vpc_get`); подсеть — `lbZoneAndSubnet` читает список | **В** (дыра согласия: человек не видит, к какой TG привязывает платный LB) | средняя |
| `tg_create` | `targets: 1` — ВМ не названа | б | 0: `GetVMDetail` на каждую цель (`tools_nlb.go:238`) → имя ВМ | В | средняя |
| `deploy_apply` | `network: subnet …d1` (у `deploy` — по имени) | б | 0 на этапе `deploy_plan` (`resolveNetwork` читал подсеть); в плане имени нет → добавить `subnet_name` в план при `deploy_plan` | С | высокая (`deploy`) |
| `provision --subnet-id` | `network: subnet <UUID>` (`create_preview.go:550`) | б | 0: `checkNetwork` читает подсети | С | высокая (`vm`) |
| `snat_create` | `zone: ru.AZ-1` **и** `availability-zone-id: …b2` второй строкой | шум | ключ не в `paramLabels`, `willPreview` добавляет оба (`tools_snat.go:112–119`) | Н | низкая |
| `vpc_route_create` | `vpc-id`, `next-hop-vm-id`, `next-hop-interface-id` — три UUID | б | 1: `vm_list` (имя ВМ), `vpc_get` (имя VPC); интерфейс — имени нет (а) | С | низкая |
| `vpc_route_delete` | `route <UUID> from VPC <UUID>` | б | 0: `GetVPC` и `ListRoutes` вызваны (`tools_vpc.go:294–306`) → имя VPC, CIDR маршрута | С | низкая |
| `dns_record_create` | `in public zone <UUID>` | б | 0/1: `dnsZoneVisibility` ищет зону (домен в `dns_zones`) | С | средняя (стек) |
| `dns_zone_delete`, `dns_record_delete` | только UUID, без домена/имени записи | б | 1 (зона читается до вопроса, W6) | С | низкая |
| `cert_delete`, `cert_renew` | `certificate "<UUID>"` — UUID в кавычках, как будто имя | б | 0/1 (`planCertDelete`/`cert_get`) | С | низкая |
| `release_floating_ip` | `releases floating IP <UUID>` при вызове по id | б | 0: `GetFloatingIP` уже дал `ip_address` (`tools_deploy.go:873–877`) | С | средняя |
| `disk_delete`, `disk_resize` | `disk <UUID>` без имени | б | 0: `GetDisk` вызван (`tools_disk.go:361, 410`) → `name` | С | средняя |
| `disk_attach`, `disk_detach` | «disk_attach changes a VM.» — ни диска, ни ВМ | б | D53 (1.6) уже планирует GET диска до вопроса → имя бесплатно; ВМ — `resolveVM` (имя дано) | С | средняя |
| `image_create` | `disk: <UUID>` | б | 0/1: зона берётся из диска («the API always names», `parity_fixtures_test.go` комментарий к `computeParity`) → GET диска даёт имя | Н | низкая |
| `image_delete` | `will_delete {"image_id": <UUID>}` | б | 1: `GetImage` (kindImage уже делает его в `--allow-project`) | Н | низкая |
| `pg_db_create`, `pg_user_create`, `pg_backup_create` | `in cluster <UUID>` | б | 1: `pg_get` (у `pg_backup_create` — `ListBackups` уже есть, кластера нет) | С | высокая (`pg`) |
| `destroy` (`vm delete`) | `target: {"id":…,"name":"web",…,"zone":""}` JSON-ом | форма | имя есть; `argLines` печатает объект; `zone` пустое поле | Н | высокая |
| `provision`, `deploy`, `disk_create`, `k8s_*`, `kafka/redis_create`, `lb_create`, `snat_create` | `zone: ru.AZ-1 (…b2)` / `ru.AZ-1` | ok | `completePreview`/`willPreview` | — | — |
| все `*_delete` через `deletePlan` | `"name" (id)` + `target` | ok | — | — | — |

### 3.3 Сообщения об успехе, ошибки, подсказки, ход

| Поверхность | Факт | Случай | Оценка |
|---|---|---|---|
| `evo vm create` итог (`vm_created.go`) | таблица ответа + SSH + пароль; id ВМ (а) | ok | — |
| `pg_create`/`lb_create`/… успех | `created: willCreate` (с `subnet_id`), `cluster_id`/`lb_id` (а) | б/а | имя подсети — то же поле, что в превью |
| `flavorNotFound` (`create_preview.go:462–472`) | «Есть: lowcost10-2-4 (uuid), …» ×N | шум | UUID flavor не нужен нигде: `--flavor` принимает имя; оставить имена |
| `notInProject` (`project_bound.go:336`) | «<kind> <id> удалён или не в проектах…; id — в выводе vm_list» | а | ok |
| `nameTakenResult` | «(id …, статус …)», `existing_id` | а | ok |
| `lb_create` VPC ≠ TG (`tools_nlb.go:328–331`) | три UUID в тексте | б | имена TG/VPC бесплатны/1 вызов |
| `resolveProjectArg` (`project.go:124–127`) | «несколько проектов с именем …: <ids>» | а | образец отказа при неоднозначности |
| `askZone` (`configure.go:613–620`) | «1) ru.AZ-1  <uuid>» | ok | — |
| справка корневого `--region` (`cli/tools.go:1016`, `commands.md:252`) и `docs/auth.md:234` | «UUID зоны доступности» | вход | расходится с README:44 и D15; см. §4 |
| ход (stderr, `Step`/`StepDone`) | «Recommended flavor: free-tier1 (1 vCPU…)», `Dry run: would create VM … in ru.AZ-1` | ok | — |

## 4. Входы: где человек обязан ввести UUID, хотя имя однозначно

Внутри evo имена уникальны в проекте по построению: каждый create отказывает
при занятом имени (D20, D21, D23, D28, `takenName`), значит разрешение
имени на входе безопасно при одном правиле: два ресурса с одним именем
(созданы консолью) — `USAGE` со списком id, как `securityGroupByRef`
(`tools_deploy.go:719–735`) и `resolveProjectArg` (`project.go:116–127`).

| Аргумент | Где обязателен UUID | Образец с именем уже есть | Список для разрешения | Цена |
|---|---|---|---|---|
| `subnet_id` | `provision`, `pg_create`, `kafka_create`, `redis_create`, `k8s_create`, `k8s_plan`, `lb_create`, `nodes_subnet_id` у `k8s_add_nodepool` | нет; схема `format: uuid` (храповик `schema_rules_test.go`) | `list_subnets` (`Default_ru.AZ-1`, `name` уникально в зоне? — гипотеза P-N2; у `vm create` подсеть по умолчанию и так берётся по зоне) | 0 — список уже читается в каждом из них |
| `security_group_id` | `k8s_add_nodepool` | `open_ports`/`close_ports` берут имя | `list_security_groups` | 0 (`ListSecurityGroups` уже вызван при автоподборе) |
| `vpc_id` | `lb_create`, `vpc_routes`, `vpc_route_create/delete`, `vpc_get/delete` | нет | `vpc_list` | 1 |
| `target_group_id` | `lb_create` | нет | `tg_list` | 1 (`ListTargetGroups`? нет — `GetTargetGroup` по id; список — 1 вызов) |
| `cluster_id` / `id` | `k8s_*` (9 инструментов), `pg_*` (10), `kafka_get/delete`, `redis_get/delete` | `ca_*` берут имя приложения; `vm_get` — `vm_id\|name` (`CLISpec.Args` «vm_id\|name», `trees.go:538–543` UUID → id, иначе name) | `k8s_list`, `pg_list`, `kafka_list`, `redis_list` | 1; у `pg_delete` список уже читается? нет — `deletePlan` делает GET; 1 вызов |
| `disk_id` | `disk_attach/detach/resize/delete`, `image_create --disk-id` | `disk_create --attach-to` берёт имя ВМ | `disk_list` | 1 |
| `cluster_sa_id` | `k8s_create/plan` (по умолчанию — свой SA из токена, ok) | — | `iam_service_accounts` (`name`, `email`) | 1 |
| `version_id` | `kafka_create/specs`, `redis_create/specs` | `pg_create --version 17` — по имени | `kafka_versions`/`redis_versions` (`id`, `name`) | 0 (список уже читается) |
| `specification_id` | `pg/kafka/redis_create` | альтернатива `--cpu --ram` есть | — | не нужно |
| `zone_id` (DNS) | `dns_records`, `dns_record_create` | — | `dns_zones` (`domain`) | 1 |
| `id` DNS-зоны, записи, сертификата, реестра AR | `dns_zone_get/delete`, `dns_record_get/delete`, `cert_*`, `ar_get/delete` | — | `dns_zones`, `dns_records`, `cert_list`, `ar_list` | 1 |
| `availability_zone_id` | `snat_create` | рядом есть `zone` по имени | — | ok (дубль аргумента — наследие) |
| **зона из конфигурации** | `disk_create` без `zone` (`tools_disk.go:135`), `snat_create` без `zone` (`tools_snat.go:42`), `image_create` без `zone` (`tools_image.go:152`) берут `Config.Region` как UUID; `k8s`, DBaaS, `lb`, `provision`/`deploy` (`resolveVMZone` :3522–3535), стек — разрешают имя | README:44, D15 | `list_zones` | **гипотеза P-Z1**: `CLOUDRU_REGION=ru.AZ-1` + `evo disk create` → имя уходит в `availability_zone_id`, 4xx после согласия (класс D15: отказ после «да») |

Не трогать (id — единственный ключ): `operation_id`, `plan_id`, `job_id`,
`interface_id`, `route_id` (есть CIDR, но маршрут адресуется id), `record_id`
(имя + тип не уникальны: несколько A-записей), `key` S3, `path` секрета.

## 5. Ограничения и позиция

**Паритет (§1, D13, D14).** `--json` команды = ответ инструмента байт в байт
(`TestParity_JSONEqualsTool`, C4). Имя для человека обязано быть полем ответа
инструмента: иначе агент (MCP) видит меньше человека — это нарушение §6 ось 4
(«поверхности получают одно и то же: аргументы, поля списков и превью — в
реестре; подача может отличаться»). «Подача» — это колонки, порядок, кавычки,
«name (id)» вместо двух полей; **не** данные, которых в ответе нет. Прецедент
уже такой: `withNames` кладёт `vm_name`/`zone_name`/`flavor_name` в ответ
`status`, а не только в таблицу; `DeployPreview` — `subnet_name`,
`security_group.name`. Поэтому: имя — поле ответа (`<поле>_name` рядом с
`<поле>_id`, или объект `{id,name}` там, где поле ещё не строка). Добавление
поля — не ломающее. Бюджет: `toolsListBytes` (`mcp/limits_test.go:57`,
117 679 байт) считает `tools/list` — описания и схемы; ответы в него не
входят. Описания аргументов вырастут на «имя или UUID» у ~15 аргументов
(≈ 0,5 КБ) — тест позволяет с причиной в diff.

**Безопасность.** Имя ресурса — недоверенные данные (G13-2, `quoteCloud`,
`untrusted.go`): в тексте evo (message, hint) имя — в кавычках одной строкой
(`quoteCloud`), в полях данных — как есть; таблица CLI пропускает через
`sanitizeCell` (`printer.go:235`). H7 маскирует по именам полей
`*key(s)`, `*token(s)`, `*pass*`: поле `subnet_name`, `zone_name`,
`cluster_name` под правило не попадает, `cluster_sa_name`/`email` — тоже;
проверить инвариантом `HTTPSecretFields` на новые суффиксы (имя SA — не
секрет). `--allow-project` (H9): разрешение имени ходит только в списки
проекта сервера (`list_subnets`, `vm_list` и т. д. — все уже «whyListed» в
`idArgs`), значит чужой проект не достижим; UUID из чужого проекта и сейчас
отвергает `idArgs`-проверка до обработчика (`project_bound.go`). Имя, которое
ничему не соответствует, — `NOT_FOUND` без утечки существования в другом
проекте (как `notInProject`).

**Надёжность.** (1) Дополнительный список не роняет команду: нет поля —
нет имени; как `completePreview` и `withNames` — `err == nil` ветка, иначе
молча id. Для человека — id как сейчас; отдельной строки «имя не прочитано»
не нужно в списках (шум), но в превью — да, одной строкой `not read:
subnet name` по образцу `not_checked` (`create_preview.go:850–853`,
`notCheckedLine`), чтобы согласие не выглядело полным. (2) Только чтение
до согласия — все списки `Mutating=false`, `listable()` (`completion.go:109`)
это и проверяет. (3) Тайм-аут: `CompletionTimeout` 1,5 с на Tab мал для
превью; у превью уже есть `PreviewLookupTimeout` 20 с на всё; один бюджет
на все дочитывания имён — 3–5 с (как `networkCheckTimeout` 5 с), кэш —
`readCompletionCache` (3 мин, ключ list+project+key, файл `0600` в
`config.CacheDir()/completion`) переиспользуется как есть. (4) На входе:
два ресурса с одним именем — `USAGE` со списком id, ничего не создано
(класс D35: отказ до вопроса). (5) Идемпотентность: разрешение имени не
пишет; повторный create по имени — `takenName`, как сейчас.

**Одна точка (§2, §3).** Сейчас знание «аргумент → список → поле» живёт в
трёх местах: `valueRefs`/`toolRefs` (completion), `idArgs` с `listTool`
(project_bound, подсказки), `paramLabels` (подписи превью) + ручные
`SubnetName`/`withNames`. Предложение — одна декларация в реестре (§6).

## 6. Механизм одной точки (предложение, ~страница)

**Декларация.** В `internal/tools` одна таблица ссылок (имя условное):

```go
// refs: a field or argument that names another object: its list tool,
// the id field of the list, the name field of the list (what a person
// knows it by), and the key of the companion name in the answer.
type ref struct{ list, idField, nameField, nameKey string; byName bool }
var refs = map[string]ref{            // by field/argument name, every tool
  "availability_zone_id": {list: "list_zones", idField: "id", nameField: "name", nameKey: "availability_zone"},
  "subnet_id":            {list: "list_subnets", …, nameKey: "subnet_name", byName: true},
  "security_group_id":    {list: "list_security_groups", …, byName: true},
  "vpc_id":               {list: "vpc_list", …, byName: true},
  "target_group_id":      {list: "tg_list", …, byName: true},
  "disk_id":              {list: "disk_list", …, byName: true},
  "image_id":             {list: "image_list", …},
  "project_id":           {list: "org_projects", …, nameKey: "project_name"},
  "cluster_sa_id":        {list: "iam_service_accounts", nameField: "email", …},
}
var toolRefs = …  // cluster_id → k8s_list | pg_list | kafka_list | redis_list; version_id → kafka_versions | redis_versions; zone_id → dns_zones (nameField: "domain")
var noRef = …     // closed list with why: operation_id, plan_id, job_id, interface_id, route_id, record_id
```

Это и есть `valueRefs`/`toolRefs` из `completion.go:46–76` плюс два поля
(`nameKey`, `byName`): таблица переезжает, не дублируется; `noCompletion`
остаётся её исключениями; `idArgs[].kind.listTool` берёт список отсюда же
(сейчас — своя строка у каждого `ownerKind`). `completionOf` не меняется по
смыслу.

**Три потребителя одной таблицы.**

1. *Tab* — как сейчас (`Complete`), плюс для `byName` предлагаются имена
   (уже есть для `vm_id`).
2. *Вход* — `tools.CheckArgs` (одна проверка аргументов, §1) для аргумента
   с `byName`: значение не UUID → до обработчика читается `list`, ищется по
   `nameField` (точно, без регистра — как `ResolveZone`), 1 совпадение →
   подставляется id и в ответ кладётся `<nameKey>` (человек увидит, что
   разрешилось); 0 → `NOT_FOUND` с подсказкой «список — `<list>`»; ≥2 →
   `USAGE` «несколько <kind> с именем …: id1, id2; укажите UUID», ничего не
   создано. Схема: `format: uuid` снимается у `byName`-аргументов, описание —
   «имя или UUID (список: …)» (храповик `schema_rules_test.go` правится
   декларацией, не списком исключений). Зона уже так работает
   (`provision.ResolveZone`); `Config.Region` в `disk/snat/image create`
   подключить к тому же `resolveVMZone` (`tools.go:3522`).
3. *Выход* — обёртка обработчика в реестре (как `markUntrusted`,
   `untrusted.go:33–57`): после ответа обходит объект, для каждого ключа
   из `refs` со строкой-UUID, у которого нет `<nameKey>` рядом, дочитывает
   `list` (один раз на вызов, через кэш `completion`, общий тайм-аут
   3–5 с), кладёт `<nameKey>`; не прочиталось — ничего. Обработчик, у
   которого имя уже в руках (прочитал список до вопроса — все восемь
   create §3.2), кладёт `<nameKey>` сам и обёртка в сеть не ходит.
   Ответ растёт на одно поле на ссылку; `--json` = ответ (паритет цел).

**Человеческая подача** — одна функция в `internal/cli` (строка плана «Линия
CLI», «строки для человека из `Details` одной функцией»): для пары
`(<поле>_id, <nameKey>)` — в превью `refText` «name (id)» (уже есть,
`preview.go:446`); в таблице — колонка показывает имя, заголовок по подписи;
в карточке get — «name (id)». Декларация «поле → подпись» из той же строки
плана — это `paramLabels` (`create_preview.go:794`) + `treeHeadersRU`
(`trees_ru.go`): свести в реестр одной таблицей `fieldLabels{en, ru, gb,
ref}` — тогда `humanUnnamed`-строки класса «id того, что превью называет по
имени» становятся проверяемыми (ref → имя обязано быть в той же строке), и
ложные (`lb_create.target_group_id`, `tg_create.targets.id`) упадут.
Номер D — тот, что строка плана уже резервирует для «поле → подпись»; одно
решение: «ссылочные поля объявляются в реестре; имя — поле ответа; человек
видит имя, id — в `--json`».

**Что НЕ делать:** не менять тип существующих строковых полей на объекты
(ломает `--json`); не переименовывать `availability_zone_id` → `zone`
(ломает); не сокращать UUID до 8 символов в человеческом выводе (копирование
в следующую команду — главный сценарий CLI; id либо целиком, либо не
показывать — как `renderImageList`, где UUID уехал в `--json`/`image get`
с подсказкой).

## 7. Инварианты для «Процесса» (какой тест поймает новый случай)

1. **`TestHuman_NoBareReferenceUUID`** (мир `withJSONWorld`, все команды
   реестра, человеческий режим, `COLUMNS=200`): каждый UUID в выводе — либо
   в колонке/поле собственного id (`id`, позиционный аргумент, `*_id` в
   `noRef`), либо на той же строке есть имя. Закрытый сокращающийся перечень
   `bareRefs` с классом и причиной — по образцу `humanUnnamed`
   (`preview_human_test.go:38–86`). Ловит `vpc subnets`, `k8s_create
   subnet:`, `cert renew "<uuid>"` и любой новый обработчик.
2. **`TestRefs_EveryIDFieldDeclared`**: каждый аргумент схемы с `format:
   uuid` или именем `*_id` и каждое поле ответа `*_id` на мире — в `refs`,
   `toolRefs` или `noRef` с причиной (образец `TestProjectBound_IDArgsDeclared`,
   `project_bound_test.go:55`). Новый `*_id` без декларации роняет сборку (§3).
3. **`TestRefs_NameEqualsList`**: `<nameKey>` в ответе равен имени из
   `list` на том же мире (ответ не выдумывает имён).
4. **`TestRefs_AmbiguousNameRefusedBeforeWrite`** (W8-класс, матрица
   сбоев): мир с двумя подсетями `Default_ru.AZ-1` → `USAGE`, `writes` пусты,
   код 1; на всех поверхностях одинаково (`TestParity_ErrorReasonEqualsTool`).
5. **`TestRefs_ListFailureKeepsTheAnswer`** (fault world: список → 500/таймаут):
   код 0, ответ без `<nameKey>`, превью — строка `not read`, запись не
   делается дважды; время ≤ бюджета (граница по `wall-clock:`,
   `internal/testrules`).
6. **`TestPreview_HumanShowsEveryValue`** — правка: строка `humanUnnamed`
   класса `unnamedNoise` «id того, что превью называет по имени» принимается
   только если в превью есть `<nameKey>` того же объекта; иначе падает
   (сейчас пропускает `lb_create`, `tg_create`).
7. **H7/H9**: `TestSensitive_NameFieldsNotMasked` (новые `*_name`, `email`
   SA — не маскируются и не содержат секретов) и `TestAllowProject_NameResolvesOnlyInServerProjects`
   (имя подсети другого проекта → `NOT_FOUND`, без утечки).
8. **Бюджет**: `toolsListBytes` — обновить с причиной; на ответы отдельный
   порог не нужен (поле ≤ 64 байт).

## 8. Ломающее и нет (`docs/RELEASING.md`)

Не ломающее (minor 1.6 без условий 1–5): новые поля ответа `*_name`; имя в
колонках и превью человека (человеческий вывод не контракт: правило
называет команды, флаги, коды, `--json`); аргументы принимают имя сверх UUID
(`format: uuid` снимается — расширение входа, старые вызовы с UUID
неизменны); подсказки/описания.

Ломающее (ждёт major или условий 1–5): переименование/удаление ключей
(`availability_zone_id`, `zone_id` в `will_create` у `disk_create`), смена
типа поля, смена `reason` у существующих отказов. Отказ при неоднозначном
имени — новое поведение только для имён, которые сегодня дают `NOT_FOUND`
или уходят в API, — не ломающее. **P-Z1** (имя зоны из конфигурации уходит
в API у `disk/snat/image create`) — если подтвердится, это D15-класс
«согласие на вызов, который не пройдёт»: исправление меняет код после «да»
(5 → отказ до вопроса 1/2) — по условиям 1–5 можно в minor, запись D и
«Миграция».

## 9. Топ-10 по пользе

1. `evo vpc subnets`, `evo vpc security-groups`: `availability_zone` именем
   (0 вызовов: `Subnet.UnmarshalJSON` — не выбрасывать `name`; SG — P-N1). 1.6.
2. `subnet:` именем в восьми превью (`pg/kafka/redis/k8s_create`,
   `k8s_add_nodepool`, `lb_create`, `provision --subnet-id`, `deploy_apply`
   через `subnet_name` в плане) — имя уже прочитано. 1.6, «Линия CLI».
3. `--subnet-id`, `--security-group-id`, `--vpc-id`, `--target-group-id`,
   `cluster_id` (k8s/pg/kafka/redis), `disk_id`, DNS `zone_id` — по имени с
   отказом при двух (§6, вход). 1.6 (подсеть, SG, кластеры), остальное 1.7.
4. `lb_create` и `tg_create`: превью называет целевую группу, VPC и ВМ-цели
   по имени (всё уже прочитано) — дыра согласия, 1.6 первым.
5. Зона по имени из конфигурации в `disk_create`, `snat_create`,
   `image_create` через `resolveVMZone`; справка `--region` и `docs/auth.md:234`
   — «имя или UUID». После P-Z1; 1.6.
6. Проект по имени в `whoami`, `doctor`, превью (`projectText`): из
   `org_projects` с кэшем, молча без него. 1.6.
7. Все delete/resize/release не через `deletePlan` — через него
   (`disk_delete`, `disk_resize`, `image_delete`, `release_floating_ip`,
   `vpc_route_delete`, `dns_zone_delete`, `dns_record_delete`, `cert_delete`,
   `cert_renew`): «"name" (id)», `target`. 1.6 вместе с D53.
8. `k8s_add_nodepool`: `cluster:` именем (GET уже сделан); `pg_db_create`,
   `pg_user_create`, `pg_backup_create`: кластер именем (1 вызов, кэш). 1.6.
9. Сырые get (`k8s/pg/kafka/redis/lb/tg/vpc/cert get`) → проекция с именами
   ссылок (после P-K1, P-D1) — ось 4; 1.7.
10. Шум: `snat_create` вторая строка `availability-zone-id`; `flavorNotFound`
    без UUID; `kafka/redis_create` `version-id: v1` → имя версии; `k8s options`
    `defaults.zone` именем; `destroy` `target` строкой «web (id), зона …», не
    JSON-ом. 1.6, мелочи внутри «Линии CLI».

## 10. Где я против идеи (id лучше имени)

- Собственный id в списках и в ответе create (`vm list ID`, `cluster_id`,
  `lb_id`) — оставить: это ключ для следующей команды и для поддержки
  (`failedDeleteNote`: «обратитесь в поддержку с его id»).
- `--json` всегда с id; имя — дополнительное поле, никогда вместо.
- Не сокращать UUID (8 символов): неоднозначно и не копируется; либо целиком,
  либо спрятать с подсказкой, как в `image list`.
- `interface_id`, `operation_id`, `record_id`, `route_id`, `plan_id`, `job_id`
  — имени нет или оно не уникально; разрешать по имени нельзя.
- Разрешение имени на входе для delete — только с отказом при двух; имя,
  заданное консолью с дублем, иначе удалит не то (именно поэтому
  `securityGroupByRef` отказывает — ревью #315).
- Проект по имени в `whoami` — не ходить в `org_projects`, когда ключ
  уровня проекта (400): лишний вызов на каждую первую команду; только из кэша
  или когда список уже был (`visible_projects`).
- Имя зоны вместо UUID в `config.yaml` — нет: профиль хранит UUID
  (`configure.go:577` разрешает имя при записи), менять формат профиля —
  ломающее и не нужно.

## 11. Пробы (api-probe, только чтение, ≈0 ₽)

| Проба | Вопрос | Для чего |
|---|---|---|
| P-N1 | `GET /api/v1/security-groups`: есть ли `availability_zone.name` в элементе списка | п. 1: 0 или 1 вызов |
| P-N2 | уникально ли `name` подсети в проекте (две подсети `Default_ru.AZ-1` в разных зонах?) | правило отказа при двух; возможно, ключ «имя + зона» |
| P-Z1 | `CLOUDRU_REGION=ru.AZ-1 evo disk create d --size-gb 10 --yes` (cloud-e2e, с уборкой) — уходит ли имя в `availability_zone_id` и что отвечает API | п. 5, класс D15 |
| P-K1 | `GET /v3/clusters/{id}`: имена полей зоны, подсети, flavor, SA в ответе (`controlPlane.zones`, `networkConfiguration.privateVipSubnetId`, …) | п. 9 |
| P-D1 | `GET /v1/clusters/{id}` PostgreSQL/Kafka/Redis: `specification_id`, `subnet_ids`, `version_id`, `availability_zone` | п. 9 |
| P-A1 | `audit_events`: несут ли события имена ресурсов рядом с id | §3.1, низкий приоритет |

Все пробы — «Процессу» первым блоком 1.6 (в план уже заложен один прогон
api-probe по аудиту слепых зон, #496: P-N1, P-K1, P-D1 ложатся туда же).
