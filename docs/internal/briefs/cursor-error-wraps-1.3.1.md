# Бриф Cursor: язык CLI в обёртках ошибок (1.3.1, 25.10.2026)

Решение владельца 25.10: ошибки, в которых evo оборачивает ответ облака
(`fmt.Errorf("create zone: %w", err)`), в русском CLI показывают приставку evo
по-английски — «create zone: dns API 500: …». Правка — в патче 1.3.1.
Бриф пишет «Процесс и тестирование»; исполнитель — Cursor; ревью каждого
файла — «1.3.1 — разработка»; ревью Qwen приходит само.

Репозиторий `shatim-ops/evo-cli`. **Базовая ветка — `claude/proc-api-error-lang`**
(от `main`): в ней уже лежат два падающих теста — это и есть критерий
приёмки. Ветку не переписывать: свою ветку создать от неё, PR — в `main`.

## Что сделать

Каждый вызов `fmt.Errorf`, у которого формат — строковый литерал с `%w` и
словами (а не только идентификаторами и глаголами формата), перевести на
`i18n.T` / `i18n.Tf` так, чтобы:

- **английский текст не изменился ни на символ** (первый аргумент `i18n.T` —
  ровно прежний литерал);
- русский — тот же смысл по-русски, в стиле уже переведённых сообщений
  соседнего кода (`internal/s3/client.go`: `fmt.Errorf(i18n.T("list buckets: %w", "список бакетов: %w"), err)`);
- глаголы формата (`%w`, `%s`, `%q`, `%d`, `%v`) и их порядок в русской
  строке — те же, что в английской (`go vet` их проверяет);
- имена аргументов, инструментов и полей (`image_metadata`, `k8s_helm`,
  `compose_file`, `repo_url`) остаются как есть в обеих строках.

Образец:

```go
// было
return errResult(fmt.Errorf("create zone: %w", err)), true
// стало
return errResult(fmt.Errorf(i18n.T("create zone: %w", "создание зоны: %w"), err)), true
```

Пакет без импорта `internal/i18n` — добавить импорт. Если `go vet` пишет
«non-constant format string» — значит, у вызова нет аргументов после
формата; такого случая среди мест ниже быть не должно, иначе — вопрос в PR.

## Где (182 места в 24 пакетах)

Полный список с номерами строк печатает сам тест:

```
go test ./internal/testrules -run TestErrorWrapsSpeakCLILanguage
```

| Файл | Мест |
|---|---|
| `internal/config/profile.go` | 14 |
| `internal/tools/tools_stack.go` | 9 |
| `internal/tools/tools_pg.go` | 9 |
| `internal/tools/tools_deploy.go` | 8 |
| `internal/tools/tools_disk.go` | 7 |
| `internal/tools/tools.go` | 7 |
| `internal/mk8s/client.go` | 7 |
| `internal/vpc/client.go` | 6 |
| `internal/dbaas/client.go` | 6 |
| `internal/tools/tools_k8s.go` | 5 |
| `internal/postgresql/client.go` | 5 |
| `internal/nlb/client.go` | 5 |
| `internal/dns/client.go` | 5 |
| `internal/containerapp/client.go` | 5 |
| `internal/artifact/client.go` | 5 |
| `internal/tools/tools_vpc.go` | 4 |
| `internal/tools/tools_redis.go` | 4 |
| `internal/tools/tools_kafka.go` | 4 |
| `internal/tools/tools_dns.go` | 4 |
| `internal/secret/client.go` | 4 |
| `internal/mk8s/addons.go` | 4 |
| `internal/deployplan/resolve.go` | 4 |
| `internal/tools/tools_secret.go` | 3 |
| `internal/tools/tools_nlb.go` | 3 |
| `internal/mcp/http_kube.go` | 3 |
| `internal/deployplan/apply.go` | 3 |
| `internal/billing/client.go` | 3 |
| `internal/auth/keygen.go` | 3 |
| `internal/tools/tools_snat.go` | 2 |
| `internal/tools/tools_kube.go` | 2 |
| `internal/tools/tools_image.go` | 2 |
| `internal/tools/tools_certmanager.go` | 2 |
| `internal/tools/tools_ar.go` | 2 |
| `internal/stack/store.go` | 2 |
| `internal/s3/client.go` | 2 |
| `internal/mk8s/nodepool.go` | 2 |
| `internal/kubetools/kubetools.go` | 2 |
| `internal/config/private_windows.go` | 2 |
| `internal/certmanager/client.go` | 2 |
| `internal/audit/client.go` | 2 |
| `internal/tools/tools_k8s_addons.go` | 1 |
| `internal/monitoring/client.go` | 1 |
| `internal/mk8s/kubeconfig.go` | 1 |
| `internal/mcp/server.go` | 1 |
| `internal/mcp/http_policy.go` | 1 |
| `internal/deployplan/store.go` | 1 |
| `internal/config/config.go` | 1 |
| `internal/cli/mcp.go` | 1 |
| `internal/agentconf/agentconf.go` | 1 |

Второй тест — мир «500» (`internal/tools/qa_lang_test.go`,
`TestQA11_ToolErrorsSpeakCLILanguage`, новая ветвь `API_ERROR`): вызывает
каждый инструмент с отказом облака и проверяет, что в русском CLI
приставка evo до текста облака (`dns API 500`, `HTTP 500`, «API вернул
статус…», тело ответа, фраза клиентской библиотеки) не содержит сегмента без
кириллицы. Сейчас он называет 11 инструментов (`dns_zone_create`,
`disk_create`, `image_create`, `pg_db_create`, `pg_user_create`,
`secret_create`, `secret_add_version`, `release_floating_ip`, `s3_get`,
`provision` и `deploy_plan` через `list flavors`) — они входят в 182.

## Приёмка

Обе команды зелёные, и ничего больше не сломано:

```
go test ./internal/testrules -run TestErrorWrapsSpeakCLILanguage
go test ./internal/tools -run TestQA11_ToolErrorsSpeakCLILanguage
gofmt -l . ; go vet ./... ; GOOS=windows go vet ./...
go test -race ./...
```

## Нельзя

- менять `reason`, коды выхода, ключи и структуру `--json`, `isError` MCP;
- менять английский текст (с `EVO_LANG=en` и в MCP-сервере вывод
  байт-в-байт прежний);
- трогать тесты: `TestContract_*`, `TestParity_*`, `TestQA11_*`,
  `TestErrorWrapsSpeakCLILanguage` и любые другие `_test.go` — правка
  только в продуктовом коде (`*.go` без `_test`);
- переписывать сообщения, которые уже идут через `i18n`, и тексты облака
  (`dns API 500`, тела ответов);
- менять логику: только формат ошибки.

## PR

- Заголовок: «Язык CLI в обёртках ошибок облака (продукт, 1.3.1)».
- Фрагмент CHANGELOG: `changelog.d/error-wraps-lang.md` (формат —
  `changelog.d/README.md`), раздел «Исправлено»: «Русский CLI: приставка evo
  перед ошибкой облака — по-русски (было по-английски); `reason`, коды
  выхода и английский текст не меняются». Не «Миграция»: `reason` и коды
  те же (`docs/RELEASING.md`).
- В описании: «Ускользнул: ревью Qwen на #319; инвариант: был
  `walkLang` только для USAGE/CONFIG_INVALID/NOT_FOUND — расширен
  «Процессом» (`TestErrorWrapsSpeakCLILanguage`, ветвь API_ERROR мира «500»)».
- Реальных ID, IP, ключей и сведений Cloud.ru в PR нет — репозиторий публичный.
