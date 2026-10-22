# Аудит: cloudru-vm как самостоятельный инструмент

Дата: 2026-10-14. Основание: чтение кода репозитория (ничего не менялось),
сборка и тесты на linux/amd64 (`go build` ок, `go test` ок), сверка с
документацией Cloud.ru (ссылки в конце). Факты, гипотезы и мнения разделены.

## 1. Что есть сейчас (факты)

| Область | Состояние |
|---|---|
| Сборка/тесты | Собирается, тесты зелёные. `internal/engine` идёт 91 с, `internal/provision` 10 с — реальные `time.Sleep` и реальная сеть (`203.0.113.42`) в тестах |
| CLI | 9 команд одного сценария «compose → ВМ»: `deploy status logs destroy verify init list-zones list-images version` + `mcp` |
| MCP | 146 инструментов / 23 группы, транспорт **только stdio**, protocolVersion `2024-11-05` |
| Покрытие CLI ↔ MCP | ~6 из 146 инструментов имеют CLI-аналог; 21 группа без CLI вообще |
| Креды | Фактически только env: `CLOUDRU_KEY_ID`+`CLOUDRU_SECRET` или `CLOUDRU_API_KEY=keyId:secret`. Поля `key_id`/`secret` в YAML **не доходят до провайдера**; `KeyringProvider` — заглушка без реализации; `login`/`configure` нет |
| Токен | IAM `POST iam.api.cloud.ru/api/v1/auth/token`, кэш только в памяти; MCP создаёт Engine на каждый вызов → обмен с IAM на каждый инструмент |
| Релиз | Ничего: нет goreleaser, тегов, ldflags, checksums, подписи, матрицы ОС. `version` = константа `0.1.0` (README говорит про 0.3.0+) |
| Лицензия | `LICENSE`: «Внутренний код… не публикуется». **Блокер распространения** |
| Кросс-платформа | Внешние бинарники нужны только `k8s_kubectl/apply/helm` (`kubectl`, `helm`), поиск путей Unix-only; в дочерние процессы уходит весь `os.Environ()` включая `CLOUDRU_SECRET`. SSH — чистый Go, это плюс. Права 0600 на Windows не работают; ANSI без VT-режима |
| Состояние на диске | Разбросано: `./.cloudru-vm/` (ключ, state, планы) и `~/.cloudru-vm/` (stack-jobs, pricing). SSH-ключ свой на каждый каталог запуска — даже для `list-zones` |

## 2. Дефекты, которые увидит первый же внешний пользователь

1. `deploy` с конфигом из `init` не работает: образ ubuntu-22.04 без Docker, пакет `internal/bootstrap` (cloud-init с установкой Docker) нигде не подключён.
2. `destroy` не освобождает floating IP (MCP-версия освобождает). Деньги продолжают капать.
3. `destroy` без TTY → EOF → «cancelled» → **код 0**. Скрипт решит, что удалено.
4. Неизвестная команда/флаг → код 1 **без сообщения** (`SilenceErrors` + `os.Exit(1)`).
5. `logs --follow` не стримит: `CombinedOutput` с таймаутом 120 с, затем ошибка.
6. `--verbose` ничего не делает (пакет `internal/log` не импортируется).
7. `--json` непоследователен: `deploy --json` при успехе печатает пусто, ошибки идут текстом.
8. `deploy` создаёт платную ВМ без подтверждения и без оценки стоимости (оценка есть только в MCP `deploy_plan`).
9. Хардкод: `DiskTypeID`, UUID зон/образов в `init` и справке, ssh-user `user1`.
10. Нет таймаутов у IAM и compute клиентов (`http.DefaultClient`, `context.Background()`).

## 3. Сверка с документацией Cloud.ru

Подтверждено:
- Эндпоинт токена и формат (`keyId`/`secret` → `access_token`, Bearer, TTL 1 ч) — совпадает с кодом. Тот же вход работает для персонального ключа доступа пользователя.
- Хосты `*.api.cloud.ru` для compute, vpc, dns, nlb, postgresql, redis, kafka, mk8s, ar, containers, secretmanager, certificatemanager, monitoring, audit, iam, organization — совпадают.
- Marketplace Cloud.ru: есть категории **«AI-агенты» и «MCP-серверы»**; MCP-сервер публикуется как Docker-образ из Artifact Registry. Типа «скачиваемый бинарь/CLI» **нет** — ближайшее «Приложение»/«Другое», нужно уточнять у команды маркетплейса. Оферта (ред. 01.06.2024): комиссия 20 %, партнёр даёт L2/L3, OSS-лицензии должны разрешать распространение.

Расхождения и риски:
- **Терминология.** У Cloud.ru «API-ключ» — отдельная сущность (`Authorization: Api-Key …`, статический, не все сервисы). У нас `CLOUDRU_API_KEY` = `keyId:secret`. Переименовать или задокументировать.
- `id.cloud.ru/auth/system/openid/token` (используется для kubeconfig mk8s) в документации не найден; quickstart называет `auth.iam.cloud.ru/auth/system/openid/token`. Проверить вживую до релиза.
- Внутри документации роль для API-ключей называется то `iam.project.admin`, то `platform.project.admin`.

