### Безопасность

- **Поля облака в ответах помечены недоверенными (`_untrusted`).** Раньше в ответе шли как слова evo:
  - `project_id` ресурса в превью удалений и в ответах создания — отмечен общей точкой, у любого инструмента;
  - время операции (`createdAt`, `updatedAt`, `created_at`, `updated_at`) у всех `*_operation`;
  - `version` и `zone` у `k8s_nodepool_get`, `created_at` и `type` у `backup_get`;
  - `next_page_token` у `audit_events`.

  Значения, коды выхода и `reason` не меняются.
