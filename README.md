# Cloud.ru Evolution CLI CREW edition

> Команда — `evo`. Эта редакция — для тестирования внутри Cloud.ru и сбора
> обратной связи: в Telegram [@shatim](https://t.me/shatim) и в
> [чат тестировщиков](https://t.me/+SYvXziA4G9c1Y2Ni).

Уязвимость — не в Issues, а приватным отчётом: [SECURITY.md](SECURITY.md).

Командная строка и MCP-сервер для [Cloud.ru Evolution](https://cloud.ru):
виртуальные машины, диски, сети и балансировщики, DNS, Managed Kubernetes,
PostgreSQL, Kafka, Redis, Object Storage, секреты и сертификаты, Container
Apps — и развёртывание `docker-compose`-проекта на ВМ одной командой.

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

## Что сейчас можно (v1.2.2)

Версия 1.2.2 от 22.10.2026 — для догфуда внутри Cloud.ru.
Перед выпуском 1.0 на живом облаке прогнано 63 из 65 инструментов, которые
создают, меняют или удаляют ресурсы; найденные ошибки исправлены до выпуска
([отчёт](docs/internal/live-runs-2026-10.md)). Перед 1.2.0 `deploy`, мастер
`deploy_plan` → `deploy_apply`, MCP по stdio и HTTP, секреты и сертификаты
прогнаны вживую в ru.AZ-2, все инструменты чтения — на Linux, Windows и macOS.

Главное в 1.2: у каждого инструмента MCP есть команда CLI, и `--json`
команды — тот же ответ, что у инструмента (паритет CLI ↔ MCP); `evo mcp
--read-only` — граница прав для агента, чьи вызовы никто не подтверждает
([рецепт](docs/mcp.md)); `whoami` и `doctor` — инструменты MCP; секреты
(`--value-file`, `--password-file`, `--private-key-file`) — из файла или
stdin, а не из командной строки; Go 1.26.10.

1.2.1 — исправления по первым отзывам догфуда и атаке на 1.2:
`evo configure` спрашивает зону, `--region` понимает имя (`ru.AZ-1`);
`evo vm create` показывает ход создания, ждёт ВМ 25 минут и выдаёт команду
`ssh` с ключом evo и пароль один раз; превью `evo deploy apply` называет ВМ
и цену; превью без цены говорит, что ресурс платный, и даёт ссылку на тариф;
`evo mcp --allow-project` проверяет, чей ресурс по id; маскирование
секретов в ответах MCP по HTTP закрывает пары `Name`/`Value`, вложенный
JSON и ключи `tls.key` и `id_rsa`.

1.2.2 — исправления по догфуду 1.2.1 и внешним проверкам: `evo billing
finops`, `consumption` и `balance` печатают отчёт для человека — целые рубли,
итоги с НДС и без, русские подписи (`--json` прежний, в ответе добавились
суммы с НДС); ошибка `--json` несёт поля ответа инструмента в `details` —
например, ВМ и пароль после Ctrl-C в `evo vm create`; `evo deploy --region`
и `deploy_plan` понимают имя зоны; примечания и подсказки `evo k8s` называют
команды evo; неверное имя бакета отклоняется до вопроса о согласии.

Ломающие правки — только там, где прежнее поведение было пробелом в
согласии. В 1.2.2: MCP-сервер с `--allow-project` не отдаёт баланс
договора (`billing_balance` — `CONFIG_INVALID`); `evo s3 put` и `rm` с
именем бакета, которое S3 не примет, — `USAGE`, код 1, до согласия.
В 1.2.1: `evo vm create` (и MCP `provision`) без зоны, образа или
проекта отказывает до вопроса о согласии — без зоны `CONFIG_INVALID`, код 2.
В 1.2.0: повторный `evo deploy`, который обновляет ВМ, спрашивает
подтверждение (в скриптах — `evo deploy --yes`); MCP `provision` и `deploy`
отклоняют недоступный проекту образ уже на превью. Подробно — разделы
«Миграция» в [CHANGELOG.md](CHANGELOG.md).

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
`CLOUDRU_VM_VERSION=v1.2.2` (см. [install.md](docs/install.md)).

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
инструмент сначала возвращает превью ([подтверждения](#подтверждения)).
HTTP-режим, Docker-образ, отбор групп инструментов (`--toolsets`), ограничение
проектов (`--allow-project`) и режим только на чтение для агента без
присмотра (`--read-only`) — [docs/mcp.md](docs/mcp.md); все 154
инструмента — [docs/tools.md](docs/tools.md).

## Что ещё умеет

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
| `tools list` / `describe` / `call` | любой из 154 инструментов MCP-сервера по имени, с аргументами `--arg` |
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

## Подтверждения

Команды, которые создают платные ресурсы или удаляют данные (`deploy`,
`destroy`, `vm create`, `disk delete`, `dns zone-delete`, `tools call`
изменяющих инструментов…),
сначала показывают, что произойдёт. Повторный `deploy`, который обновляет
ВМ развёртывания, — тоже. В терминале спрашивают подтверждение
(`y` или `д` — да, Enter — нет); `--yes`
(`-y`) — согласие заранее; `--dry-run` — только превью, код выхода `0`. Без
терминала и без `--yes` команда ничего не делает и выходит с кодом `3`, а с
`--json` печатает в stdout `{"status":"confirmation_required","preview":{…}}`.
MCP-сервер держит ту же границу: изменяющий инструмент без `confirmed=true`
возвращает `status: confirmation_required` и превью, действие выполняется
только повторным вызовом с `confirmed=true` — после согласия человека.
Так же спрашивают подтверждение команды, которые ничего не меняют, но
показывают секрет открытым текстом: `evo secret value` и `evo cert
private-key` (MCP `secret_value`, `cert_private_key`).

```sh
evo disk delete 1b2c… --dry-run   # что будет удалено
evo disk delete 1b2c… --yes       # удалить без вопроса (скрипт, CI)
evo disk delete 1b2c… </dev/null; echo $?   # 3: без терминала и без --yes ничего не удалено
```

Цена ВМ в превью (`vm create`, `deploy`, MCP `deploy_plan`, `provision`,
`deploy`) — из API расчёта цены Cloud.ru; если он не ответил, — **оценка** по
встроенной таблице тарифов.
Лицензия образа маркетплейса не входит ни в то, ни в другое
([guide.md → Цена в превью](docs/guide.md#цена-в-превью)).

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
| [docs/RELEASING.md](docs/RELEASING.md) | для сопровождающих: выпуск релиза |

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
обновите справку: `go test ./internal/cli -run 'TestCommandsDoc|TestToolsDoc|TestAgentSkillDoc|TestParityInventory' -update-docs`
(`docs/commands.md`, `docs/tools.md`, правила для агента `docs/agent-skill.md` и инвентаризация паритета CLI ↔ MCP
`docs/internal/cli-mcp-parity/inventory.md` генерируются, CI проверяет, что
они актуальны).
Внутренние планы и история проекта — `docs/internal/`.

## Лицензия

Apache-2.0 (см. `LICENSE` и `NOTICE`). Правообладатель — ООО «Облачные
технологии» (Cloud.ru).
