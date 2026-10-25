# Бриф Cursor: четыре текста по живому прогону Grok 1.3.0 (25.10.2026)

Репозиторий `shatim-ops/evo-cli`, база — `main` после слияния #340 (резервы
D39–D42). Задача — правка четырёх текстов, которые видит человек или агент;
поведение, коды выхода и `reason` не меняются. Находки — живой прогон Grok на
`v1.3.0` (G130-16, G130-27, G130-35, G130-36); сверка релиза —
`docs/internal/release-plan.md`, блок «В 1.3.1», строка «Тексты».

**Правила.** Читать `CLAUDE.md` целиком до первой правки. В облако Cloud.ru не
ходить: ключи фиктивные, сеть — поддельные клиенты тестов. Каждая правка —
сначала тест, который падает без неё (§4). Тексты — парами
`i18n.T(en, ru)` / `i18n.Tf`, английский и русский; комментарии в коде —
английские, в стиле окружающих. Одна точка решения на правило (§2): текст,
общий для CLI и MCP, правится в одном месте, а не копией под команду. Ни
одного реального ID, IP и ключа — только нули и RFC 5737.

**Сдача.** Ветка `cursor/texts-1.3.1`, один PR в `main` с заголовком
«Тексты подсказок по живому прогону Grok 1.3.0 (продукт, 1.3.1)», фрагмент
`changelog.d/hint-texts.md` с разделом `### Изменено` (четыре пункта, формат —
`changelog.d/README.md`), в описании PR — строка
«Ускользнул: живой прогон Grok 1.3.0; инвариант: нет — тексты подсказок
тестами на смысл не проверяются». PR не сливать: сливает сессия «релиз» по
«да» владельца. На каждую находку ревью Qwen в PR — ответ «исправлено» или
«опровергнуто» с путём к коду.

## 1. G130-16 — «не найден в проектах этого сервера» после удаления

- Где: `internal/tools/project_bound.go`, `notInProject`. Один текст на два
  случая — ресурс другого проекта и ресурс, которого уже нет (D26, H9 б —
  оба случая отвечают одинаково, это не меняется).
- Сейчас: «%s %s is not found in the projects of this server
  (--allow-project); nothing was done». После удаления агент читает это как
  «не тот сервер» и ищет проект.
- Нужно: текст называет обе причины — «%s %s is deleted or not in the
  projects of this server (--allow-project); nothing was done» (русский —
  «удалён или не в проектах этого сервера»). Подсказка (`hint`) без
  изменений.
- Тест: `internal/cli/allow_project_resource_test.go:112` проверяет старую
  фразу — обновить ожидание; добавить проверку, что оба слова («deleted»,
  «not in the projects») есть в тексте и в EN, и в RU
  (`i18n.SetLang`).

## 2. G130-27 — «в зоне нет подсети» отправляет в консоль

- Где: подсказка `engine.NoSubnetError` (`internal/engine/network.go`) —
  её видят `provision`, `deploy`, `evo vm create`, `evo deploy`; и строка
  `internal/tools/tools_kafka.go` («no subnet of the project in zone %s»,
  подсказка «Pass subnet_id, or create a subnet … in the Cloud.ru
  console»). Проверить `grep -rn "no subnet" internal --include=*.go` на
  другие копии (`tools_nlb.go`, `tools_k8s.go`) и привести к одному правилу.
- Сейчас: подсказка зовёт в консоль Cloud.ru. Агент по MCP консоли не видит,
  а другая зона или явная подсеть у него есть.
- Нужно: подсказка сначала называет, что передать, — `subnet_id`
  (список: `list_subnets`) или другую зону (`region`), — и только потом
  консоль, если подсети нет ни в одной зоне. В CLI те же сведения
  должны звучать флагами и командами (`--subnet-id`, `--region`,
  `evo vpc subnets` — сверить имена команд в `docs/commands.md`): для
  этого есть `missingInputHints` в `internal/cli/tools.go` (подсказка CLI
  по аргументу `missing`), а не вторая строка текста в движке. Сообщение
  (`message`) и `reason` (`CONFIG_INVALID` / `USAGE`) не меняются.
