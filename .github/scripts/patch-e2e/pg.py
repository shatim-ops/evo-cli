"""One PostgreSQL 18 Standard cluster for the reviewed patch run."""
import json
import os
import hashlib
import secrets
import tempfile
import time
from urllib.parse import quote

import common


ZONE = 'ru.AZ-2'
CLUSTER = 'pg-cluster'


def require(condition, why):
    if not condition:
        raise RuntimeError('PG: ' + why)


def text(value):
    return (isinstance(value, str) and bool(value.strip()) and value not in ('.', '..')
            and all(c.isprintable() for c in value))


def marker():
    # A cleanup-only job has its own run number; durable receipts belong to the source run.
    run_id = os.environ['P90_SOURCE_RUN']
    require(run_id.isascii() and run_id.isdigit(), 'invalid run identity')
    return 'evo-p90-pg-' + run_id


def tool(tool_name, *, mode=None, **args):
    flags = [mode] if mode else []
    argv = ['tools', 'call', tool_name, '--json']
    for k, v in args.items():
        argv.extend(['--arg', f'{k}={v}'])
    return common.call_evo(*flags, *argv, timeout=50)


def rows(out, field):
    # Candidate clients walk service cursors; never interpret a CLI page or a missing field as an empty list.
    require(isinstance(out, dict) and isinstance(out.get(field), list), 'incomplete list')
    require(not any(out.get(k) for k in ('next_page_token', 'next_offset', 'next_cursor', 'truncated', 'has_more')), 'unread list continuation')
    require(all(isinstance(row, dict) for row in out[field]), 'invalid list row')
    return out[field]


def clusters():
    listed = rows(tool('pg_list'), 'clusters')
    require(all(text(row.get('id')) and text(row.get('name')) for row in listed), 'incomplete cluster identity')
    require(len({row['id'] for row in listed}) == len(listed), 'duplicate cluster identity')
    return listed


def preflight():
    name, project = marker(), os.environ['CLOUDRU_PROJECT_ID']
    require(text(project), 'missing project')
    require(all(c['name'] != name for c in clusters()), 'cluster name already in use')
    zones = [z for z in rows(tool('list_zones'), 'zones') if z.get('name') == ZONE and z.get('enabled') is True]
    require(len(zones) == 1 and text(zones[0].get('id')), 'zone unavailable')
    subs = [s for s in rows(tool('list_subnets'), 'subnets') if s.get('availability_zone_id') == zones[0]['id'] and text(s.get('id'))]
    default = [s for s in subs if s.get('name') == 'Default_' + ZONE]
    eligible = default or subs
    require(len(eligible) == 1, 'expected one existing subnet in the reviewed zone')
    specs = rows(tool('pg_specs', version='18'), 'specifications')
    specs = [s for s in specs if s.get('deployment_mode') == 'STANDARD' and s.get('cpu') == 1 and s.get('memory_gb') == 2 and s.get('min_storage_gb') == 10 and text(s.get('id'))]
    require(len(specs) == 1, 'reviewed small configuration unavailable or ambiguous')
    args = dict(name=name, version='18', specification_id=specs[0]['id'], instances=1, storage_gb=10, subnet_id=eligible[0]['id'])
    out = tool('pg_create', mode='--dry-run', **args)
    require(out.get('status') == 'dry_run' and out.get('preview', {}).get('action') == 'pg_create', 'missing creation preview')
    evidence = out['preview'].get('details', {})
    will, preview = evidence.get('will_create', {}), evidence.get('preview', {})
    expected = dict(args, cpu=1, memory_gb=2, deployment_mode='STANDARD', zone=ZONE, backup_enabled=True)
    require(all(will.get(k) == v for k, v in expected.items()), 'preview differs from reviewed size or subnet')
    require(preview.get('resource') == 'pg_cluster' and preview.get('project_id') == project and preview.get('zone', {}).get('name') == ZONE, 'preview project or zone differs')
    # The pinned pg_create request has no public IP setting; its access parameter explicitly discloses this.
    access = [p.get('value') for p in preview.get('params', []) if p.get('key') == 'access']
    require(len(access) == 1 and isinstance(access[0], str) and any(s in access[0] for s in ('evo sets up none', 'evo его не настраивает')) and not preview.get('public_ip'), 'public IP policy unverified')
    return args


