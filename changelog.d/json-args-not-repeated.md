### Безопасность

- **Отказы по аргументам JSON не повторяют их текст.** `evo tools call --args-json`, `--args-file` и `tools/call` в
  MCP (stdio и HTTP).
  - Было: число вне диапазона (`27183650904512e999`, длинная десятичная запись) отказ декодера печатал как есть, до
    проверки типов и согласия. Неизвестное имя аргумента повторялось («unknown argument(s) <имя>», «has no argument
    "<имя>"»), хотя у `--arg` оно скрыто.
  - Стало: ошибка JSON названа видом и местом («not valid JSON (at byte N)», «a value that does not fit its JSON
    type… (at byte N)»), неизвестные аргументы — числом («1 unknown argument(s)»). Подсказка, как прежде, перечисляет
    аргументы инструмента.
  - `reason` (`USAGE`) и коды не меняются; MCP-код `-32602` для неразбираемых параметров прежний.

- **Пара `--meta` (`evo vm create`, `evo deploy plan`) и `--env` (`evo ca deploy`) без `=` не печатается.**
  - Было: «--meta "admin_password:<значение>": want key=value» — пара целиком, в том числе с `--dry-run` и в `--json`.
  - Стало: «--meta number N: want key=value», как у `--arg`. `reason` (`USAGE`) и код выхода не меняются.

- **Отказ по правилу схемы не повторяет значение** — id (UUID), CIDR и значение из закрытого списка, в CLI и MCP.
  - Было: «disk_id "<значение>" is not an id: it is a UUID», «status "<значение>" is not one of …» — значение
    целиком, до согласия.
  - Стало: «disk_id: the value is not an id: it is a UUID», «status: the value is not one of …»; допустимые значения
    по-прежнему перечислены. `reason` (`USAGE`) и коды не меняются.
