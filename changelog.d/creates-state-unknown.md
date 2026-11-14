### Исправлено

- **Создания PostgreSQL, Kafka, Redis и Kubernetes не молчат о состоянии ресурса и помечают его как текст облака.** Это `pg_create`, `kafka_create`, `redis_create`, `pg_backup_create`, `k8s_create`, `k8s_add_nodepool` и `k8s_add_addon`; в CLI — их команды с `--json`.
  - Раньше в ответе не было ни `resource_status`, ни `resource_status_error`, если облако не назвало id ресурса или чтение нашло ресурс без состояния. Теперь в этих случаях ответ несёт `resource_status_error: API_ERROR` и заметку `resource_status_note`: операция идёт, её чтение скажет, чем она кончится, создавать ресурс повторно не нужно.
  - `resource_status` — слово облака. Теперь оно названо в `_untrusted` — и в ответах созданий, и в ответах `*_operation`. У `k8s_add_addon` туда же попали `namespace` и `note`: пространство имён берётся из каталога плагинов.

  Поля добавлены, прежние не меняются.
