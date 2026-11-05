# Ручная проба входа через браузер (B-5, 1.8)

Хозяин — «Процесс · 1.8» (строка плана **B-5**, `release-plan.md`, блок
«Авторизация»). Вход в браузере cloud-e2e не автоматизирует: нужен человек.
Этот протокол проверяет до кода `evo login` (B-3) то, на чём B-3 стоит:
клиент `evo_cmcp` зарегистрирован, код обменивается на токен по PKCE, и
токен человека принимает Organization API. Делает владелец или догфудер —
тот, у кого есть учётная запись Cloud.ru с доступом к проекту.

Проба ничего не создаёт и ничего не стоит. Вывод о платформе — из двух
прогонов (CLAUDE.md §0); один прогон — гипотеза.

## Что нельзя записывать

Токены (access, ID, refresh), код авторизации, `code_verifier`, id
организации, проектов и пользователя, e-mail — ни в отчёт, ни в чат, ни в
issue. В отчёт идут только коды HTTP, имена полей, совпало / не совпало и
счётчики. Сырой вывод — только в закрытом `evo-internal`, `runs/`.

## Подготовка

- Значения входа — из таблицы констант A0-4 (`internal/auth`) и контракта
  B-1 (`scripts/api-contract.json`): issuer, `client_id` (`evo_cmcp`),
  три `redirect_uri` (loopback, порты 18386, 23386, 28386), `scope=openid`.
  Пока таблицы нет — у владельца; в отчёт значения не переписываются.
- Нужны `curl`, `jq`, `openssl`, `python3` и браузер на той же машине:
  `redirect_uri` — loopback, по SSH и в контейнере браузер его не увидит.

```bash
ISSUER='<issuer из таблицы A0-4>'
REDIRECT='<redirect_uri с портом 18386 из контракта B-1>'
umask 077; work=$(mktemp -d)
verifier=$(openssl rand -base64 48 | tr -d '=+/\n' | cut -c1-64)
challenge=$(printf %s "$verifier" | openssl dgst -sha256 -binary | openssl base64 -A | tr '+/' '-_' | tr -d '=')
state=$(openssl rand -hex 16); nonce=$(openssl rand -hex 16)
```

## Шаги

1. **Discovery.** `curl -sS "$ISSUER/.well-known/openid-configuration" > "$work/d.json"`;
   записать: код HTTP; `issuer` в ответе совпадает с `$ISSUER` (да / нет);
   есть ли `authorization_endpoint`, `token_endpoint`, `jwks_uri`;
   `code_challenge_methods_supported` содержит `S256` (да / нет / поля нет).
2. **Слушатель.** В отдельном терминале — приём одного запроса на
   127.0.0.1 (не 0.0.0.0), код и state — в файл, на экран — только имена
   параметров:
   ```bash
   python3 -c 'import http.server,urllib.parse as u,sys
   class H(http.server.BaseHTTPRequestHandler):
       def log_message(s, *a): pass  # the request line carries the code
       def do_GET(s):
           q=u.parse_qs(u.urlparse(s.path).query); open(sys.argv[1],"w").write(repr(q))
           s.send_response(200); s.end_headers(); s.wfile.write(b"ok, close the tab")
           print("params:",sorted(q))
   http.server.HTTPServer(("127.0.0.1",18386),H).handle_request()' "$work/cb"
   ```
3. **Авторизация.** Открыть в браузере
   `authorization_endpoint?response_type=code&client_id=evo_cmcp&scope=openid&redirect_uri=<REDIRECT, url-encoded>&state=$state&nonce=$nonce&code_challenge=$challenge&code_challenge_method=S256`
   и войти. Записать: страница входа открылась; после входа браузер
   пришёл на loopback; параметры — `code` и `state` (или `error` — его
   значение записать: это не секрет); `state` в ответе совпал с `$state`.
4. **Обмен кода.** `code` — из `$work/cb`.
   ```bash
   curl -sS -o "$work/t.json" -w '%{http_code}\n' -X POST "<token_endpoint>" \
     -d grant_type=authorization_code -d client_id=evo_cmcp -d "code=$code" \
     --data-urlencode "redirect_uri=$REDIRECT" -d "code_verifier=$verifier"
   jq '{keys: keys, expires_in, token_type}' "$work/t.json"
   ```
   Записать: код HTTP; имена полей; `expires_in` (ожидается 3600 — «access
   token человека — 1 час», со слов владельца 05.11). Повтор того же `code`
   — код HTTP и `error` (ожидается отказ: код одноразовый).
