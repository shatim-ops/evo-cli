### Миграция

- **`evo ca start` и `evo ca stop`: операция платформы, завершённая
  ошибкой, — отказ, а не `ok`** (DECISIONS D111). Было: `status: ok`, код 0. Стало:
  `API_ERROR`, код 5. В тексте отказа — сообщение платформы и номер
  операции, код платформы — в `platform_code`. Что поменять: скрипт, который
  считал код 0 успехом запуска или остановки, теперь получает отказ и может
  на него реагировать.
- **`evo ca start` и `evo ca stop`: незавершённая операция — `starting` или
  `stopping`, а не `ok`** (DECISIONS D111). Было: `status: ok`, код 0, даже если Cloud.ru
  ещё не выполнил команду. Стало: `status: starting` (или `stopping`), код
  0, `app_status` — состояние приложения, `next_tool: ca_get` с
  `next_args: {name}`. Ответ без операции — `status: waiting`. Если
  операция завершена, ответ прежний — `ok`. Что поменять: скрипт, который
  сверял `status == "ok"`, принимает также `starting`, `stopping` и
  `waiting` и в этих случаях опрашивает `evo ca get`.
- **`evo disk create` без `--attach-to` ждёт, пока диск станет доступен, и
  не отвечает `created` за диск в `creating`** (DECISIONS D111). Было: `status: created`
  сразу после запроса, хотя вложенный `disk.status` был `creating`. Стало:
  evo ждёт состояния `available` так же, как перед подключением с
  `--attach-to` (до 5 минут; в MCP — в пределах вызова). Дождался —
  `status: created`, как раньше. Не дождался — `status: creating`,
  `disk_id`, `disk_state` и `next_tool: disk_list` с
  `next_args: {ids: [<id>]}`. Диск в ошибочном состоянии — `API_ERROR`,
  код 5, с `disk_id`. Прерванное ожидание (Ctrl-C, отмена вызова MCP) —
  `INTERRUPTED` (D69) с `disk_id`, `disk_state` и тем же следующим шагом;
  диск в облаке продолжает создаваться. Что поменять: скрипт, который сверял
  `status == "created"`, принимает также `creating` и в этом случае
  опрашивает `evo disk list --ids <id>`.
