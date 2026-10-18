# AI-ассистент в CLI: практика лидеров рынка и варианты для Evo CLI

Дата: 18.10.2026. Статус: аналитика. Решений по реализации нет, код не
менялся. Основание: открытые источники (первичные — документация и
репозитории вендоров, вторичные — пресса) и код этого репозитория (`main`,
v1.0.1). Пометки: **[П]** — подтверждено первичным источником, **[В]** —
только вторичный источник, **[Г]** — вывод или гипотеза автора.

Содержание:

0. [Главные выводы](#0-главные-выводы)
1. [Карта рынка](#1-карта-рынка)
2. [Как это устроено: три архитектурных паттерна](#2-как-это-устроено-три-архитектурных-паттерна)
3. [Что уже есть у Cloud.ru и Сбера](#3-что-уже-есть-у-cloudru-и-сбера)
4. [Где сейчас Evo CLI](#4-где-сейчас-evo-cli)
5. [Варианты для Evo CLI](#5-варианты-для-evo-cli)
6. [Рекомендация](#6-рекомендация)
7. [Риски и контраргументы](#7-риски-и-контраргументы)
8. [Открытые вопросы и следующие шаги](#8-открытые-вопросы-и-следующие-шаги)
9. [Источники](#9-источники)

## 0. Главные выводы

1. **Посылка «у Azure есть ИИ-ассистент в CLI» требует поправки.**
   `az copilot` — экспериментальное расширение сообщества (вырос из
   Microsoft Global Hackathon 2023, в README: «не для production»), а не
   продукт Microsoft. Собственный терминальный ассистент Microsoft — AI
   Shell (`aish`, sidecar к PowerShell, агент Azure Copilot) —
   заархивирован в январе 2026. Azure Copilot живёт только в портале и
   мобильном приложении. В терминале у Azure в 2026 году остались Azure MCP
   Server и плагин Azure Skills для GitHub Copilot CLI и Claude Code. [П]
2. **Ни один гиперскейлер не встроил LLM в основной бинарь CLI** (`az`,
   `aws`, `gcloud`, `oci`, `aliyun`, `yc`). Вместо этого два хода:
   (а) отдельный агентный терминальный продукт — AWS Kiro CLI (бывший
   Amazon Q Developer CLI), Google Gemini CLI (с июня 2026 — Antigravity
   CLI), GitHub Copilot CLI; (б) MCP-сервер, который оборачивает облачный
   API или сам CLI, чтобы любой агент (Claude Code, Codex, Cursor, Copilot
   CLI) мог управлять облаком. [П]
3. **Рынок ушёл от «перевода фразы в команду» к агентному циклу с
   инструментами и движком политик** (allow / ask / deny). Перевод остался
   вспомогательной функцией (`kiro-cli translate`), а `gh copilot
   suggest|explain` закрыт в октябре 2025 — его заменил полноценный агент. [П]
4. **Тренд 2026: «CLI — инструмент агента, а не его мозг».** Kiro V3
   удаляет собственный инструмент `aws` в пользу MCP-сервера; IBM Cloud CLI
   сам запускается как MCP-сервер (`ibmcloud --mcp-transport stdio`); AWS,
   Alibaba, Yandex Cloud, Cloudflare, Vercel переходят на хостируемые MCP с
   OAuth. [П]/[В]
5. **Evo CLI уже реализовал «нижнюю половину» этой архитектуры**: единый
   реестр из 152 инструментов с JSON-схемами (65 изменяющих), аннотации
   read-only / destructive, подтверждение на стороне сервера с превью и
   ценой, MCP по stdio и HTTP. Нет «верхней половины»: вызова модели,
   диалога, перевода запроса в команду, объяснения ошибок.
6. **У Cloud.ru есть все компоненты верхней половины, но они не соединены
   с CLI**: Гига-помощник (LangGraph, GigaChat MAX, RAG по документации,
   27 тыс. пользователей на апрель 2026), Foundation Models с
   OpenAI-совместимым API и вызовом функций у GigaChat 3 / 3.5 и
   Qwen3-Coder (проверено запросом 18.10.2026), AI Agents для хостинга MCP,
   Guardrails Filter для маскирования секретов.
7. **Рекомендация (аналитическая позиция, не план работ):** идти путём
   лидеров — сначала сделать evo лучшим инструментом для чужих агентов
   (MCP elicitation, skills, хостируемый MCP с OAuth, проверка с GigaCode
   CLI), затем добавить тонкий встроенный слой (`evo ask` с валидацией
   сгенерированной команды и объяснение ошибок) на Foundation Models с
   подключаемым OpenAI-совместимым бэкендом, и только при подтверждённом
   спросе — агентный режим в бинаре. Стратегически — один «мозг» с
   Гига-помощником через API ассистента, а не третий ассистент рядом с
   консолью и GigaCode.

## 1. Карта рынка

### 1.1. Облачные провайдеры

| Провайдер | LLM в основном CLI | Терминальный агент | MCP | Модель подтверждений | Открытость |
|---|---|---|---|---|---|
| **Microsoft Azure** | нет; `az find` / `az next` — ML-ранжирование по базе Aladdin, не генерация [П] | AI Shell (C#, sidecar к PowerShell, агенты Azure OpenAI и Azure Copilot, токен из `az login`) — **архив с 01.2026** [П]; `az copilot` — расширение сообщества на Semantic Kernel с пользовательским Azure OpenAI [П] | Azure MCP Server (microsoft/mcp, .NET, MIT, 200+ инструментов, 45+ сервисов, телеметрия вкл. по умолчанию) [П]; Azure Skills Plugin (03.2026) для Copilot CLI, VS Code, Claude Code [П]; `azd`: ИИ-разбор ошибок, `azd coding-agent config` с ролью Reader по умолчанию [П] | RBAC как граница; подтверждение на стороне клиента; `--read-only` [В] | MCP и skills — OSS; Copilot — закрыт |
| **AWS** | нет | Fig → CodeWhisperer CLI → Amazon Q Developer CLI → **Kiro CLI** (v1.20, 17.11.2025); `kiro-cli chat`, `translate`, `inline`, headless, ACP; модели Claude/Qwen на стороне AWS [П] | awslabs/mcp (~40 серверов, Python, только stdio); AWS API MCP: `call_aws` (валидированные команды AWS CLI), `suggest_aws_commands`, `READ_OPERATIONS_ONLY`, `REQUIRE_MUTATION_CONSENT` через MCP elicitation; **Agent Toolkit for AWS** (05.2026): managed MCP, IAM condition keys «агент vs человек» [П] | `permissions.yaml`: capabilities × allow/ask/deny, deny > ask > allow, разбиение составных shell-команд, доверие рабочей области, headless: ask = deny [П] | предшественник — Rust, MIT/Apache; Kiro **закрыт** (AWS IP License) [П] |
| **Google Cloud** | нет; Gemini Cloud Assist — чат в консоли [П] | **Gemini CLI** (TS, Apache-2.0, 107k ★): инструменты fs/shell/web, MCP, расширения, headless, GitHub Action; с 18.06.2026 переводится на **Antigravity CLI** (по данным прессы — закрытый) [П]/[В] | `@google-cloud/gcloud-mcp` (preview): один инструмент `run_gcloud_command` + denylist интерактивных и опасных команд, креды активного аккаунта gcloud; Cloud Run / GKE / Observability MCP как расширения Gemini CLI [П] | policy engine (TOML, уровни Default…Admin, `allow|deny|ask_user`), режимы default/autoEdit/plan/yolo, trusted folders, телеметрия выкл. по умолчанию [П] | Gemini CLI — OSS; Antigravity — [В] закрыт |
| **Oracle OCI** | нет; `oci -i` — детерминированное автодополнение [П] | нет; Console AI (07.2026) — пилот в консоли, каждое изменение требует одобрения [П] | oracle/mcp (UPL): `oci-api-mcp-server` оборачивает CLI (`run_oci_command`, `get_oci_command_help`), **без read-only режима и подтверждений** — только IAM [П] | IAM | OSS, «не для production» |
| **Alibaba Cloud** | нет генерации, но **`aliyun utils mcp-proxy`** — OAuth-прокси в самом CLI к хостируемому OpenAPI MCP Server [П] | Qwen Code (TS, Apache-2.0, 28k ★, форк Gemini CLI v0.8.2; BYO-модели, Ollama/vLLM) [П] | OpenAPI MCP Server (Core: все API через семантический поиск; Custom: до ~30 API), CloudControl MCP (300+ типов ресурсов, без встроенного подтверждения) [П] | allow / block списки серверов в прокси; предупреждение: порт прокси даёт права пользователя CLI любому локальному процессу [П] | CLI — Go, Apache-2.0 |
| **IBM Cloud** | **да, как MCP**: `ibmcloud --mcp-transport stdio --mcp-tools <namespaces>` — бинарь CLI сам MCP-сервер, инструменты зеркалят команды [В] | нет | встроен | Safe Mode (только чтение) по умолчанию, `--mcp-allow-write` [В] | закрыт |
| **Yandex Cloud** | нет | нет; AI-ассистент консоли (Preview): план → подтверждение → применение под ролями пользователя, запросы не используются для обучения [П] | 10 официальных MCP (`toolkit`, `docs`, `functions`…): `npx @yandex-cloud/mcp` или Streamable HTTP `https://<server>.mcp.cloud.yandex.net/mcp` с IAM-токеном; в документации — раздел «CLI как альтернатива для агентов»: `yc --format json` [П] | план показывают, применяют после явного подтверждения | закрыт |
| **Cloud.ru** | нет | нет; Гига-помощник в консоли (см. раздел 3) | хостинг MCP в Evolution AI Agents; `evo mcp` (этот репозиторий) | «спрашивает разрешения на важные действия» [В]; evo — `confirmed=true` на стороне сервера | evo — Apache-2.0 |

Прочие: DigitalOcean, Cloudflare, Vercel, Netlify — без LLM в CLI, но у всех
хостируемые MCP с OAuth (`mcp.cloudflare.com`, `mcp.vercel.com`,
`apps.mcp.digitalocean.com`); Vercel пускает только проверенные клиенты и
рекомендует «включить подтверждение человеком». [П]

### 1.2. Референсные агенты (не облачные, но задают стандарт UX)

| Продукт | Паттерн | Модель | Политика | Открытость |
|---|---|---|---|---|
| **GitHub Copilot CLI** (GA 25.02.2026) | полный агент, GitHub MCP встроен, plan mode, `-p` headless | Claude Sonnet 4.5 по умолчанию, GPT-5, **BYO-провайдер** (OpenAI-совместимый, Azure OpenAI, Anthropic, Ollama) | `--allow-tool='shell(git:*)' --deny-tool='shell(git push)'`, deny всегда побеждает, сохранённые разрешения на репозиторий, корпоративный запрет `--allow-all` | закрыт (репозиторий — только README) [П] |
| **Claude Code** | полный агент, hooks, субагенты, OS-sandbox с allowlist сети | Anthropic / Bedrock / Vertex / Foundry / свой шлюз | режимы default / acceptEdits / plan / dontAsk / bypass / auto; правила `Bash(npm run *)`, `mcp__server__tool`; deny > ask > allow; разбиение составных команд; «правила исполняет Claude Code, а не модель» | закрыт [П] |
| **OpenAI Codex CLI** | полный агент, **sandbox как главная граница** (Seatbelt / bubblewrap), approvals вторичны | OpenAI | `sandbox_mode` read-only / workspace-write / full; `approval_policy`; MCP с OAuth | Rust, Apache-2.0, 129k ★ [П] |
| **Warp (Oz)** | терминал с агентом, профили Default / YOLO / Prod («всегда спрашивать») | Anthropic / OpenAI / Google | allow/deny списки команд, пауза на подтверждение | частично [В] |

## 2. Как это устроено: три архитектурных паттерна

### 2.1. Паттерн A — перевод естественного языка в команду с подтверждением

```
пользователь → запрос + справка CLI (+ RAG по документации) → модель
            → команда → валидация CLI → показать → y / Enter
            → обычный путь выполнения CLI со своими гейтами
```

Примеры: `az copilot --prompt` (Semantic Kernel + Azure OpenAI, по желанию
RAG через Azure Cognitive Search), `gh copilot suggest|explain` (закрыт
25.10.2025), `kiro-cli translate` (до 5 вариантов, только печатает),
AI Shell (вставка в строку и в Predictive IntelliSense), Гига-помощник в
режиме co-pilot (генерирует команды для serial console, пользователь
вставляет сам), Azure Copilot в портале (генерирует скрипты az / PowerShell /
KQL / Bicep). Модель **никогда не исполняет**; исполняет человек.

Сильные стороны: дёшево, безопасно, не требует состояния ресурсов,
встраивается в существующий CLI. Слабые: одноходово; качество зависит от
полноты справки; галлюцинации флагов — отсюда у AWS API MCP формулировка
«validated AWS CLI commands, no arbitrary code execution»; `gh copilot
suggest` закрыли, потому что агент покрывает это как частный случай.

Отдельный подвид — **«объясни / исправь ошибку»**: AI Shell восстанавливал
последнюю команду по ошибке; `azd` с марта 2026 на упавшей команде
предлагает «объяснить / дать агенту исправить / повторить». [П]

### 2.2. Паттерн B — агентный цикл с инструментами и движком политик

```
          ┌──────────── модель (у вендора или BYO) ────────────┐
запрос →  │ рассуждение → вызов инструмента → результат → …     │ → ответ
          └──────────────────────┬───────────────────────────────┘
                     движок политик до каждого вызова:
                     read-only → allow; write / shell → ask; deny всегда побеждает
                     инструменты: файлы, shell, web, cloud (свой или MCP)
```

Так устроены Kiro, Gemini CLI, Copilot CLI, Claude Code, Codex, Qwen Code.
Доступ к облаку — один из инструментов (`aws` в Kiro 2.x,
`run_gcloud_command`, `run_oci_command`, `call_aws`), и **вендоры от своих
облачных инструментов избавляются в пользу MCP** (Kiro V3 удаляет `aws`;
AWS API MCP «superseded» managed AWS MCP). [П]

Что сошлось у всех по модели подтверждений: чтения авто-одобряются;
запись и shell — спросить; deny > ask > allow; «всегда разрешать»
сохраняется на уровне репозитория; составные команды (`a && b; c | d`)
разбиваются и проверяются по частям — после инцидентов класса Tracebit;
доверие рабочей области (untrusted → не грузить конфиги проекта, `.env`,
MCP, hooks); флаги «yolo» с корпоративным выключателем; headless-режим:
любой ask = deny. Codex и Claude Code добавляют OS-sandbox как основную
границу, потому что строковые allow-list для shell обходятся. [П]

**Идентичность двухуровневая.** Действия в облаке идут под кредами CLI
(`az login`, boto3 chain, аккаунт gcloud, профиль `yc`) — RBAC/IAM и есть
реальная граница. LLM оплачивается отдельно: Kiro — кредиты ($20–200/мес),
Copilot — seat, Gemini — Code Assist. AWS в Agent Toolkit добавил
IAM condition keys, чтобы в CloudTrail отличать действия агента от действий
человека. [П]

**Контекст.** Справка и метаданные API (Alibaba OpenAPI meta,
`get_oci_command_help`, Kiro `introspect`), файлы памяти проекта
(`AGENTS.md`, `GEMINI.md`, `CLAUDE.md`, Kiro steering), состояние ресурсов
(Azure Copilot — control plane, Yandex — контекст страницы). Инструменты не
грузятся все сразу: Kiro `tool_search` подгружает MCP-инструменты по
запросу, Alibaba Core MCP ищет API семантически. [П]

**Инциденты, которые сформировали эти правила** [В]: вредоносный PR в
расширение Amazon Q для VS Code 1.84.0 (07.2025, промпт «очистить систему
и удалить облачные ресурсы»); Gemini CLI — обход allowlist через цепочку
команд с эксфильтрацией переменных окружения (Tracebit, исправлено в
0.1.14), CVE-2026-12537 (`--yolo` игнорировал allowlist); «Comment and
Control» (04.2026) — инъекции через заголовки PR и комментарии в CI против
Claude Code, Gemini CLI, Copilot. Общая причина — доверие к строкам в
репозитории и к тексту, пришедшему из инструментов.

### 2.3. Паттерн C — CLI как инструмент агента (MCP)

Два направления. **CLI потребляет MCP**: Kiro, Gemini CLI, Copilot CLI,
Claude Code, Codex, AI Shell. **CLI или облако предоставляет MCP**:
IBM Cloud CLI (бинарь сам сервер), gcloud-mcp / oci-api-mcp / AWS API MCP
(оборачивают CLI), Azure MCP Server (оборачивает SDK), хостируемые MCP с
OAuth (AWS managed MCP, Alibaba OpenAPI MCP, Yandex `*.mcp.cloud.yandex.net`,
Cloudflare, Vercel, DigitalOcean). Серверные предохранители:
`READ_OPERATIONS_ONLY`, `REQUIRE_MUTATION_CONSENT` через elicitation
(AWS API MCP), Safe Mode (IBM), denylist (gcloud-mcp), allow/block серверов
(Alibaba). Норма 2026 — хостируемый MCP с OAuth вместо локальных
`npx`-серверов; `evo mcp` по stdio/HTTP и его Docker-образ для Marketplace
лежат ровно в этом русле. [П]

### 2.4. Сводка по измерениям и что из этого следует для evo

| Измерение | Как у лидеров | Следствие для evo [Г] |
|---|---|---|
| Где работает модель | у вендора облака (Kiro, Gemini) или BYO (Copilot CLI, Qwen Code, Claude Code через Bedrock/Vertex) | Foundation Models как «вендорский» бэкенд по умолчанию плюс любой OpenAI-совместимый эндпоинт для закрытых контуров (on-prem vLLM, GigaChat, GigaCode on-prem) |
| Идентичность | креды CLI для действий, отдельная подписка для LLM | ключ сервисного аккаунта — для действий, API-ключ Foundation Models — для модели: два ключа, два биллинга, разные роли |
| Контекст | справка, метаданные, память проекта, состояние ресурсов, RAG по документации | `docs/commands.md` — 243 КБ (~60 тыс. токенов), полные схемы 152 инструментов — 131 КБ (~33 тыс. токенов): целиком в контекст не положить, нужен роутер по группам (`--toolsets`) или поиск инструментов, как `tool_search` у Kiro и роутер у Гига-помощника |
| Подтверждения | deny > ask > allow; чтения авто; разрушительные — всегда спросить; разбиение составных команд; доверие рабочей области | у evo уже есть `mutating`, `readOnlyHint`/`destructiveHint`, `confirmed` и превью с ценой; не хватает политики на стороне клиента и подтверждения человеком через elicitation (решение D10 бэклога) |
| Телеметрия | разнобой: Azure MCP и AWS API MCP — вкл. по умолчанию; Gemini CLI — выкл.; Kiro не использует контент корпоративных (IdC) пользователей | для enterprise в РФ — выкл. по умолчанию, явный opt-in, запросы не хранить и не использовать для обучения (как заявляет Yandex Cloud) |
| Открытость | откат: Q Developer CLI и Gemini CLI закрыты после перехода на Kiro и Antigravity | Apache-2.0 у evo — аргумент для enterprise: код, который ходит в облако с их ключами, можно аудировать |

## 3. Что уже есть у Cloud.ru и Сбера

Раздел собран по открытым источникам (октябрь 2026); то, что не удалось
подтвердить первичным документом, помечено как «не подтверждено».

### 3.1. Гига-помощник (бывшая «Клаудия»)

| Факт | Источник |
|---|---|
| Запущен в июне 2025 (GigaConf) как AI-помощник консоли Cloud.ru Evolution в Public Preview; в сентябре 2025 — сценарии SRE и FinOps; в 2026 году в прессе называется «Гига-помощник» | ComNews 25.06.2025, 03.09.2025, 23.04.2026 |
| Архитектура (по статье команды на Хабре, октябрь 2025): GigaChat MAX 2.0 + LangGraph, мультиагентная система из 7 агентов (Main Router, Monitoring Router, Advisor, VM Creator, Alerting, Visualization, Analyzer), RAG с гибридным поиском и эмбеддингами BGE; интеграция с облаком через integration API и события фронтенда | habr.com/ru/articles/952346 |
| Апрель 2026: создание нескольких ВМ за раз и управление ими; три готовых агента — DevOps (PostgreSQL, Kafka, WordPress, GitLab, MinIO по промпту), SRE (виджеты, алерты, поиск причин), FinOps («зомби»-ресурсы, самые дорогие ресурсы, динамика трат) | ComNews 23.04.2026 |
| Метрики за полгода: 27 000+ пользователей, 330 000+ сообщений, каждый четвёртый возвращается, 1 000+ запущенных ВМ, создание ВМ 1–2 минуты вместо 5–30 | ComNews 23.04.2026 |
| Канал — только чат в консоли (бесплатно, для роли «Администратор организации»); в режиме co-pilot генерирует команды для serial console, которые пользователь вставляет сам; «спрашивает разрешения на важные действия». CLI, API ассистента и MCP в публичных материалах не упоминаются | cloud.ru/ai-assistant, ComNews |

**Вывод.** У Cloud.ru уже есть «мозг» — оркестратор агентов на LangGraph с
RAG по документации и интеграцией с control plane, но он заперт в
веб-консоли. Это главный актив для CLI: не нужно строить второй ассистент,
нужно открыть первый через API.

### 3.2. Evolution Foundation Models

| Факт | Источник |
|---|---|
| OpenAI-совместимый API: `https://foundation-models.api.cloud.ru/v1`; ключ — API key сервисного аккаунта с областью «Foundation Models», срок 1 день — 1 год | cloud.ru/docs/foundation-models quickstart |
| Каталог: 90 моделей (на 18.10.2026), из них ~24 внутреннего размещения — GigaChat 3 / 3.5 / 2-Max, Qwen3.x, gpt-oss-120b, DeepSeek-V4, GLM-5.x, MiniMax, Kimi, эмбеддинги BGE/Qwen, Whisper; остальные — внешнего размещения (Anthropic Claude, Google Gemini, OpenAI GPT-5.x и др.) через тот же API | Хабр Cloud.ru 927880, ComNews 14.08.2025, 24.11.2025, Inc. 2026 |
| Цены с 01.11.2025: в среднем 35 ₽ / 1M входных и 70 ₽ / 1M выходных токенов (открытые модели); 450 млрд токенов обработано с ноября 2025 | tproger (GoCloud Tech), интервью Код.ру 15.04.2026 |
| Официальный Python-клиент `cloud-ru/evolution-openai-python`: drop-in замена OpenAI SDK, аутентификация `key_id`/`secret` → IAM-токен с автообновлением | GitHub |
| Вызов инструментов **подтверждён** (проверка 18.10.2026 запросом `GET /v1/models`, без ключа): у каждой модели поля `function_calling`, `structure_output`, `reasoning`, `context_length`. Из моделей внутреннего размещения `function_calling: true` у `ai-sage/GigaChat3-10B-A1.8B`, `GigaChat/GigaChat-3-Pro`, `ai-sage/GigaChat3.5-432B-A28B(-Reasoning)`, `Qwen/Qwen3-Coder-Next`, `Qwen/Qwen3.5-397B-A17B`, `openai/gpt-oss-120b`, `deepseek-ai/DeepSeek-V4-Pro`, `zai-org/GLM-5.x`, `MiniMaxAI/MiniMax-M3`; **нет** у `GigaChat/GigaChat-2-Max` (контекст 131k). Всего в каталоге 90 моделей; страница «Обзор доступных моделей» делит их на внутреннее и внешнее размещение (внешнее — Anthropic, Google, OpenAI и др.) | `foundation-models.api.cloud.ru/v1/models`, cloud.ru/docs overview__available__models |
| В руководствах (quickstart, `guides__llm-chat-request`) параметры `tools`/`tool_choice` не документированы — для агентного режима нужен прямой тест на выбранной модели | cloud.ru/docs/foundation-models |
| Туториалы по подключению к Aider, VS Code, Open WebUI, Chatbox, LibreChat — Cloud.ru уже позиционирует FM как бэкенд для сторонних консольных ассистентов | cloud.ru/docs/tutorials-evolution |

### 3.3. GigaChat API (Сбер)

| Факт | Источник |
|---|---|
| Нативный вызов функций: поле `functions` в `/chat/completions`, ответ `function_call` + `finish_reason: "function_call"`, `functions_state_id`; все модели GigaChat поддерживают | developers.sber.ru function-calling |
| OpenAI-совместимый контур: `/openai/v1/chat/completions` с `tools` и `tool_choice` (пример для GigaChat-3-Ultra в документации B2Bank); с июля 2026 — единый URL `https://api.giga.chat` для физлиц и юрлиц | developers.sber.ru (B2Bank guides, changelog) |
| Агенты и MCP: официальный туториал «GigaChat-агент с MCP-сервером» — LangGraph ReAct + `langchain_mcp_adapters`, т. е. MCP подключается через адаптер, а не нативно в API | developers.sber.ru gigachain |
| Ограничения: самоподписанные TLS-сертификаты (LiteLLM требует `ssl_verify=False`), документация местами отстаёт от API | docs.litellm.ai |

### 3.4. GigaCode и GigaCode CLI

| Факт | Источник |
|---|---|
| IDE-ассистент (VS Code, JetBrains, GigaIDE, Android Studio, Jupyter); версия 2.0 — ансамбль моделей (GigaChat + кодовые), 35+ языков, агентный режим; бесплатен авторизованным пользователям GitVerse | sber.pro, Хабр, kod.ru |
| GigaCode CLI: генерация и анализ кода из терминала на любой ОС, встраивание в сборку/тесты/доставку (CI), доступ к 30+ моделям; есть в on-premise-версии и в облачной | sber.pro (20 июля) |
| Май 2026: интеграция GigaCode с Evolution AI Factory через плагин GigaIDE — корпоративные клиенты Cloud.ru получают 20+ LLM из Foundation Models с оплатой в рублях, режимы «чат» и «агент» | ComNews 20.05.2026 |
| Публичной документации API GigaCode CLI (команды, конфиг, MCP-клиент, схема аутентификации) **не найдено** | не подтверждено |

### 3.5. Evolution AI Agents, EvoClaw, Guardrails

| Факт | Источник |
|---|---|
| Хостинг MCP-серверов: из каталога или из образа в Artifact Registry, serverless (min 0 инстансов), аутентификация access key / API key сервисного аккаунта; к одному агенту — несколько MCP-серверов с включением/выключением инструментов | cloud.ru/docs/ai-agents, дайджест март 2026 |
| Хореография агентов без оркестратора, A2A, память агента (сессионная и долгосрочная), сценарии | дайджест март 2026 |
| EvoClaw — управляемый OpenClaw (Zero Trust, пониженные привилегии, изоляция рабочих пространств); AI Workflows — конструктор уровня n8n | дайджест март 2026, Код.ру 15.04.2026 |
| Guardrails Filter: маскирует ПДн, реквизиты, API-ключи и секреты до отправки в LLM и восстанавливает после ответа; доступен on-premise | Код.ру 15.04.2026 |
| `cloud-ru/evo-aifactory-skills`: шесть агент-агностичных skills (Claude Code, Cursor, Windsurf, Cline, Aider) для сервисного аккаунта, Foundation Models, ML Inference, ВМ, Managed RAG, AI Agents; Evo CLI в них не упоминается | GitHub |

## 4. Где сейчас Evo CLI

Выведено из кода и документации репозитория (ветка `main`, v1.0.1).

| Свойство | Состояние | Почему это важно для ассистента |
|---|---|---|
| Язык и стек | Go, cobra, статический бинарь без зависимостей, Apache-2.0 | ассистент можно встроить в тот же бинарь; нет Node/Python-рантайма, как у Gemini CLI или `aws` |
| Единый реестр инструментов | 152 инструмента в `internal/tools`, одни и те же для деревьев CLI (`vm`, `k8s`, `pg`…), `evo tools call` и MCP-сервера `evo mcp`; у каждого — JSON-schema аргументов, флаг `mutating`, аннотации `readOnlyHint`/`destructiveHint` | это уже готовый «набор функций» для tool calling; у гиперскейлеров такой слой пришлось строить отдельно |
| Модель подтверждений | изменяющие операции без `--yes` / `confirmed=true` возвращают превью с ценой; без TTY — код выхода `3`; в MCP — `status: confirmation_required` + поле `approval` с инструкцией агенту | это и есть «preview-and-confirm», который Azure, AWS и Google считают обязательным для агента; в evo граница проходит в сервере, до облака |
| Машиночитаемость | `--json` у всех команд, стабильные коды выхода, ошибки с `reason` (`AUTH_FAILED`, `NOT_FOUND`…) и `hint` | готовый контекст для «объясни ошибку» без парсинга текста |
| Самоописание | `docs/commands.md` и `docs/tools.md` генерируются из бинаря; `evo tools describe` отдаёт схему | можно собирать контекст для модели из бинаря, а не из внешней базы |
| Контур доступа | `--toolsets` (экономия контекста, не граница), `--allow-project` (граница), HTTP с bearer, Docker-образ для Marketplace; в бэклоге — `--auth platform` для хостинга в AI Agents и OAuth 2.1 | те же механизмы ограничат и встроенного ассистента |
| Оркестрация для слабых моделей | `stack_apply`/`stack_status`: цикл выполнения вынесен из агента в сервер, контекст агента O(1) | уже учтён опыт работы с малоконтекстными LLM; важно для GigaChat Lightning / 10B-моделей |
| Локализация | справка и ошибки по-русски (`EVO_LANG=en`), MCP — всегда по-английски | ассистент должен говорить по-русски; промпты инструментов — английские |
| Решение D10 (бэклог) | подтверждение человеком на стороне сервера через MCP elicitation (ревизия 2025-06-18), v1.1 | закрывает главный риск «гейт останавливает вызов, а не агента» |
| Наблюдаемость | клиент Audit Logging API, логгер с санитизацией секретов | трассы действий ассистента можно писать туда же |

**Итог.** Evo CLI уже реализовал «нижнюю половину» архитектуры
гиперскейлеров — слой инструментов с подтверждениями и MCP-сервер.
Отсутствует «верхняя половина»: вызов модели, диалог, перевод естественного
языка в команды, объяснение ошибок.

## 5. Варианты для Evo CLI

Варианты не исключают друг друга; оценка сложности — порядок величины для
одного инженера, знакомого с кодом evo [Г].

| Вариант | Что это | Аналог на рынке | Сложность | Главные риски |
|---|---|---|---|---|
| **A. `evo ask` и объяснение ошибок** | модель получает запрос и сжатый индекс дерева команд, возвращает команду в структурированном виде; evo проверяет её по дереву cobra (команда и флаги существуют), показывает, по Enter запускает обычным путём с теми же гейтами подтверждения. На ошибке с `reason` — опциональная подсказка «объяснить» | `az copilot`, `kiro-cli translate`, `gh copilot suggest` (закрыт), `azd` troubleshooting | низкая: один HTTP-клиент к OpenAI-совместимому API без новых зависимостей; недели | галлюцинации флагов (снимается валидацией); вопрос, кто платит за токены; может не найти спроса — как `gh copilot suggest` |
| **B. `evo chat` — агент в бинаре** | цикл «модель ↔ реестр инструментов» через function calling; политика: чтения авто, изменяющие — превью и подтверждение в терминале, разрушительные — всегда; **без инструментов shell и файловой системы** | Kiro, Gemini CLI, Copilot CLI, но с замкнутым набором инструментов | средняя–высокая: роутер по 23 группам инструментов, стриминг, сессии, тесты на инъекции; квартал | третий ассистент рядом с Гига-помощником и GigaCode; качество малых моделей; стоимость поддержки |
| **C. «evo — инструмент, не мозг»** | довести MCP до уровня лидеров: подтверждение человеком через elicitation (D10), хостируемый `evo mcp` в AI Agents с OAuth, skills для Claude Code / Cursor / Codex / GigaCode CLI, `evo --json` как инструмент агента (как у Yandex Cloud) | AWS Agent Toolkit, Azure Skills Plugin, IBM Cloud CLI, Yandex MCP | низкая–средняя; большая часть уже сделана | зависимость от чужих агентов (Claude Code и Codex недоступны части enterprise в РФ); нет собственной витрины |
| **D. Тонкий клиент к Гига-помощнику** | `evo ask` обращается к API ассистента (LangGraph, RAG по документации, знание аккаунта), получает план или команды, evo исполняет локально с гейтами | AI Shell + агент Azure Copilot: мозг в облаке, токен из `az login` | зависит от платформенной команды: нужен публичный API ассистента и маппинг идентичности (ассистент работает под пользователем консоли, evo — под сервисным аккаунтом) | организационный: два владельца; латентность; API пока нет |

Схема варианта A поверх существующего кода:

```
evo ask "удали диск data-old"            evo vm create … (ошибка AUTH_FAILED)
   │                                          │ --explain (opt-in)
   ▼                                          ▼
индекс команд (генерируется из cobra, как docs/commands.md) + запрос
   │
   ▼
Foundation Models (/v1/chat/completions, structured output)
   │  {"command":"evo disk delete data-old","why":"…"}
   ▼
валидация по дереву cobra ──✗──▶ «не нашёл такой команды, вот ближайшие»
   │ ✓
   ▼
показать команду → Enter → обычный путь: превью, цена, y/д, код выхода 3
```

Схема варианта B — тот же реестр `internal/tools`, что у MCP, но
потребитель — встроенный клиент модели, а не внешний агент:

```
evo chat ─▶ роутер: группы инструментов по запросу (--toolsets)
         ─▶ модель с tools = схемы 10–30 инструментов
         ─▶ вызов: readOnlyHint → выполнить; mutating → превью → спросить в TTY
         ─▶ результат инструмента = недоверенные данные для модели
```

## 6. Рекомендация

Позиция автора [Г], в порядке приоритета. Это не план работ: решения о
сроках и бюджете — за владельцем продукта.

**Этап 0 — без модели (вариант C).** Закрыть D10: подтверждение человеком
на стороне сервера через MCP elicitation. Опубликовать skill для evo в
`cloud-ru/evo-aifactory-skills` (сейчас evo там не упомянут). Проверить
`evo mcp` как инструмент GigaCode CLI (есть ли у него MCP-клиент — публично
не описано). Довести хостинг `evo mcp` в Evolution AI Agents
(`--auth platform`, OAuth 2.1 из бэклога). Это ровно то, что AWS, Azure и
Google делают в 2026 году, и это нужно при любом решении по A/B/D.

**Этап 1 — вариант A на Foundation Models.** Настройки `ai.endpoint`,
`ai.model`, `ai.api_key` в профиле, по умолчанию выключено (`evo configure
--ai`). Модель по умолчанию — из внутреннего размещения с вызовом функций и
структурированным выводом (GigaChat 3 Pro / 3.5 или Qwen3-Coder-Next);
модели внешнего размещения (Anthropic, OpenAI, Google) — только явный
opt-in с предупреждением о трансграничной передаче. Метрики пилота на
CREW-тестировщиках: доля команд, принятых без правки; доля отказов на
превью; p50 латентности; стоимость запроса в рублях.

**Этап 2 — вариант B за флагом**, только если метрики этапа 1 показывают
спрос на многошаговые сценарии, которые не закрывают внешние агенты через
MCP. Без инструментов shell и файловой системы: замкнутый набор
инструментов evo — главное преимущество перед универсальными агентами по
профилю рисков.

**Стратегически — вариант D.** Один «мозг» для консоли, CLI и IDE
(GigaCode через AI Factory). Иначе Cloud.ru получит три ассистента с
разной логикой подтверждений и разным знанием документации. Это решение
уровня платформы, а не репозитория evo.

**Чего не делать.** Не копировать Kiro или Gemini CLI целиком —
универсальный агент с shell другой категории продукта, с другим профилем
рисков и стоимостью поддержки. Не строить в CLI второй RAG по документации:
он уже есть у Гига-помощника.

## 7. Риски и контраргументы

- **Спрос не доказан.** Аудитория CLI уже сидит в Claude Code, Cursor,
  Codex и подключает `evo mcp`. `gh copilot suggest` закрыли за ненадобностью.
  Проверять на CREW до инвестиций в B.
- **Экономика токенов.** Гига-помощник бесплатен; Kiro — от 50 кредитов до
  $200/мес. Кто платит за `evo ask`: пользователь своим ключом Foundation
  Models или Cloud.ru? Для партнёров Marketplace это вопрос монетизации
  Foundation Models, а не фичи CLI.
- **Комплаенс и данные.** Внешнее размещение моделей — трансграничная
  передача; имена ресурсов, IP, теги, логи `evo logs` могут быть
  конфиденциальны. Guardrails Filter — обязательный слой для enterprise.
  Никогда не передавать модели `credentials.yaml`, ответы `secret_value`,
  `cert_private_key`, `k8s_kubeconfig` (HTTP-политика evo их уже блокирует).
- **Инъекции через данные.** Выводы инструментов (имена ВМ, метаданные
  образов, логи) — недоверенный текст для модели. Гейт `confirmed=true`
  останавливает вызов, а не агента: модель может сама повторить вызов с
  `confirmed=true` (это записано в `docs/mcp.md`). Поэтому подтверждение
  человеком должно жить в терминале evo (TTY или elicitation), а не в
  решении модели.
- **Галлюцинации.** Несуществующие флаги и UUID. Валидация по дереву cobra
  обязательна; у AWS это «validated AWS CLI commands», у gcloud-mcp —
  denylist.
- **Зависимость от моделей.** `GigaChat-2-Max` в Foundation Models без
  вызова функций; у малых моделей слабое следование схеме и малый
  контекст — stack-паттерн evo (цикл выполнения на стороне сервера) уже
  отвечает на это.
- **Открытый код.** Промпты и политика будут публичны — плюс для доверия,
  минус для «секретного соуса». Эндпоинты и ключи внутреннего контура в
  репозиторий класть нельзя.
- **Организация.** Гига-помощник — другой продукт и другая команда; без
  решения о едином API получится дублирование и расхождение в поведении.
- **Хрупкость рынка.** За год два флагманских OSS-агента (Q Developer CLI,
  Gemini CLI) закрыли и переименовали; строить интеграции на один чужой
  агент рискованно — поэтому MCP и skills, а не плагин под конкретный хост.

## 8. Открытые вопросы и следующие шаги

1. Проверить вызов функций на Foundation Models практически (нужен API-ключ):
   запрос с `tools` к GigaChat 3 Pro / 3.5 и Qwen3-Coder-Next; качество
   выбора из 20–30 инструментов evo; стоимость и латентность.
2. Выяснить у Сбера: есть ли у GigaCode CLI MCP-клиент и конфигурация
   серверов; условия подключения `evo mcp` в on-prem версии.
3. Выяснить у команды Гига-помощника: планы API или MCP-интерфейса
   ассистента; модель идентичности (пользователь консоли vs сервисный
   аккаунт); возможность вызывать `evo mcp` из ассистента как инструмент.
4. Решить вопрос биллинга токенов и бюджета пилота.
5. Прототип варианта A на 10 сценариях из `docs/guide.md`; метрики на CREW.
6. Закрыть D10 (elicitation) — независимо от решения по ассистенту.
7. Юридическая оценка: использование моделей внешнего размещения для
   клиентов enterprise; условия Foundation Models для встраивания в
   продукт с открытым кодом.

## 9. Источники

Дата обращения ко всем источникам — 18.10.2026.

**Azure.** github.com/irarainey/az-copilot · learn.microsoft.com
PowerShell AI Shell overview и agent-azure · learn.microsoft.com Azure
Copilot overview · learn.microsoft.com azure-mcp-server overview ·
github.com/microsoft/mcp (servers/Azure.Mcp.Server) · devblogs.microsoft.com
«Announcing the Azure Skills Plugin» (09.03.2026) · github.com/microsoft/
github-copilot-for-azure · learn.microsoft.com azd copilot-coding-agent
extension · devblogs.microsoft.com azd March 2026.

**AWS.** github.com/aws/amazon-q-developer-cli · kiro.dev/docs
(migrating-from-q, cli, permissions, built-in-tools, headless, mcp/security,
billing) · docs.aws.amazon.com amazonq command-line · github.com/awslabs/mcp ·
awslabs.github.io/mcp aws-api-mcp-server · aws.amazon.com what's-new
2026/05 agent-toolkit · research.jfrog.com (инцидент Amazon Q 1.84.0) [В].

**Google.** github.com/google-gemini/gemini-cli · geminicli.com docs
(trusted-folders, policy-engine, telemetry, quota-and-pricing) ·
github.com/googleapis/gcloud-mcp · github.com/GoogleCloudPlatform/
cloud-run-mcp · github.com/google-github-actions/run-gemini-cli ·
docs.cloud.google.com cloud-assist chat-panel · developers.googleblog.com
«Transitioning Gemini CLI to Antigravity CLI» (19.05.2026) ·
bleepingcomputer.com, endorlabs.com CVE-2026-12537 [В].

**Oracle, Alibaba, прочие.** github.com/oracle/mcp · docs.oracle.com
Console AI preview (07.2026) · help.aliyun.com (aliyun CLI, mcp-proxy,
OpenAPI MCP Server, CloudControl MCP) · github.com/aliyun/aliyun-cli ·
github.com/QwenLM/qwen-code · IBM Cloud CLI MCP — зеркало README
(cloud.ibm.com отвечал 500) [В] · github.com/digitalocean-labs/
mcp-digitalocean · developers.cloudflare.com MCP servers · vercel.com/docs
MCP · docs.netlify.com MCP server.

**Референсные агенты.** github.blog changelog (deprecation gh-copilot
25.09.2025; Copilot CLI GA 25.02.2026) · github.com/github/copilot-cli ·
docs.github.com Copilot CLI allowing-tools · code.claude.com/docs (iam,
permissions) · github.com/openai/codex · learn.chatgpt.com docs sandboxing,
MCP · docs.warp.dev.

**Российский рынок.** yandex.cloud/ru/docs console ai-assistant, overview
mcp-servers · cloud.vk.ru docs ai-bot · ComNews 25.06.2025, 03.09.2025,
23.04.2026 (Гига-помощник), 20.05.2026 (GigaCode + AI Factory), 14.08.2025,
24.11.2025 (Foundation Models) · habr.com/ru/articles/952346 (архитектура
Клаудии) · habr.com/ru/companies/cloud_ru/posts/927880 · cloud.ru/blog
daydzhest-mart-2026 · kod.ru интервью Cloud.ru 15.04.2026 · tproger
(GoCloud Tech, цены LLM) · cloud.ru/docs foundation-models (quickstart,
guides__llm-chat-request, overview__available__models), ai-agents
guides__create-mcp-server, tutorials-evolution (Aider, VS Code) ·
`GET https://foundation-models.api.cloud.ru/v1/models` (без ключа,
18.10.2026) · github.com/cloud-ru/evo-aifactory-skills ·
github.com/cloud-ru/evolution-openai-python · sber.pro (GigaCode CLI,
GigaCode 2.0) · developers.sber.ru (gigachat function-calling,
compatible-openai, changelog, gigachain agent-gigachat-mcp, b2bank
sending-openai-request) · docs.litellm.ai providers/gigachat [В].

**Не подтверждено первичными источниками:** официальная документация IBM
Cloud CLI в режиме MCP; закрытость Antigravity CLI; тарифы Kiro по моделям;
команды, MCP-клиент и лицензирование GigaCode CLI; переименование «Клаудии»
в «Гига-помощник» (вывод из совпадения даты запуска и описания функций).
