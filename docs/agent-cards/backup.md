# evo: the `backup` group

<!-- Generated from the tool registry of evo: 'evo agent skill --group backup' prints it. -->

4 tools. Consent, exit codes and untrusted answers: the rules of the `evo` skill. The arguments of a tool: `evo tools describe <tool> --json`.

| Tool | Command | Does | Ids from |
|---|---|---|---|
| `backup_list` | `evo backup list` | reads |  |
| `backup_get` | `evo backup get` | reads | `backup_id` ← `backup_list` |
| `backup_create` | `evo backup create` | creates: id in `backup_id`; found with `backup_list`; deleted with `backup_delete` | `vm_id` ← `vm_list`, `disk_id` ← `disk_list` |
| `backup_delete` | `evo backup delete` | deletes; a repeat answers `already_deleted` | `backup_id` ← `backup_list` |

Filters of the lists (the rule of the `evo` skill): `backup_list` — state, name, resource_type, resource_id, ids, states.
