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

Filters of the lists (the rule of the `evo` skill): `image_list` — query, type, zone.

## Notes

- `image_create` takes a detached disk: one still attached to a stopped VM answers `invalid_disk_state_to_create_image`.
- An image builds at about 0.7 GB a minute.
- An entry of `image_create`'s `image_metadata` (the write schema, not `image_get`'s): `field_type` is public_key, linux_password, linux_login, hostname or flag; public_key and linux_password in one `mandatory_group` (`authentication`) let either give access.
- Keep an image's `description` to the product, its pinned version and digest: legal text belongs in the Marketplace listing.
