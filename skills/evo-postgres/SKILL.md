---
name: evo-postgres
description: Create a Managed PostgreSQL cluster of a specification the person picks on Cloud.ru Evolution with evo, add a user and a database, hand over a connection string without the password, and delete the cluster. Use for "set up PostgreSQL", "a small Postgres", "create a database and a user", "give me the connection string", "delete the cluster".
license: Apache-2.0
metadata:
  evo-version: "1.5"
  evo-mode: human-in-the-loop
---

# evo-postgres: PostgreSQL with a user and a database

The rules of evo are in the built-in skill `evo`; if it is missing, ask the
person to run `evo agent setup --client <client> --skill`. Long waits and
delete answers: skill `evo-scenarios`, sections 4 and 5.

## Use this skill when

- The person wants a Managed PostgreSQL cluster, a user or a database in
  it, a connection string, or the cluster deleted.
- Not for PostgreSQL inside a VM or a compose project: use `evo-deploy`.

## Prerequisites

- `evo doctor --json` (tool `doctor`, group `auth`) passes.
- Tool groups `pg,vpc,auth`. The default of `evo agent setup` has no `pg`:
  ask the person to add it.
- The subnet: by default the main subnet of the profile's zone
  (`region`); the cluster goes to that zone. Another zone: `evo vpc
  subnets --json` / `list_subnets`, then `subnet_id`. A zone without a
  subnet of the project is `USAGE` before the question, with `missing`
  naming `subnet_id`.

## Procedure

1. Versions: `evo pg versions --json` / `pg_versions`. Specifications:
   `evo pg specs --version 17 --json` / `pg_specs` with `version`. The size
   is the person's: state what the workload needs (cpu, memory, storage,
   nodes) and where that comes from — the product's documentation, the
   person, your own estimate said as such — then show the person two or
   three specifications of the list that meet it, and let them pick (skill
   `evo-scenarios`, section 7). `min_storage_gb` is a specification's least
   storage. evo has no PostgreSQL prices: do not name one; the person
   compares on the page of `tariff_url`.
2. Preview: `evo pg create <name> --specification-id <id> --dry-run --json`
   / `pg_create` with `name`, `specification_id`, and `version`,
   `storage_gb`, `subnet_id` if needed, no `confirmed`. It names the
   version, cpu, memory, storage, nodes, subnet, the initial database and
   backups. evo gives no price for PostgreSQL: the preview has `tariff_url`,
   the Cloud.ru tariff page. A taken name, or storage below the
   specification's minimum, is `USAGE` before any question.
3. Stop and ask: show the person the configuration, storage, subnet and
   zone, that automatic backups are on (they cannot be turned off later;
   `no_backup` only at creation), and the tariff link. Go on only after
   their explicit yes.
4. Create: `evo pg create <name> --specification-id <id> --yes --json` /
   `pg_create` with `confirmed: true`. It returns `operation_id`.
5. Wait: `evo pg operation <operation-id> --wait-seconds 300 --json` /
   `pg_operation` with `operation_id` and `wait_seconds: 300`, again while
   `done` is false. Then find the cluster by name in `evo pg list --json` /
   `pg_list` and read it: `evo pg get <cluster-id> --json` / `pg_get`
   (`status`, `connection_string`).
6. User: first check `evo pg users <cluster-id> --json` / `pg_users`: the
   preview does not check that the name is free. The password comes from
   the person, or you generate one and hand it to them outside the chat.
   Preview `evo pg user-create <cluster-id> <user> --password-file <file>
   --dry-run --json` / `pg_user_create` with
   `cluster_id`, `name`, `password`; stop and ask; then `--yes` /
   `confirmed: true`. In the CLI pass the password with `--password-file`
   (a file or `-` for stdin), never on the command line. evo never echoes
   it back.
7. Database: first check `evo pg databases <cluster-id> --json` /
   `pg_databases`: the preview does not check that the name is free. Preview
   `evo pg db-create <cluster-id> <db> --owner <user> --dry-run --json` /
   `pg_db_create` with `cluster_id`, `name`, `owner`; stop and ask; then
   `--yes` / `confirmed: true`.
8. Hand over: the `connection_string` with the user and the database, and
   no password in it; say where the password is.

## Stop and ask

- Before every call with `confirmed: true` or `--yes`: `pg_create`,
  `pg_user_create`, `pg_db_create`, `pg_delete`. Show the preview, wait for
  the person's yes.
- Before `pg_delete`: the preview warns that the databases and all backups
  go and cannot be restored; say it in your own words too.

## Waiting

- A cluster takes minutes. One `pg_operation` call waits up to 300 s;
  repeat while `done` is false.
- Read the state with `pg_get`, not by scanning `pg_list`.
- After 20 waits with no change of state, or a cluster still `creating`
  well past the usual minutes, stop and ask the person.

## If it fails or is interrupted

| `reason` | What to do |
|---|---|
| `USAGE` with `existing_id` | the name is taken: pick another, or use that cluster |
| `USAGE` (storage, nodes) | below the specification's minimum or above its nodes: fix, preview again |
| `API_RATE_LIMITED` on create (`ResourceExhausted`, DB-23) | the zone has no capacity now: offer the person to try later or another zone (`subnet_id` of a subnet in another zone); do not repeat the call in a loop |
| `API_ERROR` on create | read the message; check `pg_list` before any new create |
| `pg_operation` with `error` | the operation failed: `pg_get` for the state, then ask the person |

After an interruption find the cluster by name in `pg_list`. Never create
it again before you looked.

## Cleanup

1. Preview: `evo pg delete <cluster-id> --dry-run --json` / `pg_delete`
   with `id` and no `confirmed`: it warns that the data and all backups go.
2. Stop and ask (see above).
3. `evo pg delete <cluster-id> --yes --json` / `pg_delete` with
   `confirmed: true`; it returns `operation_id`; wait with `pg_operation`
   until `done`.
4. Verify: the cluster is gone from `pg_list`, and `pg_get` answers
   `NOT_FOUND`; a repeated `pg_delete` answers `already_deleted`.
5. `pg_backups` of a deleted cluster answers an error (DB-22): not a fault
   of yours; deleted backups are not billed.

## Known platform limits

- DB-21: a cluster in FAILED cannot be deleted through the API; evo
  answers the error with `resource_status` and a hint: the console or
  Cloud.ru support with the id. Do not repeat the delete and do not report
  success: tell the person that way.
- A cluster `pg_get` reads as ERASED or DELETED is gone: `pg_delete`
  answers `already_deleted` without a question.
- DB-22: listing the backups of a deleted cluster answers an error.
- DB-23: a zone without capacity refuses `pg_create` with
  `API_RATE_LIMITED` (`ResourceExhausted`): another zone or later.
- Backups are set at creation and cannot be turned on or off later.

## Never

- Never pass `confirmed: true` or `--yes` before the person saw the preview
  and said yes.
- Never put a password in your reply, a command line or a log.
- Never call `pg_create` again after an interruption before checking
  `pg_list`.
- Never treat an answer to a delete of a FAILED cluster as success.
