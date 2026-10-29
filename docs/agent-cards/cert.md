# evo: the `cert` group

<!-- Generated from the tool registry of evo: 'evo agent skill --group cert' prints it. -->

10 tools. Consent, exit codes and untrusted answers: the rules of the `evo` skill. The arguments of a tool: `evo tools describe <tool> --json`.

| Tool | Command | Does | Ids from |
|---|---|---|---|
| `cert_list` | `evo cert list` | reads |  |
| `cert_get` | `evo cert get` | reads | `id` ← `cert_list` |
| `cert_issue_le` | `evo cert issue-le` | creates: id in `certificate_id`; found with `cert_list`; deleted with `cert_delete` |  |
| `cert_import` | `evo cert import` | creates: id in `certificate.id`; found with `cert_list`; deleted with `cert_delete` |  |
| `cert_renew` | `evo cert renew` | changes | `certificate_id` ← `cert_list` |
| `cert_delete` | `evo cert delete` | deletes; a repeat answers `already_deleted` | `id` ← `cert_list` |
| `cert_usage` | `evo cert usage` | reads | `id` ← `cert_list` |
| `cert_versions` | `evo cert versions` | reads | `certificate_id` ← `cert_list` |
| `cert_private_key` | `evo cert private-key` | reveals a secret | `certificate_id` ← `cert_list` |
| `cert_operation` | `evo cert operation` | reads |  |

A change of this group that answers an `operation_id` runs on: wait for it with `cert_operation` and its `wait_seconds`, not by polling a list.
