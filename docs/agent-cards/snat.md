# evo: the `snat` group

<!-- Generated from the tool registry of evo: 'evo agent skill --group snat' prints it. -->

3 tools. Consent, exit codes and untrusted answers: the rules of the `evo` skill. The arguments of a tool: `evo tools describe <tool> --json`.

| Tool | Command | Does | Ids from |
|---|---|---|---|
| `snat_list` | `evo snat list` | reads |  |
| `snat_create` | `evo snat create` | creates: id in `nat_gateway.id`; found with `snat_list`; deleted with `snat_delete` |  |
| `snat_delete` | `evo snat delete` | deletes; a repeat answers `already_deleted` | `id` ← `snat_list` |
