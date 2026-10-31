# evo CMCP — CLI и MCP-сервер для Cloud.ru Evolution

**evo CMCP** — командная строка и MCP-сервер для [Cloud.ru Evolution](https://cloud.ru).
Один инструмент управляет облаком и для человека, и для AI-агента:
виртуальные машины, диски, сети и балансировщики, DNS, Managed Kubernetes,
PostgreSQL, Kafka, Redis, Object Storage, секреты и сертификаты, Artifact
Registry и Container Apps — и развёртывает `docker-compose`-проект на ВМ одной
командой. Команда — `evo`.

- **CLI для человека и скриптов**: `evo vm list`, `evo deploy`,
  `evo k8s kubeconfig …`; у каждой команды есть `--json` и
  [стабильные коды выхода](docs/exit-codes.md).
- **MCP-сервер для AI-агентов** (Claude Code, Cursor и др.): `evo mcp` по stdio
  или Streamable HTTP, в том числе в Docker. У каждого инструмента MCP есть
  команда CLI с тем же ответом: `--json` команды — то, что видит агент.
- **Ничего платного без согласия**: создание ресурсов и удаление данных
  показывают превью с ценой и ждут подтверждения — одинаково у человека и у
  агента.

**CREW edition** — редакция для команд Cloud.ru. Вопросы и ошибки — в
[Issues](https://github.com/shatim-ops/evo-cli/issues/new/choose); уязвимость —
не в Issues, а приватным отчётом: [SECURITY.md](SECURITY.md).

## Что сейчас можно (v1.6.0, 31.10.2026)

**Ресурсы облака.** ВМ, диски, образы и маркетплейс; VPC, подсети, группы
безопасности и порты, floating IP, балансировщики и группы целей, SNAT-шлюзы,
публичные и приватные зоны DNS; Managed Kubernetes с группами узлов, плагинами
и `kubectl`/`helm`; PostgreSQL (базы, пользователи, бэкапы), Kafka, Redis;
Object Storage; Secret Manager и Certificate Manager (в том числе
Let's Encrypt); Artifact Registry и Container Apps; биллинг, аудит, метрики,
сервисные аккаунты и квоты — только чтение.

**Сценарии.** `evo deploy` — compose-проект на ВМ: ВМ, публичный адрес, порты,
Docker и `docker compose up`, проверка и логи, удаление одной командой.
`evo stack` — стек ресурсов по одному манифесту: план с ценой, применение,
ход, удаление.

**Согласие и превью.**
- Превью каждого создания называет проект, зону, подсеть, группу
  безопасности, выбранный flavor и всё, что evo выбирает сам, и цену — из API
  расчёта Cloud.ru или оценкой по тарифу с пометкой.
- Вызов, который платформа заведомо отвергнет, отказывает до вопроса о
  согласии и называет причину: flavor, которого нет в зоне, версия или подсеть
  не из каталога, второй SNAT-шлюз в зоне, удаление подключённого диска или
  ресурса, который ещё создаётся, приватная зона DNS без VPC и DNS-сервера.
- Без подсказки evo подбирает flavor, который есть в зоне создания.

**Повтор и уборка.**
- Повторное создание с занятым именем отказывает и называет существующий
  ресурс.
- Повторное удаление того, чего уже нет, — `already_deleted`.
- «Удалено» значит, что ресурса нет: 404 без ответа сервиса не выдаётся за
  удаление.
- Долгие операции называют состояние ресурса, а `*_operation` — ход операции.

**CLI.**
- Ссылки выводятся как «имя (id)».
- Подсеть, группу безопасности и зону можно указать по имени.
- Tab дополняет имена и id ресурсов.
- `evo --help` группирует команды по продуктам Cloud.ru Evolution.
- Журнал сессии `evo diag` помогает разобрать проблему без значений
  аргументов.

**MCP.** Отбор групп инструментов (`--toolsets`), режим только на чтение
(`--read-only`), ограничение проектов (`--allow-project`), HTTP с
маскированием секретов; `evo agent setup` подключает сервер к клиенту одной
командой и ставит правила для агента.

Подробности версии и ломающие изменения — раздел 1.6.0 в
[CHANGELOG.md](CHANGELOG.md).

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

Скрипт ставит последний стабильный релиз, проверяет sha256 архива и, если
установлен `cosign`, подпись релиза, и кладёт бинарь в `~/.local/bin` (под
root — в `/usr/local/bin`). Конкретная версия —
`CLOUDRU_VM_VERSION=v1.6.0` перед `sh` (см. [install.md](docs/install.md)).

Windows: скачайте `evo_<версия>_windows_amd64.zip` (или `arm64`) из
[Releases](https://github.com/shatim-ops/evo-cli/releases), распакуйте `evo.exe`
в каталог из `PATH` — пошагово с проверкой sha256 в
[docs/install.md](docs/install.md#windows). Там же ручная установка, проверка
подписи, Gatekeeper (macOS) и SmartScreen (Windows).

## Обновление

**CLI.** Linux и macOS — тот же запуск `install.sh`: он заменит бинарь на
последний стабильный релиз. Windows — замените `evo.exe` новым из
[Releases](https://github.com/shatim-ops/evo-cli/releases). Проверка:

```sh
evo version
```

Профиль (`evo configure`) и ключ при обновлении сохраняются. Перед обновлением
на следующую минорную версию прочитайте раздел «Миграция» её
[CHANGELOG](CHANGELOG.md).

**Локальный агент (Claude Code, Cursor и др.).** Клиент запускает `evo mcp` из
`PATH`, поэтому новый бинарь он подхватит при следующем запуске сервера:
начните новую сессию агента (в Claude Code — или переподключите сервер в
`/mcp`). Правила для агента обновите той же командой, что их поставила, — она
кладёт правила текущей версии evo:

```sh
evo agent setup --client claude-code --skill
```

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
# ключ, проект и зона для новых ВМ → профиль "default"
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

Зону лучше выбрать явно (`evo vm zones` показывает, какие открыты для новых
ресурсов): подробности и другие известные ограничения — в
[docs/troubleshooting.md](docs/troubleshooting.md).

## Подключить к AI-агенту

После `evo configure` агент получает те же возможности через MCP. Проще всего —
одной командой: она дополняет файл настроек клиента (другие серверы остаются),
перед записью показывает diff и спрашивает согласие:

```sh
evo agent setup --client claude-code          # cursor, vscode, gemini, codex; gigaagent — печать
evo agent setup --client claude-code --skill  # и правила для агента (docs/agent-skill.md)
```

Вручную — Claude Code:

```sh
claude mcp add --transport stdio --scope project evo -- evo mcp
```

Cursor — `~/.cursor/mcp.json` (или `.cursor/mcp.json` в проекте):

```json
{ "mcpServers": { "evo": { "command": "evo", "args": ["mcp"] } } }
```

Агент не создаёт и не удаляет ничего платного без вашего согласия: изменяющий
инструмент сначала возвращает превью с ценой и ждёт подтверждения человека.
HTTP-режим, Docker-образ, отбор групп инструментов (`--toolsets`), ограничение
проектов (`--allow-project`) и режим только на чтение для агента без
присмотра (`--read-only`) — [docs/mcp.md](docs/mcp.md); все 157
инструмента — [docs/tools.md](docs/tools.md).

## Команды

| Команда | Что делает |
|---|---|
| `vm`, `disk`, `vpc` | ВМ, диски, сети, подсети, группы безопасности и порты (`vpc open-ports`), floating IP |
| `image` | образы ВМ: ОС и продукты маркетплейса — поиск по имени, карточка с UUID; свой образ из диска (`image create`) |
| `marketplace` | витрина: ОС, продукты маркетплейса и плагины Kubernetes одним списком, с командой развёртывания каждого (только чтение) |
| `k8s` | кластеры Managed Kubernetes, node pool'ы, плагины (каталог, версии, установка), kubeconfig, `kubectl`/`helm` |
| `pg`, `kafka`, `redis` | кластеры PostgreSQL (базы, пользователи, бэкапы), Kafka и Redis |
| `s3` | бакеты и объекты Object Storage |
| `secret`, `cert` | Secret Manager; TLS-сертификаты Certificate Manager, в том числе Let's Encrypt |
| `dns`, `lb`, `tg`, `snat` | зоны и записи DNS, балансировщики нагрузки и их группы целей, SNAT-шлюзы |
| `ar`, `ca` | реестры Artifact Registry; приложения Container Apps |
| `stack` | стек ресурсов по одному манифесту: план с ценой, применение, ход, удаление |
| `billing`, `audit`, `metrics`, `iam`, `org` | баланс, потребление и FinOps; журнал аудита; метрики мониторинга; сервисные аккаунты; квоты (только чтение) |
| `tools list` / `describe` / `call` | любой из 157 инструментов MCP-сервера по имени, с аргументами `--arg` |
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
ответы читает агент. Справка команд и флагов всех деревьев (`vm`, `disk`,
`image`, `vpc`, `k8s`, `pg`, `s3`, `secret` и другие) и описания всех
инструментов в `evo tools list` и `evo tools describe` — по-русски; с
`EVO_LANG=en` — те же английские описания, что видит агент.

## Документация

| | |
|---|---|
| [docs/guide.md](docs/guide.md) | **начните отсюда**: установка, ключ, первые команды, deploy, агент, ограничения |
| [docs/install.md](docs/install.md) | установка на Linux, macOS, Windows; проверка подписи; kubectl/helm |
| [docs/auth.md](docs/auth.md) | сервисный аккаунт и ключ, роли, профили, все переменные окружения, что хранится на диске, ротация ключа |
| [docs/mcp.md](docs/mcp.md) | MCP по stdio и HTTP, конфиги Claude Code и Cursor, Docker-образ |
| [docs/commands.md](docs/commands.md) | справка по всем командам; у команд без инструмента MCP — пометка «Локальная команда» с причиной |
| [docs/tools.md](docs/tools.md) | каталог всех инструментов MCP и `evo tools`: группа, команда CLI, меняет ли, обязательные аргументы, описание по-английски и по-русски |
| [docs/exit-codes.md](docs/exit-codes.md) | коды выхода и формат ошибок `--json` |
| [docs/troubleshooting.md](docs/troubleshooting.md) | типовые ошибки и что приложить к обращению |
| [docs/journal.md](docs/journal.md) | журнал сессии: `evo diag on/status/export`, что записывается и чего нет |
| [docs/RELEASING.md](docs/RELEASING.md) | для сопровождающих: выпуск релиза |

## Поддержка

Ошибки, идеи, неудобства и вопросы — в
[Issues](https://github.com/shatim-ops/evo-cli/issues/new/choose) этого
репозитория, по форме. Приложите `evo version` и `evo doctor` — подробнее в
[docs/troubleshooting.md](docs/troubleshooting.md#обращение-в-поддержку).
Репозиторий публичный: в выводе замените ID проектов и ресурсов, IP, Key ID и
имена ресурсов на `<uuid>`, `<ip>`, `<имя>`; ключи не вставляйте никогда.

## Сборка из исходников

```sh
go build -o bin/evo ./cmd/evo
gofmt -l . && go vet ./... && go test -race ./... && govulncheck ./...
```

Нужен Go из `go.mod`. `GORACE=atexit_sleep_ms=0` ускоряет `go test -race`
в несколько раз: без неё race-рантайм ждёт секунду на выходе каждого пакета.
CI гоняет то же на Linux, macOS и Windows
(`.github/workflows/ci.yml`). После изменения команд, флагов или инструментов
обновите справку: `go test ./internal/cli -run 'TestCommandsDoc|TestToolsDoc|TestAgentSkillDoc|TestParityInventory' -update-docs`
(`docs/commands.md`, `docs/tools.md`, правила для агента `docs/agent-skill.md` и инвентаризация паритета CLI ↔ MCP
`docs/internal/cli-mcp-parity/inventory.md` генерируются, CI проверяет, что
они актуальны).
Внутренние планы и история проекта — `docs/internal/`.

## Лицензия

Apache-2.0 (см. `LICENSE` и `NOTICE`). Правообладатель — ООО «Облачные
технологии» (Cloud.ru).
