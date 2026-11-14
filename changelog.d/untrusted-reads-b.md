### Исправлено

- **Чтения баз, Kubernetes и секретов помечают текст облака как недоверенный.** Ответы двенадцати чтений теперь называют в `_untrusted` поля, где стоит текст облака:
  - `metrics_history` — `series`;
  - `pg_specs` — `specifications`;
  - `k8s_addons` — `addons`;
  - `k8s_addon_versions` — `display_name`;
  - `secret_add_version` — `version`;
  - `k8s_plan` и `k8s_nodepool_get` — `flavor`;
  - `pg_versions`, `kafka_versions`, `redis_versions` — `versions`;
  - `pg_databases` — `databases`;
  - `pg_users` — `users`.
- **Подсказка «не найден» у `k8s_addon_versions` и `k8s_add_addon` берёт похожие имена из каталога в кавычки.** Перевод строки в имени не читается как слова evo.

  Поле `_untrusted` добавлено, прежние поля не меняются.
