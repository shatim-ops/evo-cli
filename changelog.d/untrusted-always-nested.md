### Безопасность

- **Метка `_untrusted` у любого ответа называет и `warning`, и поле, внутри
  которого лежит поле с текстом облака.** Было: одиночное `warning`
  (`image_list`, `list_images`) и вложенные `result.error` и
  `result.warnings` (`disk_attach`, `disk_detach`) шли без метки. Стало:
  `_untrusted` называет `warning` и поле верхнего уровня, внутри которого
  на любой глубине есть `error`, `hint`, `warning`, `warnings`,
  `failures`, `failed` или `preview`.
- **Строка вывода нагрузки в ходе `deploy` не выглядит строкой evo.** Было:
  последняя строка `docker compose up` шла в ход (терминал,
  `notifications/progress`, `log`) как этап evo — `==> <строка>`. Стало:
  `    compose-up: "<строка>"` — с именем этапа и в кавычках.
