# evo: the `nlb` group

<!-- Generated from the tool registry of evo: 'evo agent skill --group nlb' prints it. -->

9 tools. Consent, exit codes and untrusted answers: the rules of the `evo` skill. The arguments of a tool: `evo tools describe <tool> --json`.

| Tool | Command | Does | Ids from |
|---|---|---|---|
| `lb_list` | `evo lb list` | reads |  |
| `lb_get` | `evo lb get` | reads | `id` ← `lb_list` |
| `lb_operation` | `evo lb operation` | reads |  |
| `tg_list` | `evo tg list` | reads |  |
| `tg_get` | `evo tg get` | reads | `id` ← `tg_list` |
| `tg_create` | `evo tg create` | creates: id in `tg_id`; found with `tg_list`; deleted with `tg_delete` | `vm_id` ← `vm_list`, `targets` ← `vm_list` |
| `tg_delete` | `evo tg delete` | deletes; a repeat answers `already_deleted` | `id` ← `tg_list` |
| `lb_create` | `evo lb create` | creates: id in `lb_id`; found with `lb_list`; deleted with `lb_delete` | `target_group_id` ← `tg_list`, `vpc_id` ← `vpc_list` |
| `lb_delete` | `evo lb delete` | deletes; a repeat answers `already_deleted` | `id` ← `lb_list` |
