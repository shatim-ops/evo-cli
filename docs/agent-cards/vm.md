# evo: the `vm` group

<!-- Generated from the tool registry of evo: 'evo agent skill --group vm' prints it. -->

5 tools. Consent, exit codes and untrusted answers: the rules of the `evo` skill. The arguments of a tool: `evo tools describe <tool> --json`.

| Tool | Command | Does | Ids from |
|---|---|---|---|
| `provision` | `evo vm create` | creates: id in `vm_id`; found with `vm_list`; deleted with `destroy` | `image` ← `image_list`, `image_id` ← `image_list` |
| `vm_get` | `evo vm get` | reads | `vm_id` ← `vm_list` |
| `vm_list` | `evo vm list` | reads |  |
| `list_zones` | `evo vm zones` | reads |  |
| `list_flavors` | `evo vm flavors` | reads |  |
