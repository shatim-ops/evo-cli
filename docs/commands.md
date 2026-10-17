# Справка по командам

<!-- Файл сгенерирован из дерева команд (cobra/doc). Руками не править:
     go test ./internal/cli -run TestCommandsDoc -update-docs
     CI сверяет файл с бинарём (TestCommandsDoc). -->

То же самое печатает `evo <команда> --help`; с `EVO_LANG=en` справка
английская (решение D6, пересмотр). Заголовки разделов (Synopsis, Options)
ставит генератор cobra/doc. В деревьях `vm` и `disk` описания команд и флаги
русские; в остальных деревьях они пока на английском, как в MCP. Подробнее — в
[README](../README.md) и соседних файлах `docs/`.

## Содержание

- [evo](#evo)
  - [evo completion](#evo-completion)
    - [evo completion bash](#evo-completion-bash)
    - [evo completion fish](#evo-completion-fish)
    - [evo completion powershell](#evo-completion-powershell)
    - [evo completion zsh](#evo-completion-zsh)
  - [evo configure](#evo-configure)
    - [evo configure delete](#evo-configure-delete)
    - [evo configure list](#evo-configure-list)
  - [evo deploy](#evo-deploy)
  - [evo destroy](#evo-destroy)
  - [evo disk](#evo-disk)
    - [evo disk attach](#evo-disk-attach)
    - [evo disk create](#evo-disk-create)
    - [evo disk delete](#evo-disk-delete)
    - [evo disk detach](#evo-disk-detach)
    - [evo disk list](#evo-disk-list)
    - [evo disk resize](#evo-disk-resize)
    - [evo disk types](#evo-disk-types)
  - [evo doctor](#evo-doctor)
  - [evo image](#evo-image)
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
  - [evo list-zones](#evo-list-zones)
  - [evo logs](#evo-logs)
  - [evo marketplace](#evo-marketplace)
    - [evo marketplace list](#evo-marketplace-list)
  - [evo mcp](#evo-mcp)
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
  - [evo status](#evo-status)
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

* [evo completion](#evo-completion)	 - Скрипт автодополнения для оболочки
* [evo configure](#evo-configure)	 - Ключ доступа и проект по умолчанию для профиля
* [evo deploy](#evo-deploy)	 - Развернуть docker-compose проект на ВМ Cloud.ru
* [evo destroy](#evo-destroy)	 - Удалить развёрнутую ВМ и связанные ресурсы
* [evo disk](#evo-disk)	 - Диски
* [evo doctor](#evo-doctor)	 - Проверить настройки, ключ доступа и связь с API
* [evo image](#evo-image)	 - Образы ВМ: операционные системы и продукты маркетплейса
* [evo init](#evo-init)	 - Создать .evo.yaml с зоной и образом из API Cloud.ru
* [evo k8s](#evo-k8s)	 - Кластеры Managed Kubernetes
* [evo list-zones](#evo-list-zones)	 - Список зон доступности
* [evo logs](#evo-logs)	 - Журналы контейнеров на развёрнутой ВМ
* [evo marketplace](#evo-marketplace)	 - Витрина: образы ВМ и плагины Kubernetes с командой развёртывания каждого
* [evo mcp](#evo-mcp)	 - MCP-сервер для ИИ-агентов (stdio или Streamable HTTP с --http)
* [evo pg](#evo-pg)	 - Managed PostgreSQL: кластеры, базы данных, пользователи, резервные копии
* [evo project](#evo-project)	 - Проекты, которые видит ключ, и проект профиля
* [evo s3](#evo-s3)	 - Object Storage: бакеты и объекты
* [evo secret](#evo-secret)	 - Secret Manager: секреты
* [evo status](#evo-status)	 - Состояние текущего развёртывания
* [evo tools](#evo-tools)	 - Список, описание и вызов инструментов evo (тех же, что у MCP-сервера)
* [evo verify](#evo-verify)	 - Проверить, отвечает ли приложение (код 1 — не отвечает, 4 — ничего не развёрнуто)
* [evo version](#evo-version)	 - Версия, коммит, дата сборки, версия Go и платформа
* [evo vm](#evo-vm)	 - Виртуальные машины
* [evo vpc](#evo-vpc)	 - Сети: VPC, маршруты, подсети, группы безопасности, floating IP
* [evo whoami](#evo-whoami)	 - Активный профиль и проверка его ключа

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
      --non-interactive       Никогда не спрашивать; тогда для создания ВМ нужен --yes
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
* [evo image get](#evo-image-get)	 - Образ по имени или UUID: минимум ресурсов, зоны, поля и кто их заполнит, команда создания ВМ
* [evo image list](#evo-image-list)	 - Поиск образов: ОС (public), маркетплейс (marketplace), образы проекта (private)

## evo image get

Образ по имени или UUID: минимум ресурсов, зоны, поля и кто их заполнит, команда создания ВМ

### Synopsis

```text
Образ по имени или UUID: минимум ресурсов, зоны, поля и кто их заполнит, команда создания ВМ.

Вызывает инструмент image_get (подробнее: 'evo tools describe image_get').
Описание инструмента и его флагов — на английском, как в MCP:

Get one VM image by name, display name or UUID (resolved like image_list: exact name, exact display_name, then a unique substring; several matches → USAGE with candidates[]). Returns the image, ready and zones_ready, and fields[]: each image_metadata field with filled_by — evo (SSH key, VM name, login, random password), default, user (mandatory, evo cannot fill it) or none — plus create{cli, tool, args}. An image the project cannot use comes with available_in_project=false, a note and no create. The icon and the cloud-init user_data_template only with full=true. Read-only. Also the way to poll an image build until ready=true.
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
      --full   Also return the SVG icon and the cloud-init user_data_template (large).
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

Вызывает инструмент image_list (подробнее: 'evo tools describe image_list').
Описание инструмента и его флагов — на английском, как в MCP:

Search the VM image catalogue: OS images (type public), Cloud.ru Marketplace products (marketplace) and the project's own images (private). query matches a substring of the name or display_name, case-insensitive. Images ready in no zone and images of the shared catalogue the project cannot use are hidden unless all=true (then the latter carry available_in_project=false; hidden_not_in_project counts the hidden ones). Compact rows {id, name, display_name, type, zones_ready, min_disk_gb}, public first; total counts the matches before hiding. Next: image_get for the fields a VM from the image needs; the id goes to provision or deploy as image_id.
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
      --all           Also show catalogue images ready in no zone and those the project cannot use (hidden by default: the shared catalogue carries test and broken images, and images a VM of this project cannot be created from — these come with available_in_project=false).
  -h, --help          Справка по list
      --type string   Only this type: public (OS images), marketplace (products of the Cloud.ru Marketplace) or private (images of the project). Default: all three.
      --zone string   Only images ready in this availability zone: name ('ru.AZ-1', 'AZ-1') or UUID.
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

Вызывает инструмент k8s_add_addon (подробнее: 'evo tools describe k8s_add_addon').
Описание инструмента и его флагов — на английском, как в MCP:

Install a Managed Kubernetes addon release into a cluster. REQUIRES confirmed=true after the user approves. Without it only reads and returns a preview: the version picked (default: newest ACTIVE), namespace, the release's requirements and the cluster checks (Kubernetes version against kube_versions, node pools against ready_workers; what cannot be determined is listed in not_checked, a certain mismatch refuses with USAGE). Async: answers status installing with operation_id / cluster_addon_id; readiness: k8s_kubectl args="get pods -n <namespace>" (or -A) until the addon's pods are Running and Ready (pods of a Job, e.g. cert-manager-startupapicheck, end Completed). Pods stuck in ImagePullBackOff may mean the cluster has no internet egress: Cloud.ru needs a public SNAT gateway in the nodes' zone for the cluster and pods to reach the internet. resolve_mode OVERWRITE may replace objects already in the cluster.

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
      --config string         A file with the value ('-' reads stdin until EOF: Ctrl-D on a terminal). Addon configuration matching the release's configuration schema (k8s_addon_versions full=true). A JSON object; a string holding a JSON object is accepted too. Cloud.ru substitutes {{clusterID}}, {{clusterName}}, {{productInstanceID}}, {{nodeCIDR}} and {{serviceCIDR}} in it. Default: the API reuses the addon's last configuration in the cluster (the addon's defaults on a first install).
  -h, --help                  Справка по add-addon
      --resolve-mode string   What to do with objects of the addon already in the cluster (installed by hand): NONE keeps them, the install may then fail; OVERWRITE resets them to the recommended values. Default NONE.
      --version string        Release version: the app version from k8s_addon_versions. The API installs by app version and picks the addon_version itself, so an addon_version is accepted only when it is the newest of its app version. Default: newest ACTIVE.
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

Вызывает инструмент k8s_add_nodepool (подробнее: 'evo tools describe k8s_add_nodepool').
Описание инструмента и его флагов — на английском, как в MCP:

Add a worker node pool to a Managed Kubernetes cluster (workers to run pods — a fresh cluster has none). REQUIRES confirmed=true after the user approves (billable). Async. Auto-resolves flavor (cpu/ram or name), nodes subnet and security group. NB: Cloud.ru's node-pool backend occasionally returns 503 'no healthy upstream' (transient outage) — just retry.

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo k8s add-nodepool <cluster-id> [flags]
```

### Options

```
      --count int                  Number of worker nodes (fixed scale, default 2).
      --cpu int                    Node vCPUs for auto flavor (default 2).
      --disk-gb int                Node disk GB, min 10 (default 20).
      --flavor string              Node flavor name (see k8s_options); overrides cpu/ram_gb.
  -h, --help                       Справка по add-nodepool
      --name string                Node pool name, 4-60 (default nodepool-1).
      --nodes-subnet-id string     Subnet UUID for worker nodes (default auto zone-matched).
      --ram-gb int                 Node RAM GB for auto flavor (default 4).
      --security-group-id string   Security group UUID for nodes (default auto zone-matched SSH-access).
      --zone string                Zone name (default: the zone of the cluster's control plane; refused when the cluster does not report it).
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

Вызывает инструмент k8s_addon_versions (подробнее: 'evo tools describe k8s_addon_versions').
Описание инструмента и его флагов — на английском, как в MCP:

List the releases of one Managed Kubernetes addon, newest first: version (appVersion, what k8s_add_addon takes), addon_version, mode (ACTIVE, EXPERIMENTAL, END_OF_SUPPORT, END_OF_LIFE, INACTIVE), kube_versions (semver range the cluster must satisfy), ready_workers (ready worker nodes needed), namespace, has_schema and schema_bytes of the configuration JSON Schema, latest (the release k8s_add_addon picks by default: newest ACTIVE). Read-only. The schema itself (up to ~94 KB) only with full=true.
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
      --full   Include configuration_schema of every release (large; default false).
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

Вызывает инструмент k8s_addons (подробнее: 'evo tools describe k8s_addons').
Описание инструмента и его флагов — на английском, как в MCP:

Search the Managed Kubernetes addon catalogue (Cloud.ru-packaged plugins: ingress, cert-manager, monitoring, CSI drivers, …). Read-only. Compact rows {name, display_name, categories, billable, deploy_with}; categories are Russian words of the console (e.g. 'Сети', 'Безопасность', 'Мониторинг'), the answer lists them all. Next: k8s_addon_versions for versions and requirements, k8s_add_addon to install.
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
      --billable          true: only billable addons; false: only free ones. Default: both.
      --category string   Only addons of this category, exact and case-insensitive (see 'categories' in the answer).
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

Вызывает инструмент k8s_apply (подробнее: 'evo tools describe k8s_apply').
Описание инструмента и его флагов — на английском, как в MCP:

Apply a Kubernetes manifest (YAML/JSON, may be multi-document) to a cluster — like `kubectl apply -f -`. For deploying/updating raw workloads (Deployments, StatefulSets, Services, ServiceMonitors, etc.). Mutation: requires confirmed=true after the user approves. NB: a fresh cluster needs egress (snat_create) to pull public images, and CRDs (e.g. ServiceMonitor) must exist before objects that use them.

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo k8s apply <cluster-id> [flags]
```

### Options

```
  -h, --help               Справка по apply
      --manifest string    The manifest YAML/JSON to apply (multi-doc allowed).
      --namespace string   Namespace to apply into (optional; manifests may set their own).
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

Вызывает инструмент k8s_create (подробнее: 'evo tools describe k8s_create').
Описание инструмента и его флагов — на английском, как в MCP:

STEP 2 for Managed Kubernetes: create the cluster resolved by k8s_plan. REQUIRES confirmed=true after the user approves (billable). Async — returns cluster_id + operation_id; the cluster provisions in about 10 min (5-10 per the docs; poll k8s_get). Accepts the same args as k8s_plan.

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo k8s create [flags]
```

### Options

```
      --cluster-sa-id string   Cluster service-account UUID.
      --cpu int                Control-plane vCPUs.
      --flavor string          Control-plane flavor name.
  -h, --help                   Справка по create
      --kube-api-internet      Publish kube-apiserver to the internet. To turn off: --kube-api-internet=false (по умолчанию true)
      --master-count int       1, 3 or 5 (default 1).
      --name string            Cluster name.
      --product string         What the cluster is for.
      --ram-gb int             Control-plane RAM GB.
      --subnet-id string       Control-plane VIP subnet UUID, of the cluster's zone.
      --version string         Kubernetes version.
      --zone string            Zone name or UUID (as in k8s_plan).
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

Вызывает инструмент k8s_delete (подробнее: 'evo tools describe k8s_delete').
Описание инструмента и его флагов — на английском, как в MCP:

Permanently delete a Managed Kubernetes cluster. REQUIRES confirmed=true after the user approves. Async.

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

Вызывает инструмент k8s_get (подробнее: 'evo tools describe k8s_get').
Описание инструмента и его флагов — на английском, как в MCP:

Get a Managed Kubernetes cluster by id (full details incl. status). Read-only.
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

Вызывает инструмент k8s_helm (подробнее: 'evo tools describe k8s_helm').
Описание инструмента и его флагов — на английском, как в MCP:

Drive Helm on a cluster: action=install|upgrade|uninstall|list|status. install/upgrade are idempotent (helm upgrade --install --wait). Optionally add a chart repo via repo_url. Pass values (inline YAML) and/or set (array of key=value). Mutating actions require confirmed=true. Ideal for standard products (e.g. kube-prometheus-stack, databases). NB: nodes need egress (snat_create) to pull chart images; size the node pool for the chart (monitoring stacks want ≥8GB nodes).

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo k8s helm <cluster-id> <action> [flags]
```

### Options

```
      --chart string       Chart ref, e.g. 'prometheus-community/kube-prometheus-stack' (install/upgrade).
  -h, --help               Справка по helm
      --namespace string   Target namespace (created on install/upgrade).
      --release string     Helm release name (install/upgrade/uninstall/status).
      --repo-url string    Chart repo URL to add+update first; repo name taken from the chart's '<repo>/' prefix.
      --set stringArray    Array of 'key=value' Helm --set overrides (optional). (repeatable, or comma-separated)
      --values string      Inline values.yaml content (optional).
      --version string     Chart version (optional; default latest).
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

Вызывает инструмент k8s_kubeconfig (подробнее: 'evo tools describe k8s_kubeconfig').
Описание инструмента и его флагов — на английском, как в MCP:

Get the kubeconfig for a Managed Kubernetes cluster. By DEFAULT it embeds a static bearer token so kubectl/helm work as-is (the raw Cloud.ru kubeconfig needs a 'cloudlogin' exec-plugin that isn't installed → kubectl would fail 'executable cloudlogin not found'). Token TTL ~1h. Pass embed_token=false for the raw exec-plugin kubeconfig.
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
      --embed-token      Embed a static bearer token so kubectl works without the cloudlogin plugin. To turn off: --embed-token=false (по умолчанию true)
  -h, --help             Справка по kubeconfig
      --network string   NETWORK_TYPE_PUBLIC or NETWORK_TYPE_PRIVATE (default: cluster default).
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

Вызывает инструмент k8s_kubectl (подробнее: 'evo tools describe k8s_kubectl').
Описание инструмента и его флагов — на английском, как в MCP:

Run ANY kubectl command against a cluster (auto-embedded token — no cloudlogin needed). Read verbs (get/describe/logs/top/events/'rollout status'/explain/…) run freely for inspection & verification. Mutating verbs (create/delete/patch/scale/label/'rollout restart'/exec/cordon/…) require confirmed=true. General escape hatch for whatever a product needs beyond helm/apply. Example args: "get pods -A", "-n db logs deploy/postgres", "create secret generic s --from-literal=k=v".

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

Вызывает инструмент k8s_list (подробнее: 'evo tools describe k8s_list').
Описание инструмента и его флагов — на английском, как в MCP:

List Managed Kubernetes (mk8s) clusters in the project (id, name, status). Read-only.
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

Вызывает инструмент k8s_nodepools (подробнее: 'evo tools describe k8s_nodepools').
Описание инструмента и его флагов — на английском, как в MCP:

List node pools of one Managed Kubernetes cluster (name, state, desired/ready nodes). Read-only. The API has no list across clusters: get cluster_id from k8s_list.
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

Вызывает инструмент k8s_options (подробнее: 'evo tools describe k8s_options').
Описание инструмента и его флагов — на английском, как в MCP:

List available Kubernetes versions (by release channel), node flavors and zones for mk8s. Read-only. Use before k8s_plan in manual mode.
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

Вызывает инструмент k8s_plan (подробнее: 'evo tools describe k8s_plan').
Описание инструмента и его флагов — на английском, как в MCP:

STEP 1 for Managed Kubernetes: resolve a cluster config (k8s version, control-plane flavor, master count, subnet, service account) WITHOUT creating anything. Show it to the user, then call k8s_create with the same args + confirmed=true.
```

```
evo k8s plan [flags]
```

### Options

```
      --cluster-sa-id string   Cluster service-account UUID (default: the agent's own service account).
      --cpu int                Control-plane vCPUs for auto flavor pick (default 2).
      --flavor string          Control-plane flavor name (see k8s_options); overrides cpu/ram_gb.
  -h, --help                   Справка по plan
      --kube-api-internet      Publish kube-apiserver to the internet. To turn off: --kube-api-internet=false (по умолчанию true)
      --master-count int       Control-plane node count: 1, 3 or 5 (default 1); any other value is refused.
      --name string            Cluster name (3-60; default k8s-<product>).
      --product string         What the cluster is for (used for the name).
      --ram-gb int             Control-plane RAM GB for auto flavor pick (default 4).
      --subnet-id string       Subnet UUID for the control-plane private VIP, of the cluster's zone (default: the zone's main subnet). A subnet of another zone is refused.
      --version string         Kubernetes version, e.g. v1.34.6 (default: newest STABLE; see k8s_options).
      --zone string            Availability zone of the control plane, name or UUID, e.g. ru.AZ-2 (default: the zone of subnet_id, else the default of productConfiguration).
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

## evo list-zones

Список зон доступности

### Synopsis

```text
Список зон доступности из API Cloud.ru.

ID зоны передаётся в deploy флагом --region или переменной CLOUDRU_REGION.
Для машинного разбора — --json.
```

```
evo list-zones [flags]
```

### Examples

```
  evo list-zones
  evo list-zones --json
```

### Options

```
  -h, --help   Справка по list-zones
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

Вызывает инструмент marketplace_list (подробнее: 'evo tools describe marketplace_list').
Описание инструмента и его флагов — на английском, как в MCP:

Read-only showcase of what Cloud.ru offers to deploy, in one list: VM images of the shared catalogue (OS images and Cloud.ru Marketplace products; the project's own images are not included) and Managed Kubernetes addons. Changes nothing. kind: vm-os (type public, or a marketplace image whose name or display name starts with an OS family word: Ubuntu, Debian, CentOS, Rocky, Alma, Fedora, openSUSE, Astra, Alt/Альт, RED OS/РЕД ОС, SberLinux, ROSA/РОСА, Windows — a heuristic, the catalogue has no such field), vm-app (any other marketplace image, e.g. nginx-ubuntu-22.04), k8s-addon. query matches a substring of the name or display_name (addons: the description too), case-insensitive. Images the project cannot use and images ready in no zone are hidden unless all=true (counted in hidden_not_in_project, hidden_not_ready). Rows {kind, name, display_name, id (images), billable (addons only: whether an image or its licence is paid is not known — the price calculation gives 0 for every image), available_in_project (images, when checked against the catalogue as the project sees it), ready=false (an image ready in no zone, with all=true), deploy{cli, tool, args}}: deploy is provision with args.image (the name, or the UUID when names repeat) or, for an addon, k8s_add_addon with args.name (plus cluster_id; k8s_addon_versions shows its versions); an image not available to the project has no deploy. Sorted by kind, then display name; counts{vm-os, vm-app, k8s-addon} are the rows shown per kind, total the matches before hiding. If one catalogue cannot be read, the other is still answered, with warnings[]; an error only when none is read.
```

```
evo marketplace list [query] [flags]
```

### Options

```
      --all           Also show the images hidden by default: those the project cannot use (available_in_project=false, no deploy) and those ready in no zone (ready=false).
  -h, --help          Справка по list
      --kind string   Only this kind: vm-os (OS images), vm-app (Marketplace products on a VM) or k8s-addon (Managed Kubernetes addons). Default: all three; a VM kind does not read the addon catalogue and k8s-addon not the image one.
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

Вызывает инструмент pg_backup_create (подробнее: 'evo tools describe pg_backup_create').
Описание инструмента и его флагов — на английском, как в MCP:

Start a manual backup of a PostgreSQL cluster (async). Mutation: requires confirmed=true after the user approves.

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

Вызывает инструмент pg_backups (подробнее: 'evo tools describe pg_backups').
Описание инструмента и его флагов — на английском, как в MCP:

List backups of a PostgreSQL cluster (name, status, size, timestamps). Read-only.
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

Вызывает инструмент pg_create (подробнее: 'evo tools describe pg_create').
Описание инструмента и его флагов — на английском, как в MCP:

Create a Managed PostgreSQL cluster. Refuses a name an existing cluster has (pg_list). Resolves version (default: latest), specification (by specification_id OR cpu+ram; none matching is a USAGE error with the list), storage (default: spec minimum), and subnet (default: first project subnet). Without confirmed=true returns a preview only. Mutation (BILLABLE, async ~minutes): requires confirmed=true after the user approves. Poll the returned operation_id with pg_operation.

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
      --backup-retention-days int   Retention for automatic backups in days (default 14, max 30).
      --backup-schedule string      CRON schedule for automatic backups (default '0 3 * * 0' = weekly Sun 03:00; min once per 6h).
      --cpu int                     vCPU count — used to auto-pick a specification when specification_id is omitted.
      --description string          Optional cluster description.
  -h, --help                        Справка по create
      --initial-database string     Name of the default database to create. Default 'defaultdb'.
      --instances int               Number of nodes (1-2 standard, 1-3 business: checked against the spec's max_hosts). Default 1.
      --no-backup                   Disable automatic backups. Backups are ENABLED by default and CANNOT be toggled after creation; a cluster without backups rejects pg_backup_create/pg_backups with 'backup disabled'.
      --ram int                     RAM in GB — used with cpu to auto-pick a specification.
      --specification-id string     Specification UUID (from pg_specs). Optional if cpu+ram given.
      --storage-gb int              Main data disk size in GB. Optional; default = specification minimum.
      --subnet-id string            Subnet UUID (defines the AZ). Optional; default = first project subnet.
      --version string              PostgreSQL version (from pg_versions). Optional; default = latest.
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

Вызывает инструмент pg_databases (подробнее: 'evo tools describe pg_databases').
Описание инструмента и его флагов — на английском, как в MCP:

List databases in a PostgreSQL cluster (name, owner, size). Read-only.
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

Вызывает инструмент pg_db_create (подробнее: 'evo tools describe pg_db_create').
Описание инструмента и его флагов — на английском, как в MCP:

Create a database in a PostgreSQL cluster. Mutation: requires confirmed=true after the user approves.

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo pg db-create <cluster-id> <name> [flags]
```

### Options

```
  -h, --help                Справка по db-create
      --lc-collate string   Collation locale (optional, default C).
      --lc-ctype string     Character-set locale (optional, default C).
      --owner string        Owning user (from pg_users). Required.
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

Вызывает инструмент pg_db_delete (подробнее: 'evo tools describe pg_db_delete').
Описание инструмента и его флагов — на английском, как в MCP:

Drop a database in a PostgreSQL cluster. Mutation: requires confirmed=true after the user approves.

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

Вызывает инструмент pg_delete (подробнее: 'evo tools describe pg_delete').
Описание инструмента и его флагов — на английском, как в MCP:

Delete a Managed PostgreSQL cluster by id (from pg_list) AND its data, including all its backups (they cannot be restored). Mutation (async): requires confirmed=true after the user approves.

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo pg delete <cluster-id> [flags]
```

### Options

```
  -h, --help        Справка по delete
      --id string   Cluster UUID (from pg_list). Required (alias: cluster_id).
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

Вызывает инструмент pg_get (подробнее: 'evo tools describe pg_get').
Описание инструмента и его флагов — на английском, как в MCP:

Get full details of one Managed PostgreSQL cluster by id (from pg_list): version, instances, storage, connection_string, subnets, status. Read-only.
```

```
evo pg get <cluster-id> [flags]
```

### Options

```
  -h, --help        Справка по get
      --id string   PostgreSQL cluster UUID (from pg_list). Required (alias: cluster_id).
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

Вызывает инструмент pg_list (подробнее: 'evo tools describe pg_list').
Описание инструмента и его флагов — на английском, как в MCP:

List Managed PostgreSQL clusters in the project (id, name, status, version, health). Read-only. Managed PostgreSQL is Cloud.ru's managed database service (postgresql.api.cloud.ru).
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

Вызывает инструмент pg_operation (подробнее: 'evo tools describe pg_operation').
Описание инструмента и его флагов — на английском, как в MCP:

Poll a Managed PostgreSQL async operation by id (returned by pg_create/pg_delete/pg_backup_create). Returns {done, error}. Read-only.
```

```
evo pg operation <operation-id> [flags]
```

### Options

```
  -h, --help               Справка по operation
      --wait-seconds int   Optional: poll up to N seconds (capped 300) with backoff and return the FINAL state in ONE call — instead of the agent re-checking every few seconds. Omit for a single instant check.
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

Вызывает инструмент pg_specs (подробнее: 'evo tools describe pg_specs').
Описание инструмента и его флагов — на английском, как в MCP:

List Managed PostgreSQL specifications (id, cpu, memory_gb, deployment_mode standard|business, min_storage_gb) for a version. Read-only. Use to choose specification_id for pg_create.
```

```
evo pg specs [flags]
```

### Options

```
  -h, --help             Справка по specs
      --version string   Product version (e.g. '17'); optional — filters specs to that version.
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

Вызывает инструмент pg_user_create (подробнее: 'evo tools describe pg_user_create').
Описание инструмента и его флагов — на английском, как в MCP:

Create a user in a PostgreSQL cluster. Mutation: requires confirmed=true after the user approves. The password is never echoed back.

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo pg user-create <cluster-id> <name> [flags]
```

### Options

```
  -h, --help              Справка по user-create
      --password string   User password. Required.
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

Вызывает инструмент pg_user_delete (подробнее: 'evo tools describe pg_user_delete').
Описание инструмента и его флагов — на английском, как в MCP:

Delete a user in a PostgreSQL cluster. Mutation: requires confirmed=true after the user approves.

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

Вызывает инструмент pg_users (подробнее: 'evo tools describe pg_users').
Описание инструмента и его флагов — на английском, как в MCP:

List users of a PostgreSQL cluster (name, granted roles). Read-only.
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

Вызывает инструмент pg_versions (подробнее: 'evo tools describe pg_versions').
Описание инструмента и его флагов — на английском, как в MCP:

List available Managed PostgreSQL versions (e.g. 15/16/17). Read-only. Use before pg_create.
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

Вызывает инструмент s3_bucket_create (подробнее: 'evo tools describe s3_bucket_create').
Описание инструмента и его флагов — на английском, как в MCP:

Create an Object Storage (S3) bucket. Use this when a bucket doesn't exist yet (s3_put needs an existing bucket). Idempotent — a bucket you already own is not an error. Mutation: requires confirmed=true after the user approves.

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo s3 bucket-create <bucket> [flags]
```

### Options

```
  -h, --help               Справка по bucket-create
      --s3-region string   Region: only ru-central-1 is accepted (the API answers 'unexpected region' otherwise). Optional; default: the configured S3 region.
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

Вызывает инструмент s3_bucket_delete (подробнее: 'evo tools describe s3_bucket_delete').
Описание инструмента и его флагов — на английском, как в MCP:

Delete an EMPTY S3 bucket. Destructive mutation: requires confirmed=true after the user approves.

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

Вызывает инструмент s3_buckets (подробнее: 'evo tools describe s3_buckets').
Описание инструмента и его флагов — на английском, как в MCP:

List Object Storage (S3) buckets. Read-only. s3.cloud.ru (S3-compatible).
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

Вызывает инструмент s3_get (подробнее: 'evo tools describe s3_get').
Описание инструмента и его флагов — на английском, как в MCP:

Download an S3 object up to 10 MiB (a larger one is refused with USAGE: use an S3 client or the console). Returns text inline (utf-8) when small & valid, else base64. Read-only.
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

Вызывает инструмент s3_ls (подробнее: 'evo tools describe s3_ls').
Описание инструмента и его флагов — на английском, как в MCP:

List objects in an S3 bucket (key, size, last_modified). Read-only.
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
      --max-keys int    Max objects to return (default 1000).
      --prefix string   Key prefix filter (optional).
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

Вызывает инструмент s3_put (подробнее: 'evo tools describe s3_put').
Описание инструмента и его флагов — на английском, как в MCP:

Upload an S3 object from inline content (content) or base64 (content_base64). Mutation (overwrites if key exists): requires confirmed=true after the user approves.

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo s3 put <bucket> <key> [flags]
```

### Options

```
      --content string          Inline text content to upload.
      --content-base64 string   Base64-encoded binary content (alternative to content).
      --content-type string     MIME type (default application/octet-stream).
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

Вызывает инструмент s3_rm (подробнее: 'evo tools describe s3_rm').
Описание инструмента и его флагов — на английском, как в MCP:

Delete an S3 object. Destructive mutation: requires confirmed=true after the user approves.

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

Вызывает инструмент secret_add_version (подробнее: 'evo tools describe secret_add_version').
Описание инструмента и его флагов — на английском, как в MCP:

Add a new value version to an existing secret (rotation). Mutation: requires confirmed=true after the user approves.

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo secret add-version <path> [flags]
```

### Options

```
  -h, --help           Справка по add-version
      --value string   New secret value (plaintext). Required.
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

Вызывает инструмент secret_create (подробнее: 'evo tools describe secret_create').
Описание инструмента и его флагов — на английском, как в MCP:

Store a new secret at a path (value is Base64-encoded internally; never echoed back). Path 'folder/name' places it in a folder. Mutation: requires confirmed=true after the user approves.

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo secret create <path> [flags]
```

### Options

```
      --description string   Optional description.
  -h, --help                 Справка по create
      --kms-key-id string    Optional KMS key id for encryption.
      --value string         Secret value (plaintext; stored encrypted). Required.
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

Вызывает инструмент secret_delete (подробнее: 'evo tools describe secret_delete').
Описание инструмента и его флагов — на английском, как в MCP:

Delete a secret by path AND all its versions. force defaults to true (a secret with active versions is deleted too); force=false lets the API refuse such a secret. Mutation: requires confirmed=true after the user approves.

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo secret delete <path> [flags]
```

### Options

```
      --force   Delete the secret even if it has active versions. To turn off: --force=false (по умолчанию true)
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

Вызывает инструмент secret_get (подробнее: 'evo tools describe secret_get').
Описание инструмента и его флагов — на английском, как в MCP:

Get one secret's metadata by path (from secret_list): folder, labels, versions, KMS key. Read-only — does NOT return the value.
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

Вызывает инструмент secret_list (подробнее: 'evo tools describe secret_list').
Описание инструмента и его флагов — на английском, как в MCP:

List secrets in the project (id, name, path, description) from Cloud.ru Secret Management. Read-only — does NOT return values. secretmanager.api.cloud.ru (v2).
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

Вызывает инструмент secret_value (подробнее: 'evo tools describe secret_value').
Описание инструмента и его флагов — на английском, как в MCP:

Retrieve the DECRYPTED value of a secret by path. Sensitive — returns the plaintext value; never store it in logs or files. Read-only.
```

```
evo secret value <path> [flags]
```

### Options

```
  -h, --help          Справка по value
      --version int   Specific version number (default: latest).
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
завершается с кодом 3. Описания и ответы инструментов — на английском, как в MCP.
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

Вызывает инструмент vpc_create (подробнее: 'evo tools describe vpc_create').
Описание инструмента и его флагов — на английском, как в MCP:

Create a new VPC (virtual private cloud) (async). Mutation: requires confirmed=true after the user approves.

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo vpc create <name> [flags]
```

### Options

```
      --description string   Optional description.
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

Вызывает инструмент vpc_delete (подробнее: 'evo tools describe vpc_delete').
Описание инструмента и его флагов — на английском, как в MCP:

Delete a VPC by id (from vpc_list) (async). Its subnets/resources must be removed first. Mutation: requires confirmed=true after the user approves.

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

Вызывает инструмент fip_attach (подробнее: 'evo tools describe fip_attach').
Описание инструмента и его флагов — на английском, как в MCP:

Bind an existing FREE floating IP to a VM's primary interface, giving it a public address. Use when a VM has no public_ip — e.g. provisioning from a large custom image outran the wizard's wait and it returned before assigning one. Pick a free address from list_floating_ips. Mutation: requires confirmed=true.

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo vpc fip-attach <fip> [flags]
```

### Options

```
  -h, --help        Справка по fip-attach
      --vm string   VM name or UUID to attach it to.
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

Вызывает инструмент release_floating_ip (подробнее: 'evo tools describe release_floating_ip').
Описание инструмента и его флагов — на английском, как в MCP:

Release (delete) a floating IP, freeing it from the project. Identify it by 'id' or 'ip_address'. REFUSES to release an IP that is currently in use by a VM (detach or destroy the VM first) or that belongs to a SNAT gateway (it goes with the gateway: snat_delete). Mutation: requires confirmed=true after the user approves.

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo vpc fip-release [id] [flags]
```

### Options

```
  -h, --help                Справка по fip-release
      --ip-address string   Floating IP address, e.g. '203.0.113.11' (alternative to id).
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

Вызывает инструмент list_floating_ips (подробнее: 'evo tools describe list_floating_ips').
Описание инструмента и его флагов — на английском, как в MCP:

List Cloud.ru floating IPs (address, state, whether available to attach). Read-only.
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

Вызывает инструмент vpc_get (подробнее: 'evo tools describe vpc_get').
Описание инструмента и его флагов — на английском, как в MCP:

Get full details of one VPC by id (from vpc_list). Read-only.
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

Вызывает инструмент vpc_list (подробнее: 'evo tools describe vpc_list').
Описание инструмента и его флагов — на английском, как в MCP:

List Evolution VPCs (virtual private clouds) in the project (id, name, default, type). Read-only. vpc.api.cloud.ru.
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

## evo vpc operation

Состояние асинхронной операции VPC

### Synopsis

```text
Состояние асинхронной операции VPC.

Вызывает инструмент vpc_operation (подробнее: 'evo tools describe vpc_operation').
Описание инструмента и его флагов — на английском, как в MCP:

Poll a VPC async operation by id (from vpc_create/delete, vpc_route_create/delete). Returns {done, error}. Read-only.
```

```
evo vpc operation <operation-id> [flags]
```

### Options

```
  -h, --help               Справка по operation
      --wait-seconds int   Optional: poll up to N seconds (capped 300) with backoff and return the FINAL state in ONE call — instead of re-checking every few seconds. Omit for a single instant check.
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

Вызывает инструмент vpc_route_create (подробнее: 'evo tools describe vpc_route_create').
Описание инструмента и его флагов — на английском, как в MCP:

Add a static route to a VPC (async). Destination is a CIDR (subnet); the next hop is a VM interface (next_hop_vm_id+next_hop_interface_id), a VIP (next_hop_vip_id), or a Magic Router (next_hop_magic_router_id+next_hop_az). Mutation: requires confirmed=true after the user approves.

Спрашивает подтверждение: вопрос на терминале, --yes — согласие заранее, --dry-run — только превью.
```

```
evo vpc route-create <vpc-id> [flags]
```

### Options

```
      --description string                Optional route description.
  -h, --help                              Справка по route-create
      --next-hop-az string                Availability zone name (e.g. 'ru.AZ-1') for a Magic-Router next hop.
      --next-hop-interface-id string      VM network interface UUID.
      --next-hop-magic-router-id string   Magic Router UUID for a Magic-Router next hop (with next_hop_az).
      --next-hop-vip-id string            Virtual IP UUID for a VIP next hop.
      --next-hop-vm-id string             VM UUID for a VM-interface next hop (with next_hop_interface_id).
      --subnet string                     Destination CIDR, e.g. '192.168.0.0/24'. Required.
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

Вызывает инструмент vpc_route_delete (подробнее: 'evo tools describe vpc_route_delete').
Описание инструмента и его флагов — на английском, как в MCP:

Delete a static route from a VPC by id (from vpc_routes) (async). Mutation: requires confirmed=true after the user approves.

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

Вызывает инструмент vpc_routes (подробнее: 'evo tools describe vpc_routes').
Описание инструмента и его флагов — на английском, как в MCP:

List static routes of a VPC (id, subnet, next_hop_type, status). Read-only.
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

Вызывает инструмент list_security_groups (подробнее: 'evo tools describe list_security_groups').
Описание инструмента и его флагов — на английском, как в MCP:

List Cloud.ru security groups (UUID, name, availability zone). Read-only. With rules=true each group carries its rules (id, direction, protocol, port range, source, description, state).
```

```
evo vpc security-groups [flags]
```

### Options

```
  -h, --help    Справка по security-groups
      --rules   Also list each group's rules (one more API call per group).
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

Вызывает инструмент list_subnets (подробнее: 'evo tools describe list_subnets').
Описание инструмента и его флагов — на английском, как в MCP:

List Cloud.ru subnets (UUID, name, availability zone). Read-only.
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
