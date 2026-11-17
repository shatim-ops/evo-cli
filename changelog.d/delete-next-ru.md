### Исправлено

- **Следующий шаг удаления с `wait=false` — по-русски в русском CLI.** Было: поле `next` ответа удаления, которое
  ещё идёт (`status: deleting`), — «… shows when the delete ends» на любом языке, у 13 инструментов (`ar_delete`,
  `backup_delete`, `ca_delete`, `dns_record_delete`, `dns_zone_delete`, `k8s_delete`, `k8s_nodepool_delete`,
  `kafka_delete`, `lb_delete`, `pg_delete`, `redis_delete`, `tg_delete`, `vpc_delete`). Стало: «… покажет, когда
  удаление закончится» в русском CLI. `--json` и ответы MCP — по-английски, как прежде; `next_tool`, `next_args`,
  `reason` и коды не меняются.
