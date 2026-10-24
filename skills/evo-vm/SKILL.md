---
name: evo-vm
description: Create a Cloud.ru Evolution VM with a public IP using evo, open or close its ports, attach or release a floating IP, and delete the VM. Use for "create a VM", "give me a server", "open port 8080", "close the port", "give the VM a public address", "delete the VM". Not for deploying a docker-compose app (use evo-deploy).
license: Apache-2.0
metadata:
  evo-version: "1.3"
  evo-mode: human-in-the-loop
---

# evo-vm: a VM with a public address and ports

The rules of evo are in the built-in skill `evo`; if it is missing, ask the
person to run `evo agent setup --client <client> --skill`. Long waits and
delete answers: skill `evo-scenarios`, sections 4 and 5.

## Use this skill when

- The person wants a bare VM, a port opened or closed, a public IP attached
  or released, or a VM deleted.
- Not for an app from a compose file: use `evo-deploy`.

## Prerequisites

- `evo doctor --json` (tool `doctor`, group `auth`) passes.
- Tool groups `vm,deploy,vpc,image,auth`: `open_ports`, `close_ports` and
  `destroy` are in the group `deploy`.
- The image by name: `evo image list ubuntu --json` / `image_list` with
  `query`. The zone comes from the profile or from `region`.
- Run evo in one working directory: the VM's deploy key and record live in
  its `.evo/`.

## Procedure

1. Preview: `evo vm create <name> --image Ubuntu-24.04 --dry-run --json` /
   `provision` with `name`, `image` and no `confirmed`. It names the flavor
   (default: the smallest lowcost, not free-tier), zone, image, disk, the
   security group and the monthly price. An image the project cannot use is
   `NOT_FOUND`; a zone without a subnet or a security group is
   `CONFIG_INVALID`, both before any question.
2. Do not pass ports to `provision` (`--port`): it has no `cidr`, so they
   open from `0.0.0.0/0`.
   Open them in step 5 with a source the person names.
3. Stop and ask: show the preview: what is created, the price, the zone,
   the public address. Go on only after their explicit yes.
4. Create: `evo vm create <name> --image Ubuntu-24.04 --yes --json` /
   `provision` with `confirmed: true`. It waits for the VM (up to 25
   minutes). Take `vm_id` and `vm_ip`. If the answer has `password`, it is
   the only copy: give it to the person and do not repeat it in the chat.
5. Open a port: ask the person which source may reach it (a CIDR such as
   `198.51.100.0/24`). Preview: `evo vpc open-ports <group> --port 8080
   --cidr <cidr> --dry-run --json` / `open_ports` with `security_group`,
   `ports`, `cidr` and no `confirmed`. The group is the one the preview of
   step 1 named, or the zone's group from `list_security_groups`. The
   preview names the group, the protocol, the source, `will_open` and
   `already_open`. Stop and ask: if the source is `0.0.0.0/0`, say so and
   that the group is shared by every VM of the zone; a separate yes. Then
   the same call with `--yes` / `confirmed: true`.
6. Close a port: `evo vpc close-ports <group> --port 8080 --cidr <cidr>
   --dry-run --json` / `close_ports` with the same `ports`, `cidr` and
   `protocol` as when it was opened. Only rules of exactly that port,
   protocol and source go. No such rule: `already_deleted`, no question.
   Otherwise stop and ask, then `--yes` / `confirmed: true`.
7. Public address: `evo vm get <name> --json` / `vm_get` gives `public_ip`
   and `private_ip`. A VM without one: pick a free address
   (`available: true`) from `evo vpc fips --json` / `list_floating_ips`,
   preview `evo vpc fip-attach <address> --vm <name> --dry-run --json` /
   `fip_attach` with `fip`, `vm`; stop and ask; then `--yes` /
   `confirmed: true`. An address that is not free, or a VM that has one
   already, is `USAGE` before the question; the address already on this VM
   is `already_attached`.

## Stop and ask

- Before every call with `confirmed: true` or `--yes`: `provision`,
  `open_ports`, `close_ports`, `fip_attach`, `release_floating_ip`,
  `destroy`. Show the preview, wait for the person's yes.
- `open_ports` from `0.0.0.0/0` needs its own yes, named as such.

## Waiting

- `provision` waits for the VM in the call. Over MCP watch
  `notifications/progress`.
- If `vm_get` shows the same state for 10 checks, stop and ask.

## If it fails or is interrupted

| `reason` | What to do |
|---|---|
| refused, naming a VM of that name | the name is taken, or it is the VM of an interrupted run still being created: check it with `vm_get`, then pick another name or use it |
| `NOT_FOUND` | the image, group, address or VM is not there: list again |
| `CONFIG_INVALID` | the zone has no subnet or security group: another zone, or ask the person |
| `VM_PROVISION_FAILED` | the answer names `vm_id`: tell the person, then delete it with their yes |
| exit code 130 | interrupted: the message names the VM |

After an interruption find the VM by name with `evo vm list --json` /
`vm_list` before creating again.

## Cleanup

1. Ports opened by hand with `open_ports`: close them with `close_ports`
   (step 6). Deleting the VM does not close them: `destroy` deletes only
   the rules evo opened with the VM itself.
2. Preview: `evo vm delete <vm_id> --dry-run --json` / `destroy` with
   `vm_id` and no `confirmed`: it names the VM, its floating IP and the
   rules that go with it.
3. Stop and ask, then `evo vm delete <vm_id> --yes --json` / `destroy` with
   `vm_id` and `confirmed: true`. The floating IP is released too
   (`release_ip`, default true).
4. Verify: `vm_get` answers `NOT_FOUND`; `list_floating_ips` no longer has
   the address; a repeated delete answers `already_deleted`.
5. A free address not needed: `evo vpc fip-release <id> --dry-run --json` /
   `release_floating_ip`; an address in use by a VM or a SNAT gateway is
   refused.

## Known platform limits

- G1: a security group is shared by the VMs of a zone; a rule opened for
  one VM is open for all of them.
- The generated password is returned once, in the create answer.

## Never

- Never pass `confirmed: true` or `--yes` before the person saw the preview
  and said yes.
- Never open a port from `0.0.0.0/0` without the person's separate yes.
- Never repeat a password from an answer in the chat.
- Never delete a VM found only by a name several VMs share: use `vm_id`.