Не найдено в документации: минимальные сервисные роли (`compute.*`, `vpc.*`), rate limits API, хост billing API, OAuth device flow / браузерный логин для CLI.

**Конкурентный ландшафт (факт, важно для позиционирования):**
- `cloud-ru/evo-terraform` — официальный Terraform-провайдер, покрывает почти всё IaaS/PaaS.
- `cloud-ru/evolution-devservices-cli` (`eds`) — официальный CLI, но только Repo и Workflow Studio.
- `cloud-ru/evo-aifactory-skills` содержит Python-скилл **с именем `cloudru-vm`** для управления ВМ из агентов. Прямое пересечение по имени и по нише.
- `cloud-ru/mcp-servers` — официальный MCP только для Artifact Registry.

Вывод по нише: единого императивного CLI для IaaS у Cloud.ru нет, место свободно. Но имя `cloudru-vm` уже занято официальным скиллом, и «vm» в названии противоречит 23 группам инструментов.

## 4. Мнение: где главный риск

Не в коде. Код чинится по списку. Три решения выше кода:

1. **Продукт или компонент.** Сейчас это движок внутренней системы. Внешний продукт требует поддержки L2/L3 по оферте, обратной совместимости CLI и обязательств по безопасности хранения кредов на машине пользователя. Это ресурсное решение, а не техническое.
2. **Форма поставки в Marketplace.** Поддерживаемый путь — MCP-сервер в Docker. Для него нужен транспорт Streamable HTTP (сейчас stdio) и OAuth-/ключевая авторизация на HTTP. Бинарь CLI — через GitHub Releases, в карточке маркетплейса как ссылка. Ставка только на «скачиваемый бинарь» упирается в отсутствие такого типа продукта.
3. **Имя и лицензия.** `cloudru-vm` конфликтует с официальным скиллом; «cloudru» — бренд Cloud.ru (внутри компании это решаемо, но требует согласования). Лицензия должна быть заменена до первого публичного тега.

## 5. Решения, которые нужны от владельца до старта работ

| # | Вопрос | Рекомендация |
|---|---|---|
| D1 | Лицензия | Apache-2.0 (как у официальных `eds`, `evo-ai-agents-cli`) |
| D2 | Имя бинаря/модуля | Сменить; варианты: `evo-infra`, `cru`. Оставить `cloudru-vm` только как alias на переходный период |
| D3 | Форма в Marketplace | MCP-сервер (Docker, Streamable HTTP) как основная карточка + бинарь CLI в GitHub Releases |
| D4 | Хранение кредов | Файл в `os.UserConfigDir()/<name>/credentials` 0600, профили; OS keyring — позже (P1), не блокер |
| D5 | Поддержка Windows в первом релизе | Да, бинарь; но без гарантий по `kubectl/helm`-инструментам |
| D6 | Язык интерфейса | Один: английский в CLI/MCP (как сейчас), русский README + русская документация отдельно |
| D7 | Scope первого публичного релиза | «Все 23 группы через универсальный `call`» + первоклассные команды для vm/disk/vpc/k8s/pg/s3; остальное — v1.x |
| D8 | Подпись бинарей | cosign (keyless) обязательно; нотаризация macOS / Authenticode — если есть сертификаты компании |

## 6. Приоритеты (сводка; детали в 01-plan.md)

**P0 (без этого выпускать нельзя):** лицензия и имя · релизный пайплайн · `configure`/профили/чтение кредов из файла · покрытие 23 групп в CLI · подтверждения и коды возврата в CLI · починка `deploy` по умолчанию и `destroy` FIP · внешний README.

**P1:** таймауты и `signal.NotifyContext` · единый `--json` · кэш токена между вызовами · `logs --follow` · Windows-пути/VT · быстрые тесты (инъекция часов, без реальной сети) · Streamable HTTP для MCP · убрать хардкод ID.

**P2:** `--verbose` + санитайзер · тесты 16 пакетов без тестов · man/completion · `.env`/`build:` в deploy · переопределение базовых URL через env.

## Источники

- Auth API: https://cloud.ru/docs/console_api/ug/topics/guides__auth_api
- Quickstart (альтернативный токен-эндпоинт): https://cloud.ru/docs/console_api/ug/topics/quickstart
- Статические API-ключи: https://cloud.ru/docs/console_api/ug/topics/guides__static-api-keys
- Ключи сервисных аккаунтов: https://cloud.ru/docs/console_api/ug/topics/guides__service_accounts_key-create
- Роли: https://cloud.ru/docs/administration/ug/topics/concepts__roles
- Реестр API: https://cloud.ru/docs/console_api/ug/topics/overview__reestr_api
- VM API/OpenAPI: https://cloud.ru/docs/virtual-machines/ug/topics/api-ref
- Marketplace, публикация: https://cloud.ru/marketplace/publishment
- MCP-сервер в AI Agents: https://cloud.ru/docs/ai-agents/ug/topics/guides__create-mcp-server
- Оферта Marketplace: https://cdn.cloud.ru/backend/docs/contracts/oferta_marketplace_010624.pdf
- Официальные репозитории: https://github.com/cloud-ru (evo-terraform, evolution-devservices-cli, evo-aifactory-skills, mcp-servers)
