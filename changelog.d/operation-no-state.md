### Исправлено

- **Чтения операций не молчат о ресурсе без состояния, а состояние из восстановленного создания помечено как текст облака.**
  - `*_operation` (`ar_operation`, `cert_operation`, `dns_operation`, `k8s_operation`, `kafka_operation`, `lb_operation`, `pg_operation`, `redis_operation`, `vpc_operation`): чтение могло найти ресурс без `status`/`state`/`phase`, и ответ тогда не называл ни `resource_status`, ни `resource_status_error`. Теперь у ресурса, которому сервис даёт состояние, ответ несёт `resource_status_error: API_ERROR` и заметку `resource_status_note`. У ресурса, которому спецификация состояния не даёт (сеть VPC, запись DNS; у сертификата состояние — объект `issueState`), — только заметку, без ошибки.
  - Создание, чей ответ потерялся и которое evo нашёл в списке по имени, переносит в ответ `resource_status` из списка. Теперь это поле названо в `_untrusted`, как в обычном ответе создания.

  Поля добавлены, прежние не меняются.
