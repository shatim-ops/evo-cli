# evo: the `ca` group

<!-- Generated from the tool registry of evo: 'evo agent skill --group ca' prints it. -->

7 tools. Consent, exit codes and untrusted answers: the rules of the `evo` skill. The arguments of a tool: `evo tools describe <tool> --json`.

| Tool | Command | Does | Ids from |
|---|---|---|---|
| `ca_list` | `evo ca list` | reads |  |
| `ca_get` | `evo ca get` | reads |  |
| `ca_deploy` | `evo ca deploy` | creates: known by its `name`; found with `ca_list`; deleted with `ca_delete` |  |
| `ca_delete` | `evo ca delete` | deletes; a repeat answers `already_deleted` |  |
| `ca_logs` | `evo ca logs` | reads |  |
| `ca_start` | `evo ca start` | changes |  |
| `ca_stop` | `evo ca stop` | changes |  |
