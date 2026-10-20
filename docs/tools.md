# Каталог инструментов

<!-- Файл сгенерирован из реестра инструментов. Руками не править:
     go test ./internal/cli -run TestToolsDoc -update-docs
     CI сверяет файл с реестром (TestToolsDoc). -->

Инструменты evo (152) — те же, что у MCP-сервера ([mcp.md](mcp.md)) и у
`evo tools call`. «Команда CLI» — команда инструмента в evo
([commands.md](commands.md)). «Описание» — первая фраза английского описания,
которое читает агент в MCP; «Описание (RU)» — краткое русское, из справки
команды. Полное описание и схема аргументов — `evo tools describe <имя>`
(с `EVO_LANG=en` — по-английски). «Меняет»: «да» — инструмент
создаёт или меняет ресурсы и без `confirmed=true` (в CLI — `--yes`)
только показывает превью; «удаляет» — может удалить или перезаписать данные;
«нет, секрет» — ничего не меняет, но показывает секрет открытым текстом и
тоже требует `confirmed=true` (`--yes`).
Группы — для `evo mcp --toolsets` и `evo tools list --group`.

## ar

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `ar_list` | `evo ar list` | нет | — | List Artifact Registry registries in the project (id, name, type, status, public, tariff). | Реестры Artifact Registry проекта |
| `ar_get` | `evo ar get` | нет | `id` | Get full details of one Artifact Registry registry by id (from ar_list). | Реестр Artifact Registry по ID: все сведения |
| `ar_create` | `evo ar create` | да | `name` | Create an Artifact Registry registry (async); refuses a name an existing registry has (ar_list). | Создать реестр Artifact Registry (платно, асинхронно) |
| `ar_delete` | `evo ar delete` | удаляет | `id` | Delete an Artifact Registry registry by id (from ar_list) AND its artifacts (async). | Удалить реестр вместе с артефактами (асинхронно) |
| `ar_operation` | `evo ar operation` | нет | `operation_id` | Poll an Artifact Registry async operation by id (from ar_create/ar_delete). | Состояние асинхронной операции Artifact Registry |

## audit

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `audit_events` | `evo audit events` | нет | — | Audit Logging — who did what in the project (read-only): control-plane events (create/delete/modify of VMs, DBs, k8s, S3, IAM, …) with actor, action, resource … | Журнал аудита проекта: кто, что и когда сделал |

