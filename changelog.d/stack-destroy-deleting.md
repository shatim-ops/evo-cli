### Миграция

- **`evo stack destroy` и MCP `stack_destroy` отвечают `destroyed`, только
  когда облако удалило всё** (DECISIONS D96). Было: пока облако ещё удаляло
  Postgres, Kafka, Redis, кластер Kubernetes или балансировщик стека, ответ
  был `status: destroyed`, а эти ресурсы стояли и в `deleted`, и в
  `deleting`. Стало: `status: deleting`, код 0, такие ресурсы — только в
  `deleting`, следующий шаг — тот же `stack_destroy` с согласием; когда облако
  закончит, ответ — `destroyed`, затем `already_destroyed`. Что поменять:
  скрипт, который считал `destroyed` концом удаления, повторяет
  `evo stack destroy <job-id> --yes`, пока `status` — `deleting`. Задание,
  сохранённое 1.8.0-rc.1 и раньше со `destroyed` и непустым `deleting`,
  читается как `deleting`. `evo stack status` и MCP `stack_status` такого
  стека отвечают так же: `status: deleting`, список `deleting` и следующий
  шаг `stack_destroy` (было: без списка и шага, запись rc.1 — `destroyed`).
