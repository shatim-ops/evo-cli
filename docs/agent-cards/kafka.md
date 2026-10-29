# evo: the `kafka` group

<!-- Generated from the tool registry of evo: 'evo agent skill --group kafka' prints it. -->

7 tools. Consent, exit codes and untrusted answers: the rules of the `evo` skill. The arguments of a tool: `evo tools describe <tool> --json`.

| Tool | Command | Does | Ids from |
|---|---|---|---|
| `kafka_list` | `evo kafka list` | reads |  |
| `kafka_get` | `evo kafka get` | reads | `cluster_id` ← `kafka_list` |
| `kafka_versions` | `evo kafka versions` | reads |  |
| `kafka_specs` | `evo kafka specs` | reads |  |
| `kafka_create` | `evo kafka create` | creates: id in `cluster_id`; found with `kafka_list`; deleted with `kafka_delete` | `subnet_id` ← `list_subnets` |
| `kafka_delete` | `evo kafka delete` | deletes; a repeat answers `already_deleted` | `cluster_id` ← `kafka_list` |
| `kafka_operation` | `evo kafka operation` | reads |  |

A change of this group that answers an `operation_id` runs on: wait for it with `kafka_operation` and its `wait_seconds`, not by polling a list.
