# evo: the `pg` group

<!-- Generated from the tool registry of evo: 'evo agent skill --group pg' prints it. -->

15 tools. Consent, exit codes and untrusted answers: the rules of the `evo` skill. The arguments of a tool: `evo tools describe <tool> --json`.

| Tool | Command | Does | Ids from |
|---|---|---|---|
| `pg_list` | `evo pg list` | reads |  |
| `pg_get` | `evo pg get` | reads | `id` ← `pg_list`, `cluster_id` ← `pg_list` |
| `pg_versions` | `evo pg versions` | reads |  |
| `pg_specs` | `evo pg specs` | reads |  |
| `pg_create` | `evo pg create` | creates: id in `cluster_id`; found with `pg_list`; deleted with `pg_delete` | `subnet_id` ← `list_subnets` |
| `pg_delete` | `evo pg delete` | deletes; a repeat answers `already_deleted` | `id` ← `pg_list`, `cluster_id` ← `pg_list` |
| `pg_operation` | `evo pg operation` | reads |  |
| `pg_databases` | `evo pg databases` | reads | `cluster_id` ← `pg_list` |
| `pg_db_create` | `evo pg db-create` | creates: known by its `name`; deleted with `pg_db_delete` | `cluster_id` ← `pg_list` |
| `pg_db_delete` | `evo pg db-delete` | deletes; a repeat answers `already_deleted` | `cluster_id` ← `pg_list` |
| `pg_users` | `evo pg users` | reads | `cluster_id` ← `pg_list` |
| `pg_user_create` | `evo pg user-create` | creates: known by its `name`; deleted with `pg_user_delete` | `cluster_id` ← `pg_list` |
| `pg_user_delete` | `evo pg user-delete` | deletes; a repeat answers `already_deleted` | `cluster_id` ← `pg_list` |
| `pg_backups` | `evo pg backups` | reads | `cluster_id` ← `pg_list` |
| `pg_backup_create` | `evo pg backup-create` | creates: id in `backup_id`; found with `pg_backups` | `cluster_id` ← `pg_list` |

A change of this group that answers an `operation_id` runs on: wait for it with `pg_operation` and its `wait_seconds`, not by polling a list.

Filters of the lists (the rule of the `evo` skill): `pg_list` — status, name, ids, statuses; `pg_specs` — version; `pg_databases` — name; `pg_users` — name; `pg_backups` — status, name, ids, statuses.

## Notes

- A cluster made with `no_backup` refuses `pg_backup_create` and `pg_backups` with 'backup disabled'.
- The cost in the preview of `pg_create` is an estimate by the tariff of its date (`cost_note`), not a bill; the rates are on `tariff_url`.
- A zone with no capacity refuses `pg_create` with `API_RATE_LIMITED` (`ResourceExhausted`, DB-23): later or another zone (`subnet_id`), not the same call in a loop.
- A cluster in FAILED cannot be deleted through the API (DB-21): the answer is an error with `resource_status` and a hint (the console, Cloud.ru support), not a success; a repeat changes nothing.
- `pg_backups` of a deleted cluster answers an error (DB-22).