- Тест: подсказка в ответе `provision` без подсети в зоне (фейк без
  подсетей — см. `TestProvision_*NoSubnet*` или мир в
  `internal/tools/provision_zone_test.go`) содержит `subnet_id` и
  `list_subnets`; через CLI (`evo vm create --json`) — флаг `--subnet-id`.

## 3. G130-35 — ошибка CLI `evo vpc fip-release` упоминает `inputSchema`

- Где: `internal/tools/argcheck.go`, `ruleHint` — «See %s in the tool's
  inputSchema.» Правило `format: uuid` в схеме (D36) проверяет
  `tools.CheckArgs` для MCP и CLI одинаково; CLI печатает подсказку
  инструмента как есть (`internal/cli/trees.go`, `toolError(…, hints)`), а у
  пользователя CLI нет inputSchema.
- Нужно: в CLI подсказка называет флаг или позиционный аргумент команды и
  `--help` («Pass a UUID in --id; see 'evo vpc fip-release --help'»); по
  MCP — как сейчас. Точка решения одна — `toolHints` / `commandHints` в
  `internal/cli/tools.go`, где CLI уже переводит подсказки инструмента в
  свои термины (`missingInputHints`); не дописывать условие по имени
  команды. Если `ruleHint` не даёт CLI понять, о каком аргументе речь, —
  добавить поле ответа (например, `argument`) в `errAnswer` правила, а не
  разбирать текст.
- Тест: `evo vpc fip-release not-a-uuid --json` — `reason: USAGE`, в
  `hint` есть имя флага/аргумента и нет слова `inputSchema`; тот же вызов
  через `evo tools call release_floating_ip` и в тестах паритета
  (`TestParity_*`) — `reason` и код те же. Проверить по всем командам с
  правилом `format` в схеме (`grep -n '"format"' internal/tools/*.go`), не
  только `fip-release`: тест обходит реестр, а не одну команду.

## 4. G130-36 — превью `destroy` пишет «released: <ip>» до согласия

- Где: `internal/tools/tools.go`, `destroyTargetPreview`, поле
  `floating_ip`: «released: …», «kept: …», «kept (release_ip=false): …».
- Сейчас: прошедшее время в превью — агент читает «released» как
  уже сделанное до `confirmed=true`.
- Нужно: будущее или инфинитив — «will be released: …», «will be kept: …»
  (или «to release»/«to keep»); то же у строк `ports`/правил в этом превью,
  если они в прошедшем времени. Ответ после выполнения (`destroy` с
  `confirmed`) не трогать — там действие совершено.
- Тест: превью `destroy` с floating IP (мир `internal/tools/destroy_target_test.go`)
  не содержит слова `released:` без `will be`; ответ после `confirmed=true`
  — содержит `released`. Проверить `docs/tools.md`/`docs/mcp.md` на цитаты
  превью (`grep -rn "released:" docs`), если есть — перегенерировать
  (`go generate` или скрипт из `docs/RELEASING.md`, шаг 0).

## Проверка перед PR

```
gofmt -l . ; go vet ./... ; GOOS=windows go vet ./...
~/go/bin/staticcheck ./...
go test -race -shuffle=on ./...
python3 scripts/changelog.py check
```

`TestQA11_ToolErrorsSpeakCLILanguage` (`internal/tools/qa_lang_test.go`)
ловит английские слова в русских ответах и наоборот: каждая новая пара
`i18n.T` проходит через него. `TestDocsExamples` и тесты документации падают,
если изменённый текст процитирован в `docs/` без перегенерации.

## Чего не делать

- Не менять `reason`, коды выхода, поля ответов (кроме, при необходимости,
  нового поля с именем аргумента в п. 3 — тогда сказать в PR).
- Не трогать D26/H9 (один ответ «нет или не в проекте»), не добавлять
  проверку «удалён ли» отдельным запросом.
- Не править `skills/` и `docs/internal/` кроме этого брифа.
