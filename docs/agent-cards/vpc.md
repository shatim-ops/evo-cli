# evo: the `vpc` group

<!-- Generated from the tool registry of evo: 'evo agent skill --group vpc' prints it. -->

13 tools. Consent, exit codes and untrusted answers: the rules of the `evo` skill. The arguments of a tool: `evo tools describe <tool> --json`.

| Tool | Command | Does | Ids from |
|---|---|---|---|
| `fip_attach` | `evo vpc fip-attach` | changes |  |
| `list_subnets` | `evo vpc subnets` | reads |  |
| `list_security_groups` | `evo vpc security-groups` | reads |  |
| `list_floating_ips` | `evo vpc fips` | reads |  |
| `release_floating_ip` | `evo vpc fip-release` | deletes; a repeat answers `already_deleted` | `id` ← `list_floating_ips` |
| `vpc_list` | `evo vpc list` | reads |  |
| `vpc_get` | `evo vpc get` | reads | `id` ← `vpc_list` |
| `vpc_create` | `evo vpc create` | creates: id in `vpc_id`; found with `vpc_list`; deleted with `vpc_delete` |  |
| `vpc_delete` | `evo vpc delete` | deletes; a repeat answers `already_deleted` | `id` ← `vpc_list` |
| `vpc_operation` | `evo vpc operation` | reads |  |
| `vpc_routes` | `evo vpc routes` | reads | `vpc_id` ← `vpc_list` |
| `vpc_route_create` | `evo vpc route-create` | creates: id in `route_id`; found with `vpc_routes`; deleted with `vpc_route_delete` | `vpc_id` ← `vpc_list`, `next_hop_vm_id` ← `vm_list` |
| `vpc_route_delete` | `evo vpc route-delete` | deletes; a repeat answers `already_deleted` | `vpc_id` ← `vpc_list` |

A change of this group that answers an `operation_id` runs on: wait for it with `vpc_operation` and its `wait_seconds`, not by polling a list.
