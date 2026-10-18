# Cloud.ru Evolution CLI CREW edition

> Команда — `evo`. Эта редакция — для тестирования внутри Cloud.ru и сбора
> обратной связи: в Telegram [@shatim](https://t.me/shatim) и в
> [чат тестировщиков](https://t.me/+SYvXziA4G9c1Y2Ni). Раньше — `cloudru-vm`:
> см. [«Переименование»](#переименование).

Командная строка и MCP-сервер для [Cloud.ru Evolution](https://cloud.ru):
виртуальные машины, диски, сети, Managed Kubernetes, PostgreSQL, Object
Storage, Secret Manager — и развёртывание `docker-compose`-проекта на ВМ одной
командой.

- **CLI для человека и скриптов**: `evo vm list`, `evo deploy`,
  `evo k8s kubeconfig …`; у каждой команды есть `--json` и
  [стабильные коды выхода](docs/exit-codes.md).
- **MCP-сервер для AI-агентов** (Claude Code, Cursor и др.): `evo mcp`
  по stdio или Streamable HTTP, в том числе в Docker (образ собирается из
  `Dockerfile`, см. [docs/mcp.md](docs/mcp.md#docker-образ)). Те же
  инструменты доступны из CLI: `evo tools list|describe|call`.
- **Ничего платного без согласия**: создание ресурсов и удаление данных
  показывают превью (с ценой, где она есть) и ждут подтверждения —
  одинаково у человека и у агента ([модель подтверждений](#подтверждения)).

## Что понадобится

- **Ключ доступа сервисного аккаунта** (Key ID и Key Secret) и **UUID проекта**
  Cloud.ru Evolution. Как создать сервисный аккаунт, какие роли дать и где
  взять UUID — [docs/auth.md](docs/auth.md).
- Для `evo deploy` — исходящий доступ с вашей машины на порт 22 создаваемой ВМ
  (SSH).
- Для `evo k8s kubectl/apply/helm` — установленные `kubectl` и `helm`
  ([docs/install.md](docs/install.md#kubectl-и-helm)). Остальное работает без
  зависимостей: бинарь статический.

## Установка

Linux и macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/shatim-ops/evo-cli/main/install.sh | sh
evo version
```

Скрипт без версии ставит последний стабильный релиз; конкретная версия —
`CLOUDRU_VM_VERSION=v1.0.0` (см. [install.md](docs/install.md)).

Скрипт проверяет sha256 архива и, если установлен `cosign`, подпись релиза,
и кладёт бинарь в `~/.local/bin` (под root — в `/usr/local/bin`). Обновление —
тот же запуск.

Windows: скачайте `evo_<версия>_windows_amd64.zip` (или `arm64`) из
[Releases](https://github.com/shatim-ops/evo-cli/releases), распакуйте `evo.exe`
в каталог из `PATH` — пошагово с проверкой sha256 в
[docs/install.md](docs/install.md#windows). Там же ручная установка, проверка
подписи, Gatekeeper (macOS) и SmartScreen (Windows).

## Быстрый старт

Впервые? Пошаговое руководство — [docs/guide.md](docs/guide.md).

Нужен ключ доступа сервисного аккаунта (Key ID и Key Secret) и UUID проекта —
как их получить, в [docs/auth.md](docs/auth.md).

Строки, которые начинаются с `#`, — пояснения к командам. В zsh (оболочка по
умолчанию на macOS) скопированный комментарий без настройки даёт ошибку
«command not found: #»: включите комментарии один раз —
`echo 'setopt interactivecomments' >> ~/.zshrc && source ~/.zshrc` — или
копируйте только сами команды.

```sh
# ключ и проект → профиль "default"
evo configure
# ключ принят IAM, какой проект выбран
evo whoami
# конфиг, права файлов, доступность API
evo doctor
# ВМ проекта
evo vm list

# каталог с docker-compose.yml
cd my-app
# .evo.yaml: зона и образ из API
evo init
# что будет создано и сколько стоит; ничего не создаёт
evo deploy --dry-run
# ВМ + публичный IP + docker compose up -d
evo deploy
# порты приложения отвечают
evo verify
# логи контейнеров
evo logs -f
# удалить ВМ и освободить IP
evo destroy
```

Зону лучше выбрать явно (`evo list-zones` показывает, какие открыты для новых
ресурсов): подробности и другие известные ограничения — в
[docs/troubleshooting.md](docs/troubleshooting.md).

## Подключить к AI-агенту

После `evo configure` агент получает те же возможности через MCP. Claude Code:

```sh
claude mcp add --transport stdio --scope project evo -- evo mcp
```

Cursor — `~/.cursor/mcp.json` (или `.cursor/mcp.json` в проекте):

```json
{ "mcpServers": { "evo": { "command": "evo", "args": ["mcp"] } } }
```

Агент не создаёт и не удаляет ничего платного без вашего согласия: изменяющий
инструмент сначала возвращает превью ([подтверждения](#подтверждения)).
HTTP-режим, Docker-образ, отбор групп инструментов (`--toolsets`) и ограничение
проектов (`--allow-project`) — [docs/mcp.md](docs/mcp.md); все 152
инструмента — [docs/tools.md](docs/tools.md).

## Что ещё умеет

| Команда | Что делает |
|---|---|
| `vm`, `disk`, `vpc` | ВМ, диски, сети, подсети, группы безопасности, floating IP |
| `image` | образы ВМ: ОС и продукты маркетплейса — поиск по имени, карточка с UUID |
| `marketplace` | витрина: ОС, продукты маркетплейса и плагины Kubernetes одним списком, с командой развёртывания каждого (только чтение) |
| `k8s` | кластеры Managed Kubernetes, node pool'ы, плагины (каталог, версии, установка), kubeconfig, `kubectl`/`helm` |
| `pg` | кластеры PostgreSQL, базы, пользователи, бэкапы |
| `s3` | бакеты и объекты Object Storage |
| `secret` | Secret Manager |
| `tools list` / `describe` / `call` | любой из 152 инструментов MCP-сервера, включая DNS, балансировщики, Kafka, Redis, Artifact Registry, сертификаты, мониторинг, биллинг |
| `deploy`, `status`, `logs`, `verify`, `destroy` | сценарий «compose → ВМ» |
| `configure`, `whoami`, `doctor` | профили, проверка ключа и окружения |
| `mcp` | MCP-сервер для агентов |

Полная справка по всем командам и флагам — [docs/commands.md](docs/commands.md)
(генерируется из бинаря).

## Язык

CLI говорит по-русски: справка, ход выполнения, превью, ошибки и подсказки.
По-английски — с переменной `EVO_LANG=en`:

```sh
EVO_LANG=en evo deploy --help
```

Команды, флаги, коды ошибок (`AUTH_FAILED`), коды выхода и ключи JSON — одни и
те же в обоих языках; в JSON-ошибке символьный код — в поле `reason`.
MCP-сервер (`evo mcp`) всегда отвечает по-английски: описания инструментов и
ответы читает агент. Описания инструментов в `tools describe` — английские, это
те же описания, что видит агент. В деревьях `vm` и `disk` описания команд и
флаги — по-русски; в остальных деревьях (`image`, `vpc`, `k8s`, `pg`, `s3`,
`secret` и другие) они пока английские, как в MCP.

## Подтверждения

Команды, которые создают платные ресурсы или удаляют данные (`deploy`,
`destroy`, `vm create`, `disk delete`, `tools call` изменяющих инструментов…),
сначала показывают, что произойдёт. В терминале спрашивают подтверждение
(`y` или `д` — да, Enter — нет); `--yes`
(`-y`) — согласие заранее; `--dry-run` — только превью, код выхода `0`. Без
терминала и без `--yes` команда ничего не делает и выходит с кодом `3`, а с
`--json` печатает в stdout `{"status":"confirmation_required","preview":{…}}`.
MCP-сервер держит ту же границу: изменяющий инструмент без `confirmed=true`
возвращает `status: confirmation_required` и превью, действие выполняется
только повторным вызовом с `confirmed=true` — после согласия человека.

```sh
evo disk delete 1b2c… --dry-run   # что будет удалено
evo disk delete 1b2c… --yes       # удалить без вопроса (скрипт, CI)
evo disk delete 1b2c… </dev/null; echo $?   # 3: без терминала и без --yes ничего не удалено
```

Цена ВМ в превью (`vm create`, `deploy`, MCP `deploy_plan`) — из API расчёта
цены Cloud.ru; если он не ответил, — **оценка** по встроенной таблице тарифов.
Лицензия образа маркетплейса не входит ни в то, ни в другое
([guide.md → Цена в превью](docs/guide.md#цена-в-превью)).

## Документация

| | |
|---|---|
| [docs/guide.md](docs/guide.md) | **начните отсюда**: установка, ключ, первые команды, deploy, агент, ограничения |
| [docs/install.md](docs/install.md) | установка на Linux, macOS, Windows; проверка подписи; kubectl/helm |
| [docs/auth.md](docs/auth.md) | сервисный аккаунт и ключ, роли, профили, все переменные окружения, что хранится на диске, ротация ключа |
| [docs/mcp.md](docs/mcp.md) | MCP по stdio и HTTP, конфиги Claude Code и Cursor, Docker-образ |
| [docs/commands.md](docs/commands.md) | справка по всем командам |
| [docs/tools.md](docs/tools.md) | каталог всех инструментов MCP и `evo tools`: группа, меняет ли, обязательные аргументы |
| [docs/exit-codes.md](docs/exit-codes.md) | коды выхода и формат ошибок `--json` |
| [docs/troubleshooting.md](docs/troubleshooting.md) | типовые ошибки и что приложить к обращению |
| [docs/RELEASING.md](docs/RELEASING.md) | для сопровождающих: выпуск релиза |

**Переход со старых версий:** файл `~/.cloudru-vm.yaml` больше не читается —
ключ и проект живут в профиле (`evo configure`), а настройки проекта —
в `./.evo.yaml`. `evo doctor` предупреждает, если старый файл
остался. `CLOUDRU_API_KEY=keyId:secret` пока работает, но устарел: используйте
`CLOUDRU_KEY_ID` и `CLOUDRU_SECRET`.

## Переименование

До версии 1.0 инструмент назывался `cloudru-vm`. Что изменилось и что
продолжает работать до v1.1:

| Было | Стало | До v1.1 |
|---|---|---|
| бинарь `cloudru-vm` | `evo` | в архиве релиза и в `install.sh` есть alias `cloudru-vm` — тот же бинарь; печатает в stderr «cloudru-vm переименован в evo, alias будет удалён в v1.1» и работает как `evo` |
| сборка из исходников `go build ./cmd/cloudru-vm` | `go build ./cmd/evo` | `./cmd/cloudru-vm` собирает тот же код (`internal/cli`): скрипты вида `go build -o bin/cloudru-vm ./cmd/cloudru-vm` продолжают работать, бинарь ведёт себя как alias |
| профили в `<каталог конфигурации>/cloudru-vm/` | `<каталог конфигурации>/evo/` | при первом запуске `evo` копирует `config.yaml` и `credentials.yaml` (права `0600`, каталог `0700`, на Windows — свой ACL); старый каталог не трогается, кэш токена не копируется — токен выписывается заново |
| `./.cloudru-vm.yaml` | `./.evo.yaml` | старый файл читается с предупреждением, если нового нет |
| `./.cloudru-vm/` (state, SSH-ключ, планы), `~/.cloudru-vm/` (задания `stack_*`, цены) | `./.evo/`, `~/.evo/` | старый каталог используется на месте, с предупреждением; `destroy` и `status` видят ВМ из его `state.json` |
| MCP-сервер `cloudru-vm` (`serverInfo.name`) | `evo` | имена инструментов не менялись; в конфиге клиента поменяйте команду на `evo mcp` |
| Docker-образ `<реестр>/cloudru-vm` | `<реестр>/evo` | |
| ВМ `cloudru-vm-<проект>`, правила групп безопасности «cloudru-vm: port N» | `evo-<проект>`, «evo: port N» для новых ресурсов | уже созданные ресурсы не переименовываются; `destroy` удаляет их по ID из state |

Переменные окружения `CLOUDRU_*` не переименованы: это имя провайдера, а не
инструмента. Каталог проекта переименуйте сами, когда в нём не работает
старая версия (например, MCP-сервер со старым бинарём):

```sh
evo status                  # ВМ на месте, предупреждение о ./.cloudru-vm/
mv .cloudru-vm .evo && mv .cloudru-vm.yaml .evo.yaml
```

## Поддержка

Ошибки CLI, идеи, неудобства и вопросы — в [Issues](https://github.com/shatim-ops/evo-cli/issues/new/choose)
этого репозитория, по форме («Ошибка CLI», «Идея, неудобство или вопрос»,
для участников догфуда — «Чек-ин»): каждое issue разбирается, на каждое
приходит ответ. Нужен аккаунт GitHub, другого канала отзывов нет. Порядок
догфуда — [docs/internal/dogfood-protocol.md](docs/internal/dogfood-protocol.md). Репозиторий публичный: в выводе замените ID проектов и ресурсов, IP,
Key ID и имена ресурсов на `<uuid>`, `<ip>`, `<имя>`, ключи не вставляйте
никогда. Приложите `evo version` и `evo doctor` — подробнее в
[docs/troubleshooting.md](docs/troubleshooting.md#обращение-в-поддержку).
Быстрые вопросы — в Telegram [@shatim](https://t.me/shatim) и в
[чат тестировщиков](https://t.me/+SYvXziA4G9c1Y2Ni).

## Сборка из исходников

```sh
go build -o bin/evo ./cmd/evo
gofmt -l . && go vet ./... && go test -race ./... && govulncheck ./...
```

Нужен Go из `go.mod`. `GORACE=atexit_sleep_ms=0` ускоряет `go test -race`
в несколько раз: без неё race-рантайм ждёт секунду на выходе каждого пакета.
CI гоняет то же на Linux, macOS и Windows
(`.github/workflows/ci.yml`). После изменения команд, флагов или инструментов
обновите справку: `go test ./internal/cli -run 'TestCommandsDoc|TestToolsDoc' -update-docs`
(`docs/commands.md` и `docs/tools.md` генерируются, CI проверяет, что они
актуальны).
Внутренние планы и история проекта — `docs/internal/`.

## Лицензия

Apache-2.0 (см. `LICENSE` и `NOTICE`). Правообладатель — ООО «Облачные
технологии» (Cloud.ru).
