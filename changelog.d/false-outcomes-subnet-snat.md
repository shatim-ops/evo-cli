### Миграция

- **`evo vpc subnet-create` и `evo snat create` не отвечают `created`, пока
  Cloud.ru не создал подсеть или шлюз** (DECISIONS D111). Было: `status:
  created` сразу после запроса, хотя вложенная подсеть была `creating`, а
  шлюз — `CREATING`. Стало: evo ждёт, пока платформа завершит создание (до 2
  минут; в MCP — в пределах вызова). Дождался — `status: created`, как
  раньше. Не дождался — `status: creating` (`waiting`, если список ещё не
  показал ресурс или его состояние не из известных: не `created`,
  `updating`, `creating` и не ошибка), `subnet_id` и `subnet_state` (у шлюза —
  `nat_gateway_id` и `nat_gateway_state`) и `next_tool: list_subnets` (у
  шлюза — `snat_list`) с `next_args: {ids: [<id>]}`. Ресурс в ошибочном
  состоянии — `API_ERROR`, код 5, с id. Прерванное ожидание —
  `INTERRUPTED` с id и тем же следующим шагом. Что поменять: скрипт,
  который сверял `status == "created"`, принимает также `creating` и
  `waiting` и в этих случаях опрашивает `evo vpc subnets --ids <id>` или
  `evo snat list --ids <id>`.
