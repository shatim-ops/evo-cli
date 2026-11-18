### Безопасность

- **Отказы и превью не повторяют введённые значения.** Было: введённое печаталось как есть, в том числе с `--json`:
  - `evo help <слово>` — «Unknown help topic [`<слово>`]»;
  - `evo tools call <имя>` и `evo tools describe <имя>` — «unknown tool "<имя>"»;
  - `evo mcp --http <адрес>` — «invalid --http address "<адрес>"»;
  - `history --since <значение>` (и `evo history`) — «since "<значение>" is not a time»;
  - превью `k8s_kubectl` (`evo k8s kubectl`) — `kubectl "<первое слово>" changes the cluster`;
  - отказ флага подключения kubectl и политика MCP по HTTP — флаг целиком, со значением (`--token=…`, `--password=…`).

  Стало:
  - тема справки названа местом («Unknown help topic (argument 1).», код выхода 0, как прежде);
  - неизвестный инструмент — «unknown tool» с подсказкой `evo tools list`;
  - адрес и дата — без значения;
  - глагол kubectl называется, только если это команда самого kubectl, иначе «This kubectl command changes the cluster»;
  - флаг kubectl назван именем без значения (`--token`).

  `reason` и коды выхода не меняются.
