### Исправлено

- MCP: `notifications/initialized`, `initialized` и `notifications/cancelled`,
  присланные с `id`, получают ответ `{}` (JSON-RPC 2.0, §4: запрос с `id` —
  не уведомление). Раньше ответа не было, и клиент, который его ждёт,
  зависал. Без `id` — по-прежнему без ответа.
