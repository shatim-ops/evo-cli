# evo: the `vm` group

<!-- Generated from the tool registry of evo: 'evo agent skill --group vm' prints it. -->

11 tools. Consent, exit codes and untrusted answers: the rules of the `evo` skill. The arguments of a tool: `evo tools describe <tool> --json`.

| Tool | Command | Does | Ids from |
|---|---|---|---|
| `provision` | `evo vm create` | creates: id in `vm_id`; found with `vm_list`; deleted with `destroy` | `image` ← `image_list`, `image_id` ← `image_list` |
| `vm_get` | `evo vm get` | reads | `vm_id` ← `vm_list` |
| `vm_history` | `evo vm history` | reads |  |
| `vm_console` | `evo vm console` | reads | `vm_id` ← `vm_list` |
| `vm_start` | `evo vm start` | changes | `vm_id` ← `vm_list` |
| `vm_stop` | `evo vm stop` | changes | `vm_id` ← `vm_list` |
| `vm_reboot` | `evo vm reboot` | changes | `vm_id` ← `vm_list` |
| `vm_resize` | `evo vm resize` | changes | `vm_id` ← `vm_list` |
| `vm_list` | `evo vm list` | reads |  |
| `list_zones` | `evo vm zones` | reads |  |
| `list_flavors` | `evo vm flavors` | reads |  |

Filters of the lists (the rule of the `evo` skill): `vm_list` — zone, state, name, flavor, public_ip, images, ids, states; `list_flavors` — name, cpu, ram, share, zone, gpu, spot.

## Notes

- Use `deploy` for Compose. For `provision`, the default flavor is the smallest lowcost flavor outside free-tier. The region defaults to CLOUDRU_REGION, the subnet to the zone's, and the boot disk to 10 GB or the image minimum.
- Use `image_get` for fields_required. Supply missing mandatory image_metadata; supplied fields override evo's SSH key, VM name, login and generated password. Unknown fields, an unavailable image or missing required inputs are refused before creation.
- Ports use TCP by default and open from cidr in the shared security group; `destroy` removes the rules it created. Keep the returned SSH details and state_dir (key and state). For wait=true, allow the client enough time to wait for the VM.
