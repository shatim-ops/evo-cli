# evo: the `ar` group

<!-- Generated from the tool registry of evo: 'evo agent skill --group ar' prints it. -->

5 tools. Consent, exit codes and untrusted answers: the rules of the `evo` skill. The arguments of a tool: `evo tools describe <tool> --json`.

| Tool | Command | Does | Ids from |
|---|---|---|---|
| `ar_list` | `evo ar list` | reads |  |
| `ar_get` | `evo ar get` | reads |  |
| `ar_create` | `evo ar create` | creates: id in `registry_id`; found with `ar_list`; deleted with `ar_delete` |  |
| `ar_delete` | `evo ar delete` | deletes; a repeat answers `already_deleted` |  |
| `ar_operation` | `evo ar operation` | reads |  |

A change of this group that answers an `operation_id` runs on: wait for it with `ar_operation` and its `wait_seconds`, not by polling a list.
