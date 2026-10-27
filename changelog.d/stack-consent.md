### Миграция

- **`stack_destroy` с `confirmed=true` (MCP) задания, удалённого раньше и
  без незавершённых удалений, — `status: already_destroyed` без повторного
  разбора, как превью и `evo --yes stack destroy`** (DECISIONS D47;
  инвариант «Процесса» `TestParity_YesEqualsConfirmed`, #405). Было: разбор
  заново и `status: destroyed`; код 0 и `isError: false` не меняются. Что
  поменять: скрипт, который ждал `destroyed` на повторном вызове, читает
  `already_destroyed`.
- **`evo stack apply` и MCP `stack_apply` без `--yes`/`confirmed` для
  задания, которого нет, — `NOT_FOUND`, код 4, до вопроса; для удалённого
  задания — `USAGE`, код 1, до вопроса** (DECISIONS D47; ось 1 CLAUDE.md
  §6, D35). Было: превью платного развёртывания и вопрос (код 3 без
  `--yes`), отказ — только после согласия. Что поменять: скрипт, который
  ждал код 3 на этом шаге, получает 4 или 1 и сообщение об ошибке.
