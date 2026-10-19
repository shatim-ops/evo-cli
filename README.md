# Cloud.ru Evolution CLI CREW edition

> Команда — `evo`. Эта редакция — для тестирования внутри Cloud.ru и сбора
> обратной связи: в Telegram [@shatim](https://t.me/shatim) и в
> [чат тестировщиков](https://t.me/+SYvXziA4G9c1Y2Ni).

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

## Что сейчас можно (v1.0.4)

Версия 1.0.4 от 19.10.2026 идёт в первую волну догфуда внутри Cloud.ru.
Перед выпуском на живом облаке прогнано 63 из 65 инструментов, которые
создают, меняют или удаляют ресурсы; найденные ошибки исправлены до выпуска
([отчёт](docs/internal/live-runs-2026-10.md)). Все инструменты чтения
вживую проверяются перед каждым выпуском.

| Область | Команды | Состояние |
|---|---|---|
| compose-проект на ВМ | `evo deploy`, `status`, `logs`, `verify`, `destroy` | работает; только Ubuntu и Debian, на ВМ уезжает один `docker-compose.yml` (без `build:` и `.env`) |
| ВМ и диски | `evo vm`, `evo disk` | работает: создание, удаление, подключение, расширение и отключение дисков |
| образы и маркетплейс | `evo image`, `evo marketplace` | поиск и ВМ из образа работают; Astra Linux, Альт и РЕД ОС тестовому проекту недоступны, evo отказывает до создания. Свой образ из диска (`image_create`) вживую не проверен — квота организации |
| сеть | `evo vpc`, `tools call lb_* / tg_* / snat_*` | работает: VPC, подсети, группы безопасности и порты, floating IP, маршруты, SNAT, балансировщик NLB |
| DNS | `tools call dns_*` | работает (вживую проверены приватные зоны); операции с зоной платформа выполняет от секунд до нескольких часов |
| Kubernetes | `evo k8s` | работает: кластер, пулы узлов, плагины, kubeconfig, `kubectl`, `helm` |
| PostgreSQL, Kafka, Redis | `evo pg`, `tools call kafka_* / redis_*` | работает: кластеры, базы и пользователи PostgreSQL, бэкапы |
| Object Storage | `evo s3` | работает; нужны отдельные ключи S3 ([auth.md](docs/auth.md)) |
| секреты и сертификаты | `evo secret`, `tools call cert_*` | работает; выпуск Let's Encrypt (`cert_issue_le`) нужен делегированной публичной зоне — вживую не проверен |
| Container Apps, Artifact Registry | `tools call ca_* / ar_*` | работает: реестр, `docker push`, приложение, логи, остановка и запуск |
| стеки | `tools call stack_*` | работает: план, применение, статус, удаление |
| мониторинг, биллинг, аудит, IAM | `tools call metrics_* / billing_* / audit_events …` | только чтение; `billing_*` может не отвечать — тогда консоль, «Контроль затрат» |
| AI-агент | `evo mcp` (stdio и HTTP) | работает: те же 152 инструмента, создание и удаление — только после согласия |

**Чего ждать от платформы.** `evo deploy` иногда падает не по вине evo:
новая ВМ долго не открывает SSH или apt-зеркало образа не отвечает (в
живых прогонах 18.10 — 5 из 10 развёртываний). evo называет причину,
удаляет созданное, и запуск можно повторить. Kubernetes и базы данных стоят
от 100 ₽ в сутки и могут зависнуть на стороне платформы: после проб
проверяйте `evo k8s list`, `evo pg list` и удаляйте лишнее. Остальные
известные особенности — [docs/troubleshooting.md](docs/troubleshooting.md).

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
`CLOUDRU_VM_VERSION=v1.0.4` (см. [install.md](docs/install.md)).

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
те же описания, что видит агент. Справка команд и флагов всех деревьев (`vm`,
`disk`, `image`, `vpc`, `k8s`, `pg`, `s3`, `secret` и другие) — по-русски.

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
Так же спрашивают подтверждение команды, которые ничего не меняют, но
показывают секрет открытым текстом: `evo secret value` и `evo tools call
cert_private_key` (MCP `secret_value`, `cert_private_key`).

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
