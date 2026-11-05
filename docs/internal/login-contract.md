# Контракт входа `evo login` / `evo logout` (B-1)

Строка плана 1.8 **B-1**. Контракт написала «Fable — авторизация» 05.11, перенос в документ и сверку с кодом сделала «1.8 — разработка». Этот документ — то, по чему сверяет «Fable». Код — `internal/login`, `internal/iamadmin/login.go`, `internal/cli/login.go`, PR #987 (B-3). Мир паритета — `internal/loginworld` (B-2, #972). Запросы, тела и поля — записи описи `scripts/api-contract.json`: службы `oidc`, `organization`, `iam`, с `_note` у каждой записи входа.

Условия «Риска» вошли в контракт: заключение 24 и два дополнения (#991, комментарии в #987), принятые решением владельца #988. Это R-47 (токен согласия), «у `login` нет `--yes`», «терминал не выдаёт доступ», У-8 (объявление в описи), У-9 (ошибка `evaluate` — не отказ).

Гипотезы до живой пробы B-5 и прогона 2 (R-46):
- имя роли `platform.project.admin` как `roleIdentifier.roleName`;
- имена действий `evaluate` (`auth.LoginActionCreateAccount`, `auth.LoginActionGrantRole`);
- удаление ключа токеном самого аккаунта в `logout`.

## 1. Константы

Одна таблица — `internal/auth` (`oidc.go`). Адрес литералом стоит только в ней.

| Что | Значение |
|---|---|
| issuer | `auth.OIDCIssuer`: сверяется побайтно с полем `issuer` discovery (RFC 8414 §3.3) |
| discovery | `<issuer>/.well-known/openid-configuration`; `authorization_endpoint`, `token_endpoint`, `jwks_uri` берутся только отсюда, хосты — из закрытого списка `auth.TrustedOIDCURL` |
| client_id | `evo_cmcp`: публичный, без секрета |
| scope | `openid` |
| PKCE | только `S256` |
| redirect_uri | `http://127.0.0.1:{18386,23386,28386}/callback` (`auth.LoginRedirectPorts`), первый свободный по порядку |
| Роль | `auth.LoginRoleName` («Администратор проекта»); замена на роль без удаления (R-24) — одна строка |
| Сервисные аккаунты | `evo` — для человека, `evo-agent` — для агента |
| Описание аккаунта и ключа | «evo login, <логин>, <дата>» — без хоста |
| Срок ключа | 7, 30 или 90 дней, по умолчанию 30; бессрочный — только в консоли |
| Ожидание входа | 5 минут (`wall-clock:`) |
| Лимиты (#563; до подтверждения IAM) | 10 аккаунтов в проекте, 50 ключей у аккаунта |

## 2. Поток `evo login`

`evo login [--profile P] [--project ID|имя] [--for me|agent] [--key-ttl 7d|30d|90d] [--no-browser]`

1. **Флаги.** Значение вне `me|agent` или вне 7/30/90 — `USAGE` до любого запроса. **`--yes` — `USAGE` до любого запроса**: доступ выдаёт только страница («Риск» 24).
2. **Терминал**, если есть stdin-TTY и нет `--json`, до браузера спрашивает только «кому» (по умолчанию — агенту) и срок. Затем печатает сводку: аккаунт, роль до даты, ключ на N дней, профиль. И строку «доступ выдаёт только кнопка «Выдать доступ» на странице». Ответ терминала доступ не выдаёт: псевдо-TTY получает любой процесс.
3. **Discovery** и проверка issuer; PKCE `code_verifier` (43+ символа), `code_challenge` = BASE64URL(SHA256); `state` и `nonce` — по 128 бит.
4. **Слушатель** — только 127.0.0.1, первый свободный порт таблицы; все заняты — `CONFIG_INVALID`.
5. **Адрес входа** — в stderr (с `--json` — строкой JSON) и в браузер. Ошибка открытия — не ошибка входа. С `--no-browser` выводится, что по SSH и в контейнере вход не пройдёт: провайдер возвращает на 127.0.0.1 машины с браузером.
6. **`GET /callback`.** `Host` — только `127.0.0.1:<порт>` (защита от DNS rebinding); чужой `state` — 403; второй callback — 403; `error=access_denied` — отмена (раздел 4); другой `error` — отказ провайдера.
7. **Обмен кода**: `POST token_endpoint` (form: `grant_type=authorization_code`, `code`, `redirect_uri` — тот же, `client_id`, `code_verifier`; без `client_secret` и без редиректов). Токены — только в памяти, refresh не запрашивается.
8. **ID token**: RS256 по `kid` из JWKS, `iss`, `aud ∋ evo_cmcp`, `nonce`, `exp`. Сбой — `AUTH_FAILED`, ничего не создано.
9. **Организации и проекты**: `GET /v3/customers`, `GET /v3/projects?customer_ids=…` (без `deleted`); `--project` оставляет только его, нет такого — `USAGE`.
10. **`POST /api/v1/permissions/bulk-evaluate`** по всем проектам: «создать аккаунт» и «выдать роль».
    - Явное `accessGranted: false` везде — `AUTH_FAILED` до страницы.
    - Ошибка запроса (400, 404, недоступность) — не отказ (У-9): страница и терминал пишут «право не проверено: <причина>», дальше решает первая запись.
11. **Страница согласия** — единственная поверхность согласия. Она отдаётся один раз, в ответе на callback, и несёт **токен согласия** (128 бит, R-47): он выдаётся только с ней и нигде не печатается.
    - На странице: проект (без права — неактивен), для кого, срок, сводка; выбор флагов и терминала отмечен.
    - `POST /grant` и `POST /cancel` принимаются только с `state` и токеном (сравнение constant-time); без токена или с чужим — 403, ничего не создано.
    - Выбор, которого страница не предлагала, — `USAGE`.
    - Страница — без JS и внешних ресурсов; CSP `default-src 'none'`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`.
12. **До первой записи** (ось 1):
    - `GET /api/v1/permissions/evaluate` выбранного проекта по двум действиям: `false` — `AUTH_FAILED`, ошибка — «право не проверено» (У-9);
    - аккаунт по имени (`GET /api/v2/serviceAccounts?filter=name=…`): найден — его ключи и лимит 50; не найден — все аккаунты и лимит 10. Лимит — `USAGE`.
13. **Аккаунт**: найден — переиспользуется; иначе `POST /api/v2/serviceAccounts` (`name`, `description`, `target.projectId`); 409 — список ещё раз и переиспользовать.
14. **Роль** до срока ключа (`GET /api/v3/permissions` аккаунта):
    - есть с более коротким сроком — `PUT /api/v3/permissions/{id}` (`expiresAt` только вперёд);
    - есть с более длинным — без записи;
    - нет — `POST /api/v3/permissions` (`roleIdentifier.roleName`, `expiresAt`); 400 (коллизия имени с пользовательской ролью) — `GET /api/v1/roles` и `roleIdentifier.roleId`.
15. **Ключ последним**: `POST …/accessKeys` (`description`, `ttl` = `<N*24>h`); `secret` есть только в этом ответе.
16. **Проверка ключа** обменом на токен IAM (`POST /api/v1/auth/token`), как у каждой следующей команды.
17. **Профиль сразу** (R-44). Записывается в профиль адресата:
    - для `--for agent` — `agent`, если `--profile` или `CLOUDRU_PROFILE` не задают другой;
    - для себя — текущий профиль.
    - Поля: `key_id`, `secret`, `project_id` и `Credentials.Login` (`service_account_id`, `access_key_id`, `key_expires_at`, `customer_id`, `role`, `logged_in_as`, `login_at`).
    - Не записался — ключ удаляется.
18. **Прежний ключ этого профиля** — `DELETE` по `access_key_id` после записи нового (R-25). Не удалось — предупреждение с маской ключа, вход успешен.
19. **Итог.** Страница «Готово»; stdout или `--json`: `profile`, `key_id` (маска), `project_id`, `project_name`, `service_account`, `role`, `key_expires_at`, `reused_account`, `revoked_key_id` (маска или `null`), `warning`. Секрета нет нигде.

**Откат.** Сбой после записи шагов 13–17 удаляет созданное этим входом, токеном человека, в обратном порядке: ключ → разрешение → аккаунт. Продлённое `PUT` не откатывается: срок вперёд — не вред, а возврат был бы ещё одной записью. Отказ называет созданное, отменённое и оставшееся (`details.created`, `undone`, `left`). Если что-то осталось — `API_ERROR` и путь «консоль → Сервисные аккаунты».

## 3. `evo logout [--profile P] [--yes]`

Подтверждение. Без терминала нужен `--yes`, иначе `CONFIRMATION_REQUIRED`, 3 — общее правило evo. `--yes` здесь допустим: он удаляет, а не выдаёт права.
- Профиль из `evo login`: `DELETE /api/v2/serviceAccounts/{sa}/accessKeys/{access_key_id}` токеном самого аккаунта, затем профиль и его кэш. 404 — `already_deleted` (D26). Другая ошибка — профиль остаётся, `API_ERROR` с путём «консоль».
- Профиль без сведений входа (ключ из `evo configure` или консоли) удаляется один, с предупреждением «ключ остаётся в облаке».
- Аккаунт и роль не трогаются.

`evo configure` с другим ключом снимает сведения входа (`Credentials.ForKey`), чтобы `logout` не удалил чужой ключ.

## 4. Ответы и коды

Каждый отказ входа называет **шаг** и **HTTP-статус** ответа Cloud.ru, если отказ вызван им (решение владельца #981). Шаг и статус — в `details.step` и `details.http_status`, то же в тексте: «(шаг key, HTTP 500)». Токена и кода в отказе нет. Шаги: `discovery`, `listen`, `wait`, `callback`, `token`, `id_token`, `customers`, `projects`, `bulk-evaluate`, `choice`, `evaluate`, `account`, `permission`, `key`, `key-check`, `profile`.

| Случай | reason, код | Создано |
|---|---|---|
| флаг вне допустимого, `--yes` | `USAGE`, 1 | ничего, запросов нет |
| все порты заняты | `CONFIG_INVALID`, 2 | ничего |
| ожидание 5 минут истекло, Ctrl-C | `INTERRUPTED`, 130 | ничего |
| `error=access_denied`, «Отмена» на странице или в терминале | `USAGE`, 1 | ничего |
| другой `error` провайдера, сессия истекла (код не принят) | `AUTH_FAILED`, 2 | ничего |
| ID token не прошёл проверку | `AUTH_FAILED`, 2 | ничего |
| явное «нет права» (`accessGranted: false`) | `AUTH_FAILED`, 2 | ничего |
| ошибка `evaluate` | не отказ (У-9) | — |
| лимит аккаунтов или ключей | `USAGE`, 1 | ничего |
| проекта `--project` нет | `USAGE`, 1 | ничего |
| отказ платформы на записи, откат полный | код отказа (`API_ERROR`, 5 и т. д.) | ничего |
| откат неполный | `API_ERROR`, 5 | названо в `details.left` |
| новый ключ не получил токен IAM | `AUTH_FAILED`, 2 | откат |

Истёкший ключ `evo login` (R-26): отказ IAM 401/403 при прошедшем `key_expires_at` профиля — `AUTH_FAILED`, 2 (вариант А «релиза», а не `AUTH_EXPIRED` черновика). Текст — «ключ доступа истёк <дата>; нужен человек: evo login… Повтор не поможет», плюс `key_expired: true`.

Ломающего нет: новые команды; `login` и `logout` — `localCommands` с причиной «браузер и порт на машине человека; у MCP-сервера инструмента нет».

## 5. Инварианты

| # | Инвариант | Тест |
|---|---|---|
| 1 | Адреса — только из discovery, хост — из закрытого списка, issuer совпадает; флага `--issuer` нет | `TestCheckDiscovery` |
| 2 | Только S256 | мир B-2 отвергает иное |
| 3 | Слушатель — 127.0.0.1; один код; чужой `state` и повторный callback — 403 | `TestLogin_ForeignStateAndSecondCallback` |
| 4 | Доступ выдаёт только «Выдать доступ» по токену страницы; без токена — 403; страница отдаётся один раз (R-47) | `TestLogin_GrantTakesTheConsentToken` |
| 5 | У `login` нет `--yes`; терминал не выдаёт доступ | `TestLogin_RefusesYes`, `TestLogin_TerminalGathersThePageGrants` |
| 6 | Секрет ключа, code, verifier, токены и токен согласия — ни в один канал | проверка секретов мира в каждом тесте `internal/login`; `TestLoginLogout_OnTheWorld` |
| 7 | Токен человека не сохраняется | только поле `session.human` |
| 8 | Повторный вход не плодит аккаунты (по имени), прежний ключ отзывается по `access_key_id` | `TestLogin_PreviousKeyRevokedByID`, вариант `AccountExists` |
| 9 | Отказ после записи не оставляет аккаунт без ключа или ключ без профиля | `TestLogin_Variants` (обрывы), `TestLogin_ProfileFailsKeyGoes` |
| 10 | `logout` без удаления ключа не выглядит успехом; 404 — `already_deleted` | `TestLoginLogout_OnTheWorld` |
| 11 | Отказ называет шаг и статус | `TestLogin_RefusalNamesStepAndStatus` |
| 12 | Отказ до записи — только по явному «нет права» | `TestLogin_EvaluateErrorIsNoRefusal` |
| 13 | Каждая запись входа объявлена в описи: `creates: ["evo login"]` и `impact` | `TestWorkflows_WritesDeclareCreates`, `TestLocalCommands_CreatesAreCommands` (#989) |
| 14 | Тесты не занимают зарегистрированные порты: в тестах `Ports = [0]`, мир принимает любой loopback-порт (`AnyLoopbackPort`) | `TestLogin_RegisteredPorts` |

## 6. Мир паритета (B-2)

`internal/loginworld`: discovery, authorize (302 с кодом), token (проверка verifier), JWKS (тестовый RS256), customers, projects, roles, actions, evaluate, bulk-evaluate, serviceAccounts, permissions (POST, PUT, DELETE), accessKeys — без сети.

Варианты:
- поведение человека и провайдера: отмена, чужой `state`, второй callback, протухший код, ID token с ошибкой в `iss`, `aud`, `nonce`, подписи или `exp`;
- права: нет права, `EvaluateUnknown` (400 на `evaluate`);
- аккаунт и роль: аккаунт есть, 409, роль короче или длиннее срока, 403 на permissions, коллизия имени роли;
- лимиты; обрыв на каждом шаге 13–16;
- `AnyLoopbackPort`.

## 7. Только живьём

Вход с `scope=openid`, чтение `/v3/customers` и `/v3/projects` токеном человека, имена действий `evaluate`, `roleName`, `ttl` и `expiredAt` ключа, удаление ключа его же токеном — живая проба B-5 и приёмка B-3 по #981 (2 из 2 входов на двух ОС, отмена 1 из 1). Регистр ответов Administration API: в спецификации camelCase, живое API отвечает snake_case (IAM-2); код читает оба написания.

## 8. Отличия от черновика «Fable» 05.11 14:59 — приняты

- Продление роли — `PUT /api/v3/permissions/{id}`, а не новое разрешение: PUT есть в спецификации.
- Роль — по `roleIdentifier.roleName`; `GET /api/v1/roles` нужен только при коллизии имени: без `type` он перечисляет одни пользовательские роли.
- `GET /api/v1/actions` не вызывается: имена действий — константы таблицы до пробы.
- `userId` в `evaluate` не шлётся: по спецификации он только для сервисного аккаунта; спрашивает сам человек.
- Описание аккаунта и ключа — без хоста.
- Адрес входа — в stderr, а не в stdout: stdout держит один ответ.
- Страница согласия — с токеном R-47; `--yes` у `login` нет; терминал только собирает выбор («Риск» 24).
- Отказ называет шаг и HTTP-статус (#981); ошибка `evaluate` — не отказ (У-9).
- Истёкший ключ — `AUTH_FAILED` (вариант А), а не `AUTH_EXPIRED`.
