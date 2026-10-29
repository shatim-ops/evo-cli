# evo: the `secret` group

<!-- Generated from the tool registry of evo: 'evo agent skill --group secret' prints it. -->

6 tools. Consent, exit codes and untrusted answers: the rules of the `evo` skill. The arguments of a tool: `evo tools describe <tool> --json`.

| Tool | Command | Does | Ids from |
|---|---|---|---|
| `secret_list` | `evo secret list` | reads |  |
| `secret_get` | `evo secret get` | reads |  |
| `secret_value` | `evo secret value` | reveals a secret |  |
| `secret_create` | `evo secret create` | creates: known by its `path`; found with `secret_list`; deleted with `secret_delete` |  |
| `secret_add_version` | `evo secret add-version` | changes |  |
| `secret_delete` | `evo secret delete` | deletes; a repeat answers `already_deleted` |  |
