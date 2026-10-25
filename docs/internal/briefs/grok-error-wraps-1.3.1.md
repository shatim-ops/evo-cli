# Бриф Grok: язык CLI в обёртках ошибок (1.3.1, 25.10.2026)

Решение владельца 25.10: ошибки, в которых evo оборачивает ответ облака
(`fmt.Errorf("create zone: %w", err)`), в русском CLI показывают приставку evo
по-английски — «create zone: dns API 500: …». Правка — в патче 1.3.1.
Бриф пишет «Процесс и тестирование»; исполнитель — Grok (решение владельца 25.10: Grok недоступен); ревью каждого
файла — «1.3.1 — разработка»; ревью Qwen приходит само.

Репозиторий `shatim-ops/evo-cli`, база — `main`. Инварианты уже в `main`
(«Процесс»): два закрытых перечня мест, которые ещё по-английски. Перечни
только сокращаются: каждый PR правит свои пакеты и вычёркивает их строки, и
тест сам проверяет, что вычеркнутое действительно исправлено, а нового
английского не появилось.

**Работа идёт тремя PR по частям** (решение владельца 25.10), по порядку,
каждый — после слияния предыдущего:

| Часть | Пакеты | Вычеркнуть |
|---|---|---|
| 1 | `internal/tools` | строку `internal/tools` в `englishWraps` (`internal/testrules/errwrap_test.go`) и весь перечень `apiErrorEnglishLeft` (`internal/tools/qa_lang_test.go`) |
| 2 | клиенты сервисов: `artifact`, `audit`, `billing`, `certmanager`, `containerapp`, `dbaas`, `dns`, `mk8s`, `monitoring`, `nlb`, `postgresql`, `s3`, `secret`, `vpc` | их строки «part 2» в `englishWraps` |
| 3 | остальное: `agentconf`, `auth`, `cli`, `config`, `deployplan`, `kubetools`, `mcp`, `stack` | их строки «part 3»; после неё `englishWraps` пуст |

Если правка части 1 не убирает инструмент из `apiErrorEnglishLeft` (его
английская приставка — из клиента сервиса, например `s3_get` → `internal/s3`),
строка остаётся до части 2 — тест скажет, какая.

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

## Где (182 места в 23 пакетах)

Список с номерами строк печатает сам тест, если вычеркнуть пакет из
`englishWraps`:

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
каждый инструмент с отказом облака и проверяет (кроме перечня `apiErrorEnglishLeft`), что в русском CLI
приставка evo до текста облака (`dns API 500`, `HTTP 500`, «API вернул
статус…», тело ответа, фраза клиентской библиотеки) не содержит сегмента без
кириллицы. В перечне — 11 инструментов (`dns_zone_create`,
`disk_create`, `image_create`, `pg_db_create`, `pg_user_create`,
`secret_create`, `secret_add_version`, `release_floating_ip`, `s3_get`,
`provision` и `deploy_plan` через `list flavors`) — они входят в 182.

## Приёмка (каждой части)

Строки своей части вычеркнуты, и всё зелёное:

```
go test ./internal/testrules -run "TestErrorWrapsSpeakCLILanguage|TestErrorWrapsKeepTheirEnglish"
go test ./internal/tools -run TestQA11_ToolErrorsSpeakCLILanguage
gofmt -l . ; go vet ./... ; GOOS=windows go vet ./...
go test -race ./...
```

## Английский текст — под тестом

`TestErrorWrapsKeepTheirEnglish` (`internal/testrules`) сверяет каждую
английскую строку обёрток с `internal/testrules/testdata/english-wraps.txt`:
литерал, ставший первым аргументом `i18n.T`, должен совпасть с прежним
байт-в-байт. «Улучшенный» английский текст роняет тест. Файл
`english-wraps.txt` **не править**.

**Окружение — без ключей Cloud.ru**: задача только про код, живые прогоны
не нужны; ключ оператора прошлых волн в этот сеанс не передаётся.

## Нельзя

- менять `reason`, коды выхода, ключи и структуру `--json`, `isError` MCP;
- менять английский текст (с `EVO_LANG=en` и в MCP-сервере вывод
  байт-в-байт прежний);
- трогать тесты: `TestContract_*`, `TestParity_*`, `TestQA11_*`,
  `TestErrorWrapsSpeakCLILanguage` и любые другие `_test.go`; единственная
  правка в тестах — вычеркнуть строки своей части в `englishWraps` и
  `apiErrorEnglishLeft` (добавлять строки нельзя);
- переписывать сообщения, которые уже идут через `i18n`, и тексты облака
  (`dns API 500`, тела ответов);
- менять логику: только формат ошибки.

## PR

- Заголовок: «Язык CLI в обёртках ошибок, часть N (продукт, 1.3.1)»; ревью «разработки» — каждый файл.
- Фрагмент CHANGELOG — в части 1, части 2 и 3 его дополняют: `changelog.d/error-wraps-lang.md` (формат —
  `changelog.d/README.md`), раздел «Исправлено»: «Русский CLI: приставка evo
  перед ошибкой облака — по-русски (было по-английски); `reason`, коды
  выхода и английский текст не меняются». Не «Миграция»: `reason` и коды
  те же (`docs/RELEASING.md`).
- В описании: «Ускользнул: ревью Qwen на #319; инвариант: был
  `walkLang` только для USAGE/CONFIG_INVALID/NOT_FOUND — расширен
  «Процессом» (`TestErrorWrapsSpeakCLILanguage`, ветвь API_ERROR мира «500»)».
- Реальных ID, IP, ключей и сведений Cloud.ru в PR нет — репозиторий публичный.
