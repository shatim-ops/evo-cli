# QA r4: сквозной прогон после rc.4

Дата: 2026-10-16. База: `main` @ `7d5346e` (rc.4 = `f0272b8` плюс PR #62, #63).
Четвёртый прогон; исправления по r1–r3 (PR #52–#63) считаются внесёнными,
остатки r3 из BACKLOG `[qa-r3]` не переоткрываются — только подтверждаются.

Метод:

- **Этап 1.** Установка опубликованного `v1.0.0-rc.4` скриптом `install.sh`
  из `main` тремя способами (без cosign, cosign v2.4.1, cosign v3.1.3) и
  ручная проверка подписи и sha256 по `docs/install.md`; сравнение бинаря
  релиза со сборкой из `main` (`GOTOOLCHAIN=go1.26.9 go build -o /tmp/evo ./cmd/evo`).
- **Этап 2.** Бинарь из `main` без изменений, против **поддельного облака**:
  TLS-сервер на `127.0.0.1:443`, имена `iam/compute/organization/*.api.cloud.ru`
  через `/etc/hosts`, свой CA через `SSL_CERT_FILE`, поддельный `sshd` на
  `127.0.0.1:22` (принимает любой ключ, отвечает на команды установки Docker,
  `cat >`, `docker compose up/logs`), «приложение» на `127.0.0.1:8089`.
  Маршруты и формы ответов — по `internal/provision/{adapter,discovery,types}.go`
  и тестам `internal/cli/credentials_test.go`, `internal/engine/timings_test.go`,
  `internal/deploy/ssh_server_test.go`. Неисправности включаются
  переключателями (ВМ не становится активной, `error_creating`, 422 на
  удаление, отказ SSH-ключа, падение `compose up`, 422 квоты FIP, потерянный
  ответ на создание ВМ, 400 на правило SG, задержки ответов, остановка
  сервера). Профиль, кэш токена и `HOME` — во временном каталоге;
  `HTTPS_PROXY` снят, ни один запрос в настоящий Cloud.ru не ушёл.
- **Этап 3.** Два запуска `cloud-e2e` на `main` через `workflow_dispatch`,
  по очереди: run #22 (зона по умолчанию ru.AZ-2, timeout 1200) и run #24
  (`zone=ru.AZ-1`, `timeout=1500`). Между ними владелец запускал #23 из
  другой ветки — он сюда не входит.
- Воспроизводящие тесты прогнаны через `go test -overlay` (дерево проекта
  не менялось, в код не добавлялись).

Серьёзность: **critical** — потеря денег/данных без следа; **high** — утечка
платного ресурса или секрета по реальному сценарию; **medium** — поведение,
противоречащее документации или решениям, или ловушка для нового коллеги;
**low/info** — гигиена, формулировки.

## Вердикт

**`v1.0.0` ставить можно** — после решения по Q3 (выбор группы безопасности)
и Q14 (диагностика красного `deploy`), желательно и Q1–Q2 (мелкие по коду,
но это обещания README и руководства). Блокеров уровня critical/high нет.

- **Critical / high: не найдено.** Полный цикл
  `configure → whoami → init → deploy --dry-run → deploy → verify → logs → destroy`
  проходит на поддельном облаке и в настоящем (ru.AZ-2, run #22) без утечек:
  после `destroy` ВМ, floating IP и правило порта удалены, state пуст; Ctrl-C на
  каждом шаге оставляет либо запись в state, либо ничего; подтверждения
  держатся в CLI (код 3, `--yes`, `--dry-run`) и в MCP (`confirmed=true`) по
  stdio и HTTP; `--allow-project` закрывает чужие и «свой» проект сервера.
- **Medium (4):** Q1 — `deploy --json` не печатает результат; Q2 — превью
  молчит про `build:`/`env_file:`/`volumes:`; Q3 — группа безопасности
  берётся первая попавшаяся в зоне (в тестовом проекте это группа другого
  сервиса, и в неё открывается порт 80 для `0.0.0.0/0`); Q14 — красный
  `deploy` в облаке (run #24, ru.AZ-1) не оставляет ни вывода установки
  Docker, ни причины ошибки: диагностировать нечем.
- **Облако:** ru.AZ-2 — зелёный (run #22, 11 мин 38 с); ru.AZ-1 — красный
  (run #24): ВМ и SSH поднялись быстро, Docker не поставился за 10 мин —
  сеть первой загрузки, как в BACKLOG `[e2e]`; `destroy` всё убрал, сирот
  нет. Выбор ru.AZ-2 по умолчанию (PR #63) подтверждён.
- Остатки `[qa-r3]`, на которые наткнулся: подтверждены R14, C4, C5, C6,
  C7, C12, I2; опровергнутых нет.

### Блокеры

Critical/high — нет. Medium — Q1, Q2, Q3, Q14 в таблице находок.

## Этап 1. Установка глазами коллеги

| Способ | Команда | Результат |
|---|---|---|
| без cosign | `curl … install.sh \| CLOUDRU_VM_VERSION=v1.0.0-rc.4 INSTALL_DIR=… sh` | rc 0; «cosign не найден: подпись не проверена, только sha256», «sha256: OK», установлены `evo` и alias `cloudru-vm`, подсказка про PATH, `evo 1.0.0-rc.4`, коммит `f0272b8`, go1.26.9 |
| cosign v2.4.1 (`go install …/cosign/v2/cmd/cosign@v2.4.1`) | то же | rc 0; «Подпись checksums.txt: OK (cosign)» — скрипт сам повторил проверку с `--new-bundle-format` (PR #62 работает) |
| cosign v3.1.3 (`…/cosign/v3/cmd/cosign@latest`) | то же | rc 0; «Подпись checksums.txt: OK (cosign)» без флага |
| без версии | `curl … install.sh \| sh` | «стабильного релиза пока нет; задайте CLOUDRU_VM_VERSION, например v1.0.0-rc.1» — ожидаемо до v1.0.0 (пример устарел, Q9) |
| cosign, который отказывает (подделка в PATH) | то же, rc.4 | «cosign: Error: fake refusal», «подпись checksums.txt не прошла проверку cosign (нужен cosign v2.4 или новее)», бинарь не установлен — верно |

Ручная проверка по `docs/install.md` (раздел «Проверка подписи»):

| Проверка | cosign v2.4.1 | cosign v3.1.3 |
|---|---|---|
| `verify-blob --bundle checksums.txt.sigstore.json …` без флага | `bundle does not contain cert for verification` — как и написано в docs | `Verified OK` |
| то же с `--new-bundle-format` | `Verified OK` | — |
| подпись архива `evo_1.0.0-rc.4_linux_amd64.tar.gz.sigstore.json` | `Verified OK` | — |
| чужой identity (`shatim-ops/cloudru-vm-cli`) | отказ: `no matching CertificateIdentity` | — |
| `sha256sum --ignore-missing -c checksums.txt` | `OK` | |

Архив: `evo`, `cloudru-vm`, LICENSE, NOTICE, README.md, `docs/{auth,commands,exit-codes,install,mcp,troubleshooting}.md`.

**Релиз rc.4 и сборка из `main`.** Список инструментов (147, из них 64
изменяющих) и корневая справка совпадают. Различия — только PR #63:
`evo init --help` и `--non-interactive` описывают ru.AZ-2 как зону по
умолчанию; `docs/commands.md` в `main` актуален (`TestCommandsDoc` зелёный).
Поведение `init` проверено на фейке (ниже). PR #62 — только `install.sh` и
документация, проверен установкой выше. Сборка из `main` печатает
`evo dev / коммит: none / собран: unknown` — Q4.

## Этап 2. Сквозной сценарий по руководству (поддельное облако)

### Таблица находок

| id | серьёзность | файл:строка | путь к дефекту (сценарий → последствие) | воспроизведение | исправление |
|---|---|---|---|---|---|
| Q1 | medium | `internal/engine/deploy.go:255-294` (успешный `Deploy` печатает только `Printer.Success`; `Printer.JSON` нет) | README («у каждой команды есть `--json`») и `docs/exit-codes.md` («`evo deploy --yes --json > deploy.json`») обещают JSON-результат. На деле успешный `deploy --yes --json` не пишет в stdout **ничего**: единственная JSON-строка — `{"status":"confirmed","preview":…}` в stderr. Повторный `deploy --yes --json` при неизменном compose — пустой stdout и stderr, rc 0. `destroy --yes --json` при этом печатает `{"status":"destroyed",…}`. Скрипт из docs получает пустой `deploy.json` | `evo deploy --yes --json >out.json 2>err.json; wc -c out.json` → `0`; тест T-J1 (падает) | В конце `Deploy` при `IsJSON()` печатать запись (`rec`) и результат проверки, как `status --json`; при «нет изменений» — `{"status":"unchanged","vm_id":…}` |
| Q2 | medium | `internal/compose/parse.go:90-130` (`Parse` читает только `image`, `ports`, `deploy.resources`; предупреждения — лишь про `${VAR}`) | Руководство §5: «На ВМ попадает только compose-файл… `build:`, файл `.env` и локальные файлы через `volumes:` не переносятся». Compose с `build: .`, `env_file: .env`, `volumes: ./data:/data` проходит `deploy --dry-run` и превью **без единого предупреждения**; `deploy --yes` создаёт платную ВМ, и только `docker compose up` на ней падает (нет Dockerfile/`.env`); запись остаётся `deploying`, ВМ тарифицируется, пока коллега разбирается. BACKLOG `[audit]` держит это как фичу (перенос файлов), но предупреждение в превью — дёшево | фейк: `deploy --dry-run` с таким compose → превью без предупреждений (вывод в разделе «Compose»); тест T-C1 (падает: `предупреждения: []`) | В `Parse` добавлять в `Warnings` сервисы с `build:`, `env_file:`, `volumes:` с относительным путём, `secrets:`/`configs:` файлами; показывать в превью `deploy` |
| Q3 | medium | `internal/engine/network.go:31-67` (`resolveNetwork`), `:73-91` (`pickInZone`: первая группа с совпавшим id зоны) | В проекте с несколькими группами безопасности в зоне `deploy` берёт **первую по порядку API**, а не `Default_<зона>` и не группу, которую коллега выбрал бы сам. В тестовом проекте cloud-e2e (runs #22 и #24) превью показало группу другого сервиса, не `Default_ru.AZ-2`, и `deploy` открыл в ней `80` для `0.0.0.0/0`: правило попало в группу, где живут чужие ВМ (превью честно пишет «она общая для всех ВМ в ней», но выбор — случайность порядка). Повторный `deploy` после пересортировки списка API возьмёт другую группу: ВМ и правила разъедутся. `.evo.yaml` не даёт задать группу | лог run #22, шаг `deploy --dry-run`: «группа безопасности <не Default_ru.AZ-2>»; на фейке с одной группой в зоне — `Default_ru.AZ-2` | Предпочитать группу с именем `Default_<зона>` / `default`, затем единственную; при нескольких кандидатах — предупреждение с именами и ключ `security_group:` в `.evo.yaml` (`evo init` может спросить). Записывать выбор в state уже делается (`security_group_id`) |
| Q4 | low (docs) | `internal/cli/main.go:65-66` (`Main` присваивает `version, commit, date = buildVersion…` **после** `init()` в `internal/cli/version.go:16-19`, который уже заполнил их из `debug.ReadBuildInfo`) | `docs/install.md:180`: «`evo version` у такой сборки показывает версию модуля и коммит из данных VCS». Сборка `go build -o bin/evo ./cmd/evo` из checkout печатает `evo dev / коммит: none / собран: unknown` и то же отдаёт MCP `serverInfo.version`, хотя `go version -m` видит `vcs.revision=7d5346e…`, `vcs.time`, pseudo-версию модуля. В обращении в поддержку (`evo version --json`) коммит теряется. Релиз через goreleaser (`-X main.version`) не затронут | `go build -o /tmp/evo ./cmd/evo && /tmp/evo version` → `dev/none/unknown`; `go version -m /tmp/evo \| grep vcs` → есть; тест T-V1 (падает) | В `Main` вызывать `resolveVersion(buildVersion, buildCommit, buildDate, info)` вместо голого присваивания (или убрать `init`) |
| Q5 | low | `internal/state/state.go:48, 62-63`; `internal/state/lock.go:38-39` | Ошибки `STATE_CORRUPTED` («state file contains invalid JSON», «has version 99; this evo understands up to 1 / Upgrade evo…») и `STATE_LOCKED` («another evo deploy or destroy is running… Wait for it to finish; the lock (…) is released…») — по-английски в русском CLI (D6, пересмотр). Это те ошибки, которые коллега увидит при двух `deploy` подряд в одном каталоге | `echo '{' > .evo/state.json; evo status` ; два `evo deploy --yes` параллельно | `i18n.T` в `internal/state` (пакет без i18n вообще) |
| Q6 | low | `internal/engine/deploy.go:678-682` (ветка `VMFailed` возвращает ошибку, не обновляя `rec.Status`) | ВМ уходит в `error_creating`: `deploy` корректно падает с `VM_PROVISION_FAILED` (rc 1), но запись остаётся `creating`; `evo status` показывает «Состояние creating», хотя ВМ в ошибке. `destroy` её удаляет (проверено) | фейк `vm_fail=error_creating`: `state: [('vm-19','creating',…)]`, фейк: `error_creating` | Писать `rec.Status = "error"` (или фактический статус) и `state.Write` перед `return` |
| Q7 | low | `internal/engine/deploy.go:199-206` | Любая ошибка `waitForSSH`, включая `SSH_AUTH_FAILED` и `SSH_HOST_KEY_CHANGED` (они не повторяются), пишется в state как `ssh_timeout` | фейк `ssh_auth=deny`: ошибка «аутентификация SSH не прошла», `state: ssh_timeout` | Статус по коду ошибки (`ssh_auth_failed`, `ssh_host_key_changed`) |
| Q8 | low | `internal/verify/verify.go:15-25` (`ProbeResult` без json-тегов) | `evo verify --json` — единственный вывод с ключами `Port`, `StatusCode`, `Healthy`, `Latency` (наносекунды, число), `Error`, `Protocol`; остальной JSON CLI — snake_case (`vm_id`, `token_ttl_seconds`). `docs/exit-codes.md` называет ключи JSON частью контракта — менять лучше до v1.0.0 | `evo verify --json` → `[{"Port":8089,"StatusCode":200,"Healthy":true,"Latency":1067919,…}]` | json-теги `port`, `status_code`, `healthy`, `latency_ms`, `error`, `protocol` (MCP `verify`-инструмента нет, совместимость не ломается) |
| Q9 | info | `install.sh:9, 78` | Сообщение «стабильного релиза пока нет; задайте CLOUDRU_VM_VERSION, например v1.0.0-rc.1» и шапка скрипта советуют rc.1 (бинарь `cloudru-vm`), README/guide — rc.4 | `curl … install.sh \| sh` | `v1.0.0-rc.4` или без примера |
| Q10 | info | `internal/cli/tools.go` (`toolError`, ошибка без подсказки) | `evo tools call dns_zones --json` при ошибке API → stderr `{"error":{…,"hint":""}}` — пустая подсказка вместо отсутствующего поля | фейк 404 на `/v1/publicZones` | `omitempty` у `hint` |
| Q11 | low | `internal/cli/configure.go:326-336` (`chooseProject`, неинтерактивная ветка «проект остаётся прежним») | `evo configure` без терминала и без `--key-id`/`--secret-stdin` при уже существующем профиле — rc 0, «Профиль default сохранён», ключ не менялся. Без профиля та же команда честно отвечает «нужны Key ID и секрет» (rc 2). В CI это маскирует ошибку: скрипт «настроил» профиль, не передав ключ | `evo configure </dev/null` в каталоге с профилем → rc 0 | Без терминала требовать `--key-id` и `--secret-stdin` всегда |
| Q12 | info | `internal/config/config.go:233-238` (частичное окружение заменяет профиль целиком — по дизайну, комментарий там же) | `CLOUDRU_SECRET=wrong evo whoami` при настроенном профиле → «не задан ключ доступа Cloud.ru» (AUTH_REQUIRED). Коллега с «забытой» переменной из `.env` не поймёт, почему профиль не работает; `troubleshooting.md` это описывает только для AUTH_FAILED | см. команду | В подсказке называть, какая из `CLOUDRU_KEY_ID`/`CLOUDRU_SECRET` задана, а какая нет |
| Q13 | low (UX, облако) | `internal/engine/deploy.go:1004-1030` (`waitForSSH`: до 60×5 с без вывода) | В run #22 между «[4/5] Развёртывание приложения…» и «Подготовка Docker…» прошло **3 мин 49 с** тишины (ВМ активна, sshd ещё не поднят). `deploy` при ожидании ВМ раз в минуту пишет состояние (PR #61), при ожидании SSH — нет; коллега не отличит «ждём sshd» от зависания | лог run #22: 04:34:14 → 04:38:03 без строк | Раз в минуту: «ожидание SSH на <ip>:22 (прошло …)» |
| Q14 | medium (диагностика) | `internal/engine/deploy.go:1047-1113` (`prepareDocker`: хвост вывода в подсказку только если он не пуст; `lastErr` уходит в `clerrors.Wrap` и теряется), `internal/cli/main.go:424-452` (`handleError` печатает `Message` и `Hint`, причину — никогда, `--verbose` не влияет) | Живой red-прогон #24: 11 попыток установки Docker по ~52 с, затем «Docker на ВМ … не готов за 10m0s» и подсказка про «Temporary failure in name resolution выше» — но «выше» ничего нет: вывод скрипта пуст, а что именно вернула попытка (код выхода `sudo -n sh`, обрыв SSH-сессии, таймаут), не напечатано. Коллега не отличит «нет DNS» от «sudo просит пароль» или «sshd рвёт сессии» и не сможет приложить к обращению ничего, кроме гипотезы | лог run #24, шаг `deploy --yes` (05:06:59–05:17:02) | При пустом выводе включать в подсказку `lastErr` (`ssh: exit status N`, `wait: remote command exited without exit status` и т. п.) и длительность попытки; `--verbose` печатать цепочку причин (`errors.Unwrap`) у любой ошибки; в журнал попытки писать код выхода скрипта

### Подтверждённые остатки `[qa-r3]` (не переоткрываю)

| id r3 | Что увидел |
|---|---|
| R14 | compose с новым портом `8090` + `evo deploy` **без терминала и без `--yes`** → rc 0, порт открыт для `0.0.0.0/0` без вопроса (превью обновления — только с `--dry-run`) |
| C4 | `evo disk delete 1b2c </dev/null` → превью с английским «disk_delete permanently destroys disk… Approve, then confirmed=true», rc 3 (как в README) |
| C5 | `evo help nosuch` → `Unknown help topic`, rc 0; `evo completion nosuch` → rc 0 |
| C6 | `whoami`: «источник: profile» / «project»; превью: «из .evo.yaml»; JSON `project_from: "project"` для `.evo.yaml` |
| C7 | `EVO_LANG=en cloudru-vm version` — русское уведомление об alias (у релиза rc.4 тоже) |
| C12 | `--auth none` на `:18090` → rc 1 (USAGE), без `MCP_BEARER_TOKEN` → rc 2 |
| I2 | `evo tools call release_floating_ip --arg id=fip-nope --yes` → JSON с «floating IP "fip-nope" not found: VM_NOT_FOUND: ресурс не найден: GET …» — два языка в одной строке |
| R10 | `destroy` CLI: `DELETE /api/v1/vms/{id}` уходит без `project_id` (FIP и правила — с ним); на фейке и вживую работает |

### Проверено и в порядке

**Установка и бинарь.** См. этап 1. Alias `cloudru-vm` печатает уведомление и работает как `evo`. `evo version --json` релиза — версия, коммит, дата.

**Первый запуск.** `configure --key-id … --secret-stdin --project-id …` → профиль, файлы `0600`, каталог `0700`; `configure` в терминале (pty) спрашивает Key ID, секрет (скрыт), после 400 от Organization API — UUID проекта вручную, как и обещает guide §3; `configure --profile test`, `configure list`, `--profile`/`CLOUDRU_PROFILE`, `configure delete test` (спрашивает, с `--yes` удаляет); `whoami` (текст и `--json`: `token_source: iam|cache`, `credentials_from`, `project_from`); `doctor` (rc 0; без ключа — rc 2; битый YAML — rc 2 с указанием файла и строки; сеть недоступна — rc 1, обе пробы `недоступен: … connection refused`); права `0644` у `credentials.yaml` → предупреждение (с `--json` — строкой `{"warning":…}` в stderr); устаревший `CLOUDRU_API_KEY` → предупреждение и работа.

**Чтения (guide §4).** `vm list` (пусто/`--json {"vms":[]}`), `list-zones`, `list-images`, `vm flavors` (таблица с примечанием про оценку цены), `vm get nope` → rc 4, `project list` для ключа без списка проектов → rc 0, активный проект и пояснение (JSON `projects: null`, `note`); `tools list` (147), `tools describe dns_zones`, `tools call vm_get` без аргументов → USAGE rc 1, неизвестный инструмент → rc 1. Команды к другим сервисам (`vpc`, `k8s`, `pg`, `dns_zones`) на фейке отвечают 404 → rc 5 `API_ERROR` с текстом ответа; `s3 buckets` без ключей S3 → rc 2 CONFIG_INVALID (D11).

**`evo init` (PR #63).** Без терминала (`</dev/null`) и с `--non-interactive`: ru.AZ-2 (`az-2-id`) и Ubuntu-24.04, `timeout: 600`, `project_id` из профиля; в меню (pty) ru.AZ-2 первая, `2` выбирает ru.AZ-1, Enter — ru.AZ-2, `9` → «недопустимый выбор "9"… от 1 до 3», файл не создаётся; без ru.AZ-2 в API → порядок API (ru.AZ-1); `EVO_LANG=en` — английский шаблон; повторный `init` — «уже существует, файл не изменён», rc 0.

**Цикл deploy (guide §5, §6).** `deploy --dry-run` → превью с проектом и источником, flavor, зоной, образом, диском, подсетью, группой, портами, ценой-оценкой; rc 0, `.evo/` **не создаётся** (ключ не генерируется); `--dry-run --json` → `{"status":"dry_run","preview":…}`. Без терминала и без `--yes` → превью и rc 3; с `--json` — `{"status":"confirmation_required",…}` в stdout, ошибка в stderr. `deploy --yes`: FIP создаётся до ВМ и привязывается при создании (`attach_external_ip_id`), запись в state сразу после `POST /api/v1.1/vms` (статус `creating`, `floating_ip_id`, `security_group_id`), ожидание active, открытие портов с записью id правил, SSH (ключ хоста TOFU, отпечаток в state), установка Docker одним скриптом, `cat >` compose-файла, `docker compose up -d`, проверка `:8089 -> 200`; `.evo/` с `.gitignore`, `id_ed25519` 0600. `status`/`status --json`, `verify`/`verify --json`, `logs`, `logs -s web -n 1` (на ВМ: `docker compose logs --tail 1 'web'`), `logs -f` + Ctrl-C → rc 130 («прервано», с `--json` — `{"error":{"code":130,"reason":"INTERRUPTED"}}`). Повторный `deploy` без изменений → «Compose-файл не изменился», rc 0; `--force` → повторная загрузка и `up`; изменённый compose → `--dry-run` показывает превью обновления («новые ресурсы не создаются»), `deploy` обновляет ту же ВМ. `destroy --dry-run` перечисляет ВМ, FIP, правила, запись; без терминала — rc 3 (`--json`: превью в stdout); `destroy --yes`: `DELETE vm` → ожидание исчезновения → `DELETE floating-ips/{id}?project_id=…` → `DELETE …/rules/{id}`; state пуст, фейк пуст; повторный `destroy` → «нечего удалять», rc 0 (`--json {"status":"none"}`); после destroy `status` rc 0, `verify`/`logs` rc 4 (D11).

**Несколько проектов (guide §7).** `.evo.yaml` с чужим проектом: предупреждение «проект … из .evo.yaml заменяет проект … профиля» у всех команд (с `--json` — JSON-строкой); `deploy` на существующую запись из другого проекта → rc 2 CONFIG_INVALID с подсказкой `--project-id`; `deploy --project-id <проект записи>` работает; `destroy` работает в проекте записи («где создано развёртывание», `project_id` в URL FIP); `CLOUDRU_PROJECT_ID` — то же предупреждение; `--project-id` у `whoami` — без предупреждения (S2 из r3 закрыт).

**Негатив и Ctrl-C** (фейк, все сценарии):

| Сценарий | Результат |
|---|---|
| Ctrl-C на превью `deploy` (pty) | «прервано», rc 130, ничего не создано |
| Ctrl-C пока ВМ `creating`, без терминала | rc 130, «Получен interrupt. ВМ … продолжает работать (режим без вопросов)», запись `creating` с FIP; `status` показывает её; `destroy --yes` удаляет ВМ и FIP |
| Ctrl-C пока ВМ `creating`, pty, «y» | вопрос «Удалить ВМ …?», «ВМ удалена», «Floating IP … освобождён», rc 130, state пуст, фейк пуст |
| Ctrl-C пока ВМ `creating`, pty, Enter | «ВМ … продолжает работать», запись остаётся, `destroy --yes` убирает |
| Ctrl-C при ожидании SSH (sshd не принимает) | rc 130, «Прервано. ВМ … продолжает работать», запись `ssh_timeout` с FIP и правилом; повторный `deploy --yes` доводит до `running` |
| Ctrl-C при установке Docker (SSH-команды по 30 с) | rc 130, то же сообщение, запись `deploying`; `destroy --yes` чистит всё |
| Ctrl-C при `destroy` (compute отвечает через 20 с) | rc 130 «прервано: запрос отменён», запись возвращается в `running`; повторный `destroy --yes` удаляет ВМ, FIP, правило |
| SSH-ключ отвергнут | rc 1 `SSH_AUTH_FAILED` без 60 повторов (статус в state — Q7) |
| ВМ → `error_creating` | rc 1 «ВМ … перешла в состояние error_creating» (ждёт только один опрос, PR #61); `destroy` удаляет ВМ и FIP (статус записи — Q6) |
| `DELETE vm` дважды 422 | «пока нельзя удалить… ждём до 10m0s», затем удалена; FIP и правило удалены |
| `docker compose up` падает | rc 1 «docker compose up завершился ошибкой», запись `deploying`; повторный `deploy --yes` без `--force` повторяет и доводит до `running` |
| квота FIP (422 на создание) | rc 1 «квота floating IP исчерпана… Ничего не создано», state нет, фейк пуст |
| ответ на `POST vms` потерян | rc 0: ВМ найдена по имени и времени (`FindCreatedVM`), записана, развёрнута; `destroy` чистит |
| второй `deploy`/`destroy` в том же каталоге | rc 1 `STATE_LOCKED` (`--json` — `reason: STATE_LOCKED`); первый завершается нормально |
| 400 на правило SG | предупреждение «Не удалось открыть порты 8089 — откройте их… в консоли», deploy завершён, `opened_rules` пуст, `destroy` чист |
| нет ключа | rc 2 `AUTH_REQUIRED` (текст, `--json`, деревья `vm list`) |
| неверный секрет | rc 2 `AUTH_FAILED` «IAM отклонил ключ доступа (статус 401)» |
| битый `credentials.yaml` / нет профиля `test` | rc 2 `CONFIG_INVALID` с файлом и подсказкой |
| нет compose / YAML с ошибкой / `${PORT}` без значения | rc 1 `COMPOSE_NOT_FOUND` / `COMPOSE_INVALID` / предупреждение «порт не открывается» |
| битый state / state новее | rc 1 `STATE_CORRUPTED` (английский — Q5) |
| сеть недоступна (сервер остановлен) | `whoami` с живым кэшем токена — rc 0 (по docs); без кэша — rc 1 `API_UNREACHABLE` за 1,5 с (3 попытки IAM); `vm list`, `list-zones`, `deploy --dry-run` — rc 1 `API_UNREACHABLE` за 7 с (повторы 1+2+4 с); `doctor` — rc 1, обе пробы «недоступен» |
| `EVO_LANG=en` | `whoami`, ошибки, `init`, `deploy` — английские; `EVO_LANG=xx` → предупреждение и русский |
| секреты в выводе | `qa-secret-0001` не встречается в `doctor --json --verbose`, `whoami --verbose`, логах MCP |

**MCP по stdio.** `initialize` (2025-06-18 и 2024-11-05 — сервер отвечает запрошенной), `serverInfo {evo, dev}`, `tools/list` = 147 = `evo tools list`; `vm_list` → `{"vms":[]}`; `deploy`, `destroy`, `release_floating_ip` без `confirmed` → `isError: true`, `status: confirmation_required`, поле `approval`; с `confirmed=true` вызов уходит в API (404 → `reason: VM_NOT_FOUND`, `hint`); `s3_bucket_create` без имени → USAGE; неизвестный инструмент → `unknown tool`; ответы английские при `EVO_LANG=ru`. **Полный цикл через MCP:** `deploy confirmed=true` → `deployment` со статусом `running`, `status`, `logs tail=2`, `destroy` → превью, `destroy confirmed=true` → лог «VM deleted / Floating IP released / Closed ports 8089 / Deployment destroyed», `status` → `none`; state и фейк пусты. (`verify` — не инструмент MCP; guide это не обещает.)

**MCP по HTTP.** Без `MCP_BEARER_TOKEN` → rc 2 с подсказкой; `--auth none` не на loopback → отказ; без `--allow-project` — WARN в журнале; `/healthz` 200 без токена; `POST /mcp` без токена → 401; чужой `Origin` → 403; `initialize` → `Mcp-Session-Id`; `tools/list` 147; `deploy` без `confirmed` → превью; `compose_file=/etc/passwd` → «outside the server's working directory»; `secret_value` → отключён без `--allow-secret-export`; батч из `ping` + уведомление + вызов → массив ответов без ответа на уведомление; `DELETE /mcp` без сессии → 400; в журнале — имя, статус, длительность, без аргументов. **`--allow-project 2222…`:** `deploy` в проекте сервера (`1111…`) → `CONFIG_INVALID: project … is not allowed on this server` с подсказкой; `deploy project_id=2222…` → превью; `project_id=3333…` → отказ; `vm_list`, `list_zones`, `destroy vm_id` в проекте сервера → отказ (docs/mcp.md: «включая проект самого сервера» — верно).

**Коды выхода (docs/exit-codes.md).** 0/1/2/3/4/5/130 — все встречены в сценариях выше и совпадают с таблицей; `--json` ошибка — один объект в stderr с `code`, `reason`, `message`, `hint`; предупреждения — отдельными JSON-строками перед ошибкой.

### Compose без предупреждений (к Q2)

```
$ cat docker-compose.yml
services:
  web:
    image: nginx
    build: .
    env_file: .env
    volumes:
      - ./data:/data
    ports:
      - "${PORT:-8089}:80"
      - "5353:53/udp"
  db:
    image: postgres:16
    ports:
      - "5432:5432"
$ evo deploy --dry-run
==> [1/5] Аутентификация...
    Токен IAM действителен
    Подобран flavor: lowcost10-2-4 (2 vCPU, 4 ГБ RAM)
==> [2/5] Разбор compose-файла...
    сервисов: 2, открытых портов: 3, хеш=eb88d53e5b47
==> [3/5] Создание ВМ...
Пробный запуск, ничего не меняется. План: создать ВМ evo-app4 (lowcost10-2-4) в зоне ru.AZ-2
  …
  порты:      5432,8089,5353/udp открываются для 0.0.0.0/0 в группе безопасности Default_ru.AZ-2 …
  цена:       ~2114.00 RUB в месяц …
rc=0
```

## Этап 3. Живой e2e в облаке (`cloud-e2e`, `main` @ `7d5346e`)

| run | зона / timeout | configure+whoami | init | dry-run | `deploy --yes` всего | ВМ active | FIP+порты | ожидание SSH | Docker готов | compose up | verify | `logs -f` 150 с | destroy | сироты | правило 80 | итог |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| #22 | ru.AZ-2 (по умолчанию) / 1200 | 4 с | 8 с | 5 с (диск 10→11 ГБ по min образа) | **7 м 57 с** | 2 м 42 с (`creating` отчёты через 5 с, 1м10с, 2м15с) | 1 с | 3 м 49 с (тишина, Q13) | 1 м 12 с (1 повтор «Docker ещё не готов») | 7 с | `:80 -> 200` (359 мс); `verify` 200/366 мс; curl снаружи — nginx | rc **130** | 21 с (ВМ 20,6 с, FIP 0,5 с, правило 0,4 с) | «нет» (4 с) | удалено | **зелёный**, job 11 м 38 с |
| #24 | ru.AZ-1 / 1500 | 4 с | 8 с | 4 с | **14 м 27 с, rc 1** | 2 м 32 с (отчёты 5 с, 1м6с, 2м10с) | <1 с (порт 80 уже был открыт в группе — строки «Открыты порты» нет, `opened_rules: null`) | 1 м 47 с | **не готов за 10m0s**: 11 попыток по ~52 с, вывода установки нет (Q14) | — | пропущен | пропущен | 28 с (ВМ 28 с, FIP 0,5 с) | «нет» (4 с) | не было «evo:»-правила ни до, ни после — проверка зелёная | **красный**, job 15 м 46 с |

Разбор #24 по логу (отделяю облако от кода). **Облако:** в ru.AZ-1 ВМ
создалась за 2 м 32 с (быстрее, чем вечером 15.10), sshd поднялся через
1 м 47 с, но скрипт установки Docker (`internal/bootstrap/install.sh` через
`sudo -n sh`) 11 раз подряд завершался ошибкой примерно через 52 с каждый,
пока не истёк `DockerWaitTimeout` 10 мин; `destroy` затем удалил ВМ и FIP за
28 с, сирот нет. Это та же картина «первая загрузка без сети», что в BACKLOG
`[e2e]` для ru.AZ-1 (apt-зеркало / get.docker.com недоступны), и не код
`evo`: тот же бинарь в ru.AZ-2 (#22) прошёл весь сценарий. **Код:** `evo` не
дал ни одного факта для диагноза — ни вывода установки («Последние строки
установки Docker» не напечатаны, выход скрипта был пуст), ни причины
ошибки (`lastErr` в `clerrors.Wrap` никогда не печатается, `--verbose` на
это не влияет), только гипотезу про DNS в подсказке; это Q14. Группа
безопасности в ru.AZ-1 — `SSH-access_ru.AZ-1` (совпала по имени зоны), порт
80 в ней уже был открыт чужим правилом, `deploy` об этом промолчал
(`AlreadyOpen` не печатается) — и после `destroy` чужое правило осталось,
как и задумано.

Разбор #22 по логу: `evo init --non-interactive` выбрал ru.AZ-2 и Ubuntu-24.04
(PR #63 вживую), workflow затем подставил `ubuntu-22.04` и `lowcost10-2-4`;
`deploy --dry-run` не создал state; в превью — группа безопасности **не**
`Default_ru.AZ-2`, а группа другого сервиса проекта (Q3); порт 80 открыт в ней
и после `destroy` удалён (шаг «Правило порта 80 удалено» зелёный); `destroy`
удалил ВМ и FIP, «Сирот нет». Состояние зоны: ru.AZ-2 за ночь не изменилась —
ВМ активна за 2 м 42 с, SSH поднялся через ~4 мин после этого, Docker ещё
~1 мин — это сеть первой загрузки ВМ, не код.

## Руководство для коллег: где расходится с поведением

| Место | Написано | На деле |
|---|---|---|
| `docs/install.md:180` | `evo version` у сборки из исходников показывает версию модуля и коммит из VCS | `dev / none / unknown` (Q4) |
| README «Быстрый старт», `docs/exit-codes.md` «В скриптах» | `evo deploy --yes --json > deploy.json` | файл пустой; результат только у `status --json` (Q1) |
| `docs/guide.md` §5 «Что важно знать» | `build:`, `.env`, локальные `volumes:` на ВМ не переносятся | верно, но `deploy --dry-run` об этом не предупреждает (Q2) |
| `docs/guide.md` §5, `troubleshooting.md` «Порты в группе безопасности» | правило ставится «в общей группе безопасности зоны» | в группе, которую `deploy` выбрал первой в зоне — в тестовом проекте это группа другого сервиса; какой именно, видно только в превью (Q3) |
| `docs/guide.md` §3 | `evo configure` спросит Key ID и Key Secret | в терминале — да; без терминала при существующем профиле — молча пересохраняет (Q11) |
| `install.sh` (сообщение без версии) | «например v1.0.0-rc.1» | guide/README — rc.4 (Q9) |
| `docs/guide.md` §4, `docs/mcp.md` | «~150 инструментов» | 147 — приемлемо |
| `docs/guide.md` §8 | «Агент получает те же инструменты, что `evo tools list`» | верно; `verify` в MCP нет — guide этого и не обещает |
| `docs/troubleshooting.md` «SSH и deploy»: «`Docker is not ready` с `Temporary failure in name resolution` в выводе» | вывод установки показывается | в run #24 вывода нет вовсе — только подсказка (Q14) |
| `docs/troubleshooting.md` «Прерванный deploy» | «команда спросит, удалить ли её (в терминале), или напишет её ID и команду для удаления» | верно для всех шагов (таблица негатива) |
| `docs/mcp.md` «Проекты» | вызов в проекте сервера, если его нет в `--allow-project`, отклоняется | верно, включая чтения `vm_list`/`list_zones` |

Остальные шаги руководства (§2–§7, §9 ограничения, шпаргалка) совпадают с
поведением бинаря из `main`.

## Воспроизводящие тесты

Прогнаны через `go test -overlay` без изменения дерева; все три **падают** на
`main` @ `7d5346e`. В код не добавлены.

### T-J1 — `internal/engine/qa_r4_json_test.go` (Q1)

```go
package engine

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/output"
)

// README и docs/exit-codes.md: «evo deploy --yes --json > deploy.json».
// Успешный deploy в режиме JSON должен напечатать объект с результатом.
func TestDeploy_JSONPrintsResultObject(t *testing.T) {
	cloud := &mockCloudAPI{}
	dep := newMockDeployer()
	eng, tmpDir := newTestEngine(t, cloud, dep)
	var out bytes.Buffer
	eng.Printer = output.NewWithWriter(&out, output.ModeJSON, false)

	composePath := writeComposeFile(t, tmpDir)
	err := eng.Deploy(context.Background(), DeployOptions{ComposeFile: composePath, Flavor: "small",
		Region: "ru-moscow-1", NonInteractive: true, Yes: true})
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if !strings.Contains(out.String(), `"vm_id"`) {
		t.Fatalf("deploy --json: stdout без результата развёртывания; получено %q", out.String())
	}
}
```

Результат: `FAIL … получено "{\"status\":\"confirmed\",\"preview\":…}\n"` —
в stdout только превью подтверждения.

### T-C1 — `internal/compose/qa_r4_warnings_test.go` (Q2)

```go
package compose

import (
	"strings"
	"testing"
)

// guide §5: build:, env_file: и локальные volumes на ВМ не попадают —
// Parse должен предупреждать, чтобы превью deploy это показало.
func TestParse_WarnsAboutLocalOnlyKeys(t *testing.T) {
	path := writeTempCompose(t, `
services:
  web:
    image: nginx
    build: .
    env_file: .env
    volumes:
      - ./data:/data
    ports:
      - "8080:80"
`)
	proj, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(proj.Warnings, "\n")
	for _, key := range []string{"build", "env_file", "volumes"} {
		if !strings.Contains(all, key) {
			t.Errorf("нет предупреждения про %s; предупреждения: %q", key, proj.Warnings)
		}
	}
}
```

Результат: `нет предупреждения про build/env_file/volumes; предупреждения: []`.

### T-V1 — `internal/cli/qa_r4_version_test.go` (Q4)

```go
package cli

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
)

// docs/install.md: сборка из checkout показывает коммит из данных VCS.
// init() в version.go его читает, Main() затем перезаписывает "none".
func TestBuiltBinaryReportsVCSCommit(t *testing.T) {
	if testing.Short() {
		t.Skip("собирает бинарь")
	}
	bin := filepath.Join(t.TempDir(), "evo")
	if out, err := exec.Command("go", "build", "-o", bin, "../../cmd/evo").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "version", "--json").Output()
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Commit string }
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatal(err)
	}
	if v.Commit == "none" || v.Commit == "" {
		t.Fatalf("commit = %q: данные VCS потеряны (Main перезаписывает результат init); %s", v.Commit, out)
	}
}
```

Результат: `commit = "none"` при `go version -m` с `vcs.revision=7d5346e…`.
Юнит-вариант после исправления: `resolveVersion` вызывается из `Main`, и
`TestResolveVersion_FromBuildInfo` покрывает логику.

## Что не проверено

- Windows (zip, ACL), macOS/Gatekeeper, Docker-образ MCP (`docker info` в
  контейнере недоступен), `k8s kubectl/helm` (нет `kubectl`/`helm`).
- Настоящие ответы сервисов vpc/mk8s/postgresql/dns/s3: фейк отвечает им 404,
  проверены только коды выхода CLI на такой ответ.
- Ключ уровня организации (список проектов) — на фейке `org=list` не
  гонялся; поведение `whoami`/`project list` с ним проверено в r3.
- Ctrl-C с терминалом на шагах после создания ВМ (SSH, Docker) — только без
  терминала (`timeout -s INT`); с терминалом — только на ожидании ВМ.
