### Изменено

- Описания 17 самых длинных инструментов MCP (`deploy_plan`, `provision`, `image_create`, `pg_create`,
  `redis_create`, `k8s_add_addon`, `ca_deploy`, `deploy`, `lb_create`, `kafka_create`, `subnet_create`,
  `k8s_helm`, `k8s_plan`, `marketplace_list`, `k8s_add_nodepool`, `destroy`, `image_list`) короче: в
  `tools/list` осталось только то, без чего вызов неверен или опасен; справочные факты перешли в карточки
  групп (`evo agent skill --group`). `tools/list` всех инструментов меньше на 11 748 байт (127 148 → 115 400);
  смысл, аргументы, коды выхода и `reason` не меняются. Русская справка команд сокращена так же.
