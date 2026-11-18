### Безопасность

- **Чтение операции помечает id её ресурса как недоверенный.** `ar_operation`, `dns_operation`, `k8s_operation`,
  `lb_operation`, `kafka_operation`, `pg_operation`, `redis_operation`, `vpc_operation` (и `--json` их команд):
  поле `resource_id` / `resourceId` — текст облака — теперь названо в `_untrusted` рядом с `error`. Остальные поля,
  коды выхода и `reason` не меняются.
