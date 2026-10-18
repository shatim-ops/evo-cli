# Финальный аудит перед v1.0.0-rc.1

Аудитор: Claude Fable 5.1, 2026-10-15 (ночь после слияния PR #3–#19).
Ревизия: `main` @ `10ccc2c`; исправления — ветка `claude/fable-audit`.
Все утверждения ниже проверены заново: командой, тестом или чтением кода;
заявления предыдущих отчётов не переписывались без проверки.

## Вердикт

**Готово к `v1.0.0-rc.1` с оговорками.** Код, тесты, релизный конвейер и
документация в состоянии, когда тег можно ставить. Две метрики готовности
закрыты не полностью, и обе не закрываются из этого контейнера:

1. сквозной `init → deploy → logs -f → destroy` на живой ВМ (SSH-шаги) —
   нужен прогон `cloud-e2e` с секретами репозитория;
2. подпись cosign и Release — только на реальном теге в GitHub Actions.

Плюс три вопроса, которые должен решить владелец до GA (не до rc): правообладатель
в `NOTICE`, `isError` у `confirmation_required`, переименование в `evo`.

## 1. Метрики готовности v1.0 (01-plan.md)

| # | Метрика | Статус | Доказательство |
|---|---|---|---|
| 1 | `go test -race ./...` < 10 с; CI зелёный на трёх ОС | **выполнено** (с оговоркой) | `GORACE=atexit_sleep_ms=0 go test -race -count=1 ./...` — 5,4 с; без переменной — 17,5 с стенки, потому что race-рантайм спит 1 с при выходе каждого из 25 пакетов (BACKLOG [06]). CI `ci.yml` run #133 на `main@10ccc2c`: `success` (test ubuntu/macos/windows + docker) |
| 2 | `configure && whoami` без env-переменных | **выполнено** | В изолированном `CLOUDRU_CONFIG_DIR`, все `CLOUDRU_*` сняты: `configure --key-id … --secret-stdin --project-id …` → `config.yaml`/`credentials.yaml` `0600`, каталог `0700`; `whoami` → 0, `token_source: cache` на втором вызове; `whoami --profile nosuch` → 2 с подсказкой |
| 3 | `tools list` = 146; `tools call` отдаёт тот же JSON, что MCP | **выполнено, число другое: 147** | `tools list --json \| jq length` = 147 (146 + `vm_list`, добавлен на этапе 4b; закреплено в `internal/tools/registry_test.go:40`). `tools call list_zones --json` и `tools/call list_zones` по stdio — `diff` пустой |
| 4 | `vm create` без `--yes` и без TTY → код 3, ничего не создано | **выполнено** | `vm create auditvm </dev/null` → превью в stderr, код 3; `--json` → `{"status":"confirmation_required"}` в stdout; то же `disk delete X`, `tools call disk_delete`, `k8s kubectl … delete`, `destroy`, `echo \| deploy` (с кредами: превью с ценой, код 3, `.cloudru-vm/` без ключа и state) |
| 5 | `init → deploy → logs -f → destroy` на дефолтном образе | **частично** | Проверено: `init --non-interactive` (зона и образ по имени из API), `deploy --dry-run` (flavor, зона, образ, диск, сеть, порты, цена ~800 ₽/мес), `deploy --dry-run --json`. **Не проверено** мной: создание ВМ, SSH, `logs -f`, `destroy` — из контейнера закрыт порт 22, а создавать ресурсы аудиту запрещено. Приёмка 14.10 подтвердила только создание ВМ в ru.AZ-2. Закрывается запуском `.github/workflows/cloud-e2e.yml` (см. «Утро») |
| 6 | Тег → Release: 6 бинарей, `checksums.txt`, cosign | **частично** | Локально `goreleaser release --snapshot --clean --skip=sign` (goreleaser скачан с GitHub, sha256 сверен): 6 бинарей (`file`: ELF static ×2, Mach-O ×2, PE32+ ×2), 4 tar.gz + 2 zip с LICENSE/NOTICE/README, `checksums.txt`; `dist/…/cloudru-vm version` печатает version/commit/date/go/os-arch (`0.0.1-snapshot-10ccc2c`, полный SHA). `install.sh` против локального зеркала: ставит бинарь, подменённый sha256 → отказ (код 1). `release.yml` прочитан: `permissions` по job, cosign-installer, bundle `.sigstore.json`, prerelease auto. **Не проверено**: сама подпись и Release (нужен OIDC GitHub Actions, то есть реальный тег); тестовый тег `v0.0.0-test` из брифа 7 не делался |
| 7 | Docker-образ MCP отвечает на `initialize` по HTTP с токеном | **частично** | Бинарём: `mcp --http 127.0.0.1:18080` + `MCP_BEARER_TOKEN` → `initialize` отвечает `2025-06-18` (и `2024-11-05`, если попросить; незнакомую → новейшую), без токена 401, `tools/list` 147, `Mcp-Session-Id` выдаётся. Образ собрать не удалось — в контейнере нет Docker-демона; job `docker` в CI (hadolint, сборка, 401, initialize, `mcp healthcheck`) зелёный на `main` |
| 8 | README: установка за 3 команды, таблица env, роли SA, троублшутинг | **выполнено** | README: `curl … \| sh` + `version`; таблица всех env — `docs/auth.md` §7, расхождение с кодом ловит `TestAuthDoc_ListsEveryEnvVar`; роли — честно «минимум не опубликован»; `docs/troubleshooting.md` по кодам ошибок. `docs/commands.md` сверяется с бинарём (`TestCommandsDoc`) |
| 9 | LICENSE заменена; имя согласовано | **частично** | `LICENSE` — Apache-2.0 (`head -3`), `NOTICE` есть. **Но**: бриф 00 требовал `Copyright 2026 shatim-ops` до подтверждения юрлица, а в `NOTICE` и README стоит «ООО «Облачные технологии» (Cloud.ru)» — вопрос владельцу. Имя `evo` (D2, этап 10) не делалось: ждёт согласования бренда |

## 2. Критерии брифов этапов

| Этап | Критерии приёмки брифа | Статус | Чем проверено |
|---|---|---|---|
| 0 лицензия | `head -3 LICENSE` = Apache 2.0; таблица лицензий зависимостей в PR | выполнено / не проверял PR | `LICENSE`, `NOTICE`; `go-licenses` не перепроверял — все прямые зависимости (cobra, minio-go, x/*, yaml.v3) MIT/BSD/Apache |
| 1 фундамент | `nosuchcmd` → 1 со stderr; `--nosuchflag` → 1; без кредов → 2 с подсказкой; `echo \| destroy` → 3; `--json` → `{"error":{"code":N}}` | выполнено | скрипт `accept1.sh`/`accept2.sh`: все пять команд дали ожидаемые коды; ошибка до разбора флагов тоже уходит JSON-ом, если `--json` есть в argv |
| 2 креды | `configure` интерактивно/неинтерактивно, 0600, `whoami` без env → 0, `--profile nosuch` → 2, второй `whoami` без IAM | выполнено | метрика 2; кэш `token-default.json` 0600 в `$CLOUDRU_CONFIG_DIR/cache` |
| 3 подтверждения | `deploy --dry-run` → 0 с ценой; `echo \| deploy` → 3; `destroy --json </dev/null` → JSON + 3 | выполнено | метрика 4; `confirm.Ask` — единственный примитив согласия в CLI (grep: `fmt.Scanln` нет) |
| 4 покрытие | `tools list --json \| jq length` = 146; `describe`; `call list_zones` = MCP; `call disk_delete … </dev/null` → 3; деревья `vm/k8s/pg/s3` | выполнено (147) | метрика 3; `vm list`, `disk list`, `vpc subnets`, `k8s list`, `pg list`, `secret list` на живом аккаунте; `s3 buckets` без ключей S3 → код 5 (BACKLOG [04]) |
| 5 deploy | ВМ с Docker, `logs -f` поток, `destroy` удаляет FIP | частично | см. метрику 5; юнит-тесты: мутация «FIP не освобождать» роняет 4 теста, `verify` на unhealthy → ошибка |
| 6 тесты/CI | `go test -race` < 10 с; `203.0.113` в `internal/` только в тестах; CI на трёх ОС | выполнено | `grep -rn 203.0.113 internal/` — только `*_test.go` и мои фикстуры; CI зелёный |
| 7 релиз | snapshot → 6 бинарей; `version`; cosign на тестовом теге | частично | метрика 6 |
| 8 MCP/HTTP | `initialize` → `2025-06-18`; 401 без токена; отказ `--http :8080 --auth none` | выполнено (без Docker) | метрика 7; `mcp --http :8080 --auth none` → код 1 с подсказкой; без `MCP_BEARER_TOKEN` → код 2 |
| 9 документация | человек проходит quickstart; тесты на env и `commands.md` | частично | тесты есть и зелёные; «проверить на коллеге» — не делалось (BACKLOG [09]) |
| 10 rename | — | не начинался | предусловие (бренд) не выполнено |

## 3. Что проверял помимо критериев

### Целостность после параллельных слияний

- **Одна реализация на механизм**: подтверждения — `internal/confirm.Ask` (CLI) + гейт `confirmed` в обработчике (общий для CLI и MCP); блокировки — `state.Lock` (flock/LockFileEx), `config.lockProfiles` (O_EXCL + stale 30 с), `Registry.lockJob` (in-process, BACKLOG про flock); ретраи — IAM (`iam.go`) и compute (`adapter.doWithRetry`) раздельно, но это разные протоколы ошибок; маскирование — `config.MaskKeyID` (вывод) и `mcp.redactSecrets` (HTTP), разные задачи. **Найдено и исправлено**: две проверки loopback (`cmd/isLoopbackHost` и `mcp/loopbackHost`), два одинаковых типа `EngineFactory`.
- **Мёртвый код** (`deadcode ./...`): `config.Load`, `deploy.CompareHostKey`, `deploy.parsePrivateKey` — удалены вместе с пятью тестами, которые проверяли только их (рабочая проверка ключа хоста — `buildHostKeyCallback`, покрыта `ssh_server_test.go`). Осталось: `internal/bootstrap` (`Defaults`, `Validate`, `Render`, шаблон cloud-init) — BACKLOG [05], продуктовое решение; `config.Resolve` — используется тестами.
- **Тесты без проверок**: сканер (функции `Test*` без `t.Error/Fatal/…`) — ни одной. `t.Skip` — 10 штук, все с причиной (Windows-права, симлинки).
- **Коды возврата между командами**: противоречий сверх уже записанных в BACKLOG нет (`logs` без развёртывания → 4, `status/verify/destroy` → 0; s3 без ключей → 5 вместо 2).
- `staticcheck`, `go vet` (linux/windows/darwin), `gofmt`, `govulncheck` (0 достижимых; 1 в требуемом модуле, код не вызывает), `go mod tidy` без диффа, `shellcheck install.sh` — чисто.

### Мутационная проверка тестов

Ломал по одной защите и смотрел, падают ли тесты (`scratchpad/mutate.sh`, 40 мутаций):

| Защита | Результат |
|---|---|
| `confirm.Ask` без TTY даёт согласие | убита (9 тестов) |
| код 3 → 1 для `CONFIRMATION_REQUIRED` | убита (7) |
| bearer принимает любой токен | убита (1) |
| `Host` не проверяется при `--auth none` (DNS rebinding) | убита (1) |
| `Origin` не проверяется | убита (1) |
| `secret_value` / `cert_private_key` / `k8s_kubeconfig embed_token` не отключены по HTTP | убиты (1/2/1) |
| маскирование секретов выключено | убита (1) |
| kubectl: запрещённые флаги / allowlist глаголов | убиты (4/2) |
| helm: частный IP / локальный chart | убиты (1/1) |
| `compose_file` вне рабочего каталога | убита (1) |
| HTTP-ограничения протекли в stdio | убита (4) |
| `plan_id` / `job_id` без валидации (path traversal) | убиты (1/1) |
| SSH-ключ: симлинк файла / каталога | убиты (3/1) |
| flock — no-op | убита (3) |
| OIDC: недоверенный `iam-url` / редиректы | убиты (1/1) |
| `s3_bucket_create` без гейта; `rollout restart` как чтение | убиты (1/1) |
| `tools call` не досылает `confirmed=true` после согласия | убита (2) |
| FIP не освобождается в `destroy`; 401/403 IAM не → `AUTH_FAILED`; просроченный токен из кэша; `verify` unhealthy → nil; каталог state 0755; профили 0644; секреты stack в файле | все убиты |
| **тело POST /mcp без лимита 16 МиБ** | **выжила** → добавлен `TestAudit_HTTPBodySizeLimited` (проверено: с мутацией падает) |
| `tools call`: ветка «mutating без `confirmed` в схеме» | выжила — ветка недостижима по построению (`isMutating` выводится из наличия `confirmed`); оставлена как защита |

### Безопасность: попытки обхода (сервер `mcp --http`, живой профиль)

- `secret_value`, `cert_private_key`, `k8s_kubeconfig` (embed), `k8s_kubectl config view --raw` — отказ с понятной ошибкой; `embed_token=false` доходит до API.
- kubectl (19 вариантов): `--kubeconfig=/…`, `--kubeconfig /…` до глагола, `-ojsonpath-file=`, `-o custom-columns-file=`, `--output=jsonpath-file=`, `-o go-template-file=`, `apply -k .`, `cp`, `plugin list`, `myplugin`, `get secret -o yaml`, `get secrets.v1 -ojson`, `create token`, `get --raw`, `-n --kubeconfig` — все отклонены политикой; `get pods -v=9`, `exec -- cat`, `--as=admin` прошли политику (и упёрлись в «kubectl not installed») — корректно, это кластерная сторона.
- helm: `./x`, `/etc/passwd`, `c:\x`, `oci://127.0.0.1/`, `http://169.254.169.254/`, `https://[::ffff:127.0.0.1]/`, `https://localhost/` — отклонены; `https://charts.bitnami.com` — прошёл.
- `deploy compose_file=../../etc/passwd` и `/etc/passwd` — отклонены. `deploy_plan compose_file=/etc/passwd` **проходит**, но файл не читается (только флаг `compose_required`) — не утечка.
- DNS rebinding при `--auth none`: `Host: evil.com`, `127.0.0.1.nip.io` → 403; `localhost`, `127.0.0.1`, `[::1]` → 200. `Origin: http://evil.com`, `null` → 403.
- `iam-url` из kubeconfig: `trustedOIDCURL` требует https, хост из двух IAM, без порта и userinfo; иначе дефолт. Редиректы OIDC не следуются.
- SSH-ключ: симлинк каталога `.cloudru-vm`, `id_ed25519`, `id_ed25519.pub` — отказ; запись temp 0600 + rename; `.gitignore` O_EXCL. Планы `plans/<random>.json` и задания `stk-<hex>.json` — имя не от клиента.
- `MCP_BEARER_TOKEN` не виден в `ps`; в логах нет аргументов.
- `gosec`: 24 срабатывания, все просмотрены — `G304` (пути из конфигурации), `G302` (0644 у `state.json`, `.gitignore`, `.pub` — задокументировано), `G402` в `verify` (liveness-проба по IP, `nolint`), `G101` — имена констант.

### Облако (только чтение, тестовый аккаунт)

`whoami`, `doctor` (один из двух прогонов: IAM `context deadline exceeded` — та самая треть обрывов; второй — ok), `vm zones/list/flavors`, `disk list`, `vpc subnets`, `k8s list`, `pg list`, `secret list`, `list-images`, `list_floating_ips`, `init`, `deploy --dry-run`. Ничего не создавалось.

## 4. Найдено и исправлено (этот PR)

1. Нет теста на лимит тела HTTP (`MaxBytesReader`, 16 МиБ) — добавлен.
2. Две реализации проверки loopback — одна `mcp.LoopbackHost`.
3. Два типа `EngineFactory` — псевдоним.
4. Мёртвый код `config.Load`, `deploy.CompareHostKey`, `deploy.parsePrivateKey` и тесты только на них.
5. `docs/auth.md`: «ключ создаётся уже на `deploy --dry-run`» — устарело после QA (проверено: `--dry-run` и отказ без `--yes` ключ не создают).
6. Комментарий `Dockerfile` про каталог кэша токена.
7. `auth/iam.go`: обмен ключа шёл общим `httpClient`, который на 307/308 повторил бы POST с секретом по `Location` — теперь без редиректов (BACKLOG [sec-supply]); тест с редиректом.

## 5. Найдено и НЕ исправлено

| Severity | Что | Почему не здесь |
|---|---|---|
| medium | Правила SG (`deploy` открывает порты в общей группе зоны) остаются после `destroy` (BACKLOG [05/приёмка]); задокументировано в troubleshooting | нужен `DELETE /security-groups/{id}/rules/{rule_id}`, проверка вживую и хранение sg_id в state — продуктовое и требует облака |
| medium | Вызовы инструментов MCP не отменяются и не ограничены по времени (`context.Background()` в `callHandler`); разрыв HTTP-клиента не останавливает `provision`/`helm` | общий таймаут убил бы `deploy` на середине — решение владельца (BACKLOG [sec-surface]) |
| medium | Windows: `.cloudru-vm\id_ed25519` наследует ACL каталога проекта (BACKLOG [sec-supply]); инструкция `icacls` в auth.md | ~100 строк с `x/sys/windows` и тест на windows-раннере |
| medium (продукт) | `confirmation_required` приходит с `isError: true`; по спецификации MCP это не ошибка. После rc менять уже ломающе | решение владельца, см. вопрос 2 |
| low | `whoami`/`doctor` говорят «ok» с неверным секретом, пока жив кэш токена того же Key ID (BACKLOG [qa-cli]) | меняет интерфейс `TokenCache` |
| low | `s3 …` без ключей S3 → код 5 вместо 2; `logs` без развёртывания → 4, `status/verify/destroy` → 0 (BACKLOG) | продуктовое единое правило |
| low | `vm flavors`, `k8s options` и другие ответы «объект с одним списком» печатаются таблицей FIELD/VALUE с JSON-строкой; `--json` нормальный | UX деревьев 4b, не ошибка |
| low | Блокировка job стека только внутри процесса; stdio- и http-сервер на одном `~/.cloudru-vm/stack-jobs` могут крутить один job вдвоём (BACKLOG [qa-mcp]) | flock на трёх ОС отдельной задачей |
| low | `internal/bootstrap` (cloud-init) мёртв: `Render`, `Defaults`, `Validate`, шаблон (BACKLOG [05]) | удалить или подтвердить поле `cloud_init` в compute API — решение владельца |
| low | Образ MCP не подписан, без SBOM/provenance (BACKLOG [sec-supply]) | нужны права `attestations: write` и проверка AR |
| info | Метрика «146 инструментов» фактически 147 (`vm_list`) | поправить число в плане/карточке |
| info | `go test -race` по умолчанию 17 с из-за `atexit_sleep_ms`; в CI это не критерий | `GORACE=atexit_sleep_ms=0` в CI при желании |

## 6. Вопросы владельцу

1. **Правообладатель.** Бриф 00: «`Copyright 2026 shatim-ops`, владелец подтверждает юрлицо до мержа». В `NOTICE` и README — «ООО «Облачные технологии» (Cloud.ru)». Это подтверждённое решение или надо вернуть `shatim-ops` до публичного тега?
2. **`isError: true` у `confirmation_required`.** Оставить (клиенты различают по `status`, так и написано в docs/mcp.md) или привести к спецификации до rc.1, пока нет внешних клиентов?
3. **Переименование в `evo` (D2).** rc.1 выходит как `cloudru-vm`; переименование между rc и 1.0.0 меняет пути конфигурации и имя образа. Либо согласовать бренд до rc.1, либо принять, что `evo` — это v1.1 с alias-периодом.
4. **Правила SG после `destroy`** — принять для rc.1 с документацией (как сейчас) или блокер GA?
5. **Архивы релиза** без `docs/*.md` (BACKLOG [09]) — ссылки README в распакованном архиве ведут в никуда; добавить?
6. **Поддержка/SLA** (DECISIONS, открытый вопрос 3) — текст раздела «Поддержка» пока ведёт в Issues и поддержку Cloud.ru.

## 7. Рекомендованный порядок действий на утро

0. **GitHub Actions не запускается.** На PR #24 все четыре job упали за 3 с
   без логов с аннотацией «The job was not started because recent account
   payments have failed or your spending limit needs to be increased»
   (Settings → Billing & plans). Последний успешный прогон — `main@10ccc2c`
   в 23:19 UTC; после него ночные прогоны на трёх ОС (macOS считается ×10)
   выбрали лимит. Поднять лимит / поправить оплату → «Re-run all jobs» на
   PR #24. Локально эквивалент CI пройден: `go build`, `go vet` (linux,
   windows, darwin), `gofmt`, `go test -race` (все пакеты), `staticcheck`,
   `govulncheck`, `shellcheck`, `goreleaser` snapshot; не воспроизведены
   здесь только `hadolint` и сборка образа (нет Docker-демона) — они
   зелёные на `main`, Dockerfile в этом PR меняет один комментарий.
1. Прочитать и смержить этот PR после зелёного CI на трёх ОС + docker.
2. **Секреты для `cloud-e2e`**: Settings → Secrets → Actions: `CLOUDRU_KEY_ID`, `CLOUDRU_SECRET`, `CLOUDRU_PROJECT_ID` тестового SA. Запустить `cloud-e2e` вручную (zone `ru.AZ-2`). Зелёный прогон закрывает метрику 5 (ВМ с Docker, `verify`, `logs -f` > 120 с, `destroy` удаляет ВМ и FIP). Если `logs -f` или SSH упадут — это первый реальный дефект для rc.2, не блокер тега.
3. **Ротация ключа SA.** Ключ тестового аккаунта этой ночью лежал в окружении нескольких агентских контейнеров (`/root/.cloudru.env`). Создать новый ключ в консоли → положить в секреты репозитория (п. 2) → удалить старый. Проверить `cloudru-vm whoami` с новым.
4. Ответить на вопросы 1–3 (остальные могут подождать GA). Если правообладателя надо менять — одна правка `NOTICE` + README до тега.
5. **Тег**: `git tag -a v1.0.0-rc.1 -m v1.0.0-rc.1 && git push origin v1.0.0-rc.1`. Дождаться `release.yml`; проверить по `docs/install.md`: `cosign verify-blob --bundle …` и `install.sh` без `CLOUDRU_VM_BASE_URL` с `CLOUDRU_VM_VERSION=v1.0.0-rc.1`. Если хочется образ в Artifact Registry — заранее `CLOUDRU_AR_REGISTRY/KEY_ID/SECRET`.
6. Критерий этапа 9: дать коллеге без доступа к репозиторию `install → configure → vm list → deploy --dry-run` по документации, засечь время, записать в BACKLOG.
7. До GA: пункты severity medium из раздела 5 (SG-правила, отмена вызовов, Windows ACL) и решение по `isError`.
