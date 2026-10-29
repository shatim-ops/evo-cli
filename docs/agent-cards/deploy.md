# evo: the `deploy` group

<!-- Generated from the tool registry of evo: 'evo agent skill --group deploy' prints it. -->

9 tools. Consent, exit codes and untrusted answers: the rules of the `evo` skill. The arguments of a tool: `evo tools describe <tool> --json`.

| Tool | Command | Does | Ids from |
|---|---|---|---|
| `deploy` |  | creates: id in `vm_id`; deleted with `destroy` | `image` ← `image_list`, `image_id` ← `image_list` |
| `status` |  | reads |  |
| `list` | `evo deploy list` | reads |  |
| `logs` |  | reads |  |
| `destroy` | `evo vm delete` | deletes; a repeat answers `already_deleted` |  |
| `deploy_plan` | `evo deploy plan` | reads | `image` ← `image_list` |
| `deploy_apply` | `evo deploy apply` | creates: id in `vm_id`; deleted with `destroy` |  |
| `open_ports` | `evo vpc open-ports` | changes |  |
| `close_ports` | `evo vpc close-ports` | deletes; a repeat answers `already_deleted` |  |
