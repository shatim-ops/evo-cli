# Справка по командам

<!-- Файл сгенерирован из дерева команд (cobra/doc). Руками не править:
     go test ./internal/cli -run TestCommandsDoc -update-docs
     CI сверяет файл с бинарём (TestCommandsDoc). -->

То же самое печатает `evo <команда> --help`; с `EVO_LANG=en` справка
английская (решение D6, пересмотр). Заголовки разделов (Synopsis, Options)
ставит генератор cobra/doc. Описания команд, флагов и инструментов
(`evo tools describe`) — русские; в MCP описания инструментов английские: их читает
агент. Подробнее — в [README](../README.md) и соседних файлах `docs/`.

## Содержание

- [evo](#evo)
  - [evo ar](#evo-ar)
    - [evo ar create](#evo-ar-create)
    - [evo ar delete](#evo-ar-delete)
    - [evo ar get](#evo-ar-get)
    - [evo ar list](#evo-ar-list)
    - [evo ar operation](#evo-ar-operation)
  - [evo audit](#evo-audit)
    - [evo audit events](#evo-audit-events)
  - [evo billing](#evo-billing)
    - [evo billing balance](#evo-billing-balance)
    - [evo billing consumption](#evo-billing-consumption)
    - [evo billing finops](#evo-billing-finops)
  - [evo ca](#evo-ca)
    - [evo ca delete](#evo-ca-delete)
    - [evo ca deploy](#evo-ca-deploy)
    - [evo ca get](#evo-ca-get)
    - [evo ca list](#evo-ca-list)
    - [evo ca logs](#evo-ca-logs)
    - [evo ca start](#evo-ca-start)
    - [evo ca stop](#evo-ca-stop)
  - [evo cert](#evo-cert)
    - [evo cert delete](#evo-cert-delete)
    - [evo cert get](#evo-cert-get)
    - [evo cert import](#evo-cert-import)
    - [evo cert issue-le](#evo-cert-issue-le)
    - [evo cert list](#evo-cert-list)
    - [evo cert operation](#evo-cert-operation)
    - [evo cert private-key](#evo-cert-private-key)
    - [evo cert renew](#evo-cert-renew)
    - [evo cert usage](#evo-cert-usage)
    - [evo cert versions](#evo-cert-versions)
  - [evo completion](#evo-completion)
    - [evo completion bash](#evo-completion-bash)
    - [evo completion fish](#evo-completion-fish)
    - [evo completion powershell](#evo-completion-powershell)
    - [evo completion zsh](#evo-completion-zsh)
  - [evo configure](#evo-configure)
    - [evo configure delete](#evo-configure-delete)
    - [evo configure list](#evo-configure-list)
  - [evo deploy](#evo-deploy)
    - [evo deploy apply](#evo-deploy-apply)
    - [evo deploy list](#evo-deploy-list)
    - [evo deploy plan](#evo-deploy-plan)
  - [evo destroy](#evo-destroy)
  - [evo disk](#evo-disk)
    - [evo disk attach](#evo-disk-attach)
    - [evo disk create](#evo-disk-create)
    - [evo disk delete](#evo-disk-delete)
    - [evo disk detach](#evo-disk-detach)
    - [evo disk list](#evo-disk-list)
    - [evo disk resize](#evo-disk-resize)
    - [evo disk types](#evo-disk-types)
  - [evo dns](#evo-dns)
    - [evo dns operation](#evo-dns-operation)
    - [evo dns record-create](#evo-dns-record-create)
    - [evo dns record-delete](#evo-dns-record-delete)
    - [evo dns record-get](#evo-dns-record-get)
    - [evo dns records](#evo-dns-records)
    - [evo dns zone-create](#evo-dns-zone-create)
    - [evo dns zone-delete](#evo-dns-zone-delete)
    - [evo dns zone-get](#evo-dns-zone-get)
    - [evo dns zones](#evo-dns-zones)
  - [evo doctor](#evo-doctor)
  - [evo iam](#evo-iam)
    - [evo iam service-accounts](#evo-iam-service-accounts)
  - [evo image](#evo-image)
    - [evo image create](#evo-image-create)
    - [evo image delete](#evo-image-delete)
    - [evo image get](#evo-image-get)
    - [evo image list](#evo-image-list)
  - [evo init](#evo-init)
  - [evo k8s](#evo-k8s)
    - [evo k8s add-addon](#evo-k8s-add-addon)
    - [evo k8s add-nodepool](#evo-k8s-add-nodepool)
    - [evo k8s addon-versions](#evo-k8s-addon-versions)
    - [evo k8s addons](#evo-k8s-addons)
    - [evo k8s apply](#evo-k8s-apply)
    - [evo k8s create](#evo-k8s-create)
    - [evo k8s delete](#evo-k8s-delete)
    - [evo k8s get](#evo-k8s-get)
    - [evo k8s helm](#evo-k8s-helm)
    - [evo k8s kubeconfig](#evo-k8s-kubeconfig)
    - [evo k8s kubectl](#evo-k8s-kubectl)
    - [evo k8s list](#evo-k8s-list)
    - [evo k8s nodepools](#evo-k8s-nodepools)
    - [evo k8s options](#evo-k8s-options)
    - [evo k8s plan](#evo-k8s-plan)
  - [evo kafka](#evo-kafka)
    - [evo kafka create](#evo-kafka-create)
    - [evo kafka delete](#evo-kafka-delete)
    - [evo kafka get](#evo-kafka-get)
    - [evo kafka list](#evo-kafka-list)
    - [evo kafka operation](#evo-kafka-operation)
    - [evo kafka specs](#evo-kafka-specs)
    - [evo kafka versions](#evo-kafka-versions)
  - [evo lb](#evo-lb)
    - [evo lb create](#evo-lb-create)
    - [evo lb delete](#evo-lb-delete)
    - [evo lb get](#evo-lb-get)
    - [evo lb list](#evo-lb-list)
    - [evo lb operation](#evo-lb-operation)
  - [evo logs](#evo-logs)
  - [evo marketplace](#evo-marketplace)
    - [evo marketplace list](#evo-marketplace-list)
  - [evo mcp](#evo-mcp)
  - [evo metrics](#evo-metrics)
    - [evo metrics history](#evo-metrics-history)
    - [evo metrics scrape](#evo-metrics-scrape)
  - [evo org](#evo-org)
    - [evo org projects](#evo-org-projects)
    - [evo org quotas](#evo-org-quotas)
  - [evo pg](#evo-pg)
    - [evo pg backup-create](#evo-pg-backup-create)
    - [evo pg backups](#evo-pg-backups)
    - [evo pg create](#evo-pg-create)
    - [evo pg databases](#evo-pg-databases)
    - [evo pg db-create](#evo-pg-db-create)
    - [evo pg db-delete](#evo-pg-db-delete)
    - [evo pg delete](#evo-pg-delete)
    - [evo pg get](#evo-pg-get)
    - [evo pg list](#evo-pg-list)
    - [evo pg operation](#evo-pg-operation)
    - [evo pg specs](#evo-pg-specs)
    - [evo pg user-create](#evo-pg-user-create)
    - [evo pg user-delete](#evo-pg-user-delete)
    - [evo pg users](#evo-pg-users)
    - [evo pg versions](#evo-pg-versions)
  - [evo project](#evo-project)
    - [evo project list](#evo-project-list)
    - [evo project use](#evo-project-use)
  - [evo redis](#evo-redis)
    - [evo redis create](#evo-redis-create)
    - [evo redis delete](#evo-redis-delete)
    - [evo redis get](#evo-redis-get)
    - [evo redis list](#evo-redis-list)
    - [evo redis operation](#evo-redis-operation)
    - [evo redis specs](#evo-redis-specs)
    - [evo redis versions](#evo-redis-versions)
  - [evo s3](#evo-s3)
    - [evo s3 bucket-create](#evo-s3-bucket-create)
    - [evo s3 bucket-delete](#evo-s3-bucket-delete)
    - [evo s3 buckets](#evo-s3-buckets)
    - [evo s3 get](#evo-s3-get)
    - [evo s3 ls](#evo-s3-ls)
    - [evo s3 put](#evo-s3-put)
    - [evo s3 rm](#evo-s3-rm)
  - [evo secret](#evo-secret)
    - [evo secret add-version](#evo-secret-add-version)
    - [evo secret create](#evo-secret-create)
    - [evo secret delete](#evo-secret-delete)
    - [evo secret get](#evo-secret-get)
    - [evo secret list](#evo-secret-list)
    - [evo secret value](#evo-secret-value)
  - [evo snat](#evo-snat)
    - [evo snat create](#evo-snat-create)
    - [evo snat delete](#evo-snat-delete)
    - [evo snat list](#evo-snat-list)
  - [evo stack](#evo-stack)
    - [evo stack apply](#evo-stack-apply)
    - [evo stack destroy](#evo-stack-destroy)
    - [evo stack plan](#evo-stack-plan)
    - [evo stack status](#evo-stack-status)
  - [evo status](#evo-status)
  - [evo tg](#evo-tg)
    - [evo tg create](#evo-tg-create)
    - [evo tg delete](#evo-tg-delete)
    - [evo tg get](#evo-tg-get)
    - [evo tg list](#evo-tg-list)
  - [evo tools](#evo-tools)
    - [evo tools call](#evo-tools-call)
    - [evo tools describe](#evo-tools-describe)
    - [evo tools list](#evo-tools-list)
  - [evo verify](#evo-verify)
  - [evo version](#evo-version)
  - [evo vm](#evo-vm)
    - [evo vm create](#evo-vm-create)
    - [evo vm delete](#evo-vm-delete)
    - [evo vm flavors](#evo-vm-flavors)
    - [evo vm get](#evo-vm-get)
    - [evo vm list](#evo-vm-list)
    - [evo vm zones](#evo-vm-zones)
  - [evo vpc](#evo-vpc)
    - [evo vpc create](#evo-vpc-create)
    - [evo vpc delete](#evo-vpc-delete)
    - [evo vpc fip-attach](#evo-vpc-fip-attach)
    - [evo vpc fip-release](#evo-vpc-fip-release)
    - [evo vpc fips](#evo-vpc-fips)
    - [evo vpc get](#evo-vpc-get)
    - [evo vpc list](#evo-vpc-list)
    - [evo vpc open-ports](#evo-vpc-open-ports)
    - [evo vpc operation](#evo-vpc-operation)
    - [evo vpc route-create](#evo-vpc-route-create)
    - [evo vpc route-delete](#evo-vpc-route-delete)
    - [evo vpc routes](#evo-vpc-routes)
    - [evo vpc security-groups](#evo-vpc-security-groups)
    - [evo vpc subnets](#evo-vpc-subnets)
  - [evo whoami](#evo-whoami)

## evo

Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов

### Synopsis

```text
Cloud.ru Evolution CLI CREW edition (evo)

CLI и MCP-сервер для Cloud.ru Evolution: развёртывание docker-compose проектов
на ВМ и управление ресурсами облака: ВМ, диски, сети, Kubernetes, PostgreSQL,
Object Storage, секреты.

Сообщения по-английски: EVO_LANG=en.

Обратная связь: Telegram https://t.me/shatim, чат тестировщиков https://t.me/+SYvXziA4G9c1Y2Ni
```

### Options

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
  -h, --help                Справка по evo
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
      --version             Версия (как 'evo version')
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo ar](#evo-ar)	 - Artifact Registry: реестры образов и пакетов
* [evo audit](#evo-audit)	 - Журнал аудита: кто что сделал в проекте
* [evo billing](#evo-billing)	 - Биллинг: баланс, потребление, отчёт FinOps
* [evo ca](#evo-ca)	 - Container Apps: бессерверные контейнеры
* [evo cert](#evo-cert)	 - Certificate Manager: TLS-сертификаты
* [evo completion](#evo-completion)	 - Скрипт автодополнения для оболочки
* [evo configure](#evo-configure)	 - Ключ доступа и проект по умолчанию для профиля
* [evo deploy](#evo-deploy)	 - Развернуть docker-compose проект на ВМ Cloud.ru
* [evo destroy](#evo-destroy)	 - Удалить развёрнутую ВМ и связанные ресурсы
* [evo disk](#evo-disk)	 - Диски
* [evo dns](#evo-dns)	 - Evolution DNS: зоны и записи
* [evo doctor](#evo-doctor)	 - Проверить настройки, ключ доступа и связь с API
* [evo iam](#evo-iam)	 - IAM: сервисные аккаунты
* [evo image](#evo-image)	 - Образы ВМ: операционные системы и продукты маркетплейса
* [evo init](#evo-init)	 - Создать .evo.yaml с зоной и образом из API Cloud.ru
* [evo k8s](#evo-k8s)	 - Кластеры Managed Kubernetes
* [evo kafka](#evo-kafka)	 - Кластеры Managed Kafka
* [evo lb](#evo-lb)	 - Балансировщики нагрузки (L4)
* [evo logs](#evo-logs)	 - Журналы контейнеров на развёрнутой ВМ
* [evo marketplace](#evo-marketplace)	 - Витрина: образы ВМ и плагины Kubernetes с командой развёртывания каждого
* [evo mcp](#evo-mcp)	 - MCP-сервер для ИИ-агентов (stdio или Streamable HTTP с --http)
* [evo metrics](#evo-metrics)	 - Service Monitoring: метрики
* [evo org](#evo-org)	 - Организация: проекты и квоты
* [evo pg](#evo-pg)	 - Managed PostgreSQL: кластеры, базы данных, пользователи, резервные копии
* [evo project](#evo-project)	 - Проекты, которые видит ключ, и проект профиля
* [evo redis](#evo-redis)	 - Кластеры Managed Redis
* [evo s3](#evo-s3)	 - Object Storage: бакеты и объекты
* [evo secret](#evo-secret)	 - Secret Manager: секреты
* [evo snat](#evo-snat)	 - SNAT-шлюзы: исходящий интернет для приватных подсетей
* [evo stack](#evo-stack)	 - Стеки: несколько ресурсов по одному манифесту
* [evo status](#evo-status)	 - Состояние текущего развёртывания
* [evo tg](#evo-tg)	 - Группы целей балансировщиков
* [evo tools](#evo-tools)	 - Список, описание и вызов инструментов evo (тех же, что у MCP-сервера)
* [evo verify](#evo-verify)	 - Проверить, отвечает ли приложение (код 1 — не отвечает, 4 — ничего не развёрнуто)
* [evo version](#evo-version)	 - Версия, коммит, дата сборки, версия Go и платформа
* [evo vm](#evo-vm)	 - Виртуальные машины
* [evo vpc](#evo-vpc)	 - Сети: VPC, маршруты, подсети, группы безопасности, floating IP
* [evo whoami](#evo-whoami)	 - Активный профиль и проверка его ключа

## evo ar

Artifact Registry: реестры образов и пакетов

```
evo ar [flags]
```

### Options

```
  -h, --help   Справка по ar
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo ar create](#evo-ar-create)	 - Создать реестр Artifact Registry (платно, асинхронно)
* [evo ar delete](#evo-ar-delete)	 - Удалить реестр вместе с артефактами (асинхронно)
* [evo ar get](#evo-ar-get)	 - Реестр Artifact Registry по ID: все сведения
* [evo ar list](#evo-ar-list)	 - Реестры Artifact Registry проекта
* [evo ar operation](#evo-ar-operation)	 - Состояние асинхронной операции Artifact Registry

## evo ar create

Создать реестр Artifact Registry (платно, асинхронно)

### Synopsis

```text
Создать реестр Artifact Registry (платно, асинхронно).

Создать реестр Artifact Registry; имя, которое уже носит реестр проекта ('evo ar list'), отклоняется. Тип (--registry-type): DOCKER (по умолчанию), DEBIAN, RPM, GENERIC, PYPI или NPM. Реестр платный. Операция асинхронная: ход — 'evo ar operation <operation-id>'; затем образы загружают через docker login в <registry>.cr.cloud.ru.

Вызывает инструмент ar_create (подробнее: 'evo tools describe ar_create').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo ar create <name> [flags]
```

### Examples

```
  evo ar create images --dry-run
  evo ar create images --registry-type DOCKER --yes
```

### Options

```
  -h, --help                   Справка по create
      --public                 Сделать реестр публичным (по умолчанию false)
      --registry-type string   Тип реестра: DOCKER (по умолчанию), DEBIAN, RPM, GENERIC, PYPI, NPM
      --tariff string          Тариф: BASIC (по умолчанию) или PREMIUM
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo ar](#evo-ar)	 - Artifact Registry: реестры образов и пакетов

## evo ar delete

Удалить реестр вместе с артефактами (асинхронно)

### Synopsis

```text
Удалить реестр вместе с артефактами (асинхронно).

Удалить реестр Artifact Registry по id (список: 'evo ar list') вместе с его артефактами. Операция асинхронная.

Вызывает инструмент ar_delete (подробнее: 'evo tools describe ar_delete').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo ar delete <id> [flags]
```

### Examples

```
  evo ar delete <registry-id> --dry-run
  evo ar delete <registry-id> --yes
```

### Options

```
  -h, --help   Справка по delete
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo ar](#evo-ar)	 - Artifact Registry: реестры образов и пакетов

## evo ar get

Реестр Artifact Registry по ID: все сведения

### Synopsis

```text
Реестр Artifact Registry по ID: все сведения.

Все сведения об одном реестре Artifact Registry по id (список: 'evo ar list'). Только чтение.

Вызывает инструмент ar_get (подробнее: 'evo tools describe ar_get').
```

```
evo ar get <id> [flags]
```

### Examples

```
  evo ar get <registry-id>
```

### Options

```
  -h, --help   Справка по get
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo ar](#evo-ar)	 - Artifact Registry: реестры образов и пакетов

## evo ar list

Реестры Artifact Registry проекта

### Synopsis

```text
Реестры Artifact Registry проекта.

Реестры Artifact Registry проекта: id, имя, тип, состояние, публичный ли, тариф. Только чтение. Реестр хранит образы Docker и чарты Helm — источник образов для Container Apps и Managed Kubernetes там, где docker.io не подходит.

Вызывает инструмент ar_list (подробнее: 'evo tools describe ar_list').
```

```
evo ar list [flags]
```

### Examples

```
  evo ar list
  evo ar list --json | jq -r '.registries[] | "\(.id)\t\(.name)"'
```

### Options

```
  -h, --help   Справка по list
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo ar](#evo-ar)	 - Artifact Registry: реестры образов и пакетов

## evo ar operation

Состояние асинхронной операции Artifact Registry

### Synopsis

```text
Состояние асинхронной операции Artifact Registry.

Состояние асинхронной операции Artifact Registry по id — его возвращают создание и удаление реестра: done — завершена ли операция, error — ошибка. С --wait-seconds команда сама ждёт завершения до N секунд и выводит итоговое состояние. Только чтение.

Вызывает инструмент ar_operation (подробнее: 'evo tools describe ar_operation').
```

```
evo ar operation <operation-id> [flags]
```

### Examples

```
  evo ar operation <operation-id>
  evo ar operation <operation-id> --wait-seconds 120
```

### Options

```
  -h, --help               Справка по operation
      --wait-seconds int   Ждать итогового состояния до N секунд (не больше 300), опрашивая операцию с нарастающим интервалом. Без флага — одна проверка
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo ar](#evo-ar)	 - Artifact Registry: реестры образов и пакетов

## evo audit

Журнал аудита: кто что сделал в проекте

```
evo audit [flags]
```

### Options

```
  -h, --help   Справка по audit
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo audit events](#evo-audit-events)	 - Журнал аудита проекта: кто, что и когда сделал

## evo audit events

Журнал аудита проекта: кто, что и когда сделал

### Synopsis

```text
Журнал аудита проекта: кто, что и когда сделал.

Audit Logging — кто что сделал в проекте (только чтение): события управления (создание, удаление и изменение ВМ, баз данных, Kubernetes, S3, IAM и другого) с исполнителем, действием, ресурсом и временем. Отвечает на вопросы «что произошло на стенде», «кто удалил X», «что недавно менялось». Постранично: next_page_token. Доступ к журналам аудита включает поддержка Cloud.ru; до этого API отвечает 401, и ответ сообщает, что аудит для проекта не включён.

Вызывает инструмент audit_events (подробнее: 'evo tools describe audit_events').
```

```
evo audit events [flags]
```

### Examples

```
  evo audit events
  evo audit events --since 30m
  evo audit events --since 1d --page-size 500 --json | jq -r '.next_page_token'
```

### Options

```
  -h, --help                Справка по events
      --page-size int       Наибольшее число событий на странице (по умолчанию 100, не больше 500)
      --page-token string   Токен продолжения — next_page_token предыдущего ответа (необязательно)
      --since string        Окно назад от текущего момента: 30m, 1h, 3h, 1d или число минут строкой, например "90" (по умолчанию 1h; лучше короче — для активных проектов API рекомендует не больше 30 минут)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo audit](#evo-audit)	 - Журнал аудита: кто что сделал в проекте

## evo billing

Биллинг: баланс, потребление, отчёт FinOps

```
evo billing [flags]
```

### Options

```
  -h, --help   Справка по billing
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo billing balance](#evo-billing-balance)	 - Баланс по договору: сумма в рублях, признак активности
* [evo billing consumption](#evo-billing-consumption)	 - Стоимость потребления за период по сервисам: по договору или по проекту
* [evo billing finops](#evo-billing-finops)	 - Отчёт FinOps: распределение затрат, аномалии, темп расходов, запас баланса

## evo billing balance

Баланс по договору: сумма в рублях, признак активности

### Synopsis

```text
Баланс по договору: сумма в рублях, признак активности.

Баланс счёта по договору: сумма в рублях, признак активности и исходный объект договора. Только чтение. Нужен CLOUDRU_AGREEMENT_ID: баланс принадлежит договору, по проекту его не даёт ни один API; id договора видят только администраторы организации и администраторы затрат. Запрос идёт к billing.api.cloud.ru/v1/agreement/{agreement_id}; этого метода нет в опубликованных спецификациях API Cloud.ru, поэтому он работает по возможности и может измениться без предупреждения. Так проверяют, на сколько ещё хватит баланса.

Вызывает инструмент billing_balance (подробнее: 'evo tools describe billing_balance').
```

```
evo billing balance [flags]
```

### Examples

```
  CLOUDRU_AGREEMENT_ID=<agreement-id> evo billing balance
  evo billing balance --json
```

### Options

```
  -h, --help   Справка по balance
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo billing](#evo-billing)	 - Биллинг: баланс, потребление, отчёт FinOps

## evo billing consumption

Стоимость потребления за период по сервисам: по договору или по проекту

### Synopsis

```text
Стоимость потребления за период по сервисам: по договору или по проекту.

Стоимость потребления за период — по договору (CLOUDRU_AGREEMENT_ID) или, без него, по проекту; поле scope в ответе говорит, по чему. Сводка по сервисам: total (без НДС, сумма amount), total_with_nds (с НДС, сумма amount_nds), records, by_service — по убыванию стоимости (без НДС). Только чтение. Роли: для договора — администратор организации или администратор затрат; для проекта — администратор проекта (platform.project.admin). Для FinOps и анализа затрат.

Вызывает инструмент billing_consumption (подробнее: 'evo tools describe billing_consumption').
```

```
evo billing consumption [flags]
```

### Examples

```
  evo billing consumption --start 2026-10-01 --end 2026-10-19
  evo billing consumption --start 2026-10-01 --project-id <UUID> --json | jq '.by_service'
```

### Options

```
      --end string          Дата окончания, ГГГГ-ММ-ДД (по умолчанию — дата начала)
  -h, --help                Справка по consumption
      --project-id string   UUID проекта, чьё потребление прочитать (по умолчанию — текущий проект, если CLOUDRU_AGREEMENT_ID не задан; с ним — весь договор)
      --start string        Дата начала, ГГГГ-ММ-ДД (обязательный)
```

### Options inherited from parent commands

```
      --dry-run          Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json             Вывод в формате JSON
      --no-color         Без цвета
      --profile string   Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --region string    UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose          Подробный журнал
  -y, --yes              Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo billing](#evo-billing)	 - Биллинг: баланс, потребление, отчёт FinOps

## evo billing finops

Отчёт FinOps: распределение затрат, аномалии, темп расходов, запас баланса

### Synopsis

```text
Отчёт FinOps: распределение затрат, аномалии, темп расходов, запас баланса.

Отчёт FinOps (только чтение). INFORM — распределение затрат по сервисам и аномалии относительно предыдущего периода той же длины; QUANTIFY — дневной темп расходов, на сколько дней хватит баланса, прогнозная дата нулевого баланса и, если задан бюджет, его проверка. Затраты без НДС (amount). Отбор — как у 'evo billing consumption'; без CLOUDRU_AGREEMENT_ID баланс и запас неизвестны. Перенесён из finops_report.py. Для анализа затрат, предупреждений о запасе и проверки бюджета.

Вызывает инструмент billing_finops (подробнее: 'evo tools describe billing_finops').
```

```
evo billing finops [flags]
```

### Examples

```
  evo billing finops
  evo billing finops --days 7 --budget 50000
  evo billing finops --json
```

### Options

```
      --budget int          Месячный бюджет, руб.: прогноз расходов за месяц сравнивается с ним (необязательно)
      --days int            Длина окна в днях, если дата начала не задана: 1–366 (по умолчанию 30)
      --end string          Дата окончания, ГГГГ-ММ-ДД (по умолчанию — сегодня)
  -h, --help                Справка по finops
      --project-id string   UUID проекта (по умолчанию — текущий проект, если CLOUDRU_AGREEMENT_ID не задан; с ним — весь договор)
      --start string        Дата начала, ГГГГ-ММ-ДД (по умолчанию — за --days дней по дату окончания включительно)
```

### Options inherited from parent commands

```
      --dry-run          Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json             Вывод в формате JSON
      --no-color         Без цвета
      --profile string   Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --region string    UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose          Подробный журнал
  -y, --yes              Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo billing](#evo-billing)	 - Биллинг: баланс, потребление, отчёт FinOps

## evo ca

Container Apps: бессерверные контейнеры

```
evo ca [flags]
```

### Options

```
  -h, --help   Справка по ca
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo ca delete](#evo-ca-delete)	 - Удалить приложение Container Apps (асинхронно)
* [evo ca deploy](#evo-ca-deploy)	 - Развернуть приложение Container Apps из образа контейнера (асинхронно)
* [evo ca get](#evo-ca-get)	 - Приложение Container Apps по имени: состояние, публичный URL
* [evo ca list](#evo-ca-list)	 - Приложения Container Apps проекта
* [evo ca logs](#evo-ca-logs)	 - Последние журналы приложения Container Apps
* [evo ca start](#evo-ca-start)	 - Запустить остановленное приложение Container Apps
* [evo ca stop](#evo-ca-stop)	 - Остановить приложение Container Apps

## evo ca delete

Удалить приложение Container Apps (асинхронно)

### Synopsis

```text
Удалить приложение Container Apps (асинхронно).

Удалить приложение Container Apps по имени. Операция асинхронная.

Вызывает инструмент ca_delete (подробнее: 'evo tools describe ca_delete').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo ca delete <name> [flags]
```

### Examples

```
  evo ca delete my-app --dry-run
  evo ca delete my-app --yes
```

### Options

```
  -h, --help   Справка по delete
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo ca](#evo-ca)	 - Container Apps: бессерверные контейнеры

## evo ca deploy

Развернуть приложение Container Apps из образа контейнера (асинхронно)

### Synopsis

```text
Развернуть приложение Container Apps из образа контейнера (асинхронно).

Развернуть (создать и запустить) приложение Container Apps из образа контейнера — бессерверно, без нагрузки приложение масштабируется до нуля. Превью (will_create) перечисляет все настройки, в том числе работает ли контейнер от root. Имя, порт и число экземпляров проверяются до подтверждения. Операция асинхронная: ответ содержит operation_id; состояние и публичный URL — 'evo ca get <name>'. Подходит для веб-сервисов и API без состояния, когда не нужна своя ВМ.

Вызывает инструмент ca_deploy (подробнее: 'evo tools describe ca_deploy').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo ca deploy <name> [flags]
```

### Examples

```
  evo ca deploy my-app --image myregistry.cr.cloud.ru/my-app:1.0 --dry-run
  evo ca deploy my-app --image myregistry.cr.cloud.ru/my-app:1.0 --port 8080 --env LOG_LEVEL=info --yes
  evo ca deploy worker --image myregistry.cr.cloud.ru/worker:1.0 --public=false --env API_TOKEN=@./api.token --yes
```

### Options

```
      --cpu string           Запрос CPU, например 0.1 или 1 (по умолчанию 0.1)
      --description string   Описание (необязательно)
      --env stringArray      Переменные окружения: объект имя→значение (поле=значение, флаг повторяется; значение может содержать '='. поле=- читает значение из stdin (не больше одного поля), поле=@файл — из файла без завершающего перевода строки: так пароль не попадает в командную строку и историю оболочки)
  -h, --help                 Справка по deploy
      --image string         URI образа контейнера из Artifact Registry этого проекта (или публичного реестра другого проекта), linux/amd64, например <registry>.cr.cloud.ru/<repository>:<tag>. Приложение должно слушать 0.0.0.0:$PORT
      --max-instances int    Наибольшее число экземпляров (по умолчанию 1, не меньше 1)
      --memory string        Запрос памяти, например 256Mi или 1Gi (по умолчанию 256Mi)
      --min-instances int    Наименьшее число экземпляров (по умолчанию 0 — масштабирование до нуля); не больше --max-instances
      --port int             Порт контейнера, который слушает приложение (по умолчанию 8080). Порты, занятые платформой, отклоняются: 1–1024, 8008, 8012, 8013, 8022, 8112, 9090, 9091, 9092
      --privileged           Запустить контейнер в привилегированном режиме, от root (по умолчанию false: от UID 1000). Только для образов, которым нужен root; превью об этом сообщает
      --public               Открыть публичный URL. Выключить: --public=false (по умолчанию true)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo ca](#evo-ca)	 - Container Apps: бессерверные контейнеры

## evo ca get

Приложение Container Apps по имени: состояние, публичный URL

### Synopsis

```text
Приложение Container Apps по имени: состояние, публичный URL.

Все сведения о приложении Container Apps по имени, в том числе состояние и публичный URL. Только чтение.

Вызывает инструмент ca_get (подробнее: 'evo tools describe ca_get').
```

```
evo ca get <name> [flags]
```

### Examples

```
  evo ca get my-app
  evo ca get my-app --json
```

### Options

```
  -h, --help   Справка по get
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo ca](#evo-ca)	 - Container Apps: бессерверные контейнеры

## evo ca list

Приложения Container Apps проекта

### Synopsis

```text
Приложения Container Apps проекта.

Приложения Container Apps (бессерверные контейнеры) проекта: имя, состояние. Только чтение.

Вызывает инструмент ca_list (подробнее: 'evo tools describe ca_list').
```

```
evo ca list [flags]
```

### Examples

```
  evo ca list
  evo ca list --json
```

### Options

```
  -h, --help   Справка по list
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo ca](#evo-ca)	 - Container Apps: бессерверные контейнеры

## evo ca logs

Последние журналы приложения Container Apps

### Synopsis

```text
Последние журналы приложения Container Apps.

Последние журналы приложения Container Apps: 100 последних строк по времени, старые первыми (журналы приложения — stdout и stderr контейнера). С --system — события платформы. Только чтение.

Вызывает инструмент ca_logs (подробнее: 'evo tools describe ca_logs').
```

```
evo ca logs <name> [flags]
```

### Examples

```
  evo ca logs my-app
  evo ca logs my-app --system
  evo ca logs my-app --json
```

### Options

```
  -h, --help     Справка по logs
      --system   Системные журналы вместо журналов приложения
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo ca](#evo-ca)	 - Container Apps: бессерверные контейнеры

## evo ca start

Запустить остановленное приложение Container Apps

### Synopsis

```text
Запустить остановленное приложение Container Apps.

Запустить остановленное приложение Container Apps; пока оно работает, оно оплачивается.

Вызывает инструмент ca_start (подробнее: 'evo tools describe ca_start').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo ca start <name> [flags]
```

### Examples

```
  evo ca start my-app --yes
```

### Options

```
  -h, --help   Справка по start
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo ca](#evo-ca)	 - Container Apps: бессерверные контейнеры

## evo ca stop

Остановить приложение Container Apps

### Synopsis

```text
Остановить приложение Container Apps.

Остановить работающее приложение Container Apps: оно перестаёт обслуживать запросы.

Вызывает инструмент ca_stop (подробнее: 'evo tools describe ca_stop').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo ca stop <name> [flags]
```

### Examples

```
  evo ca stop my-app --yes
```

### Options

```
  -h, --help   Справка по stop
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo ca](#evo-ca)	 - Container Apps: бессерверные контейнеры

## evo cert

Certificate Manager: TLS-сертификаты

```
evo cert [flags]
```

### Options

```
  -h, --help   Справка по cert
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo cert delete](#evo-cert-delete)	 - Удалить сертификат (безвозвратно)
* [evo cert get](#evo-cert-get)	 - Сертификат по ID: метаданные, основная версия, срок действия, состояние
* [evo cert import](#evo-cert-import)	 - Загрузить в Certificate Manager свой сертификат и закрытый ключ
* [evo cert issue-le](#evo-cert-issue-le)	 - Выпустить бесплатный сертификат Let's Encrypt (асинхронно)
* [evo cert list](#evo-cert-list)	 - TLS-сертификаты проекта в Certificate Manager
* [evo cert operation](#evo-cert-operation)	 - Состояние асинхронной операции Certificate Manager
* [evo cert private-key](#evo-cert-private-key)	 - Закрытый ключ версии сертификата — открытым текстом
* [evo cert renew](#evo-cert-renew)	 - Перевыпустить сертификат Let's Encrypt (асинхронно)
* [evo cert usage](#evo-cert-usage)	 - Ресурсы, которые используют сертификат
* [evo cert versions](#evo-cert-versions)	 - Версии сертификата

## evo cert delete

Удалить сертификат (безвозвратно)

### Synopsis

```text
Удалить сертификат (безвозвратно).

Удалить сертификат безвозвратно. Если он привязан к балансировщику, удаление может сломать HTTPS; удалить всё равно — --force.

Вызывает инструмент cert_delete (подробнее: 'evo tools describe cert_delete').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo cert delete <id> [flags]
```

### Examples

```
  evo cert delete <certificate-id> --dry-run
  evo cert delete <certificate-id> --force --yes
```

### Options

```
      --force   Удалить, даже если сертификат используют другие сервисы (по умолчанию false)
  -h, --help    Справка по delete
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo cert](#evo-cert)	 - Certificate Manager: TLS-сертификаты

## evo cert get

Сертификат по ID: метаданные, основная версия, срок действия, состояние

### Synopsis

```text
Сертификат по ID: метаданные, основная версия, срок действия, состояние.

Один сертификат Certificate Manager по id: метаданные, основная версия, срок действия, состояние. Только чтение.

Вызывает инструмент cert_get (подробнее: 'evo tools describe cert_get').
```

```
evo cert get <id> [flags]
```

### Examples

```
  evo cert get <certificate-id>
  evo cert get <certificate-id> --json
```

### Options

```
  -h, --help   Справка по get
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo cert](#evo-cert)	 - Certificate Manager: TLS-сертификаты

## evo cert import

Загрузить в Certificate Manager свой сертификат и закрытый ключ

### Synopsis

```text
Загрузить в Certificate Manager свой сертификат и закрытый ключ.

Импортировать в Certificate Manager собственный сертификат пользователя (bring-your-own). Сертификат (--server-certificate) и закрытый ключ (--private-key) передаются как PEM в кодировке Base64.

Вызывает инструмент cert_import (подробнее: 'evo tools describe cert_import').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo cert import <name> [flags]
```

### Examples

```
  evo cert import my-cert --server-certificate "$(base64 < cert.pem | tr -d '\n')" --private-key "$(base64 < key.pem | tr -d '\n')" --dry-run
```

### Options

```
      --description string                Описание (необязательно)
  -h, --help                              Справка по import
      --intermediate-certificate string   Цепочка промежуточных сертификатов, PEM в Base64 (необязательно)
      --private-key string                Закрытый ключ, PEM в Base64 (обязательный)
      --server-certificate string         Сертификат (открытая часть), PEM в Base64 (обязательный)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo cert](#evo-cert)	 - Certificate Manager: TLS-сертификаты

## evo cert issue-le

Выпустить бесплатный сертификат Let's Encrypt (асинхронно)

### Synopsis

```text
Выпустить бесплатный сертификат Let's Encrypt (асинхронно).

Выпустить бесплатный TLS-сертификат Let's Encrypt для одного или нескольких доменов. Так включают HTTPS на сайте (например, shkrbkv.ru): выпуск, затем привязка к обработчику балансировщика. Зоны доменов должны быть заранее делегированы в Evolution DNS, иначе — ошибка ERROR_CODE_NOT_DELEGATED. Операция асинхронная: ответ содержит id операции, ход — 'evo cert operation <operation-id>' (до done=true).

Вызывает инструмент cert_issue_le (подробнее: 'evo tools describe cert_issue_le').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo cert issue-le <name> [flags]
```

### Examples

```
  evo cert issue-le site --dns-names example.com,www.example.com --dry-run
  evo cert issue-le site --dns-names example.com --yes
```

### Options

```
      --description string      Описание (необязательно)
      --dns-names stringArray   Домены для CN и SAN, например shkrbkv.ru и www.shkrbkv.ru (обязательный) (флаг повторяется или значения через запятую)
  -h, --help                    Справка по issue-le
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo cert](#evo-cert)	 - Certificate Manager: TLS-сертификаты

## evo cert list

TLS-сертификаты проекта в Certificate Manager

### Synopsis

```text
TLS-сертификаты проекта в Certificate Manager.

TLS-сертификаты проекта в Certificate Manager; отбор — по имени, DNS-имени, common name, типу и состоянию. Только чтение. Так находят сертификат для HTTPS-балансировщика или сайта.

Вызывает инструмент cert_list (подробнее: 'evo tools describe cert_list').
```

```
evo cert list [flags]
```

### Examples

```
  evo cert list
  evo cert list --dns-name example.com
  evo cert list --state CERTIFICATE_STATE_EXPIRED --json
```

### Options

```
      --common-name string   Часть common name (CN) для отбора (необязательно)
      --dns-name string      Одно из DNS-имён сертификата, например shkrbkv.ru (необязательно)
  -h, --help                 Справка по list
      --name string          Имя сертификата или его часть (необязательно)
      --state string         CERTIFICATE_STATE_ACTIVE или CERTIFICATE_STATE_EXPIRED (необязательно)
      --type string          CERTIFICATE_TYPE_IMPORTED, CERTIFICATE_TYPE_SELF_SIGNED или CERTIFICATE_TYPE_LETS_ENCRYPT (необязательно)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo cert](#evo-cert)	 - Certificate Manager: TLS-сертификаты

## evo cert operation

Состояние асинхронной операции Certificate Manager

### Synopsis

```text
Состояние асинхронной операции Certificate Manager.

Состояние асинхронной операции Certificate Manager (выпуск или перевыпуск сертификата) по id: опрашивайте, пока не станет done=true. С --wait-seconds команда сама ждёт завершения до N секунд и выводит итоговое состояние. Только чтение.

Вызывает инструмент cert_operation (подробнее: 'evo tools describe cert_operation').
```

```
evo cert operation <operation-id> [flags]
```

### Examples

```
  evo cert operation <operation-id>
  evo cert operation <operation-id> --wait-seconds 300
```

### Options

```
  -h, --help               Справка по operation
      --wait-seconds int   Ждать итогового состояния до N секунд (не больше 300), опрашивая операцию с нарастающим интервалом. Без флага — одна проверка
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo cert](#evo-cert)	 - Certificate Manager: TLS-сертификаты

## evo cert private-key

Закрытый ключ версии сертификата — открытым текстом

### Synopsis

```text
Закрытый ключ версии сертификата — открытым текстом.

СЕКРЕТНЫЙ закрытый ключ версии сертификата. privateKey (в живом API — private_key) — текст PEM (-----BEGIN …), раскодированный из Base64 ответа API (key_format: pem; base64 — если значение API не PEM в Base64). 'evo cert import' принимает ключ в Base64.

Вызывает инструмент cert_private_key (подробнее: 'evo tools describe cert_private_key').

Показывает секрет, поэтому спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее.
```

```
evo cert private-key <certificate-id> [flags]
```

### Examples

```
  evo cert private-key <certificate-id>
  evo cert private-key <certificate-id> --version-id 2 --yes --json > key.json
```

### Options

```
  -h, --help             Справка по private-key
      --version-id int   Номер версии; 0 или без значения — последняя версия
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo cert](#evo-cert)	 - Certificate Manager: TLS-сертификаты

## evo cert renew

Перевыпустить сертификат Let's Encrypt (асинхронно)

### Synopsis

```text
Перевыпустить сертификат Let's Encrypt (асинхронно).

Запустить перевыпуск сертификата Let's Encrypt. Операция асинхронная: ответ содержит id операции, ход — 'evo cert operation <operation-id>'.

Вызывает инструмент cert_renew (подробнее: 'evo tools describe cert_renew').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo cert renew <certificate-id> [flags]
```

### Examples

```
  evo cert renew <certificate-id> --dry-run
  evo cert renew <certificate-id> --yes
```

### Options

```
  -h, --help   Справка по renew
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo cert](#evo-cert)	 - Certificate Manager: TLS-сертификаты

## evo cert usage

Ресурсы, которые используют сертификат

### Synopsis

```text
Ресурсы, которые используют сертификат.

Ресурсы (балансировщики и их обработчики), которые используют сертификат. Проверяйте перед удалением сертификата. Только чтение.

Вызывает инструмент cert_usage (подробнее: 'evo tools describe cert_usage').
```

```
evo cert usage <id> [flags]
```

### Examples

```
  evo cert usage <certificate-id>
```

### Options

```
  -h, --help   Справка по usage
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo cert](#evo-cert)	 - Certificate Manager: TLS-сертификаты

## evo cert versions

Версии сертификата

### Synopsis

```text
Версии сертификата.

Версии сертификата: id, CN, DNS-имена, состояние, срок действия — со всех страниц списка. Только чтение.

Вызывает инструмент cert_versions (подробнее: 'evo tools describe cert_versions').
```

```
evo cert versions <certificate-id> [flags]
```

### Examples

```
  evo cert versions <certificate-id>
  evo cert versions <certificate-id> --json
```

### Options

```
  -h, --help   Справка по versions
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo cert](#evo-cert)	 - Certificate Manager: TLS-сертификаты

## evo completion

Скрипт автодополнения для оболочки

### Synopsis

```text
Печатает скрипт автодополнения evo для выбранной оболочки.
Как подключить скрипт — в справке подкоманды (bash, zsh, fish, powershell).
```

```
evo completion [flags]
```

### Options

```
  -h, --help   Справка по completion
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo completion bash](#evo-completion-bash)	 - Скрипт автодополнения для bash
* [evo completion fish](#evo-completion-fish)	 - Скрипт автодополнения для fish
* [evo completion powershell](#evo-completion-powershell)	 - Скрипт автодополнения для powershell
* [evo completion zsh](#evo-completion-zsh)	 - Скрипт автодополнения для zsh

## evo completion bash

Скрипт автодополнения для bash

### Synopsis

```text
Скрипт автодополнения для bash. Нужен пакет bash-completion (ставится
пакетным менеджером системы).

В текущей сессии:

	source <(evo completion bash)

Для всех новых сессий выполните один раз:

#### Linux:

	evo completion bash > /etc/bash_completion.d/evo

#### macOS:

	evo completion bash > $(brew --prefix)/etc/bash_completion.d/evo

Настройка действует в новой оболочке.
```

```
evo completion bash
```

### Options

```
  -h, --help              Справка по bash
      --no-descriptions   Без описаний команд в подсказках
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo completion](#evo-completion)	 - Скрипт автодополнения для оболочки

## evo completion fish

Скрипт автодополнения для fish

### Synopsis

```text
Скрипт автодополнения для fish.

В текущей сессии:

	evo completion fish | source

Для всех новых сессий выполните один раз:

	evo completion fish > ~/.config/fish/completions/evo.fish

Настройка действует в новой оболочке.
```

```
evo completion fish [flags]
```

### Options

```
  -h, --help              Справка по fish
      --no-descriptions   Без описаний команд в подсказках
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo completion](#evo-completion)	 - Скрипт автодополнения для оболочки

## evo completion powershell

Скрипт автодополнения для powershell

### Synopsis

```text
Скрипт автодополнения для PowerShell.

В текущей сессии:

	evo completion powershell | Out-String | Invoke-Expression

Для всех новых сессий добавьте вывод этой команды в профиль PowerShell.
```

```
evo completion powershell [flags]
```

### Options

```
  -h, --help              Справка по powershell
      --no-descriptions   Без описаний команд в подсказках
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo completion](#evo-completion)	 - Скрипт автодополнения для оболочки

## evo completion zsh

Скрипт автодополнения для zsh

### Synopsis

```text
Скрипт автодополнения для zsh. Если автодополнение в zsh ещё не включено,
выполните один раз:

	echo "autoload -U compinit; compinit" >> ~/.zshrc

В текущей сессии:

	source <(evo completion zsh)

Для всех новых сессий выполните один раз:

#### Linux:

	evo completion zsh > "${fpath[1]}/_evo"

#### macOS:

	evo completion zsh > $(brew --prefix)/share/zsh/site-functions/_evo

Настройка действует в новой оболочке.
```

```
evo completion zsh [flags]
```

### Options

```
  -h, --help              Справка по zsh
      --no-descriptions   Без описаний команд в подсказках
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo completion](#evo-completion)	 - Скрипт автодополнения для оболочки

## evo configure

Ключ доступа и проект по умолчанию для профиля

### Synopsis

```text
Сохранить ключ доступа сервисного аккаунта и проект по умолчанию в профиле.

В интерактивном режиме спрашивает Key ID и секрет (секрет не отображается),
проверяет их в IAM Cloud.ru, затем показывает проекты, которые видит ключ, и
предлагает выбрать один (единственный проект выбирается сам).

Без вопросов (CI, скрипты) — секрет читается из stdin, никогда из флага:
  printf '%s' "$SECRET" | evo configure --key-id KEY_ID --secret-stdin --project-id PROJECT_ID

Файлы (права 0600, каталог 0700; каталог меняет CLOUDRU_CONFIG_DIR):
  <каталог настроек>/evo/config.yaml        project_id, region, умолчания
  <каталог настроек>/evo/credentials.yaml   key_id, secret, необязательная секция s3

Нужные права: сервисному аккаунту нужна роль в проекте, разрешающая управлять
ресурсами Compute и VPC; для списка проектов — ещё и чтение организации
(иначе передайте --project-id). Подробнее:
https://github.com/shatim-ops/evo-cli/blob/main/docs/auth.md
```

```
evo configure [flags]
```

### Examples

```
  evo configure
  evo configure --profile test
  printf '%s' "$SECRET" | evo configure --key-id "$KEY_ID" --secret-stdin --project-id <UUID>
```

### Options

```
  -h, --help                Справка по configure
      --key-id string       Key ID ключа доступа сервисного аккаунта
      --project-id string   UUID проекта Cloud.ru (без выбора из списка)
      --region string       UUID зоны доступности по умолчанию для профиля
      --secret-stdin        Прочитать секрет из первой строки stdin (без вопросов)
```

### Options inherited from parent commands

```
      --dry-run          Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json             Вывод в формате JSON
      --no-color         Без цвета
      --profile string   Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --verbose          Подробный журнал
  -y, --yes              Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo configure delete](#evo-configure-delete)	 - Удалить сохранённый профиль и его токен из кэша
* [evo configure list](#evo-configure-list)	 - Сохранённые профили

## evo configure delete

Удалить сохранённый профиль и его токен из кэша

```
evo configure delete <profile> [flags]
```

### Options

```
  -h, --help   Справка по delete
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo configure](#evo-configure)	 - Ключ доступа и проект по умолчанию для профиля

## evo configure list

Сохранённые профили

```
evo configure list [flags]
```

### Options

```
  -h, --help   Справка по list
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo configure](#evo-configure)	 - Ключ доступа и проект по умолчанию для профиля

## evo deploy

Развернуть docker-compose проект на ВМ Cloud.ru

### Synopsis

```text
Развернуть docker-compose проект на ВМ Cloud.ru.

Первый запуск создаёт ВМ с floating IP, открывает порты из compose-файла в
группе безопасности зоны, ставит Docker, загружает compose-файл и выполняет
'docker compose up -d'. Перед созданием ВМ показывается превью с ценой
(расчёт Cloud.ru, иначе оценка evo) и запрашивается подтверждение: вопрос на
терминале, --yes — согласие заранее, --dry-run — только превью. Повторный
запуск обновляет ту же ВМ, если compose-файл изменился (с --force — и без
изменений).
```

```
evo deploy [flags]
```

### Examples

```
  evo deploy --dry-run
  evo deploy --yes
  evo deploy -f compose.prod.yml --flavor lowcost10-2-4
```

### Options

```
  -f, --compose-file string   Путь к compose-файлу (по умолчанию docker-compose.yml)
      --disk-size int         Размер загрузочного диска, ГБ (по умолчанию 10)
      --flavor string         Flavor ВМ (по умолчанию auto — подбор по сервисам)
      --force                 Развернуть заново, даже если compose-файл не изменился
  -h, --help                  Справка по deploy
      --image string          Загрузочный образ: имя из 'evo image list' или UUID (Ubuntu или Debian)
      --image-id string       UUID загрузочного образа
      --non-interactive       Никогда не спрашивать; тогда и для создания ВМ, и для обновления ВМ развёртывания нужен --yes
      --project-id string     UUID проекта Cloud.ru
      --region string         UUID зоны доступности
```

### Options inherited from parent commands

```
      --dry-run          Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json             Вывод в формате JSON
      --no-color         Без цвета
      --profile string   Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --verbose          Подробный журнал
  -y, --yes              Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo deploy apply](#evo-deploy-apply)	 - Шаг 2 мастера развёртывания: выполнить рассчитанный план
* [evo deploy list](#evo-deploy-list)	 - Локальные записи развёртываний этого проекта
* [evo deploy plan](#evo-deploy-plan)	 - Шаг 1 мастера развёртывания: рассчитать ВМ и её цену, ничего не создавая

## evo deploy apply

Шаг 2 мастера развёртывания: выполнить рассчитанный план

### Synopsis

```text
Шаг 2 мастера развёртывания: выполнить рассчитанный план.

Шаг 2 мастера развёртывания: выполнить план, который вернул 'evo deploy plan'. Выполняйте его только после того, как план и его цена одобрены. Идемпотентно: если ВМ с именем из плана уже есть, она подхватывается, а не создаётся вторая. Возвращает vm_id, public_ip, ssh и (один раз) сгенерированный пароль.

Вызывает инструмент deploy_apply (подробнее: 'evo tools describe deploy_apply').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo deploy apply <plan-id> [flags]
```

### Examples

```
  evo deploy apply <plan-id> --dry-run
  evo deploy apply <plan-id> --yes
```

### Options

```
  -h, --help   Справка по apply
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo deploy](#evo-deploy)	 - Развернуть docker-compose проект на ВМ Cloud.ru

## evo deploy list

Локальные записи развёртываний этого проекта

### Synopsis

```text
Локальные записи развёртываний этого проекта.

Все записанные развёртывания этого проекта: IP ВМ, состояние, flavor, время создания. Читает локальное состояние, без обращений к API.

Вызывает инструмент list (подробнее: 'evo tools describe list').
```

```
evo deploy list [flags]
```

### Examples

```
  evo deploy list
  evo deploy list --json | jq -r '.deployments[] | "\(.vm_ip)\t\(.status)"'
```

### Options

```
  -h, --help   Справка по list
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo deploy](#evo-deploy)	 - Развернуть docker-compose проект на ВМ Cloud.ru

## evo deploy plan

Шаг 1 мастера развёртывания: рассчитать ВМ и её цену, ничего не создавая

### Synopsis

```text
Шаг 1 мастера развёртывания: рассчитать ВМ и её цену, ничего не создавая.

Шаг 1 мастера развёртывания (только чтение, ничего не создаёт). Подбирает полную конфигурацию ВМ — flavor, образ, зону, диск, сеть, SSH — и возвращает её вместе с ЦЕНОЙ ЗА МЕСЯЦ и plan_id (cost.source: API расчёта цен Cloud.ru или оценка по тарифам, если API цены не дал; лицензия образа не учитывается никогда). В режиме auto передайте требования продукта (--cpu, --ram-gb, --disk-gb из его README), остальное заполнят значения по умолчанию; в режиме manual — явный выбор (--flavor, --zone, --image, --disk-type). Проверьте план и цену, затем выполните план: 'evo deploy apply <plan-id>'.

Вызывает инструмент deploy_plan (подробнее: 'evo tools describe deploy_plan').
```

```
evo deploy plan [flags]
```

### Examples

```
  evo deploy plan --product web --cpu 2 --ram-gb 4 --port 80
  evo deploy plan --mode manual --flavor lowcost10-2-4 --zone ru.AZ-2 --image Ubuntu-24.04 --json
  evo deploy plan --base-plan-id <plan-id> --disk-gb 20
```

### Options

```
      --base-plan-id string   Необязательно: прежний plan_id, который нужно ИЗМЕНИТЬ. Берутся параметры того плана, поверх — только переданные здесь поля (например, одно --disk-gb); возвращается новый plan_id. Так меняют одно значение, не повторяя остальные
      --compose-file string   Путь к docker-compose.yml, если это развёртывание compose (задаёт compose_required). С ним образ Astra Linux, Альт или РЕД ОС отклоняется (USAGE, плана нет): установочный скрипт Docker их не поддерживает
      --cpu int               Нужное число vCPU (автоподбор выберет самый маленький подходящий flavor)
      --disk-gb int           Размер загрузочного диска, ГБ (по умолчанию 10)
      --disk-type string      manual: имя типа диска, например SSD (список: evo disk types)
      --external-ip           Подключить публичный floating IP (по умолчанию true)
      --flavor string         manual: точное имя flavor (список: evo vm flavors). По умолчанию — flavor из настроек (профиль, .evo.yaml, CLOUDRU_FLAVOR), если не заданы --cpu и --ram-gb, иначе — автоподбор
  -h, --help                  Справка по plan
      --image string          manual: имя или UUID образа (поиск: evo image list). По умолчанию — образ из настроек (image или image_id: профиль, .evo.yaml), иначе Ubuntu 24.04
      --login string          Пользователь для входа на ВМ (по умолчанию user1)
      --meta stringArray      Значения полей метаданных образа по имени — обязательные поля, которые evo не заполняет сам (evo image get <образ>). Без них план отклоняется. Хранятся в локальном файле плана до его выполнения (поле=значение, флаг повторяется; значение может содержать '='. поле=- читает значение из stdin (не больше одного поля), поле=@файл — из файла без завершающего перевода строки: так пароль не попадает в командную строку и историю оболочки)
      --mode string           auto (значения по умолчанию и требования) или manual (явный выбор). По умолчанию auto
      --name string           Имя ВМ (по умолчанию <product>-<время>)
      --port stringArray      Порты продукта, которые открыть в группе безопасности ВМ (например, 80,443 для веб-сервера — из README). Выполнение плана открывает их само; evo vm delete удаляет созданные им правила (флаг повторяется или значения через запятую)
      --product string        Имя разворачиваемого продукта или приложения (для имени ВМ и отчётов)
      --ram-gb int            Нужный объём RAM, ГБ (автоподбор)
      --ssh-key string        Полный открытый SSH-ключ для ВМ (например, 'ssh-ed25519 AAAA... user'). Без него на ВМ попадает ключ развёртывания evo из рабочего каталога (.evo/id_ed25519.pub), чтобы evo мог зайти по SSH; ключ пользователя добавьте в authorized_keys потом
      --zone string           manual: зона доступности, имя или UUID (список: evo vm zones). По умолчанию — зона из настроек (профиль, .evo.yaml, CLOUDRU_REGION), иначе ru.AZ-2
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo deploy](#evo-deploy)	 - Развернуть docker-compose проект на ВМ Cloud.ru

## evo destroy

Удалить развёрнутую ВМ и связанные ресурсы

### Synopsis

```text
Удалить ВМ развёртывания из этого каталога, освободить её floating IP и
удалить правила группы безопасности, которые создал deploy.

Перед удалением показывается превью и запрашивается подтверждение: вопрос на
терминале, --yes — согласие заранее, --dry-run — только превью.
Данные на дисках ВМ теряются.
```

```
evo destroy [flags]
```

### Examples

```
  evo destroy --dry-run
  evo destroy
  evo destroy --yes
```

### Options

```
  -h, --help   Справка по destroy
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов

## evo disk

Диски

```
evo disk [flags]
```

### Options

```
  -h, --help   Справка по disk
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo disk attach](#evo-disk-attach)	 - Подключить диск к ВМ (диск и ВМ должны быть в одной зоне)
* [evo disk create](#evo-disk-create)	 - Создать диск (с --attach-to — в зоне ВМ и сразу подключить)
* [evo disk delete](#evo-disk-delete)	 - Удалить диск и все данные на нём (безвозвратно)
* [evo disk detach](#evo-disk-detach)	 - Отключить диск от ВМ
* [evo disk list](#evo-disk-list)	 - Диски: id, имя, размер, состояние, тип, зона, к каким ВМ подключены
* [evo disk resize](#evo-disk-resize)	 - Увеличить диск (только рост; затем расширьте раздел и файловую систему на ВМ)
* [evo disk types](#evo-disk-types)	 - Типы дисков (UUID и имя, например SSD или HDD)

## evo disk attach

Подключить диск к ВМ (диск и ВМ должны быть в одной зоне)

### Synopsis

```text
Подключить диск к ВМ (диск и ВМ должны быть в одной зоне).

Подключить существующий диск к ВМ (--vm — имя или UUID ВМ). Диск и ВМ должны быть в одной зоне; зону и id диска показывает 'evo disk list'. Подключённый диск — новое блочное устройство: разметьте, отформатируйте и смонтируйте его на ВМ.

Вызывает инструмент disk_attach (подробнее: 'evo tools describe disk_attach').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo disk attach <disk-id> [flags]
```

### Options

```
  -h, --help        Справка по attach
      --vm string   ВМ: имя или UUID
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo disk](#evo-disk)	 - Диски

## evo disk create

Создать диск (с --attach-to — в зоне ВМ и сразу подключить)

### Synopsis

```text
Создать диск (с --attach-to — в зоне ВМ и сразу подключить).

Создать диск. С --attach-to <ВМ> диск создаётся в зоне этой ВМ и сразу к ней подключается: диск и ВМ должны быть в одной зоне. Подключённый диск — новое блочное устройство, файловая система ВМ от него не растёт: разметьте, отформатируйте и смонтируйте его на ВМ. Увеличить существующий диск — 'evo disk resize'. Диск платный.

Вызывает инструмент disk_create (подробнее: 'evo tools describe disk_create').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo disk create <name> [flags]
```

### Options

```
      --attach-to string   ВМ (имя или UUID): диск создаётся в её зоне и сразу к ней подключается
      --disk-type string   Тип диска: имя или UUID (список: evo disk types). По умолчанию — SSD
  -h, --help               Справка по create
      --size-gb int        Размер диска, ГБ (обязательный; не больше 16384)
      --zone string        Зона доступности: имя или UUID, вместо --attach-to (по умолчанию — зона из --region или профиля)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo disk](#evo-disk)	 - Диски

## evo disk delete

Удалить диск и все данные на нём (безвозвратно)

### Synopsis

```text
Удалить диск и все данные на нём (безвозвратно).

Удалить диск и все данные на нём безвозвратно. Подключённый диск сначала отключите: 'evo disk detach'.

Вызывает инструмент disk_delete (подробнее: 'evo tools describe disk_delete').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo disk delete <disk-id> [flags]
```

### Options

```
  -h, --help   Справка по delete
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo disk](#evo-disk)	 - Диски

## evo disk detach

Отключить диск от ВМ

### Synopsis

```text
Отключить диск от ВМ.

Отключить диск от ВМ (--vm — ВМ, к которой он подключён). Загрузочный или используемый диск не отключайте: сначала отмонтируйте файловую систему на ВМ.

Вызывает инструмент disk_detach (подробнее: 'evo tools describe disk_detach').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo disk detach <disk-id> [flags]
```

### Options

```
  -h, --help        Справка по detach
      --vm string   ВМ, к которой подключён диск: имя или UUID
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo disk](#evo-disk)	 - Диски

## evo disk list

Диски: id, имя, размер, состояние, тип, зона, к каким ВМ подключены

### Synopsis

```text
Диски: id, имя, размер, состояние, тип, зона, к каким ВМ подключены.

Диски проекта: id, имя, размер (ГБ), состояние, тип, зона и ВМ, к которым диск подключён. Только чтение.

Вызывает инструмент disk_list (подробнее: 'evo tools describe disk_list').
```

```
evo disk list [flags]
```

### Examples

```
  evo disk list
  evo disk list --json
```

### Options

```
  -h, --help   Справка по list
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo disk](#evo-disk)	 - Диски

## evo disk resize

Увеличить диск (только рост; затем расширьте раздел и файловую систему на ВМ)

### Synopsis

```text
Увеличить диск (только рост; затем расширьте раздел и файловую систему на ВМ).

Увеличить диск на месте, до 16384 ГБ (16 ТБ). Только рост: уменьшение и диски бесплатного уровня облако отклоняет (бесплатный загрузочный диск не увеличить — пересоздайте ВМ с платным диском). После увеличения расширьте раздел и файловую систему на ВМ (growpart и resize2fs).

Вызывает инструмент disk_resize (подробнее: 'evo tools describe disk_resize').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo disk resize <disk-id> [flags]
```

### Options

```
  -h, --help          Справка по resize
      --size-gb int   Новый размер, ГБ: больше текущего, не больше 16384
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo disk](#evo-disk)	 - Диски

## evo disk types

Типы дисков (UUID и имя, например SSD или HDD)

### Synopsis

```text
Типы дисков (UUID и имя, например SSD или HDD).

Типы дисков: UUID и имя (например, SSD или HDD). Имя или UUID типа передаётся в --disk-type команды 'evo disk create'. Только чтение.

Вызывает инструмент list_disk_types (подробнее: 'evo tools describe list_disk_types').
```

```
evo disk types [flags]
```

### Options

```
  -h, --help   Справка по types
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo disk](#evo-disk)	 - Диски

## evo dns

Evolution DNS: зоны и записи

```
evo dns [flags]
```

### Options

```
  -h, --help   Справка по dns
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo dns operation](#evo-dns-operation)	 - Состояние асинхронной операции DNS
* [evo dns record-create](#evo-dns-record-create)	 - Добавить запись DNS в зону (асинхронно)
* [evo dns record-delete](#evo-dns-record-delete)	 - Удалить запись DNS (асинхронно)
* [evo dns record-get](#evo-dns-record-get)	 - Запись DNS по ID
* [evo dns records](#evo-dns-records)	 - Записи зоны DNS
* [evo dns zone-create](#evo-dns-zone-create)	 - Создать зону DNS: публичную или приватную
* [evo dns zone-delete](#evo-dns-zone-delete)	 - Удалить зону DNS вместе с её записями (асинхронно)
* [evo dns zone-get](#evo-dns-zone-get)	 - Зона DNS по ID: все сведения
* [evo dns zones](#evo-dns-zones)	 - Зоны DNS проекта: публичные и приватные

## evo dns operation

Состояние асинхронной операции DNS

### Synopsis

```text
Состояние асинхронной операции DNS.

Состояние асинхронной операции DNS по id — его возвращают создание и удаление зон и записей DNS: done — завершена ли операция, error — ошибка. С --wait-seconds команда сама ждёт завершения до N секунд и выводит итоговое состояние. Только чтение.

Вызывает инструмент dns_operation (подробнее: 'evo tools describe dns_operation').
```

```
evo dns operation <operation-id> [flags]
```

### Examples

```
  evo dns operation <operation-id>
  evo dns operation <operation-id> --wait-seconds 120
```

### Options

```
  -h, --help               Справка по operation
      --wait-seconds int   Ждать итогового состояния до N секунд (не больше 300), опрашивая операцию с нарастающим интервалом. Без флага — одна проверка
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo dns](#evo-dns)	 - Evolution DNS: зоны и записи

## evo dns record-create

Добавить запись DNS в зону (асинхронно)

### Synopsis

```text
Добавить запись DNS в зону (асинхронно).

Добавить запись DNS в зону. Операция асинхронная: ход — 'evo dns operation <operation-id>'.

Вызывает инструмент dns_record_create (подробнее: 'evo tools describe dns_record_create').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo dns record-create <zone-id> <name> [flags]
```

### Examples

```
  evo dns record-create <zone-id> www --record-type a --value 203.0.113.10 --dry-run
  evo dns record-create <zone-id> api --record-type a --values 203.0.113.10,203.0.113.11 --ttl 60 --yes
```

### Options

```
  -h, --help                 Справка по record-create
      --record-type string   Тип записи: a, aaaa, cname, mx, ns, spf, txt, caa, srv (в любом регистре; отправляется строчными, как принимает API), обязательный
      --ttl int              TTL, секунд (по умолчанию 300)
      --type string          Видимость зоны: public или private. Необязательно: без неё evo определяет видимость по id
      --value string         Одно значение записи (например, IP для записи A). Задаётся --value или --values, не оба
      --values stringArray   Несколько значений записи (флаг повторяется или значения через запятую)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo dns](#evo-dns)	 - Evolution DNS: зоны и записи

## evo dns record-delete

Удалить запись DNS (асинхронно)

### Synopsis

```text
Удалить запись DNS (асинхронно).

Удалить запись DNS по id (список: 'evo dns records <zone-id>'). Операция асинхронная.

Вызывает инструмент dns_record_delete (подробнее: 'evo tools describe dns_record_delete').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo dns record-delete <record-id> [flags]
```

### Examples

```
  evo dns record-delete <record-id> --dry-run
  evo dns record-delete <record-id> --yes
```

### Options

```
  -h, --help          Справка по record-delete
      --type string   Видимость зоны: public или private. Необязательно: без неё evo определяет видимость по id
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo dns](#evo-dns)	 - Evolution DNS: зоны и записи

## evo dns record-get

Запись DNS по ID

### Synopsis

```text
Запись DNS по ID.

Одна запись DNS по id (список: 'evo dns records <zone-id>'). Только чтение. Видимость зоны (--type public или private) указывать не обязательно: evo определяет её по id.

Вызывает инструмент dns_record_get (подробнее: 'evo tools describe dns_record_get').
```

```
evo dns record-get <record-id> [flags]
```

### Examples

```
  evo dns record-get <record-id>
  evo dns record-get <record-id> --json
```

### Options

```
  -h, --help          Справка по record-get
      --type string   Видимость зоны: public или private. Необязательно: без неё evo определяет видимость по id
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo dns](#evo-dns)	 - Evolution DNS: зоны и записи

## evo dns records

Записи зоны DNS

### Synopsis

```text
Записи зоны DNS.

Ресурсные записи зоны DNS: id, имя, тип, значения, ttl. Только чтение. Видимость зоны (--type public или private) указывать не обязательно: evo определяет её по id.

Вызывает инструмент dns_records (подробнее: 'evo tools describe dns_records').
```

```
evo dns records <zone-id> [flags]
```

### Examples

```
  evo dns records <zone-id>
  evo dns records <zone-id> --json | jq -r '.records[] | "\(.name)\t\(.type)"'
```

### Options

```
  -h, --help          Справка по records
      --type string   Видимость зоны: public или private. Необязательно: без неё evo определяет видимость по id
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo dns](#evo-dns)	 - Evolution DNS: зоны и записи

## evo dns zone-create

Создать зону DNS: публичную или приватную

### Synopsis

```text
Создать зону DNS: публичную или приватную.

Создать зону DNS: --type public (по умолчанию) или private. Публичная зона обслуживает запросы только после подтверждения владения доменом через делегирование (в ответе — проверочная запись); приватная зона — внутренняя.

Вызывает инструмент dns_zone_create (подробнее: 'evo tools describe dns_zone_create').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo dns zone-create <domain> [flags]
```

### Examples

```
  evo dns zone-create example.com --dry-run
  evo dns zone-create example.com --yes
  evo dns zone-create corp.internal --type private --yes
```

### Options

```
      --description string   Описание зоны (необязательно)
  -h, --help                 Справка по zone-create
      --name string          Отображаемое имя зоны (по умолчанию — домен)
      --type string          Видимость зоны: public (по умолчанию) или private
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo dns](#evo-dns)	 - Evolution DNS: зоны и записи

## evo dns zone-delete

Удалить зону DNS вместе с её записями (асинхронно)

### Synopsis

```text
Удалить зону DNS вместе с её записями (асинхронно).

Удалить зону DNS по id (список: 'evo dns zones') вместе с её записями. Операция асинхронная. Видимость зоны (--type public или private) указывать не обязательно: evo определяет её по id зоны.

Вызывает инструмент dns_zone_delete (подробнее: 'evo tools describe dns_zone_delete').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo dns zone-delete <id> [flags]
```

### Examples

```
  evo dns zone-delete <zone-id> --dry-run
  evo dns zone-delete <zone-id>
```

### Options

```
  -h, --help          Справка по zone-delete
      --type string   Видимость зоны: public или private. Необязательно: без неё evo определяет видимость по id
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo dns](#evo-dns)	 - Evolution DNS: зоны и записи

## evo dns zone-get

Зона DNS по ID: все сведения

### Synopsis

```text
Зона DNS по ID: все сведения.

Все сведения об одной зоне DNS по id (список: 'evo dns zones'). Только чтение.

Вызывает инструмент dns_zone_get (подробнее: 'evo tools describe dns_zone_get').
```

```
evo dns zone-get <id> [flags]
```

### Examples

```
  evo dns zone-get <zone-id>
  evo dns zone-get <zone-id> --json
```

### Options

```
  -h, --help          Справка по zone-get
      --type string   Видимость зоны: public или private. Необязательно: без неё evo определяет видимость по id
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo dns](#evo-dns)	 - Evolution DNS: зоны и записи

## evo dns zones

Зоны DNS проекта: публичные и приватные

### Synopsis

```text
Зоны DNS проекта: публичные и приватные.

Зоны Evolution DNS проекта, публичные и приватные: id, имя, домен, видимость, состояние (у публичной зоны — и confirm_state), valid, delegated. Только чтение.

Вызывает инструмент dns_zones (подробнее: 'evo tools describe dns_zones').
```

```
evo dns zones [flags]
```

### Examples

```
  evo dns zones
  evo dns zones --json | jq -r '.zones[] | "\(.id)\t\(.domain)"'
```

### Options

```
  -h, --help   Справка по zones
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo dns](#evo-dns)	 - Evolution DNS: зоны и записи

## evo doctor

Проверить настройки, ключ доступа и связь с API

### Synopsis

```text
Проверить окружение: файлы настроек и их права, откуда взята каждая
настройка, доступность IAM и Compute API, ключ доступа, kubectl/helm в PATH
(нужны только инструментам k8s; их отсутствие — предупреждение, не ошибка).

Код выхода 2 — сломаны настройки или ключ, 1 — не прошли только проверки
связи.
```

```
evo doctor [flags]
```

### Examples

```
  evo doctor
  evo doctor --json
```

### Options

```
  -h, --help   Справка по doctor
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов

## evo iam

IAM: сервисные аккаунты

```
evo iam [flags]
```

### Options

```
  -h, --help   Справка по iam
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo iam service-accounts](#evo-iam-service-accounts)	 - Сервисные аккаунты IAM проекта

## evo iam service-accounts

Сервисные аккаунты IAM проекта

### Synopsis

```text
Сервисные аккаунты IAM проекта.

Сервисные аккаунты IAM проекта: id, имя, email, тип, включён ли. Только чтение. Так находят UUID сервисного аккаунта кластера (--cluster-sa-id команды 'evo k8s create') или проверяют сервисные аккаунты.

Вызывает инструмент iam_service_accounts (подробнее: 'evo tools describe iam_service_accounts').
```

```
evo iam service-accounts [flags]
```

### Examples

```
  evo iam service-accounts
  evo iam service-accounts --json | jq -r '.service_accounts[] | "\(.id)\t\(.name)"'
```

### Options

```
  -h, --help                Справка по service-accounts
      --project-id string   UUID проекта (по умолчанию — текущий проект)
```

### Options inherited from parent commands

```
      --dry-run          Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json             Вывод в формате JSON
      --no-color         Без цвета
      --profile string   Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --region string    UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose          Подробный журнал
  -y, --yes              Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo iam](#evo-iam)	 - IAM: сервисные аккаунты

## evo image

Образы ВМ: операционные системы и продукты маркетплейса

```
evo image [flags]
```

### Options

```
  -h, --help   Справка по image
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo image create](#evo-image-create)	 - Создать образ ВМ из загрузочного диска (платно, асинхронно)
* [evo image delete](#evo-image-delete)	 - Удалить образ ВМ
* [evo image get](#evo-image-get)	 - Образ по имени или UUID: минимум ресурсов, зоны, поля и кто их заполнит, команда создания ВМ
* [evo image list](#evo-image-list)	 - Поиск образов: ОС (public), маркетплейс (marketplace), образы проекта (private)

## evo image create

Создать образ ВМ из загрузочного диска (платно, асинхронно)

### Synopsis

```text
Создать образ ВМ из загрузочного диска (платно, асинхронно).

Создать многоразовый образ ВМ ИЗ ЗАГРУЗОЧНОГО ДИСКА — последний шаг подготовки распространяемого образа (например, продукта Маркетплейса): создайте ВМ, установите продукт, очистите систему, остановите ВМ и снимите образ с её загрузочного диска. Диск должен быть загрузочным И отключённым: выключите ВМ, отключите загрузочный диск ('evo disk detach') и дождитесь состояния available в 'evo disk list'. Остановить ВМ недостаточно: пока диск подключён, API отвечает invalid_disk_state_to_create_image.

Для образа Маркетплейса передайте и --user-data-template (шаблон cloud-init на Jinja), и --image-metadata с объявлением каждой {{ var }} шаблона: так SSH-ключ или пароль покупателя попадает в гостевую систему. Сборка асинхронная (около 0,7 ГБ в минуту): повторяйте 'evo image get', пока образ не станет готов (ready=true). Хранение образа платное.

Вызывает инструмент image_create (подробнее: 'evo tools describe image_create').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo image create <name> [flags]
```

### Examples

```
  evo image create my-product-1.0 --disk-id <disk-id> --dry-run
  evo image create my-product-1.0 --disk-id <disk-id> --display-name 'My product 1.0' --min-disk 20 --yes
```

### Options

```
      --description string           Описание в свободной форме, НЕ БОЛЬШЕ 255 символов (длиннее API отклоняет с too_long). Достаточно продукта, закреплённой версии и digest; юридические оговорки — в карточке Маркетплейса, а не здесь
      --disk-id string               UUID загрузочного диска-источника (список: evo disk list)
      --display-name string          Понятное имя, которое показывает консоль
  -h, --help                         Справка по create
      --image-metadata stringArray   Поля, которые заполняет покупатель; они подставляются в --user-data-template, и каждая {{ var }} шаблона должна быть объявлена здесь. Элемент: {name, display_name, display_position, field_type, default_value, sensitive, mandatory_group} — display_name и display_position ОБЯЗАТЕЛЬНЫ, а 'id' и 'is_required' отклоняются (схема записи отличается от того, что показывает evo image get). field_type — типизированные значения платформы (public_key, linux_password, linux_login, hostname, flag); public_key и linux_password поместите в одну mandatory_group ('authentication'), чтобы доступ давало любое из них (флаг повторяется или значения через запятую)
      --min-cpu int                  Наименьшее число vCPU, с которым ВМ загрузится из образа
      --min-disk int                 Наименьший загрузочный диск, ГБ — не меньше размера образа, иначе ВМ из него не создать
      --min-ram int                  Наименьший объём RAM, ГБ
      --user-data-template string    Шаблон cloud-init (Jinja), который подставляется при создании ВМ из образа. Обязателен для образов Маркетплейса
      --zone string                  Зона доступности, в которой собирать образ: имя или UUID. По умолчанию — зона из настроек, а если она не задана — зона диска-источника. Должна совпадать с зоной диска
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo image](#evo-image)	 - Образы ВМ: операционные системы и продукты маркетплейса

## evo image delete

Удалить образ ВМ

### Synopsis

```text
Удалить образ ВМ.

Удалить образ ВМ. Работающие ВМ продолжают работать, но новую ВМ из образа уже не создать.

Вызывает инструмент image_delete (подробнее: 'evo tools describe image_delete').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo image delete <image-id> [flags]
```

### Examples

```
  evo image delete <image-id> --dry-run
  evo image delete <image-id> --yes
```

### Options

```
  -h, --help   Справка по delete
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo image](#evo-image)	 - Образы ВМ: операционные системы и продукты маркетплейса

## evo image get

Образ по имени или UUID: минимум ресурсов, зоны, поля и кто их заполнит, команда создания ВМ

### Synopsis

```text
Образ по имени или UUID: минимум ресурсов, зоны, поля и кто их заполнит, команда создания ВМ.

Образ ВМ по имени, отображаемому имени или UUID. Поиск — как в 'evo image list': точное имя, точное отображаемое имя, затем единственное совпадение по подстроке; при нескольких совпадениях — ошибка со списком кандидатов. Показывает, готов ли образ и в каких зонах, и поля метаданных образа с тем, кто заполнит каждое: evo (SSH-ключ, имя ВМ, логин, случайный пароль), значение по умолчанию, пользователь (обязательное поле, которое evo заполнить не может: задаётся через --meta команды 'evo vm create') или никто. В конце — команда создания ВМ из образа. Образ, недоступный проекту, показывается с пояснением и без команды создания. Значок и шаблон cloud-init (user_data_template) — только с --full. Только чтение. Так же следят за сборкой образа: повторяйте команду, пока образ не станет готов.

Вызывает инструмент image_get (подробнее: 'evo tools describe image_get').
```

```
evo image get <image-id|image> [flags]
```

### Examples

```
  evo image get AstraLinuxSmolensk1.8.3KES
  evo image get <UUID> --json
```

### Options

```
      --full   Вывести и значок SVG, и шаблон cloud-init user_data_template (большой объём)
  -h, --help   Справка по get
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo image](#evo-image)	 - Образы ВМ: операционные системы и продукты маркетплейса

## evo image list

Поиск образов: ОС (public), маркетплейс (marketplace), образы проекта (private)

### Synopsis

```text
Поиск образов: ОС (public), маркетплейс (marketplace), образы проекта (private).

Поиск по каталогу образов ВМ: образы ОС (тип public), продукты Маркетплейса Cloud.ru (marketplace) и собственные образы проекта (private). Запрос ищет подстроку в имени или отображаемом имени без учёта регистра. Образы, не готовые ни в одной зоне, и образы общего каталога, недоступные проекту, скрыты; --all показывает и их (недоступные — с пометкой). В строке — id, имя, отображаемое имя, тип, зоны, где образ готов, и минимальный размер диска; образы ОС идут первыми. Только чтение. Какие поля нужны ВМ из образа — 'evo image get <образ>'; имя или id образа передаётся в --image или --image-id команды 'evo vm create'.

Вызывает инструмент image_list (подробнее: 'evo tools describe image_list').
```

```
evo image list [query] [flags]
```

### Examples

```
  evo image list astra
  evo image list --type public
  evo image list ubuntu --zone ru.AZ-2 --json | jq -r '.images[] | "\(.id)\t\(.name)"'
```

### Options

```
      --all           Показать и образы, скрытые по умолчанию: не готовые ни в одной зоне и недоступные проекту (общий каталог содержит тестовые и неисправные образы и образы, из которых ВМ этого проекта не создать)
  -h, --help          Справка по list
      --type string   Только этот тип: public (образы ОС), marketplace (продукты Маркетплейса Cloud.ru) или private (образы проекта). По умолчанию — все три
      --zone string   Только образы, готовые в этой зоне доступности: имя (ru.AZ-1, AZ-1) или UUID
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo image](#evo-image)	 - Образы ВМ: операционные системы и продукты маркетплейса

## evo init

Создать .evo.yaml с зоной и образом из API Cloud.ru

### Synopsis

```text
Создать .evo.yaml для этого проекта.

Зона доступности и загрузочный образ берутся из API Cloud.ru (нужен ключ
доступа). На терминале их выбирают из нумерованного списка. Первыми идут
зоны, где у проекта есть подсеть (в другую deploy ВМ не подключит; в новом
проекте подсеть есть только в ru.AZ-1): ru.AZ-2, если подсеть есть в ней
(15.10.2026 в ru.AZ-1 ВМ не создавались), иначе зона основной подсети
проекта. С --non-interactive или без терминала берутся первая зона и самый
новый обычный образ Ubuntu (если его нет — Debian), выбор печатается. --image задаёт образ по имени (регистр не важен) или UUID: имя
пишется в .evo.yaml так, как в каталоге (image:), UUID — как image_id:.
deploy нужен Ubuntu или Debian: образы Astra Linux, Альт и РЕД ОС
отклоняются (ВМ из них создаёт evo vm create).
```

```
evo init [flags]
```

### Examples

```
  evo init
  evo init --non-interactive
  evo init --image debian-12
```

### Options

```
  -h, --help              Справка по init
      --image string      Загрузочный образ по имени или UUID (evo image list) вместо самого нового Ubuntu
      --non-interactive   Без вопросов взять первую зону (ru.AZ-2, если в ней есть подсеть проекта) и предпочтительный образ
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов

## evo k8s

Кластеры Managed Kubernetes

```
evo k8s [flags]
```

### Options

```
  -h, --help   Справка по k8s
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo k8s add-addon](#evo-k8s-add-addon)	 - Установить плагин в кластер (асинхронно; версия по умолчанию — последняя активная)
* [evo k8s add-nodepool](#evo-k8s-add-nodepool)	 - Добавить группу рабочих узлов (платно, асинхронно)
* [evo k8s addon-versions](#evo-k8s-addon-versions)	 - Версии плагина: режим поддержки, требования к кластеру, схема настроек
* [evo k8s addons](#evo-k8s-addons)	 - Каталог плагинов Kubernetes: поиск по имени и описанию, категория, платность
* [evo k8s apply](#evo-k8s-apply)	 - Применить манифест Kubernetes (как kubectl apply -f -)
* [evo k8s create](#evo-k8s-create)	 - Шаг 2: создать кластер по расчёту k8s plan (платно, асинхронно, около 10 минут)
* [evo k8s delete](#evo-k8s-delete)	 - Удалить кластер Managed Kubernetes (асинхронно)
* [evo k8s get](#evo-k8s-get)	 - Кластер Managed Kubernetes по ID
* [evo k8s helm](#evo-k8s-helm)	 - Helm в кластере: install, upgrade, uninstall, list, status
* [evo k8s kubeconfig](#evo-k8s-kubeconfig)	 - kubeconfig кластера (по умолчанию со встроенным токеном примерно на час)
* [evo k8s kubectl](#evo-k8s-kubectl)	 - Команда kubectl в кластере (изменяющие — с подтверждением)
* [evo k8s list](#evo-k8s-list)	 - Кластеры Managed Kubernetes проекта
* [evo k8s nodepools](#evo-k8s-nodepools)	 - Группы узлов кластера
* [evo k8s options](#evo-k8s-options)	 - Версии Kubernetes, flavor узлов и зоны для кластеров
* [evo k8s plan](#evo-k8s-plan)	 - Шаг 1: рассчитать конфигурацию кластера, ничего не создавая

## evo k8s add-addon

Установить плагин в кластер (асинхронно; версия по умолчанию — последняя активная)

### Synopsis

```text
Установить плагин в кластер (асинхронно; версия по умолчанию — последняя активная).

Установить версию плагина Managed Kubernetes в кластер. Превью показывает выбранную версию (по умолчанию — новейшая ACTIVE), namespace, требования версии и проверки кластера: версию Kubernetes и группы узлов против требований плагина. Что определить не удалось, перечислено в not_checked; явное несоответствие — отказ до установки.

Операция асинхронная: ответ — состояние installing с operation_id и cluster_addon_id. Готовность — 'evo k8s kubectl <cluster-id> get pods -n <namespace>' (или -A): поды плагина должны стать Running и Ready (поды заданий Job, например cert-manager-startupapicheck, завершаются в Completed). Поды в ImagePullBackOff могут означать, что у кластера нет выхода в интернет: в Cloud.ru для этого нужен публичный SNAT-шлюз в зоне узлов ('evo snat create'). --resolve-mode OVERWRITE может заменить объекты, уже созданные в кластере.

Вызывает инструмент k8s_add_addon (подробнее: 'evo tools describe k8s_add_addon').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo k8s add-addon <cluster-id> <name> [flags]
```

### Examples

```
  evo k8s add-addon <cluster-id> cert-manager --dry-run
  evo k8s add-addon <cluster-id> cert-manager --config settings.json
```

### Options

```
      --config string         Файл со значением ('-' — из stdin до EOF: на терминале — Ctrl-D). Настройки плагина по схеме его версии (evo k8s addon-versions <плагин> --full): объект JSON или строка с объектом JSON. Cloud.ru подставляет в них {{clusterID}}, {{clusterName}}, {{productInstanceID}}, {{nodeCIDR}} и {{serviceCIDR}}. По умолчанию облако берёт последние настройки плагина в кластере (при первой установке — настройки по умолчанию)
  -h, --help                  Справка по add-addon
      --resolve-mode string   Что делать с объектами плагина, уже созданными в кластере вручную: NONE — оставить (установка тогда может не пройти), OVERWRITE — сбросить к рекомендуемым значениям. По умолчанию NONE
      --version string        Версия приложения из evo k8s addon-versions. Облако устанавливает плагин по версии приложения и само выбирает addon_version, поэтому addon_version принимается, только если она новейшая для своей версии приложения. По умолчанию — новейшая ACTIVE
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Кластеры Managed Kubernetes

## evo k8s add-nodepool

Добавить группу рабочих узлов (платно, асинхронно)

### Synopsis

```text
Добавить группу рабочих узлов (платно, асинхронно).

Добавить в кластер Managed Kubernetes группу рабочих узлов — на них запускаются поды; в новом кластере рабочих узлов нет. Группа узлов платная. Операция асинхронная. Flavor (по --cpu и --ram-gb или по имени), подсеть узлов и группу безопасности evo подбирает сам. Сервис групп узлов Cloud.ru иногда отвечает 503 'no healthy upstream' — это временный сбой: повторите команду.

Вызывает инструмент k8s_add_nodepool (подробнее: 'evo tools describe k8s_add_nodepool').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo k8s add-nodepool <cluster-id> [flags]
```

### Options

```
      --count int                  Число рабочих узлов, постоянное (по умолчанию 2)
      --cpu int                    vCPU узла для автоподбора flavor (по умолчанию 2)
      --disk-gb int                Диск узла, ГБ, не меньше 10 (по умолчанию 20)
      --flavor string              Flavor узлов (список: evo k8s options); заменяет --cpu и --ram-gb
  -h, --help                       Справка по add-nodepool
      --name string                Имя группы узлов, 4–60 символов (по умолчанию nodepool-1)
      --nodes-subnet-id string     UUID подсети рабочих узлов (по умолчанию — подходящая подсеть зоны)
      --ram-gb int                 RAM узла, ГБ, для автоподбора flavor (по умолчанию 4)
      --security-group-id string   UUID группы безопасности узлов (по умолчанию — группа зоны с доступом по SSH)
      --zone string                Зона (по умолчанию — зона мастер-узлов кластера; если кластер её не сообщает — отказ)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Кластеры Managed Kubernetes

## evo k8s addon-versions

Версии плагина: режим поддержки, требования к кластеру, схема настроек

### Synopsis

```text
Версии плагина: режим поддержки, требования к кластеру, схема настроек.

Версии (релизы) плагина Managed Kubernetes, новые первыми: версия приложения (её принимает 'evo k8s add-addon --version') и addon_version, режим поддержки (ACTIVE, EXPERIMENTAL, END_OF_SUPPORT, END_OF_LIFE, INACTIVE), диапазон версий Kubernetes, которому должен соответствовать кластер, сколько нужно готовых рабочих узлов, namespace, есть ли JSON Schema настроек и её размер. Отмечена версия, которую 'evo k8s add-addon' берёт по умолчанию, — новейшая ACTIVE. Сама схема (до ~94 КБ) — только с --full. Только чтение.

Вызывает инструмент k8s_addon_versions (подробнее: 'evo tools describe k8s_addon_versions').
```

```
evo k8s addon-versions <name> [flags]
```

### Examples

```
  evo k8s addon-versions cert-manager
  evo k8s addon-versions cert-manager --full --json | jq '.releases[0].configuration_schema'
```

### Options

```
      --full   Вывести и схему настроек (configuration_schema) каждой версии (большой объём)
  -h, --help   Справка по addon-versions
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Кластеры Managed Kubernetes

## evo k8s addons

Каталог плагинов Kubernetes: поиск по имени и описанию, категория, платность

### Synopsis

```text
Каталог плагинов Kubernetes: поиск по имени и описанию, категория, платность.

Поиск по каталогу плагинов Managed Kubernetes — плагинов, собранных Cloud.ru: ingress, cert-manager, мониторинг, драйверы CSI и другие. В строке — имя, отображаемое имя, категории, платность и команда установки; категории — слова консоли (например, «Сети», «Безопасность», «Мониторинг»), в ответе перечислены все. Только чтение. Версии и требования плагина — 'evo k8s addon-versions <плагин>', установка — 'evo k8s add-addon'.

Вызывает инструмент k8s_addons (подробнее: 'evo tools describe k8s_addons').
```

```
evo k8s addons [query] [flags]
```

### Examples

```
  evo k8s addons cert
  evo k8s addons --category Сети
  evo k8s addons --json | jq -r '.addons[].name'
```

### Options

```
      --billable          true — только платные плагины, false — только бесплатные. По умолчанию — все
      --category string   Только плагины этой категории: точное совпадение без учёта регистра (категории перечислены в ответе)
  -h, --help              Справка по addons
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Кластеры Managed Kubernetes

## evo k8s apply

Применить манифест Kubernetes (как kubectl apply -f -)

### Synopsis

```text
Применить манифест Kubernetes (как kubectl apply -f -).

Применить манифест Kubernetes (YAML или JSON, можно несколько документов) к кластеру — как 'kubectl apply -f -'. Для развёртывания и обновления рабочих нагрузок: Deployment, StatefulSet, Service, ServiceMonitor и других. Новому кластеру нужен выход в интернет, чтобы скачивать публичные образы (SNAT-шлюз: 'evo snat create'), а CRD (например, ServiceMonitor) должны существовать до объектов, которые их используют.

Вызывает инструмент k8s_apply (подробнее: 'evo tools describe k8s_apply').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo k8s apply <cluster-id> [flags]
```

### Options

```
  -h, --help               Справка по apply
      --manifest string    Манифест YAML или JSON (можно несколько документов)
      --namespace string   Namespace, в который применить манифест (необязательно; манифест может задать свой)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Кластеры Managed Kubernetes

## evo k8s create

Шаг 2: создать кластер по расчёту k8s plan (платно, асинхронно, около 10 минут)

### Synopsis

```text
Шаг 2: создать кластер по расчёту k8s plan (платно, асинхронно, около 10 минут).

Шаг 2: создать кластер Managed Kubernetes по расчёту 'evo k8s plan' — с теми же флагами. Кластер платный. Операция асинхронная: ответ содержит cluster_id и operation_id, кластер создаётся около 10 минут (по документации — 5–10); состояние — 'evo k8s get <cluster-id>'.

Вызывает инструмент k8s_create (подробнее: 'evo tools describe k8s_create').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo k8s create [flags]
```

### Options

```
      --cluster-sa-id string   UUID сервисного аккаунта кластера (как в evo k8s plan)
      --cpu int                vCPU мастер-узла (как в evo k8s plan)
      --flavor string          Flavor мастер-узлов (как в evo k8s plan)
  -h, --help                   Справка по create
      --kube-api-internet      Опубликовать kube-apiserver в интернет. Выключить: --kube-api-internet=false (по умолчанию true)
      --master-count int       Число мастер-узлов: 1, 3 или 5 (по умолчанию 1)
      --name string            Имя кластера (как в evo k8s plan)
      --product string         Назначение кластера (как в evo k8s plan)
      --ram-gb int             RAM мастер-узла, ГБ (как в evo k8s plan)
      --subnet-id string       UUID подсети для VIP мастер-узлов, в зоне кластера (как в evo k8s plan)
      --version string         Версия Kubernetes (как в evo k8s plan)
      --zone string            Зона: имя или UUID (как в evo k8s plan)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Кластеры Managed Kubernetes

## evo k8s delete

Удалить кластер Managed Kubernetes (асинхронно)

### Synopsis

```text
Удалить кластер Managed Kubernetes (асинхронно).

Удалить кластер Managed Kubernetes безвозвратно. Операция асинхронная.

Вызывает инструмент k8s_delete (подробнее: 'evo tools describe k8s_delete').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo k8s delete <cluster-id> [flags]
```

### Options

```
  -h, --help   Справка по delete
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Кластеры Managed Kubernetes

## evo k8s get

Кластер Managed Kubernetes по ID

### Synopsis

```text
Кластер Managed Kubernetes по ID.

Все сведения о кластере Managed Kubernetes по id, в том числе состояние. Только чтение.

Вызывает инструмент k8s_get (подробнее: 'evo tools describe k8s_get').
```

```
evo k8s get <cluster-id> [flags]
```

### Options

```
  -h, --help   Справка по get
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Кластеры Managed Kubernetes

## evo k8s helm

Helm в кластере: install, upgrade, uninstall, list, status

### Synopsis

```text
Helm в кластере: install, upgrade, uninstall, list, status.

Helm в кластере: действие <action> — install, upgrade, uninstall, list или status. install и upgrade идемпотентны (helm upgrade --install --wait). --repo-url сначала добавляет репозиторий чартов. Значения — --values (содержимое values.yaml) и --set (ключ=значение). Изменяющие действия — с подтверждением. Подходит для типовых продуктов (например, kube-prometheus-stack, базы данных). Узлам нужен выход в интернет, чтобы скачать образы чарта (SNAT-шлюз: 'evo snat create'); размер группы узлов подбирайте под чарт: стекам мониторинга нужны узлы от 8 ГБ.

Вызывает инструмент k8s_helm (подробнее: 'evo tools describe k8s_helm').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo k8s helm <cluster-id> <action> [flags]
```

### Options

```
      --chart string       Чарт, например prometheus-community/kube-prometheus-stack (для install и upgrade)
  -h, --help               Справка по helm
      --namespace string   Namespace (создаётся при install и upgrade)
      --release string     Имя релиза Helm (для install, upgrade, uninstall и status)
      --repo-url string    URL репозитория чартов: сначала добавить и обновить его; имя репозитория — из префикса <repo>/ в --chart
      --set stringArray    Значение Helm, как в helm --set: ключ=значение (необязательно) (флаг повторяется или значения через запятую)
      --values string      Содержимое values.yaml (необязательно)
      --version string     Версия чарта (необязательно; по умолчанию — последняя)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Кластеры Managed Kubernetes

## evo k8s kubeconfig

kubeconfig кластера (по умолчанию со встроенным токеном примерно на час)

### Synopsis

```text
kubeconfig кластера (по умолчанию со встроенным токеном примерно на час).

kubeconfig кластера Managed Kubernetes. По умолчанию в него встроен статический токен, и kubectl и helm работают сразу: исходный kubeconfig Cloud.ru требует exec-плагин cloudlogin, без него kubectl завершается ошибкой 'executable cloudlogin not found'. Токен действует около часа. Исходный kubeconfig с exec-плагином — --embed-token=false.

Вызывает инструмент k8s_kubeconfig (подробнее: 'evo tools describe k8s_kubeconfig').
```

```
evo k8s kubeconfig <cluster-id> [flags]
```

### Examples

```
  evo k8s kubeconfig <cluster-id> > kubeconfig.yaml
  evo k8s kubeconfig <cluster-id> --embed-token=false > kubeconfig.yaml
```

### Options

```
      --embed-token      Встроить статический токен, чтобы kubectl работал без плагина cloudlogin. Выключить: --embed-token=false (по умолчанию true)
  -h, --help             Справка по kubeconfig
      --network string   Адрес API кластера: NETWORK_TYPE_PUBLIC или NETWORK_TYPE_PRIVATE (по умолчанию — как настроено в кластере)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Кластеры Managed Kubernetes

## evo k8s kubectl

Команда kubectl в кластере (изменяющие — с подтверждением)

### Synopsis

```text
Команда kubectl в кластере (изменяющие — с подтверждением).

Любая команда kubectl в кластере; токен встраивается сам, плагин cloudlogin не нужен. Читающие команды (get, describe, logs, top, events, rollout status, explain и другие) выполняются сразу, изменяющие (create, delete, patch, scale, label, rollout restart, exec, cordon и другие) — с подтверждением; с подтверждением и любая команда с -f/--filename, -k/--kustomize, --output-directory, --cache-dir или форматом вывода из файла (-o go-template-file=…, jsonpath-file=…, custom-columns-file=…): она читает файлы или любой URL либо пишет на хост (-f у logs — это --follow). Подходит для всего, чего нет в 'evo k8s helm' и 'evo k8s apply'. Примеры аргументов: 'get pods -A', '-n db logs deploy/postgres', 'create secret generic s --from-literal=k=v'.

Вызывает инструмент k8s_kubectl (подробнее: 'evo tools describe k8s_kubectl').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
Флаги evo (--json, --yes, --dry-run, --profile) ставятся перед аргументами: всё после первого аргумента передаётся инструменту как есть.
```

```
evo k8s kubectl <cluster-id> <args>... [flags]
```

### Options

```
  -h, --help   Справка по kubectl
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Кластеры Managed Kubernetes

## evo k8s list

Кластеры Managed Kubernetes проекта

### Synopsis

```text
Кластеры Managed Kubernetes проекта.

Кластеры Managed Kubernetes проекта: id, имя, состояние. Только чтение.

Вызывает инструмент k8s_list (подробнее: 'evo tools describe k8s_list').
```

```
evo k8s list [flags]
```

### Examples

```
  evo k8s list
```

### Options

```
  -h, --help   Справка по list
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Кластеры Managed Kubernetes

## evo k8s nodepools

Группы узлов кластера

### Synopsis

```text
Группы узлов кластера.

Группы узлов кластера Managed Kubernetes: имя, состояние, сколько узлов задано и сколько готово. Только чтение. Списка по всем кластерам в API нет: id кластера — в 'evo k8s list'.

Вызывает инструмент k8s_nodepools (подробнее: 'evo tools describe k8s_nodepools').
```

```
evo k8s nodepools <cluster-id> [flags]
```

### Options

```
  -h, --help   Справка по nodepools
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Кластеры Managed Kubernetes

## evo k8s options

Версии Kubernetes, flavor узлов и зоны для кластеров

### Synopsis

```text
Версии Kubernetes, flavor узлов и зоны для кластеров.

Доступные версии Kubernetes (по каналам выпуска), flavor узлов и зоны для кластеров Managed Kubernetes. Только чтение. Нужны, чтобы задать флаги 'evo k8s plan' вручную.

Вызывает инструмент k8s_options (подробнее: 'evo tools describe k8s_options').
```

```
evo k8s options [flags]
```

### Options

```
  -h, --help   Справка по options
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Кластеры Managed Kubernetes

## evo k8s plan

Шаг 1: рассчитать конфигурацию кластера, ничего не создавая

### Synopsis

```text
Шаг 1: рассчитать конфигурацию кластера, ничего не создавая.

Шаг 1 создания кластера Managed Kubernetes: рассчитать конфигурацию — версию Kubernetes, flavor и число мастер-узлов, подсеть, сервисный аккаунт, — ничего не создавая. Что не задано флагами, evo подбирает сам. Проверьте расчёт и создайте кластер командой 'evo k8s create' с теми же флагами.

Вызывает инструмент k8s_plan (подробнее: 'evo tools describe k8s_plan').
```

```
evo k8s plan [flags]
```

### Options

```
      --cluster-sa-id string   UUID сервисного аккаунта кластера (по умолчанию — сервисный аккаунт ключа доступа evo)
      --cpu int                vCPU мастер-узла для автоподбора flavor (по умолчанию 2)
      --flavor string          Flavor мастер-узлов (список: evo k8s options); заменяет --cpu и --ram-gb
  -h, --help                   Справка по plan
      --kube-api-internet      Опубликовать kube-apiserver в интернет. Выключить: --kube-api-internet=false (по умолчанию true)
      --master-count int       Число мастер-узлов: 1, 3 или 5 (по умолчанию 1); другие значения отклоняются
      --name string            Имя кластера, 3–60 символов (по умолчанию k8s-<значение --product>)
      --product string         Назначение кластера (используется в имени)
      --ram-gb int             RAM мастер-узла, ГБ, для автоподбора flavor (по умолчанию 4)
      --subnet-id string       UUID подсети для приватного VIP мастер-узлов, в зоне кластера (по умолчанию — основная подсеть зоны). Подсеть другой зоны отклоняется
      --version string         Версия Kubernetes, например v1.34.6 (по умолчанию — новейшая STABLE; список: evo k8s options)
      --zone string            Зона доступности мастер-узлов: имя или UUID, например ru.AZ-2 (по умолчанию — зона --subnet-id, иначе зона по умолчанию сервиса)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo k8s](#evo-k8s)	 - Кластеры Managed Kubernetes

## evo kafka

Кластеры Managed Kafka

```
evo kafka [flags]
```

### Options

```
  -h, --help   Справка по kafka
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo kafka create](#evo-kafka-create)	 - Создать кластер Managed Kafka (платно, асинхронно)
* [evo kafka delete](#evo-kafka-delete)	 - Удалить кластер Kafka вместе с данными (асинхронно)
* [evo kafka get](#evo-kafka-get)	 - Кластер Kafka по ID: состояние, адреса, брокеры, версия
* [evo kafka list](#evo-kafka-list)	 - Кластеры Managed Kafka проекта
* [evo kafka operation](#evo-kafka-operation)	 - Состояние асинхронной операции Kafka
* [evo kafka specs](#evo-kafka-specs)	 - Конфигурации кластера Kafka для версии: CPU, память, хранилище, брокеры
* [evo kafka versions](#evo-kafka-versions)	 - Доступные версии Managed Kafka

## evo kafka create

Создать кластер Managed Kafka (платно, асинхронно)

### Synopsis

```text
Создать кластер Managed Kafka (платно, асинхронно).

Создать кластер Managed Kafka. Имя, которое уже носит кластер проекта ('evo kafka list'), отклоняется. evo подбирает версию (последнюю), подсеть (основную подсеть зоны из настроек, иначе первой зоны, где она есть, начиная с ru.AZ-2) и по ней — зону кластера, конфигурацию этой зоны (по --specification-id или по --cpu и --ram; если подходящей нет — ошибка со списком конфигураций), хранилище (минимум конфигурации) и число брокеров (min_brokers конфигурации; живой API его не сообщает, тогда 1 — без репликации). Кластер платный. Операция асинхронная: ход — 'evo kafka operation <operation-id>'.

Вызывает инструмент kafka_create (подробнее: 'evo tools describe kafka_create').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo kafka create <name> [flags]
```

### Examples

```
  evo kafka create events --dry-run
  evo kafka create events --cpu 2 --ram 8 --brokers 3 --yes
```

### Options

```
      --brokers int               Число брокеров. По умолчанию — min_brokers конфигурации, иначе 1 (один брокер — без репликации; для реплицируемого кластера — 3). Проверяется по min_brokers и max_brokers конфигурации, если они есть
      --cpu int                   Число vCPU — для автоподбора конфигурации, если --specification-id не задан
      --description string        Описание (необязательно)
  -h, --help                      Справка по create
      --ram int                   RAM, ГБ — вместе с --cpu
      --specification-id string   UUID конфигурации (список: evo kafka specs). Необязателен, если заданы --cpu и --ram
      --storage-gib int           Размер диска, ГиБ (по умолчанию — минимум конфигурации)
      --subnet-id string          UUID подсети (список: evo vpc subnets). По умолчанию — основная подсеть зоны из настроек. Кластер создаётся в зоне подсети
      --ui-enabled                Включить Kafka UI (по умолчанию false)
      --version-id string         UUID версии Kafka (список: evo kafka versions). По умолчанию — последняя
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo kafka](#evo-kafka)	 - Кластеры Managed Kafka

## evo kafka delete

Удалить кластер Kafka вместе с данными (асинхронно)

### Synopsis

```text
Удалить кластер Kafka вместе с данными (асинхронно).

Удалить кластер Managed Kafka по id вместе с данными. Операция асинхронная.

Вызывает инструмент kafka_delete (подробнее: 'evo tools describe kafka_delete').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo kafka delete <cluster-id> [flags]
```

### Examples

```
  evo kafka delete <cluster-id> --dry-run
  evo kafka delete <cluster-id> --yes
```

### Options

```
  -h, --help   Справка по delete
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo kafka](#evo-kafka)	 - Кластеры Managed Kafka

## evo kafka get

Кластер Kafka по ID: состояние, адреса, брокеры, версия

### Synopsis

```text
Кластер Kafka по ID: состояние, адреса, брокеры, версия.

Кластер Managed Kafka по id: состояние, адреса (kafka_hosts), брокеры, версия. Только чтение.

Вызывает инструмент kafka_get (подробнее: 'evo tools describe kafka_get').
```

```
evo kafka get <cluster-id> [flags]
```

### Examples

```
  evo kafka get <cluster-id>
  evo kafka get <cluster-id> --json
```

### Options

```
  -h, --help   Справка по get
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo kafka](#evo-kafka)	 - Кластеры Managed Kafka

## evo kafka list

Кластеры Managed Kafka проекта

### Synopsis

```text
Кластеры Managed Kafka проекта.

Кластеры Managed Kafka: id, имя, состояние, брокеры, версия. Только чтение.

Вызывает инструмент kafka_list (подробнее: 'evo tools describe kafka_list').
```

```
evo kafka list [flags]
```

### Examples

```
  evo kafka list
  evo kafka list --json | jq -r '.clusters[] | "\(.id)\t\(.name)\t\(.status)"'
```

### Options

```
  -h, --help   Справка по list
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo kafka](#evo-kafka)	 - Кластеры Managed Kafka

## evo kafka operation

Состояние асинхронной операции Kafka

### Synopsis

```text
Состояние асинхронной операции Kafka.

Состояние асинхронной операции Managed Kafka по id — его возвращают создание и удаление кластера: done — завершена ли операция, error — ошибка. С --wait-seconds команда сама ждёт завершения до N секунд и выводит итоговое состояние. Только чтение.

Вызывает инструмент kafka_operation (подробнее: 'evo tools describe kafka_operation').
```

```
evo kafka operation <operation-id> [flags]
```

### Examples

```
  evo kafka operation <operation-id>
  evo kafka operation <operation-id> --wait-seconds 300
```

### Options

```
  -h, --help               Справка по operation
      --wait-seconds int   Ждать итогового состояния до N секунд (не больше 300), опрашивая операцию с нарастающим интервалом. Без флага — одна проверка
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo kafka](#evo-kafka)	 - Кластеры Managed Kafka

## evo kafka specs

Конфигурации кластера Kafka для версии: CPU, память, хранилище, брокеры

### Synopsis

```text
Конфигурации кластера Kafka для версии: CPU, память, хранилище, брокеры.

Конфигурации кластера Managed Kafka для версии: id, CPU, память (ГБ), минимальный объём хранилища, наименьшее и наибольшее число брокеров. Только чтение. id конфигурации выбирают для создания кластера ('evo kafka create').

Вызывает инструмент kafka_specs (подробнее: 'evo tools describe kafka_specs').
```

```
evo kafka specs [flags]
```

### Examples

```
  evo kafka specs
  evo kafka specs --version-id <version-id> --zone ru.AZ-2
```

### Options

```
  -h, --help                Справка по specs
      --version-id string   UUID версии (список: evo kafka versions); необязательно — по умолчанию последняя
      --zone string         Зона доступности: имя (ru.AZ-2) или UUID; необязательно — по умолчанию зона из настроек, иначе ru.AZ-2. API нужна зона
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo kafka](#evo-kafka)	 - Кластеры Managed Kafka

## evo kafka versions

Доступные версии Managed Kafka

### Synopsis

```text
Доступные версии Managed Kafka.

Доступные версии Managed Kafka: id и имя. Только чтение. Нужны перед созданием кластера ('evo kafka create').

Вызывает инструмент kafka_versions (подробнее: 'evo tools describe kafka_versions').
```

```
evo kafka versions [flags]
```

### Examples

```
  evo kafka versions
```

### Options

```
  -h, --help   Справка по versions
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo kafka](#evo-kafka)	 - Кластеры Managed Kafka

## evo lb

Балансировщики нагрузки (L4)

```
evo lb [flags]
```

### Options

```
  -h, --help   Справка по lb
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo lb create](#evo-lb-create)	 - Создать балансировщик L4 перед группой целей (платно, асинхронно)
* [evo lb delete](#evo-lb-delete)	 - Удалить балансировщик (асинхронно)
* [evo lb get](#evo-lb-get)	 - Балансировщик по ID: правила, обработчики, адреса, зоны
* [evo lb list](#evo-lb-list)	 - Балансировщики нагрузки проекта (L4)
* [evo lb operation](#evo-lb-operation)	 - Состояние асинхронной операции балансировщика

## evo lb create

Создать балансировщик L4 перед группой целей (платно, асинхронно)

### Synopsis

```text
Создать балансировщик L4 перед группой целей (платно, асинхронно).

Создать балансировщик нагрузки L4 перед группой целей. evo определяет VPC (по группе целей), зону (--zone, иначе зона --subnet-id, иначе зона ВМ группы целей, иначе зона из настроек; без зоны — отказ) и подсеть (основная подсеть зоны в этом VPC) и создаёт правило с обработчиком (--port → --target-port) и проверкой состояния по TCP. По умолчанию балансировщик получает публичный IP (--external). Балансировщик платный. Операция асинхронная: ход — 'evo lb operation <operation-id>'.

Вызывает инструмент lb_create (подробнее: 'evo tools describe lb_create').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo lb create <name> [flags]
```

### Examples

```
  evo lb create web-lb --target-group-id <tg-id> --port 80 --dry-run
  evo lb create web-lb --target-group-id <tg-id> --port 443 --target-port 8443 --yes
  evo lb create db-lb --target-group-id <tg-id> --port 5432 --external=false --yes
```

### Options

```
      --external                 Выделить публичный IP; без него балансировщик только внутренний. Выключить: --external=false (по умолчанию true)
      --hc-port int              Порт проверки состояния (по умолчанию — --target-port)
  -h, --help                     Справка по create
      --port int                 Порт обработчика, на который балансировщик принимает трафик (обязательный)
      --subnet-id string         UUID подсети в зоне балансировщика (по умолчанию — основная подсеть зоны в VPC балансировщика)
      --target-group-id string   UUID группы целей, на которую балансировщик распределяет трафик (список: evo tg list), обязательный
      --target-port int          Порт на целях, куда перенаправляется трафик (по умолчанию — --port)
      --vpc-id string            UUID VPC (по умолчанию — VPC группы целей)
      --zone string              Зона доступности, которую обслуживает балансировщик: имя или UUID (список: evo vm zones). По умолчанию — зона --subnet-id, иначе зона ВМ группы целей, иначе зона из настроек
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo lb](#evo-lb)	 - Балансировщики нагрузки (L4)

## evo lb delete

Удалить балансировщик (асинхронно)

### Synopsis

```text
Удалить балансировщик (асинхронно).

Удалить балансировщик по id (список: 'evo lb list'). Операция асинхронная.

Вызывает инструмент lb_delete (подробнее: 'evo tools describe lb_delete').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo lb delete <id> [flags]
```

### Examples

```
  evo lb delete <lb-id> --dry-run
  evo lb delete <lb-id> --yes
```

### Options

```
  -h, --help   Справка по delete
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo lb](#evo-lb)	 - Балансировщики нагрузки (L4)

## evo lb get

Балансировщик по ID: правила, обработчики, адреса, зоны

### Synopsis

```text
Балансировщик по ID: правила, обработчики, адреса, зоны.

Все сведения об одном балансировщике по id (список: 'evo lb list'): правила, обработчики, адреса, зоны доступности, состояние. Только чтение.

Вызывает инструмент lb_get (подробнее: 'evo tools describe lb_get').
```

```
evo lb get <id> [flags]
```

### Examples

```
  evo lb get <lb-id>
  evo lb get <lb-id> --json
```

### Options

```
  -h, --help   Справка по get
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo lb](#evo-lb)	 - Балансировщики нагрузки (L4)

## evo lb list

Балансировщики нагрузки проекта (L4)

### Synopsis

```text
Балансировщики нагрузки проекта (L4).

Балансировщики нагрузки Evolution (L4, сетевые) проекта: id, имя, состояние, VPC, внешний IP. Только чтение.

Вызывает инструмент lb_list (подробнее: 'evo tools describe lb_list').
```

```
evo lb list [flags]
```

### Examples

```
  evo lb list
  evo lb list --json | jq -r '.load_balancers[] | "\(.id)\t\(.name)"'
```

### Options

```
  -h, --help   Справка по list
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo lb](#evo-lb)	 - Балансировщики нагрузки (L4)

## evo lb operation

Состояние асинхронной операции балансировщика

### Synopsis

```text
Состояние асинхронной операции балансировщика.

Состояние асинхронной операции балансировщика по id — его возвращают создание и удаление балансировщиков и групп целей: done — завершена ли операция, error — ошибка. С --wait-seconds команда сама ждёт завершения до N секунд и выводит итоговое состояние. Только чтение.

Вызывает инструмент lb_operation (подробнее: 'evo tools describe lb_operation').
```

```
evo lb operation <operation-id> [flags]
```

### Examples

```
  evo lb operation <operation-id>
  evo lb operation <operation-id> --wait-seconds 300
```

### Options

```
  -h, --help               Справка по operation
      --wait-seconds int   Ждать итогового состояния до N секунд (не больше 300), опрашивая операцию с нарастающим интервалом. Без флага — одна проверка
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo lb](#evo-lb)	 - Балансировщики нагрузки (L4)

## evo logs

Журналы контейнеров на развёрнутой ВМ

```
evo logs [flags]
```

### Examples

```
  evo logs
  evo logs -s web -n 50
  evo logs -f
```

### Options

```
  -f, --follow           Следить за журналом (до Ctrl-C)
  -h, --help             Справка по logs
  -s, --service string   Только журнал этого сервиса
  -n, --tail int         Сколько последних строк показать (0 — все) (по умолчанию 100)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов

## evo marketplace

Витрина: образы ВМ и плагины Kubernetes с командой развёртывания каждого

```
evo marketplace [flags]
```

### Options

```
  -h, --help   Справка по marketplace
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo marketplace list](#evo-marketplace-list)	 - Витрина: ОС, продукты маркетплейса и плагины Kubernetes одним списком, с командой развёртывания

## evo marketplace list

Витрина: ОС, продукты маркетплейса и плагины Kubernetes одним списком, с командой развёртывания

### Synopsis

```text
Витрина: ОС, продукты маркетплейса и плагины Kubernetes одним списком, с командой развёртывания.

Витрина того, что Cloud.ru предлагает развернуть, одним списком: образы ВМ общего каталога (образы ОС и продукты Маркетплейса Cloud.ru; собственные образы проекта не входят) и плагины Managed Kubernetes. Только чтение.

Вид (--kind): vm-os — образ ОС (тип public или образ маркетплейса, имя которого начинается с названия семейства ОС: Ubuntu, Debian, CentOS, Rocky, Alma, Fedora, openSUSE, Astra, Alt/Альт, RED OS/РЕД ОС, SberLinux, ROSA/РОСА, Windows; это эвристика — в каталоге такого поля нет), vm-app — любой другой образ маркетплейса (например, nginx-ubuntu-22.04), k8s-addon — плагин Kubernetes. Запрос ищет подстроку в имени или отображаемом имени (у плагинов — и в описании) без учёта регистра. Образы, недоступные проекту, и образы, не готовые ни в одной зоне, скрыты; --all показывает и их.

У каждой строки — команда развёртывания: для образа — 'evo vm create <имя-ВМ> --image <образ>' (по имени, а при повторяющихся именах — по UUID), для плагина — 'evo k8s add-addon <cluster-id> <плагин>' (версии плагина — 'evo k8s addon-versions <плагин>'); у образа, недоступного проекту, команды нет. Платность известна только у плагинов: платный ли образ или его лицензия, неизвестно — расчёт цены даёт 0 для любого образа. Порядок — по виду, затем по отображаемому имени. Если один каталог прочитать не удалось, список строится по другому с предупреждением; ошибка — только если не прочитан ни один.

Вызывает инструмент marketplace_list (подробнее: 'evo tools describe marketplace_list').
```

```
evo marketplace list [query] [flags]
```

### Options

```
      --all           Показать и образы, скрытые по умолчанию: недоступные проекту (без команды развёртывания) и не готовые ни в одной зоне
  -h, --help          Справка по list
      --kind string   Только этот вид: vm-os (образы ОС), vm-app (продукты Маркетплейса на ВМ) или k8s-addon (плагины Managed Kubernetes). По умолчанию — все три; для видов ВМ каталог плагинов не читается, для k8s-addon — каталог образов
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo marketplace](#evo-marketplace)	 - Витрина: образы ВМ и плагины Kubernetes с командой развёртывания каждого

## evo mcp

MCP-сервер для ИИ-агентов (stdio или Streamable HTTP с --http)

### Synopsis

```text
Запустить evo как сервер Model Context Protocol.

Сервер открывает инструменты evo (ВМ, диски, сети, Kubernetes, базы данных
и другие), и ИИ-агент (например, Claude Code) управляет ресурсами Cloud.ru
напрямую. Описания инструментов и их ответы — на английском: их читает агент.

По умолчанию — JSON-RPC 2.0 построчно через stdin/stdout; текст для человека
не печатается (stdout занят протоколом). SIGINT/SIGTERM и
notifications/cancelled отменяют выполняемые вызовы; закрытие stdin — нет
(уже отправленные вызовы доработают и получат ответ). Подключение к Claude Code:
  claude mcp add --transport stdio --scope project evo -- evo mcp

С --http ADDR сервер работает по MCP Streamable HTTP на http://ADDR/mcp
(и отвечает на проверку живости без аутентификации на /healthz):
  MCP_BEARER_TOKEN=... evo mcp --http :8080
  claude mcp add --transport http evo http://localhost:8080/mcp \
    --header "Authorization: Bearer $MCP_BEARER_TOKEN"

Аутентификация HTTP (--auth):
  bearer  клиент передаёт "Authorization: Bearer <токен>"; токен берётся из
          $MCP_BEARER_TOKEN, без него сервер не запускается
  none    без аутентификации; только на адресе loopback
          (--http 127.0.0.1:8080)

По HTTP отключены k8s_kubeconfig со встроенным токеном, cert_private_key и
secret_value, а учётные данные (пароли, ключи, токены) в остальных ответах
маскируются, если не задан --allow-secret-export. Аргументы, которые
затрагивают хост сервера, ограничены: compose_file у deploy — рабочим
каталогом, k8s_kubectl — встроенными командами без флагов хоста (--kubeconfig,
-f и т. п.), k8s_helm — удалёнными чартами на публичных хостах https/oci.
k8s_kubectl/k8s_apply/k8s_helm отвечают ошибкой, если kubectl/helm не
установлен (в Docker-образе нет ни того, ни другого). Одновременно выполняется
не больше 32 вызовов, в пакете — не больше 100 сообщений. Вызов отменяется,
когда клиент закрывает соединение, удаляет сессию (DELETE /mcp) или
присылает notifications/cancelled, и при остановке сервера; тайм-аута нет. На
каждый вызов в stderr пишется одна строка журнала (имя, статус,
длительность; аргументы — никогда).

Ключ доступа Cloud.ru и параметры проекта — собственные у сервера и
определяются так же, как у обычных команд (переменные окружения, профиль из
'evo configure', .evo.yaml); клиенты их не передают.

Проекты: deploy и provision принимают аргумент project_id, поэтому с ключом,
который видит несколько проектов (ключ уровня организации), любой клиент
сервера может работать в любом из них. --allow-project ограничивает
инструменты перечисленными проектами (флаг повторяется или через запятую):
вызов в любом другом проекте, в том числе в проекте самого сервера,
отклоняется. org_quotas и iam_service_accounts только читают и по-прежнему
видят другие проекты. Без флага сервер в режиме HTTP предупреждает при
запуске. Для общего сервера лучше ключ уровня проекта.
```

```
evo mcp [flags]
```

### Examples

```
  evo mcp
  claude mcp add --transport stdio --scope project evo -- evo mcp
  MCP_BEARER_TOKEN="$(openssl rand -hex 32)" evo mcp --http :8080 --allow-project <UUID>
```

### Options

```
      --allow-project strings   Разрешить вызовы инструментов только в этих проектах Cloud.ru (UUID; флаг повторяется или через запятую). По умолчанию — любой проект, который видит ключ
      --allow-secret-export     Режим HTTP: разрешить k8s_kubeconfig со встроенным токеном, cert_private_key, secret_value и немаскированные учётные данные в ответах
      --auth string             Аутентификация HTTP: bearer (токен из $MCP_BEARER_TOKEN) или none (только адрес loopback) (по умолчанию "bearer")
  -h, --help                    Справка по mcp
      --http string             Обслуживать MCP по Streamable HTTP на этом адресе (например, :8080), а не по stdio
      --toolsets strings        Показывать агенту только эти группы инструментов (через запятую, например deploy,vm,vpc; группы — в 'evo tools list'). По умолчанию все. Экономит контекст агента; это не ограничение прав
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов

## evo metrics

Service Monitoring: метрики

```
evo metrics [flags]
```

### Options

```
  -h, --help   Справка по metrics
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo metrics history](#evo-metrics-history)	 - История метрики за недавний период (Service Monitoring)
* [evo metrics scrape](#evo-metrics-scrape)	 - Текущие значения метрик в текстовом формате Prometheus

## evo metrics history

История метрики за недавний период (Service Monitoring)

### Synopsis

```text
История метрики за недавний период (Service Monitoring).

Service Monitoring: история метрики за недавний период (только чтение). Чтобы понять, здоров ли сервис и как загружен CPU, история метрики полезнее, чем повторные 'evo vm get' и 'evo k8s get'. Возвращает временные ряды: метки метрики, значения, отметки времени. Данные есть, только если в проекте подключён тенант мониторинга; иначе API отвечает 'tenant not found'.

Вызывает инструмент metrics_history (подробнее: 'evo tools describe metrics_history').
```

```
evo metrics history <match> [flags]
```

### Examples

```
  evo metrics history cpu_usage
  evo metrics history node_load1 --since 6h --json
```

### Options

```
  -h, --help           Справка по history
      --since string   Окно назад от текущего момента: 30m, 1h, 6h, 1d или число минут строкой, например "90" (по умолчанию 1h; не больше 24h — ограничение API, большее отклоняется)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo metrics](#evo-metrics)	 - Service Monitoring: метрики

## evo metrics scrape

Текущие значения метрик в текстовом формате Prometheus

### Synopsis

```text
Текущие значения метрик в текстовом формате Prometheus.

Service Monitoring: текущие значения метрик в текстовом формате экспозиции Prometheus — снимок всех или отобранных метрик на этот момент (только чтение). Отбор — аргумент [match]; без него — все метрики.

Вызывает инструмент metrics_scrape (подробнее: 'evo tools describe metrics_scrape').
```

```
evo metrics scrape [match] [flags]
```

### Examples

```
  evo metrics scrape
  evo metrics scrape '{__name__=~"cpu.*"}' | grep -v '^#'
```

### Options

```
  -h, --help   Справка по scrape
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo metrics](#evo-metrics)	 - Service Monitoring: метрики

## evo org

Организация: проекты и квоты

```
evo org [flags]
```

### Options

```
  -h, --help   Справка по org
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo org projects](#evo-org-projects)	 - Проекты Cloud.ru, которые видит ключ доступа
* [evo org quotas](#evo-org-quotas)	 - Квоты: ресурс, лимит, текущее использование

## evo org projects

Проекты Cloud.ru, которые видит ключ доступа

### Synopsis

```text
Проекты Cloud.ru, которые видит ключ доступа.

Проекты Cloud.ru, которые видит ключ доступа: id, имя, состояние, подразделение организации, — и активный проект (active). Только чтение. Если Organization API не показывает ключу ни одного проекта (сервисный аккаунт уровня проекта), это не ошибка: в ответе projects: null, активный проект и примечание.

Вызывает инструмент org_projects (подробнее: 'evo tools describe org_projects').
```

```
evo org projects [flags]
```

### Examples

```
  evo org projects
  evo org projects --json | jq -r '.projects[].id'
```

### Options

```
  -h, --help   Справка по projects
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo org](#evo-org)	 - Организация: проекты и квоты

## evo org quotas

Квоты: ресурс, лимит, текущее использование

### Synopsis

```text
Квоты: ресурс, лимит, текущее использование.

Использование квот: ресурс, лимит, текущее использование — для планирования мощностей. Только чтение. Можно ограничить одним проектом (--project-id). Ответ может быть неполным, если у сервисного аккаунта недостаточно прав.

Вызывает инструмент org_quotas (подробнее: 'evo tools describe org_quotas').
```

```
evo org quotas [flags]
```

### Examples

```
  evo org quotas
  evo org quotas --project-id <UUID> --json
```

### Options

```
  -h, --help                Справка по quotas
      --project-id string   UUID проекта, которым ограничить квоты (необязательно)
```

### Options inherited from parent commands

```
      --dry-run          Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json             Вывод в формате JSON
      --no-color         Без цвета
      --profile string   Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --region string    UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose          Подробный журнал
  -y, --yes              Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo org](#evo-org)	 - Организация: проекты и квоты

## evo pg

Managed PostgreSQL: кластеры, базы данных, пользователи, резервные копии

```
evo pg [flags]
```

### Options

```
  -h, --help   Справка по pg
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo pg backup-create](#evo-pg-backup-create)	 - Создать резервную копию вручную (асинхронно)
* [evo pg backups](#evo-pg-backups)	 - Резервные копии кластера
* [evo pg create](#evo-pg-create)	 - Создать кластер PostgreSQL (платно, асинхронно)
* [evo pg databases](#evo-pg-databases)	 - Базы данных кластера
* [evo pg db-create](#evo-pg-db-create)	 - Создать базу данных
* [evo pg db-delete](#evo-pg-db-delete)	 - Удалить базу данных
* [evo pg delete](#evo-pg-delete)	 - Удалить кластер PostgreSQL вместе с данными и всеми резервными копиями (асинхронно)
* [evo pg get](#evo-pg-get)	 - Кластер PostgreSQL по ID: версия, хранилище, строка подключения, состояние
* [evo pg list](#evo-pg-list)	 - Кластеры Managed PostgreSQL проекта
* [evo pg operation](#evo-pg-operation)	 - Состояние асинхронной операции PostgreSQL
* [evo pg specs](#evo-pg-specs)	 - Конфигурации кластера PostgreSQL для версии: CPU, память, минимальный объём
* [evo pg user-create](#evo-pg-user-create)	 - Создать пользователя (пароль не выводится)
* [evo pg user-delete](#evo-pg-user-delete)	 - Удалить пользователя
* [evo pg users](#evo-pg-users)	 - Пользователи кластера и их роли
* [evo pg versions](#evo-pg-versions)	 - Доступные версии PostgreSQL

## evo pg backup-create

Создать резервную копию вручную (асинхронно)

### Synopsis

```text
Создать резервную копию вручную (асинхронно).

Запустить резервное копирование кластера PostgreSQL вручную. Операция асинхронная: ход — 'evo pg operation <operation-id>'.

Вызывает инструмент pg_backup_create (подробнее: 'evo tools describe pg_backup_create').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo pg backup-create <cluster-id> <name> [flags]
```

### Options

```
  -h, --help   Справка по backup-create
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL: кластеры, базы данных, пользователи, резервные копии

## evo pg backups

Резервные копии кластера

### Synopsis

```text
Резервные копии кластера.

Резервные копии кластера PostgreSQL: имя, состояние, размер, время создания. Только чтение.

Вызывает инструмент pg_backups (подробнее: 'evo tools describe pg_backups').
```

```
evo pg backups <cluster-id> [flags]
```

### Options

```
  -h, --help   Справка по backups
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL: кластеры, базы данных, пользователи, резервные копии

## evo pg create

Создать кластер PostgreSQL (платно, асинхронно)

### Synopsis

```text
Создать кластер PostgreSQL (платно, асинхронно).

Создать кластер Managed PostgreSQL. Имя, которое уже носит кластер проекта ('evo pg list'), отклоняется. evo подбирает версию (по умолчанию — последняя), конфигурацию (по --specification-id или по --cpu и --ram; если подходящей нет — ошибка со списком конфигураций), хранилище (по умолчанию — минимум конфигурации) и подсеть (по умолчанию — первая подсеть проекта). Кластер платный. Операция асинхронная и занимает несколько минут: ход — 'evo pg operation <operation-id>'.

Вызывает инструмент pg_create (подробнее: 'evo tools describe pg_create').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo pg create <name> [flags]
```

### Examples

```
  evo pg create app-db --dry-run
  evo pg create app-db --cpu 2 --ram 4
```

### Options

```
      --backup-retention-days int   Срок хранения автоматических резервных копий, дней (по умолчанию 14, не больше 30)
      --backup-schedule string      Расписание автоматических резервных копий в формате CRON (по умолчанию '0 3 * * 0' — по воскресеньям в 03:00; не чаще раза в 6 часов)
      --cpu int                     Число vCPU — для автоподбора конфигурации, если --specification-id не задан
      --description string          Описание кластера (необязательно)
  -h, --help                        Справка по create
      --initial-database string     Имя базы данных, которая создаётся вместе с кластером (по умолчанию defaultdb)
      --instances int               Число узлов: 1–2 для standard, 1–3 для business (проверяется по конфигурации). По умолчанию 1
      --no-backup                   Без автоматических резервных копий. По умолчанию они включены, и после создания кластера это не изменить; кластер без резервных копий отклоняет evo pg backup-create и evo pg backups с ошибкой 'backup disabled'
      --ram int                     RAM, ГБ — вместе с --cpu для автоподбора конфигурации
      --specification-id string     UUID конфигурации (список: evo pg specs). Необязателен, если заданы --cpu и --ram
      --storage-gb int              Размер основного диска данных, ГБ (по умолчанию — минимум конфигурации)
      --subnet-id string            UUID подсети, определяет зону доступности (по умолчанию — первая подсеть проекта)
      --version string              Версия PostgreSQL (список: evo pg versions; по умолчанию — последняя)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL: кластеры, базы данных, пользователи, резервные копии

## evo pg databases

Базы данных кластера

### Synopsis

```text
Базы данных кластера.

Базы данных кластера PostgreSQL: имя, владелец, размер. Только чтение.

Вызывает инструмент pg_databases (подробнее: 'evo tools describe pg_databases').
```

```
evo pg databases <cluster-id> [flags]
```

### Options

```
  -h, --help   Справка по databases
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL: кластеры, базы данных, пользователи, резервные копии

## evo pg db-create

Создать базу данных

### Synopsis

```text
Создать базу данных.

Создать базу данных в кластере PostgreSQL. Владелец — --owner (пользователи кластера: 'evo pg users <cluster-id>').

Вызывает инструмент pg_db_create (подробнее: 'evo tools describe pg_db_create').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo pg db-create <cluster-id> <name> [flags]
```

### Options

```
  -h, --help                Справка по db-create
      --lc-collate string   Локаль сортировки (необязательно, по умолчанию C)
      --lc-ctype string     Локаль набора символов (необязательно, по умолчанию C)
      --owner string        Пользователь-владелец (список: evo pg users), обязательный
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL: кластеры, базы данных, пользователи, резервные копии

## evo pg db-delete

Удалить базу данных

### Synopsis

```text
Удалить базу данных.

Удалить базу данных кластера PostgreSQL вместе с её данными.

Вызывает инструмент pg_db_delete (подробнее: 'evo tools describe pg_db_delete').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo pg db-delete <cluster-id> <name> [flags]
```

### Options

```
  -h, --help   Справка по db-delete
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL: кластеры, базы данных, пользователи, резервные копии

## evo pg delete

Удалить кластер PostgreSQL вместе с данными и всеми резервными копиями (асинхронно)

### Synopsis

```text
Удалить кластер PostgreSQL вместе с данными и всеми резервными копиями (асинхронно).

Удалить кластер Managed PostgreSQL по id (список: 'evo pg list') вместе с данными и всеми резервными копиями — восстановить их нельзя. Операция асинхронная.

Вызывает инструмент pg_delete (подробнее: 'evo tools describe pg_delete').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo pg delete <cluster-id> [flags]
```

### Options

```
  -h, --help   Справка по delete
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL: кластеры, базы данных, пользователи, резервные копии

## evo pg get

Кластер PostgreSQL по ID: версия, хранилище, строка подключения, состояние

### Synopsis

```text
Кластер PostgreSQL по ID: версия, хранилище, строка подключения, состояние.

Все сведения о кластере Managed PostgreSQL по id (список: 'evo pg list'): версия, узлы, хранилище, строка подключения, подсети, состояние. Только чтение.

Вызывает инструмент pg_get (подробнее: 'evo tools describe pg_get').
```

```
evo pg get <cluster-id> [flags]
```

### Options

```
  -h, --help   Справка по get
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL: кластеры, базы данных, пользователи, резервные копии

## evo pg list

Кластеры Managed PostgreSQL проекта

### Synopsis

```text
Кластеры Managed PostgreSQL проекта.

Кластеры Managed PostgreSQL проекта: id, имя, состояние, версия, работоспособность. Только чтение.

Вызывает инструмент pg_list (подробнее: 'evo tools describe pg_list').
```

```
evo pg list [flags]
```

### Examples

```
  evo pg list
```

### Options

```
  -h, --help   Справка по list
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL: кластеры, базы данных, пользователи, резервные копии

## evo pg operation

Состояние асинхронной операции PostgreSQL

### Synopsis

```text
Состояние асинхронной операции PostgreSQL.

Состояние асинхронной операции Managed PostgreSQL по id — его возвращают 'evo pg create', 'evo pg delete' и 'evo pg backup-create': done — завершена ли операция, error — ошибка. С --wait-seconds команда сама ждёт завершения до N секунд и выводит итоговое состояние. Только чтение.

Вызывает инструмент pg_operation (подробнее: 'evo tools describe pg_operation').
```

```
evo pg operation <operation-id> [flags]
```

### Options

```
  -h, --help               Справка по operation
      --wait-seconds int   Ждать итогового состояния до N секунд (не больше 300), опрашивая операцию с нарастающим интервалом. Без флага — одна проверка
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL: кластеры, базы данных, пользователи, резервные копии

## evo pg specs

Конфигурации кластера PostgreSQL для версии: CPU, память, минимальный объём

### Synopsis

```text
Конфигурации кластера PostgreSQL для версии: CPU, память, минимальный объём.

Конфигурации кластера Managed PostgreSQL для версии: id, CPU, память (ГБ), режим развёртывания (standard или business), минимальный объём хранилища. id конфигурации передаётся в --specification-id команды 'evo pg create'. Только чтение.

Вызывает инструмент pg_specs (подробнее: 'evo tools describe pg_specs').
```

```
evo pg specs [flags]
```

### Options

```
  -h, --help             Справка по specs
      --version string   Версия продукта, например 17: только конфигурации этой версии (необязательно)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL: кластеры, базы данных, пользователи, резервные копии

## evo pg user-create

Создать пользователя (пароль не выводится)

### Synopsis

```text
Создать пользователя (пароль не выводится).

Создать пользователя в кластере PostgreSQL. Пароль в ответе не выводится.

Вызывает инструмент pg_user_create (подробнее: 'evo tools describe pg_user_create').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo pg user-create <cluster-id> <name> [flags]
```

### Options

```
  -h, --help              Справка по user-create
      --password string   Пароль пользователя (обязательный)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL: кластеры, базы данных, пользователи, резервные копии

## evo pg user-delete

Удалить пользователя

### Synopsis

```text
Удалить пользователя.

Удалить пользователя кластера PostgreSQL.

Вызывает инструмент pg_user_delete (подробнее: 'evo tools describe pg_user_delete').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo pg user-delete <cluster-id> <name> [flags]
```

### Options

```
  -h, --help   Справка по user-delete
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL: кластеры, базы данных, пользователи, резервные копии

## evo pg users

Пользователи кластера и их роли

### Synopsis

```text
Пользователи кластера и их роли.

Пользователи кластера PostgreSQL и выданные им роли. Только чтение.

Вызывает инструмент pg_users (подробнее: 'evo tools describe pg_users').
```

```
evo pg users <cluster-id> [flags]
```

### Options

```
  -h, --help   Справка по users
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL: кластеры, базы данных, пользователи, резервные копии

## evo pg versions

Доступные версии PostgreSQL

### Synopsis

```text
Доступные версии PostgreSQL.

Доступные версии Managed PostgreSQL (например, 15, 16, 17). Версия передаётся в --version команды 'evo pg create'. Только чтение.

Вызывает инструмент pg_versions (подробнее: 'evo tools describe pg_versions').
```

```
evo pg versions [flags]
```

### Options

```
  -h, --help   Справка по versions
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo pg](#evo-pg)	 - Managed PostgreSQL: кластеры, базы данных, пользователи, резервные копии

## evo project

Проекты, которые видит ключ, и проект профиля

```
evo project [flags]
```

### Options

```
  -h, --help   Справка по project
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo project list](#evo-project-list)	 - Проекты, которые видит ключ (* — активный)
* [evo project use](#evo-project-use)	 - Сделать проект проектом профиля по умолчанию

## evo project list

Проекты, которые видит ключ (* — активный)

```
evo project list [flags]
```

### Examples

```
  evo project list
  evo project list --json | jq -r '.projects[].id'
```

### Options

```
  -h, --help   Справка по list
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo project](#evo-project)	 - Проекты, которые видит ключ, и проект профиля

## evo project use

Сделать проект проектом профиля по умолчанию

### Synopsis

```text
Записать проект в активный профиль: команды работают в нём, если
--project-id, CLOUDRU_PROJECT_ID или ./.evo.yaml не указывают другой.

Проект задаётся UUID или, если ключ может получить список проектов, именем.
Проект, которого ключ не видит, не принимается. Если Organization API не
отдаёт этому ключу список проектов, UUID сохраняется как есть.
```

```
evo project use <UUID|имя> [flags]
```

### Examples

```
  evo project use 3f2a8c1e-0000-0000-0000-000000000000
  evo project use my-team-dev
  evo --profile test project use <UUID>
```

### Options

```
  -h, --help   Справка по use
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo project](#evo-project)	 - Проекты, которые видит ключ, и проект профиля

## evo redis

Кластеры Managed Redis

```
evo redis [flags]
```

### Options

```
  -h, --help   Справка по redis
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo redis create](#evo-redis-create)	 - Создать кластер Managed Redis (платно, асинхронно)
* [evo redis delete](#evo-redis-delete)	 - Удалить кластер Redis вместе с данными (асинхронно)
* [evo redis get](#evo-redis-get)	 - Кластер Redis по ID: состояние, адреса, тип, шарды, реплики
* [evo redis list](#evo-redis-list)	 - Кластеры Managed Redis проекта
* [evo redis operation](#evo-redis-operation)	 - Состояние асинхронной операции Redis
* [evo redis specs](#evo-redis-specs)	 - Конфигурации кластера Redis для версии: CPU, память, режим, хранилище
* [evo redis versions](#evo-redis-versions)	 - Доступные версии Managed Redis

## evo redis create

Создать кластер Managed Redis (платно, асинхронно)

### Synopsis

```text
Создать кластер Managed Redis (платно, асинхронно).

Создать кластер Managed Redis. Имя, которое уже носит кластер проекта ('evo redis list'), отклоняется. evo подбирает версию (последнюю), подсеть (как для Kafka) и по ней — зону кластера, конфигурацию этой зоны (по --specification-id или по --cpu и --ram; если подходящей нет — ошибка со списком конфигураций). Тип MASTER_REPLICA (по умолчанию): в каждом шарде мастер и две реплики, все в зоне подсети — Single-AZ; API до сих пор отклонял STANDALONE.

--replicas здесь — реплики БЕЗ мастера (поле replicas запроса на создание, 2), а сведения о кластере ('evo redis get <cluster-id>', 'evo redis list') показывают replicas как число узлов в шарде вместе с мастером (3). Кластер платный. Операция асинхронная: ход — 'evo redis operation <operation-id>'.

Вызывает инструмент redis_create (подробнее: 'evo tools describe redis_create').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo redis create <name> [flags]
```

### Examples

```
  evo redis create cache --dry-run
  evo redis create cache --cpu 2 --ram 4 --yes
```

### Options

```
      --cpu int                   Число vCPU — для автоподбора конфигурации, если --specification-id не задан
      --description string        Описание (необязательно)
  -h, --help                      Справка по create
      --ram int                   RAM, ГБ — вместе с --cpu
      --replicas int              Реплики в шарде БЕЗ мастера — поле replicas запроса на создание. Для MASTER_REPLICA — 2 (по умолчанию: мастер и две реплики, всего 3 узла — это число сведения о кластере показывают как replicas); для STANDALONE — 0
      --shards int                Число шардов, 1–8 (по умолчанию 1)
      --specification-id string   UUID конфигурации (список: evo redis specs). Необязателен, если заданы --cpu и --ram
      --storage-gb int            Размер диска, ГБ (по умолчанию — минимум конфигурации)
      --subnet-id string          UUID подсети (список: evo vpc subnets). По умолчанию — основная подсеть зоны из настроек. Кластер создаётся в зоне подсети
      --type string               MASTER_REPLICA (по умолчанию) или STANDALONE (API пока его отклоняет)
      --version-id string         UUID версии Redis (список: evo redis versions). По умолчанию — последняя
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo redis](#evo-redis)	 - Кластеры Managed Redis

## evo redis delete

Удалить кластер Redis вместе с данными (асинхронно)

### Synopsis

```text
Удалить кластер Redis вместе с данными (асинхронно).

Удалить кластер Managed Redis по id вместе с данными. Операция асинхронная.

Вызывает инструмент redis_delete (подробнее: 'evo tools describe redis_delete').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo redis delete <cluster-id> [flags]
```

### Examples

```
  evo redis delete <cluster-id> --dry-run
  evo redis delete <cluster-id> --yes
```

### Options

```
  -h, --help   Справка по delete
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo redis](#evo-redis)	 - Кластеры Managed Redis

## evo redis get

Кластер Redis по ID: состояние, адреса, тип, шарды, реплики

### Synopsis

```text
Кластер Redis по ID: состояние, адреса, тип, шарды, реплики.

Кластер Managed Redis по id: состояние, адреса (redis_hosts), тип, шарды, реплики. Только чтение.

Вызывает инструмент redis_get (подробнее: 'evo tools describe redis_get').
```

```
evo redis get <cluster-id> [flags]
```

### Examples

```
  evo redis get <cluster-id>
  evo redis get <cluster-id> --json
```

### Options

```
  -h, --help   Справка по get
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo redis](#evo-redis)	 - Кластеры Managed Redis

## evo redis list

Кластеры Managed Redis проекта

### Synopsis

```text
Кластеры Managed Redis проекта.

Кластеры Managed Redis: id, имя, состояние, тип, шарды, реплики. Только чтение.

Вызывает инструмент redis_list (подробнее: 'evo tools describe redis_list').
```

```
evo redis list [flags]
```

### Examples

```
  evo redis list
  evo redis list --json | jq -r '.clusters[] | "\(.id)\t\(.name)\t\(.status)"'
```

### Options

```
  -h, --help   Справка по list
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo redis](#evo-redis)	 - Кластеры Managed Redis

## evo redis operation

Состояние асинхронной операции Redis

### Synopsis

```text
Состояние асинхронной операции Redis.

Состояние асинхронной операции Managed Redis по id — его возвращают создание и удаление кластера: done — завершена ли операция, error — ошибка. С --wait-seconds команда сама ждёт завершения до N секунд и выводит итоговое состояние. Только чтение.

Вызывает инструмент redis_operation (подробнее: 'evo tools describe redis_operation').
```

```
evo redis operation <operation-id> [flags]
```

### Examples

```
  evo redis operation <operation-id>
  evo redis operation <operation-id> --wait-seconds 300
```

### Options

```
  -h, --help               Справка по operation
      --wait-seconds int   Ждать итогового состояния до N секунд (не больше 300), опрашивая операцию с нарастающим интервалом. Без флага — одна проверка
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo redis](#evo-redis)	 - Кластеры Managed Redis

## evo redis specs

Конфигурации кластера Redis для версии: CPU, память, режим, хранилище

### Synopsis

```text
Конфигурации кластера Redis для версии: CPU, память, режим, хранилище.

Конфигурации кластера Managed Redis для версии: id, CPU, память (ГБ), режим развёртывания, минимальный объём хранилища. Только чтение. id конфигурации выбирают для создания кластера ('evo redis create').

Вызывает инструмент redis_specs (подробнее: 'evo tools describe redis_specs').
```

```
evo redis specs [flags]
```

### Examples

```
  evo redis specs
  evo redis specs --version-id <version-id> --zone ru.AZ-2
```

### Options

```
  -h, --help                Справка по specs
      --version-id string   UUID версии (список: evo redis versions); необязательно — по умолчанию последняя
      --zone string         Зона доступности: имя (ru.AZ-2) или UUID; необязательно — по умолчанию зона из настроек, иначе ru.AZ-2. API нужна зона
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo redis](#evo-redis)	 - Кластеры Managed Redis

## evo redis versions

Доступные версии Managed Redis

### Synopsis

```text
Доступные версии Managed Redis.

Доступные версии Managed Redis: id и имя. Только чтение. Нужны перед созданием кластера ('evo redis create').

Вызывает инструмент redis_versions (подробнее: 'evo tools describe redis_versions').
```

```
evo redis versions [flags]
```

### Examples

```
  evo redis versions
```

### Options

```
  -h, --help   Справка по versions
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo redis](#evo-redis)	 - Кластеры Managed Redis

## evo s3

Object Storage: бакеты и объекты

```
evo s3 [flags]
```

### Options

```
  -h, --help   Справка по s3
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo s3 bucket-create](#evo-s3-bucket-create)	 - Создать бакет
* [evo s3 bucket-delete](#evo-s3-bucket-delete)	 - Удалить пустой бакет
* [evo s3 buckets](#evo-s3-buckets)	 - Бакеты Object Storage
* [evo s3 get](#evo-s3-get)	 - Скачать объект до 10 МиБ (небольшой текст — как есть, остальное — в base64; больший объект — через S3-клиент или консоль)
* [evo s3 ls](#evo-s3-ls)	 - Объекты бакета: ключ, размер, время изменения
* [evo s3 put](#evo-s3-put)	 - Загрузить объект (существующий ключ перезаписывается)
* [evo s3 rm](#evo-s3-rm)	 - Удалить объект

## evo s3 bucket-create

Создать бакет

### Synopsis

```text
Создать бакет.

Создать бакет Object Storage — например, перед 'evo s3 put', которому нужен существующий бакет. Повторное создание своего бакета ошибкой не считается.

Вызывает инструмент s3_bucket_create (подробнее: 'evo tools describe s3_bucket_create').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo s3 bucket-create <bucket> [flags]
```

### Options

```
  -h, --help               Справка по bucket-create
      --s3-region string   Регион: принимается только ru-central-1 (иначе облако отвечает 'unexpected region'). По умолчанию — регион S3 из настроек
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo s3](#evo-s3)	 - Object Storage: бакеты и объекты

## evo s3 bucket-delete

Удалить пустой бакет

### Synopsis

```text
Удалить пустой бакет.

Удалить пустой бакет. Объекты из бакета сначала удалите: 'evo s3 rm'.

Вызывает инструмент s3_bucket_delete (подробнее: 'evo tools describe s3_bucket_delete').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo s3 bucket-delete <bucket> [flags]
```

### Options

```
  -h, --help   Справка по bucket-delete
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo s3](#evo-s3)	 - Object Storage: бакеты и объекты

## evo s3 buckets

Бакеты Object Storage

### Synopsis

```text
Бакеты Object Storage.

Бакеты Object Storage (S3-совместимое хранилище s3.cloud.ru). Только чтение.

Вызывает инструмент s3_buckets (подробнее: 'evo tools describe s3_buckets').
```

```
evo s3 buckets [flags]
```

### Examples

```
  evo s3 buckets
```

### Options

```
  -h, --help   Справка по buckets
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo s3](#evo-s3)	 - Object Storage: бакеты и объекты

## evo s3 get

Скачать объект до 10 МиБ (небольшой текст — как есть, остальное — в base64; больший объект — через S3-клиент или консоль)

### Synopsis

```text
Скачать объект до 10 МиБ (небольшой текст — как есть, остальное — в base64; больший объект — через S3-клиент или консоль).

Скачать объект до 10 МиБ; больший объект отклоняется — используйте S3-клиент или консоль. Небольшой текст в UTF-8 выводится как есть, остальное — в base64. Только чтение.

Вызывает инструмент s3_get (подробнее: 'evo tools describe s3_get').
```

```
evo s3 get <bucket> <key> [flags]
```

### Options

```
  -h, --help   Справка по get
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo s3](#evo-s3)	 - Object Storage: бакеты и объекты

## evo s3 ls

Объекты бакета: ключ, размер, время изменения

### Synopsis

```text
Объекты бакета: ключ, размер, время изменения.

Объекты бакета: ключ, размер, время изменения. Только чтение.

Вызывает инструмент s3_ls (подробнее: 'evo tools describe s3_ls').
```

```
evo s3 ls <bucket> [flags]
```

### Examples

```
  evo s3 ls my-bucket
  evo s3 ls my-bucket --prefix backups/
```

### Options

```
  -h, --help            Справка по ls
      --max-keys int    Наибольшее число объектов в ответе (по умолчанию 1000)
      --prefix string   Только ключи с этим префиксом (необязательно)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo s3](#evo-s3)	 - Object Storage: бакеты и объекты

## evo s3 put

Загрузить объект (существующий ключ перезаписывается)

### Synopsis

```text
Загрузить объект (существующий ключ перезаписывается).

Загрузить объект: текст — через --content, двоичные данные — через --content-base64. Объект с тем же ключом перезаписывается. Бакет должен существовать: 'evo s3 bucket-create'.

Вызывает инструмент s3_put (подробнее: 'evo tools describe s3_put').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo s3 put <bucket> <key> [flags]
```

### Options

```
      --content string          Текст для загрузки
      --content-base64 string   Двоичные данные в base64 (вместо --content)
      --content-type string     MIME-тип (по умолчанию application/octet-stream)
  -h, --help                    Справка по put
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo s3](#evo-s3)	 - Object Storage: бакеты и объекты

## evo s3 rm

Удалить объект

### Synopsis

```text
Удалить объект.

Удалить объект из бакета безвозвратно.

Вызывает инструмент s3_rm (подробнее: 'evo tools describe s3_rm').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo s3 rm <bucket> <key> [flags]
```

### Options

```
  -h, --help   Справка по rm
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo s3](#evo-s3)	 - Object Storage: бакеты и объекты

## evo secret

Secret Manager: секреты

```
evo secret [flags]
```

### Options

```
  -h, --help   Справка по secret
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo secret add-version](#evo-secret-add-version)	 - Добавить новую версию значения секрета (ротация)
* [evo secret create](#evo-secret-create)	 - Создать секрет
* [evo secret delete](#evo-secret-delete)	 - Удалить секрет со всеми версиями
* [evo secret get](#evo-secret-get)	 - Метаданные секрета: папка, метки, версии, ключ KMS (без значения)
* [evo secret list](#evo-secret-list)	 - Секреты проекта (без значений)
* [evo secret value](#evo-secret-value)	 - Расшифрованное значение секрета

## evo secret add-version

Добавить новую версию значения секрета (ротация)

### Synopsis

```text
Добавить новую версию значения секрета (ротация).

Добавить новую версию значения существующего секрета (ротация).

Вызывает инструмент secret_add_version (подробнее: 'evo tools describe secret_add_version').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo secret add-version <path> [flags]
```

### Options

```
  -h, --help           Справка по add-version
      --value string   Новое значение секрета открытым текстом (обязательный)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo secret](#evo-secret)	 - Secret Manager: секреты

## evo secret create

Создать секрет

### Synopsis

```text
Создать секрет.

Создать секрет по пути; путь вида папка/имя кладёт секрет в папку. Значение хранится зашифрованным и в ответе не выводится.

Вызывает инструмент secret_create (подробнее: 'evo tools describe secret_create').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo secret create <path> [flags]
```

### Options

```
      --description string   Описание (необязательно)
  -h, --help                 Справка по create
      --kms-key-id string    id ключа KMS для шифрования (необязательно)
      --value string         Значение секрета открытым текстом; хранится зашифрованным (обязательный)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo secret](#evo-secret)	 - Secret Manager: секреты

## evo secret delete

Удалить секрет со всеми версиями

### Synopsis

```text
Удалить секрет со всеми версиями.

Удалить секрет по пути вместе со всеми версиями. По умолчанию удаляется и секрет с активными версиями; с --force=false облако отказывает в удалении такого секрета.

Вызывает инструмент secret_delete (подробнее: 'evo tools describe secret_delete').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo secret delete <path> [flags]
```

### Options

```
      --force   Удалить секрет, даже если у него есть активные версии. Выключить: --force=false (по умолчанию true)
  -h, --help    Справка по delete
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo secret](#evo-secret)	 - Secret Manager: секреты

## evo secret get

Метаданные секрета: папка, метки, версии, ключ KMS (без значения)

### Synopsis

```text
Метаданные секрета: папка, метки, версии, ключ KMS (без значения).

Метаданные секрета по пути (список: 'evo secret list'): папка, метки, версии, ключ KMS. Только чтение; значение не выводится.

Вызывает инструмент secret_get (подробнее: 'evo tools describe secret_get').
```

```
evo secret get <path> [flags]
```

### Options

```
  -h, --help   Справка по get
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo secret](#evo-secret)	 - Secret Manager: секреты

## evo secret list

Секреты проекта (без значений)

### Synopsis

```text
Секреты проекта (без значений).

Секреты проекта в Secret Manager Cloud.ru: id, имя, путь, описание. Только чтение; значения не выводятся — значение секрета показывает 'evo secret value <путь>'.

Вызывает инструмент secret_list (подробнее: 'evo tools describe secret_list').
```

```
evo secret list [flags]
```

### Options

```
  -h, --help   Справка по list
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo secret](#evo-secret)	 - Secret Manager: секреты

## evo secret value

Расшифрованное значение секрета

### Synopsis

```text
Расшифрованное значение секрета.

Расшифрованное значение секрета по пути — открытым текстом. Не сохраняйте его в журналы и файлы. Ничего не меняет.

Вызывает инструмент secret_value (подробнее: 'evo tools describe secret_value').

Показывает секрет, поэтому спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее.
```

```
evo secret value <path> [flags]
```

### Options

```
  -h, --help          Справка по value
      --version int   Номер версии (по умолчанию — последняя)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo secret](#evo-secret)	 - Secret Manager: секреты

## evo snat

SNAT-шлюзы: исходящий интернет для приватных подсетей

```
evo snat [flags]
```

### Options

```
  -h, --help   Справка по snat
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo snat create](#evo-snat-create)	 - Создать публичный SNAT-шлюз: исходящий интернет для подсетей зоны (платно)
* [evo snat delete](#evo-snat-delete)	 - Удалить SNAT-шлюз (узлы зоны теряют исходящий интернет)
* [evo snat list](#evo-snat-list)	 - SNAT-шлюзы проекта

## evo snat create

Создать публичный SNAT-шлюз: исходящий интернет для подсетей зоны (платно)

### Synopsis

```text
Создать публичный SNAT-шлюз: исходящий интернет для подсетей зоны (платно).

Создать публичный SNAT-шлюз: исходящий доступ в интернет для каждой МАРШРУТИЗИРУЕМОЙ подсети его зоны (изолированная подсеть не обслуживается; ВМ с собственным публичным IP выходит в интернет напрямую). Один шлюз на зону; он получает публичный IP, который учитывается в квоте публичных IP и освобождается только вместе со шлюзом. Зона — из настроек; другая — --zone (имя) или --availability-zone-id. Шлюз платный.

'evo k8s create' шлюз не создаёт: кластер Managed Kubernetes поднимается без исходящего доступа. Если нагрузкам нужно скачивать публичные образы, создайте шлюз в зоне узлов кластера.

Вызывает инструмент snat_create (подробнее: 'evo tools describe snat_create').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo snat create <name> [flags]
```

### Examples

```
  evo snat create nat-az2 --zone ru.AZ-2 --dry-run
  evo snat create nat-az2 --zone ru.AZ-2 --yes
```

### Options

```
      --availability-zone-id string   UUID зоны доступности (вместо --zone)
  -h, --help                          Справка по create
      --zone string                   ИМЯ зоны доступности, например ru.AZ-2 (необязательно; evo находит её UUID). По умолчанию — зона из настроек
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo snat](#evo-snat)	 - SNAT-шлюзы: исходящий интернет для приватных подсетей

## evo snat delete

Удалить SNAT-шлюз (узлы зоны теряют исходящий интернет)

### Synopsis

```text
Удалить SNAT-шлюз (узлы зоны теряют исходящий интернет).

Удалить SNAT-шлюз по id (список: 'evo snat list'). Узлы в этой зоне теряют исходящий доступ в интернет.

Вызывает инструмент snat_delete (подробнее: 'evo tools describe snat_delete').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo snat delete <id> [flags]
```

### Examples

```
  evo snat delete <snat-id> --dry-run
  evo snat delete <snat-id> --yes
```

### Options

```
  -h, --help   Справка по delete
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo snat](#evo-snat)	 - SNAT-шлюзы: исходящий интернет для приватных подсетей

## evo snat list

SNAT-шлюзы проекта

### Synopsis

```text
SNAT-шлюзы проекта.

SNAT-шлюзы проекта. SNAT-шлюз даёт узлам в приватных подсетях (например, рабочим узлам Managed Kubernetes) ИСХОДЯЩИЙ доступ в интернет: он нужен, чтобы скачивать публичные образы контейнеров (docker.io и другие) и пакеты apt или обращаться к внешним API. Это не то же, что --kube-api-internet кластера: тот открывает входящий доступ к API-серверу. Только чтение.

Вызывает инструмент snat_list (подробнее: 'evo tools describe snat_list').
```

```
evo snat list [flags]
```

### Examples

```
  evo snat list
  evo snat list --json
```

### Options

```
  -h, --help   Справка по list
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo snat](#evo-snat)	 - SNAT-шлюзы: исходящий интернет для приватных подсетей

## evo stack

Стеки: несколько ресурсов по одному манифесту

```
evo stack [flags]
```

### Options

```
  -h, --help   Справка по stack
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo stack apply](#evo-stack-apply)	 - Создать ресурсы спланированного стека (платно)
* [evo stack destroy](#evo-stack-destroy)	 - Удалить все ресурсы стека (безвозвратно)
* [evo stack plan](#evo-stack-plan)	 - Спланировать стек ресурсов по одному манифесту (ничего не создаёт)
* [evo stack status](#evo-stack-status)	 - Ход развёртывания стека

## evo stack apply

Создать ресурсы спланированного стека (платно)

### Synopsis

```text
Создать ресурсы спланированного стека (платно).

Применить спланированный стек (из 'evo stack plan'). Ресурсы платные. Вызов запускает готовые шаги и возвращает краткое состояние; он может идти минутами: шаг ВМ ждёт, пока ВМ станет активной (вживую — около 3 минут), а асинхронные ресурсы (PostgreSQL, Kafka, Redis, Kubernetes, балансировщик) только запускаются. Затем опрашивайте 'evo stack status <job-id>' с этим job_id, пока status не станет done. Управлять ресурсами по отдельности не нужно.

Вызывает инструмент stack_apply (подробнее: 'evo tools describe stack_apply').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo stack apply <job-id> [flags]
```

### Examples

```
  evo stack apply <job-id> --dry-run
  evo stack apply <job-id> --yes
```

### Options

```
  -h, --help   Справка по apply
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo stack](#evo-stack)	 - Стеки: несколько ресурсов по одному манифесту

## evo stack destroy

Удалить все ресурсы стека (безвозвратно)

### Synopsis

```text
Удалить все ресурсы стека (безвозвратно).

Удалить все ресурсы стека — в обратном порядке, идемпотентно. status=destroyed значит, что каждое удаление принято: ВМ, секреты и объекты S3 удалены, а асинхронные ресурсы из списка deleting (PostgreSQL, Kafka, Redis, Kubernetes, балансировщик и другие) облако удаляет ещё несколько минут (PostgreSQL вживую — около 5 минут); проверьте их через 'evo pg list', 'evo k8s list' и подобные команды.

Вызывает инструмент stack_destroy (подробнее: 'evo tools describe stack_destroy').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo stack destroy <job-id> [flags]
```

### Examples

```
  evo stack destroy <job-id> --dry-run
  evo stack destroy <job-id> --yes
```

### Options

```
  -h, --help   Справка по destroy
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo stack](#evo-stack)	 - Стеки: несколько ресурсов по одному манифесту

## evo stack plan

Спланировать стек ресурсов по одному манифесту (ничего не создаёт)

### Synopsis

```text
Спланировать стек ресурсов по одному манифесту (ничего не создаёт).

Спланировать стек из нескольких ресурсов по ОДНОМУ декларативному манифесту. Для развёртывания из нескольких сервисов используйте стек, а не создавайте кластер PostgreSQL, кластер Kubernetes, ВМ, группу целей и остальное по одному: всю цепочку выполняет evo. Возвращает job_id и оценку стоимости по тарифам; стек создаёт 'evo stack apply <job-id>'. Только чтение: ничего не создаёт.

manifest = {name, vpc?{name}, postgres?{name,cpu,ram,storage_gb,no_backup}, kafka?{name,cpu,ram,storage_gib,brokers}, redis?{name,cpu,ram,storage_gb,shards,replicas,type}, k8s?{name,version,cpu,ram_gb,master_count,kube_api_internet}, registry?{name,registry_type,public}, snat?{name,zone}, vm?{name,private,flavor,image,zone}, nlb?{name,port,target_port,external,target(default 'vm')}, dns?{zone_id,name,record_type,target(default 'lb'→LB public IP)}, secrets?[{path,value}], s3?[{bucket,key,content}]}.

vm.image (имя или UUID, поиск: 'evo image list') и vm.zone (имя вида ru.AZ-1 или UUID, список: 'evo vm zones') по умолчанию берутся из CLOUDRU_IMAGE_ID и CLOUDRU_REGION сервера; план находит оба и отказывает стеку с ВМ, если чего-то из них нет или оно неизвестно, — до создания чего-либо. (Container Apps пока нет: нужен образ из реестра.)

Вызывает инструмент stack_plan (подробнее: 'evo tools describe stack_plan').
```

```
evo stack plan [flags]
```

### Examples

```
  evo stack plan --manifest stack.json
  evo stack plan --manifest stack.json --json | jq -r '.job_id'
```

### Options

```
  -h, --help              Справка по plan
      --manifest string   Файл со значением ('-' — из stdin до EOF: на терминале — Ctrl-D). Декларативное описание стека (формат — в описании)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo stack](#evo-stack)	 - Стеки: несколько ресурсов по одному манифесту

## evo stack status

Ход развёртывания стека

### Synopsis

```text
Ход развёртывания стека.

Ход развёртывания стека, кратко: status, done[], running[], pending[], outputs{postgres, vm, tg, lb, …; lb_external_ip — когда у балансировщика появился публичный адрес}. Для задания, запущенного 'evo stack apply <job-id>', каждый вызов продвигает развёртывание И ждёт внутри (до --wait-seconds), так что на долгое развёртывание хватает нескольких вызовов. Спланированное, но не запущенное задание только показывается и никогда не запускается. Повторяйте, пока status не станет done (тогда читайте outputs; lb_external_ip есть только у балансировщика с публичным адресом) или error. Задание хранится на диске: состояние можно запросить снова по job_id в любой момент.

Вызывает инструмент stack_status (подробнее: 'evo tools describe stack_status').
```

```
evo stack status <job-id> [flags]
```

### Examples

```
  evo stack status <job-id>
  evo stack status <job-id> --wait-seconds 240 --json
```

### Options

```
  -h, --help               Справка по status
      --wait-seconds int   Сколько секунд этот вызов может ждать, продвигая развёртывание (по умолчанию 90, не больше 240). Чем больше, тем меньше вызовов для долгого развёртывания
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo stack](#evo-stack)	 - Стеки: несколько ресурсов по одному манифесту

## evo status

Состояние текущего развёртывания

### Synopsis

```text
Показать развёртывание этого каталога по локальному state и живое состояние
ВМ — одним запросом к Cloud.ru (не дольше 8 с). ВМ, удалённая вне evo (в
консоли), показывается как не найденная в облаке: 'evo destroy' уберёт её
запись. Если облако спросить не удалось (нет ключа, сети, ошибка API),
status всё равно показывает локальное состояние и пишет, почему облако не
проверено. state команда не меняет.

--no-refresh — только локальный state, без запроса. С --json в ответе
добавляются cloud_checked, cloud_state, not_found_in_cloud, cloud_error и
cloud_reason.
```

```
evo status [flags]
```

### Examples

```
  evo status
  evo status --json
```

### Options

```
  -h, --help         Справка по status
      --no-refresh   Только локальный state: не спрашивать Cloud.ru о живом состоянии ВМ
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов

## evo tg

Группы целей балансировщиков

```
evo tg [flags]
```

### Options

```
  -h, --help   Справка по tg
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo tg create](#evo-tg-create)	 - Создать группу целей балансировщика (асинхронно)
* [evo tg delete](#evo-tg-delete)	 - Удалить группу целей (асинхронно)
* [evo tg get](#evo-tg-get)	 - Группа целей по ID: ВМ, VPC, состояние
* [evo tg list](#evo-tg-list)	 - Группы целей балансировщиков проекта

## evo tg create

Создать группу целей балансировщика (асинхронно)

### Synopsis

```text
Создать группу целей балансировщика (асинхронно).

Создать группу целей — набор ВМ, между которыми балансировщик распределяет трафик. Цель — одна ВМ (--vm-id) или несколько (--targets, массив {id}). Сетевой интерфейс ВМ evo находит сам: --interface-id нужен, только чтобы задать другой. Цели — приватные ВМ в подсети VPC (создаются с 'evo vm create --no-public-ip'). Операция асинхронная.

Вызывает инструмент tg_create (подробнее: 'evo tools describe tg_create').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo tg create <name> [flags]
```

### Examples

```
  evo tg create web-tg --vm-id <vm-id> --dry-run
  evo tg create web-tg --targets '{"id":"<vm-id-1>"}' --targets '{"id":"<vm-id-2>"}' --yes
```

### Options

```
  -h, --help                  Справка по create
      --interface-id string   UUID сетевого интерфейса (необязательно; по умолчанию evo берёт интерфейс ВМ)
      --targets stringArray   Несколько целей: массив {id} (interface_id необязателен, evo находит его сам) (флаг повторяется или значения через запятую)
      --vm-id string          UUID ВМ — единственной цели; интерфейс evo находит сам. Задаётся --vm-id или --targets, не оба
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo tg](#evo-tg)	 - Группы целей балансировщиков

## evo tg delete

Удалить группу целей (асинхронно)

### Synopsis

```text
Удалить группу целей (асинхронно).

Удалить группу целей по id (список: 'evo tg list'). Операция асинхронная.

Вызывает инструмент tg_delete (подробнее: 'evo tools describe tg_delete').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo tg delete <id> [flags]
```

### Examples

```
  evo tg delete <tg-id> --dry-run
  evo tg delete <tg-id> --yes
```

### Options

```
  -h, --help   Справка по delete
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo tg](#evo-tg)	 - Группы целей балансировщиков

## evo tg get

Группа целей по ID: ВМ, VPC, состояние

### Synopsis

```text
Группа целей по ID: ВМ, VPC, состояние.

Сведения об одной группе целей по id (список: 'evo tg list'): ВМ-участники, VPC, состояние. Только чтение.

Вызывает инструмент tg_get (подробнее: 'evo tools describe tg_get').
```

```
evo tg get <id> [flags]
```

### Examples

```
  evo tg get <tg-id>
```

### Options

```
  -h, --help   Справка по get
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo tg](#evo-tg)	 - Группы целей балансировщиков

## evo tg list

Группы целей балансировщиков проекта

### Synopsis

```text
Группы целей балансировщиков проекта.

Группы целей балансировщиков нагрузки проекта: id, имя, состояние, VPC. Только чтение.

Вызывает инструмент tg_list (подробнее: 'evo tools describe tg_list').
```

```
evo tg list [flags]
```

### Examples

```
  evo tg list
  evo tg list --json | jq -r '.target_groups[] | "\(.id)\t\(.name)"'
```

### Options

```
  -h, --help   Справка по list
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo tg](#evo-tg)	 - Группы целей балансировщиков

## evo tools

Список, описание и вызов инструментов evo (тех же, что у MCP-сервера)

### Synopsis

```text
Любая операция MCP-сервера доступна здесь как инструмент.

  evo tools list [--group vm]
  evo tools describe vm_get
  evo tools call vm_get --arg name=web

Инструменты, которые создают платные ресурсы или удаляют данные, спрашивают
подтверждение так же, как остальные команды: вопрос на терминале, --yes —
согласие заранее, --dry-run — только превью; без терминала и без --yes вызов
завершается с кодом 3. Описания инструментов — по-русски (с EVO_LANG=en — по-английски,
как их видит агент в MCP); ответы инструментов — на английском, как в MCP.
```

```
evo tools [flags]
```

### Options

```
  -h, --help   Справка по tools
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo tools call](#evo-tools-call)	 - Вызвать инструмент и напечатать его ответ (JSON)
* [evo tools describe](#evo-tools-describe)	 - Описание и аргументы инструмента
* [evo tools list](#evo-tools-list)	 - Список инструментов: имя, группа, меняет ли что-то, описание

## evo tools call

Вызвать инструмент и напечатать его ответ (JSON)

### Synopsis

```text
Вызвать инструмент и напечатать его ответ (JSON) как есть.

Аргументы берутся из --args-file, затем из --args-json, затем из --arg
(более поздние перекрывают ранние). Значения --arg приводятся к типам схемы
инструмента: integer, number и boolean разбираются; array — список через
запятую (или массив JSON); object — JSON. Имена и типы из JSON тоже
сверяются со схемой; null означает «не задан».

  evo tools call list_zones
  evo tools call vm_get --arg name=web
  evo tools call open_ports --arg security_group=web-sg --arg ports=80,443 --yes
  evo tools call pg_operation --arg operation_id=OP --wait

Коды выхода: 0 — успех; 3 — нет подтверждения; ошибка инструмента с кодом
(API_UNREACHABLE, AUTH_FAILED, …) — код выхода этого кода, как у других
команд (см. docs/exit-codes.md); без кода — 4, если инструмент сообщил, что
объект не найден, иначе 5.
```

```
evo tools call <name> [flags]
```

### Examples

```
  evo tools call dns_zones
  evo tools call vm_get --arg vm_id=<ID>
  evo tools call pg_operation --arg operation_id=<ID> --wait
```

### Options

```
      --arg stringArray    Аргумент инструмента: имя=значение (флаг повторяется)
      --args-file string   Прочитать аргументы инструмента из файла JSON
      --args-json string   Аргументы инструмента объектом JSON
  -h, --help               Справка по call
      --wait               Для инструментов с wait_seconds (*_operation, stack_status): ждать конечного состояния, опрашивая с нарастающей паузой до предела инструмента
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo tools](#evo-tools)	 - Список, описание и вызов инструментов evo (тех же, что у MCP-сервера)

## evo tools describe

Описание и аргументы инструмента

```
evo tools describe <name> [flags]
```

### Examples

```
  evo tools describe dns_zones
  evo tools describe provision --json
```

### Options

```
  -h, --help   Справка по describe
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo tools](#evo-tools)	 - Список, описание и вызов инструментов evo (тех же, что у MCP-сервера)

## evo tools list

Список инструментов: имя, группа, меняет ли что-то, описание

```
evo tools list [flags]
```

### Examples

```
  evo tools list
  evo tools list --json
```

### Options

```
      --group string   Только эта группа (ar, audit, billing, ca, cert, deploy, disk, dns, iam, image, k8s, kafka, monitoring, nlb, org, pg, redis, s3, secret, snat, stack, vm, vpc)
  -h, --help           Справка по list
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo tools](#evo-tools)	 - Список, описание и вызов инструментов evo (тех же, что у MCP-сервера)

## evo verify

Проверить, отвечает ли приложение (код 1 — не отвечает, 4 — ничего не развёрнуто)

### Synopsis

```text
Проверить развёрнутое приложение: TCP-соединение с каждым портом, который
публикует compose-файл, затем HTTP (или HTTPS). Ответ HTTP 2xx или 3xx —
работает; порт, который принимает соединения, но не говорит по HTTP (база
данных, кэш), — тоже.

С --json — массив, по объекту на порт, с ключами port, status_code (0 без
ответа HTTP), healthy, latency_ms, error (только если проверка не прошла) и
protocol ("http", "https", "tcp" или "", если ничего не ответило).
```

```
evo verify [flags]
```

### Examples

```
  evo verify
  evo deploy --yes && evo verify
  evo verify --json | jq -r '.[] | select(.healthy | not) | .port'
```

### Options

```
  -h, --help   Справка по verify
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов

## evo version

Версия, коммит, дата сборки, версия Go и платформа

```
evo version [flags]
```

### Options

```
  -h, --help   Справка по version
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов

## evo vm

Виртуальные машины

```
evo vm [flags]
```

### Options

```
  -h, --help   Справка по vm
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo vm create](#evo-vm-create)	 - Создать ВМ (по умолчанию с floating IP; без внешнего адреса — --no-public-ip)
* [evo vm delete](#evo-vm-delete)	 - Удалить ВМ и все данные на ней (безвозвратно)
* [evo vm flavors](#evo-vm-flavors)	 - Flavor ВМ: vCPU, RAM, бесплатный уровень, оценка цены за месяц
* [evo vm get](#evo-vm-get)	 - ВМ по ID или имени: внутренний и внешний IP, состояние, интерфейсы
* [evo vm list](#evo-vm-list)	 - Список ВМ проекта: id, имя, состояние, зона, время создания
* [evo vm zones](#evo-vm-zones)	 - Зоны доступности (UUID и имя)

## evo vm create

Создать ВМ (по умолчанию с floating IP; без внешнего адреса — --no-public-ip)

### Synopsis

```text
Создать ВМ (по умолчанию с floating IP; без внешнего адреса — --no-public-ip).

Создать ВМ без docker-compose. По умолчанию ВМ получает floating IP (внешний адрес); с --no-public-ip создаётся приватная ВМ — только адрес в подсети (интерфейс типа regular), доступна лишь внутри VPC; так создают узлы за балансировщиком. Подсеть и группа безопасности подключаются сами.

Образ — --image (имя, отображаемое имя или UUID; ищется как в 'evo image get': при нескольких совпадениях — ошибка со списком кандидатов) или --image-id (UUID). Подходит любой образ ОС, доступный проекту, в том числе Astra Linux, Альт и РЕД ОС; образ общего каталога, недоступный проекту, отклоняется до создания чего-либо.

Поля метаданных образа (SSH-ключ, имя ВМ, логин, случайный пароль) evo заполняет сам. Обязательное поле, которое evo заполнить не может ('evo image get <образ>' показывает такие поля), задаётся через --meta, иначе команда отказывает до создания ВМ.

--port открывает входящие порты в группе безопасности ВМ после её загрузки; 'evo vm delete' закрывает их. Для docker-compose проекта — 'evo deploy'. ВМ платная.

Вызывает инструмент provision (подробнее: 'evo tools describe provision').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo vm create <name> [flags]
```

### Examples

```
  evo vm create my-vm --dry-run
  evo vm create my-vm --image Ubuntu-24.04 --flavor lowcost10-2-4 --port 80 --port 443
  evo vm create astra-1 --image 'Astra Linux SE 1.8'
  evo vm create dash-1 --image OpensearchDashboards --meta opensearch_host=10.0.0.5 --meta opensearch_password=@./opensearch.pass
  pass show opensearch | evo vm create dash-2 --image OpensearchDashboards --meta opensearch_host=10.0.0.5 --meta opensearch_password=- --yes
  evo vm create worker-1 --no-public-ip --yes
```

### Options

```
      --disk-size int      Размер загрузочного диска, ГБ (по умолчанию 10, но не меньше минимума образа)
      --flavor string      Flavor ВМ: имя или UUID, например lowcost10-2-4 (список: evo vm flavors). По умолчанию — самый маленький lowcost не бесплатного уровня
  -h, --help               Справка по create
      --image string       Образ: точное имя, отображаемое имя или UUID, например Ubuntu-24.04 (поиск: evo image list). Задаётся --image или --image-id, не оба
      --image-id string    UUID образа (см. evo image list, evo image get)
      --meta stringArray   Поле метаданных образа, например admin_password=… — обязательные поля, которые evo не заполняет сам (evo image get <образ>). Перекрывает заполненное evo; поле, которого в образе нет, отклоняется (поле=значение, флаг повторяется; значение может содержать '='. поле=- читает значение из stdin (не больше одного поля), поле=@файл — из файла без завершающего перевода строки: так пароль не попадает в командную строку и историю оболочки)
      --no-public-ip       Приватная ВМ: без floating IP, интерфейс типа regular — для узлов за балансировщиком и внутренних серверов
      --port stringArray   Входящий порт, открываемый в группе безопасности ВМ для 0.0.0.0/0: 80, 8080/tcp, 53/udp (по умолчанию tcp). Группа общая для ВМ зоны; evo vm delete удаляет правила, созданные evo (флаг повторяется или значения через запятую)
      --subnet-id string   UUID подсети для интерфейса (по умолчанию — подсеть в зоне ВМ; список: evo vpc subnets)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo vm](#evo-vm)	 - Виртуальные машины

## evo vm delete

Удалить ВМ и все данные на ней (безвозвратно)

### Synopsis

```text
Удалить ВМ и все данные на ней (безвозвратно).

Удалить ВМ и все данные на ней безвозвратно. ВМ задаётся UUID или именем; превью называет ВМ (id, имя, зону, состояние, адрес) и floating IP, который будет освобождён. Несуществующая ВМ — код 4 уже на превью; имя, которое носят несколько ВМ, — отказ со списком кандидатов: укажите UUID. Floating IP ВМ по умолчанию освобождается тоже; оставить адрес — --release-ip=false. Правила группы безопасности, которые evo открыл для ВМ ('evo deploy', 'evo vm create --port'), удаляются после неё, если порт не нужен другому развёртыванию из локального состояния; правило, которое удалить не удалось, попадает в отчёт, и следующее удаление попробует снова. Если Cloud.ru принял удаление, но ВМ осталась в списке, — код 5: floating IP, правила и запись остаются до повторного удаления.

Вызывает инструмент destroy (подробнее: 'evo tools describe destroy').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo vm delete <vm-id|name> [flags]
```

### Examples

```
  evo vm delete my-vm --dry-run
  evo vm delete my-vm
```

### Options

```
  -h, --help         Справка по delete
      --release-ip   Освободить и floating IP ВМ. Выключить: --release-ip=false (по умолчанию true)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo vm](#evo-vm)	 - Виртуальные машины

## evo vm flavors

Flavor ВМ: vCPU, RAM, бесплатный уровень, оценка цены за месяц

### Synopsis

```text
Flavor ВМ: vCPU, RAM, бесплатный уровень, оценка цены за месяц.

Flavor ВМ: имя, vCPU, RAM, признак бесплатного уровня и оценка цены за месяц. Имя flavor передаётся в --flavor команд 'evo vm create' и 'evo deploy'. Только чтение.

Вызывает инструмент list_flavors (подробнее: 'evo tools describe list_flavors').
```

```
evo vm flavors [flags]
```

### Options

```
  -h, --help   Справка по flavors
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo vm](#evo-vm)	 - Виртуальные машины

## evo vm get

ВМ по ID или имени: внутренний и внешний IP, состояние, интерфейсы

### Synopsis

```text
ВМ по ID или имени: внутренний и внешний IP, состояние, интерфейсы.

Сведения о ВМ из облака по UUID или имени: внутренний IP (private_ip), внешний или floating IP (public_ip), состояние и все сетевые интерфейсы. В отличие от 'evo status', который читает локальную запись развёртывания, показывает и внутренний IP — его, например, прописывают в настройки другого сервиса. Только чтение.

Вызывает инструмент vm_get (подробнее: 'evo tools describe vm_get').
```

```
evo vm get <vm-id|name> [flags]
```

### Examples

```
  evo vm get my-vm
  evo vm get <ID> --json
```

### Options

```
  -h, --help   Справка по get
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo vm](#evo-vm)	 - Виртуальные машины

## evo vm list

Список ВМ проекта: id, имя, состояние, зона, время создания

### Synopsis

```text
Список ВМ проекта: id, имя, состояние, зона, время создания.

Все ВМ проекта по данным облака, а не по локальным записям развёртываний: id, имя, состояние, зона доступности и время создания. Только чтение. IP-адреса и интерфейсы одной ВМ — 'evo vm get <ВМ>'.

Вызывает инструмент vm_list (подробнее: 'evo tools describe vm_list').
```

```
evo vm list [flags]
```

### Examples

```
  evo vm list
  evo vm list --json | jq -r '.vms[] | "\(.name)\t\(.state)"'
  evo --project-id <UUID> vm list
```

### Options

```
  -h, --help   Справка по list
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo vm](#evo-vm)	 - Виртуальные машины

## evo vm zones

Зоны доступности (UUID и имя)

### Synopsis

```text
Зоны доступности (UUID и имя).

Зоны доступности: UUID и имя. UUID зоны передаётся в --region, например 'evo vm create --region <UUID>'. Только чтение.

Вызывает инструмент list_zones (подробнее: 'evo tools describe list_zones').
```

```
evo vm zones [flags]
```

### Options

```
  -h, --help   Справка по zones
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo vm](#evo-vm)	 - Виртуальные машины

## evo vpc

Сети: VPC, маршруты, подсети, группы безопасности, floating IP

```
evo vpc [flags]
```

### Options

```
  -h, --help   Справка по vpc
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
* [evo vpc create](#evo-vpc-create)	 - Создать VPC (асинхронно)
* [evo vpc delete](#evo-vpc-delete)	 - Удалить VPC (асинхронно; сначала удалите его подсети и ресурсы)
* [evo vpc fip-attach](#evo-vpc-fip-attach)	 - Привязать свободный floating IP к основному интерфейсу ВМ
* [evo vpc fip-release](#evo-vpc-fip-release)	 - Освободить floating IP (адрес, занятый ВМ или SNAT-шлюзом, не освобождается)
* [evo vpc fips](#evo-vpc-fips)	 - Floating IP: адрес, состояние, свободен ли
* [evo vpc get](#evo-vpc-get)	 - VPC по ID: все сведения
* [evo vpc list](#evo-vpc-list)	 - Список VPC проекта
* [evo vpc open-ports](#evo-vpc-open-ports)	 - Открыть входящие порты в группе безопасности
* [evo vpc operation](#evo-vpc-operation)	 - Состояние асинхронной операции VPC
* [evo vpc route-create](#evo-vpc-route-create)	 - Добавить статический маршрут в VPC (асинхронно)
* [evo vpc route-delete](#evo-vpc-route-delete)	 - Удалить статический маршрут из VPC (асинхронно)
* [evo vpc routes](#evo-vpc-routes)	 - Статические маршруты VPC
* [evo vpc security-groups](#evo-vpc-security-groups)	 - Группы безопасности (с --rules — вместе с правилами)
* [evo vpc subnets](#evo-vpc-subnets)	 - Подсети: UUID, имя, зона доступности

## evo vpc create

Создать VPC (асинхронно)

### Synopsis

```text
Создать VPC (асинхронно).

Создать VPC (виртуальное частное облако). Операция асинхронная: ход — 'evo vpc operation <operation-id>'.

Вызывает инструмент vpc_create (подробнее: 'evo tools describe vpc_create').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo vpc create <name> [flags]
```

### Options

```
      --description string   Описание VPC (необязательно)
  -h, --help                 Справка по create
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Сети: VPC, маршруты, подсети, группы безопасности, floating IP

## evo vpc delete

Удалить VPC (асинхронно; сначала удалите его подсети и ресурсы)

### Synopsis

```text
Удалить VPC (асинхронно; сначала удалите его подсети и ресурсы).

Удалить VPC по id (список: 'evo vpc list'). Сначала удалите его подсети и другие ресурсы. Операция асинхронная: ход — 'evo vpc operation <operation-id>'.

Вызывает инструмент vpc_delete (подробнее: 'evo tools describe vpc_delete').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo vpc delete <id> [flags]
```

### Options

```
  -h, --help   Справка по delete
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Сети: VPC, маршруты, подсети, группы безопасности, floating IP

## evo vpc fip-attach

Привязать свободный floating IP к основному интерфейсу ВМ

### Synopsis

```text
Привязать свободный floating IP к основному интерфейсу ВМ.

Привязать существующий свободный публичный IP (floating IP) к основному интерфейсу ВМ: ВМ получает внешний адрес. Нужно, когда у ВМ нет публичного IP — например, создание ВМ из большого собственного образа завершилось раньше, чем облако назначило адрес. Свободный адрес — в 'evo vpc fips'.

Вызывает инструмент fip_attach (подробнее: 'evo tools describe fip_attach').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo vpc fip-attach <fip> [flags]
```

### Options

```
  -h, --help        Справка по fip-attach
      --vm string   ВМ, к которой привязать адрес: имя или UUID
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Сети: VPC, маршруты, подсети, группы безопасности, floating IP

## evo vpc fip-release

Освободить floating IP (адрес, занятый ВМ или SNAT-шлюзом, не освобождается)

### Synopsis

```text
Освободить floating IP (адрес, занятый ВМ или SNAT-шлюзом, не освобождается).

Освободить (удалить) публичный IP: адрес уходит из проекта. Адрес задаётся аргументом <id> или флагом --ip-address. Адрес, занятый ВМ, не освобождается: сначала отвяжите его или удалите ВМ ('evo vm delete'). Адрес SNAT-шлюза тоже не освобождается: он удаляется вместе со шлюзом ('evo snat delete').

Вызывает инструмент release_floating_ip (подробнее: 'evo tools describe release_floating_ip').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo vpc fip-release [id] [flags]
```

### Options

```
  -h, --help                Справка по fip-release
      --ip-address string   Публичный IP-адрес, например 203.0.113.11 (вместо аргумента <id>)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Сети: VPC, маршруты, подсети, группы безопасности, floating IP

## evo vpc fips

Floating IP: адрес, состояние, свободен ли

### Synopsis

```text
Floating IP: адрес, состояние, свободен ли.

Публичные IP-адреса (floating IP) проекта: адрес, состояние, свободен ли адрес для привязки. Только чтение.

Вызывает инструмент list_floating_ips (подробнее: 'evo tools describe list_floating_ips').
```

```
evo vpc fips [flags]
```

### Options

```
  -h, --help   Справка по fips
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Сети: VPC, маршруты, подсети, группы безопасности, floating IP

## evo vpc get

VPC по ID: все сведения

### Synopsis

```text
VPC по ID: все сведения.

Все сведения об одном VPC по id (список: 'evo vpc list'). Только чтение.

Вызывает инструмент vpc_get (подробнее: 'evo tools describe vpc_get').
```

```
evo vpc get <id> [flags]
```

### Options

```
  -h, --help   Справка по get
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Сети: VPC, маршруты, подсети, группы безопасности, floating IP

## evo vpc list

Список VPC проекта

### Synopsis

```text
Список VPC проекта.

VPC (виртуальные частные облака) проекта: id, имя, признак VPC по умолчанию, тип. Только чтение.

Вызывает инструмент vpc_list (подробнее: 'evo tools describe vpc_list').
```

```
evo vpc list [flags]
```

### Options

```
  -h, --help   Справка по list
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Сети: VPC, маршруты, подсети, группы безопасности, floating IP

## evo vpc open-ports

Открыть входящие порты в группе безопасности

### Synopsis

```text
Открыть входящие порты в группе безопасности.

Открыть входящие порты в группе безопасности Cloud.ru: добавить правила ingress, по умолчанию tcp из 0.0.0.0/0. Так делают сервис доступным (например, 80 и 443 для веб-сервера). Группа — аргумент <security-group> (имя или UUID). Уже открытые порты пропускаются. Группы безопасности общие для зоны доступности: открытый порт затрагивает каждую ВМ в этой группе.

Вызывает инструмент open_ports (подробнее: 'evo tools describe open_ports').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo vpc open-ports <security-group> [flags]
```

### Examples

```
  evo vpc open-ports SSH-access_ru.AZ-2 --port 80 --port 443 --dry-run
  evo vpc open-ports SSH-access_ru.AZ-2 --port 80,443 --yes
  evo vpc open-ports <security-group-id> --port 53 --protocol udp --cidr 10.0.0.0/8 --yes
```

### Options

```
      --cidr string        CIDR источника (по умолчанию 0.0.0.0/0 — откуда угодно)
  -h, --help               Справка по open-ports
      --port stringArray   Порты, которые открыть, например 80,443 (флаг повторяется или значения через запятую)
      --protocol string    Протокол: tcp (по умолчанию) или udp
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Сети: VPC, маршруты, подсети, группы безопасности, floating IP

## evo vpc operation

Состояние асинхронной операции VPC

### Synopsis

```text
Состояние асинхронной операции VPC.

Состояние асинхронной операции VPC по id — его возвращают 'evo vpc create', 'evo vpc delete', 'evo vpc route-create' и 'evo vpc route-delete': done — завершена ли операция, error — ошибка. С --wait-seconds команда сама ждёт завершения до N секунд и выводит итоговое состояние. Только чтение.

Вызывает инструмент vpc_operation (подробнее: 'evo tools describe vpc_operation').
```

```
evo vpc operation <operation-id> [flags]
```

### Options

```
  -h, --help               Справка по operation
      --wait-seconds int   Ждать итогового состояния до N секунд (не больше 300), опрашивая операцию с нарастающим интервалом. Без флага — одна проверка
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Сети: VPC, маршруты, подсети, группы безопасности, floating IP

## evo vpc route-create

Добавить статический маршрут в VPC (асинхронно)

### Synopsis

```text
Добавить статический маршрут в VPC (асинхронно).

Добавить статический маршрут в VPC. Назначение — CIDR (--subnet); следующий узел — интерфейс ВМ (--next-hop-vm-id и --next-hop-interface-id), виртуальный IP (--next-hop-vip-id) или Magic Router (--next-hop-magic-router-id и --next-hop-az). Операция асинхронная: ход — 'evo vpc operation <operation-id>'.

Вызывает инструмент vpc_route_create (подробнее: 'evo tools describe vpc_route_create').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo vpc route-create <vpc-id> [flags]
```

### Options

```
      --description string                Описание маршрута (необязательно)
  -h, --help                              Справка по route-create
      --next-hop-az string                Зона доступности (например, ru.AZ-1) для следующего узла Magic Router
      --next-hop-interface-id string      UUID сетевого интерфейса ВМ (вместе с --next-hop-vm-id)
      --next-hop-magic-router-id string   UUID Magic Router — следующего узла (вместе с --next-hop-az)
      --next-hop-vip-id string            UUID виртуального IP — следующего узла
      --next-hop-vm-id string             UUID ВМ, интерфейс которой — следующий узел (вместе с --next-hop-interface-id)
      --subnet string                     CIDR назначения, например 192.168.0.0/24 (обязательный)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Сети: VPC, маршруты, подсети, группы безопасности, floating IP

## evo vpc route-delete

Удалить статический маршрут из VPC (асинхронно)

### Synopsis

```text
Удалить статический маршрут из VPC (асинхронно).

Удалить статический маршрут из VPC по id маршрута (список: 'evo vpc routes <vpc-id>'). Операция асинхронная: ход — 'evo vpc operation <operation-id>'.

Вызывает инструмент vpc_route_delete (подробнее: 'evo tools describe vpc_route_delete').

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo vpc route-delete <vpc-id> <route-id> [flags]
```

### Options

```
  -h, --help   Справка по route-delete
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Сети: VPC, маршруты, подсети, группы безопасности, floating IP

## evo vpc routes

Статические маршруты VPC

### Synopsis

```text
Статические маршруты VPC.

Статические маршруты VPC: id, подсеть назначения, тип следующего узла (next_hop_type), состояние. Только чтение.

Вызывает инструмент vpc_routes (подробнее: 'evo tools describe vpc_routes').
```

```
evo vpc routes <vpc-id> [flags]
```

### Options

```
  -h, --help   Справка по routes
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Сети: VPC, маршруты, подсети, группы безопасности, floating IP

## evo vpc security-groups

Группы безопасности (с --rules — вместе с правилами)

### Synopsis

```text
Группы безопасности (с --rules — вместе с правилами).

Группы безопасности проекта: UUID, имя, зона доступности. С --rules — вместе с правилами каждой группы: id, направление, протокол, диапазон портов, источник, описание, состояние. Только чтение.

Вызывает инструмент list_security_groups (подробнее: 'evo tools describe list_security_groups').
```

```
evo vpc security-groups [flags]
```

### Options

```
  -h, --help    Справка по security-groups
      --rules   Вывести и правила каждой группы (по одному запросу к API на группу)
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Сети: VPC, маршруты, подсети, группы безопасности, floating IP

## evo vpc subnets

Подсети: UUID, имя, зона доступности

### Synopsis

```text
Подсети: UUID, имя, зона доступности.

Подсети проекта: UUID, имя, зона доступности. UUID подсети передаётся в --subnet-id, например 'evo vm create --subnet-id <UUID>'. Только чтение.

Вызывает инструмент list_subnets (подробнее: 'evo tools describe list_subnets').
```

```
evo vpc subnets [flags]
```

### Options

```
  -h, --help   Справка по subnets
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo vpc](#evo-vpc)	 - Сети: VPC, маршруты, подсети, группы безопасности, floating IP

## evo whoami

Активный профиль и проверка его ключа

### Synopsis

```text
Обменять ключ доступа активного профиля на токен IAM и показать профиль,
Key ID (маскированный), проект и срок действия токена.

Действующий токен, сохранённый предыдущей командой, используется повторно:
к IAM команда обращается, только когда срок токена подходит к концу. Код
выхода 2 — ключа нет или IAM его отклонил.
```

```
evo whoami [flags]
```

### Examples

```
  evo whoami
  evo --profile test whoami
  evo whoami --json
```

### Options

```
  -h, --help   Справка по whoami
```

### Options inherited from parent commands

```
      --dry-run             Показать, что будет создано или удалено (с оценкой цены), и выйти
      --json                Вывод в формате JSON
      --no-color            Без цвета
      --profile string      Профиль с ключом доступа (по умолчанию $CLOUDRU_PROFILE или "default")
      --project-id string   UUID проекта Cloud.ru (по умолчанию из профиля или $CLOUDRU_PROJECT_ID)
      --region string       UUID зоны доступности (по умолчанию из профиля или $CLOUDRU_REGION)
      --verbose             Подробный журнал
  -y, --yes                 Подтвердить без вопроса операции, которые создают платные ресурсы или удаляют данные
```

### SEE ALSO

* [evo](#evo)	 - Cloud.ru Evolution CLI CREW edition: CLI и MCP-сервер для ВМ, сетей, k8s, PostgreSQL и deploy compose-проектов