def wait(until, read, description):
    while time.monotonic() < until:
        result = read()
        if result:
            return result
        time.sleep(min(8, max(0, until - time.monotonic())))
    raise RuntimeError('PG: deadline checking ' + description)


def operation(opid, until):
    require(text(opid), 'missing operation receipt')
    def finished():
        op = tool('pg_operation', operation_id=opid)
        require(op.get('id') == opid and op.get('error') is None and op.get('status') != 'failed', 'operation failed or identity differs')
        return op if op.get('done') is True else None
    return wait(until, finished, 'operation')


def target_id(opid, provisional, until):
    op = operation(opid, until)
    resolved = op.get('resource_id')
    require(not (resolved and provisional and resolved != provisional), 'operation and receipt target differ')
    target = resolved or provisional
    require(text(target), 'no resource identity in create receipt or operation')
    return target


def path(cid, suffix=''):
    require(text(cid), 'invalid target identity')
    return '/v1/clusters/' + quote(cid, safe='') + suffix


def cluster_gone(code, body):
    if code != 404 or not isinstance(body, dict):
        return False
    message = str(body.get('message', '')).strip().lower()
    return (body.get('code') in (5, '5', 'NOT_FOUND')
            and message in ('cluster not found', 'cluster does not exist', 'postgresql cluster not found'))


def owned_card(cid, name):
    status, card = common.pg_get(path(cid))
    if cluster_gone(status, card):
        return None
    require(status == 200 and isinstance(card, dict) and card.get('id') == cid and card.get('name') == name
            and card.get('project_id') == os.environ['CLOUDRU_PROJECT_ID'], 'cluster ownership or readback unconfirmed')
    return card


def child_rows(cid, field):
    out = tool('pg_' + field, cluster_id=cid)
    if isinstance(out, dict) and field in out and out[field] is None:
        # The CLI's typed empty slice can encode as null. Confirm the service page before using it.
        status, body = common.pg_get(path(cid, '/' + field))
        require(status == 200 and isinstance(body, dict) and body.get(field, []) == []
                and body.get('next_page_token', '') == '', 'empty child list unconfirmed')
        return []
    return rows(out, field)


def backups(cid, manual):
    # Capture every identity before losing access to its parent, including the initial service backup.
    listed = child_rows(cid, 'backups')
    result = {}
    for row in listed:
        bid, name = row.get('id'), row.get('name')
        require(text(bid) and text(name) and bid not in result and name not in result.values()
                and row.get('cluster_id', cid) == cid, 'backup identity unavailable')
        result[bid] = name
        common.note('pg-backup', 'pg-backup:' + hashlib.sha256(bid.encode()).hexdigest(), id=bid, name=name, parent=cid,
                    source=('manual' if name == manual else 'provider-listed'), state='seen')
    return result


def children(cid, field):
    items = child_rows(cid, field)
    require(all(text(i.get('name')) and i.get('cluster_id', cid) == cid for i in items), 'child scope incomplete')
    names = {i['name'] for i in items}
    require(len(names) == len(items), 'duplicated child identity')
    return names