5. **ID token.** Только утверждения, без подписи и значений id:
   ```bash
   jq -r .id_token "$work/t.json" | cut -d. -f2 |
     python3 -c 'import sys,base64; p=sys.stdin.read().strip(); sys.stdout.buffer.write(base64.urlsafe_b64decode(p+"="*(-len(p)%4)))' |
     jq --arg n "$nonce" --arg i "$ISSUER" '{iss_ok: (.iss==$i), aud, nonce_ok: (.nonce==$n), ttl: (.exp-.iat), claims: keys}'
   ```
   Записать: `iss_ok`, `aud` (ожидается `evo_cmcp`; значение — не секрет),
   `nonce_ok`, `ttl`, имена утверждений.
6. **Organization API тем же access token.**
   ```bash
   tok=$(jq -r .access_token "$work/t.json")
   for p in customers projects; do
     curl -sS -o "$work/$p.json" -w "$p %{http_code}\n" -H "Authorization: Bearer $tok" \
       "https://organization.api.cloud.ru/v3/$p"
     jq -c '{keys: keys, n: ([.[]? | arrays | length] | add)}' "$work/$p.json" 2>/dev/null
   done
   ```
   Записать: коды HTTP и число элементов.
7. **Уборка.** `rm -rf "$work"`; `unset tok code verifier`. В облаке проба
   ничего не создала.

## Итоги и что дальше

| Итог шага 6 | Значит | Что дальше |
|---|---|---|
| 200 и 200 | токен человека принят Organization API | B-3 по контракту; строка B-5 — «проба входа N из 2» |
| 401 | токен не принят: вопрос про `aud` (ID token — `evo_cmcp`, чего ждёт API) | вопрос владельцу и IAM; B-3 стоит до ответа |
| 403 | токен принят, прав на чтение нет | сверить роль учётной записи; повтор под администратором организации |
| шаги 3–4 не прошли (`error`, нет перехода на loopback) | клиент или `redirect_uri` не те, что в контракте | вопрос владельцу: регистрация `evo_cmcp` |

Отчёт: `evo-internal/runs/login-probe-<дата>.md` — ОС и браузер, ответы
шагов 1–6 по списку «записать», без значений из раздела «Что нельзя
записывать». В план — одна строка итога «N из 2» под B-5.

## После B-3

- cloud-e2e — только по «да» владельца: удаление своего ключа своим токеном,
  Evaluate за свой аккаунт, ключ с `ttl` и `expiredAt` (строка B-5).

## Пункт догфуда С15 (после B-3; переедет в `dogfood-protocol.md`)

- **Цель:** человек без ключа доступа входит в evo через браузер и получает рабочий профиль; агенту — свой ключ, не ключ человека.
- **Где:** Linux, macOS и Windows (PowerShell) — по одному прохождению на каждой; на машине с браузером. По SSH и в контейнере loopback недоступен — проверить и записать, что говорит evo (ожидаемо: отказ с подсказкой про ключ из консоли, а не зависание).
- **Шаги:**
  1. Чистый каталог настроек (новый пользователь ОС или `CLOUDRU_CONFIG_DIR` на пустой каталог). `evo login` → открылся браузер, страница согласия evo на loopback называет организацию, проект, для кого ключ (я или агент) и срок (7/30/90 дней, умолчание 30), прямо: «ключ получит права администратора проекта <имя> на N дней».
  2. Согласиться для себя, 30 дней → `evo whoami` и `evo doctor` зелёные; профиль сохранён; `evo login` второй раз — аккаунт `evo` переиспользован, а не второй.
  3. Тот же вход для агента → ключ аккаунта `evo-agent`; `evo agent setup` предупреждает, что ключу разрешено удаление.
  4. Отмена на странице согласия; закрыть вкладку; Ctrl-C в терминале во время ожидания — evo заканчивает без ключа и без профиля, повтор входа работает.
  5. `evo logout` → ключ удалён в облаке (консоль: ключа нет), профиль удалён; повтор `evo logout` — не ошибка.
- **Успех:** шаги 1–5 на трёх ОС; секрет ключа не появился ни в выводе, ни в браузере (адрес, история), ни в журнале `evo diag`.
- **Дефект:** секрет или токен в выводе, истории браузера или журнале — **блокер**; второй аккаунт при повторном входе; ключ остался в облаке после отмены или `logout`; listener слушает не 127.0.0.1 (`netstat`/`ss` во время ожидания); на Windows браузер не открылся и evo не дал ссылку.
- **Цена:** 0 ₽. **Уборка:** `evo logout` для каждого профиля; в консоли — ключи и роли аккаунтов `evo` и `evo-agent` с истекающим сроком (аккаунты остаются по замыслу).
