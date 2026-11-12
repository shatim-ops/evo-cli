# Новая операция: чек-лист автора PR

Для «разработки». Один PR — одна операция (или пара create–delete).
Каждый шаг называет тест, который падает, пока шаг не сделан: чек-лист не
заменяет тесты, а говорит, в каком порядке их встретишь. Имена тестов
сверяет `TestAddOperation_NamesExistingTests`. Шаги, которые тест пока не
держит, собраны отдельно в разделе 9 — их сверяют ревью и аудит эталона.

Пройденный чек-лист копируется в описание PR (критерий rc, план версии,
раздел 5, п. 3). Ось, которую операция не может выполнить из-за платформы, —
пункт реестра расхождений и явный текст в ответе, а не пропуск строки.

## 0. До кода

- [ ] ТЗ операции с таблицей «состояние → ответ» и заключение «Риска»
      (CLAUDE.md §0, правило 7).
- [ ] Тесты пишутся до кода и падают без него (CLAUDE.md §4).

## 1. Реестр и паритет CLI ↔ MCP

- [ ] Инструмент в реестре и его команда; аргументы, ответ, превью, `reason`
      и код выхода совпадают: `TestParity_CommandsAndTools`,
      `TestParity_JSONEqualsTool`, `TestParity_ErrorReasonEqualsTool`,
      `TestParity_YesEqualsConfirmed`.
- [ ] Схема и формы аргументов: `TestContract_Schema`,
      `TestContract_ArgForms`, `TestContract_EmptyRequiredIsUsage`.
- [ ] Политика сервера объявлена: `--read-only` — `TestReadOnly_EveryTool`;
      `--allow-project` — `TestProjectBound_IDArgsDeclared`.
- [ ] Документы и карта контракта перегенерированы:
      `TestContractMap_IsTheDeclarations`.

## 2. Ось 1 — выполнимость до согласия

- [ ] Всё, что видно чтением, проверено до вопроса; отказ без записи
      приходит до вопроса: `TestConsent_RefusalWithoutWriteComesBeforeTheQuestion`.
      Каждое правило платформы — вариант мира `consentWorlds`.
- [ ] Согласие — через общий гейт; превью не пишет:
      `TestContract_GateNoWrite`, `TestContract_GatePreviewDoesNotWrite`.
- [ ] Проверка «по сути» живёт в планировщике, а не в построении превью:
      тот же тест сверяет, что отказ без `confirmed` остаётся отказом с ним.

## 3. Повтор (идемпотентность)

- [ ] Повтор объявлен: `TestConsent_EveryChangeDeclaresItsRepeat`. Одно из:
      мир `consentWorlds` с `alreadyFor` и вызов в `parityArgs` — ответ
      `already_<итог>` без вопроса и без записи; либо строка
      `repeatIsANewChange` с дословной фразой из описания инструмента
      (каждая отправка — новое изменение).
- [ ] Удаление: повтор отвечает `already_deleted` —
      `TestAlreadyDeleted_EveryDeleteDeclared`,
      `TestAlreadyDeleted_RepeatedDeleteIsDeleted`.

## 4. Ось 2 — честный ход

- [ ] Статусы ответа объявлены в словарях `statusGoingOn` / `statusOver`;
      ответ «идёт» несёт `next_tool` и `next_args`:
      `TestProgress_ChangeGoingOnNamesTheNextStep`. Платформа дала id
      операции — следующий шаг `*_operation`; не дала — тот же вызов или
      чтение. Словарь `status` закрытый: сырое состояние платформы — только
      в поле `state`.
- [ ] Инструмент, который ждёт облако, имеет случай медленного мира в
      `mcpWaits`: `TestMCP_BudgetCoversTheWaits`,
      `TestMCP_NoCallOutlivesTheBudget`.
- [ ] `*_operation` читает ресурс своей операции:
      `TestOperation_ElapsedAndResourceStatus`.

## 5. Ось 3 — уборка

- [ ] Create объявляет созданное, его список и удаление:
      `TestCreated_EveryCreateDeclared`.
- [ ] Сбои запроса разобраны: обрыв до и после отправки, `429`, `5xx`,
      отказ при достигнутой цели — `TestFault_MatrixCoversTheRegistry`.

## 6. Ось 4 — полнота

- [ ] Превью называет всё, что evo шлёт, и проект записи:
      `TestPreview_NamesWhatTheWriteSends`,
      `TestPreviewProject_NamesWhereTheWriteLands`.
- [ ] Человеческий вывод CLI называет каждое значение превью:
      `TestPreview_HumanShowsEveryValue`.
- [ ] Цена — число только после пробы цены или со ссылкой на тариф:
      `TestContract_EveryPreviewPricedOrListed`.
- [ ] Имена из облака помечены недоверенными: `TestUntrusted_FieldsMarked`.

## 7. Опись запросов

- [ ] Запрос и поля тела — в `scripts/api-contract.json` в том же PR; мир
      паритета шлёт запрос по HTTP: `TestContract_WorldBodiesInTheInventory`.
- [ ] Эталон полей: в `scripts/api-contract-fields.json` вставляется только
      блок нового запроса; файл целиком не перегенерируется.

## 8. Бюджет и документы

- [ ] `tools/list` в бюджете: `TestToolsList_SizeBudget`,
      `TestToolsList_EachToolWithinItsBudget`; рост назван в описании PR
      строкой «tools/list: рост — <причина>».
- [ ] Числа инструментов в рукописных документах руками не правятся:
      `TestDocsExamples_Counts` держит круглые; точное — в генерируемых.
- [ ] Фрагмент `changelog.d/<тема>.md`; заголовок PR — «(продукт, <версия>)».

## 9. Пока без теста — сверяют ревью и аудит эталона

Из аудита эталона `vm_start` и заключения «Риска» 123 (12.11). Пункт
переезжает в свой раздел выше, когда появляется тест.

- [ ] На каждую строку таблицы «состояние → ответ» из ТЗ есть мир; сверка
      идёт по таблице, а не по списку тестов (D99, П2).
- [ ] На каждое состояние, с которым ответ отдаёт `next_tool`, есть тест
      «шаг не пишет».
- [ ] Шаг ожидания в `next_tool` — чтение или тот же вызов без `confirmed`
      в `next_args` (R-85).
- [ ] Ожидание различает «не прочитал» и «ресурса нет»: ответ «не найден»
      кончает ожидание.
- [ ] Операция без различимого целевого состояния (перезагрузка) имеет
      признак «переход увиден» и объявлена как «повтора нет» (раздел 3).
- [ ] Цена в превью — машинными полями, а не только текстом.
- [ ] Тексты отказа — из слов действия, а не из имени команды платформы.
- [ ] Останавливающая или необратимая операция: решение о `destructiveTools`
      и о строгом разрешении имени ресурса — в ТЗ, до кода.
