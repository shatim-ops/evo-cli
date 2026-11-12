### Безопасность

- **Текст ошибки операции помечен недоверенным у каждого `*_operation`.**
  Ответы `ar_operation`, `cert_operation`, `dns_operation`, `kafka_operation`,
  `lb_operation`, `pg_operation`, `redis_operation` и `vpc_operation` несут
  `"_untrusted": ["error"]`, когда в них есть поле `error`: это текст сервиса с
  именами ресурсов проекта, которые пишет любой, у кого есть доступ к проекту.
  Раньше пометку ставил только `k8s_operation`. Остальные поля ответа, `reason`
  и коды выхода не меняются.
