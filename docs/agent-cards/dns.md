# evo: the `dns` group

<!-- Generated from the tool registry of evo: 'evo agent skill --group dns' prints it. -->

9 tools. Consent, exit codes and untrusted answers: the rules of the `evo` skill. The arguments of a tool: `evo tools describe <tool> --json`.

| Tool | Command | Does | Ids from |
|---|---|---|---|
| `dns_zones` | `evo dns zones` | reads |  |
| `dns_zone_get` | `evo dns zone-get` | reads | `id` ← `dns_zones` |
| `dns_zone_create` | `evo dns zone-create` | creates: id in `zone_id`; found with `dns_zones`; deleted with `dns_zone_delete` |  |
| `dns_zone_delete` | `evo dns zone-delete` | deletes; a repeat answers `already_deleted` | `id` ← `dns_zones` |
| `dns_operation` | `evo dns operation` | reads |  |
| `dns_records` | `evo dns records` | reads | `zone_id` ← `dns_zones` |
| `dns_record_get` | `evo dns record-get` | reads | `record_id` ← `dns_records` |
| `dns_record_create` | `evo dns record-create` | creates: id in `record_id`; found with `dns_records`; deleted with `dns_record_delete` | `zone_id` ← `dns_zones` |
| `dns_record_delete` | `evo dns record-delete` | deletes; a repeat answers `already_deleted` | `record_id` ← `dns_records` |

A change of this group that answers an `operation_id` runs on: wait for it with `dns_operation` and its `wait_seconds`, not by polling a list.
