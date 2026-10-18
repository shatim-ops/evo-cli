# Этап 2 — Креды и конфиг: `configure`, профили, `whoami`, `doctor`

Контекст: `00-audit.md` разд. 1 («Креды», «Токен») и разд. 3 (терминология
API-ключ). Зависит от этапа 1 (коды возврата).

## Целевая модель

```
$CONFIG/cloudru-vm/config.yaml        # профили: project_id, region, defaults  (0600)
$CONFIG/cloudru-vm/credentials.yaml   # per-profile: key_id, secret             (0600)
$CACHE/cloudru-vm/token-<profile>.json # кэш IAM-токена с exp                   (0600)
./.cloudru-vm.yaml                     # проектные overrides (без секретов)
```

`$CONFIG` = `os.UserConfigDir()`, `$CACHE` = `os.UserCacheDir()`. Переопределение:
`CLOUDRU_CONFIG_DIR`. Приоритет источников (высший первый):
флаги → env → `./.cloudru-vm.yaml` → профиль из `$CONFIG` → дефолты.

Профиль выбирается `--profile` / `CLOUDRU_PROFILE` / `default`.

## Задачи

1. **Чтение кредов из файла реально работает.** Сейчас `KeyID`/`Secret` не
   мержатся из файла/env (`internal/config/config.go:73-86,143-180`), а
   `auth.EnvVarProvider` читает только `os.Getenv`. Сделать провайдер, который
   получает креды из разрешённого `config.Config`, а не из окружения напрямую.
2. **Команды:**
   - `configure [--profile p]` — интерактивно: Key ID, Secret (без эха,
     `x/term`), затем запрос `GET organization.api.cloud.ru/v3/projects` и
     выбор project_id из списка (или ввод вручную при одном варианте —
     автоматом). Пишет оба файла с 0600, каталог 0700. Неинтерактивный режим:
     `configure --key-id … --secret-stdin --project-id …`.
   - `whoami` — обменивает ключ на токен, печатает профиль, key_id (маскированный),
     project_id, срок токена. Код 2 при неудаче.
   - `doctor` — проверяет: файлы и права, достижимость IAM/compute, наличие
     `kubectl`/`helm` в PATH (предупреждение, не ошибка), версию. Подключить
     или удалить `config.LoadWithSources` — он написан под эту команду.
   - `configure list` / `configure delete <profile>`.
3. **Кэш токена на диске** (TTL 1 ч по документации Cloud.ru; обновлять при
   остатке < 5 мин, как сейчас). В MCP-режиме держать один Engine на сессию
   либо общий `TokenSource` — обмен с IAM не должен идти на каждый вызов.
4. **Удалить** заглушку `auth.KeyringProvider` (`internal/auth/keyring.go`) и
   упоминание несуществующего `cloudru-vm login`. Keyring — в BACKLOG как v1.1.
5. **Переменная `CLOUDRU_API_KEY`.** У Cloud.ru «API-ключ» — отдельная
   сущность (`Authorization: Api-Key`). Оставить поддержку формата
   `keyId:secret` как deprecated с предупреждением в stderr; основной путь —
   `CLOUDRU_KEY_ID`/`CLOUDRU_SECRET` или файл. Настоящий `Api-Key` не
   реализовывать (не все сервисы поддерживают) — записать в BACKLOG.
6. **S3-креды** (`CLOUDRU_S3_*`) — переносить в профиль как опциональную секцию
   `s3:`; env остаётся приоритетнее.
7. **Документировать требуемые роли** для сервисного аккаунта в тексте
   `configure --help` одной строкой и ссылкой на README (сам README — этап 9).

## Не делать

- Не менять эндпоинт IAM и формат запроса токена.
- Не добавлять OS keyring и OAuth/device flow.
- Не трогать `./.cloudru-vm/` (ключ, state) — это этап 5.

## Критерии приёмки

```
env -i PATH=$PATH HOME=$HOME cloudru-vm configure        # интерактив, создаёт файлы 0600
env -i PATH=$PATH HOME=$HOME cloudru-vm whoami; echo $?   # 0, без env-переменных
stat -c %a $CONFIG/cloudru-vm/credentials.yaml           # 600
cloudru-vm whoami --profile nosuch; echo $?               # 2 с подсказкой
cloudru-vm whoami; cloudru-vm whoami                      # второй вызов без запроса к IAM (проверить тестом/флагом --verbose позже)
```

Тесты: разрешение приоритетов (флаг > env > проект > профиль), права файлов
(skip на Windows), кэш токена (истёкший → обновление, свежий → без сети),
deprecated-предупреждение для `CLOUDRU_API_KEY`.