## billing

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `billing_balance` | `evo billing balance` | нет | — | Account balance for the billing agreement (RUB, active flag, raw agreement object). | Баланс по договору: сумма в рублях, признак активности |
| `billing_consumption` | `evo billing consumption` | нет | `start` | Usage/consumption cost for a date range, by the agreement (CLOUDRU_AGREEMENT_ID) or, without it, by the project; scope in the answer says which. | Стоимость потребления за период по сервисам: по договору или по проекту |
| `billing_finops` | `evo billing finops` | нет | — | FinOps report (read-only): INFORM (cost allocation by service, anomalies vs the previous same-length period) + QUANTIFY (daily burn-rate, balance runway in … | Отчёт FinOps: распределение затрат, аномалии, темп расходов, запас баланса |

## ca

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `ca_list` | `evo ca list` | нет | — | List Container Apps (serverless containers) in the project (name, status). | Приложения Container Apps проекта |
| `ca_get` | `evo ca get` | нет | `name` | Get a Container App by name — full details incl. | Приложение Container Apps по имени: состояние, публичный URL |
| `ca_deploy` | `evo ca deploy` | да | `name`, `image` | Deploy (create+run) a Container App from a container image — serverless, scales to zero when idle. | Развернуть приложение Container Apps из образа контейнера (асинхронно) |
| `ca_delete` | `evo ca delete` | удаляет | `name` | Delete a Container App by name. | Удалить приложение Container Apps (асинхронно) |
| `ca_logs` | `evo ca logs` | нет | `name` | Get recent logs of a Container App: the last 100 lines by time, oldest first (app logs are the container's stdout/stderr). | Последние журналы приложения Container Apps |
| `ca_start` | `evo ca start` | да | `name` | Start a stopped Container App (it is billed while running). | Запустить остановленное приложение Container Apps |
| `ca_stop` | `evo ca stop` | да | `name` | Stop a running Container App (it stops serving requests). | Остановить приложение Container Apps |

## cert

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `cert_list` | `evo cert list` | нет | — | Certificate Manager — list TLS certificates in the project (read-only). | TLS-сертификаты проекта в Certificate Manager |
| `cert_get` | `evo cert get` | нет | `id` | Certificate Manager — get one certificate by id (metadata, primary version, expiry, status). | Сертификат по ID: метаданные, основная версия, срок действия, состояние |
| `cert_issue_le` | `evo cert issue-le` | да | `name`, `dns_names` | Certificate Manager — issue a FREE Let's Encrypt TLS certificate for one or more domains (async). | Выпустить бесплатный сертификат Let's Encrypt (асинхронно) |
| `cert_import` | `evo cert import` | да | `name`, `server_certificate`, `private_key` | Certificate Manager — import a user-managed certificate (bring-your-own). | Загрузить в Certificate Manager свой сертификат и закрытый ключ |
| `cert_renew` | `evo cert renew` | да | `certificate_id` | Certificate Manager — trigger Let's Encrypt renewal for a certificate (async). | Перевыпустить сертификат Let's Encrypt (асинхронно) |
| `cert_delete` | `evo cert delete` | удаляет | `id` | Certificate Manager — permanently delete a certificate. | Удалить сертификат (безвозвратно) |
| `cert_usage` | `evo cert usage` | нет | `id` | Certificate Manager — list resources (load balancers/listeners) that USE a certificate. | Ресурсы, которые используют сертификат |
| `cert_versions` | `evo cert versions` | нет | `certificate_id` | Certificate Manager — list a certificate's versions (id, CN, DNS names, status, expiry), all pages. | Версии сертификата |
| `cert_private_key` | `evo cert private-key` | нет, секрет | `certificate_id` | Certificate Manager — return the SECRET private key for a version: privateKey (private_key in the live API) is PEM text (-----BEGIN …), decoded from the API's … | Закрытый ключ версии сертификата — открытым текстом |
| `cert_operation` | `evo cert operation` | нет | `operation_id` | Certificate Manager — poll an async operation (issue/renew) by id until done=true. | Состояние асинхронной операции Certificate Manager |

## deploy

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `deploy` | `evo deploy` | да | — | Deploy the local docker-compose project to a Cloud.ru VM. | Развернуть локальный проект docker-compose на ВМ Cloud.ru |
| `status` | `evo status` | нет | — | Show the current deployment status, including VM ID, public IP, flavor, region, health state and exposed ports, from the local state. | Состояние текущего развёртывания |
| `list` | `evo deploy list` | нет | — | List all recorded deployments for this project (VM IP, status, flavor, creation time). | Локальные записи развёртываний этого проекта |
| `logs` | `evo logs` | нет | — | Retrieve container logs from the deployed VM via 'docker compose logs' over SSH. | Журналы контейнеров развёрнутой ВМ |
| `destroy` | `evo vm delete` | удаляет | — | Permanently delete a VM and all data on it (irreversible). | Удалить ВМ и все данные на ней (безвозвратно) |
| `deploy_plan` | `evo deploy plan` | нет | — | STEP 1 of the deploy wizard (read-only, creates nothing). | Шаг 1 мастера развёртывания: рассчитать ВМ и её цену, ничего не создавая |
| `deploy_apply` | `evo deploy apply` | да | `plan_id` | STEP 2 of the deploy wizard. | Шаг 2 мастера развёртывания: выполнить рассчитанный план |
| `open_ports` | `evo vpc open-ports` | да | `security_group`, `ports` | Open inbound ports on a Cloud.ru security group (add ingress rules, tcp, from 0.0.0.0/0 by default). | Открыть входящие порты в группе безопасности |

## disk

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `list_disk_types` | `evo disk types` | нет | — | List available Cloud.ru disk types (UUID + name, e.g. | Типы дисков (UUID и имя, например SSD или HDD) |
| `disk_list` | `evo disk list` | нет | — | List block-storage disks (id, name, size_gb, state, disk_type, zone, attached VMs). | Диски: id, имя, размер, состояние, тип, зона, к каким ВМ подключены |
| `disk_create` | `evo disk create` | да | `name`, `size_gb` | Create a block-storage disk. | Создать диск (с --attach-to — в зоне ВМ и сразу подключить) |
| `disk_attach` | `evo disk attach` | да | `disk_id`, `vm` | Attach an existing disk to a VM (POST /disks/{id}/attach). | Подключить диск к ВМ (диск и ВМ должны быть в одной зоне) |
| `disk_detach` | `evo disk detach` | да | `disk_id`, `vm` | Detach a disk from a VM (POST /disks/{id}/detach). | Отключить диск от ВМ |
| `disk_resize` | `evo disk resize` | да | `disk_id`, `size_gb` | Grow a disk in place via PUT /disks/{id} (up to 16384 GB). | Увеличить диск (только рост; затем расширьте раздел и файловую систему на ВМ) |
| `disk_delete` | `evo disk delete` | удаляет | `disk_id` | Permanently delete a disk and all its data (irreversible). | Удалить диск и все данные на нём (безвозвратно) |

## dns

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `dns_zones` | `evo dns zones` | нет | — | List Evolution DNS zones in the project (both public and private), with id, name, domain, visibility, state (and confirm_state of a public zone), valid … | Зоны DNS проекта: публичные и приватные |
| `dns_zone_get` | `evo dns zone-get` | нет | `id` | Get full details of one DNS zone by id (from dns_zones). | Зона DNS по ID: все сведения |
| `dns_zone_create` | `evo dns zone-create` | да | `domain` | Create a DNS zone. | Создать зону DNS: публичную или приватную |
| `dns_zone_delete` | `evo dns zone-delete` | удаляет | `id` | Delete a DNS zone by id (from dns_zones) AND its records (async). | Удалить зону DNS вместе с её записями (асинхронно) |
| `dns_operation` | `evo dns operation` | нет | `operation_id` | Poll a DNS async operation by id (from dns_zone_create/delete, dns_record_create/delete). | Состояние асинхронной операции DNS |
| `dns_records` | `evo dns records` | нет | `zone_id` | List resource records of a DNS zone (id, name, type, values, ttl). | Записи зоны DNS |
| `dns_record_get` | `evo dns record-get` | нет | `record_id` | Get one DNS record by id (from dns_records). | Запись DNS по ID |
| `dns_record_create` | `evo dns record-create` | да | `zone_id`, `name`, `record_type`, `value` или `values` | Add a DNS record to a zone (async). | Добавить запись DNS в зону (асинхронно) |
| `dns_record_delete` | `evo dns record-delete` | удаляет | `record_id` | Delete a DNS record by id (from dns_records) (async). | Удалить запись DNS (асинхронно) |

## iam

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `iam_service_accounts` | `evo iam service-accounts` | нет | — | List IAM service accounts for a project (id, name, email, type, enabled). | Сервисные аккаунты IAM проекта |

## image

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `image_list` | `evo image list` | нет | — | Search the VM image catalogue: OS images (type public), Cloud.ru Marketplace products (marketplace) and the project's own images (private). | Поиск образов: ОС (public), маркетплейс (marketplace), образы проекта (private) |
| `marketplace_list` | `evo marketplace list` | нет | — | Read-only showcase of what Cloud.ru offers to deploy, in one list: VM images of the shared catalogue (OS images and Cloud.ru Marketplace products; the … | Витрина: ОС, продукты маркетплейса и плагины Kubernetes одним списком, с командой развёртывания |
| `list_images` | `evo list-images` | нет | — | List available Cloud.ru VM boot images (UUID + name). | Загрузочные образы ВМ (UUID и имя) |
| `image_create` | `evo image create` | да | `name`, `disk_id` | Create a reusable VM image FROM A BOOTABLE DISK — the final step of producing a distributable image (e.g. | Создать образ ВМ из загрузочного диска (платно, асинхронно) |
| `image_get` | `evo image get` | нет | `image` или `image_id` | Get one VM image by name, display name or UUID (resolved like image_list: exact name, exact display_name, then a unique substring; several matches → USAGE with … | Образ по имени или UUID: минимум ресурсов, зоны, поля и кто их заполнит, команда создания ВМ |
| `image_delete` | `evo image delete` | удаляет | `image_id` | Delete a VM image. | Удалить образ ВМ |

## k8s

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `k8s_list` | `evo k8s list` | нет | — | List Managed Kubernetes (mk8s) clusters in the project (id, name, status). | Кластеры Managed Kubernetes проекта |
| `k8s_get` | `evo k8s get` | нет | `cluster_id` | Get a Managed Kubernetes cluster by id (full details incl. | Кластер Managed Kubernetes по ID |
| `k8s_options` | `evo k8s options` | нет | — | List available Kubernetes versions (by release channel), node flavors and zones for mk8s. | Версии Kubernetes, flavor узлов и зоны для кластеров |
| `k8s_plan` | `evo k8s plan` | нет | — | STEP 1 for Managed Kubernetes: resolve a cluster config (k8s version, control-plane flavor, master count, subnet, service account) WITHOUT creating anything. | Шаг 1: рассчитать конфигурацию кластера, ничего не создавая |
| `k8s_create` | `evo k8s create` | да | — | STEP 2 for Managed Kubernetes: create the cluster resolved by k8s_plan. | Шаг 2: создать кластер по расчёту k8s plan (платно, асинхронно, около 10 минут) |
| `k8s_kubeconfig` | `evo k8s kubeconfig` | нет | `cluster_id` | Get the kubeconfig for a Managed Kubernetes cluster. | kubeconfig кластера (по умолчанию со встроенным токеном примерно на час) |
| `k8s_delete` | `evo k8s delete` | удаляет | `cluster_id` | Permanently delete a Managed Kubernetes cluster. | Удалить кластер Managed Kubernetes (асинхронно) |
| `k8s_nodepools` | `evo k8s nodepools` | нет | `cluster_id` | List node pools of one Managed Kubernetes cluster (name, state, desired/ready nodes). | Группы узлов кластера |
| `k8s_add_nodepool` | `evo k8s add-nodepool` | да | `cluster_id` | Add a worker node pool to a Managed Kubernetes cluster (workers to run pods — a fresh cluster has none). | Добавить группу рабочих узлов (платно, асинхронно) |
| `k8s_addons` | `evo k8s addons` | нет | — | Search the Managed Kubernetes addon catalogue (Cloud.ru-packaged plugins: ingress, cert-manager, monitoring, CSI drivers, …). | Каталог плагинов Kubernetes: поиск по имени и описанию, категория, платность |
| `k8s_addon_versions` | `evo k8s addon-versions` | нет | `name` | List the releases of one Managed Kubernetes addon, newest first: version (appVersion, what k8s_add_addon takes), addon_version, mode (ACTIVE, EXPERIMENTAL … | Версии плагина: режим поддержки, требования к кластеру, схема настроек |
| `k8s_add_addon` | `evo k8s add-addon` | удаляет | `cluster_id`, `name` | Install a Managed Kubernetes addon release into a cluster. | Установить плагин в кластер (асинхронно; версия по умолчанию — последняя активная) |
| `k8s_kubectl` | `evo k8s kubectl` | удаляет | `cluster_id`, `args` | Run ANY kubectl command against a cluster (auto-embedded token — no cloudlogin needed). | Команда kubectl в кластере (изменяющие — с подтверждением) |
| `k8s_apply` | `evo k8s apply` | удаляет | `cluster_id`, `manifest` | Apply a Kubernetes manifest (YAML/JSON, may be multi-document) to a cluster — like `kubectl apply -f -`. | Применить манифест Kubernetes (как kubectl apply -f -) |
| `k8s_helm` | `evo k8s helm` | удаляет | `cluster_id`, `action` | Drive Helm on a cluster: action=install\|upgrade\|uninstall\|list\|status. | Helm в кластере: install, upgrade, uninstall, list, status |

## kafka

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `kafka_list` | `evo kafka list` | нет | — | List Managed Kafka clusters (id, name, status, brokers, version). | Кластеры Managed Kafka проекта |
| `kafka_get` | `evo kafka get` | нет | `cluster_id` | Get a Managed Kafka cluster by id (status, kafka_hosts, brokers, version). | Кластер Kafka по ID: состояние, адреса, брокеры, версия |
| `kafka_versions` | `evo kafka versions` | нет | — | List available Managed Kafka versions (id + name). | Доступные версии Managed Kafka |
| `kafka_specs` | `evo kafka specs` | нет | — | List Managed Kafka flavor specifications (id, cpu, memory_gb, min_storage_gb, min/max_brokers) for a version. | Конфигурации кластера Kafka для версии: CPU, память, хранилище, брокеры |
| `kafka_create` | `evo kafka create` | да | `name` | Create a Managed Kafka cluster. | Создать кластер Managed Kafka (платно, асинхронно) |
| `kafka_delete` | `evo kafka delete` | удаляет | `cluster_id` | Delete a Managed Kafka cluster by id AND its data. | Удалить кластер Kafka вместе с данными (асинхронно) |
| `kafka_operation` | `evo kafka operation` | нет | `operation_id` | Poll a Managed Kafka async operation by id (from kafka_create/kafka_delete). | Состояние асинхронной операции Kafka |

## monitoring

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `metrics_history` | `evo metrics history` | нет | `match` | Service Monitoring — read a metric's history over a recent window (read-only). | История метрики за недавний период (Service Monitoring) |
| `metrics_scrape` | `evo metrics scrape` | нет | — | Service Monitoring — current metric values in Prometheus text exposition (read-only snapshot of all/matched metrics right now). | Текущие значения метрик в текстовом формате Prometheus |

## nlb

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `lb_list` | `evo lb list` | нет | — | List Evolution Load Balancers (L4/network) in the project (id, name, status, vpc, external_ip). | Балансировщики нагрузки проекта (L4) |
| `lb_get` | `evo lb get` | нет | `id` | Get full details of one load balancer by id (from lb_list): rules, listeners, addresses, AZs, status. | Балансировщик по ID: правила, обработчики, адреса, зоны |
| `lb_operation` | `evo lb operation` | нет | `operation_id` | Poll a Load Balancer async operation by id (from lb_create/lb_delete/tg_create/tg_delete). | Состояние асинхронной операции балансировщика |
| `tg_list` | `evo tg list` | нет | — | List Load Balancer target groups in the project (id, name, status, vpc). | Группы целей балансировщиков проекта |
| `tg_get` | `evo tg get` | нет | `id` | Get details of one target group by id (from tg_list): member VMs, vpc, status. | Группа целей по ID: ВМ, VPC, состояние |
| `tg_create` | `evo tg create` | да | `name`, `vm_id` или `targets` | Create a target group (the set of VMs a load balancer distributes traffic to). | Создать группу целей балансировщика (асинхронно) |
| `tg_delete` | `evo tg delete` | удаляет | `id` | Delete a target group by id (from tg_list). | Удалить группу целей (асинхронно) |
| `lb_create` | `evo lb create` | да | `name`, `target_group_id`, `port` | Create an L4 load balancer fronting a target group. | Создать балансировщик L4 перед группой целей (платно, асинхронно) |
| `lb_delete` | `evo lb delete` | удаляет | `id` | Delete a load balancer by id (from lb_list). | Удалить балансировщик (асинхронно) |

## org

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `org_projects` | `evo org projects` | нет | — | List Cloud.ru projects visible to the caller (id, name, status, org unit) and the active project (active). | Проекты Cloud.ru, которые видит ключ доступа |
| `org_quotas` | `evo org quotas` | нет | — | List quota usages (resource, limit, current usage) — for capacity planning. | Квоты: ресурс, лимит, текущее использование |

## pg

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `pg_list` | `evo pg list` | нет | — | List Managed PostgreSQL clusters in the project (id, name, status, version, health). | Кластеры Managed PostgreSQL проекта |
| `pg_get` | `evo pg get` | нет | `id` или `cluster_id` | Get full details of one Managed PostgreSQL cluster by id (from pg_list): version, instances, storage, connection_string, subnets, status. | Кластер PostgreSQL по ID: версия, хранилище, строка подключения, состояние |
| `pg_versions` | `evo pg versions` | нет | — | List available Managed PostgreSQL versions (e.g. | Доступные версии PostgreSQL |
| `pg_specs` | `evo pg specs` | нет | — | List Managed PostgreSQL specifications (id, cpu, memory_gb, deployment_mode standard\|business, min_storage_gb) for a version. | Конфигурации кластера PostgreSQL для версии: CPU, память, минимальный объём |
| `pg_create` | `evo pg create` | да | `name` | Create a Managed PostgreSQL cluster. | Создать кластер PostgreSQL (платно, асинхронно) |
| `pg_delete` | `evo pg delete` | удаляет | `id` или `cluster_id` | Delete a Managed PostgreSQL cluster by id (from pg_list) AND its data, including all its backups (they cannot be restored). | Удалить кластер PostgreSQL вместе с данными и всеми резервными копиями (асинхронно) |
| `pg_operation` | `evo pg operation` | нет | `operation_id` | Poll a Managed PostgreSQL async operation by id (returned by pg_create/pg_delete/pg_backup_create). | Состояние асинхронной операции PostgreSQL |
| `pg_databases` | `evo pg databases` | нет | `cluster_id` | List databases in a PostgreSQL cluster (name, owner, size). | Базы данных кластера |
| `pg_db_create` | `evo pg db-create` | да | `cluster_id`, `name`, `owner` | Create a database in a PostgreSQL cluster. | Создать базу данных |
| `pg_db_delete` | `evo pg db-delete` | удаляет | `cluster_id`, `name` | Drop a database in a PostgreSQL cluster. | Удалить базу данных |
| `pg_users` | `evo pg users` | нет | `cluster_id` | List users of a PostgreSQL cluster (name, granted roles). | Пользователи кластера и их роли |
| `pg_user_create` | `evo pg user-create` | да | `cluster_id`, `name`, `password` | Create a user in a PostgreSQL cluster. | Создать пользователя (пароль не выводится) |
| `pg_user_delete` | `evo pg user-delete` | удаляет | `cluster_id`, `name` | Delete a user in a PostgreSQL cluster. | Удалить пользователя |
| `pg_backups` | `evo pg backups` | нет | `cluster_id` | List backups of a PostgreSQL cluster (name, status, size, timestamps). | Резервные копии кластера |
| `pg_backup_create` | `evo pg backup-create` | да | `cluster_id`, `name` | Start a manual backup of a PostgreSQL cluster (async). | Создать резервную копию вручную (асинхронно) |

## redis

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `redis_list` | `evo redis list` | нет | — | List Managed Redis clusters (id, name, status, type, shards, replicas). | Кластеры Managed Redis проекта |
| `redis_get` | `evo redis get` | нет | `cluster_id` | Get a Managed Redis cluster by id (status, redis_hosts, type, shards, replicas). | Кластер Redis по ID: состояние, адреса, тип, шарды, реплики |
| `redis_versions` | `evo redis versions` | нет | — | List available Managed Redis versions (id + name). | Доступные версии Managed Redis |
| `redis_specs` | `evo redis specs` | нет | — | List Managed Redis flavor specifications (id, cpu, memory_gb, deployment_mode, min_storage_gb) for a version. | Конфигурации кластера Redis для версии: CPU, память, режим, хранилище |
| `redis_create` | `evo redis create` | да | `name` | Create a Managed Redis cluster. | Создать кластер Managed Redis (платно, асинхронно) |
| `redis_delete` | `evo redis delete` | удаляет | `cluster_id` | Delete a Managed Redis cluster by id AND its data. | Удалить кластер Redis вместе с данными (асинхронно) |
| `redis_operation` | `evo redis operation` | нет | `operation_id` | Poll a Managed Redis async operation by id (from redis_create/redis_delete). | Состояние асинхронной операции Redis |

## s3

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `s3_buckets` | `evo s3 buckets` | нет | — | List Object Storage (S3) buckets. | Бакеты Object Storage |
| `s3_ls` | `evo s3 ls` | нет | `bucket` | List objects in an S3 bucket (key, size, last_modified). | Объекты бакета: ключ, размер, время изменения |
| `s3_get` | `evo s3 get` | нет | `bucket`, `key` | Download an S3 object up to 10 MiB (a larger one is refused with USAGE: use an S3 client or the console). | Скачать объект до 10 МиБ (небольшой текст — как есть, остальное — в base64; больший объект — через S3-клиент или консоль) |
| `s3_put` | `evo s3 put` | удаляет | `bucket`, `key` | Upload an S3 object from inline content (content) or base64 (content_base64). | Загрузить объект (существующий ключ перезаписывается) |
| `s3_rm` | `evo s3 rm` | удаляет | `bucket`, `key` | Delete an S3 object. | Удалить объект |
| `s3_bucket_create` | `evo s3 bucket-create` | да | `bucket` | Create an Object Storage (S3) bucket. | Создать бакет |
| `s3_bucket_delete` | `evo s3 bucket-delete` | удаляет | `bucket` | Delete an EMPTY S3 bucket. | Удалить пустой бакет |

## secret

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `secret_list` | `evo secret list` | нет | — | List secrets in the project (id, name, path, description) from Cloud.ru Secret Management. | Секреты проекта (без значений) |
| `secret_get` | `evo secret get` | нет | `path` | Get one secret's metadata by path (from secret_list): folder, labels, versions, KMS key. | Метаданные секрета: папка, метки, версии, ключ KMS (без значения) |
| `secret_value` | `evo secret value` | нет, секрет | `path` | Retrieve the DECRYPTED value of a secret by path. | Расшифрованное значение секрета |
| `secret_create` | `evo secret create` | да | `path`, `value` | Store a new secret at a path (value is Base64-encoded internally; never echoed back). | Создать секрет |
| `secret_add_version` | `evo secret add-version` | да | `path`, `value` | Add a new value version to an existing secret (rotation). | Добавить новую версию значения секрета (ротация) |
| `secret_delete` | `evo secret delete` | удаляет | `path` | Delete a secret by path AND all its versions. | Удалить секрет со всеми версиями |

## snat

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `snat_list` | `evo snat list` | нет | — | List SNAT gateways in the project. | SNAT-шлюзы проекта |
| `snat_create` | `evo snat create` | да | `name` | Create a public SNAT gateway: outbound internet for every ROUTED subnet of its zone (an isolated subnet is not served; a VM with its own public IP goes out … | Создать публичный SNAT-шлюз: исходящий интернет для подсетей зоны (платно) |
| `snat_delete` | `evo snat delete` | удаляет | `id` | Delete a SNAT gateway by id (from snat_list). | Удалить SNAT-шлюз (узлы зоны теряют исходящий интернет) |

## stack

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `stack_plan` | `evo stack plan` | нет | `manifest` | Plan a multi-resource STACK from ONE declarative manifest. | Спланировать стек ресурсов по одному манифесту (ничего не создаёт) |
| `stack_apply` | `evo stack apply` | да | `job_id` | Apply a planned stack (from stack_plan). | Создать ресурсы спланированного стека (платно) |
| `stack_status` | `evo stack status` | нет | `job_id` | Poll a stack deploy: returns COMPACT progress {status, done[], running[], pending[], outputs{postgres,vm,tg,lb,…; lb_external_ip once the balancer has a public … | Ход развёртывания стека |
| `stack_destroy` | `evo stack destroy` | удаляет | `job_id` | Tear down every resource in a stack (reverse order, idempotent). | Удалить все ресурсы стека (безвозвратно) |

## vm

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `provision` | `evo vm create` | да | — | Provision a bare Cloud.ru VM. | Создать ВМ (по умолчанию с floating IP; без внешнего адреса — --no-public-ip) |
| `vm_get` | `evo vm get` | нет | `vm_id` или `name` | Get LIVE details for one VM by vm_id or name: its PRIVATE IP (private_ip), any public/floating IP (public_ip), state, and every network interface. | ВМ по ID или имени: внутренний и внешний IP, состояние, интерфейсы |
| `vm_list` | `evo vm list` | нет | — | List the project's VMs from the LIVE API: id, name, state, availability zone and creation time. | Список ВМ проекта: id, имя, состояние, зона, время создания |
| `list_zones` | `evo vm zones` | нет | — | List available Cloud.ru availability zones (UUID + name). | Зоны доступности (UUID и имя) |
| `list_flavors` | `evo vm flavors` | нет | — | List available Cloud.ru VM flavors (name, vCPU, RAM, free-tier flag) with a monthly cost estimate each. | Flavor ВМ: vCPU, RAM, бесплатный уровень, оценка цены за месяц |

## vpc

| Инструмент | Команда CLI | Меняет | Обязательные аргументы | Описание | Описание (RU) |
|---|---|---|---|---|---|
| `fip_attach` | `evo vpc fip-attach` | да | `fip`, `vm` | Bind an existing FREE floating IP to a VM's primary interface, giving it a public address. | Привязать свободный floating IP к основному интерфейсу ВМ |
| `list_subnets` | `evo vpc subnets` | нет | — | List Cloud.ru subnets (UUID, name, availability zone). | Подсети: UUID, имя, зона доступности |
| `list_security_groups` | `evo vpc security-groups` | нет | — | List Cloud.ru security groups (UUID, name, availability zone). | Группы безопасности (с --rules — вместе с правилами) |
| `list_floating_ips` | `evo vpc fips` | нет | — | List Cloud.ru floating IPs (address, state, whether available to attach). | Floating IP: адрес, состояние, свободен ли |
| `release_floating_ip` | `evo vpc fip-release` | удаляет | `id` или `ip_address` | Release (delete) a floating IP, freeing it from the project. | Освободить floating IP (адрес, занятый ВМ или SNAT-шлюзом, не освобождается) |
| `vpc_list` | `evo vpc list` | нет | — | List Evolution VPCs (virtual private clouds) in the project (id, name, default, type). | Список VPC проекта |
| `vpc_get` | `evo vpc get` | нет | `id` | Get full details of one VPC by id (from vpc_list). | VPC по ID: все сведения |
| `vpc_create` | `evo vpc create` | да | `name` | Create a new VPC (virtual private cloud) (async). | Создать VPC (асинхронно) |
| `vpc_delete` | `evo vpc delete` | удаляет | `id` | Delete a VPC by id (from vpc_list) (async). | Удалить VPC (асинхронно; сначала удалите его подсети и ресурсы) |
| `vpc_operation` | `evo vpc operation` | нет | `operation_id` | Poll a VPC async operation by id (from vpc_create/delete, vpc_route_create/delete). | Состояние асинхронной операции VPC |
| `vpc_routes` | `evo vpc routes` | нет | `vpc_id` | List static routes of a VPC (id, subnet, next_hop_type, status). | Статические маршруты VPC |
| `vpc_route_create` | `evo vpc route-create` | да | `vpc_id`, `subnet`, `next_hop_vip_id` или `next_hop_vm_id` или `next_hop_magic_router_id` | Add a static route to a VPC (async). | Добавить статический маршрут в VPC (асинхронно) |
| `vpc_route_delete` | `evo vpc route-delete` | удаляет | `vpc_id`, `route_id` | Delete a static route from a VPC by id (from vpc_routes) (async). | Удалить статический маршрут из VPC (асинхронно) |