def run():
    until = time.monotonic() + 30 * 60
    require(CLUSTER not in common.ledger(), 'create already attempted')
    args = preflight()
    name = args['name']
    require(time.monotonic() < until, 'active budget expired')
    # Record before POST. A timeout is never permission to adopt a by-name list result or retry creation.
    common.note('pg-cluster', CLUSTER, name=name, project=os.environ['CLOUDRU_PROJECT_ID'], state='create-intent')
    made = tool('pg_create', mode='--yes', **args)
    opid = made.get('operation_id')
    require(made.get('status') == 'creating' and text(opid), 'no create operation receipt')
    provisional = made.get('cluster_id') or ''
    common.note('pg-cluster', CLUSTER, operation=opid, provisional=provisional, state='creating')
    cid = target_id(opid, provisional, until)
    common.note('pg-cluster', CLUSTER, id=cid, state='created')
    def ready():
        card = owned_card(cid, name)
        require(card is not None and card.get('status') not in ('FAILED', 'ERASING', 'ERASED'), 'cluster did not become ready')
        return card.get('status') == 'READY'
    wait(until, ready, 'ready cluster')
    require(any(c['id'] == cid and c['name'] == name and c.get('status') == 'READY' for c in clusters()), 'cluster list differs from get')
    user, db, manual = 'p90_user', 'p90_db', 'p90-backup'
    users, databases = children(cid, 'users'), children(cid, 'databases')
    require(user not in users and db not in databases, 'child name already in use')
    before = backups(cid, manual)
    common.note('pg-cluster', CLUSTER, baseline_users=sorted(users), baseline_databases=sorted(databases), baseline_backups=sorted(before))
    require(manual not in before.values(), 'backup name already in use')
    # Move the password in a private file to avoid credentials in process arguments, output and receipts.
    require(time.monotonic() < until, 'active budget expired')
    common.note('pg-user', 'pg-user', parent=cid, name=user, state='create-intent')
    with tempfile.NamedTemporaryFile(mode='w', dir=os.environ['RUNNER_TEMP']) as secret:
        json.dump(dict(cluster_id=cid, name=user, password=secrets.token_hex(16) + '!aQ9'), secret)
        secret.flush()
        answer = common.call_evo('--yes', 'tools', 'call', 'pg_user_create', '--args-file', secret.name, '--json', timeout=50)
    require(answer.get('status') == 'created' and answer.get('user', {}).get('name') == user, 'user receipt missing')
    common.note('pg-user', 'pg-user', parent=cid, name=user, state='created')
    wait(until, lambda: user in children(cid, 'users'), 'created user')
    require(time.monotonic() < until, 'active budget expired')
    common.note('pg-db', 'pg-db', parent=cid, name=db, state='create-intent')
    answer = tool('pg_db_create', mode='--yes', cluster_id=cid, name=db, owner=user)
    require(answer.get('status') == 'created' and answer.get('database', {}).get('name') == db, 'database receipt missing')
    common.note('pg-db', 'pg-db', parent=cid, name=db, state='created')
    wait(until, lambda: db in children(cid, 'databases'), 'created database')
    require(time.monotonic() < until, 'active budget expired')
    common.note('pg-manual', 'pg-manual', parent=cid, name=manual, state='create-intent')
    answer = tool('pg_backup_create', mode='--yes', cluster_id=cid, name=manual)
    bop = answer.get('operation_id')
    require(answer.get('status') == 'backing_up' and text(bop), 'backup operation receipt missing')
    common.note('pg-manual', 'pg-manual', operation=bop, provisional=answer.get('backup_id'), state='creating')
    completed = operation(bop, until)
    reported = answer.get('backup_id') or completed.get('resource_id')
    require(not (answer.get('backup_id') and completed.get('resource_id') and answer['backup_id'] != completed['resource_id']), 'backup receipt differs from operation')
    def manual_read():
        current = backups(cid, manual)
        exact = [bid for bid, n in current.items() if n == manual and bid not in before]
        require(not (reported and exact and exact != [reported]), 'backup receipt differs from list')
        return exact[0] if len(exact) == 1 else None
    # Some operations omit resource_id. The documented backup handle is (own cluster, exact backup name).
    bid = wait(until, manual_read, 'manual backup')
    common.note('pg-manual', 'pg-manual', id=bid, state='seen')
    for kind, field, owned in [('db', 'databases', db), ('user', 'users', user)]:
        require(time.monotonic() < until, 'active budget expired')
        tool('pg_' + kind + '_delete', mode='--yes', cluster_id=cid, name=owned)
        wait(until, lambda: owned not in children(cid, field), 'deleted child')
        common.note('pg-' + kind, 'pg-' + kind, state='absent')


def exact_backup_missing(status, body, cid, backup):
    # The PostgreSQL GetBackup route takes the parent's ID and the backup name. A
    # generic or parent error is ambiguous even after an entirely valid cluster delete.
    if status != 404 or not isinstance(body, dict) or body.get('code') not in (5, '5', 'NOT_FOUND'):
        return False
    msg = body.get('message')
    if not isinstance(msg, str):
        return False
    expected = 'clusters/' + cid + '/backups/' + backup['name']
    details = body.get('details', [])
    if not isinstance(details, list) or not all(isinstance(d, dict) for d in details):
        return False
    resources = [d for d in details if d.get('@type') == 'type.googleapis.com/google.rpc.ResourceInfo']
    named = any(msg == f'backup "{value}" not found' for value in (backup['name'], backup['id']))
    if resources:
        return ((msg.lower() in ('backup not found', 'postgresql backup not found') or
                 named) and
                all(d.get('resourceName') == expected and d.get('resourceType') in
                    ('Backup', 'backup', 'postgresql/Backup', 'postgresql.api.cloud.ru/Backup') for d in resources))
    return named


