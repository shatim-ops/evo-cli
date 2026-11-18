### Безопасность

- **Чтение операции помечает её собственный id как недоверенный.** `ar_operation`, `dns_operation`, `k8s_operation`,
  `lb_operation`, `kafka_operation`, `pg_operation`, `redis_operation`, `vpc_operation` (и `--json` их команд):
  поле `id` — id операции, как его прислал сервис, а не `operation_id` вызова — теперь названо в `_untrusted`
  рядом с `error` и id ресурса. `cert_operation` помечал его и прежде. Остальные поля, коды выхода и `reason`
  не меняются.
