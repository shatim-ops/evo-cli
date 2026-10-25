# Бриф Cursor: язык CLI в обёртках ошибок — часть 3 (evo 1.3.1)

Репозиторий `shatim-ops/evo-cli`, база — свежий `main` после слияния части 2
(#354) и #358. Общий бриф — `docs/internal/briefs/cursor-error-wraps-1.3.1.md`.
Исполнитель — Cursor (решение владельца 25.10). Ревью каждого файла —
«1.3 — разработка».

## Задача

Перевести на `i18n.T` английские приставки evo перед ошибкой (`fmt.Errorf`
с литералом, `%w` и словами) в **шести** пакетах, не меняя английский текст
ни на символ:

| Пакет | Мест | Что там |
|---|---|---|
| `internal/config` | 17 | профиль и файлы конфигурации: `create %s`, `read %s`, `write %s`, `lock %s`, `chmod %s`, ACL Windows |
| `internal/deployplan` | 8 | `create VM`, `list flavors`, `list subnets`, `list security groups`, `list images`, план повреждён |
| `internal/auth` | 3 | генерация ключа SSH |
| `internal/kubetools` | 2 | kubeconfig, тайм-аут kubectl/helm |
| `internal/cli` | 1 | `healthcheck` |
| `internal/agentconf` | 1 | `encode` |

**Не входят в часть 3:**

- `internal/mcp` — ответ агенту остаётся английским: сервер MCP ставит
  `i18n.EN` (`internal/cli/mcp.go`), D6. Строка `internal/mcp` в
  `englishWraps` остаётся навсегда, не трогать.
- `internal/stack` — его правит «разработка» в PR решения D41 (или после
  него). Строку не трогать.

## Порядок

1. В `internal/testrules/errwrap_test.go`, в `englishWraps`, удали шесть
   строк с пометкой `"1.3.1, part 3"`. Тест
   `go test ./internal/testrules -run TestErrorWrapsSpeakCLILanguage`
   покажет места с номерами строк.
2. Исправь их по образцу общего брифа: `fmt.Errorf(i18n.T("<прежний en>", "<ru>"), …)`,
   глаголы (`%w`, `%s`, `%q`, `%d`) и их порядок — те же; импорт
   `internal/i18n` — где его нет.
3. В `internal/tools/qa_lang_test.go`, в `apiErrorEnglishLeft`, удали
   `"deploy_plan": true` — его приставка «list flavors» живёт в
   `internal/deployplan`. Перечень станет пустым — оставь `map[string]bool{}`.
4. Других правок в `_test.go` нет; строки в перечни не добавлять.

## Классификаторы `reason` — главное

`uncodedReason` (`internal/tools/errreason.go`) выбирает `reason` и код
выхода **по тексту ошибки**: `parseAnswerRE` (`parse <что>: `),
`operationFailedRE` (`operation … failed`), «not found» / «не найден»,
`apiStatusRE` (`API 500`, `HTTP 502`). Если перевести слово, которое
читает классификатор, в русском CLI сменится `reason` — это ломающее
изменение (`docs/RELEASING.md`). Части 2 это стоило 162 неверных ответа.

- `TestReason_SameInEveryLanguage` и
  `TestReason_ClassifiersReadBothLanguages` (`internal/tools`) ловят это:
  они должны остаться зелёными.
- Если строку нужно перевести, а классификатор её читает (`list …` и
  `create …` — не читает; `parse …` — читает), не переводи это слово, а
  спроси в PR: правку классификатора делает «разработка».

## Приёмка

```
go test ./internal/testrules
go test ./internal/tools -run "TestQA11_ToolErrorsSpeakCLILanguage|TestReason_"
gofmt -l .
go vet ./...
GOOS=windows go vet ./...
go test -race ./...
```

## Нельзя

- менять английский текст (`TestErrorWrapsKeepTheirEnglish`,
  `internal/testrules/testdata/english-wraps.txt` — не править);
- менять `reason`, коды выхода, `--json`, `isError` MCP;
- трогать `internal/mcp` и `internal/stack`;
- трогать тесты, кроме вычеркиваний шагов 1 и 3;
- менять логику: только формат ошибки.

## PR

- Заголовок: «Язык CLI в обёртках ошибок, часть 3 (продукт, 1.3.1)».
- CHANGELOG: `changelog.d/error-wraps-lang.md` уже есть — не трогать или
  дополнить одной фразой.
- Реальных ID, IP и ключей нет: репозиторий публичный.
