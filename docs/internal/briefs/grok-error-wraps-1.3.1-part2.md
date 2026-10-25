# Cursor или Grok: язык CLI в обёртках ошибок — часть 2, клиенты сервисов (evo 1.3.1)

Репозиторий `shatim-ops/evo-cli`, база — свежий `main`. Общий бриф —
`docs/internal/briefs/grok-error-wraps-1.3.1.md`; здесь — только часть 2.
Правка только в продуктовом коде (`*.go` без `_test`), кроме двух
вычеркиваний ниже.

## Задача

В русском CLI ошибка облака приходит с английской приставкой evo:
«dns request failed: …», «parse operation: …». Переведи 65 вызовов
`fmt.Errorf` в 14 пакетах на `i18n.T`, **не меняя английский текст ни на
символ**:

```go
// было
return nil, fmt.Errorf("parse operation: %w", err)
// стало
return nil, fmt.Errorf(i18n.T("parse operation: %w", "разбор операции: %w"), err)
```

- первый аргумент `i18n.T` — ровно прежний литерал;
- глаголы (`%w`, `%s`) и их порядок в русской строке — те же;
- имена сервисов и полей (`artifact-registry`, `dbaas`, `mk8s`, `oidc`,
  `kubeconfig`, `%s` с именем поля) не переводятся;
- импорт `github.com/shatim-ops/evo-cli/internal/i18n` добавить в пакеты, где
  его нет: `artifact`, `certmanager`, `containerapp`, `dbaas`, `dns`,
  `monitoring`, `nlb`, `postgresql`, `secret`;
- стиль перевода — как в уже переведённом `internal/s3/client.go`
  (`"list buckets: %w"` → `"список бакетов: %w"`).

Словарь, чтобы части звучали одинаково:

| en | ru |
|---|---|
| `<service> request failed: %w` | `запрос к <service> не удался: %w` |
| `parse operation: %w` | `разбор операции: %w` |
| `parse response: %w` | `разбор ответа: %w` |
| `parse %s: %w` | `разбор %s: %w` |
| `parse clusters` / `cluster` | `разбор кластеров` / `кластера` |
| `parse <что-то>` | `разбор <чего-то>` |
| `init S3 client: %w` | `инициализация клиента S3: %w` |
| `get object: %w` | `чтение объекта: %w` |

## Где (65 мест)

| Пакет | Мест |
|---|---|
| `internal/mk8s` (`client.go`, `addons.go`, `kubeconfig.go`, `nodepool.go`) | 14 |
| `internal/dbaas` | 6 |
| `internal/vpc` | 6 |
| `internal/artifact` | 5 |
| `internal/containerapp` | 5 |
| `internal/dns` | 5 |
| `internal/nlb` | 5 |
| `internal/postgresql` | 5 |
| `internal/secret` | 4 |
| `internal/billing` | 3 |
| `internal/audit` | 2 |
| `internal/certmanager` | 2 |
| `internal/s3` | 2 |
| `internal/monitoring` | 1 |

Точный список с номерами строк печатает тест, когда пакет вычеркнут из
перечня (шаг 1 ниже).

## Порядок

1. В `internal/testrules/errwrap_test.go`, в `englishWraps`, удали 14 строк
   с пометкой `"1.3.1, part 2"`. Запусти
   `go test ./internal/testrules -run TestErrorWrapsSpeakCLILanguage` —
   он покажет все 65 мест.
2. Исправь их по образцу выше.
3. В `internal/tools/qa_lang_test.go`, в `apiErrorEnglishLeft`, удали
   `"s3_get": true` — его английская приставка «get object» живёт в
   `internal/s3`. Если часть 1 (`internal/tools`) ещё не слита, остальные
   строки перечня не трогай.
4. Других правок в `_test.go` нет: строки в перечни **не добавлять**.

## Приёмка

Всё зелёное:

```
go test ./internal/testrules -run "TestErrorWrapsSpeakCLILanguage|TestErrorWrapsKeepTheirEnglish"
go test ./internal/tools -run TestQA11_ToolErrorsSpeakCLILanguage
gofmt -l .
go vet ./...
GOOS=windows go vet ./...
go test -race ./...
```

Тест сам проверит обе стороны: обёртка, оставшаяся английской, и строка
перечня, у которой английского больше нет, — падение.

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
- менять английский текст: с `EVO_LANG=en` и в MCP вывод байт-в-байт прежний;
- трогать любые тесты, кроме вычеркивания строк в шагах 1 и 3; в
  частности, литералы `"mk8s request failed: %w"` в `internal/errors/network_test.go`,
  `internal/tools/errreason_test.go`, `internal/tools/errresult_test.go` —
  это тестовые данные, их не трогать;
- переписывать уже переведённые сообщения и тексты облака (`dns API 500`,
  тела ответов);
- менять логику: только формат ошибки.

## PR

- Заголовок: «Язык CLI в обёртках ошибок, часть 2: клиенты сервисов
  (продукт, 1.3.1)».
- CHANGELOG: если `changelog.d/error-wraps-lang.md` уже есть (часть 1) —
  не трогать или дополнить одной фразой; если нет — создать по
  `changelog.d/README.md`, раздел «Исправлено»: «Русский CLI: приставка evo
  перед ошибкой облака — по-русски (было по-английски); `reason`, коды
  выхода и английский текст не меняются». Не «Миграция».
- В описании PR: список 14 пакетов и строка «Ускользнул: ревью Qwen на #319;
  инвариант: `TestErrorWrapsSpeakCLILanguage` (#351)».
- Никаких реальных ID, IP, ключей: репозиторий публичный.
