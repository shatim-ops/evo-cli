# Этап 9 — Документация для внешнего пользователя

Контекст: `00-audit.md` разд. 1 (README описывает внутренний компонент).
Зависит от этапов 2, 4, 7 (команды и релизы должны существовать).

## Структура

```
README.md                 # короткий: что это, установка за 3 команды, quickstart, ссылки
docs/install.md           # linux/macos/windows, проверка подписи, Gatekeeper/SmartScreen, kubectl/helm
docs/auth.md              # создание SA и ключа в консоли (скриншоты не нужны — пути по меню),
                          # роли, configure/профили, таблица ВСЕХ env, ротация ключей
docs/commands.md          # генерируется: cobra `GenMarkdownTree` в CI, проверка на расхождение
docs/mcp.md               # stdio и HTTP, Claude Code/Cursor конфиги, Docker, модель подтверждений
docs/exit-codes.md        # таблица кодов из этапа 1
docs/troubleshooting.md   # типовые ошибки IAM/квоты/429/SSH, что собрать для обращения в поддержку
docs/RELEASING.md         # из этапа 7
docs/internal/standalone/ # эти планы (перенесены сюда из docs/standalone/ на этапе 9)
```

Язык: README и docs — русский (аудитория — РФ), `--help` — английский
(решение D6). Внутреннюю схему системы, из которой вырос движок, — убрать из README в
`docs/internal/`.

## Обязательное содержимое

1. **Таблица env/флагов** — полная: `CLOUDRU_KEY_ID`, `CLOUDRU_SECRET`,
   `CLOUDRU_API_KEY` (deprecated), `CLOUDRU_PROJECT_ID`, `CLOUDRU_AGREEMENT_ID`,
   `CLOUDRU_REGION`, `CLOUDRU_IMAGE_ID`, `CLOUDRU_S3_*`, `CLOUDRU_PROFILE`,
   `CLOUDRU_CONFIG_DIR`, `MCP_BEARER_TOKEN`. Сверить с кодом grep'ом
   `os.Getenv\|env:"` — тест на расхождение документации и кода.
2. **Роли сервисного аккаунта.** В документации Cloud.ru публичны роли уровня
   платформы (`platform.project.admin|user|viewer`, `iam.serviceaccount.*`);
   сервисные роли `compute.*` не опубликованы. Писать честно: «минимально
   проверенный набор — …; если команда возвращает 403, см. раздел …».
   Проверить набор вживую на этапе приёмки, зафиксировать дату проверки.
3. **Что хранится на машине пользователя** и где: конфиг, креды (0600), кэш
   токена, SSH-ключ проекта (`./.cloudru-vm/`), state. Как удалить всё
   (`cloudru-vm configure delete`, `rm -rf`).
4. **Модель подтверждений** одним абзацем с примерами `--yes`/`--dry-run`/код 3
   — одинаково для человека и агента.
5. **Ограничения:** `k8s_kubectl/helm` требуют внешних бинарников; недоступны в
   Docker-режиме; Windows — без гарантий для них.
6. **Поддержка:** куда писать, что приложить (`doctor --json`), SLA по оферте
   (L2/L3 на партнёре) — текст согласовать с владельцем.

## Критерии приёмки

- Человек без доступа к этому репозиторию и без помощи проходит `install → configure → vm list → deploy --dry-run` по документации. Проверить на одном коллеге, зафиксировать время.
- Тест в CI: каждая env-переменная из кода упомянута в `docs/auth.md`; `docs/commands.md` совпадает с `GenMarkdownTree`.
