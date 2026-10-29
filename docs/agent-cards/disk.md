# evo: the `disk` group

<!-- Generated from the tool registry of evo: 'evo agent skill --group disk' prints it. -->

7 tools. Consent, exit codes and untrusted answers: the rules of the `evo` skill. The arguments of a tool: `evo tools describe <tool> --json`.

| Tool | Command | Does | Ids from |
|---|---|---|---|
| `list_disk_types` | `evo disk types` | reads |  |
| `disk_list` | `evo disk list` | reads |  |
| `disk_create` | `evo disk create` | creates: id in `disk.id`; found with `disk_list`; deleted with `disk_delete` |  |
| `disk_attach` | `evo disk attach` | changes | `disk_id` ← `disk_list` |
| `disk_detach` | `evo disk detach` | changes | `disk_id` ← `disk_list` |
| `disk_resize` | `evo disk resize` | changes | `disk_id` ← `disk_list` |
| `disk_delete` | `evo disk delete` | deletes; a repeat answers `already_deleted` | `disk_id` ← `disk_list` |