def cleanup():
    until = time.monotonic() + 20 * 60
    rec = common.ledger().get(CLUSTER)
    if not rec:
        return
    require(rec.get('kind') == 'pg-cluster' and rec.get('name') == marker()
            and rec.get('project') == os.environ['CLOUDRU_PROJECT_ID'], 'cleanup scope differs')
    require(text(rec.get('operation')), 'possible cluster after a lost response; no receipt for cleanup')
    cid = rec.get('id') or target_id(rec['operation'], rec.get('provisional'), until)
    common.note('pg-cluster', CLUSTER, id=cid)
    card = owned_card(cid, rec['name'])
    if card is not None and not rec.get('delete_sent'):
        current = common.ledger()
        for field, key in [('databases', 'pg-db'), ('users', 'pg-user')]:
            baseline = rec.get('baseline_' + field)
            require(isinstance(baseline, list), 'baseline unavailable; cannot exclude unrelated dependants')
            permitted = set(baseline)
            child = current.get(key, {})
            if child.get('parent') == cid and child.get('state') == 'created':
                permitted.add(child['name'])
            require(children(cid, field) <= permitted, 'unrelated child or unconfirmed create; cluster retained')
        # Parent deletion is the service's only supported removal of its backups.
        # On unreadable inventory there is no safe cascade delete. Keep the paid parent visible.
        common.note('pg-cluster', CLUSTER, final_inventory='unconfirmed')
        observed = backups(cid, 'p90-backup')
        own = set(rec.get('baseline_backups', []))
        manual = current.get('pg-manual', {})
        if manual.get('parent') == cid and manual.get('state') == 'seen':
            own.add(manual['id'])
        require(set(observed) <= own, 'unrelated backup or unconfirmed create; cluster retained')
        common.note('pg-cluster', CLUSTER, final_inventory='complete')
        common.note('pg-cluster', CLUSTER, delete_sent=True, state='delete-intent')
        answer = tool('pg_delete', mode='--yes', cluster_id=cid)
        require(answer.get('status') in ('deleting', 'deleted', 'already_deleted'), 'cluster delete not confirmed')
        if text(answer.get('operation_id')):
            common.note('pg-cluster', CLUSTER, delete_operation=answer['operation_id'])
    def absent():
        missing = owned_card(cid, rec['name']) is None
        return missing and not any(c['id'] == cid for c in clusters())
    wait(until, absent, 'deleted cluster')
    common.note('pg-cluster', CLUSTER, state='absent')
    evidence = common.ledger()
    unresolved = evidence[CLUSTER].get('final_inventory') != 'complete'
    if 'pg-manual' in evidence and evidence['pg-manual'].get('state') != 'seen':
        unresolved = True
    # The server also exposes the former parent's list. A positive identity is evidence
    # of retention, while a 400/404 (or an empty list rooted at a gone parent) is inconclusive.
    after = {}
    try:
        after_list = child_rows(cid, 'backups')
        require(all(text(v.get('id')) and text(v.get('name'))
                    and v.get('cluster_id', cid) == cid for v in after_list), 'incomplete post-delete list')
        after = {v['id']: v['name'] for v in after_list}
        common.note('pg-cluster', CLUSTER, post_delete_backup_list='read', post_delete_rows=len(after_list))
        unresolved = unresolved or bool(after_list)
    except (RuntimeError, OSError):
        common.note('pg-cluster', CLUSTER, post_delete_backup_list='unavailable')
    for key, item in evidence.items():
        if item.get('kind') != 'pg-backup' or item.get('parent') != cid:
            continue
        # 400/404 on a child of a gone parent proves nothing about that copy's own storage.
        try:
            status, body = common.pg_get(path(cid, '/backups/' + quote(item['name'], safe='')))
        except (RuntimeError, OSError):
            status, body = None, None
        live = (after.get(item['id']) == item['name']
                or (status == 200 and isinstance(body, dict) and body.get('id') == item['id']
                    and body.get('name') == item['name'] and body.get('cluster_id', cid) == cid))
        missing = not live and exact_backup_missing(status, body, cid, item)
        outcome = 'retained' if live else ('absent' if missing else 'unknown')
        common.note('pg-backup', key, state=outcome, readback_http=status)
        unresolved = unresolved or not missing
    require(not unresolved, 'backup cleanup unconfirmed; check individual receipts with the service')
