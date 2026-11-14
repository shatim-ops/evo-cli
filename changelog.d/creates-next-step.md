### Изменено

- **Создания PostgreSQL, Kafka, Redis и Kubernetes называют следующий шаг и состояние ресурса, пока облако их делает.** Это `pg_create`, `kafka_create`, `redis_create`, `pg_backup_create`, `k8s_create`, `k8s_add_nodepool` и `k8s_add_addon`; в CLI — их команды с `--json`. Раньше ответ называл только `operation_id` и текст «опрашивайте…», и агент читал список вслепую. Теперь:
  - `next_tool` и `next_args` указывают на чтение операции (`pg_operation`, `kafka_operation`, `redis_operation`, `k8s_operation`), а оно по завершении называет состояние ресурса;
  - `resource_status` — состояние ресурса сразу после создания, тем же чтением. Копию и плагин evo ищет в списке кластера: отдельного чтения у них нет.

  Поля добавлены, прежние не меняются.
