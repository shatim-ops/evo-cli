# evo: the `image` group

<!-- Generated from the tool registry of evo: 'evo agent skill --group image' prints it. -->

6 tools. Consent, exit codes and untrusted answers: the rules of the `evo` skill. The arguments of a tool: `evo tools describe <tool> --json`.

| Tool | Command | Does | Ids from |
|---|---|---|---|
| `image_list` | `evo image list` | reads |  |
| `marketplace_list` | `evo marketplace list` | reads |  |
| `list_images` |  | reads |  |
| `image_create` | `evo image create` | creates: id in `image.id`; found with `image_list`; deleted with `image_delete` | `disk_id` ← `disk_list` |
| `image_get` | `evo image get` | reads | `image` ← `image_list`, `image_id` ← `image_list` |
| `image_delete` | `evo image delete` | deletes; a repeat answers `already_deleted` | `image_id` ← `image_list` |
