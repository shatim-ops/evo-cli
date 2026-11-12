### Добавлено

- **`k8s_operation` (`evo k8s operation <operation_id>`) — ход асинхронной
  операции Managed Kubernetes.** Создание и удаление кластера и создание
  группы узлов возвращают `operation_id`; раньше прочитать операцию было
  нечем, оставалось опрашивать `k8s_get` и `k8s_nodepools`. Ответ — как у
  остальных `*_operation`: `done`, `error`, `elapsed_sec`, `resource_status`
  (состояние кластера; для группы узлов и плагина — `not_found` с
  пояснением), `--wait-seconds` ждёт завершения. Только чтение.
