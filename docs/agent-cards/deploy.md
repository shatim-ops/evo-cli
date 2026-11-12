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

## Notes

- `deploy` reaches the VM over SSH: outbound port 22 must be open from the machine evo runs on.
- `deploy` refuses the compose file of another deployment of its directory; the VM is `evo-<project_name>`: the project name of evo's configuration, else the directory's name.
- One `deploy` at a time in a directory: another answers `STATE_LOCKED`; wait for the first.
- The defaults of `deploy_plan` (flavor, zone, image) come from evo's configuration; the plan names what it picked.
- `destroy` answers status `partial` (an error) when Cloud.ru accepted the deletion but did not confirm it, and over MCP `deleting` with `next_tool` when it still runs at ~45 s.
