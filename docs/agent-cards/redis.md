# evo: the `redis` group

<!-- Generated from the tool registry of evo: 'evo agent skill --group redis' prints it. -->

7 tools. Consent, exit codes and untrusted answers: the rules of the `evo` skill. The arguments of a tool: `evo tools describe <tool> --json`.

| Tool | Command | Does | Ids from |
|---|---|---|---|
| `redis_list` | `evo redis list` | reads |  |
| `redis_get` | `evo redis get` | reads | `cluster_id` ← `redis_list` |
| `redis_versions` | `evo redis versions` | reads |  |
| `redis_specs` | `evo redis specs` | reads |  |
| `redis_create` | `evo redis create` | creates: id in `cluster_id`; found with `redis_list`; deleted with `redis_delete` | `subnet_id` ← `list_subnets` |
| `redis_delete` | `evo redis delete` | deletes; a repeat answers `already_deleted` | `cluster_id` ← `redis_list` |
| `redis_operation` | `evo redis operation` | reads |  |

A change of this group that answers an `operation_id` runs on: wait for it with `redis_operation` and its `wait_seconds`, not by polling a list.
