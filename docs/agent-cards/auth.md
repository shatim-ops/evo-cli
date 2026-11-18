# evo: the `auth` group

<!-- Generated from the tool registry of evo: 'evo agent skill --group auth' prints it. -->

4 tools. Consent, exit codes and untrusted answers: the rules of the `evo` skill. The arguments of a tool: `evo tools describe <tool> --json`.

| Tool | Command | Does | Ids from |
|---|---|---|---|
| `agent_skill` | `evo agent skill` | reads |  |
| `whoami` |  | reads |  |
| `doctor` |  | reads |  |
| `history` |  | reads |  |

## Notes

- `history` outcomes: created, deleted, already_deleted, changed, error, unknown, found, not_found. `retired_through`, when set, is the last day pruned (90 days, 64 MB): of it and before only what is live is kept.
- `history` with `check` writes nothing. `would_close`: a read by id with the call's key answers «not found» — removed, or this key no longer sees it. `check_result` of the rest: `there`, `read_failed` (403, 429, 5xx: not checked), `other_key` (check with that key), `no_key`, `no_read_by_id` (read it with the tool of `check`), `not_by_id` (an outcome not known: read by the name), `no_project`, `in_parent`, `too_new` (made under 10 minutes ago), `later_line`, `over_budget` (call again).
